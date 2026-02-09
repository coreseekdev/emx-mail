package event

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Marker 是某个 channel 的消费位置记录。
type Marker struct {
	File          string    `json:"file"`            // 事件文件名 (如 events.003.jsonl.gz)
	FirstLineHash string    `json:"first_line_hash"` // 该文件首行的 hash，用于校验文件身份
	Offset        int64     `json:"offset"`          // 未压缩数据中的字节偏移量 (行尾位置)
	UpdatedAt     time.Time `json:"updated_at"`
}

// markerPath 返回 channel 的 marker 文件路径。
func (b *Bus) markerPath(channel string) string {
	safe := sanitizeChannel(channel)
	return filepath.Join(b.Dir, "markers", safe+".json")
}

// LoadMarker 加载指定 channel 的 marker。
// 如果 marker 不存在，返回 nil 和 os.IsNotExist 错误。
func (b *Bus) LoadMarker(channel string) (*Marker, error) {
	data, err := os.ReadFile(b.markerPath(channel))
	if err != nil {
		return nil, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("解析 marker 失败: %w", err)
	}
	return &m, nil
}

// SaveMarker 保存 channel 的 marker。
func (b *Bus) SaveMarker(channel string, m *Marker) error {
	if err := os.MkdirAll(filepath.Join(b.Dir, "markers"), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 marker 失败: %w", err)
	}
	return os.WriteFile(b.markerPath(channel), data, 0o644)
}

// ListChannels 列出所有已注册的 channel (有 marker 文件的)。
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

// ValidateMarker 检查 marker 是否仍然有效 (文件存在且首行 hash 匹配)。
func (b *Bus) ValidateMarker(m *Marker) error {
	fpath := filepath.Join(b.Dir, m.File)
	if _, err := os.Stat(fpath); err != nil {
		return fmt.Errorf("事件文件 %s 不存在", m.File)
	}

	meta, err := b.loadMeta(m.File)
	if err != nil {
		return fmt.Errorf("读取 meta 失败: %w", err)
	}

	if meta.FirstLineHash != "" && m.FirstLineHash != "" && meta.FirstLineHash != m.FirstLineHash {
		return fmt.Errorf("首行 hash 不匹配: marker=%s, 文件=%s", m.FirstLineHash, meta.FirstLineHash)
	}

	return nil
}

// sanitizeChannel 将 channel 名称转换为安全的文件名。
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
