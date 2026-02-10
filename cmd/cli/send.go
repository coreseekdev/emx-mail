package main

import (
	"fmt"

	"github.com/emx-mail/cli/pkgs/config"
	"github.com/emx-mail/cli/pkgs/email"
	flag "github.com/spf13/pflag"
)

type sendFlags struct {
	to, cc, subject, text, html, attachment, inReplyTo string
}

func parseSendFlags(args []string) sendFlags {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	var f sendFlags
	fs.StringVar(&f.to, "to", "", "Recipients (comma-separated)")
	fs.StringVar(&f.cc, "cc", "", "CC recipients (comma-separated)")
	fs.StringVar(&f.subject, "subject", "", "Email subject")
	fs.StringVar(&f.text, "text", "", "Plain text body")
	fs.StringVar(&f.html, "html", "", "HTML body")
	fs.StringVar(&f.attachment, "attachment", "", "Attachment file path")
	fs.StringVar(&f.inReplyTo, "in-reply-to", "", "Message-ID to reply to")
	if err := fs.Parse(args); err != nil {
		fatal("send: %v", err)
	}
	return f
}

func handleSend(acc *config.AccountConfig, f sendFlags) error {
	if f.to == "" {
		return fmt.Errorf("--to is required")
	}
	if f.subject == "" {
		return fmt.Errorf("--subject is required")
	}
	if f.text == "" && f.html == "" {
		return fmt.Errorf("--text or --html is required")
	}

	opts := email.SendOptions{
		From:      email.Address{Name: acc.FromName, Email: acc.Email},
		To:        parseAddressList(f.to),
		Subject:   f.subject,
		TextBody:  f.text,
		HTMLBody:  f.html,
		InReplyTo: f.inReplyTo,
	}
	if f.cc != "" {
		opts.Cc = parseAddressList(f.cc)
	}
	if f.attachment != "" {
		opts.Attachments = []email.AttachmentPath{{Filename: f.attachment, Path: f.attachment}}
	}

	client := newSMTPClient(acc)
	if err := client.Send(opts); err != nil {
		return err
	}
	fmt.Println("Email sent successfully")
	return nil
}
