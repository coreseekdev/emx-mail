package email

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// SMTPClient represents an SMTP client
type SMTPClient struct {
	config SMTPConfig
	client *smtp.Client
}

// SMTPConfig holds SMTP configuration
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	SSL      bool
	StartTLS bool
}

// NewSMTPClient creates a new SMTP client
func NewSMTPClient(config SMTPConfig) *SMTPClient {
	return &SMTPClient{
		config: config,
	}
}

// Connect establishes a connection to the SMTP server
func (c *SMTPClient) Connect() error {
	var dialFn func(addr string, tlsConfig *tls.Config) (*smtp.Client, error)

	tlsCfg := &tls.Config{ServerName: c.config.Host}

	if c.config.SSL {
		dialFn = smtp.DialTLS
	} else if c.config.StartTLS {
		dialFn = smtp.DialStartTLS
	} else {
		dialFn = func(addr string, tlsConfig *tls.Config) (*smtp.Client, error) {
			return smtp.Dial(addr)
		}
	}

	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	client, err := dialFn(addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}

	// Authenticate
	if c.config.Password != "" {
		auth := sasl.NewPlainClient("", c.config.Username, c.config.Password)
		if err := client.Auth(auth); err != nil {
			client.Close()
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	c.client = client
	return nil
}

// Send sends an email
func (c *SMTPClient) Send(opts SendOptions) error {
	if c.client == nil {
		if err := c.Connect(); err != nil {
			return err
		}
		defer c.Close()
	}

	// Build email message
	msg, err := c.buildMessage(opts)
	if err != nil {
		return fmt.Errorf("failed to build message: %w", err)
	}

	// Extract recipients
	recipients := make([]string, 0, len(opts.To)+len(opts.Cc)+len(opts.Bcc))
	for _, addr := range opts.To {
		recipients = append(recipients, addr.Email)
	}
	for _, addr := range opts.Cc {
		recipients = append(recipients, addr.Email)
	}
	for _, addr := range opts.Bcc {
		recipients = append(recipients, addr.Email)
	}

	// Send email
	from := opts.From.Email
	if err := c.client.SendMail(from, recipients, msg); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// buildMessage builds an email message from SendOptions
func (c *SMTPClient) buildMessage(opts SendOptions) (*bytes.Buffer, error) {
	var buf bytes.Buffer

	var header mail.Header
	header.SetDate(time.Now())
	header.SetSubject(opts.Subject)
	header.SetAddressList("From", []*mail.Address{{
		Name:    opts.From.Name,
		Address: opts.From.Email,
	}})

	if len(opts.To) > 0 {
		toAddrs := make([]*mail.Address, len(opts.To))
		for i, addr := range opts.To {
			toAddrs[i] = &mail.Address{
				Name:    addr.Name,
				Address: addr.Email,
			}
		}
		header.SetAddressList("To", toAddrs)
	}

	if len(opts.Cc) > 0 {
		ccAddrs := make([]*mail.Address, len(opts.Cc))
		for i, addr := range opts.Cc {
			ccAddrs[i] = &mail.Address{
				Name:    addr.Name,
				Address: addr.Email,
			}
		}
		header.SetAddressList("Cc", ccAddrs)
	}

	// Handle reply and references
	if opts.InReplyTo != "" {
		header.SetMsgIDList("In-Reply-To", []string{opts.InReplyTo})
	}
	if len(opts.References) > 0 {
		header.SetMsgIDList("References", opts.References)
	}

	// Generate Message-ID
	if opts.InReplyTo == "" {
		header.Set("Message-ID", GenerateMessageID(opts.From.Email))
	}

	// Create multipart writer
	var mw *mail.Writer
	var iw *mail.InlineWriter
	var err error

	if len(opts.Attachments) == 0 {
		// Simple inline message
		iw, err = mail.CreateInlineWriter(&buf, header)
		if err != nil {
			return nil, err
		}
	} else {
		// Multipart message with attachments
		mw, err = mail.CreateWriter(&buf, header)
		if err != nil {
			return nil, err
		}

		iw, err = mw.CreateInline()
		if err != nil {
			return nil, err
		}
	}

	// Add text body
	if opts.TextBody != "" {
		var h mail.InlineHeader
		h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		w, err := iw.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(opts.TextBody)); err != nil {
			return nil, err
		}
		w.Close()
	}

	// Add HTML body
	if opts.HTMLBody != "" {
		var h mail.InlineHeader
		h.SetContentType("text/html", map[string]string{"charset": "utf-8"})
		w, err := iw.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(opts.HTMLBody)); err != nil {
			return nil, err
		}
		w.Close()
	}

	if err := iw.Close(); err != nil {
		return nil, err
	}

	// Add attachments
	if mw != nil {
		for _, att := range opts.Attachments {
			if err := func() error {
				var h mail.AttachmentHeader
				h.SetFilename(att.Filename)
				h.SetContentType("application/octet-stream", nil)

				w, err := mw.CreateAttachment(h)
				if err != nil {
					return err
				}

				f, err := os.Open(att.Path)
				if err != nil {
					return fmt.Errorf("failed to open attachment %s: %w", att.Path, err)
				}
				defer f.Close()

				if _, err := io.Copy(w, f); err != nil {
					return fmt.Errorf("failed to copy attachment %s: %w", att.Path, err)
				}
				return w.Close()
			}(); err != nil {
				return nil, err
			}
		}

		if err := mw.Close(); err != nil {
			return nil, err
		}
	}

	return &buf, nil
}

// Close closes the SMTP connection
func (c *SMTPClient) Close() error {
	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}

// SendQuick sends an email with a simple configuration (helper function)
func SendQuickSMTP(host string, port int, username, password string, useSSL bool, opts SendOptions) error {
	client := NewSMTPClient(SMTPConfig{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		SSL:      useSSL,
	})

	return client.Send(opts)
}

// GenerateMessageID produces a RFC 5322 compliant Message-ID using the
// domain extracted from the sender's email address.
// Format: <timestamp.random@domain>
func GenerateMessageID(fromEmail string) string {
	domain := "localhost"
	if idx := strings.Index(fromEmail, "@"); idx >= 0 {
		domain = fromEmail[idx+1:]
	}

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	randomPart := hex.EncodeToString(b)

	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), randomPart, domain)
}
