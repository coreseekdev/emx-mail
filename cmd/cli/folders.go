package main

import (
	"fmt"

	"github.com/emx-mail/cli/pkgs/config"
)

func handleFolders(acc *config.AccountConfig) error {
	if acc.IMAP.Host == "" {
		if acc.POP3.Host != "" {
			fmt.Println("POP3 does not support folders. Only INBOX is available.")
			return nil
		}
		return fmt.Errorf("neither IMAP nor POP3 is configured")
	}

	client, err := newIMAPClient(acc)
	if err != nil {
		return err
	}

	folders, err := client.ListFolders()
	if err != nil {
		return err
	}

	fmt.Println("Folders:")
	for _, f := range folders {
		flags := ""
		if f.ReadOnly {
			flags = " [read-only]"
		}
		fmt.Printf("  %s%s\n", f.Name, flags)
	}
	return nil
}
