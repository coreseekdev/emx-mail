package email

import (
	"strings"
	"testing"

	gomessage "github.com/emersion/go-message"
)

func parseTestEntity(t *testing.T, raw string) *gomessage.Entity {
	t.Helper()
	entity, err := gomessage.Read(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("failed to parse test entity: %v", err)
	}
	return entity
}

func TestParseEntityBody_PlainText(t *testing.T) {
	raw := "Content-Type: text/plain; charset=utf-8\r\n\r\nHello, World!"
	entity := parseTestEntity(t, raw)
	msg := &Message{}
	parseEntityBody(msg, entity)

	if msg.TextBody != "Hello, World!" {
		t.Errorf("unexpected TextBody: %q", msg.TextBody)
	}
	if msg.HTMLBody != "" {
		t.Errorf("unexpected HTMLBody: %q", msg.HTMLBody)
	}
}

func TestParseEntityBody_HTML(t *testing.T) {
	raw := "Content-Type: text/html; charset=utf-8\r\n\r\n<p>Hello</p>"
	entity := parseTestEntity(t, raw)
	msg := &Message{}
	parseEntityBody(msg, entity)

	if msg.HTMLBody != "<p>Hello</p>" {
		t.Errorf("unexpected HTMLBody: %q", msg.HTMLBody)
	}
}

func TestParseEntityBody_MultipartMixed(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"B1\"\r\n" +
		"\r\n" +
		"--B1\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"body text\r\n" +
		"--B1\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"doc.pdf\"\r\n\r\n" +
		"PDF-BYTES\r\n" +
		"--B1--\r\n"

	entity := parseTestEntity(t, raw)
	msg := &Message{}
	parseEntityBody(msg, entity)

	if msg.TextBody == "" {
		t.Error("expected non-empty TextBody")
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	if msg.Attachments[0].Filename != "doc.pdf" {
		t.Errorf("unexpected filename: %q", msg.Attachments[0].Filename)
	}
	if msg.Attachments[0].ContentType != "application/pdf" {
		t.Errorf("unexpected content-type: %q", msg.Attachments[0].ContentType)
	}
}

func TestParseEntityBody_MultipartAlternative(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"ALT\"\r\n" +
		"\r\n" +
		"--ALT\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"plain text\r\n" +
		"--ALT\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		"<b>html</b>\r\n" +
		"--ALT--\r\n"

	entity := parseTestEntity(t, raw)
	msg := &Message{}
	parseEntityBody(msg, entity)

	if msg.TextBody == "" {
		t.Error("expected non-empty TextBody")
	}
	if msg.HTMLBody == "" {
		t.Error("expected non-empty HTMLBody")
	}
}

func TestParseEntityBody_NestedMultipart(t *testing.T) {
	entity := parseTestEntity(t, testMailNested)
	msg := &Message{}
	parseEntityBody(msg, entity)

	if msg.TextBody == "" {
		t.Error("expected text/plain body in nested multipart")
	}
	if msg.HTMLBody == "" {
		t.Error("expected text/html body in nested multipart")
	}
	if len(msg.Attachments) == 0 {
		t.Error("expected attachment in nested multipart")
	}
}

func TestParseEntityBody_MultipleAttachments(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"MA\"\r\n" +
		"\r\n" +
		"--MA\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"text\r\n" +
		"--MA\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-Disposition: attachment; filename=\"a.png\"\r\n\r\n" +
		"PNG\r\n" +
		"--MA\r\n" +
		"Content-Type: application/zip\r\n" +
		"Content-Disposition: attachment; filename=\"b.zip\"\r\n\r\n" +
		"ZIP\r\n" +
		"--MA\r\n" +
		"Content-Type: text/csv\r\n" +
		"Content-Disposition: attachment; filename=\"c.csv\"\r\n\r\n" +
		"CSV\r\n" +
		"--MA--\r\n"

	entity := parseTestEntity(t, raw)
	msg := &Message{}
	parseEntityBody(msg, entity)

	if len(msg.Attachments) != 3 {
		t.Fatalf("expected 3 attachments, got %d", len(msg.Attachments))
	}
	names := make([]string, len(msg.Attachments))
	for i, a := range msg.Attachments {
		names[i] = a.Filename
	}
	expected := []string{"a.png", "b.zip", "c.csv"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("attachment[%d] filename = %q, want %q", i, names[i], want)
		}
	}
}

func TestParseEntityBody_EmptyBody(t *testing.T) {
	raw := "Content-Type: text/plain\r\n\r\n"
	entity := parseTestEntity(t, raw)
	msg := &Message{}
	parseEntityBody(msg, entity) // should not panic

	// Empty body is fine
}

func TestParseEntityBody_AttachmentSize(t *testing.T) {
	// Create a body with known-size attachment data
	payload := strings.Repeat("X", 4096)
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"SZ\"\r\n" +
		"\r\n" +
		"--SZ\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"hi\r\n" +
		"--SZ\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"big.dat\"\r\n\r\n" +
		payload + "\r\n" +
		"--SZ--\r\n"

	entity := parseTestEntity(t, raw)
	msg := &Message{}
	parseEntityBody(msg, entity)

	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	if msg.Attachments[0].Size != int64(len(payload)) {
		t.Errorf("attachment size = %d, want %d", msg.Attachments[0].Size, len(payload))
	}
	if len(msg.Attachments[0].Data) != len(payload) {
		t.Errorf("attachment data length = %d, want %d", len(msg.Attachments[0].Data), len(payload))
	}
}
