// emx-event: 基于文件的事件总线 CLI
//
// 用法:
//
//	emx-event <命令> [选项]
//
// 命令:
//
//	add     发布一个事件
//	ls      列出新事件 (基于 channel marker)
//	mark    更新 channel 的消费位置
//	status  查看事件文件状态
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/emx-mail/cli/pkgs/event"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// 解析全局选项
	var dir string
	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-dir":
			if len(args) < 2 {
				fatalf("缺少 -dir 参数值\n")
			}
			dir = args[1]
			args = args[2:]
		case "-h", "--help":
			printUsage()
			os.Exit(0)
		default:
			fatalf("未知选项: %s\n", args[0])
		}
	}

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	bus, err := makeBus(dir)
	if err != nil {
		fatalf("初始化失败: %v\n", err)
	}

	cmd := args[0]
	args = args[1:]

	switch cmd {
	case "add":
		err = cmdAdd(bus, args)
	case "ls", "list":
		err = cmdList(bus, args)
	case "mark":
		err = cmdMark(bus, args)
	case "status":
		err = cmdStatus(bus, args)
	default:
		fatalf("未知命令: %s\n", cmd)
	}

	if err != nil {
		fatalf("%v\n", err)
	}
}

func makeBus(dir string) (*event.Bus, error) {
	if dir != "" {
		return event.NewBus(dir), nil
	}
	return event.DefaultBus()
}

// --- add 命令 ---

func cmdAdd(bus *event.Bus, args []string) error {
	var typ, channel, payload string

	for len(args) > 0 {
		switch args[0] {
		case "-type", "-t":
			if len(args) < 2 {
				return fmt.Errorf("缺少 -type 参数值")
			}
			typ = args[1]
			args = args[2:]
		case "-channel", "-c":
			if len(args) < 2 {
				return fmt.Errorf("缺少 -channel 参数值")
			}
			channel = args[1]
			args = args[2:]
		case "-payload", "-p":
			if len(args) < 2 {
				return fmt.Errorf("缺少 -payload 参数值")
			}
			payload = args[1]
			args = args[2:]
		case "-h", "--help":
			fmt.Println("用法: emx-event add -type <类型> -channel <通道> [-payload <JSON>]")
			fmt.Println("")
			fmt.Println("选项:")
			fmt.Println("  -type, -t       事件类型 (必须)")
			fmt.Println("  -channel, -c    事件通道 (必须)")
			fmt.Println("  -payload, -p    JSON 格式的载荷 (可选，默认 null)")
			return nil
		default:
			return fmt.Errorf("未知选项: %s", args[0])
		}
	}

	if typ == "" {
		return fmt.Errorf("必须指定 -type")
	}
	if channel == "" {
		return fmt.Errorf("必须指定 -channel")
	}

	var p json.RawMessage
	if payload != "" {
		if !json.Valid([]byte(payload)) {
			return fmt.Errorf("无效的 JSON payload: %s", payload)
		}
		p = json.RawMessage(payload)
	} else {
		p = json.RawMessage("null")
	}

	evt, err := bus.Add(typ, channel, p)
	if err != nil {
		return err
	}

	fmt.Printf("已发布事件:\n")
	fmt.Printf("  ID:        %s\n", evt.ID)
	fmt.Printf("  时间:      %s\n", evt.Timestamp.Format(time.RFC3339))
	fmt.Printf("  类型:      %s\n", evt.Type)
	fmt.Printf("  通道:      %s\n", evt.Channel)
	fmt.Printf("  载荷:      %s\n", string(evt.Payload))

	return nil
}

// --- ls 命令 ---

func cmdList(bus *event.Bus, args []string) error {
	var channel string
	limit := 0

	for len(args) > 0 {
		switch args[0] {
		case "-channel", "-c":
			if len(args) < 2 {
				return fmt.Errorf("缺少 -channel 参数值")
			}
			channel = args[1]
			args = args[2:]
		case "-limit", "-n":
			if len(args) < 2 {
				return fmt.Errorf("缺少 -limit 参数值")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("无效的 limit: %s", args[1])
			}
			limit = n
			args = args[2:]
		case "-h", "--help":
			fmt.Println("用法: emx-event ls -channel <通道> [-limit N]")
			fmt.Println("")
			fmt.Println("列出指定 channel 从上次 mark 位置开始的新事件。")
			fmt.Println("如果该 channel 没有 marker，从最早的文件开始。")
			fmt.Println("")
			fmt.Println("选项:")
			fmt.Println("  -channel, -c    通道名称 (必须)")
			fmt.Println("  -limit, -n      最大返回数量")
			return nil
		default:
			return fmt.Errorf("未知选项: %s", args[0])
		}
	}

	if channel == "" {
		return fmt.Errorf("必须指定 -channel")
	}

	entries, err := bus.List(channel, limit)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("没有新事件")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "序号\t时间\t类型\t通道\t载荷\t位置\n")
	fmt.Fprintf(tw, "----\t----\t----\t----\t----\t----\n")

	for i, e := range entries {
		payloadStr := string(e.Payload)
		if len(payloadStr) > 60 {
			payloadStr = payloadStr[:57] + "..."
		}
		pos := event.Position{File: e.File, Offset: e.Offset}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i+1,
			e.Timestamp.Format("15:04:05"),
			e.Type,
			e.Channel,
			payloadStr,
			pos.String(),
		)
	}
	tw.Flush()

	// 打印最后的位置，方便 mark
	last := entries[len(entries)-1]
	fmt.Printf("\n最新位置: %s\n", event.Position{File: last.File, Offset: last.Offset}.String())
	fmt.Printf("使用 emx-event mark -channel %s %s 更新消费位置\n", channel,
		event.Position{File: last.File, Offset: last.Offset}.String())

	return nil
}

// --- mark 命令 ---

func cmdMark(bus *event.Bus, args []string) error {
	var channel, posStr string

	for len(args) > 0 {
		switch args[0] {
		case "-channel", "-c":
			if len(args) < 2 {
				return fmt.Errorf("缺少 -channel 参数值")
			}
			channel = args[1]
			args = args[2:]
		case "-h", "--help":
			fmt.Println("用法: emx-event mark -channel <通道> <位置>")
			fmt.Println("")
			fmt.Println("更新指定 channel 的消费位置。位置格式: file:offset")
			fmt.Println("可从 ls 命令的输出中获取位置。")
			fmt.Println("")
			fmt.Println("选项:")
			fmt.Println("  -channel, -c    通道名称 (必须)")
			return nil
		default:
			if strings.HasPrefix(args[0], "-") {
				return fmt.Errorf("未知选项: %s", args[0])
			}
			posStr = args[0]
			args = args[1:]
		}
	}

	if channel == "" {
		return fmt.Errorf("必须指定 -channel")
	}
	if posStr == "" {
		return fmt.Errorf("必须指定位置 (格式: file:offset)")
	}

	pos, err := event.ParsePosition(posStr)
	if err != nil {
		return err
	}

	if err := bus.Mark(channel, pos); err != nil {
		return err
	}

	fmt.Printf("已更新 marker: %s → %s\n", channel, pos.String())
	return nil
}

// --- status 命令 ---

func cmdStatus(bus *event.Bus, args []string) error {
	var name string

	for len(args) > 0 {
		switch args[0] {
		case "-h", "--help":
			fmt.Println("用法: emx-event status [文件名]")
			fmt.Println("")
			fmt.Println("显示事件文件状态。默认显示 latest 文件。")
			fmt.Println("指定文件名查看特定文件的状态。")
			return nil
		default:
			if strings.HasPrefix(args[0], "-") {
				return fmt.Errorf("未知选项: %s", args[0])
			}
			name = args[0]
			args = args[1:]
		}
	}

	st, err := bus.Status(name)
	if err != nil {
		return err
	}

	fmt.Printf("文件:       %s", st.Name)
	if st.IsLatest {
		fmt.Printf(" (latest)")
	}
	fmt.Println()
	fmt.Printf("压缩大小:   %s\n", formatSize(st.CompressedSize))
	fmt.Printf("未压缩大小: %s\n", formatSize(st.UncompressedSize))
	fmt.Printf("行数:       %d\n", st.LineCount)
	if st.FirstLineHash != "" {
		fmt.Printf("首行哈希:   %s\n", st.FirstLineHash)
	}

	// 显示所有 channel marker 状态
	channels, err := bus.ListChannels()
	if err == nil && len(channels) > 0 {
		fmt.Println()
		fmt.Println("Channel Markers:")
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "  通道\t文件\t偏移量\t更新时间\n")
		fmt.Fprintf(tw, "  ----\t----\t------\t--------\n")
		for _, ch := range channels {
			m, err := bus.LoadMarker(ch)
			if err != nil {
				continue
			}
			fmt.Fprintf(tw, "  %s\t%s\t%d\t%s\n", ch, m.File, m.Offset, m.UpdatedAt.Format("01-02 15:04:05"))
		}
		tw.Flush()
	}

	// 显示所有文件列表
	files, err := bus.ListFiles()
	if err == nil && len(files) > 1 {
		fmt.Println()
		fmt.Printf("全部文件 (%d):\n", len(files))
		for _, f := range files {
			marker := ""
			if f == st.Name && st.IsLatest {
				marker = " ← latest"
			}
			fmt.Printf("  %s%s\n", f, marker)
		}
	}

	return nil
}

// --- 辅助函数 ---

func printUsage() {
	fmt.Println("emx-event: 基于文件的事件总线")
	fmt.Println()
	fmt.Println("用法: emx-event [-dir <目录>] <命令> [选项]")
	fmt.Println()
	fmt.Println("命令:")
	fmt.Println("  add      发布一个事件")
	fmt.Println("  ls       列出新事件 (基于 channel marker)")
	fmt.Println("  mark     更新 channel 的消费位置")
	fmt.Println("  status   查看事件文件状态")
	fmt.Println()
	fmt.Println("全局选项:")
	fmt.Println("  -dir     事件存储目录 (默认 ~/.emx-mail/events/)")
	fmt.Println("  -h       显示帮助")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Println("  emx-event add -type email.received -channel inbox -payload '{\"from\":\"alice@test.com\"}'")
	fmt.Println("  emx-event ls -channel inbox")
	fmt.Println("  emx-event mark -channel inbox events.001.jsonl.gz:2048")
	fmt.Println("  emx-event status")
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
	)
	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
