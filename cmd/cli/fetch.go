package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"io"

	"github.com/emx-mail/cli/pkgs/config"
	"github.com/emx-mail/cli/pkgs/email"
	flag "github.com/spf13/pflag"
)

type fetchFlags struct {
	uid             string
	folder          string
	output          string
	format          string
	protocol        string
	saveAttachments string
}

func parseFetchFlags(args []string) fetchFlags {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	var f fetchFlags
	fs.StringVar(&f.uid, "uid", "", "Message UID (IMAP) or ID (POP3) to fetch")
	fs.StringVar(&f.folder, "folder", "INBOX", "Folder containing the message")
	fs.StringVar(&f.output, "output", "", "Output file (default: stdout)")
	fs.StringVar(&f.format, "format", "text", "Output format: text or html")
	fs.StringVar(&f.protocol, "protocol", "", "Force protocol: imap or pop3")
	fs.StringVar(&f.saveAttachments, "save-attachments", "", "Save attachments to directory")
	if err := fs.Parse(args); err != nil {
		fatal("fetch: %v", err)
	}
	return f
}

// validateAttachmentPath checks that the resolved path stays within baseDir.
func validateAttachmentPath(baseDir, filename string) (string, error) {
	// Clean the filename to prevent path traversal
	cleaned := filepath.Base(filename) // strip directory components
	if cleaned == "." || cleaned == ".." || cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("invalid attachment filename: %s", filename)
	}
	full := filepath.Join(baseDir, cleaned)
	// Double-check that the resolved path is under baseDir
	absBase, _ := filepath.Abs(baseDir)
	absFull, _ := filepath.Abs(full)
	if !strings.HasPrefix(absFull, absBase+string(filepath.Separator)) && absFull != absBase {
		return "", fmt.Errorf("attachment path escapes target directory: %s", filename)
	}
	return full, nil
}

func handleFetch(acc *config.AccountConfig, f fetchFlags) error {
	if f.uid == "" {
		return fmt.Errorf("--uid is required")
	}

	var uid uint32
	if _, err := fmt.Sscanf(f.uid, "%d", &uid); err != nil {
		return fmt.Errorf("invalid UID: %s", f.uid)
	}

	proto := selectProtocol(acc, f.protocol)

	var msg *email.Message
	var err error

	switch proto {
	case "pop3":
		client, cerr := newPOP3Client(acc)
		if cerr != nil {
			return cerr
		}
		msg, err = client.FetchMessage(uid)
	default: // imap
		client, cerr := newIMAPClient(acc)
		if cerr != nil {
			return cerr
		}
		msg, err = client.FetchMessage(f.folder, uid)
	}
	if err != nil {
		return err
	}

	var out io.Writer = os.Stdout
	if f.output != "" {
		file, err := os.Create(f.output)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer file.Close()
		out = file
	}

	switch f.format {
	case "html":
		if msg.HTMLBody == "" {
			return fmt.Errorf("no HTML body available")
		}
		fmt.Fprintln(out, msg.HTMLBody)
	case "text", "":
		fmt.Fprintf(out, "From: %s\n", formatAddressList(msg.From))
		fmt.Fprintf(out, "To: %s\n", formatAddressList(msg.To))
		if len(msg.Cc) > 0 {
			fmt.Fprintf(out, "Cc: %s\n", formatAddressList(msg.Cc))
		}
		fmt.Fprintf(out, "Subject: %s\n", msg.Subject)
		fmt.Fprintf(out, "Date: %s\n", msg.Date.Format(time.RFC1123))
		fmt.Fprintf(out, "Message-ID: %s\n", msg.MessageID)

		if len(msg.Attachments) > 0 {
			fmt.Fprintf(out, "\nAttachments (%d):\n", len(msg.Attachments))
			for i, att := range msg.Attachments {
				fmt.Fprintf(out, "  [%d] %s (%s, %d bytes)\n", i+1, att.Filename, att.ContentType, att.Size)
			}

			if f.saveAttachments != "" {
				fmt.Fprintf(os.Stderr, "\nSaving attachments to: %s\n", f.saveAttachments)
				if err := os.MkdirAll(f.saveAttachments, 0755); err != nil {
					return fmt.Errorf("failed to create directory: %w", err)
				}
				for i, att := range msg.Attachments {
					if att.Data == nil {
						fmt.Fprintf(os.Stderr, "  [%d] Skipping %s (no data)\n", i+1, att.Filename)
						continue
					}
					// Validate path to prevent traversal
					filePath, err := validateAttachmentPath(f.saveAttachments, att.Filename)
					if err != nil {
						fmt.Fprintf(os.Stderr, "  [%d] Skipping %s: %v\n", i+1, att.Filename, err)
						continue
					}
					if err := os.WriteFile(filePath, att.Data, 0644); err != nil {
						return fmt.Errorf("failed to write %s: %w", att.Filename, err)
					}
					fmt.Fprintf(os.Stderr, "  [%d] Saved: %s\n", i+1, filepath.Base(att.Filename))
				}
			}
		}

		fmt.Fprintf(out, "\n%s\n", msg.TextBody)
	default:
		return fmt.Errorf("unsupported format: %s", f.format)
	}
	return nil
}
