package email

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
)

// IMAPClient represents an IMAP client
type IMAPClient struct {
	config IMAPConfig
	client *imapclient.Client
}

// IMAPConfig holds IMAP configuration
type IMAPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	SSL      bool
	StartTLS bool
}

// NewIMAPClient creates a new IMAP client
func NewIMAPClient(config IMAPConfig) *IMAPClient {
	return &IMAPClient{
		config: config,
	}
}

// Connect establishes a connection to the IMAP server
func (c *IMAPClient) Connect() error {
	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	var client *imapclient.Client
	var err error

	if c.config.SSL {
		client, err = imapclient.DialTLS(addr, &imapclient.Options{})
	} else if c.config.StartTLS {
		client, err = imapclient.DialStartTLS(addr, &imapclient.Options{})
	} else {
		client, err = imapclient.DialInsecure(addr, &imapclient.Options{})
	}
	if err != nil {
		return fmt.Errorf("failed to connect to IMAP server %s: %w", addr, err)
	}

	// Authenticate
	if err := client.Login(c.config.Username, c.config.Password).Wait(); err != nil {
		client.Close()
		return fmt.Errorf("IMAP authentication failed: %w", err)
	}

	c.client = client
	return nil
}

// Close closes the IMAP connection
func (c *IMAPClient) Close() error {
	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}

// ensureConnected ensures the client is connected, returns a cleanup func
func (c *IMAPClient) ensureConnected() (func(), error) {
	if c.client != nil {
		return func() {}, nil
	}
	if err := c.Connect(); err != nil {
		return nil, err
	}
	return func() { c.Close() }, nil
}

// ListFolders lists all folders/mailboxes
func (c *IMAPClient) ListFolders() ([]Folder, error) {
	cleanup, err := c.ensureConnected()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	mailboxes, err := c.client.List("", "*", &imap.ListOptions{}).Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to list folders: %w", err)
	}

	folders := make([]Folder, 0, len(mailboxes))
	for _, mb := range mailboxes {
		folders = append(folders, Folder{
			Name: mb.Mailbox,
		})
	}
	return folders, nil
}

// FetchMessages fetches message envelopes from a folder
func (c *IMAPClient) FetchMessages(opts FetchOptions) (*ListResult, error) {
	cleanup, err := c.ensureConnected()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	folder := opts.Folder
	if folder == "" {
		folder = "INBOX"
	}

	// Select mailbox
	selectData, err := c.client.Select(folder, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to select folder %s: %w", folder, err)
	}

	numMessages := selectData.NumMessages
	if numMessages == 0 {
		return &ListResult{
			Messages: []*Message{},
			Total:    0,
			Unread:   0,
			Folder:   folder,
		}, nil
	}

	// Get status for unread count
	var unread int
	statusData, err := c.client.Status(folder, &imap.StatusOptions{
		NumMessages: true,
		NumUnseen:   true,
	}).Wait()
	if err == nil && statusData.NumUnseen != nil {
		unread = int(*statusData.NumUnseen)
	}

	// Calculate the range of sequence numbers to fetch
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	start := uint32(1)
	if numMessages > uint32(limit) {
		start = numMessages - uint32(limit) + 1
	}

	// Fetch using sequence numbers
	seqSet := imap.SeqSet{}
	seqSet.AddRange(start, numMessages)

	fetchOptions := &imap.FetchOptions{
		Envelope: true,
		Flags:    true,
		UID:      true,
	}

	msgs, err := c.client.Fetch(seqSet, fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	messages := make([]*Message, 0, len(msgs))
	for _, buf := range msgs {
		msg := convertIMAPFetchBuffer(buf)
		messages = append(messages, msg)
	}

	// Reverse so newest messages come first
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return &ListResult{
		Messages: messages,
		Total:    int(numMessages),
		Unread:   unread,
		Folder:   folder,
	}, nil
}

// FetchMessage fetches a single message by UID, including body
func (c *IMAPClient) FetchMessage(folder string, uid uint32) (*Message, error) {
	cleanup, err := c.ensureConnected()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if folder == "" {
		folder = "INBOX"
	}

	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("failed to select folder %s: %w", folder, err)
	}

	// Fetch envelope + full body
	bodySection := &imap.FetchItemBodySection{
		Peek: true, // don't mark as read
	}
	fetchOptions := &imap.FetchOptions{
		Envelope:    true,
		Flags:       true,
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}

	uidSet := imap.UIDSetNum(imap.UID(uid))
	msgs, err := c.client.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch message UID %d: %w", uid, err)
	}

	if len(msgs) == 0 {
		return nil, fmt.Errorf("message UID %d not found in %s", uid, folder)
	}

	buf := msgs[0]
	msg := convertIMAPFetchBuffer(buf)

	// Parse the body content
	rawBody := buf.FindBodySection(bodySection)
	if rawBody != nil {
		parseIMAPMessageBody(msg, rawBody)
	}

	return msg, nil
}

// DeleteMessage deletes a message by UID
func (c *IMAPClient) DeleteMessage(folder string, uid uint32, expunge bool) error {
	cleanup, err := c.ensureConnected()
	if err != nil {
		return err
	}
	defer cleanup()

	if folder == "" {
		folder = "INBOX"
	}

	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("failed to select folder %s: %w", folder, err)
	}

	// Mark as deleted using UID
	uidSet := imap.UIDSetNum(imap.UID(uid))
	_, err = c.client.Store(uidSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}, nil).Collect()
	if err != nil {
		return fmt.Errorf("failed to mark message as deleted: %w", err)
	}

	if expunge {
		if _, err := c.client.Expunge().Collect(); err != nil {
			return fmt.Errorf("failed to expunge messages: %w", err)
		}
	}

	return nil
}

// FetchMessageByID implements MailReceiver.
func (c *IMAPClient) FetchMessageByID(folder string, uid uint32) (*Message, error) {
	return c.FetchMessage(folder, uid)
}

// DeleteMessageByID implements MailReceiver.
func (c *IMAPClient) DeleteMessageByID(folder string, uid uint32, expunge bool) error {
	return c.DeleteMessage(folder, uid, expunge)
}

// MarkAsSeen marks a message as seen
func (c *IMAPClient) MarkAsSeen(folder string, uid uint32) error {
	cleanup, err := c.ensureConnected()
	if err != nil {
		return err
	}
	defer cleanup()

	if folder == "" {
		folder = "INBOX"
	}

	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("failed to select folder %s: %w", folder, err)
	}

	uidSet := imap.UIDSetNum(imap.UID(uid))
	_, err = c.client.Store(uidSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagSeen},
	}, nil).Collect()
	if err != nil {
		return fmt.Errorf("failed to mark message as seen: %w", err)
	}

	return nil
}

// Ping sends a NOOP command to keep the connection alive
func (c *IMAPClient) Ping() error {
	if c.client == nil {
		return nil
	}
	return c.client.Noop().Wait()
}

// --- internal helpers ---

// convertIMAPFetchBuffer converts a FetchMessageBuffer to our Message
func convertIMAPFetchBuffer(buf *imapclient.FetchMessageBuffer) *Message {
	msg := &Message{
		UID:    uint32(buf.UID),
		SeqNum: buf.SeqNum,
	}

	if env := buf.Envelope; env != nil {
		msg.Subject = env.Subject
		msg.Date = env.Date
		msg.MessageID = env.MessageID
		msg.InReplyTo = strings.Join(env.InReplyTo, " ")
		msg.References = env.InReplyTo // best effort from envelope
		msg.From = convertIMAPAddresses(env.From)
		msg.To = convertIMAPAddresses(env.To)
		msg.Cc = convertIMAPAddresses(env.Cc)
		msg.Bcc = convertIMAPAddresses(env.Bcc)
	}

	// Convert flags
	for _, f := range buf.Flags {
		switch f {
		case imap.FlagSeen:
			msg.Flags.Seen = true
		case imap.FlagFlagged:
			msg.Flags.Flagged = true
		case imap.FlagAnswered:
			msg.Flags.Answered = true
		case imap.FlagDraft:
			msg.Flags.Draft = true
		case imap.FlagDeleted:
			msg.Flags.Deleted = true
		}
	}

	return msg
}

// convertIMAPAddresses converts IMAP addresses to our Addresses
func convertIMAPAddresses(addrs []imap.Address) []Address {
	result := make([]Address, 0, len(addrs))
	for _, a := range addrs {
		result = append(result, Address{
			Name:  a.Name,
			Email: a.Addr(),
		})
	}
	return result
}

// parseIMAPMessageBody parses raw RFC 5322 message bytes into text/html body
func parseIMAPMessageBody(msg *Message, raw []byte) {
	r := bytes.NewReader(raw)
	entity, err := gomessage.Read(r)
	if err != nil {
		// Fallback: treat as plain text
		msg.TextBody = string(raw)
		return
	}

	parseEntityBody(msg, entity)
}
