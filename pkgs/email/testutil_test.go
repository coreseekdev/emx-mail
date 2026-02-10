package email

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"strconv"
	"testing"
	"time"
)

// newTestTLSConfig generates a self-signed TLS config for mock servers.
func newTestTLSConfig(t *testing.T) *tls.Config {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"localhost", "127.0.0.1"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
}

// insecureTLSConfig returns a client-side TLS config that skips verification.
func insecureTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true}
}

// splitHostPort splits "host:port" into (host, int port).
func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

// testMailRFC822 is a minimal RFC 5322 message for testing.
const testMailRFC822 = "MIME-Version: 1.0\r\n" +
	"From: sender@example.com\r\n" +
	"To: rcpt@example.com\r\n" +
	"Subject: Test Subject\r\n" +
	"Date: Mon, 10 Feb 2026 08:00:00 +0000\r\n" +
	"Message-Id: <test-1@example.com>\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Hello, World!"

// testMailMultipart is a multipart/mixed message with text + attachment.
const testMailMultipart = "MIME-Version: 1.0\r\n" +
	"From: sender@example.com\r\n" +
	"To: rcpt@example.com\r\n" +
	"Subject: Multipart Test\r\n" +
	"Date: Mon, 10 Feb 2026 08:00:00 +0000\r\n" +
	"Message-Id: <test-multi@example.com>\r\n" +
	"Content-Type: multipart/mixed; boundary=\"TESTBOUNDARY\"\r\n" +
	"\r\n" +
	"--TESTBOUNDARY\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Plain text body\r\n" +
	"--TESTBOUNDARY\r\n" +
	"Content-Type: application/octet-stream\r\n" +
	"Content-Disposition: attachment; filename=\"test.bin\"\r\n" +
	"\r\n" +
	"BINARYDATA\r\n" +
	"--TESTBOUNDARY--\r\n"

// testMailNested is a multipart/mixed containing a multipart/alternative.
const testMailNested = "MIME-Version: 1.0\r\n" +
	"From: sender@example.com\r\n" +
	"To: rcpt@example.com\r\n" +
	"Subject: Nested Multipart\r\n" +
	"Date: Mon, 10 Feb 2026 08:00:00 +0000\r\n" +
	"Message-Id: <test-nested@example.com>\r\n" +
	"Content-Type: multipart/mixed; boundary=\"OUTER\"\r\n" +
	"\r\n" +
	"--OUTER\r\n" +
	"Content-Type: multipart/alternative; boundary=\"INNER\"\r\n" +
	"\r\n" +
	"--INNER\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Plain version\r\n" +
	"--INNER\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n" +
	"\r\n" +
	"<p>HTML version</p>\r\n" +
	"--INNER--\r\n" +
	"--OUTER\r\n" +
	"Content-Type: image/png\r\n" +
	"Content-Disposition: attachment; filename=\"image.png\"\r\n" +
	"\r\n" +
	"PNG-DATA\r\n" +
	"--OUTER--\r\n"
