package email

import (
	"net"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// ---------------------------------------------------------------------------
// IMAP mock server helper
// ---------------------------------------------------------------------------

const (
	imapTestUser = "testuser"
	imapTestPass = "testpass"
)

// newTestIMAPServer starts an in-memory IMAP server and returns the listen
// address.  Caller must eventually call srv.Close() (done via t.Cleanup).
func newTestIMAPServer(t *testing.T) (addr string, memSrv *imapmemserver.Server) {
	t.Helper()

	memSrv = imapmemserver.New()
	user := imapmemserver.NewUser(imapTestUser, imapTestPass)
	user.Create("INBOX", nil)
	memSrv.AddUser(user)

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(_ *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memSrv.NewSession(), nil, nil
		},
		InsecureAuth: true,
		Caps: imap.CapSet{
			imap.CapIMAP4rev1: {},
		},
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	return ln.Addr().String(), memSrv
}

// appendTestMail appends a raw RFC 5322 message to the given mailbox via
// a direct IMAP client (not through our wrapper).
func appendTestMail(t *testing.T, addr, mailbox, rawMsg string) {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	c := imapclient.New(conn, nil)
	if err := c.Login(imapTestUser, imapTestPass).Wait(); err != nil {
		t.Fatal(err)
	}

	appendCmd := c.Append(mailbox, int64(len(rawMsg)), nil)
	if _, err := appendCmd.Write([]byte(rawMsg)); err != nil {
		t.Fatal(err)
	}
	if err := appendCmd.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := appendCmd.Wait(); err != nil {
		t.Fatal(err)
	}
	c.Close()
}

// newIMAPTestClient creates an IMAPClient pointed at the test server.
func newIMAPTestClient(t *testing.T, addr string) *IMAPClient {
	t.Helper()
	host, port := splitHostPort(t, addr)
	client := NewIMAPClient(IMAPConfig{
		Host:     host,
		Port:     port,
		Username: imapTestUser,
		Password: imapTestPass,
	})
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestIMAPConnect(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	host, port := splitHostPort(t, addr)

	client := NewIMAPClient(IMAPConfig{
		Host:     host,
		Port:     port,
		Username: imapTestUser,
		Password: imapTestPass,
	})
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	defer client.Close()
}

func TestIMAPConnect_BadCredentials(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	host, port := splitHostPort(t, addr)

	client := NewIMAPClient(IMAPConfig{
		Host:     host,
		Port:     port,
		Username: "wrong",
		Password: "wrong",
	})
	if err := client.Connect(); err == nil {
		client.Close()
		t.Fatal("expected auth error, got nil")
	}
}

func TestIMAPListFolders(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	client := newIMAPTestClient(t, addr)

	folders, err := client.ListFolders()
	if err != nil {
		t.Fatalf("ListFolders() error: %v", err)
	}
	found := false
	for _, f := range folders {
		if f.Name == "INBOX" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected INBOX in folder list, got %v", folders)
	}
}

func TestIMAPFetchMessages_Empty(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	client := newIMAPTestClient(t, addr)

	result, err := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	if err != nil {
		t.Fatalf("FetchMessages() error: %v", err)
	}
	if len(result.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result.Messages))
	}
}

func TestIMAPFetchMessages_WithMail(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	appendTestMail(t, addr, "INBOX", testMailRFC822)

	client := newIMAPTestClient(t, addr)

	result, err := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	if err != nil {
		t.Fatalf("FetchMessages() error: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Subject != "Test Subject" {
		t.Errorf("unexpected subject: %q", result.Messages[0].Subject)
	}
	if result.Total != 1 {
		t.Errorf("expected Total=1, got %d", result.Total)
	}
}

func TestIMAPFetchMessage_ByUID(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	appendTestMail(t, addr, "INBOX", testMailRFC822)

	client := newIMAPTestClient(t, addr)

	// First list to get UID
	result, err := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) == 0 {
		t.Fatal("no messages")
	}
	uid := result.Messages[0].UID

	// Reconnect — FetchMessage calls ensureConnected
	client.Close()
	host, port := splitHostPort(t, addr)
	client2 := NewIMAPClient(IMAPConfig{
		Host:     host,
		Port:     port,
		Username: imapTestUser,
		Password: imapTestPass,
	})

	msg, err := client2.FetchMessage("INBOX", uid)
	if err != nil {
		t.Fatalf("FetchMessage() error: %v", err)
	}
	defer client2.Close()

	if msg.Subject != "Test Subject" {
		t.Errorf("unexpected subject: %q", msg.Subject)
	}
	if msg.TextBody == "" {
		t.Error("expected non-empty TextBody")
	}
}

func TestIMAPFetchMessage_Multipart(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	appendTestMail(t, addr, "INBOX", testMailMultipart)

	client := newIMAPTestClient(t, addr)

	result, _ := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	if len(result.Messages) == 0 {
		t.Fatal("no messages")
	}
	uid := result.Messages[0].UID

	client.Close()
	host, port := splitHostPort(t, addr)
	client2 := NewIMAPClient(IMAPConfig{
		Host: host, Port: port,
		Username: imapTestUser, Password: imapTestPass,
	})
	defer client2.Close()

	msg, err := client2.FetchMessage("INBOX", uid)
	if err != nil {
		t.Fatalf("FetchMessage() error: %v", err)
	}

	if msg.TextBody == "" {
		t.Error("expected non-empty TextBody in multipart")
	}
	if len(msg.Attachments) == 0 {
		t.Error("expected at least 1 attachment in multipart")
	}
}

func TestIMAPFetchMessage_NestedMultipart(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	appendTestMail(t, addr, "INBOX", testMailNested)

	client := newIMAPTestClient(t, addr)

	result, _ := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	uid := result.Messages[0].UID

	client.Close()
	host, port := splitHostPort(t, addr)
	c2 := NewIMAPClient(IMAPConfig{
		Host: host, Port: port,
		Username: imapTestUser, Password: imapTestPass,
	})
	defer c2.Close()

	msg, err := c2.FetchMessage("INBOX", uid)
	if err != nil {
		t.Fatal(err)
	}

	if msg.TextBody == "" {
		t.Error("expected text/plain body")
	}
	if msg.HTMLBody == "" {
		t.Error("expected text/html body")
	}
	if len(msg.Attachments) == 0 {
		t.Error("expected attachment in nested multipart")
	}
}

func TestIMAPDeleteMessage(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	appendTestMail(t, addr, "INBOX", testMailRFC822)

	client := newIMAPTestClient(t, addr)

	result, _ := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	if len(result.Messages) == 0 {
		t.Fatal("no messages to delete")
	}
	uid := result.Messages[0].UID

	if err := client.DeleteMessage("INBOX", uid, true); err != nil {
		t.Fatalf("DeleteMessage() error: %v", err)
	}

	// Verify deleted
	result2, err := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result2.Messages) != 0 {
		t.Errorf("expected 0 messages after delete, got %d", len(result2.Messages))
	}
}

func TestIMAPMarkAsSeen(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	appendTestMail(t, addr, "INBOX", testMailRFC822)

	client := newIMAPTestClient(t, addr)

	result, _ := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	uid := result.Messages[0].UID

	if err := client.MarkAsSeen("INBOX", uid); err != nil {
		t.Fatalf("MarkAsSeen() error: %v", err)
	}

	// Re-fetch to verify flag
	result2, _ := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	if !result2.Messages[0].Flags.Seen {
		t.Error("expected Seen flag after MarkAsSeen")
	}
}

func TestIMAPPing(t *testing.T) {
	addr, _ := newTestIMAPServer(t)
	client := newIMAPTestClient(t, addr)

	if err := client.Ping(); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestIMAPMailReceiver(t *testing.T) {
	// Compile-time check
	var _ MailReceiver = (*IMAPClient)(nil)

	addr, _ := newTestIMAPServer(t)
	appendTestMail(t, addr, "INBOX", testMailRFC822)

	host, port := splitHostPort(t, addr)
	var receiver MailReceiver = NewIMAPClient(IMAPConfig{
		Host: host, Port: port,
		Username: imapTestUser, Password: imapTestPass,
	})

	result, err := receiver.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 via MailReceiver, got %d", len(result.Messages))
	}

	uid := result.Messages[0].UID
	msg, err := receiver.FetchMessageByID("INBOX", uid)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("unexpected subject via MailReceiver: %q", msg.Subject)
	}

	if err := receiver.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIMAPMultipleMessages(t *testing.T) {
	addr, _ := newTestIMAPServer(t)

	// Append 3 messages
	for i := 0; i < 3; i++ {
		appendTestMail(t, addr, "INBOX", testMailRFC822)
	}

	client := newIMAPTestClient(t, addr)

	result, err := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result.Messages))
	}
	if result.Total != 3 {
		t.Errorf("expected Total=3, got %d", result.Total)
	}
}

func TestIMAPFetchMessages_WithLimit(t *testing.T) {
	addr, _ := newTestIMAPServer(t)

	for i := 0; i < 5; i++ {
		appendTestMail(t, addr, "INBOX", testMailRFC822)
	}

	client := newIMAPTestClient(t, addr)

	result, err := client.FetchMessages(FetchOptions{Folder: "INBOX", Limit: 2})
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
