package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emx-mail/cli/pkgs/config"
	"github.com/emx-mail/cli/pkgs/email"
)

const version = "1.0.0"

// app holds global options parsed from the command line
type app struct {
	account string
	verbose bool
}

func main() {
	a := &app{}
	args := os.Args[1:]

	// Parse global flags (before the subcommand)
	for len(args) > 0 {
		switch args[0] {
		case "-account":
			if len(args) < 2 {
				fatal("-account requires a value")
			}
			a.account = args[1]
			args = args[2:]
		case "-v", "--verbose":
			a.verbose = true
			args = args[1:]
		case "-version", "--version":
			fmt.Printf("emx-mail CLI v%s\n", version)
			os.Exit(0)
		case "-h", "--help", "help":
			printUsage()
			os.Exit(0)
		default:
			// Not a global flag — treat as subcommand
			goto dispatch
		}
	}

dispatch:
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	cmdArgs := args[1:]

	// "init" doesn't need config loaded
	if cmd == "init" {
		if err := handleInit(); err != nil {
			fatal("init: %v", err)
		}
		return
	}

	// Load config and resolve account
	acc := a.loadAccount()

	switch cmd {
	case "send":
		opts := parseSendFlags(cmdArgs)
		if err := handleSend(acc, opts); err != nil {
			fatal("send: %v", err)
		}
	case "list":
		opts := parseListFlags(cmdArgs)
		if err := handleList(acc, opts, a.verbose); err != nil {
			fatal("list: %v", err)
		}
	case "fetch":
		opts := parseFetchFlags(cmdArgs)
		if err := handleFetch(acc, opts); err != nil {
			fatal("fetch: %v", err)
		}
	case "delete":
		opts := parseDeleteFlags(cmdArgs)
		if err := handleDelete(acc, opts); err != nil {
			fatal("delete: %v", err)
		}
	case "folders":
		if err := handleFolders(acc); err != nil {
			fatal("folders: %v", err)
		}
	case "watch":
		opts := parseWatchFlags(cmdArgs)
		if err := handleWatch(acc, opts); err != nil {
			fatal("watch: %v", err)
		}
	default:
		fatal("unknown command '%s'", cmd)
	}
}

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

// --- Flag parsing helpers ---

type sendFlags struct {
	to, cc, subject, text, html, attachment, inReplyTo string
}

func parseSendFlags(args []string) sendFlags {
	var f sendFlags
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-to":
			i++
			f.to = args[i]
		case "-cc":
			i++
			f.cc = args[i]
		case "-subject":
			i++
			f.subject = args[i]
		case "-text":
			i++
			f.text = args[i]
		case "-html":
			i++
			f.html = args[i]
		case "-attachment":
			i++
			f.attachment = args[i]
		case "-in-reply-to":
			i++
			f.inReplyTo = args[i]
		default:
			fatal("send: unknown flag '%s'", args[i])
		}
	}
	return f
}

type listFlags struct {
	folder     string
	limit      int
	unreadOnly bool
	protocol   string
}

func parseListFlags(args []string) listFlags {
	f := listFlags{folder: "INBOX", limit: 20}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-folder":
			i++
			f.folder = args[i]
		case "-limit":
			i++
			fmt.Sscanf(args[i], "%d", &f.limit)
		case "-unread-only":
			f.unreadOnly = true
		case "-protocol":
			i++
			f.protocol = args[i]
		default:
			fatal("list: unknown flag '%s'", args[i])
		}
	}
	return f
}

type fetchFlags struct {
	uid             string
	folder          string
	output          string
	format          string
	protocol        string
	saveAttachments string // Directory to save attachments
}

func parseFetchFlags(args []string) fetchFlags {
	f := fetchFlags{folder: "INBOX", format: "text"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-uid":
			i++
			f.uid = args[i]
		case "-folder":
			i++
			f.folder = args[i]
		case "-output":
			i++
			f.output = args[i]
		case "-format":
			i++
			f.format = args[i]
		case "-protocol":
			i++
			f.protocol = args[i]
		case "-save-attachments":
			i++
			f.saveAttachments = args[i]
		default:
			fatal("fetch: unknown flag '%s'", args[i])
		}
	}
	return f
}

type deleteFlags struct {
	uid      string
	folder   string
	expunge  bool
	protocol string
}

func parseDeleteFlags(args []string) deleteFlags {
	f := deleteFlags{folder: "INBOX"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-uid":
			i++
			f.uid = args[i]
		case "-folder":
			i++
			f.folder = args[i]
		case "-expunge":
			f.expunge = true
		case "-protocol":
			i++
			f.protocol = args[i]
		default:
			fatal("delete: unknown flag '%s'", args[i])
		}
	}
	return f
}

// --- Helpers to create IMAP/SMTP/POP3 clients from config ---

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
	}), nil
}

// selectProtocol returns "imap" or "pop3" based on config and user flag.
// If protocol is empty, it auto-selects based on what is configured.
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
	return "imap" // default
}

// --- Command handlers ---

func handleSend(acc *config.AccountConfig, f sendFlags) error {
	if f.to == "" {
		return fmt.Errorf("-to is required")
	}
	if f.subject == "" {
		return fmt.Errorf("-subject is required")
	}
	if f.text == "" && f.html == "" {
		return fmt.Errorf("-text or -html is required")
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

func handleFetch(acc *config.AccountConfig, f fetchFlags) error {
	if f.uid == "" {
		return fmt.Errorf("-uid is required")
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

		// Show attachments
		if len(msg.Attachments) > 0 {
			fmt.Fprintf(out, "\nAttachments (%d):\n", len(msg.Attachments))
			for i, att := range msg.Attachments {
				fmt.Fprintf(out, "  [%d] %s (%s, %d bytes)\n", i+1, att.Filename, att.ContentType, att.Size)
			}

			// Save attachments if requested
			if f.saveAttachments != "" {
				fmt.Fprintf(os.Stderr, "\nSaving attachments to: %s\n", f.saveAttachments)
				for i, att := range msg.Attachments {
					if att.Data == nil {
						fmt.Fprintf(os.Stderr, "  [%d] Skipping %s (no data)\n", i+1, att.Filename)
						continue
					}

					// Create target directory
					if err := os.MkdirAll(f.saveAttachments, 0755); err != nil {
						return fmt.Errorf("failed to create directory: %w", err)
					}

					// Build file path
					filePath := filepath.Join(f.saveAttachments, att.Filename)

					// Write file
					if err := os.WriteFile(filePath, att.Data, 0644); err != nil {
						return fmt.Errorf("failed to write %s: %w", att.Filename, err)
					}

					fmt.Fprintf(os.Stderr, "  [%d] Saved: %s\n", i+1, att.Filename)
				}
			}
		}

		fmt.Fprintf(out, "\n%s\n", msg.TextBody)
	default:
		return fmt.Errorf("unsupported format: %s", f.format)
	}
	return nil
}

func handleDelete(acc *config.AccountConfig, f deleteFlags) error {
	if f.uid == "" {
		return fmt.Errorf("-uid is required")
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

func handleInit() error {
	root := config.ExampleRootConfig()

	if config.HasEmxConfig() {
		data, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to format example config: %w", err)
		}

		fmt.Println("emx-config detected. Configure emx-mail using emx-config.")
		fmt.Println("Example JSON (keys under 'mail'):")
		fmt.Println(string(data))
		fmt.Println("Store this in your emx-config file (e.g., config.json).")
		fmt.Println("Then verify with: emx-config list --json")
		return nil
	}

	configPath, err := config.GetEnvConfigPath()
	if err != nil {
		return err
	}
	if err := config.SaveConfig(configPath, root); err != nil {
		return err
	}
	fmt.Printf("Created config file at: %s\n", configPath)
	fmt.Printf("Set %s to point to this file.\n", config.EnvConfigJSONPath)
	fmt.Println("Please edit the file to add your email account credentials.")
	return nil
}

// --- Watch command ---

type watchFlags struct {
	folder     string
	handler    string
	pollOnly   bool
	once       bool
}

func parseWatchFlags(args []string) watchFlags {
	var f watchFlags
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-folder":
			i++
			f.folder = args[i]
		case "-handler":
			i++
			f.handler = args[i]
		case "-poll-only":
			f.pollOnly = true
		case "-once":
			f.once = true
		default:
			fatal("watch: unknown flag '%s'", args[i])
		}
	}
	return f
}

func handleWatch(acc *config.AccountConfig, opts watchFlags) error {
	// Check if IMAP is configured
	if acc.IMAP.Host == "" {
		return fmt.Errorf("watch mode requires IMAP configuration")
	}

	// Build watch options from config and flags
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

	// Create IMAP client
	client := email.NewIMAPClient(email.IMAPConfig{
		Host:     acc.IMAP.Host,
		Port:     acc.IMAP.Port,
		Username: acc.IMAP.Username,
		Password: acc.IMAP.Password,
		SSL:      acc.IMAP.SSL,
		StartTLS: acc.IMAP.StartTLS,
	})

	// Start watching
	return client.Watch(watchOpts)
}

// --- Utility functions ---

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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `emx-mail CLI v%s - Command-line email client

Usage:
  emx-mail [global options] <command> [command options]

Commands:
  send       Send an email
  list       List emails in a folder
  fetch      Fetch and display an email
  delete     Delete an email
  folders    List all folders
  watch      Watch for new emails (IMAP only)
  init       Initialize configuration file

Global Options:
  -account <name>  Account name or email to use
  -v               Verbose output
  -version         Show version information

Config Resolution:
  1) If emx-config exists: emx-mail reads config via emx-config list --json.
  2) Otherwise: set env var EMX_MAIL_CONFIG_JSON to a JSON config file.

Send Options:
  -to <emails>         Recipients (comma-separated)
  -cc <emails>         CC recipients (comma-separated)
  -subject <text>      Email subject
  -text <text>         Plain text body
  -html <html>         HTML body
  -attachment <path>   Attachment file path
  -in-reply-to <msgid> Message-ID to reply to

List Options:
  -folder <name>       Folder to list (default: INBOX)
  -limit <number>      Maximum messages to show (default: 20)
  -unread-only         Show only unread messages
  -protocol <proto>    Force protocol: imap or pop3 (auto-detected)

Fetch Options:
  -uid <uid>           Message UID (IMAP) or ID (POP3) to fetch
  -folder <name>       Folder containing the message (default: INBOX)
  -output <path>       Output file (default: stdout)
  -format <format>     Output format: text, html, or raw (default: text)
  -protocol <proto>    Force protocol: imap or pop3 (auto-detected)
  -save-attachments <dir>  Save attachments to directory

Delete Options:
  -uid <uid>           Message UID (IMAP) or ID (POP3) to delete
  -folder <name>       Folder containing the message (default: INBOX)
  -expunge             Permanently remove (expunge) the message (IMAP only)
  -protocol <proto>    Force protocol: imap or pop3 (auto-detected)

Watch Options:
  -folder <name>       Folder to watch (default: INBOX)
  -handler <cmd>       Handler command for new emails (receives raw EML via stdin)
  -poll-only          Force polling mode (disable IDLE)
  -once               Process existing emails then exit

Watch Handler:
  The handler receives the raw RFC 5322 email via stdin. Exit code 0 marks as processed.
  Use emx-save to save emails as .eml files:
  - Build: go build -o emx-save.exe ./cmd/emx-save
  - Use:   emx-mail watch -handler "emx-save ./emails"

Examples:
  emx-mail list
  emx-mail -v list -limit 5
  emx-mail send -to user@example.com -subject "Hello" -text "Hi!"
  emx-mail fetch -uid 12345
  emx-mail delete -uid 12345 -expunge
  emx-mail folders
  emx-mail init
  emx-mail watch -handler "emx-save ./emails"
  emx-mail watch -once -handler "emx-save ./emails"
`, version)
}
