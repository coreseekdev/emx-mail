package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/emx-mail/cli/pkgs/config"
	"github.com/emx-mail/cli/pkgs/email"
	flag "github.com/spf13/pflag"
)

type listFlags struct {
	folder     string
	limit      int
	unreadOnly bool
	protocol   string
}

func parseListFlags(args []string) listFlags {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	var f listFlags
	fs.StringVar(&f.folder, "folder", "INBOX", "Folder to list")
	fs.IntVar(&f.limit, "limit", 20, "Maximum messages to show")
	fs.BoolVar(&f.unreadOnly, "unread-only", false, "Show only unread messages")
	fs.StringVar(&f.protocol, "protocol", "", "Force protocol: imap or pop3")
	if err := fs.Parse(args); err != nil {
		fatal("list: %v", err)
	}
	return f
}

func handleList(acc *config.AccountConfig, f listFlags, verbose bool) error {
	proto := selectProtocol(acc, f.protocol)

	var result *email.ListResult
	var err error

	switch proto {
	case "pop3":
		client, cerr := newPOP3Client(acc)
		if cerr != nil {
			return cerr
		}
		result, err = client.FetchMessages(email.FetchOptions{
			Folder: "INBOX",
			Limit:  f.limit,
		})
	default: // imap
		client, cerr := newIMAPClient(acc)
		if cerr != nil {
			return cerr
		}
		result, err = client.FetchMessages(email.FetchOptions{
			Folder: f.folder,
			Limit:  f.limit,
		})
	}
	if err != nil {
		return err
	}

	fmt.Printf("Protocol: %s | Folder: %s\n", strings.ToUpper(proto), result.Folder)
	fmt.Printf("Total: %d, Unread: %d\n\n", result.Total, result.Unread)

	for i, msg := range result.Messages {
		if f.unreadOnly && msg.Flags.Seen {
			continue
		}

		from := "Unknown"
		if len(msg.From) > 0 {
			from = formatAddress(msg.From[0])
		}

		status := "✗"
		if msg.Flags.Seen {
			status = "✓"
		}

		idLabel := "UID"
		if proto == "pop3" {
			idLabel = "ID"
		}

		fmt.Printf("[%d] %s:%d %s From: %s\n", i+1, idLabel, msg.UID, status, from)
		fmt.Printf("    Subject: %s\n", msg.Subject)
		fmt.Printf("    Date: %s\n", msg.Date.Format(time.RFC1123))
		fmt.Printf("    Message-ID: %s\n", msg.MessageID)
		if verbose {
			fmt.Printf("    Preview: %s\n", truncate(msg.TextBody, 100))
		}
		fmt.Println()
	}
	return nil
}
