// Package event 实现基于文件的 EventBus。
//
// 事件以 JSONL 格式存储在 gzip 压缩文件中，支持 rotation、多 channel marker 消费。
// 存储目录默认为 ~/.emx-mail/events/。
//
// 目录结构:
//
//	~/.emx-mail/events/
//	├── events.001.jsonl.gz       # 已归档
//	├── events.001.meta.json      # 元数据 (未压缩大小、首行hash)
//	├── events.002.jsonl.gz       # 当前活跃文件
//	├── events.002.meta.json
//	├── latest                    # 文本文件，内容为当前活跃文件名
//	├── events.lock               # 独占锁文件 (临时)
//	└── markers/
//	    ├── my-channel.json       # channel marker
//	    └── other-channel.json
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

// MaxUncompressedSize 是单个 events 文件未压缩数据的最大大小。
// 当 (当前未压缩大小 + 新事件大小 + 64KB) >= 64MB 时触发 rotation。
const MaxUncompressedSize = 64 * 1024 * 1024 // 64 MB

// RotationHeadroom 是 rotation 判断时的预留空间。
const RotationHeadroom = 64 * 1024 // 64 KB

// Event 是 EventBus 中的一个事件。
type Event struct {
	ID        string          `json:"id"`
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Channel   string          `json:"channel"`
	Payload   json.RawMessage `json:"payload"`
}

// EventEntry 是从文件中读取的事件，附带位置信息。
type EventEntry struct {
	Event
	File   string `json:"file"`   // 事件所在文件名
	Offset int64  `json:"offset"` // 该事件之后的字节偏移量 (未压缩流中的行尾位置)
}

// FileMeta 是 events 文件的元数据，存储在 .meta.json 中。
type FileMeta struct {
	UncompressedSize int64  `json:"uncompressed_size"`
	LineCount        int64  `json:"line_count"`
	FirstLineHash    string `json:"first_line_hash"` // "sha256:<hex>"
}

// Position 表示一个消费位置，用于 mark 命令。
type Position struct {
	File   string `json:"file"`
	Offset int64  `json:"offset"`
}

// String 返回 "file:offset" 格式的位置字符串。
func (p Position) String() string {
	return fmt.Sprintf("%s:%d", p.File, p.Offset)
}

// ParsePosition 从 "file:offset" 格式字符串解析 Position。
func ParsePosition(s string) (Position, error) {
	idx := strings.LastIndex(s, ":")
	if idx <= 0 || idx >= len(s)-1 {
		return Position{}, fmt.Errorf("无效的位置格式 %q，应为 file:offset", s)
	}
	offset, err := strconv.ParseInt(s[idx+1:], 10, 64)
	if err != nil {
		return Position{}, fmt.Errorf("无效的位置格式 %q，offset 不是数字: %w", s, err)
	}
	return Position{File: s[:idx], Offset: offset}, nil
}

// FileStatus 是单个 events 文件的状态信息。
type FileStatus struct {
	Name             string `json:"name"`
	CompressedSize   int64  `json:"compressed_size"`
	UncompressedSize int64  `json:"uncompressed_size"`
	LineCount        int64  `json:"line_count"`
	FirstLineHash    string `json:"first_line_hash"`
	IsLatest         bool   `json:"is_latest"`
}

// generateID 生成事件 ID：时间戳 + 随机后缀。
func generateID() string {
	ts := time.Now().UTC().Format("20060102T150405")
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return ts + "-" + hex.EncodeToString(b)
}

// hashLine 计算一行数据的 SHA-256 哈希。
func hashLine(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}
