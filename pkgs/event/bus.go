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

// Bus 是基于文件的 EventBus。
type Bus struct {
	Dir string // 事件存储目录
}

// NewBus 创建一个 EventBus，使用指定目录。
func NewBus(dir string) *Bus {
	return &Bus{Dir: dir}
}

// DefaultBus 创建使用默认路径 (~/.emx-mail/events/) 的 EventBus。
func DefaultBus() (*Bus, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("获取用户目录失败: %w", err)
	}
	dir := filepath.Join(home, ".emx-mail", "events")
	return NewBus(dir), nil
}

// Init 初始化事件目录，创建必要的子目录和首个 events 文件。
func (b *Bus) Init() error {
	if err := os.MkdirAll(filepath.Join(b.Dir, "markers"), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	// 如果还没有 latest，创建第一个文件
	_, err := b.latestName()
	if err != nil {
		return b.createNewFile(1)
	}
	return nil
}

// Add 向 EventBus 添加一个事件。独占锁保护。
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
		return nil, fmt.Errorf("序列化事件失败: %w", err)
	}
	line = append(line, '\n')

	// 检查是否需要 rotation
	latestFile, err := b.latestName()
	if err != nil {
		return nil, err
	}

	meta, err := b.loadMeta(latestFile)
	if err != nil {
		return nil, err
	}

	if meta.UncompressedSize+int64(len(line))+RotationHeadroom >= MaxUncompressedSize {
		// 需要 rotate
		seq := parseSeq(latestFile)
		if err := b.createNewFile(seq + 1); err != nil {
			return nil, fmt.Errorf("rotation 失败: %w", err)
		}
		latestFile, err = b.latestName()
		if err != nil {
			return nil, err
		}
		meta, err = b.loadMeta(latestFile)
		if err != nil {
			return nil, err
		}
	}

	// 追加事件到 gzip 文件 (拼接新的 gzip member)
	fpath := filepath.Join(b.Dir, latestFile)
	f, err := os.OpenFile(fpath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开事件文件失败: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	if _, err := gw.Write(line); err != nil {
		return nil, fmt.Errorf("写入事件失败: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("关闭 gzip writer 失败: %w", err)
	}

	// 更新 meta
	if meta.LineCount == 0 {
		meta.FirstLineHash = hashLine(line)
	}
	meta.UncompressedSize += int64(len(line))
	meta.LineCount++
	if err := b.saveMeta(latestFile, meta); err != nil {
		return nil, err
	}

	return evt, nil
}

// List 列出指定 channel 从 marker 位置开始的新事件。
// 如果 channel 没有 marker，从最早的文件开始。
// limit <= 0 表示不限制。
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

	// 找到起始文件索引
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
			return nil, fmt.Errorf("读取 %s 失败: %w", f, err)
		}
		entries = append(entries, events...)
		if limit > 0 && len(entries) >= limit {
			entries = entries[:limit]
			break
		}
	}

	return entries, nil
}

// Mark 更新 channel 的消费位置。
func (b *Bus) Mark(channel string, pos Position) error {
	unlock, err := b.lock()
	if err != nil {
		return err
	}
	defer unlock()

	// 验证文件存在
	fpath := filepath.Join(b.Dir, pos.File)
	if _, err := os.Stat(fpath); err != nil {
		return fmt.Errorf("事件文件 %s 不存在: %w", pos.File, err)
	}

	// 获取首行 hash
	meta, err := b.loadMeta(pos.File)
	if err != nil {
		return fmt.Errorf("读取元数据失败: %w", err)
	}

	m := &Marker{
		File:          pos.File,
		FirstLineHash: meta.FirstLineHash,
		Offset:        pos.Offset,
		UpdatedAt:     time.Now().UTC(),
	}

	return b.SaveMarker(channel, m)
}

// Status 返回指定文件的状态，name 为空表示 latest。
func (b *Bus) Status(name string) (*FileStatus, error) {
	if name == "" {
		var err error
		name, err = b.latestName()
		if err != nil {
			return nil, fmt.Errorf("无活跃事件文件: %w", err)
		}
	}

	fpath := filepath.Join(b.Dir, name)
	fi, err := os.Stat(fpath)
	if err != nil {
		return nil, fmt.Errorf("文件 %s 不存在: %w", name, err)
	}

	meta, err := b.loadMeta(name)
	if err != nil {
		return nil, err
	}

	latestName, _ := b.latestName()

	return &FileStatus{
		Name:             name,
		CompressedSize:   fi.Size(),
		UncompressedSize: meta.UncompressedSize,
		LineCount:        meta.LineCount,
		FirstLineHash:    meta.FirstLineHash,
		IsLatest:         name == latestName,
	}, nil
}

// ListFiles 返回所有事件文件名（按序号排列）。
func (b *Bus) ListFiles() ([]string, error) {
	return b.listFiles()
}

// --- 内部方法 ---

// lock 获取独占锁。返回 unlock 函数。
func (b *Bus) lock() (func(), error) {
	lockPath := filepath.Join(b.Dir, "events.lock")
	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}

	// 尝试独占创建锁文件，带重试
	var f *os.File
	var err error
	for attempts := 0; attempts < 50; attempts++ {
		f, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			break
		}
		if os.IsExist(err) {
			// 检查锁文件是否过期 (超过 30 秒视为过期)
			if fi, serr := os.Stat(lockPath); serr == nil {
				if time.Since(fi.ModTime()) > 30*time.Second {
					os.Remove(lockPath)
					continue
				}
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return nil, fmt.Errorf("创建锁文件失败: %w", err)
	}
	if f == nil {
		return nil, fmt.Errorf("获取锁超时: %s", lockPath)
	}
	f.Close()

	return func() {
		os.Remove(lockPath)
	}, nil
}

// latestName 读取 latest 文件，返回当前活跃的 events 文件名。
func (b *Bus) latestName() (string, error) {
	data, err := os.ReadFile(filepath.Join(b.Dir, "latest"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// setLatest 更新 latest 文件。
func (b *Bus) setLatest(name string) error {
	return os.WriteFile(filepath.Join(b.Dir, "latest"), []byte(name+"\n"), 0o644)
}

// createNewFile 创建新的 events 文件并更新 latest。
func (b *Bus) createNewFile(seq int) error {
	name := fmt.Sprintf("events.%03d.jsonl.gz", seq)
	fpath := filepath.Join(b.Dir, name)

	// 创建空的 gzip 文件
	f, err := os.Create(fpath)
	if err != nil {
		return fmt.Errorf("创建事件文件失败: %w", err)
	}
	f.Close()

	// 创建空 meta
	meta := &FileMeta{}
	if err := b.saveMeta(name, meta); err != nil {
		return err
	}

	return b.setLatest(name)
}

// listFiles 列出所有 events.NNN.jsonl.gz 文件，按序号排序。
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

// parseSeq 从文件名提取序号。
func parseSeq(name string) int {
	// events.001.jsonl.gz → 1
	name = strings.TrimPrefix(name, "events.")
	name = strings.TrimSuffix(name, ".jsonl.gz")
	n, _ := strconv.Atoi(name)
	return n
}

// metaPath 返回 meta 文件路径。
func (b *Bus) metaPath(eventsFile string) string {
	base := strings.TrimSuffix(eventsFile, ".jsonl.gz")
	return filepath.Join(b.Dir, base+".meta.json")
}

// loadMeta 加载 meta 文件。
func (b *Bus) loadMeta(eventsFile string) (*FileMeta, error) {
	data, err := os.ReadFile(b.metaPath(eventsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return &FileMeta{}, nil
		}
		return nil, fmt.Errorf("读取 meta 失败: %w", err)
	}
	var meta FileMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("解析 meta 失败: %w", err)
	}
	return &meta, nil
}

// saveMeta 保存 meta 文件。
func (b *Bus) saveMeta(eventsFile string, meta *FileMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 meta 失败: %w", err)
	}
	return os.WriteFile(b.metaPath(eventsFile), data, 0o644)
}

// readFile 从 gzip 文件中读取事件，从指定的未压缩字节偏移量开始。
func (b *Bus) readFile(name string, fromOffset int64) ([]EventEntry, error) {
	fpath := filepath.Join(b.Dir, name)
	f, err := os.Open(fpath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// 检查文件是否为空
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() == 0 {
		return nil, nil
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("打开 gzip 失败: %w", err)
	}
	defer gr.Close()

	// gzip.Reader 的 Multistream(true) 是默认行为，会读取所有拼接的 gzip 成员
	gr.Multistream(true)

	// 读取全部未压缩内容
	data, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("解压失败: %w", err)
	}

	if int64(len(data)) <= fromOffset {
		return nil, nil
	}

	// 从 offset 处开始按行读取
	remaining := data[fromOffset:]
	scanner := bufio.NewScanner(bytes.NewReader(remaining))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 最大 10MB 单行

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
			continue // 跳过无法解析的行
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
