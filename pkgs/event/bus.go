package event

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// fileTracking tracks in-memory stats for the current file.
type fileTracking struct {
	uncompressedSize int64
	lineCount        int64
}

// Bus is a file-based EventBus.
type Bus struct {
	Dir string // Event storage directory

	// In-memory tracking for current file (only valid during lock lifetime)
	tracking map[string]*fileTracking
}

// NewBus creates an EventBus using the specified directory.
func NewBus(dir string) *Bus {
	return &Bus{
		Dir:      dir,
		tracking: make(map[string]*fileTracking),
	}
}

// DefaultBus creates an EventBus using the default path (~/.emx-mail/events/).
func DefaultBus() (*Bus, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	dir := filepath.Join(home, ".emx-mail", "events")
	return NewBus(dir), nil
}

// Init initializes the event directory, creating necessary subdirectories and the first events file.
func (b *Bus) Init() error {
	if err := os.MkdirAll(filepath.Join(b.Dir, "markers"), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	// If there's no latest file yet, create the first file
	_, err := b.latestName()
	if err != nil {
		_, err = b.createNewFile(1)
		return err
	}
	return nil
}

// Add adds an event to the EventBus. Protected by exclusive lock.
func (b *Bus) Add(typ, channel string, payload json.RawMessage) (*Event, error) {
	unlock, err := b.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	if err := b.Init(); err != nil {
		return nil, err
	}

	evt := &Event{
		ID:        generateID(),
		Timestamp: time.Now().UTC(),
		Type:      typ,
		Channel:   channel,
		Payload:   payload,
	}

	line, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize event: %w", err)
	}
	line = append(line, '\n')

	// Check if rotation is needed
	latestFile, err := b.latestName()
	if err != nil {
		return nil, err
	}

	tracking := b.getTracking(latestFile)
	if tracking.uncompressedSize+int64(len(line))+RotationHeadroom >= MaxUncompressedSize {
		// Need to rotate
		seq := parseSeq(latestFile)
		newFile, err := b.createNewFile(seq + 1)
		if err != nil {
			return nil, fmt.Errorf("rotation failed: %w", err)
		}
		latestFile = newFile
		tracking = b.getTracking(latestFile)
	}

	// Append event to gzip file (concatenate new gzip member)
	fpath := filepath.Join(b.Dir, latestFile)
	f, err := os.OpenFile(fpath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open event file: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	if _, err := gw.Write(line); err != nil {
		return nil, fmt.Errorf("failed to write event: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	// Update tracking
	tracking.uncompressedSize += int64(len(line))
	tracking.lineCount++

	return evt, nil
}

// List lists new events from the specified channel starting from the marker position.
// If the channel has no marker, starts from the earliest file.
// limit <= 0 means no limit.
func (b *Bus) List(channel string, limit int) ([]EventEntry, error) {
	unlock, err := b.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	marker, err := b.LoadMarker(channel)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	files, err := b.listFiles()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	var startFile string
	var startOffset int64

	if marker != nil {
		startFile = marker.File
		startOffset = marker.Offset
	} else {
		startFile = files[0]
		startOffset = 0
	}

	// Find starting file index
	startIdx := 0
	for i, f := range files {
		if f == startFile {
			startIdx = i
			break
		}
	}

	var entries []EventEntry
	for i := startIdx; i < len(files); i++ {
		f := files[i]
		offset := int64(0)
		if i == startIdx {
			offset = startOffset
		}

		events, err := b.readFile(f, offset)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", f, err)
		}
		entries = append(entries, events...)
		if limit > 0 && len(entries) >= limit {
			entries = entries[:limit]
			break
		}
	}

	return entries, nil
}

// Mark updates the consumption position for a channel.
func (b *Bus) Mark(channel string, pos Position) error {
	unlock, err := b.lock()
	if err != nil {
		return err
	}
	defer unlock()

	// Verify file exists
	fpath := filepath.Join(b.Dir, pos.File)
	if _, err := os.Stat(fpath); err != nil {
		return fmt.Errorf("event file %s does not exist: %w", pos.File, err)
	}

	m := &Marker{
		File:      pos.File,
		Offset:    pos.Offset,
		UpdatedAt: time.Now().UTC(),
	}

	return b.SaveMarker(channel, m)
}

// Status returns the status of the specified file, empty name means latest.
func (b *Bus) Status(name string) (*FileStatus, error) {
	if name == "" {
		var err error
		name, err = b.latestName()
		if err != nil {
			return nil, fmt.Errorf("no active event file: %w", err)
		}
	}

	fpath := filepath.Join(b.Dir, name)
	fi, err := os.Stat(fpath)
	if err != nil {
		return nil, fmt.Errorf("file %s does not exist: %w", name, err)
	}

	// Read file to get uncompressed size and line count
	uncompressedSize, lineCount, err := b.getFileStats(name)
	if err != nil {
		return nil, err
	}

	latestName, _ := b.latestName()

	return &FileStatus{
		Name:             name,
		CompressedSize:   fi.Size(),
		UncompressedSize: uncompressedSize,
		LineCount:        lineCount,
		IsLatest:         name == latestName,
	}, nil
}

// ListFiles returns all event file names (in sequence order).
func (b *Bus) ListFiles() ([]string, error) {
	return b.listFiles()
}

// --- Internal methods ---

// getTracking returns the tracking info for a file, creating it if needed.
func (b *Bus) getTracking(file string) *fileTracking {
	if b.tracking[file] == nil {
		b.tracking[file] = &fileTracking{}
	}
	return b.tracking[file]
}

// lock acquires an exclusive lock. Returns an unlock function.
func (b *Bus) lock() (func(), error) {
	lockPath := filepath.Join(b.Dir, "events.lock")
	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Try to exclusively create lock file, with retry
	var f *os.File
	var err error
	for attempts := 0; attempts < 50; attempts++ {
		f, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			break
		}
		if os.IsExist(err) {
			// Check if lock file is stale (older than 30 seconds is considered stale)
			if fi, serr := os.Stat(lockPath); serr == nil {
				if time.Since(fi.ModTime()) > 30*time.Second {
					os.Remove(lockPath)
					continue
				}
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return nil, fmt.Errorf("failed to create lock file: %w", err)
	}
	if f == nil {
		return nil, fmt.Errorf("failed to acquire lock: %s", lockPath)
	}
	f.Close()

	// Clear tracking on lock acquisition
	b.tracking = make(map[string]*fileTracking)

	return func() {
		os.Remove(lockPath)
		b.tracking = make(map[string]*fileTracking)
	}, nil
}

// latestName reads the latest file and returns the currently active events file name.
func (b *Bus) latestName() (string, error) {
	data, err := os.ReadFile(filepath.Join(b.Dir, "latest"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// setLatest updates the latest file.
func (b *Bus) setLatest(name string) error {
	return os.WriteFile(filepath.Join(b.Dir, "latest"), []byte(name+"\n"), 0o644)
}

// createNewFile creates a new events file with a rotate event and updates latest.
// Returns the created filename.
func (b *Bus) createNewFile(seq int) (string, error) {
	// Create rotate event
	uuid := generateUUID()
	rotateEvt := &Event{
		ID:        generateID(),
		Timestamp: time.Now().UTC(),
		Type:      RotateEventType,
		Channel:   "",
	}
	rotatePayload, _ := json.Marshal(RotateEvent{UUID: uuid})
	rotateEvt.Payload = rotatePayload

	rotateLine, err := json.Marshal(rotateEvt)
	if err != nil {
		return "", fmt.Errorf("failed to serialize rotate event: %w", err)
	}
	rotateLine = append(rotateLine, '\n')

	// Calculate hash of rotate line for filename
	hash := hashLine(rotateLine)
	name := fmt.Sprintf("events.%03d-%s.jsonl.gz", seq, hash)
	fpath := filepath.Join(b.Dir, name)

	// Create gzip file with rotate event
	f, err := os.Create(fpath)
	if err != nil {
		return "", fmt.Errorf("failed to create event file: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	if _, err := gw.Write(rotateLine); err != nil {
		return "", fmt.Errorf("failed to write rotate event: %w", err)
	}
	if err := gw.Close(); err != nil {
		return "", fmt.Errorf("failed to close gzip writer: %w", err)
	}

	// Initialize tracking for this file
	b.tracking[name] = &fileTracking{
		uncompressedSize: int64(len(rotateLine)),
		lineCount:        1,
	}

	if err := b.setLatest(name); err != nil {
		return "", err
	}

	return name, nil
}

// listFiles lists all events.NNN-*.jsonl.gz files, sorted by sequence number.
func (b *Bus) listFiles() ([]string, error) {
	entries, err := os.ReadDir(b.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "events.") && strings.HasSuffix(name, ".jsonl.gz") {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}

// parseSeq extracts the sequence number from a file name.
func parseSeq(name string) int {
	// events.001-a1b2c3d4.jsonl.gz → 1
	name = strings.TrimPrefix(name, "events.")
	idx := strings.Index(name, "-")
	if idx > 0 {
		name = name[:idx]
	}
	name = strings.TrimSuffix(name, ".jsonl.gz")
	n, _ := strconv.Atoi(name)
	return n
}

// getFileStats calculates uncompressed size and line count by reading the file.
func (b *Bus) getFileStats(name string) (int64, int64, error) {
	fpath := filepath.Join(b.Dir, name)
	f, err := os.Open(fpath)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	// Check if file is empty
	fi, err := f.Stat()
	if err != nil {
		return 0, 0, err
	}
	if fi.Size() == 0 {
		return 0, 0, nil
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open gzip: %w", err)
	}
	defer gr.Close()

	gr.Multistream(true)

	data, err := io.ReadAll(gr)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decompress: %w", err)
	}

	lineCount := int64(0)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) > 0 {
			lineCount++
		}
	}

	return int64(len(data)), lineCount, scanner.Err()
}

// readFile reads events from a gzip file, starting from the specified uncompressed byte offset.
func (b *Bus) readFile(name string, fromOffset int64) ([]EventEntry, error) {
	fpath := filepath.Join(b.Dir, name)
	f, err := os.Open(fpath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Check if file is empty
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() == 0 {
		return nil, nil
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to open gzip: %w", err)
	}
	defer gr.Close()

	// gzip.Reader's Multistream(true) is the default behavior, reads all concatenated gzip members
	gr.Multistream(true)

	// Read all uncompressed content
	data, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress: %w", err)
	}

	if int64(len(data)) <= fromOffset {
		return nil, nil
	}

	// Read line by line starting from offset
	remaining := data[fromOffset:]
	scanner := bufio.NewScanner(bytes.NewReader(remaining))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // Max 10MB single line

	var entries []EventEntry
	currentOffset := fromOffset

	for scanner.Scan() {
		line := scanner.Bytes()
		lineLen := int64(len(line)) + 1 // +1 for \n
		endOffset := currentOffset + lineLen

		if len(bytes.TrimSpace(line)) == 0 {
			currentOffset = endOffset
			continue
		}

		var evt Event
		if err := json.Unmarshal(line, &evt); err != nil {
			currentOffset = endOffset
			continue // Skip unparseable lines
		}

		// Skip rotate events when listing
		if evt.Type == RotateEventType {
			currentOffset = endOffset
			continue
		}

		entries = append(entries, EventEntry{
			Event:  evt,
			File:   name,
			Offset: endOffset,
		})
		currentOffset = endOffset
	}

	return entries, scanner.Err()
}
