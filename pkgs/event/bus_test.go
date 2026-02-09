package event

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Event 类型测试 ---

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Fatal("生成的 ID 为空")
	}
	if id1 == id2 {
		t.Fatalf("两次生成的 ID 相同: %s", id1)
	}
	// 格式: 20060102T150405-xxxxxxxx
	if !strings.Contains(id1, "T") || !strings.Contains(id1, "-") {
		t.Fatalf("ID 格式不正确: %s", id1)
	}
}

func TestHashLine(t *testing.T) {
	h1 := hashLine([]byte("hello\n"))
	h2 := hashLine([]byte("hello\n"))
	h3 := hashLine([]byte("world\n"))

	if h1 != h2 {
		t.Fatal("相同输入应产生相同 hash")
	}
	if h1 == h3 {
		t.Fatal("不同输入应产生不同 hash")
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Fatalf("hash 应以 sha256: 开头: %s", h1)
	}
}

func TestParsePosition(t *testing.T) {
	tests := []struct {
		input   string
		file    string
		offset  int64
		wantErr bool
	}{
		{"events.001.jsonl.gz:1024", "events.001.jsonl.gz", 1024, false},
		{"events.999.jsonl.gz:0", "events.999.jsonl.gz", 0, false},
		{"events.001.jsonl.gz:999999", "events.001.jsonl.gz", 999999, false},
		{"invalid", "", 0, true},
		{"", "", 0, true},
		{":123", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pos, err := ParsePosition(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParsePosition(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err == nil {
				if pos.File != tt.file {
					t.Errorf("File = %q, want %q", pos.File, tt.file)
				}
				if pos.Offset != tt.offset {
					t.Errorf("Offset = %d, want %d", pos.Offset, tt.offset)
				}
			}
		})
	}
}

func TestPositionString(t *testing.T) {
	p := Position{File: "events.001.jsonl.gz", Offset: 2048}
	s := p.String()
	if s != "events.001.jsonl.gz:2048" {
		t.Fatalf("String() = %q, want %q", s, "events.001.jsonl.gz:2048")
	}

	// 往返测试
	p2, err := ParsePosition(s)
	if err != nil {
		t.Fatal(err)
	}
	if p2.File != p.File || p2.Offset != p.Offset {
		t.Fatalf("往返失败: %+v != %+v", p2, p)
	}
}

// --- Bus 核心测试 ---

func setupTestBus(t *testing.T) *Bus {
	t.Helper()
	dir := t.TempDir()
	bus := NewBus(filepath.Join(dir, "events"))
	if err := bus.Init(); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	return bus
}

func TestBusInit(t *testing.T) {
	bus := setupTestBus(t)

	// 应该创建了目录
	if _, err := os.Stat(bus.Dir); err != nil {
		t.Fatalf("目录不存在: %v", err)
	}

	// 应该创建了 markers 子目录
	if _, err := os.Stat(filepath.Join(bus.Dir, "markers")); err != nil {
		t.Fatalf("markers 目录不存在: %v", err)
	}

	// 应该创建了 latest 文件
	name, err := bus.latestName()
	if err != nil {
		t.Fatalf("读取 latest 失败: %v", err)
	}
	if name != "events.001.jsonl.gz" {
		t.Fatalf("latest = %q, want events.001.jsonl.gz", name)
	}

	// 应该创建了事件文件
	if _, err := os.Stat(filepath.Join(bus.Dir, name)); err != nil {
		t.Fatalf("事件文件不存在: %v", err)
	}

	// 重复 Init 应该不报错
	if err := bus.Init(); err != nil {
		t.Fatalf("重复 Init 失败: %v", err)
	}
}

func TestBusAddSingle(t *testing.T) {
	bus := setupTestBus(t)

	payload := json.RawMessage(`{"from": "alice@example.com"}`)
	evt, err := bus.Add("email.received", "inbox", payload)
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}

	if evt.ID == "" {
		t.Fatal("事件 ID 为空")
	}
	if evt.Type != "email.received" {
		t.Errorf("Type = %q, want email.received", evt.Type)
	}
	if evt.Channel != "inbox" {
		t.Errorf("Channel = %q, want inbox", evt.Channel)
	}
	if string(evt.Payload) != `{"from": "alice@example.com"}` {
		t.Errorf("Payload = %s", string(evt.Payload))
	}

	// 检查 meta 更新
	meta, err := bus.loadMeta("events.001.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	if meta.LineCount != 1 {
		t.Errorf("LineCount = %d, want 1", meta.LineCount)
	}
	if meta.UncompressedSize <= 0 {
		t.Errorf("UncompressedSize = %d, want > 0", meta.UncompressedSize)
	}
	if meta.FirstLineHash == "" {
		t.Error("FirstLineHash 为空")
	}
}

func TestBusAddMultiple(t *testing.T) {
	bus := setupTestBus(t)

	for i := 0; i < 10; i++ {
		payload := json.RawMessage(fmt.Sprintf(`{"index": %d}`, i))
		_, err := bus.Add("test.event", "test", payload)
		if err != nil {
			t.Fatalf("Add[%d] 失败: %v", i, err)
		}
	}

	meta, err := bus.loadMeta("events.001.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	if meta.LineCount != 10 {
		t.Errorf("LineCount = %d, want 10", meta.LineCount)
	}
}

func TestBusListAll(t *testing.T) {
	bus := setupTestBus(t)

	// 添加 3 个事件
	for i := 0; i < 3; i++ {
		payload := json.RawMessage(`{"i": ` + strings.Repeat("x", i+1) + `}`)
		// 修正: 使用有效 JSON
		payload = json.RawMessage(`{"i": ` + itoa(i) + `}`)
		_, err := bus.Add("test", "ch1", payload)
		if err != nil {
			t.Fatal(err)
		}
	}

	// 不带 marker 的 channel，应列出所有
	entries, err := bus.List("new-channel", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}

	// 每个 entry 应有正确的位置
	for i, e := range entries {
		if e.File != "events.001.jsonl.gz" {
			t.Errorf("[%d] File = %q", i, e.File)
		}
		if e.Offset <= 0 {
			t.Errorf("[%d] Offset = %d, want > 0", i, e.Offset)
		}
		if e.Type != "test" {
			t.Errorf("[%d] Type = %q", i, e.Type)
		}
	}

	// offset 应该递增
	for i := 1; i < len(entries); i++ {
		if entries[i].Offset <= entries[i-1].Offset {
			t.Errorf("offset 未递增: [%d]=%d, [%d]=%d",
				i-1, entries[i-1].Offset, i, entries[i].Offset)
		}
	}
}

func TestBusListWithMarker(t *testing.T) {
	bus := setupTestBus(t)

	// 添加 5 个事件
	for i := 0; i < 5; i++ {
		_, err := bus.Add("test", "ch1", json.RawMessage(`{"i":`+itoa(i)+`}`))
		if err != nil {
			t.Fatal(err)
		}
	}

	// 列出所有
	all, err := bus.List("reader", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("len(all) = %d, want 5", len(all))
	}

	// 在第 3 个事件处 mark
	pos := Position{File: all[2].File, Offset: all[2].Offset}
	if err := bus.Mark("reader", pos); err != nil {
		t.Fatal(err)
	}

	// 再列出，应只有后 2 个
	remaining, err := bus.List("reader", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 {
		t.Fatalf("len(remaining) = %d, want 2", len(remaining))
	}

	// mark 到最后
	lastPos := Position{File: remaining[1].File, Offset: remaining[1].Offset}
	if err := bus.Mark("reader", lastPos); err != nil {
		t.Fatal(err)
	}

	// 应该没有新事件
	empty, err := bus.List("reader", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("len(empty) = %d, want 0", len(empty))
	}
}

func TestBusListLimit(t *testing.T) {
	bus := setupTestBus(t)

	for i := 0; i < 10; i++ {
		_, err := bus.Add("test", "ch1", json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
	}

	entries, err := bus.List("reader", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
}

func TestBusRotation(t *testing.T) {
	dir := t.TempDir()
	bus := NewBus(filepath.Join(dir, "events"))
	if err := bus.Init(); err != nil {
		t.Fatal(err)
	}

	// 手动设置 meta 使其接近 rotation 阈值
	meta := &FileMeta{
		UncompressedSize: MaxUncompressedSize - RotationHeadroom + 1, // 会超过阈值
		LineCount:        1000,
		FirstLineHash:    "sha256:fake",
	}
	if err := bus.saveMeta("events.001.jsonl.gz", meta); err != nil {
		t.Fatal(err)
	}

	// 下次 add 应触发 rotation
	_, err := bus.Add("test", "ch1", json.RawMessage(`{"big": true}`))
	if err != nil {
		t.Fatal(err)
	}

	// 应该创建了 events.002.jsonl.gz
	name, err := bus.latestName()
	if err != nil {
		t.Fatal(err)
	}
	if name != "events.002.jsonl.gz" {
		t.Fatalf("latest = %q, want events.002.jsonl.gz", name)
	}

	// 新文件应该有 1 条记录
	meta2, err := bus.loadMeta("events.002.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	if meta2.LineCount != 1 {
		t.Errorf("LineCount = %d, want 1", meta2.LineCount)
	}

	// 列出所有文件
	files, err := bus.listFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
}

func TestBusListAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	bus := NewBus(filepath.Join(dir, "events"))
	if err := bus.Init(); err != nil {
		t.Fatal(err)
	}

	// 往 file 001 写 3 个事件
	for i := 0; i < 3; i++ {
		_, err := bus.Add("batch1", "ch", json.RawMessage(`{"batch":1,"i":`+itoa(i)+`}`))
		if err != nil {
			t.Fatal(err)
		}
	}

	// 手动强制 rotation
	meta, _ := bus.loadMeta("events.001.jsonl.gz")
	meta.UncompressedSize = MaxUncompressedSize // 强制超过
	bus.saveMeta("events.001.jsonl.gz", meta)

	// 再写 2 个事件（应该到 file 002）
	for i := 0; i < 2; i++ {
		_, err := bus.Add("batch2", "ch", json.RawMessage(`{"batch":2,"i":`+itoa(i)+`}`))
		if err != nil {
			t.Fatal(err)
		}
	}

	latestName, _ := bus.latestName()
	if latestName != "events.002.jsonl.gz" {
		t.Fatalf("latest = %q, want events.002.jsonl.gz", latestName)
	}

	// 列出所有：应有 5 个 (3 + 2)
	all, err := bus.List("reader", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("len(all) = %d, want 5", len(all))
	}

	// 前 3 个在 file 001
	for i := 0; i < 3; i++ {
		if all[i].File != "events.001.jsonl.gz" {
			t.Errorf("[%d] File = %q, want events.001.jsonl.gz", i, all[i].File)
		}
	}
	// 后 2 个在 file 002
	for i := 3; i < 5; i++ {
		if all[i].File != "events.002.jsonl.gz" {
			t.Errorf("[%d] File = %q, want events.002.jsonl.gz", i, all[i].File)
		}
	}

	// 在 file 001 的最后 mark
	pos := Position{File: all[2].File, Offset: all[2].Offset}
	if err := bus.Mark("reader", pos); err != nil {
		t.Fatal(err)
	}

	// 只应列出 file 002 的 2 个
	remaining, err := bus.List("reader", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 {
		t.Fatalf("len(remaining) = %d, want 2", len(remaining))
	}
}

func TestBusStatus(t *testing.T) {
	bus := setupTestBus(t)

	_, err := bus.Add("test", "ch1", json.RawMessage(`{"hello": "world"}`))
	if err != nil {
		t.Fatal(err)
	}

	// 默认 status (latest)
	st, err := bus.Status("")
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsLatest {
		t.Error("应该是 latest")
	}
	if st.LineCount != 1 {
		t.Errorf("LineCount = %d, want 1", st.LineCount)
	}
	if st.CompressedSize <= 0 {
		t.Errorf("CompressedSize = %d, want > 0", st.CompressedSize)
	}
	if st.UncompressedSize <= 0 {
		t.Errorf("UncompressedSize = %d, want > 0", st.UncompressedSize)
	}

	// 指定文件名 status
	st2, err := bus.Status("events.001.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	if st2.Name != "events.001.jsonl.gz" {
		t.Errorf("Name = %q", st2.Name)
	}

	// 不存在的文件
	_, err = bus.Status("events.999.jsonl.gz")
	if err == nil {
		t.Fatal("应该报错")
	}
}

func TestBusParseSeq(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{"events.001.jsonl.gz", 1},
		{"events.010.jsonl.gz", 10},
		{"events.999.jsonl.gz", 999},
	}

	for _, tt := range tests {
		got := parseSeq(tt.name)
		if got != tt.want {
			t.Errorf("parseSeq(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestBusMultipleChannels(t *testing.T) {
	bus := setupTestBus(t)

	// 添加事件到不同 channel
	bus.Add("a", "ch1", json.RawMessage(`{}`))
	bus.Add("b", "ch2", json.RawMessage(`{}`))
	bus.Add("c", "ch1", json.RawMessage(`{}`))

	// ch1 应列出 3 个 (所有事件，因为 List 不按 channel 过滤内容)
	all, _ := bus.List("ch1-reader", 0)
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(all))
	}

	// 两个独立的 reader 各自 mark
	bus.Mark("reader-a", Position{File: all[0].File, Offset: all[0].Offset})
	bus.Mark("reader-b", Position{File: all[1].File, Offset: all[1].Offset})

	ra, _ := bus.List("reader-a", 0)
	rb, _ := bus.List("reader-b", 0)

	if len(ra) != 2 {
		t.Errorf("reader-a: len = %d, want 2", len(ra))
	}
	if len(rb) != 1 {
		t.Errorf("reader-b: len = %d, want 1", len(rb))
	}
}

func TestBusEmptyList(t *testing.T) {
	bus := setupTestBus(t)

	entries, err := bus.List("empty-channel", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(entries))
	}
}

func TestBusMarkInvalidFile(t *testing.T) {
	bus := setupTestBus(t)

	err := bus.Mark("test", Position{File: "events.999.jsonl.gz", Offset: 0})
	if err == nil {
		t.Fatal("应该报错: 文件不存在")
	}
}

func itoa(i int) string {
	return strings.TrimSpace("  " + itoa2(i) + "  ")
}

func itoa2(i int) string {
	b := make([]byte, 0, 4)
	b = append(b, byte('0'+i%10))
	return string(b)
}
