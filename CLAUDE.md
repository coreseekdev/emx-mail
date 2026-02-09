# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

emx-mail is a command-line email client written in Go that provides SMTP sending and IMAP/POP3 receiving capabilities. The project follows a layered architecture with clear separation between CLI presentation, email domain types, and protocol implementations.

## Build and Development Commands

### Environment Setup (Required for Dependency Downloads)

Before running any Go commands, set up the proxy:
```bash
export http_proxy="http://127.0.0.1:49725"
export https_proxy="http://127.0.0.1:49725"
```

### Building

```bash
# Install/update dependencies
go mod tidy

# Build the CLI executable
go build -o emx-mail.exe ./cmd/cli
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
├── cmd/cli/main.go          # CLI entry point, command parsing, and dispatch
└── pkgs/
    ├── config/              # JSON configuration management (~/.emx-mail/config.json)
    └── email/               # All email-related functionality
        ├── email.go         # Core email types (Message, Address, SendOptions, etc.)
        ├── smtp.go          # SMTP sending implementation
        ├── imap.go          # IMAP receiving implementation
        └── pop3.go          # POP3 receiving implementation (custom protocol)
```

### Layer Architecture

1. **Presentation Layer** (`cmd/cli/main.go`):
   - Manual flag parsing (not using `flag` package)
   - Command dispatch to handlers
   - Account and protocol resolution
   - All errors go through `fatal()` which exits with code 1

2. **Domain Layer** (`pkgs/email/`):
   - **email.go**: Protocol-agnostic email types
     - `Message`: Core email representation with headers, body, flags, attachments
     - `SendOptions`, `FetchOptions`, `ListResult`: Operation parameters
   - **imap.go**: IMAP client implementation (IMAPClient)
   - **pop3.go**: POP3 client implementation (POP3Client)
   - **smtp.go**: SMTP client implementation (SMTPClient)
   - All protocol clients in one package with clear naming (IMAPClient, POP3Client, SMTPClient)

3. **Configuration Layer** (`pkgs/config/`):
   - JSON-based config at `~/.emx-mail/config.json`
   - Multi-account support with default account selection
   - Passwords stored in plaintext (0600 permissions)

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

1. Create flag parsing function (e.g., `parseSearchFlags()`) in `cmd/cli/main.go`
2. Create handler function (e.g., `handleSearch()`) in `cmd/cli/main.go`
3. Add case to main switch statement after command dispatch
4. If protocol-specific, implement in `pkgs/email/` (adding to imap.go, pop3.go, or smtp.go as appropriate)
5. If protocol-agnostic, add types to `pkgs/email/email.go` as needed

## Current Status

**Implemented**:
- Multi-account configuration
- SMTP sending (attachments, HTML, CC/BCC, threading)
- IMAP (folders, list, fetch, delete)
- POP3 (list, fetch, delete)
- MIME message parsing
- Protocol auto-detection

**Not Implemented**:
- Test suite (no test files exist)
- Search functionality
- Batch operations
- Password encryption
- CI/CD
- Git repository (not initialized)
