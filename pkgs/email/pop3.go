package email

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
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
	conn   *pop3Conn // reusable session; nil when not connected
}

// POP3Config holds POP3 configuration
type POP3Config struct {
	Host      string
	Port      int
	Username  string
	Password  string
	SSL       bool
	StartTLS  bool
	TLSConfig *tls.Config // optional; if nil a default config is used
}

// NewPOP3Client creates a new POP3 client
func NewPOP3Client(config POP3Config) *POP3Client {
	return &POP3Client{config: config}
}

// Connect establishes and authenticates a POP3 session that will be
// reused across subsequent method calls. Call Close() when done.
func (c *POP3Client) Connect() error {
	if c.conn != nil {
		return nil // already connected
	}
	conn, err := c.dial()
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

// Close closes the POP3 connection (issues QUIT to commit any pending DELE).
func (c *POP3Client) Close() error {
	if c.conn != nil {
		err := c.conn.quit()
		c.conn = nil
		return err
	}
	return nil
}

// ensureConnected returns a cleanup function. If the client already has a
// persistent connection (via Connect), cleanup is a no-op. Otherwise a
// temporary connection is created and cleanup will QUIT it.
func (c *POP3Client) ensureConnected() (func(), error) {
	if c.conn != nil {
		return func() {}, nil
	}
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	c.conn = conn
	return func() {
		c.conn.quit()
		c.conn = nil
	}, nil
}

// FetchMessages connects, authenticates, and fetches message headers.
func (c *POP3Client) FetchMessages(opts FetchOptions) (*ListResult, error) {
	cleanup, err := c.ensureConnected()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	count, _, err := c.conn.stat()
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
		entity, err := c.conn.top(id, 0)
		if err != nil {
			// If TOP is not supported, fall back to RETR
			entity, err = c.conn.retr(id)
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
	cleanup, err := c.ensureConnected()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	entity, err := c.conn.retr(int(msgID))
	if err != nil {
		return nil, fmt.Errorf("POP3 RETR %d failed: %w", msgID, err)
	}

	msg := pop3EntityToMessage(entity, msgID)
	parseEntityBody(msg, entity)

	return msg, nil
}

// DeleteMessage deletes a message by its sequence number.
// POP3 deletions are only finalized on a successful QUIT.
func (c *POP3Client) DeleteMessage(msgID uint32) error {
	cleanup, err := c.ensureConnected()
	if err != nil {
		return err
	}

	if err := c.conn.dele(int(msgID)); err != nil {
		// On error, discard the connection without QUIT to avoid committing
		c.conn.conn.Close()
		c.conn = nil
		cleanup = func() {} // already cleaned up
		return fmt.Errorf("POP3 DELE %d failed: %w", msgID, err)
	}

	// If using a temporary connection, QUIT commits the deletion.
	// If using a persistent connection, deletion is committed on Close().
	cleanup()
	return nil
}

// FetchMessageByID implements MailReceiver.
func (c *POP3Client) FetchMessageByID(_ string, uid uint32) (*Message, error) {
	return c.FetchMessage(uid)
}

// DeleteMessageByID implements MailReceiver.
func (c *POP3Client) DeleteMessageByID(_ string, uid uint32, _ bool) error {
	return c.DeleteMessage(uid)
}

// tlsConfig returns the TLS configuration to use. If none is set in the
// config, a sensible default with the server name is returned.
func (c *POP3Client) tlsConfig() *tls.Config {
	if c.config.TLSConfig != nil {
		cfg := c.config.TLSConfig.Clone()
		if cfg.ServerName == "" {
			cfg.ServerName = c.config.Host
		}
		return cfg
	}
	return &tls.Config{ServerName: c.config.Host}
}

// ListMessageIDs returns all message (id, size) pairs.
func (c *POP3Client) ListMessageIDs() ([]POP3MessageID, error) {
	cleanup, err := c.ensureConnected()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	return c.conn.list(0)
}

// dial establishes a new POP3 connection (TCP + TLS + AUTH).
func (c *POP3Client) dial() (*pop3Conn, error) {
	// Require encryption — refuse plaintext connections
	if !c.config.SSL && !c.config.StartTLS {
		return nil, fmt.Errorf("POP3 requires SSL or StartTLS; plaintext connections are not allowed")
	}

	addr := net.JoinHostPort(c.config.Host, fmt.Sprintf("%d", c.config.Port))

	var netConn net.Conn
	var err error

	dialer := &net.Dialer{Timeout: 10 * time.Second}

	if c.config.SSL {
		tlsCfg := c.tlsConfig()
		netConn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		netConn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("POP3 connection to %s failed: %w", addr, err)
	}

	// Set read/write deadline for the entire session (5 minutes).
	netConn.SetDeadline(time.Now().Add(5 * time.Minute))

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

	// Upgrade to TLS via STLS if needed
	if c.config.StartTLS && !c.config.SSL {
		if err := conn.send("STLS"); err != nil {
			netConn.Close()
			return nil, fmt.Errorf("POP3 STLS command failed: %w", err)
		}
		if _, err := conn.readOne(); err != nil {
			netConn.Close()
			return nil, fmt.Errorf("POP3 STLS negotiation failed: %w", err)
		}
		tlsConn := tls.Client(netConn, c.tlsConfig())
		if err := tlsConn.Handshake(); err != nil {
			netConn.Close()
			return nil, fmt.Errorf("POP3 TLS handshake failed: %w", err)
		}
		conn.conn = tlsConn
		conn.r = bufio.NewReader(tlsConn)
		conn.w = bufio.NewWriter(tlsConn)
		// Reset deadline on upgraded connection
		tlsConn.SetDeadline(time.Now().Add(5 * time.Minute))
	}

	// Authenticate
	if err := conn.auth(c.config.Username, c.config.Password); err != nil {
		conn.conn.Close()
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
// Args can be int or string - converted efficiently without fmt.Sprintf.
func (c *pop3Conn) cmd(cmd string, isMulti bool, args ...interface{}) (*bytes.Buffer, error) {
	cmdLine := cmd
	if len(args) > 0 {
		parts := make([]string, len(args))
		for i, a := range args {
			switch v := a.(type) {
			case int:
				parts[i] = strconv.Itoa(v)
			case string:
				parts[i] = v
			default:
				// Fallback for any other type (should not happen in current code)
				parts[i] = fmt.Sprintf("%v", v)
			}
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

const maxPOP3ResponseSize = 100 << 20 // 100MB maximum POP3 response size

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

		// Check response size limit to prevent OOM on malicious server
		if buf.Len() > maxPOP3ResponseSize {
			return nil, fmt.Errorf("POP3 response exceeds maximum size (%d bytes)", maxPOP3ResponseSize)
		}
	}
	return buf, nil
}

// auth authenticates with USER/PASS.
// PASS is sent directly via send()/readOne() instead of cmd() to avoid
// the password being captured in the cmdLine variable (defence-in-depth
// against accidental logging).
func (c *pop3Conn) auth(user, password string) error {
	if _, err := c.cmd("USER", false, user); err != nil {
		return err
	}
	// Send PASS directly to avoid password appearing in cmd()'s cmdLine string
	if err := c.send("PASS " + password); err != nil {
		return err
	}
	if _, err := c.readOne(); err != nil {
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
		return 0, 0, fmt.Errorf("POP3 STAT: unexpected response format")
	}
	count, err = strconv.Atoi(string(f[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("POP3 STAT: invalid count %q: %w", f[0], err)
	}
	size, err = strconv.Atoi(string(f[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("POP3 STAT: invalid size %q: %w", f[1], err)
	}
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
		id, err := strconv.Atoi(string(f[0]))
		if err != nil {
			continue // skip unparseable lines
		}
		sz, err := strconv.Atoi(string(f[1]))
		if err != nil {
			continue
		}
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
		id, err := strconv.Atoi(string(f[0]))
		if err != nil {
			continue
		}
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
