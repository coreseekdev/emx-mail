package email

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"strconv"
	"strings"
	"time"

	gomessage "github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
)

// POP3Client represents a POP3 client with high-level operations
// that return email.Message types.
type POP3Client struct {
	config POP3Config
}

// POP3Config holds POP3 configuration
type POP3Config struct {
	Host     string
	Port     int
	Username string
	Password string
	SSL      bool
}

// NewPOP3Client creates a new POP3 client
func NewPOP3Client(config POP3Config) *POP3Client {
	return &POP3Client{config: config}
}

// FetchMessages connects, authenticates, and fetches message headers.
func (c *POP3Client) FetchMessages(opts FetchOptions) (*ListResult, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.quit()

	count, _, err := conn.stat()
	if err != nil {
		return nil, fmt.Errorf("POP3 STAT failed: %w", err)
	}

	if count == 0 {
		return &ListResult{
			Messages: []*Message{},
			Total:    0,
			Folder:   "INBOX",
		}, nil
	}

	// Determine range to fetch
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	start := 1
	if count > limit {
		start = count - limit + 1
	}

	messages := make([]*Message, 0, count-start+1)

	for id := start; id <= count; id++ {
		// Use TOP to fetch headers + 0 body lines for listing
		entity, err := conn.top(id, 0)
		if err != nil {
			// If TOP is not supported, fall back to RETR
			entity, err = conn.retr(id)
			if err != nil {
				continue // skip messages that fail to parse
			}
		}

		msg := pop3EntityToMessage(entity, uint32(id))
		messages = append(messages, msg)
	}

	// Reverse so newest messages come first
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return &ListResult{
		Messages: messages,
		Total:    count,
		Folder:   "INBOX",
	}, nil
}

// FetchMessage fetches a single message by its sequence number (1-based).
// POP3 does not have UIDs like IMAP; the "uid" here maps to the message number.
func (c *POP3Client) FetchMessage(msgID uint32) (*Message, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.quit()

	entity, err := conn.retr(int(msgID))
	if err != nil {
		return nil, fmt.Errorf("POP3 RETR %d failed: %w", msgID, err)
	}

	msg := pop3EntityToMessage(entity, msgID)
	parsePOP3EntityBody(msg, entity)

	return msg, nil
}

// DeleteMessage deletes a message by its sequence number.
// POP3 deletions are only finalized on a successful QUIT.
func (c *POP3Client) DeleteMessage(msgID uint32) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}

	if err := conn.dele(int(msgID)); err != nil {
		conn.conn.Close() // don't QUIT to avoid committing partial state
		return fmt.Errorf("POP3 DELE %d failed: %w", msgID, err)
	}

	// QUIT commits the deletion
	return conn.quit()
}

// ListMessageIDs returns all message (id, size) pairs.
func (c *POP3Client) ListMessageIDs() ([]POP3MessageID, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.quit()

	return conn.list(0)
}

// connect dials and authenticates to the POP3 server.
func (c *POP3Client) connect() (*pop3Conn, error) {
	addr := net.JoinHostPort(c.config.Host, fmt.Sprintf("%d", c.config.Port))

	var netConn net.Conn
	var err error

	dialer := &net.Dialer{Timeout: 10 * time.Second}

	if c.config.SSL {
		netConn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			ServerName: c.config.Host,
		})
	} else {
		netConn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("POP3 connection to %s failed: %w", addr, err)
	}

	conn := &pop3Conn{
		conn: netConn,
		r:    bufio.NewReader(netConn),
		w:    bufio.NewWriter(netConn),
	}

	// Read the server greeting
	if _, err := conn.readOne(); err != nil {
		netConn.Close()
		return nil, fmt.Errorf("POP3 greeting failed: %w", err)
	}

	// Authenticate
	if err := conn.auth(c.config.Username, c.config.Password); err != nil {
		netConn.Close()
		return nil, fmt.Errorf("POP3 authentication failed: %w", err)
	}

	return conn, nil
}

// ---------- low-level POP3 protocol ----------

// POP3MessageID contains the ID and size of an individual message.
type POP3MessageID struct {
	ID   int
	Size int
	UID  string // only available via UIDL
}

var (
	pop3LineBreak   = []byte("\r\n")
	pop3RespOK      = []byte("+OK")
	pop3RespOKInfo  = []byte("+OK ")
	pop3RespErr     = []byte("-ERR")
	pop3RespErrInfo = []byte("-ERR ")
)

// pop3Conn is a raw POP3 connection.
type pop3Conn struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

// send writes a POP3 command line.
func (c *pop3Conn) send(s string) error {
	if _, err := c.w.WriteString(s + "\r\n"); err != nil {
		return err
	}
	return c.w.Flush()
}

// cmd sends a command and reads the response.
// If isMulti is true, it reads until the "." terminator.
func (c *pop3Conn) cmd(cmd string, isMulti bool, args ...interface{}) (*bytes.Buffer, error) {
	cmdLine := cmd
	if len(args) > 0 {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = fmt.Sprintf("%v", a)
		}
		cmdLine = cmd + " " + strings.Join(parts, " ")
	}

	if err := c.send(cmdLine); err != nil {
		return nil, err
	}

	b, err := c.readOne()
	if err != nil {
		return nil, err
	}

	if !isMulti {
		return bytes.NewBuffer(b), nil
	}

	return c.readAll()
}

// readOne reads a single-line response and checks +OK/-ERR.
func (c *pop3Conn) readOne() ([]byte, error) {
	b, _, err := c.r.ReadLine()
	if err != nil {
		return nil, err
	}
	return parsePOP3Resp(b)
}

// readAll reads lines until the POP3 multiline terminator ".".
func (c *pop3Conn) readAll() (*bytes.Buffer, error) {
	buf := &bytes.Buffer{}
	for {
		b, _, err := c.r.ReadLine()
		if err != nil {
			return nil, err
		}
		if bytes.Equal(b, []byte(".")) {
			break
		}
		// Byte-stuff: lines starting with "." have the leading dot removed
		if bytes.HasPrefix(b, []byte("..")) {
			b = b[1:]
		}
		buf.Write(b)
		buf.Write(pop3LineBreak)
	}
	return buf, nil
}

// auth authenticates with USER/PASS.
func (c *pop3Conn) auth(user, password string) error {
	if _, err := c.cmd("USER", false, user); err != nil {
		return err
	}
	if _, err := c.cmd("PASS", false, password); err != nil {
		return err
	}
	// NOOP to confirm auth succeeded
	_, err := c.cmd("NOOP", false)
	return err
}

// stat returns message count and total size.
func (c *pop3Conn) stat() (count, size int, err error) {
	b, err := c.cmd("STAT", false)
	if err != nil {
		return 0, 0, err
	}
	f := bytes.Fields(b.Bytes())
	if len(f) < 2 {
		return 0, 0, nil
	}
	count, _ = strconv.Atoi(string(f[0]))
	size, _ = strconv.Atoi(string(f[1]))
	return count, size, nil
}

// list returns message IDs and sizes. If msgID > 0, only that message.
func (c *pop3Conn) list(msgID int) ([]POP3MessageID, error) {
	var buf *bytes.Buffer
	var err error

	if msgID <= 0 {
		buf, err = c.cmd("LIST", true)
	} else {
		buf, err = c.cmd("LIST", false, msgID)
	}
	if err != nil {
		return nil, err
	}

	var out []POP3MessageID
	for _, l := range bytes.Split(buf.Bytes(), pop3LineBreak) {
		f := bytes.Fields(l)
		if len(f) < 2 {
			continue
		}
		id, _ := strconv.Atoi(string(f[0]))
		sz, _ := strconv.Atoi(string(f[1]))
		out = append(out, POP3MessageID{ID: id, Size: sz})
	}
	return out, nil
}

// uidl returns message IDs and UIDs.
func (c *pop3Conn) uidl(msgID int) ([]POP3MessageID, error) {
	var buf *bytes.Buffer
	var err error

	if msgID <= 0 {
		buf, err = c.cmd("UIDL", true)
	} else {
		buf, err = c.cmd("UIDL", false, msgID)
	}
	if err != nil {
		return nil, err
	}

	var out []POP3MessageID
	for _, l := range bytes.Split(buf.Bytes(), pop3LineBreak) {
		f := bytes.Fields(l)
		if len(f) < 2 {
			continue
		}
		id, _ := strconv.Atoi(string(f[0]))
		out = append(out, POP3MessageID{ID: id, UID: string(f[1])})
	}
	return out, nil
}

// retr downloads and parses a message.
func (c *pop3Conn) retr(msgID int) (*gomessage.Entity, error) {
	b, err := c.cmd("RETR", true, msgID)
	if err != nil {
		return nil, err
	}
	m, err := gomessage.Read(b)
	if err != nil && !gomessage.IsUnknownCharset(err) {
		return nil, err
	}
	return m, nil
}

// top retrieves headers + numLines body lines.
func (c *pop3Conn) top(msgID, numLines int) (*gomessage.Entity, error) {
	b, err := c.cmd("TOP", true, msgID, numLines)
	if err != nil {
		return nil, err
	}
	m, err := gomessage.Read(b)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// dele marks a message for deletion.
func (c *pop3Conn) dele(msgID int) error {
	_, err := c.cmd("DELE", false, msgID)
	return err
}

// quit sends QUIT and closes the connection.
func (c *pop3Conn) quit() error {
	c.cmd("QUIT", false) //nolint: ignore QUIT errors
	return c.conn.Close()
}

// ---------- response parsing ----------

func parsePOP3Resp(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, nil
	}
	if bytes.Equal(b, pop3RespOK) {
		return nil, nil
	}
	if bytes.HasPrefix(b, pop3RespOKInfo) {
		return bytes.TrimPrefix(b, pop3RespOKInfo), nil
	}
	if bytes.Equal(b, pop3RespErr) {
		return nil, errors.New("POP3: unknown error")
	}
	if bytes.HasPrefix(b, pop3RespErrInfo) {
		return nil, fmt.Errorf("POP3: %s", bytes.TrimPrefix(b, pop3RespErrInfo))
	}
	return nil, fmt.Errorf("POP3: unexpected response: %s", string(b))
}

// ---------- message conversion ----------

// pop3EntityToMessage converts a go-message Entity to our Message,
// extracting headers from the entity's mail.Header.
func pop3EntityToMessage(entity *gomessage.Entity, seqNum uint32) *Message {
	msg := &Message{
		UID:      seqNum, // POP3 has no real UID; use sequence number
		SeqNum:   seqNum,
		Internal: true,
	}

	h := mail.Header{Header: entity.Header}

	msg.Subject, _ = h.Subject()
	msg.Date, _ = h.Date()
	msg.MessageID = h.Get("Message-Id")
	msg.InReplyTo = h.Get("In-Reply-To")

	if refs := h.Get("References"); refs != "" {
		msg.References = strings.Fields(refs)
	}

	if from, err := h.AddressList("From"); err == nil {
		msg.From = pop3MailAddrsToEmail(from)
	}
	if to, err := h.AddressList("To"); err == nil {
		msg.To = pop3MailAddrsToEmail(to)
	}
	if cc, err := h.AddressList("Cc"); err == nil {
		msg.Cc = pop3MailAddrsToEmail(cc)
	}

	return msg
}

// parsePOP3EntityBody reads the body of an entity into TextBody/HTMLBody.
func parsePOP3EntityBody(msg *Message, entity *gomessage.Entity) {
	if mr := entity.MultipartReader(); mr != nil {
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			ct, _, _ := part.Header.ContentType()
			body, err := io.ReadAll(part.Body)
			if err != nil {
				continue
			}
			switch {
			case strings.HasPrefix(ct, "text/plain") && msg.TextBody == "":
				msg.TextBody = string(body)
			case strings.HasPrefix(ct, "text/html") && msg.HTMLBody == "":
				msg.HTMLBody = string(body)
			case strings.HasPrefix(ct, "multipart/"):
				parseNestedPOP3Multipart(msg, part)
			default:
				ah := mail.AttachmentHeader{Header: part.Header}
				filename, _ := ah.Filename()
				msg.Attachments = append(msg.Attachments, Attachment{
					Filename:    filename,
					ContentType: ct,
					Size:        int64(len(body)),
					Data:        body, // Store attachment data
				})
			}
		}
	} else {
		ct, _, _ := entity.Header.ContentType()
		body, err := io.ReadAll(entity.Body)
		if err != nil {
			return
		}
		if strings.HasPrefix(ct, "text/html") {
			msg.HTMLBody = string(body)
		} else {
			msg.TextBody = string(body)
		}
	}
}

func parseNestedPOP3Multipart(msg *Message, entity *gomessage.Entity) {
	mr := entity.MultipartReader()
	if mr == nil {
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		ct, _, _ := part.Header.ContentType()
		body, err := io.ReadAll(part.Body)
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(ct, "text/plain") && msg.TextBody == "":
			msg.TextBody = string(body)
		case strings.HasPrefix(ct, "text/html") && msg.HTMLBody == "":
			msg.HTMLBody = string(body)
		}
	}
}

func pop3MailAddrsToEmail(addrs []*mail.Address) []Address {
	dec := &mime.WordDecoder{}
	out := make([]Address, len(addrs))
	for i, a := range addrs {
		name := a.Name
		if decoded, err := dec.DecodeHeader(name); err == nil {
			name = decoded
		}
		out[i] = Address{Name: name, Email: a.Address}
	}
	return out
}
