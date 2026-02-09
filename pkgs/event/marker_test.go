package event

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMarkerSaveLoad(t *testing.T) {
	bus := setupTestBus(t)

	m := &Marker{
		File:          "events.001.jsonl.gz",
		FirstLineHash: "sha256:abc123",
		Offset:        2048,
		UpdatedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := bus.SaveMarker("my-channel", m); err != nil {
		t.Fatalf("SaveMarker 失败: %v", err)
	}

	loaded, err := bus.LoadMarker("my-channel")
	if err != nil {
		t.Fatalf("LoadMarker 失败: %v", err)
	}

	if loaded.File != m.File {
		t.Errorf("File = %q, want %q", loaded.File, m.File)
	}
	if loaded.FirstLineHash != m.FirstLineHash {
		t.Errorf("FirstLineHash = %q, want %q", loaded.FirstLineHash, m.FirstLineHash)
	}
	if loaded.Offset != m.Offset {
		t.Errorf("Offset = %d, want %d", loaded.Offset, m.Offset)
	}
}

func TestMarkerLoadNotExist(t *testing.T) {
	bus := setupTestBus(t)

	_, err := bus.LoadMarker("nonexistent")
	if !os.IsNotExist(err) {
		t.Fatalf("应返回 os.IsNotExist 错误, got: %v", err)
	}
}

func TestMarkerOverwrite(t *testing.T) {
	bus := setupTestBus(t)

	m1 := &Marker{File: "events.001.jsonl.gz", Offset: 100, UpdatedAt: time.Now().UTC()}
	bus.SaveMarker("ch", m1)

	m2 := &Marker{File: "events.001.jsonl.gz", Offset: 200, UpdatedAt: time.Now().UTC()}
	bus.SaveMarker("ch", m2)

	loaded, _ := bus.LoadMarker("ch")
	if loaded.Offset != 200 {
		t.Errorf("Offset = %d, want 200", loaded.Offset)
	}
}

func TestListChannels(t *testing.T) {
	bus := setupTestBus(t)

	// 空列表
	channels, err := bus.ListChannels()
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 0 {
		t.Fatalf("len = %d, want 0", len(channels))
	}

	// 添加 marker
	m := &Marker{File: "events.001.jsonl.gz", Offset: 0, UpdatedAt: time.Now().UTC()}
	bus.SaveMarker("alpha", m)
	bus.SaveMarker("beta", m)
	bus.SaveMarker("gamma", m)

	channels, err = bus.ListChannels()
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 3 {
		t.Fatalf("len = %d, want 3", len(channels))
	}

	// 应该包含所有 channel
	found := map[string]bool{}
	for _, ch := range channels {
		found[ch] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !found[want] {
			t.Errorf("缺少 channel: %s", want)
		}
	}
}

func TestSanitizeChannel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with spaces", "with_spaces"},
		{"path/to/channel", "path_to_channel"},
		{"special:chars*here", "special_chars_here"},
		{"normal-name.v2", "normal-name.v2"},
	}

	for _, tt := range tests {
		got := sanitizeChannel(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeChannel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateMarker(t *testing.T) {
	bus := setupTestBus(t)

	// 写入一个事件使 meta 有 firstLineHash
	bus.Add("test", "ch", []byte(`{}`))

	meta, _ := bus.loadMeta("events.001.jsonl.gz")

	// 有效 marker
	m := &Marker{
		File:          "events.001.jsonl.gz",
		FirstLineHash: meta.FirstLineHash,
		Offset:        0,
	}
	if err := bus.ValidateMarker(m); err != nil {
		t.Fatalf("ValidateMarker 应通过: %v", err)
	}

	// hash 不匹配
	m.FirstLineHash = "sha256:wrong"
	if err := bus.ValidateMarker(m); err == nil {
		t.Fatal("hash 不匹配应报错")
	}

	// 文件不存在
	m.File = "events.999.jsonl.gz"
	if err := bus.ValidateMarker(m); err == nil {
		t.Fatal("文件不存在应报错")
	}
}

func TestMarkerPathSafety(t *testing.T) {
	bus := setupTestBus(t)

	path := bus.markerPath("../../etc/passwd")
	// 应该被 sanitize 到 markers 目录下
	dir := filepath.Dir(path)
	if filepath.Base(dir) != "markers" {
		t.Errorf("marker 应在 markers/ 下, got: %s", path)
	}
}
