package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/emx-mail/cli/pkgs/config"
	"github.com/emx-mail/cli/pkgs/email"
	flag "github.com/spf13/pflag"
)

type sendFlags struct {
	to, cc, subject, text, html, inReplyTo string
	textFile, htmlFile                     string
	attachments                            []string
}

func parseSendFlags(args []string) sendFlags {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	var f sendFlags
	fs.StringVar(&f.to, "to", "", "Recipients (comma-separated)")
	fs.StringVar(&f.cc, "cc", "", "CC recipients (comma-separated)")
	fs.StringVar(&f.subject, "subject", "", "Email subject")
	fs.StringVar(&f.text, "text", "", "Plain text body")
	fs.StringVar(&f.html, "html", "", "HTML body")
	fs.StringVar(&f.textFile, "text-file", "", "Plain text body from file (\"-\" for stdin)")
	fs.StringVar(&f.htmlFile, "html-file", "", "HTML body from file (\"-\" for stdin)")
	fs.StringArrayVar(&f.attachments, "attachment", nil, "Attachment file path (repeatable)")
	fs.StringVar(&f.inReplyTo, "in-reply-to", "", "Message-ID to reply to")
	if err := fs.Parse(args); err != nil {
		fatal("send: %v", err)
	}
	return f
}

// readBodySource reads body content from a file path or stdin ("-").
func readBodySource(path string) (string, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()
		r = f
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func handleSend(acc *config.AccountConfig, f sendFlags) error {
	if f.to == "" {
		return fmt.Errorf("--to is required")
	}
	if f.subject == "" {
		return fmt.Errorf("--subject is required")
	}

	// Resolve text body: --text-file takes precedence over --text
	textBody := f.text
	if f.textFile != "" {
		body, err := readBodySource(f.textFile)
		if err != nil {
			return fmt.Errorf("--text-file: %w", err)
		}
		textBody = body
	}

	// Resolve HTML body: --html-file takes precedence over --html
	htmlBody := f.html
	if f.htmlFile != "" {
		body, err := readBodySource(f.htmlFile)
		if err != nil {
			return fmt.Errorf("--html-file: %w", err)
		}
		htmlBody = body
	}

	if textBody == "" && htmlBody == "" {
		return fmt.Errorf("--text, --text-file, --html, or --html-file is required")
	}

	opts := email.SendOptions{
		From:      email.Address{Name: acc.FromName, Email: acc.Email},
		To:        parseAddressList(f.to),
		Subject:   f.subject,
		TextBody:  textBody,
		HTMLBody:  htmlBody,
		InReplyTo: f.inReplyTo,
	}
	if f.cc != "" {
		opts.Cc = parseAddressList(f.cc)
	}
	for _, att := range f.attachments {
		opts.Attachments = append(opts.Attachments, email.AttachmentPath{
			Filename: filepath.Base(att),
			Path:     att,
		})
	}

	client := newSMTPClient(acc)
	if err := client.Send(opts); err != nil {
		return err
	}
	fmt.Println("Email sent successfully")
	return nil
}
