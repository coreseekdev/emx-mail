package main

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/emx-mail/cli/pkgs/config"
	"github.com/emx-mail/cli/pkgs/email"
)

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}

func (a *app) loadAccount() *config.AccountConfig {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Run 'emx-mail init' to create config instructions\n")
		os.Exit(1)
	}
	acc, err := cfg.GetAccount(a.account)
	if err != nil {
		fatal("%v", err)
	}
	return acc
}

// parseAddressList splits a comma-separated address string.
func parseAddressList(s string) []email.Address {
	parts := strings.Split(s, ",")
	addrs := make([]email.Address, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			addrs = append(addrs, email.Address{Email: part})
		}
	}
	return addrs
}

func formatAddress(addr email.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s>", addr.Name, addr.Email)
	}
	return addr.Email
}

func formatAddressList(addrs []email.Address) string {
	if len(addrs) == 0 {
		return ""
	}
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = formatAddress(a)
	}
	return strings.Join(parts, ", ")
}

// truncate truncates a string to maxLen runes, preserving UTF-8 boundaries.
func truncate(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen]) + "..."
}
