package email

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// WatchOptions holds options for watch mode
type WatchOptions struct {
	Folder        string
	HandlerCmd    string
	KeepAlive     int // seconds
	PollInterval  int // seconds
	MaxRetries    int
	PollOnly      bool
	Once          bool
	IdleKeepAlive int // seconds, NOOP interval during IDLE
}

// WatchStatus represents a status message type
type WatchStatus struct {
	Type    string `json:"type"`            // "connection", "idle", "process", "mark", "error"
	Level   string `json:"level,omitempty"` // "info", "warn", "error"
	Message string `json:"message"`
	UID     uint32 `json:"uid,omitempty"`
}

// EmailNotification represents a new email notification
type EmailNotification struct {
	Type      string   `json:"type"` // "email"
	UID       uint32   `json:"uid"`
	MessageID string   `json:"message_id"`
	From      string   `json:"from"`
	To        []string `json:"to"`
	Subject   string   `json:"subject"`
	Date      string   `json:"date"`
	Flags     []string `json:"flags"`
}

// Watch starts watching for new emails on the IMAP server.
// The provided context controls the lifetime of the watch loop; cancel it
// (e.g. on SIGINT/SIGTERM) for a graceful shutdown.
func (c *IMAPClient) Watch(ctx context.Context, opts WatchOptions) error {
	// Set defaults
	if opts.Folder == "" {
		opts.Folder = "INBOX"
	}
	if opts.KeepAlive <= 0 {
		opts.KeepAlive = 30
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 30
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 5
	}
	if opts.IdleKeepAlive <= 0 {
		opts.IdleKeepAlive = 300 // 5 minutes default
	}
	// Validate IDLE keep-alive range (min 1 minute, max 29 minutes per RFC 2177)
	if opts.IdleKeepAlive < 60 {
		opts.IdleKeepAlive = 60 // minimum 1 minute
	}
	if opts.IdleKeepAlive > 1740 {
		opts.IdleKeepAlive = 1740 // maximum 29 minutes
	}

	// Connect
	if err := c.Connect(); err != nil {
		return err
	}
	defer c.Close()

	statusWrite := func(s WatchStatus) {
		data, _ := json.Marshal(s)
		fmt.Fprintln(os.Stderr, string(data))
	}

	statusWrite(WatchStatus{
		Type:    "connection",
		Level:   "info",
		Message: fmt.Sprintf("Connected to %s", c.config.Host),
	})

	// Select folder
	if _, err := c.client.Select(opts.Folder, nil).Wait(); err != nil {
		return fmt.Errorf("failed to select folder %s: %w", opts.Folder, err)
	}

	// Check for IDLE support
	supportsIDLE := c.checkIDLESupport()
	if !supportsIDLE && !opts.PollOnly {
		statusWrite(WatchStatus{
			Type:    "idle",
			Level:   "warn",
			Message: fmt.Sprintf("Server doesn't support IDLE, falling back to polling (%ds interval)", opts.PollInterval),
		})
	}

	// Process existing unprocessed emails
	if err := c.processUnprocessed(opts, statusWrite); err != nil {
		statusWrite(WatchStatus{
			Type:    "error",
			Level:   "error",
			Message: fmt.Sprintf("Failed to process existing emails: %v", err),
		})
		// Continue anyway
	}

	if opts.Once {
		statusWrite(WatchStatus{
			Type:    "connection",
			Level:   "info",
			Message: "One-time processing complete, exiting",
		})
		return nil
	}

	// Enter watch loop
	if supportsIDLE && !opts.PollOnly {
		return c.watchIDLE(ctx, opts, statusWrite)
	}
	return c.watchPoll(ctx, opts, statusWrite)
}

// checkIDLESupport checks if the server supports IDLE
func (c *IMAPClient) checkIDLESupport() bool {
	caps, err := c.client.Capability().Wait()
	if err != nil {
		return false
	}
	return caps.Has("IDLE")
}

// processUnprocessed processes emails that are not yet Seen
func (c *IMAPClient) processUnprocessed(opts WatchOptions, statusWrite func(WatchStatus)) error {
	// Use SEARCH UNSEEN to directly fetch unseen emails (avoids N+1 query problem)
	searchData, err := c.client.UIDSearch(&imap.SearchCriteria{
		NotFlag: []imap.Flag{imap.FlagSeen},
	}, nil).Wait()

	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		statusWrite(WatchStatus{
			Type:    "process",
			Level:   "info",
			Message: "No unprocessed emails found",
		})
		return nil
	}

	statusWrite(WatchStatus{
		Type:    "process",
		Level:   "info",
		Message: fmt.Sprintf("Processing %d unprocessed emails", len(uids)),
	})

	// Process each email
	for _, uid := range uids {
		if err := c.processEmail(uint32(uid), opts, statusWrite); err != nil {
			statusWrite(WatchStatus{
				Type:    "error",
				Level:   "error",
				Message: fmt.Sprintf("Failed to process UID %d: %v", uid, err),
				UID:     uint32(uid),
			})
			// Continue with next email (sequential processing)
			continue
		}
	}

	return nil
}

// emailIsSeen checks if an email has the \Seen flag
func (c *IMAPClient) emailIsSeen(uid uint32) (bool, error) {
	uidSet := imap.UIDSetNum(imap.UID(uid))
	msgs, err := c.client.Fetch(uidSet, &imap.FetchOptions{
		Flags: true,
	}).Collect()

	if err != nil {
		return false, err
	}

	if len(msgs) == 0 {
		return false, fmt.Errorf("no messages returned for UID %d", uid)
	}

	msg := msgs[0]
	// Check if Seen
	for _, f := range msg.Flags {
		if f == imap.FlagSeen {
			return true, nil
		}
	}
	return false, nil
}

// processEmail processes a single email
func (c *IMAPClient) processEmail(uid uint32, opts WatchOptions, statusWrite func(WatchStatus)) error {
	// Fetch email metadata
	metadata, err := c.fetchEmailMetadata(uid)
	if err != nil {
		return fmt.Errorf("failed to fetch metadata: %w", err)
	}

	// Fetch full email as a streaming reader (RFC 5322 format).
	// The reader is backed by the IMAP connection and does not buffer the
	// entire message in memory.
	emailReader, cleanup, err := c.fetchRawEmailReader(uid)
	if err != nil {
		return fmt.Errorf("failed to fetch email: %w", err)
	}
	defer cleanup()

	// Notify stdout about new email
	notification := EmailNotification{
		Type:      "email",
		UID:       uid,
		MessageID: metadata.MessageID,
		From:      metadata.From,
		To:        metadata.To,
		Subject:   metadata.Subject,
		Date:      metadata.Date,
		Flags:     metadata.Flags,
	}
	notifData, _ := json.Marshal(notification)
	fmt.Fprintln(os.Stdout, string(notifData))

	// If no handler, just mark as processed
	if opts.HandlerCmd == "" {
		statusWrite(WatchStatus{
			Type:    "process",
			Level:   "info",
			Message: fmt.Sprintf("No handler configured, marking UID %d as processed", uid),
			UID:     uid,
		})
		return c.markAsProcessed(uid, statusWrite)
	}

	// Run handler
	statusWrite(WatchStatus{
		Type:    "process",
		Level:   "info",
		Message: fmt.Sprintf("Processing UID %d with handler: %s", uid, opts.HandlerCmd),
		UID:     uid,
	})

	exitCode, err := c.runHandler(opts.HandlerCmd, emailReader)
	if err != nil {
		return fmt.Errorf("handler execution failed: %w", err)
	}

	if exitCode != 0 {
		return fmt.Errorf("handler failed with exit code %d", exitCode)
	}

	// Handler succeeded, mark as processed
	statusWrite(WatchStatus{
		Type:    "process",
		Level:   "info",
		Message: fmt.Sprintf("Handler succeeded for UID %d, marking as processed", uid),
		UID:     uid,
	})

	return c.markAsProcessed(uid, statusWrite)
}

// EmailMetadata holds email metadata
type EmailMetadata struct {
	MessageID string
	From      string
	To        []string
	Subject   string
	Date      string
	Flags     []string
}

// fetchEmailMetadata fetches email metadata
func (c *IMAPClient) fetchEmailMetadata(uid uint32) (*EmailMetadata, error) {
	uidSet := imap.UIDSetNum(imap.UID(uid))
	msgs, err := c.client.Fetch(uidSet, &imap.FetchOptions{
		Envelope: true,
		Flags:    true,
	}).Collect()

	if err != nil {
		return nil, err
	}

	if len(msgs) == 0 {
		return nil, fmt.Errorf("no messages returned for UID %d", uid)
	}

	msg := msgs[0]

	metadata := &EmailMetadata{
		Flags: convertFlags(msg.Flags),
	}

	if env := msg.Envelope; env != nil {
		metadata.MessageID = env.MessageID
		metadata.Subject = env.Subject
		metadata.Date = env.Date.Format(time.RFC1123)
		if len(env.From) > 0 {
			metadata.From = env.From[0].Addr()
		}
		to := make([]string, 0, len(env.To))
		for _, addr := range env.To {
			to = append(to, addr.Addr())
		}
		metadata.To = to
	}

	return metadata, nil
}

// convertFlags converts imap.Flags to string slice
func convertFlags(flags []imap.Flag) []string {
	result := make([]string, 0, len(flags))
	for _, f := range flags {
		// imap.Flag already includes the backslash prefix (e.g., "\Seen")
		result = append(result, string(f))
	}
	return result
}

// fetchRawEmailReader fetches the raw RFC 5322 email as a streaming reader.
// It returns:
//   - reader: an io.Reader backed by the IMAP literal (OS-pipe friendly).
//   - cleanup: must be called after the reader is fully consumed to release
//     the underlying IMAP fetch command.
//   - err: any error from the IMAP FETCH.
//
// This avoids buffering the entire message in memory. The caller should pipe
// the reader into the handler's stdin via os.Pipe / exec.Cmd.StdinPipe so
// that the kernel pipe buffer (~64 KB) controls peak memory usage.
func (c *IMAPClient) fetchRawEmailReader(uid uint32) (io.Reader, func(), error) {
	uidSet := imap.UIDSetNum(imap.UID(uid))
	bodySection := &imap.FetchItemBodySection{Peek: true}
	fetchCmd := c.client.Fetch(uidSet, &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{bodySection},
	})

	msg := fetchCmd.Next()
	if msg == nil {
		fetchCmd.Close()
		return nil, func() {}, fmt.Errorf("no messages returned for UID %d", uid)
	}

	// Iterate the message's data items to find the body section literal.
	var literal io.Reader
	for {
		item := msg.Next()
		if item == nil {
			break
		}
		if bs, ok := item.(imapclient.FetchItemDataBodySection); ok {
			if bs.Literal != nil {
				literal = bs.Literal
				break
			}
		}
	}

	if literal == nil {
		fetchCmd.Close()
		return nil, func() {}, fmt.Errorf("no body section returned for UID %d", uid)
	}

	// cleanup drains remaining items and closes the fetch command so that the
	// IMAP client can proceed with subsequent commands.
	cleanup := func() {
		fetchCmd.Close()
	}

	return literal, cleanup, nil
}

// runHandler executes the handler program, streaming emailReader into the
// process's stdin through an OS pipe. The kernel pipe buffer (~64 KB on
// Linux, ~1 MB on macOS) provides automatic back-pressure so peak memory
// usage stays bounded regardless of email size.
func (c *IMAPClient) runHandler(cmd string, emailReader io.Reader) (int, error) {
	// Use sh -c to wrap the command, supporting spaces and quotes in paths/args
	cmdObj := exec.Command("sh", "-c", cmd)
	cmdObj.Stdout = os.Stderr // Handler stdout goes to stderr
	cmdObj.Stderr = os.Stderr

	stdinPipe, err := cmdObj.StdinPipe()
	if err != nil {
		return 0, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	if err := cmdObj.Start(); err != nil {
		return 0, fmt.Errorf("failed to start handler: %w", err)
	}

	// Stream email data into the handler's stdin via the OS pipe.
	// io.Copy reads/writes in 32 KB chunks; the kernel pipe buffer
	// handles back-pressure automatically.
	writeErr := make(chan error, 1)
	go func() {
		_, werr := io.Copy(stdinPipe, emailReader)
		stdinPipe.Close() // signals EOF to the handler
		writeErr <- werr
	}()

	waitErr := cmdObj.Wait()

	// Prefer the process exit error; surface write errors only if the
	// process itself succeeded (e.g. broken pipe is expected when the
	// handler exits early).
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, waitErr
	}

	if wErr := <-writeErr; wErr != nil {
		return 1, fmt.Errorf("failed writing to handler stdin: %w", wErr)
	}

	return 0, nil
}

// markAsProcessed marks an email as Seen
func (c *IMAPClient) markAsProcessed(uid uint32, statusWrite func(WatchStatus)) error {
	uidSet := imap.UIDSetNum(imap.UID(uid))

	// Store flags: add Seen flag
	_, err := c.client.Store(uidSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagSeen},
	}, nil).Collect()

	if err != nil {
		return fmt.Errorf("failed to mark UID %d: %w", uid, err)
	}

	statusWrite(WatchStatus{
		Type:    "mark",
		Level:   "info",
		Message: fmt.Sprintf("Marked UID %d as \\Seen", uid),
		UID:     uid,
	})

	return nil
}

// watchIDLE watches for new emails using IMAP IDLE
func (c *IMAPClient) watchIDLE(ctx context.Context, opts WatchOptions, statusWrite func(WatchStatus)) error {
	statusWrite(WatchStatus{
		Type:    "idle",
		Level:   "info",
		Message: "IDLE mode started",
	})

	// Use IdleKeepAlive as IDLE timeout to periodically refresh connection
	// This sends NOOP at regular intervals to keep the connection alive
	idleTimeout := time.Duration(opts.IdleKeepAlive) * time.Second
	if idleTimeout > 29*time.Minute {
		idleTimeout = 29 * time.Minute // RFC 2177 recommends max 29 minutes
	}

	statusWrite(WatchStatus{
		Type:    "idle",
		Level:   "info",
		Message: fmt.Sprintf("IDLE keep-alive interval: %v", idleTimeout),
	})

	for {
		// Check context before starting a new IDLE cycle
		select {
		case <-ctx.Done():
			statusWrite(WatchStatus{
				Type:    "connection",
				Level:   "info",
				Message: "Shutting down (context cancelled)",
			})
			return nil
		default:
		}

		// Start IDLE
		idleCmd, err := c.client.Idle()
		if err != nil {
			return fmt.Errorf("IDLE start failed: %w", err)
		}

		// Wait for updates or timeout.
		// The goroutine waits for server-side IDLE events;
		// buffered channel ensures it can exit even if we time out first,
		// and idleCmd.Close() ensures Wait() returns promptly.
		done := make(chan error, 1)
		go func() {
			done <- idleCmd.Wait()
		}()

		timer := time.NewTimer(idleTimeout)
		select {
		case <-ctx.Done():
			timer.Stop()
			idleCmd.Close()
			<-done // drain the channel
			statusWrite(WatchStatus{
				Type:    "connection",
				Level:   "info",
				Message: "Shutting down (context cancelled)",
			})
			return nil

		case <-timer.C:
			// IDLE timeout - refresh connection with NOOP
			idleCmd.Close()
			<-done // Drain goroutine
			statusWrite(WatchStatus{
				Type:    "idle",
				Level:   "info",
				Message: "IDLE timeout, sending NOOP to keep connection alive",
			})

		case err := <-done:
			// Server sent new email data or IDLE failed
			timer.Stop()
			idleCmd.Close()
			if err != nil {
				statusWrite(WatchStatus{
					Type:    "error",
					Level:   "error",
					Message: fmt.Sprintf("IDLE failed: %v", err),
				})
				// Try to reconnect
				if err := c.reconnect(ctx, opts, statusWrite); err != nil {
					return err
				}
				continue
			}
			statusWrite(WatchStatus{
				Type:    "idle",
				Level:   "info",
				Message: "IDLE response received, new emails detected",
			})
		}

		// Process new emails
		if err := c.processUnprocessed(opts, statusWrite); err != nil {
			statusWrite(WatchStatus{
				Type:    "error",
				Level:   "error",
				Message: fmt.Sprintf("Failed to process new emails: %v", err),
			})
		}

		// Send NOOP to keep connection alive
		if err := c.client.Noop().Wait(); err != nil {
			statusWrite(WatchStatus{
				Type:    "connection",
				Level:   "error",
				Message: fmt.Sprintf("NOOP failed: %v", err),
			})
			// Try to reconnect
			if err := c.reconnect(ctx, opts, statusWrite); err != nil {
				return err
			}
		}
	}
}

// watchPoll watches for new emails using polling
func (c *IMAPClient) watchPoll(ctx context.Context, opts WatchOptions, statusWrite func(WatchStatus)) error {
	interval := time.Duration(opts.PollInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	statusWrite(WatchStatus{
		Type:    "idle",
		Level:   "info",
		Message: fmt.Sprintf("Polling mode started (interval: %ds)", opts.PollInterval),
	})

	for {
		select {
		case <-ctx.Done():
			statusWrite(WatchStatus{
				Type:    "connection",
				Level:   "info",
				Message: "Shutting down (context cancelled)",
			})
			return nil

		case <-ticker.C:
			// Check for new emails
			if err := c.processUnprocessed(opts, statusWrite); err != nil {
				statusWrite(WatchStatus{
					Type:    "error",
					Level:   "error",
					Message: fmt.Sprintf("Failed to check for new emails: %v", err),
				})
			}

			// NOOP to keep connection alive
			if err := c.client.Noop().Wait(); err != nil {
				statusWrite(WatchStatus{
					Type:    "connection",
					Level:   "error",
					Message: fmt.Sprintf("NOOP failed: %v", err),
				})
				// Try to reconnect
				if err := c.reconnect(ctx, opts, statusWrite); err != nil {
					return err
				}
			}
		}
	}
}

// reconnect attempts to reconnect with exponential backoff
func (c *IMAPClient) reconnect(ctx context.Context, opts WatchOptions, statusWrite func(WatchStatus)) error {
	for attempt := 0; attempt < opts.MaxRetries; attempt++ {
		waitTime := time.Duration(1<<uint(attempt)) * time.Second
		if waitTime > 30*time.Second {
			waitTime = 30 * time.Second
		}

		statusWrite(WatchStatus{
			Type:    "connection",
			Level:   "warn",
			Message: fmt.Sprintf("Connection lost, reconnecting in %v (attempt %d/%d)", waitTime, attempt+1, opts.MaxRetries),
		})

		// Check context cancellation during backoff wait
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}

		c.Close()
		if err := c.Connect(); err != nil {
			statusWrite(WatchStatus{
				Type:    "connection",
				Level:   "error",
				Message: fmt.Sprintf("Reconnect failed: %v", err),
			})
			continue
		}

		if _, err := c.client.Select(opts.Folder, nil).Wait(); err != nil {
			c.Close()
			statusWrite(WatchStatus{
				Type:    "connection",
				Level:   "error",
				Message: fmt.Sprintf("Failed to select folder after reconnect: %v", err),
			})
			continue
		}

		statusWrite(WatchStatus{
			Type:    "connection",
			Level:   "info",
			Message: "Reconnected successfully",
		})
		return nil
	}

	return fmt.Errorf("failed to reconnect after %d attempts", opts.MaxRetries)
}
