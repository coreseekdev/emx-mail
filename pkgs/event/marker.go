package event

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Marker is the consumption position record for a channel.
type Marker struct {
	File      string    `json:"file"`      // Event file name (e.g., events.001-a1b2c3d4.jsonl.gz)
	Offset    int64     `json:"offset"`    // Byte offset in uncompressed data (line end position)
	UpdatedAt time.Time `json:"updated_at"`
}

// markerPath returns the marker file path for a channel.
func (b *Bus) markerPath(channel string) string {
	safe := sanitizeChannel(channel)
	return filepath.Join(b.Dir, "markers", safe+".json")
}

// LoadMarker loads the marker for the specified channel.
// If the marker does not exist, returns nil and os.IsNotExist error.
func (b *Bus) LoadMarker(channel string) (*Marker, error) {
	data, err := os.ReadFile(b.markerPath(channel))
	if err != nil {
		return nil, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse marker: %w", err)
	}
	return &m, nil
}

// SaveMarker saves the marker for a channel.
func (b *Bus) SaveMarker(channel string, m *Marker) error {
	if err := os.MkdirAll(filepath.Join(b.Dir, "markers"), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize marker: %w", err)
	}
	return os.WriteFile(b.markerPath(channel), data, 0o644)
}

// ListChannels lists all registered channels (those with marker files).
func (b *Bus) ListChannels() ([]string, error) {
	markersDir := filepath.Join(b.Dir, "markers")
	entries, err := os.ReadDir(markersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var channels []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".json") {
			channels = append(channels, strings.TrimSuffix(name, ".json"))
		}
	}
	return channels, nil
}

// sanitizeChannel converts a channel name to a safe filename.
func sanitizeChannel(channel string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)
	return replacer.Replace(channel)
}
