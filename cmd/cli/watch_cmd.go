package main

import (
	"fmt"

	"github.com/emx-mail/cli/pkgs/config"
	"github.com/emx-mail/cli/pkgs/email"
	flag "github.com/spf13/pflag"
)

type watchFlags struct {
	folder   string
	handler  string
	pollOnly bool
	once     bool
}

func parseWatchFlags(args []string) watchFlags {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	var f watchFlags
	fs.StringVar(&f.folder, "folder", "", "Folder to watch (default: INBOX)")
	fs.StringVar(&f.handler, "handler", "", "Handler command for new emails")
	fs.BoolVar(&f.pollOnly, "poll-only", false, "Force polling mode (disable IDLE)")
	fs.BoolVar(&f.once, "once", false, "Process existing emails then exit")
	if err := fs.Parse(args); err != nil {
		fatal("watch: %v", err)
	}
	return f
}

func handleWatch(acc *config.AccountConfig, opts watchFlags) error {
	if acc.IMAP.Host == "" {
		return fmt.Errorf("watch mode requires IMAP configuration")
	}

	watchOpts := email.WatchOptions{
		Folder:     opts.folder,
		HandlerCmd: opts.handler,
		PollOnly:   opts.pollOnly,
		Once:       opts.once,
	}

	// Apply config defaults if specified
	if acc.Watch != nil {
		if watchOpts.Folder == "" && acc.Watch.Folder != "" {
			watchOpts.Folder = acc.Watch.Folder
		}
		if watchOpts.HandlerCmd == "" && acc.Watch.HandlerCmd != "" {
			watchOpts.HandlerCmd = acc.Watch.HandlerCmd
		}
		if acc.Watch.KeepAlive > 0 {
			watchOpts.KeepAlive = acc.Watch.KeepAlive
		}
		if acc.Watch.PollInterval > 0 {
			watchOpts.PollInterval = acc.Watch.PollInterval
		}
		if acc.Watch.MaxRetries > 0 {
			watchOpts.MaxRetries = acc.Watch.MaxRetries
		}
	}

	client := email.NewIMAPClient(email.IMAPConfig{
		Host:     acc.IMAP.Host,
		Port:     acc.IMAP.Port,
		Username: acc.IMAP.Username,
		Password: acc.IMAP.Password,
		SSL:      acc.IMAP.SSL,
		StartTLS: acc.IMAP.StartTLS,
	})

	return client.Watch(watchOpts)
}
