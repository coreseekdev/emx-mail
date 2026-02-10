package email

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// POP3 mock server (raw TCP, RFC 1939)
// ---------------------------------------------------------------------------

type pop3MockMsg struct {
	ID   int
	UIDL string
	Data string // RFC 5322 raw
}

type pop3MockOpts struct {
	Messages    []pop3MockMsg
	UseTLS      bool // implicit TLS (POP3S)
	SupportSTLS bool // advertise and handle STLS
	RejectAuth  bool
}

func newTestPOP3Server(t *testing.T, opts pop3MockOpts) string {
	t.Helper()

	var tlsConfig *tls.Config
	if opts.UseTLS || opts.SupportSTLS {
		tlsConfig = newTestTLSConfig(t)
	}

	var ln net.Listener
	var err error
	if opts.UseTLS {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go handlePOP3MockConn(raw, opts, tlsConfig)
		}
	}()

	return ln.Addr().String()
}

func handlePOP3MockConn(conn net.Conn, opts pop3MockOpts, tlsCfg *tls.Config) {
	defer conn.Close()

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	writeLine := func(s string) {
		fmt.Fprintf(rw, "%s\r\n", s)
		rw.Flush()
	}

	writeLine("+OK POP3 server ready")

	authed := false
	deleted := map[int]bool{}

	for {
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		cmd := strings.ToUpper(fields[0])

		switch cmd {
		case "CAPA":
			writeLine("+OK")
			if opts.SupportSTLS {
				writeLine("STLS")
			}
			writeLine("UIDL")
			writeLine("TOP")
			writeLine(".")

		case "STLS":
			if !opts.SupportSTLS || tlsCfg == nil {
				writeLine("-ERR STLS not supported")
				continue
			}
			writeLine("+OK Begin TLS")
			rw.Flush()
			tlsConn := tls.Server(conn, tlsCfg)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			rw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

		case "USER":
			writeLine("+OK")

		case "PASS":
			if opts.RejectAuth {
				writeLine("-ERR auth failed")
				continue
			}
			authed = true
			writeLine("+OK Logged in")

		case "NOOP":
			if !authed {
				writeLine("-ERR not authenticated")
				continue
			}
			writeLine("+OK")

		case "STAT":
			if !authed {
				writeLine("-ERR not authenticated")
				continue
			}
			total := 0
			totalSize := 0
			for _, m := range opts.Messages {
				if !deleted[m.ID] {
					total++
					totalSize += len(m.Data)
				}
			}
			writeLine(fmt.Sprintf("+OK %d %d", total, totalSize))

		case "LIST":
			if !authed {
				writeLine("-ERR not authenticated")
				continue
			}
			if len(fields) > 1 {
				// single-message LIST
				idx := 0
				fmt.Sscanf(fields[1], "%d", &idx)
				for _, m := range opts.Messages {
					if m.ID == idx && !deleted[idx] {
						writeLine(fmt.Sprintf("+OK %d %d", m.ID, len(m.Data)))
						goto listDone
					}
				}
				writeLine("-ERR no such message")
			listDone:
			} else {
				writeLine("+OK")
				for _, m := range opts.Messages {
					if !deleted[m.ID] {
						writeLine(fmt.Sprintf("%d %d", m.ID, len(m.Data)))
					}
				}
				writeLine(".")
			}

		case "UIDL":
			if !authed {
				writeLine("-ERR not authenticated")
				continue
			}
			writeLine("+OK")
			for _, m := range opts.Messages {
				if !deleted[m.ID] {
					uid := m.UIDL
					if uid == "" {
						uid = fmt.Sprintf("msg-%d", m.ID)
					}
					writeLine(fmt.Sprintf("%d %s", m.ID, uid))
				}
			}
			writeLine(".")

		case "RETR":
			if !authed {
				writeLine("-ERR not authenticated")
				continue
			}
			idx := 0
			if len(fields) > 1 {
				fmt.Sscanf(fields[1], "%d", &idx)
			}
			if idx < 1 || idx > len(opts.Messages) || deleted[idx] {
				writeLine("-ERR no such message")
				continue
			}
			writeLine("+OK")
			// Write message data line by line
			for _, dataLine := range strings.Split(opts.Messages[idx-1].Data, "\r\n") {
				// Byte-stuff lines starting with "."
				if strings.HasPrefix(dataLine, ".") {
					writeLine("." + dataLine)
				} else {
					writeLine(dataLine)
				}
			}
			writeLine(".")

		case "TOP":
			if !authed {
				writeLine("-ERR not authenticated")
				continue
			}
			idx, numLines := 0, 0
			if len(fields) > 1 {
				fmt.Sscanf(fields[1], "%d", &idx)
			}
			if len(fields) > 2 {
				fmt.Sscanf(fields[2], "%d", &numLines)
			}
			if idx < 1 || idx > len(opts.Messages) {
				writeLine("-ERR no such message")
				continue
			}
			writeLine("+OK")
			parts := strings.SplitN(opts.Messages[idx-1].Data, "\r\n\r\n", 2)
			// Headers
			for _, hl := range strings.Split(parts[0], "\r\n") {
				writeLine(hl)
			}
			writeLine("") // empty line between headers and body
			if len(parts) > 1 && numLines > 0 {
				bodyLines := strings.Split(parts[1], "\r\n")
				for i := 0; i < numLines && i < len(bodyLines); i++ {
					writeLine(bodyLines[i])
				}
			}
			writeLine(".")

		case "DELE":
			if !authed {
				writeLine("-ERR not authenticated")
				continue
			}
			idx := 0
			if len(fields) > 1 {
				fmt.Sscanf(fields[1], "%d", &idx)
			}
			deleted[idx] = true
			writeLine("+OK")

		case "QUIT":
			writeLine("+OK Bye")
			return

		default:
			writeLine("-ERR unknown command")
		}
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPOP3Connect_SSL(t *testing.T) {
	addr := newTestPOP3Server(t, pop3MockOpts{
		UseTLS: true,
		Messages: []pop3MockMsg{
			{ID: 1, UIDL: "u1", Data: testMailRFC822},
		},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host:      host,
		Port:      port,
		Username:  "testuser",
		Password:  "testpass",
		SSL:       true,
		TLSConfig: insecureTLSConfig(),
	})

	result, err := client.FetchMessages(FetchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("FetchMessages() via SSL error: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
}

func TestPOP3Connect_STARTTLS(t *testing.T) {
	addr := newTestPOP3Server(t, pop3MockOpts{
		SupportSTLS: true,
		Messages: []pop3MockMsg{
			{ID: 1, UIDL: "u1", Data: testMailRFC822},
		},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host:      host,
		Port:      port,
		Username:  "testuser",
		Password:  "testpass",
		StartTLS:  true,
		TLSConfig: insecureTLSConfig(),
	})

	result, err := client.FetchMessages(FetchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("FetchMessages() via STARTTLS error: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
}

func TestPOP3Connect_Plaintext_Rejected(t *testing.T) {
	// Server is available, but client should refuse plaintext
	addr := newTestPOP3Server(t, pop3MockOpts{
		Messages: []pop3MockMsg{
			{ID: 1, Data: testMailRFC822},
		},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host:     host,
		Port:     port,
		Username: "testuser",
		Password: "testpass",
		SSL:      false,
		StartTLS: false,
	})

	_, err := client.FetchMessages(FetchOptions{Limit: 10})
	if err == nil {
		t.Fatal("expected error for plaintext POP3, got nil")
	}
	if !strings.Contains(err.Error(), "SSL") && !strings.Contains(err.Error(), "encryption") &&
		!strings.Contains(err.Error(), "StartTLS") && !strings.Contains(err.Error(), "plaintext") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPOP3Connect_BadAuth(t *testing.T) {
	addr := newTestPOP3Server(t, pop3MockOpts{
		UseTLS:     true,
		RejectAuth: true,
		Messages:   []pop3MockMsg{{ID: 1, Data: testMailRFC822}},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host:      host,
		Port:      port,
		Username:  "testuser",
		Password:  "testpass",
		SSL:       true,
		TLSConfig: insecureTLSConfig(),
	})

	_, err := client.FetchMessages(FetchOptions{Limit: 10})
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
}

func TestPOP3FetchMessages(t *testing.T) {
	addr := newTestPOP3Server(t, pop3MockOpts{
		UseTLS: true,
		Messages: []pop3MockMsg{
			{ID: 1, UIDL: "uid-1", Data: testMailRFC822},
			{ID: 2, UIDL: "uid-2", Data: testMailRFC822},
			{ID: 3, UIDL: "uid-3", Data: testMailRFC822},
		},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
		SSL: true, TLSConfig: insecureTLSConfig(),
	})

	result, err := client.FetchMessages(FetchOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 {
		t.Errorf("expected Total=3, got %d", result.Total)
	}
	if len(result.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result.Messages))
	}
}

func TestPOP3FetchMessage_Single(t *testing.T) {
	addr := newTestPOP3Server(t, pop3MockOpts{
		UseTLS: true,
		Messages: []pop3MockMsg{
			{ID: 1, UIDL: "uid-1", Data: testMailRFC822},
		},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
		SSL: true, TLSConfig: insecureTLSConfig(),
	})

	msg, err := client.FetchMessage(1)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("unexpected subject: %q", msg.Subject)
	}
	if msg.TextBody == "" {
		t.Error("expected non-empty TextBody")
	}
}

func TestPOP3FetchMessage_Multipart(t *testing.T) {
	addr := newTestPOP3Server(t, pop3MockOpts{
		UseTLS: true,
		Messages: []pop3MockMsg{
			{ID: 1, UIDL: "uid-mp", Data: testMailMultipart},
		},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
		SSL: true, TLSConfig: insecureTLSConfig(),
	})

	msg, err := client.FetchMessage(1)
	if err != nil {
		t.Fatal(err)
	}
	if msg.TextBody == "" {
		t.Error("expected non-empty TextBody in multipart")
	}
	if len(msg.Attachments) == 0 {
		t.Error("expected attachment in multipart")
	}
}

func TestPOP3DeleteMessage(t *testing.T) {
	addr := newTestPOP3Server(t, pop3MockOpts{
		UseTLS: true,
		Messages: []pop3MockMsg{
			{ID: 1, UIDL: "uid-del", Data: testMailRFC822},
		},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
		SSL: true, TLSConfig: insecureTLSConfig(),
	})

	err := client.DeleteMessage(1)
	if err != nil {
		t.Fatalf("DeleteMessage() error: %v", err)
	}
}

func TestPOP3ListMessageIDs(t *testing.T) {
	addr := newTestPOP3Server(t, pop3MockOpts{
		UseTLS: true,
		Messages: []pop3MockMsg{
			{ID: 1, UIDL: "uid-1", Data: testMailRFC822},
			{ID: 2, UIDL: "uid-2", Data: testMailRFC822},
		},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
		SSL: true, TLSConfig: insecureTLSConfig(),
	})

	ids, err := client.ListMessageIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}
}

func TestPOP3MailReceiver(t *testing.T) {
	// Compile-time check
	var _ MailReceiver = (*POP3Client)(nil)

	addr := newTestPOP3Server(t, pop3MockOpts{
		UseTLS: true,
		Messages: []pop3MockMsg{
			{ID: 1, UIDL: "uid-mr", Data: testMailRFC822},
		},
	})
	host, port := splitHostPort(t, addr)

	var receiver MailReceiver = NewPOP3Client(POP3Config{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
		SSL: true, TLSConfig: insecureTLSConfig(),
	})

	result, err := receiver.FetchMessages(FetchOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 via MailReceiver, got %d", len(result.Messages))
	}

	msg, err := receiver.FetchMessageByID("", 1)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("unexpected subject: %q", msg.Subject)
	}

	if err := receiver.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPOP3FetchMessages_WithLimit(t *testing.T) {
	addr := newTestPOP3Server(t, pop3MockOpts{
		UseTLS: true,
		Messages: []pop3MockMsg{
			{ID: 1, UIDL: "u1", Data: testMailRFC822},
			{ID: 2, UIDL: "u2", Data: testMailRFC822},
			{ID: 3, UIDL: "u3", Data: testMailRFC822},
			{ID: 4, UIDL: "u4", Data: testMailRFC822},
			{ID: 5, UIDL: "u5", Data: testMailRFC822},
		},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
		SSL: true, TLSConfig: insecureTLSConfig(),
	})

	result, err := client.FetchMessages(FetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Errorf("expected 2 messages (limit), got %d", len(result.Messages))
	}
	if result.Total != 5 {
		t.Errorf("expected Total=5, got %d", result.Total)
	}
}
