package email

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
)

// ---------------------------------------------------------------------------
// SMTP mock server
// ---------------------------------------------------------------------------

type smtpTestMessage struct {
	From string
	To   []string
	Data []byte
}

type smtpTestBackend struct {
	mu       sync.Mutex
	messages []*smtpTestMessage
}

func (be *smtpTestBackend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &smtpTestSession{backend: be}, nil
}

func (be *smtpTestBackend) Messages() []*smtpTestMessage {
	be.mu.Lock()
	defer be.mu.Unlock()
	return append([]*smtpTestMessage(nil), be.messages...)
}

type smtpTestSession struct {
	backend *smtpTestBackend
	msg     *smtpTestMessage
}

func (s *smtpTestSession) AuthMechanisms() []string { return []string{"PLAIN"} }

func (s *smtpTestSession) Auth(mech string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, username, password string) error {
		if username != "testuser" || password != "testpass" {
			return errors.New("invalid credentials")
		}
		return nil
	}), nil
}

func (s *smtpTestSession) Mail(from string, _ *gosmtp.MailOptions) error {
	s.msg = &smtpTestMessage{From: from}
	return nil
}

func (s *smtpTestSession) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	s.msg.To = append(s.msg.To, to)
	return nil
}

func (s *smtpTestSession) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.msg.Data = b
	s.backend.mu.Lock()
	s.backend.messages = append(s.backend.messages, s.msg)
	s.backend.mu.Unlock()
	return nil
}

func (s *smtpTestSession) Reset()        { s.msg = nil }
func (s *smtpTestSession) Logout() error { return nil }

// Ensure interface conformance
var _ gosmtp.AuthSession = (*smtpTestSession)(nil)

// newTestSMTPServer starts a mock SMTP server.  Returns the backend (to
// inspect received mail) and the listen address.
func newTestSMTPServer(t *testing.T) (*smtpTestBackend, string) {
	t.Helper()

	be := &smtpTestBackend{}
	srv := gosmtp.NewServer(be)
	srv.Domain = "localhost"
	srv.AllowInsecureAuth = true

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	return be, ln.Addr().String()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSMTPSend_PlainText(t *testing.T) {
	be, addr := newTestSMTPServer(t)
	host, port := splitHostPort(t, addr)

	client := NewSMTPClient(SMTPConfig{
		Host:     host,
		Port:     port,
		Username: "testuser",
		Password: "testpass",
	})

	err := client.Send(SendOptions{
		From:     Address{Name: "Sender", Email: "sender@example.com"},
		To:       []Address{{Name: "Recipient", Email: "rcpt@example.com"}},
		Subject:  "Test Subject",
		TextBody: "Hello, World!",
	})
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	msgs := be.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].From != "sender@example.com" {
		t.Errorf("unexpected From: %s", msgs[0].From)
	}
	if len(msgs[0].To) != 1 || msgs[0].To[0] != "rcpt@example.com" {
		t.Errorf("unexpected To: %v", msgs[0].To)
	}
	// Check Subject appears in raw data
	if !strings.Contains(string(msgs[0].Data), "Test Subject") {
		t.Error("subject not found in message data")
	}
}

func TestSMTPSend_HTMLBody(t *testing.T) {
	be, addr := newTestSMTPServer(t)
	host, port := splitHostPort(t, addr)

	client := NewSMTPClient(SMTPConfig{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
	})

	err := client.Send(SendOptions{
		From:     Address{Email: "sender@example.com"},
		To:       []Address{{Email: "rcpt@example.com"}},
		Subject:  "HTML",
		HTMLBody: "<p>Hello</p>",
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs := be.Messages()
	if !strings.Contains(string(msgs[0].Data), "text/html") {
		t.Error("expected text/html in message data")
	}
}

func TestSMTPSend_MultipleRecipients(t *testing.T) {
	be, addr := newTestSMTPServer(t)
	host, port := splitHostPort(t, addr)

	client := NewSMTPClient(SMTPConfig{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
	})

	err := client.Send(SendOptions{
		From: Address{Email: "sender@example.com"},
		To: []Address{
			{Email: "to1@example.com"},
			{Email: "to2@example.com"},
		},
		Cc:       []Address{{Email: "cc@example.com"}},
		Bcc:      []Address{{Email: "bcc@example.com"}},
		Subject:  "Multi",
		TextBody: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs := be.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	// SMTP RCPT TO should contain all recipients (To+Cc+Bcc)
	if len(msgs[0].To) != 4 {
		t.Errorf("expected 4 RCPT TO, got %d: %v", len(msgs[0].To), msgs[0].To)
	}
}

func TestSMTPSend_BadAuth(t *testing.T) {
	_, addr := newTestSMTPServer(t)
	host, port := splitHostPort(t, addr)

	client := NewSMTPClient(SMTPConfig{
		Host:     host,
		Port:     port,
		Username: "wrong",
		Password: "wrong",
	})

	err := client.Send(SendOptions{
		From:     Address{Email: "sender@example.com"},
		To:       []Address{{Email: "rcpt@example.com"}},
		Subject:  "fail",
		TextBody: "should fail",
	})
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
}

func TestSMTPSend_MessageIDPresent(t *testing.T) {
	be, addr := newTestSMTPServer(t)
	host, port := splitHostPort(t, addr)

	client := NewSMTPClient(SMTPConfig{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
	})

	err := client.Send(SendOptions{
		From:     Address{Email: "sender@example.com"},
		To:       []Address{{Email: "rcpt@example.com"}},
		Subject:  "MID Test",
		TextBody: "check message-id",
	})
	if err != nil {
		t.Fatal(err)
	}

	data := string(be.Messages()[0].Data)
	if !strings.Contains(data, "Message-Id: <") {
		t.Error("Message-Id header not found in sent message")
	}
	if !strings.Contains(data, "@example.com>") {
		t.Error("Message-Id does not contain sender domain")
	}
}

func TestSMTPSend_Reply(t *testing.T) {
	be, addr := newTestSMTPServer(t)
	host, port := splitHostPort(t, addr)

	client := NewSMTPClient(SMTPConfig{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
	})

	err := client.Send(SendOptions{
		From:       Address{Email: "sender@example.com"},
		To:         []Address{{Email: "rcpt@example.com"}},
		Subject:    "Re: Original",
		TextBody:   "reply body",
		InReplyTo:  "<original@example.com>",
		References: []string{"<original@example.com>"},
	})
	if err != nil {
		t.Fatal(err)
	}

	data := string(be.Messages()[0].Data)
	if !strings.Contains(data, "In-Reply-To") {
		t.Error("In-Reply-To header not found")
	}
	if !strings.Contains(data, "References") {
		t.Error("References header not found")
	}
}

func TestSMTPGenerateMessageID(t *testing.T) {
	id := GenerateMessageID("user@example.com")

	if id == "" {
		t.Fatal("empty message ID")
	}
	if id[0] != '<' || id[len(id)-1] != '>' {
		t.Errorf("missing angle brackets: %s", id)
	}
	if !strings.Contains(id, "@example.com") {
		t.Errorf("missing domain: %s", id)
	}
}

func TestSMTPGenerateMessageID_DifferentDomains(t *testing.T) {
	tests := []struct {
		email  string
		domain string
	}{
		{"user@gmail.com", "@gmail.com"},
		{"admin@corp.co.uk", "@corp.co.uk"},
		{"nodomain", "@localhost"},
	}

	for _, tc := range tests {
		id := GenerateMessageID(tc.email)
		if !strings.Contains(id, tc.domain) {
			t.Errorf("GenerateMessageID(%q) = %q, want domain %q", tc.email, id, tc.domain)
		}
	}
}

func TestSMTPGenerateMessageID_Uniqueness(t *testing.T) {
	ids := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id := GenerateMessageID("user@example.com")
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate ID: %s", id)
		}
		ids[id] = struct{}{}
	}
}

func TestSMTPClose(t *testing.T) {
	_, addr := newTestSMTPServer(t)
	host, port := splitHostPort(t, addr)

	client := NewSMTPClient(SMTPConfig{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
	})
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	// Second close should be fine
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}
