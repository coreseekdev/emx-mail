// emx-event: file-based event bus CLI
//
// Usage:
//
//	emx-event <command> [options]
//
// Commands:
//
//	add     publish an event
//	ls      list new events (based on channel marker)
//	mark    update channel consumption position
//	status  show event file status
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
				fatal("missing -dir argument value")
			}
			dir = args[1]
			args = args[2:]
		case "-h", "--help":
			printUsage()
			os.Exit(0)
		default:
			fatal("unknown option: %s", args[0])
		}
	}

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	bus, err := makeBus(dir)
	if err != nil {
		fatal("initialization failed: %v", err)
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
		fatal("unknown command: %s", cmd)
	}

	if err != nil {
		fatal("%v", err)
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
				return fmt.Errorf("missing -type argument value")
			}
			typ = args[1]
			args = args[2:]
		case "-channel", "-c":
			if len(args) < 2 {
				return fmt.Errorf("missing -channel argument value")
			}
			channel = args[1]
			args = args[2:]
		case "-payload", "-p":
			if len(args) < 2 {
				return fmt.Errorf("missing -payload argument value")
			}
			payload = args[1]
			args = args[2:]
		case "-h", "--help":
			fmt.Println("Usage: emx-event add -type <type> -channel <channel> [-payload <JSON>]")
			fmt.Println("")
			fmt.Println("Options:")
			fmt.Println("  -type, -t       event type (required)")
			fmt.Println("  -channel, -c    event channel (required)")
			fmt.Println("  -payload, -p    JSON payload (optional, default null)")
			return nil
		default:
			return fmt.Errorf("unknown option: %s", args[0])
		}
	}

	if typ == "" {
		return fmt.Errorf("-type is required")
	}
	if channel == "" {
		return fmt.Errorf("-channel is required")
	}

	var p json.RawMessage
	if payload != "" {
		if !json.Valid([]byte(payload)) {
			return fmt.Errorf("invalid JSON payload: %s", payload)
		}
		p = json.RawMessage(payload)
	} else {
		p = json.RawMessage("null")
	}

	evt, err := bus.Add(typ, channel, p)
	if err != nil {
		return err
	}

	fmt.Printf("Event published:\n")
	fmt.Printf("  ID:        %s\n", evt.ID)
	fmt.Printf("  Time:      %s\n", evt.Timestamp.Format(time.RFC3339))
	fmt.Printf("  Type:      %s\n", evt.Type)
	fmt.Printf("  Channel:   %s\n", evt.Channel)
	fmt.Printf("  Payload:   %s\n", string(evt.Payload))

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
				return fmt.Errorf("missing -channel argument value")
			}
			channel = args[1]
			args = args[2:]
		case "-limit", "-n":
			if len(args) < 2 {
				return fmt.Errorf("missing -limit argument value")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid limit: %s", args[1])
			}
			limit = n
			args = args[2:]
		case "-h", "--help":
			fmt.Println("Usage: emx-event ls -channel <channel> [-limit N]")
			fmt.Println("")
			fmt.Println("List new events for a channel starting from the last mark position.")
			fmt.Println("If the channel has no marker, starts from the earliest file.")
			fmt.Println("")
			fmt.Println("Options:")
			fmt.Println("  -channel, -c    channel name (required)")
			fmt.Println("  -limit, -n      maximum number of results")
			return nil
		default:
			return fmt.Errorf("unknown option: %s", args[0])
		}
	}

	if channel == "" {
		return fmt.Errorf("-channel is required")
	}

	entries, err := bus.List(channel, limit)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("no new events")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "#\tTime\tType\tChannel\tPayload\tPosition\n")
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
	fmt.Printf("\nLatest position: %s\n", event.Position{File: last.File, Offset: last.Offset}.String())
	fmt.Printf("Use emx-event mark -channel %s %s to update consumption position\n", channel,
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
				return fmt.Errorf("missing -channel argument value")
			}
			channel = args[1]
			args = args[2:]
		case "-h", "--help":
			fmt.Println("Usage: emx-event mark -channel <channel> <position>")
			fmt.Println("")
			fmt.Println("Update a channel's consumption position. Format: file:offset")
			fmt.Println("Position can be obtained from the ls command output.")
			fmt.Println("")
			fmt.Println("Options:")
			fmt.Println("  -channel, -c    channel name (required)")
			return nil
		default:
			if strings.HasPrefix(args[0], "-") {
				return fmt.Errorf("unknown option: %s", args[0])
			}
			posStr = args[0]
			args = args[1:]
		}
	}

	if channel == "" {
		return fmt.Errorf("-channel is required")
	}
	if posStr == "" {
		return fmt.Errorf("position is required (format: file:offset)")
	}

	pos, err := event.ParsePosition(posStr)
	if err != nil {
		return err
	}

	if err := bus.Mark(channel, pos); err != nil {
		return err
	}

	fmt.Printf("Marker updated: %s → %s\n", channel, pos.String())
	return nil
}

// --- status 命令 ---

func cmdStatus(bus *event.Bus, args []string) error {
	var name string

	for len(args) > 0 {
		switch args[0] {
		case "-h", "--help":
			fmt.Println("Usage: emx-event status [filename]")
			fmt.Println("")
			fmt.Println("Show event file status. Defaults to the latest file.")
			fmt.Println("Specify a filename to view a specific file's status.")
			return nil
		default:
			if strings.HasPrefix(args[0], "-") {
				return fmt.Errorf("unknown option: %s", args[0])
			}
			name = args[0]
			args = args[1:]
		}
	}

	st, err := bus.Status(name)
	if err != nil {
		return err
	}

	fmt.Printf("File:         %s", st.Name)
	if st.IsLatest {
		fmt.Printf(" (latest)")
	}
	fmt.Println()
	fmt.Printf("Compressed:   %s\n", formatSize(st.CompressedSize))
	fmt.Printf("Uncompressed: %s\n", formatSize(st.UncompressedSize))
	fmt.Printf("Lines:        %d\n", st.LineCount)
	if st.FirstLineHash != "" {
		fmt.Printf("First hash:   %s\n", st.FirstLineHash)
	}

	// 显示所有 channel marker 状态
	channels, err := bus.ListChannels()
	if err == nil && len(channels) > 0 {
		fmt.Println()
		fmt.Println("Channel Markers:")
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "  Channel\tFile\tOffset\tUpdated\n")
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
		fmt.Printf("All files (%d):\n", len(files))
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
	fmt.Println("emx-event: file-based event bus")
	fmt.Println()
	fmt.Println("Usage: emx-event [-dir <directory>] <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  add      publish an event")
	fmt.Println("  ls       list new events (based on channel marker)")
	fmt.Println("  mark     update channel consumption position")
	fmt.Println("  status   show event file status")
	fmt.Println()
	fmt.Println("Global options:")
	fmt.Println("  -dir     event storage directory (default ~/.emx-mail/events/)")
	fmt.Println("  -h       show help")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  emx-event add -type email.received -channel inbox -payload '{\"from\":\"alice@test.com\"}'")
	fmt.Println("  emx-event ls -channel inbox")
	fmt.Println("  emx-event mark -channel inbox events.001.jsonl.gz:2048")
	fmt.Println("  emx-event status")
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
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
