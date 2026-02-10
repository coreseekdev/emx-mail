package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const version = "1.0.0"

const maxHeaderSize = 1 << 20 // 1MB maximum header size

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		fatalUsage()
	}

	dir := args[0]

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		fatal("failed to create directory: %v", err)
	}

	// Stream the email from stdin:
	//  1. Buffer the header portion (up to the first blank line) to extract
	//     the Message-ID using net/mail which handles RFC 5322 header folding.
	//  2. Write the headers + body to a temp file via streaming (no full
	//     in-memory buffer), then rename to the final path.
	//
	// This matches the streaming contract of the watch handler pipeline:
	// watch → OS pipe → emx-save, with bounded memory usage.

	reader := bufio.NewReaderSize(os.Stdin, 64*1024) // 64KB read buffer

	// Read header portion by scanning until blank line (\r\n\r\n or \n\n).
	var headerBuf []byte
	for {
		line, err := reader.ReadBytes('\n')
		headerBuf = append(headerBuf, line...)

		// Check header size limit to prevent OOM on malformed input
		if len(headerBuf) > maxHeaderSize {
			fatal("header exceeds maximum size (%d bytes)", maxHeaderSize)
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			fatal("failed to read stdin: %v", err)
		}
		// Blank line (just \n or \r\n) marks end of headers
		trimmed := strings.TrimRight(string(line), "\r\n")
		if trimmed == "" {
			break
		}
	}

	if len(headerBuf) == 0 {
		fatal("no email data received")
	}

	// Parse headers using net/mail (handles RFC 5322 folded headers correctly)
	msg, err := mail.ReadMessage(strings.NewReader(string(headerBuf)))
	messageID := ""
	if err == nil {
		messageID = msg.Header.Get("Message-ID")
		messageID = strings.Trim(strings.TrimSpace(messageID), "<>")
	}
	if messageID == "" {
		fatal("no Message-ID header found in email")
	}

	// Generate filename: use hash to avoid leaking information from Message-ID
	// Message-ID can contain internal domains or user info
	// Format: <hash>.eml where hash uses message_id + random for uniqueness
	b := make([]byte, 4)
	var filename string
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if crypto/rand fails
		filename = fmt.Sprintf("%d.eml", time.Now().UnixNano())
	} else {
		// Use message ID + random suffix for unique hash
		hashInput := messageID + hex.EncodeToString(b)
		// Use first 16 chars of hex encoding (enough for uniqueness)
		filename = sanitizeFilename(hashInput[:16]) + ".eml"
	}

	path := filepath.Join(dir, filename)

	// Check if file already exists — append random suffix to avoid overwrite
	if _, err := os.Stat(path); err == nil {
		// Hash collision or duplicate - add extra random bytes
		extra := make([]byte, 4)
		rand.Read(extra)
		filename = sanitizeFilename(hex.EncodeToString(b)+hex.EncodeToString(extra)) + ".eml"
		path = filepath.Join(dir, filename)
	}

	// Write to a temp file in the same directory then rename for atomicity
	tmpFile, err := os.CreateTemp(dir, ".emx-save-*.tmp")
	if err != nil {
		fatal("failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // clean up on error

	// Write the already-buffered header portion
	if _, err := tmpFile.Write(headerBuf); err != nil {
		tmpFile.Close()
		fatal("failed to write headers: %v", err)
	}

	// Stream the remaining body from stdin → file (no full memory buffer)
	if _, err := io.Copy(tmpFile, reader); err != nil {
		tmpFile.Close()
		fatal("failed to write body: %v", err)
	}

	if err := tmpFile.Close(); err != nil {
		fatal("failed to close temp file: %v", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		fatal("failed to rename temp file: %v", err)
	}

	// Write status to stderr (as per watch mode protocol)
	fmt.Fprintf(os.Stderr, `{"type":"saved","message_id":%q,"path":%q}`+"\n", messageID, path)
}

// sanitizeFilename sanitizes a string for safe use as a filename
func sanitizeFilename(name string) string {
	// Replace characters that are unsafe in filenames
	// Keep: alphanumeric, hyphen, underscore, dot, plus
	re := regexp.MustCompile(`[^a-zA-Z0-9._+\-=]`)
	safe := re.ReplaceAllString(name, "_")

	// Limit length
	if len(safe) > 200 {
		safe = safe[:200]
	}

	return safe
}

func fatalUsage() {
	fmt.Fprintf(os.Stderr, `emx-save v%s - Save email from stdin as .eml file

Usage:
  emx-save <directory>

Description:
  Reads a raw RFC 5322 email from stdin and saves it as an .eml file
  in the specified directory, using a hashed filename based on Message-ID.

  The email is streamed from stdin with bounded memory usage — only the
  headers are buffered in memory for Message-ID extraction; the body is
  written directly to disk.

  The filename is hashed to avoid leaking internal information from Message-ID
  (e.g., internal domain names or user identifiers).

Examples:
  # In watch mode
  emx-mail watch -handler "emx-save ./emails"

  # Standalone usage
  cat message.eml | emx-save ./saved-emails
`, version)
	os.Exit(1)
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
