package event

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Event type tests ---

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Fatal("generated ID is empty")
	}
	if id1 == id2 {
		t.Fatalf("two generated IDs are the same: %s", id1)
	}
	// Format: 20060102T150405-xxxxxxxx
	if !strings.Contains(id1, "T") || !strings.Contains(id1, "-") {
		t.Fatalf("ID format incorrect: %s", id1)
	}
}

func TestHashLine(t *testing.T) {
	h1 := hashLine([]byte("hello\n"))
	h2 := hashLine([]byte("hello\n"))
	h3 := hashLine([]byte("world\n"))

	if h1 != h2 {
		t.Fatal("same input should produce same hash")
	}
	if h1 == h3 {
		t.Fatal("different input should produce different hash")
	}
	// hashLine returns first 8 chars of hex hash
	if len(h1) != 8 {
		t.Fatalf("hash length should be 8, got: %s", h1)
	}
}

func TestGenerateUUID(t *testing.T) {
	uuid1 := generateUUID()
	uuid2 := generateUUID()

	if uuid1 == "" {
		t.Fatal("generated UUID is empty")
	}
	if uuid1 == uuid2 {
		t.Fatal("two generated UUIDs are the same")
	}
	// UUID is 32 hex chars
	if len(uuid1) != 32 {
		t.Fatalf("UUID length should be 32, got: %d", len(uuid1))
	}
}

func TestParsePosition(t *testing.T) {
	tests := []struct {
		input   string
		file    string
		offset  int64
		wantErr bool
	}{
		{"events.001-a1b2c3d4.jsonl.gz:1024", "events.001-a1b2c3d4.jsonl.gz", 1024, false},
		{"events.999-e5f6g7h8.jsonl.gz:0", "events.999-e5f6g7h8.jsonl.gz", 0, false},
		{"events.001-a1b2c3d4.jsonl.gz:999999", "events.001-a1b2c3d4.jsonl.gz", 999999, false},
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
	p := Position{File: "events.001-a1b2c3d4.jsonl.gz", Offset: 2048}
	s := p.String()
	if s != "events.001-a1b2c3d4.jsonl.gz:2048" {
		t.Fatalf("String() = %q, want %q", s, "events.001-a1b2c3d4.jsonl.gz:2048")
	}

	// Round-trip test
	p2, err := ParsePosition(s)
	if err != nil {
		t.Fatal(err)
	}
	if p2.File != p.File || p2.Offset != p.Offset {
		t.Fatalf("round-trip failed: %+v != %+v", p2, p)
	}
}

// --- Bus core tests ---

func setupTestBus(t *testing.T) *Bus {
	t.Helper()
	dir := t.TempDir()
	bus := NewBus(filepath.Join(dir, "events"))
	if err := bus.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return bus
}

func TestBusInit(t *testing.T) {
	bus := setupTestBus(t)

	// Should have created directory
	if _, err := os.Stat(bus.Dir); err != nil {
		t.Fatalf("directory does not exist: %v", err)
	}

	// Should have created markers subdirectory
	if _, err := os.Stat(filepath.Join(bus.Dir, "markers")); err != nil {
		t.Fatalf("markers directory does not exist: %v", err)
	}

	// Should have created latest file
	name, err := bus.latestName()
	if err != nil {
		t.Fatalf("read latest failed: %v", err)
	}
	// Filename format: events.001-<hash>.jsonl.gz
	if !strings.HasPrefix(name, "events.001-") || !strings.HasSuffix(name, ".jsonl.gz") {
		t.Fatalf("latest = %q, want events.001-<hash>.jsonl.gz", name)
	}

	// Should have created event file
	if _, err := os.Stat(filepath.Join(bus.Dir, name)); err != nil {
		t.Fatalf("event file does not exist: %v", err)
	}

	// Duplicate Init should not error
	if err := bus.Init(); err != nil {
		t.Fatalf("duplicate Init failed: %v", err)
	}
}

func TestBusAddSingle(t *testing.T) {
	bus := setupTestBus(t)

	payload := json.RawMessage(`{"from": "alice@example.com"}`)
	evt, err := bus.Add("email.received", "inbox", payload)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if evt.ID == "" {
		t.Fatal("event ID is empty")
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

	// Check file stats (no more meta file)
	name, _ := bus.latestName()
	uncompressedSize, lineCount, _, err := bus.getFileStats(name)
	if err != nil {
		t.Fatal(err)
	}
	// 1 rotate event + 1 user event = 2 total lines
	if lineCount != 2 {
		t.Errorf("LineCount = %d, want 2", lineCount)
	}
	if uncompressedSize <= 0 {
		t.Errorf("UncompressedSize = %d, want > 0", uncompressedSize)
	}
}

func TestBusAddMultiple(t *testing.T) {
	bus := setupTestBus(t)

	for i := 0; i < 10; i++ {
		payload := json.RawMessage(fmt.Sprintf(`{"index": %d}`, i))
		_, err := bus.Add("test.event", "test", payload)
		if err != nil {
			t.Fatalf("Add[%d] failed: %v", i, err)
		}
	}

	name, _ := bus.latestName()
	uncompressedSize, lineCount, _, err := bus.getFileStats(name)
	if err != nil {
		t.Fatal(err)
	}
	// 1 rotate event + 10 user events = 11 total
	if lineCount != 11 {
		t.Errorf("LineCount = %d, want 11", lineCount)
	}
	if uncompressedSize <= 0 {
		t.Errorf("UncompressedSize = %d, want > 0", uncompressedSize)
	}
}

func TestBusListAll(t *testing.T) {
	bus := setupTestBus(t)

	// Add 3 events
	for i := 0; i < 3; i++ {
		payload := json.RawMessage(`{"i": ` + itoa(i) + `}`)
		_, err := bus.Add("test", "ch1", payload)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Channel without marker should list all (excluding rotate events)
	entries, err := bus.List("new-channel", 0)
	if err != nil {
		t.Fatal(err)
	}
	// Should only see user events, not rotate events
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}

	// Each entry should have correct position
	for i, e := range entries {
		if !strings.HasPrefix(e.File, "events.001-") {
			t.Errorf("[%d] File = %q", i, e.File)
		}
		if e.Offset <= 0 {
			t.Errorf("[%d] Offset = %d, want > 0", i, e.Offset)
		}
		if e.Type != "test" {
			t.Errorf("[%d] Type = %q", i, e.Type)
		}
		// Should not be rotate events
		if e.Type == RotateEventType {
			t.Errorf("[%d] should not be rotate event", i)
		}
	}

	// offsets should be increasing
	for i := 1; i < len(entries); i++ {
		if entries[i].Offset <= entries[i-1].Offset {
			t.Errorf("offset not increasing: [%d]=%d, [%d]=%d",
				i-1, entries[i-1].Offset, i, entries[i].Offset)
		}
	}
}

func TestBusListWithMarker(t *testing.T) {
	bus := setupTestBus(t)

	// Add 5 events
	for i := 0; i < 5; i++ {
		_, err := bus.Add("test", "ch1", json.RawMessage(`{"i":`+itoa(i)+`}`))
		if err != nil {
			t.Fatal(err)
		}
	}

	// List all
	all, err := bus.List("reader", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("len(all) = %d, want 5", len(all))
	}

	// Mark at 3rd event
	pos := Position{File: all[2].File, Offset: all[2].Offset}
	if err := bus.Mark("reader", pos); err != nil {
		t.Fatal(err)
	}

	// List again, should only have last 2
	remaining, err := bus.List("reader", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 {
		t.Fatalf("len(remaining) = %d, want 2", len(remaining))
	}

	// Mark to last
	lastPos := Position{File: remaining[1].File, Offset: remaining[1].Offset}
	if err := bus.Mark("reader", lastPos); err != nil {
		t.Fatal(err)
	}

	// Should have no new events
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

	// Get first file name
	firstFile, _ := bus.latestName()

	// Write enough events to trigger rotation
	// Each event is roughly 100-200 bytes, so we need many events
	// But for testing, we'll directly force a rotation by calling createNewFile
	unlock, err := bus.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	// Force create file 002
	_, err = bus.createNewFile(2)
	if err != nil {
		t.Fatalf("createNewFile failed: %v", err)
	}
	unlock()

	// Should have created events.002-<hash>.jsonl.gz
	name, err := bus.latestName()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "events.002-") {
		t.Fatalf("latest = %q, want events.002-<hash>.jsonl.gz", name)
	}

	// New file should have 1 record (rotate event)
	_, lineCount, _, err := bus.getFileStats(name)
	if err != nil {
		t.Fatal(err)
	}
	if lineCount != 1 {
		t.Errorf("LineCount = %d, want 1", lineCount)
	}

	// List all files
	files, err := bus.listFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}

	// Verify first file still exists
	if files[0] != firstFile {
		t.Errorf("first file = %q, want %q", files[0], firstFile)
	}
}

func TestBusListAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	bus := NewBus(filepath.Join(dir, "events"))
	if err := bus.Init(); err != nil {
		t.Fatal(err)
	}

	// Write 3 events to file 001
	for i := 0; i < 3; i++ {
		_, err := bus.Add("batch1", "ch", json.RawMessage(`{"batch":1,"i":`+itoa(i)+`}`))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Force rotation to file 002
	unlock, err := bus.lock()
	if err != nil {
		t.Fatal(err)
	}
	bus.createNewFile(2)
	unlock()

	// Write 2 more events (should go to file 002)
	for i := 0; i < 2; i++ {
		_, err := bus.Add("batch2", "ch", json.RawMessage(`{"batch":2,"i":`+itoa(i)+`}`))
		if err != nil {
			t.Fatal(err)
		}
	}

	latestName, _ := bus.latestName()
	if !strings.HasPrefix(latestName, "events.002-") {
		t.Fatalf("latest = %q, want events.002-<hash>.jsonl.gz", latestName)
	}

	// List all: should have 5 (3 + 2)
	all, err := bus.List("reader", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("len(all) = %d, want 5", len(all))
	}

	// First 3 in file 001
	for i := 0; i < 3; i++ {
		if !strings.HasPrefix(all[i].File, "events.001-") {
			t.Errorf("[%d] File = %q, want events.001-<hash>.jsonl.gz", i, all[i].File)
		}
	}
	// Last 2 in file 002
	for i := 3; i < 5; i++ {
		if !strings.HasPrefix(all[i].File, "events.002-") {
			t.Errorf("[%d] File = %q, want events.002-<hash>.jsonl.gz", i, all[i].File)
		}
	}

	// Mark at end of file 001
	pos := Position{File: all[2].File, Offset: all[2].Offset}
	if err := bus.Mark("reader", pos); err != nil {
		t.Fatal(err)
	}

	// Should only list file 002's 2 events
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

	// Default status (latest)
	st, err := bus.Status("")
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsLatest {
		t.Error("should be latest")
	}
	// 1 rotate + 1 user = 2 lines
	if st.LineCount != 2 {
		t.Errorf("LineCount = %d, want 2", st.LineCount)
	}
	if st.CompressedSize <= 0 {
		t.Errorf("CompressedSize = %d, want > 0", st.CompressedSize)
	}
	if st.UncompressedSize <= 0 {
		t.Errorf("UncompressedSize = %d, want > 0", st.UncompressedSize)
	}

	// Status by filename
	name, _ := bus.latestName()
	st2, err := bus.Status(name)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Name != name {
		t.Errorf("Name = %q", st2.Name)
	}

	// Non-existent file
	_, err = bus.Status("events.999-a1b2c3d4.jsonl.gz")
	if err == nil {
		t.Fatal("should error")
	}
}

func TestBusParseSeq(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{"events.001-a1b2c3d4.jsonl.gz", 1},
		{"events.010-e5f6g7h8.jsonl.gz", 10},
		{"events.999-i9j0k1l2.jsonl.gz", 999},
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

	// Add events to different channels
	bus.Add("a", "ch1", json.RawMessage(`{}`))
	bus.Add("b", "ch2", json.RawMessage(`{}`))
	bus.Add("c", "ch1", json.RawMessage(`{}`))

	// ch1 should list 3 (all events, List doesn't filter by channel)
	all, _ := bus.List("ch1-reader", 0)
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(all))
	}

	// Two independent readers with their own marks
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

	err := bus.Mark("test", Position{File: "events.999-a1b2c3d4.jsonl.gz", Offset: 0})
	if err == nil {
		t.Fatal("should error: file does not exist")
	}
}

func TestRotateEvent(t *testing.T) {
	bus := setupTestBus(t)

	// Read the raw file to verify it has a rotate event at the start
	name, _ := bus.latestName()

	// Read and decompress the file directly
	fpath := filepath.Join(bus.Dir, name)
	f, err := os.Open(fpath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()

	// Read first line
	scanner := bufio.NewScanner(gr)
	if !scanner.Scan() {
		t.Fatal("file should have at least one line")
	}

	line := scanner.Bytes()
	var evt Event
	if err := json.Unmarshal(line, &evt); err != nil {
		t.Fatalf("failed to unmarshal event: %v", err)
	}

	// First entry should be rotate event
	if evt.Type != RotateEventType {
		t.Errorf("first event type = %q, want %s", evt.Type, RotateEventType)
	}

	// Verify payload contains UUID
	var rotateEvt RotateEvent
	if err := json.Unmarshal(evt.Payload, &rotateEvt); err != nil {
		t.Fatalf("failed to unmarshal rotate event: %v", err)
	}
	if rotateEvt.UUID == "" {
		t.Error("rotate event UUID is empty")
	}
	if len(rotateEvt.UUID) != 32 {
		t.Errorf("UUID length = %d, want 32", len(rotateEvt.UUID))
	}
}

func TestFilenameContainsHash(t *testing.T) {
	bus := setupTestBus(t)

	name, err := bus.latestName()
	if err != nil {
		t.Fatal(err)
	}

	// Filename format: events.001-<hash>.jsonl.gz
	// Extract hash from filename
	parts := strings.Split(strings.TrimPrefix(name, "events."), "-")
	if len(parts) < 2 {
		t.Fatalf("filename format incorrect: %s", name)
	}
	hashInFilename := strings.TrimSuffix(parts[1], ".jsonl.gz")

	// Hash should be 8 chars
	if len(hashInFilename) != 8 {
		t.Errorf("hash in filename length = %d, want 8", len(hashInFilename))
	}
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
