package main

import (
	"fmt"

	"github.com/emx-mail/cli/pkgs/config"
	"github.com/emx-mail/cli/pkgs/email"
)

func newIMAPClient(acc *config.AccountConfig) (*email.IMAPClient, error) {
	if acc.IMAP.Host == "" {
		return nil, fmt.Errorf("IMAP not configured for account %s", acc.Email)
	}
	return email.NewIMAPClient(email.IMAPConfig{
		Host:     acc.IMAP.Host,
		Port:     acc.IMAP.Port,
		Username: acc.IMAP.Username,
		Password: acc.IMAP.Password,
		SSL:      acc.IMAP.SSL,
		StartTLS: acc.IMAP.StartTLS,
	}), nil
}

func newSMTPClient(acc *config.AccountConfig) *email.SMTPClient {
	return email.NewSMTPClient(email.SMTPConfig{
		Host:     acc.SMTP.Host,
		Port:     acc.SMTP.Port,
		Username: acc.SMTP.Username,
		Password: acc.SMTP.Password,
		SSL:      acc.SMTP.SSL,
		StartTLS: acc.SMTP.StartTLS,
	})
}

func newPOP3Client(acc *config.AccountConfig) (*email.POP3Client, error) {
	if acc.POP3.Host == "" {
		return nil, fmt.Errorf("POP3 not configured for account %s", acc.Email)
	}
	return email.NewPOP3Client(email.POP3Config{
		Host:     acc.POP3.Host,
		Port:     acc.POP3.Port,
		Username: acc.POP3.Username,
		Password: acc.POP3.Password,
		SSL:      acc.POP3.SSL,
		StartTLS: acc.POP3.StartTLS,
	}), nil
}

// selectProtocol returns "imap" or "pop3" based on config and user flag.
func selectProtocol(acc *config.AccountConfig, protocol string) string {
	if protocol != "" {
		return protocol
	}
	if acc.IMAP.Host != "" {
		return "imap"
	}
	if acc.POP3.Host != "" {
		return "pop3"
	}
	return "imap"
}
