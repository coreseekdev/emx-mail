package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const version = "1.0.0"

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

	// Read email from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fatal("failed to read stdin: %v", err)
	}

	if len(data) == 0 {
		fatal("no email data received")
	}

	// Extract Message-ID from headers
	messageID := extractMessageID(data)
	if messageID == "" {
		fatal("no Message-ID header found in email")
	}

	// Sanitize Message-ID for use as filename
	filename := sanitizeFilename(messageID) + ".eml"
	path := filepath.Join(dir, filename)

	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		// File exists, append timestamp to avoid overwrite
		timestamp := strings.ReplaceAll(time.Now().Format("20060102-150405"), ":", "")
		filename = sanitizeFilename(messageID) + "-" + timestamp + ".eml"
		path = filepath.Join(dir, filename)
	}

	// Write email to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		fatal("failed to write file: %v", err)
	}

	// Write status to stderr (as per watch mode protocol)
	fmt.Fprintf(os.Stderr, `{"type":"saved","message_id":%q,"path":%q}`+"\n", messageID, path)
}

// extractMessageID extracts the Message-ID header from email data
func extractMessageID(data []byte) string {
	// Message-ID is in the headers section (before the first blank line)
	headerEnd := bytes.Index(data, []byte("\n\n"))
	if headerEnd == -1 {
		headerEnd = bytes.Index(data, []byte("\r\n\r\n"))
	}
	if headerEnd == -1 {
		return ""
	}

	headers := data[:headerEnd]

	// Look for Message-ID header (case-insensitive)
	lines := bytes.Split(headers, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimLeft(line, "\r")
		if bytes.HasPrefix(bytes.ToLower(line), []byte("message-id:")) {
			// Extract the value after the colon
			parts := bytes.SplitN(line, []byte(":"), 2)
			if len(parts) == 2 {
				// Remove leading/trailing whitespace and angle brackets
				value := strings.TrimSpace(string(parts[1]))
				value = strings.Trim(value, "<>")
				return value
			}
		}
	}

	return ""
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
  in the specified directory, using the Message-ID header as the filename.

  The filename is sanitized for filesystem safety.

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
