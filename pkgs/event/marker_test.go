package event

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMarkerSaveLoad(t *testing.T) {
	bus := setupTestBus(t)

	// Get the actual filename created by the bus
	actualFile, _ := bus.latestName()

	m := &Marker{
		File:      actualFile,
		Offset:    2048,
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := bus.SaveMarker("my-channel", m); err != nil {
		t.Fatalf("SaveMarker failed: %v", err)
	}

	loaded, err := bus.LoadMarker("my-channel")
	if err != nil {
		t.Fatalf("LoadMarker failed: %v", err)
	}

	if loaded.File != m.File {
		t.Errorf("File = %q, want %q", loaded.File, m.File)
	}
	if loaded.Offset != m.Offset {
		t.Errorf("Offset = %d, want %d", loaded.Offset, m.Offset)
	}
}

func TestMarkerLoadNotExist(t *testing.T) {
	bus := setupTestBus(t)

	_, err := bus.LoadMarker("nonexistent")
	if !os.IsNotExist(err) {
		t.Fatalf("should return os.IsNotExist error, got: %v", err)
	}
}

func TestMarkerOverwrite(t *testing.T) {
	bus := setupTestBus(t)

	actualFile, _ := bus.latestName()

	m1 := &Marker{File: actualFile, Offset: 100, UpdatedAt: time.Now().UTC()}
	bus.SaveMarker("ch", m1)

	m2 := &Marker{File: actualFile, Offset: 200, UpdatedAt: time.Now().UTC()}
	bus.SaveMarker("ch", m2)

	loaded, _ := bus.LoadMarker("ch")
	if loaded.Offset != 200 {
		t.Errorf("Offset = %d, want 200", loaded.Offset)
	}
}

func TestListChannels(t *testing.T) {
	bus := setupTestBus(t)

	actualFile, _ := bus.latestName()

	// Empty list
	channels, err := bus.ListChannels()
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 0 {
		t.Fatalf("len = %d, want 0", len(channels))
	}

	// Add markers
	m := &Marker{File: actualFile, Offset: 0, UpdatedAt: time.Now().UTC()}
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

	// Should contain all channels
	found := map[string]bool{}
	for _, ch := range channels {
		found[ch] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !found[want] {
			t.Errorf("missing channel: %s", want)
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

func TestMarkerPathSafety(t *testing.T) {
	bus := setupTestBus(t)

	path := bus.markerPath("../../etc/passwd")
	// Should be sanitized to markers directory
	dir := filepath.Dir(path)
	if filepath.Base(dir) != "markers" {
		t.Errorf("marker should be under markers/, got: %s", path)
	}
}

func TestMarkerWithRotateEvent(t *testing.T) {
	bus := setupTestBus(t)

	// Add a user event
	bus.Add("test", "ch", json.RawMessage(`{}`))

	// Get the latest file name (contains hash)
	name, err := bus.latestName()
	if err != nil {
		t.Fatal(err)
	}

	// Verify filename format: events.NNN-<hash>.jsonl.gz
	if !strings.HasPrefix(name, "events.001-") || !strings.HasSuffix(name, ".jsonl.gz") {
		t.Fatalf("filename format incorrect: %s", name)
	}

	// Create a marker at position after the user event
	entries, err := bus.List("reader", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("should have at least one event")
	}

	// Mark at the end of the first event
	pos := Position{File: entries[0].File, Offset: entries[0].Offset}
	if err := bus.Mark("reader", pos); err != nil {
		t.Fatalf("Mark failed: %v", err)
	}

	// Load marker and verify
	loaded, err := bus.LoadMarker("reader")
	if err != nil {
		t.Fatalf("LoadMarker failed: %v", err)
	}
	if loaded.File != entries[0].File {
		t.Errorf("File = %q, want %q", loaded.File, entries[0].File)
	}
	if loaded.Offset != entries[0].Offset {
		t.Errorf("Offset = %d, want %d", loaded.Offset, entries[0].Offset)
	}

	// Verify marker doesn't contain FirstLineHash
	data, err := os.ReadFile(bus.markerPath("reader"))
	if err != nil {
		t.Fatal(err)
	}
	var markerMap map[string]interface{}
	if err := json.Unmarshal(data, &markerMap); err != nil {
		t.Fatal(err)
	}
	if _, exists := markerMap["first_line_hash"]; exists {
		t.Error("marker should not contain first_line_hash field")
	}
}
