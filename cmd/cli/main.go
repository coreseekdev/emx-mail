package main

import (
	"fmt"
	"os"

	flag "github.com/spf13/pflag"
)

const version = "1.0.0"

// app holds global options parsed from the command line
type app struct {
	account string
	verbose bool
}

func main() {
	a := &app{}

	// Global flags
	flag.StringVar(&a.account, "account", "", "Account name or email to use")
	flag.BoolVarP(&a.verbose, "verbose", "v", false, "Verbose output")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Usage = printUsage
	flag.Parse()

	if *showVersion {
		fmt.Printf("emx-mail CLI v%s\n", version)
		os.Exit(0)
	}

	args := flag.Args()
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
	case "help":
		printUsage()
		os.Exit(0)
	default:
		fatal("unknown command '%s'", cmd)
	}
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
  --account <name>   Account name or email to use
  -v, --verbose      Verbose output
  --version          Show version information

Config Resolution:
  1) If emx-config exists: emx-mail reads config via emx-config list --json.
  2) Otherwise: set env var EMX_MAIL_CONFIG_JSON to a JSON config file.

Send Options:
  --to <emails>          Recipients (comma-separated)
  --cc <emails>          CC recipients (comma-separated)
  --subject <text>       Email subject
  --text <text>          Plain text body
  --html <html>          HTML body
  --attachment <path>    Attachment file path
  --in-reply-to <msgid>  Message-ID to reply to

List Options:
  --folder <name>        Folder to list (default: INBOX)
  --limit <number>       Maximum messages to show (default: 20)
  --unread-only          Show only unread messages
  --protocol <proto>     Force protocol: imap or pop3 (auto-detected)

Fetch Options:
  --uid <uid>            Message UID (IMAP) or ID (POP3) to fetch
  --folder <name>        Folder containing the message (default: INBOX)
  --output <path>        Output file (default: stdout)
  --format <format>      Output format: text or html (default: text)
  --protocol <proto>     Force protocol: imap or pop3 (auto-detected)
  --save-attachments <dir>  Save attachments to directory

Delete Options:
  --uid <uid>            Message UID (IMAP) or ID (POP3) to delete
  --folder <name>        Folder containing the message (default: INBOX)
  --expunge              Permanently remove (expunge) the message (IMAP only)
  --protocol <proto>     Force protocol: imap or pop3 (auto-detected)

Watch Options:
  --folder <name>        Folder to watch (default: INBOX)
  --handler <cmd>        Handler command for new emails (receives raw EML via stdin)
  --poll-only            Force polling mode (disable IDLE)
  --once                 Process existing emails then exit

Watch Handler:
  The handler receives the raw RFC 5322 email via stdin. Exit code 0 marks as processed.
  Use emx-save to save emails as .eml files:
  - Build: go build -o emx-save.exe ./cmd/emx-save
  - Use:   emx-mail watch --handler "emx-save ./emails"

Examples:
  emx-mail list
  emx-mail -v list --limit 5
  emx-mail send --to user@example.com --subject "Hello" --text "Hi!"
  emx-mail fetch --uid 12345
  emx-mail delete --uid 12345 --expunge
  emx-mail folders
  emx-mail init
  emx-mail watch --handler "emx-save ./emails"
  emx-mail watch --once --handler "emx-save ./emails"
`, version)
}
