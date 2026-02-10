// Package event implements a file-based EventBus.
//
// Events are stored in JSONL format in gzip-compressed files, supporting rotation and multi-channel marker-based consumption.
// Default storage directory is ~/.emx-mail/events/.
//
// Directory structure:
//
//	~/.emx-mail/events/
//	├── events.001-a1b2c3d4.jsonl.gz       # Currently active file
//	├── events.002-e5f6g7h8.jsonl.gz       # Archived
//	├── latest                             # Text file containing the active file name
//	├── events.lock                        # Exclusive lock file (temporary)
//	└── markers/
//	    ├── my-channel.json               # channel marker
//	    └── other-channel.json
//
// Each events file starts with a "rotate" event containing a UUID, and the filename includes
// the hash of this rotate event line for identity verification.
package event

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MaxUncompressedSize is the maximum uncompressed size for a single events file.
// Rotation is triggered when (current uncompressed size + new event size + 64KB) >= 64MB.
const MaxUncompressedSize = 64 * 1024 * 1024 // 64 MB

// RotationHeadroom is the reserved space for rotation judgment.
const RotationHeadroom = 64 * 1024 // 64 KB

// RotateEventType is the event type for rotation marker events.
const RotateEventType = "__rotate__"

// RotateEvent is the first event in each events file, containing a UUID for file identity.
type RotateEvent struct {
	UUID string `json:"uuid"`
}

// Event is an event in the EventBus.
type Event struct {
	ID        string          `json:"id"`
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Channel   string          `json:"channel"`
	Payload   json.RawMessage `json:"payload"`
}

// EventEntry is an event read from a file with positional information.
type EventEntry struct {
	Event
	File   string `json:"file"`   // Event file name
	Offset int64  `json:"offset"` // Byte offset after this event (in uncompressed stream at line end)
}

// FileStatus is status information for a single events file.
type FileStatus struct {
	Name             string `json:"name"`
	CompressedSize   int64  `json:"compressed_size"`
	UncompressedSize int64  `json:"uncompressed_size"`
	LineCount        int64  `json:"line_count"`
	FirstLineHash    string `json:"first_line_hash,omitempty"`
	IsLatest         bool   `json:"is_latest"`
}

// Position represents a consumption position for mark commands.
type Position struct {
	File   string `json:"file"`
	Offset int64  `json:"offset"`
}

// String returns a position string in "file:offset" format.
func (p Position) String() string {
	return fmt.Sprintf("%s:%d", p.File, p.Offset)
}

// ParsePosition parses a Position from "file:offset" format string.
func ParsePosition(s string) (Position, error) {
	idx := strings.LastIndex(s, ":")
	if idx <= 0 || idx >= len(s)-1 {
		return Position{}, fmt.Errorf("invalid position format %q, expected file:offset", s)
	}
	offset, err := strconv.ParseInt(s[idx+1:], 10, 64)
	if err != nil {
		return Position{}, fmt.Errorf("invalid position format %q, offset is not a number: %w", s, err)
	}
	return Position{File: s[:idx], Offset: offset}, nil
}

// generateID generates an event ID: timestamp + random suffix.
func generateID() string {
	ts := time.Now().UTC().Format("20060102T150405")
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return ts + "-" + hex.EncodeToString(b)
}

// generateUUID generates a random UUID for file rotation.
func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// hashLine calculates the SHA-256 hash of a line, returning first 8 chars.
func hashLine(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])[:8]
}
