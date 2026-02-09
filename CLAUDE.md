# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

emx-mail is a mail client and patch management toolkit with two main components:
1. **emx-mail CLI** - Command-line email client (SMTP sending, IMAP/POP3 receiving)
2. **emx-b4** - Patch workflow tool for managing email-based patch series (similar to b4) The project follows a layered architecture with clear separation between CLI presentation, email domain types, and protocol implementations.

## Build and Development Commands

### Environment Setup (Required for Dependency Downloads)

Before running any Go commands, set up the proxy:
```bash
export http_proxy="http://127.0.0.1:49725"
export https_proxy="http://127.0.0.1:49725"
```

### Building

```bash
# Build email CLI
go build -o emx-mail.exe ./cmd/cli

# Build patch management tool (emx-b4)
go build -o emx-b4.exe ./cmd/b4
```

### Running the Application

```bash
# Initialize configuration file
./emx-mail.exe init

# Edit the generated config at ~/.emx-mail/config.json
# Then run commands:
./emx-mail.exe list
./emx-mail.exe send -to user@example.com -subject "Test" -text "Hello"
```

## Architecture

### Directory Structure

```
emx-mail/
├── cmd/
│   ├── cli/main.go          # Email CLI entry point (send, list, fetch, delete, folders)
│   └── b4/main.go           # Patch management tool (am, shazam, prep, diff, mbox)
└── pkgs/
    ├── config/              # JSON configuration management
    ├── email/               # Email client functionality
    │   ├── email.go         # Core types (Message, Address, SendOptions, etc.)
    │   ├── smtp.go          # SMTP sending (SMTPClient)
    │   ├── imap.go          # IMAP receiving (IMAPClient)
    │   └── pop3.go          # POP3 receiving (POP3Client)
    └── patchwork/           # Patch workflow management
        ├── subject.go       # Patch subject parsing ([PATCH v3 2/5])
        ├── trailer.go       # Trailer parsing (Signed-off-by, Reviewed-by, etc.)
        ├── message.go       # Email message parsing, series management
        ├── amready.go       # Generate git-am compatible mbox
        ├── git.go           # Git operations wrapper
        └── prep.go          # Prep branch workflow management
```

### Layer Architecture

**Email Client (cmd/cli/main.go)**:
1. **Presentation Layer**: Manual flag parsing, command dispatch, account/protocol resolution
2. **Domain Layer** (`pkgs/email/`): Protocol-agnostic types (Message, SendOptions, etc.)
3. **Protocol Layer**: IMAPClient, POP3Client, SMTPClient with lazy connection pattern
4. **Configuration Layer** (`pkgs/config/`): JSON-based config, multi-account support

**Patch Management (cmd/b4/main.go)**:
1. **Presentation Layer**: Command parsing for am/shazam/prep/diff/mbox
2. **Domain Layer** (`pkgs/patchwork/`):
   - `subject.go`: Parse patch subjects with version info
   - `trailer.go`: Parse and categorize trailers (Signed-off-by, Reviewed-by, etc.)
   - `message.go`: Parse email messages, manage patch series, collect follow-up trailers
   - `amready.go`: Generate git-am compatible mbox output
   - `git.go`: Git operations wrapper (am, apply, format-patch, worktree)
   - `prep.go`: Prep branch workflow (create/reroll/cover/patches)

### Key Design Patterns

**Protocol Auto-Detection**: The `selectProtocol()` function chooses IMAP vs POP3 based on what's configured, preferring IMAP when both are available.

**Lazy Connections**: Protocol clients connect on first use and close after operation completion, simplifying CLI lifecycle management.

**Manual Flag Parsing**: Custom flag parsing instead of `flag` or `cobra` to avoid dependencies. Global flags parsed before subcommand, subcommand flags parsed separately by handlers.

## Protocol Implementation Details

All protocol implementations are in `pkgs/email/` with clear naming:
- `IMAPClient` in imap.go
- `POP3Client` in pop3.go
- `SMTPClient` in smtp.go

### IMAP (pkgs/email/imap.go)

- Uses go-imap v2 (beta version) with modern API patterns
- Supports SSL/TLS, StartTLS
- UID-based operations (persistent message IDs)
- Implements: folder listing, message listing with envelopes, full retrieval, deletion with expunge
- Fetch strategy: Envelopes only for listing, full body with `Peek: true` for single messages

### POP3 (pkgs/email/pop3.go)

- **Custom POP3 implementation** (not using external library)
- Direct protocol implementation following RFC 1939
- Supports SSL/TLS only
- Limitations: No folders (INBOX only), sequence numbers instead of UIDs, no server-side flags, deletions commit on QUIT

### SMTP (pkgs/email/smtp.go)

- Supports SSL/TLS, StartTLS
- PLAIN authentication via SASL
- Builds RFC 5322 messages using `go-message/mail`
- Handles: multipart messages, attachments, HTML bodies, CC/BCC, reply threads (In-Reply-To, References)

## Special Dependencies

### Local Replace Directives

The `go.mod` contains replace directives:
```go
replace (
    github.com/emersion/go-imap/v2 => ../go-imap
    github.com/emersion/go-smtp => ../go-smtp
)
```

This means the project depends on sibling directories `../go-imap` and `../go-smtp`, likely forked/vendor copies for testing beta versions or custom modifications. When cloning this project, ensure these directories exist or remove the replace directives to use official versions.

## Configuration Management

Config location: `~/.emx-mail/config.json`

Account resolution order:
1. Explicit `-account` flag
2. `default_account` from config
3. First account in list
4. Error if no accounts

Security note: Passwords stored in plaintext. File has 0600 permissions but still readable by owner.

## Adding New Commands

### Email CLI (cmd/cli/main.go)
1. Create flag parsing function (e.g., `parseSearchFlags()`) in `cmd/cli/main.go`
2. Create handler function (e.g., `handleSearch()`) in `cmd/cli/main.go`
3. Add case to main switch statement after command dispatch
4. If protocol-specific, implement in `pkgs/email/` (adding to imap.go, pop3.go, or smtp.go)
5. If protocol-agnostic, add types to `pkgs/email/email.go` as needed

### Patch Tool (cmd/b4/main.go)
1. Add command case to switch statement
2. Implement command handler using `pkgs/patchwork` APIs
3. Key APIs:
   - `ParseSubject()` - Parse patch subject lines
   - `ParseTrailer()` - Parse trailer lines
   - `NewMailbox()` - Create mbox reader
   - `GetLatestSeries()` - Extract patch series
   - `GetAMReady()` - Generate git-am ready mbox
   - `NewGit()` - Git operations

## Current Status

**Email Client (Implemented)**:
- Multi-account configuration
- SMTP sending (attachments, HTML, CC/BCC, threading)
- IMAP (folders, list, fetch, delete, attachment download)
- POP3 (list, fetch, delete, attachment download)
- MIME message parsing with Chinese filename support
- Protocol auto-detection

**Patch Management (Implemented)**:
- Patch subject parsing with version detection ([PATCH v3 2/5])
- Trailer parsing and categorization (Signed-off-by, Reviewed-by, etc.)
- Mbox file reading and parsing
- Patch series extraction and management
- Follow-up trailer collection from replies
- Git-am compatible mbox generation
- Prep branch workflow (create/reroll/cover/patches)
- Git operations wrapper (am, apply, format-patch, worktree)
- Comprehensive test coverage

**Not Implemented**:
- Email search functionality
- Batch email operations
- Password encryption for config
- CI/CD
