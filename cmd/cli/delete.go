package main

import (
	"fmt"

	"github.com/emx-mail/cli/pkgs/config"
	flag "github.com/spf13/pflag"
)

type deleteFlags struct {
	uid      string
	folder   string
	expunge  bool
	protocol string
}

func parseDeleteFlags(args []string) deleteFlags {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	var f deleteFlags
	fs.StringVar(&f.uid, "uid", "", "Message UID (IMAP) or ID (POP3) to delete")
	fs.StringVar(&f.folder, "folder", "INBOX", "Folder containing the message")
	fs.BoolVar(&f.expunge, "expunge", false, "Permanently remove the message (IMAP only)")
	fs.StringVar(&f.protocol, "protocol", "", "Force protocol: imap or pop3")
	if err := fs.Parse(args); err != nil {
		fatal("delete: %v", err)
	}
	return f
}

func handleDelete(acc *config.AccountConfig, f deleteFlags) error {
	if f.uid == "" {
		return fmt.Errorf("--uid is required")
	}

	var uid uint32
	if _, err := fmt.Sscanf(f.uid, "%d", &uid); err != nil {
		return fmt.Errorf("invalid UID: %s", f.uid)
	}

	proto := selectProtocol(acc, f.protocol)

	switch proto {
	case "pop3":
		client, cerr := newPOP3Client(acc)
		if cerr != nil {
			return cerr
		}
		if err := client.DeleteMessage(uid); err != nil {
			return err
		}
		fmt.Println("Message deleted (POP3 DELE + QUIT)")
	default: // imap
		client, cerr := newIMAPClient(acc)
		if cerr != nil {
			return cerr
		}
		if err := client.DeleteMessage(f.folder, uid, f.expunge); err != nil {
			return err
		}
		action := "marked for deletion"
		if f.expunge {
			action = "permanently deleted"
		}
		fmt.Printf("Message %s\n", action)
	}
	return nil
}
