# 单元测试方案 — IMAP / SMTP / POP3

日期: 2026-02-10

---

## 总体原则

| 原则 | 说明 |
|------|------|
| **Mock 服务器** | 每个协议使用进程内 mock 服务器，不依赖外部邮件服务 |
| **端口分配** | 全部使用 `localhost:0`，OS 自动分配空闲端口 |
| **TLS 证书** | 在 `pkgs/email/testutil_test.go` 中嵌入自签名 RSA 证书/密钥 pair，三个协议复用 |
| **无外部依赖** | IMAP / SMTP 直接复用项目已有的 `go-imap/v2` 和 `go-smtp` 服务端包；POP3 使用原生 TCP mock |
| **包内黑盒** | 测试文件放在 `pkgs/email/` 下（`package email`），直接调用公开 API |

---

## 0. 共享测试基础设施

### 文件: `pkgs/email/testutil_test.go`

```go
package email

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"testing"
	"time"
)

// newTestTLSConfig 返回一个自签名证书的 tls.Config，供 IMAP/SMTP/POP3 mock 服务器使用。
func newTestTLSConfig(t *testing.T) *tls.Config {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"localhost"},
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

// newInsecureTLSConfig 返回跳过验证的客户端 tls.Config。
func newInsecureTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true}
}
```

---

## 1. IMAP 测试

### Mock 服务器

使用已有依赖 `github.com/emersion/go-imap/v2/imapserver/imapmemserver`，一个全内存的
IMAP 服务器实现。

### 文件: `pkgs/email/imap_test.go`

```go
package email

import (
	"net"
	"testing"

	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// newTestIMAPServer 启动一个内存 IMAP 服务器，返回地址和关闭函数。
func newTestIMAPServer(t *testing.T, withTLS bool) (addr string, cleanup func()) {
	t.Helper()

	memSrv := imapmemserver.New()
	user := memSrv.NewUser("testuser", "testpass")
	user.Create("INBOX", nil)

	opts := &imapserver.Options{
		NewSession: memSrv.NewSession,
		InsecureAuth: !withTLS,
	}
	if withTLS {
		opts.TLSConfig = newTestTLSConfig(t)
	}

	srv := imapserver.New(opts)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	go srv.Serve(ln)
	t.Cleanup(func() {
		srv.Close()
		ln.Close()
	})

	return ln.Addr().String(), func() { srv.Close(); ln.Close() }
}
```

### 测试用例

| # | 测试函数名 | 场景 | 关键断言 |
|---|-----------|------|---------|
| 1 | `TestIMAPConnect_Plain` | 明文连接+登录 | `client.Connect()` 成功 |
| 2 | `TestIMAPConnect_TLS` | 隐式 TLS 连接 | 连接成功，`tls.ConnectionState().HandshakeComplete` |
| 3 | `TestIMAPConnect_STARTTLS` | STARTTLS 升级 | 升级后认证成功 |
| 4 | `TestIMAPConnect_BadCredentials` | 错误密码 | 返回 auth error |
| 5 | `TestIMAPListFolders` | 列出文件夹 | 返回 `[]Folder` 包含 "INBOX" |
| 6 | `TestIMAPFetchMessages_Empty` | 空收件箱 LIST | `ListResult.Messages` 长度 0 |
| 7 | `TestIMAPFetchMessages_WithMail` | 先 APPEND 一封邮件再 LIST | 长度 1，Subject/From 匹配 |
| 8 | `TestIMAPFetchMessage_ByUID` | 按 UID 获取完整邮件 | TextBody 不为空 |
| 9 | `TestIMAPFetchMessage_Multipart` | APPEND multipart/mixed 邮件 | TextBody + HTMLBody + Attachment 都正确 |
| 10 | `TestIMAPFetchMessage_NestedMultipart` | multipart/mixed 嵌套 multipart/alternative | TextBody 取 text/plain |
| 11 | `TestIMAPDeleteMessage` | 删除 + EXPUNGE | 再次 LIST 长度 0 |
| 12 | `TestIMAPMarkAsSeen` | 标记已读 | Flags 包含 \Seen |
| 13 | `TestIMAPMailReceiver` | 通过 `MailReceiver` 接口调用 | 验证接口适配正确 |

### 示例核心实现

```go
func TestIMAPFetchMessages_WithMail(t *testing.T) {
	addr, _ := newTestIMAPServer(t, false)
	host, port := splitHostPort(t, addr)

	client := NewIMAPClient(IMAPConfig{
		Host:     host,
		Port:     port,
		Username: "testuser",
		Password: "testpass",
	})
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// 通过 APPEND 注入一封测试邮件
	appendTestMail(t, addr, "INBOX", testMailRFC822)

	result, err := client.FetchMessages(FetchOptions{
		Folder: "INBOX",
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Subject != "Test Subject" {
		t.Errorf("unexpected subject: %s", result.Messages[0].Subject)
	}
}
```

> **注意**: `imapmemserver` 要求显式创建 INBOX (`user.Create("INBOX", nil)`)，否则 SELECT
> 会报错。

---

## 2. SMTP 测试

### Mock 服务器

使用已有依赖 `github.com/emersion/go-smtp` 的服务端 API。实现一个最小化的
`smtp.Backend` + `smtp.Session`，将收到的邮件存入内存切片。

### 文件: `pkgs/email/smtp_test.go`

```go
package email

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

type testMessage struct {
	From string
	To   []string
	Data []byte
}

type testSMTPBackend struct {
	mu       sync.Mutex
	messages []*testMessage
}

func (be *testSMTPBackend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &testSMTPSession{backend: be}, nil
}

func (be *testSMTPBackend) Messages() []*testMessage {
	be.mu.Lock()
	defer be.mu.Unlock()
	return append([]*testMessage(nil), be.messages...)
}

type testSMTPSession struct {
	backend *testSMTPBackend
	msg     *testMessage
}

func (s *testSMTPSession) AuthMechanisms() []string { return []string{"PLAIN"} }
func (s *testSMTPSession) Auth(mech string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, username, password string) error {
		if username != "testuser" || password != "testpass" {
			return errors.New("invalid credentials")
		}
		return nil
	}), nil
}
func (s *testSMTPSession) Mail(from string, _ *smtp.MailOptions) error {
	s.msg = &testMessage{From: from}
	return nil
}
func (s *testSMTPSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.msg.To = append(s.msg.To, to)
	return nil
}
func (s *testSMTPSession) Data(r io.Reader) error {
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
func (s *testSMTPSession) Reset()        { s.msg = nil }
func (s *testSMTPSession) Logout() error { return nil }

// newTestSMTPServer 启动一个内存 SMTP 服务器。
func newTestSMTPServer(t *testing.T, withTLS bool) (*testSMTPBackend, string) {
	t.Helper()

	be := &testSMTPBackend{}
	srv := smtp.NewServer(be)
	srv.Domain = "localhost"
	srv.AllowInsecureAuth = !withTLS

	if withTLS {
		srv.TLSConfig = newTestTLSConfig(t)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close(); ln.Close() })

	return be, ln.Addr().String()
}
```

### 测试用例

| # | 测试函数名 | 场景 | 关键断言 |
|---|-----------|------|---------|
| 1 | `TestSMTPSend_Plain` | 纯文本邮件发送 | backend 收到 1 封邮件，From/To/Subject 正确 |
| 2 | `TestSMTPSend_WithAttachment` | 带附件发送 | Data 包含 multipart boundary 和附件内容 |
| 3 | `TestSMTPSend_MultipleRecipients` | To + Cc + Bcc | backend 收到多个 RCPT TO |
| 4 | `TestSMTPSend_TLS` | 启用 SSL/TLS | 连接成功，邮件送达 |
| 5 | `TestSMTPSend_STARTTLS` | 使用 STARTTLS | 升级后发送成功 |
| 6 | `TestSMTPSend_BadAuth` | 错误凭证 | 返回 error |
| 7 | `TestSMTPGenerateMessageID` | `GenerateMessageID(email)` | 格式 `<timestamp.hex@domain>`，无空字符串 |
| 8 | `TestSMTPGenerateMessageID_DifferentDomains` | 不同 email 域名 | 提取的 domain 正确 |
| 9 | `TestSMTPSend_MessageIDPresent` | 发送后检查 Data | 包含 `Message-Id: <...@domain>` header |

### 示例核心实现

```go
func TestSMTPSend_Plain(t *testing.T) {
	be, addr := newTestSMTPServer(t, false)
	host, port := splitHostPort(t, addr)

	client := NewSMTPClient(SMTPConfig{
		Host:     host,
		Port:     port,
		Username: "testuser",
		Password: "testpass",
	})
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect()

	err := client.Send(SendOptions{
		From:    Address{Name: "Test Sender", Email: "sender@example.com"},
		To:      []Address{{Name: "Recipient", Email: "rcpt@example.com"}},
		Subject: "Test Subject",
		Body:    "Hello, World!",
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs := be.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].From != "sender@example.com" {
		t.Errorf("unexpected From: %s", msgs[0].From)
	}
}

func TestSMTPGenerateMessageID(t *testing.T) {
	id := GenerateMessageID("user@example.com")
	if id == "" {
		t.Fatal("empty ID")
	}
	if !strings.Contains(id, "@example.com") {
		t.Errorf("missing domain in ID: %s", id)
	}
	if id[0] != '<' || id[len(id)-1] != '>' {
		t.Errorf("missing angle brackets: %s", id)
	}
}
```

---

## 3. POP3 测试

### Mock 服务器

POP3 协议简单 (RFC 1939)，不需要第三方服务端包。直接用 `net.Listen` + goroutine 实现
行协议状态机。

### 文件: `pkgs/email/pop3_test.go`

```go
package email

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"testing"
)

type pop3MockMessage struct {
	ID   int
	UIDL string
	Data string // RFC 822 格式
}

type pop3MockOpts struct {
	Messages  []pop3MockMessage
	TLS       bool   // 隐式 TLS (POP3S)
	STLS      bool   // STARTTLS 支持
	RejectAuth bool  // 拒绝认证
}

// newTestPOP3Server 启动一个 mock POP3 服务器。
func newTestPOP3Server(t *testing.T, opts pop3MockOpts) string {
	t.Helper()

	var tlsConfig *tls.Config
	if opts.TLS || opts.STLS {
		tlsConfig = newTestTLSConfig(t)
	}

	var ln net.Listener
	var err error
	if opts.TLS {
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
			go handlePOP3Conn(raw, opts, tlsConfig)
		}
	}()

	return ln.Addr().String()
}

func handlePOP3Conn(conn net.Conn, opts pop3MockOpts, tlsConfig *tls.Config) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	fmt.Fprintf(conn, "+OK POP3 server ready\r\n")

	authed := false
	deleted := map[int]bool{}

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		cmd := strings.ToUpper(fields[0])

		switch cmd {
		case "STLS":
			if !opts.STLS || tlsConfig == nil {
				fmt.Fprintf(conn, "-ERR STLS not supported\r\n")
				continue
			}
			fmt.Fprintf(conn, "+OK Begin TLS\r\n")
			tlsConn := tls.Server(conn, tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			scanner = bufio.NewScanner(conn)

		case "CAPA":
			fmt.Fprintf(conn, "+OK\r\n")
			if opts.STLS {
				fmt.Fprintf(conn, "STLS\r\n")
			}
			fmt.Fprintf(conn, "UIDL\r\n")
			fmt.Fprintf(conn, ".\r\n")

		case "USER":
			fmt.Fprintf(conn, "+OK\r\n")

		case "PASS":
			if opts.RejectAuth {
				fmt.Fprintf(conn, "-ERR auth failed\r\n")
				continue
			}
			authed = true
			fmt.Fprintf(conn, "+OK Logged in\r\n")

		case "STAT":
			if !authed {
				fmt.Fprintf(conn, "-ERR not authenticated\r\n")
				continue
			}
			total := 0
			for _, m := range opts.Messages {
				if !deleted[m.ID] {
					total++
				}
			}
			fmt.Fprintf(conn, "+OK %d 0\r\n", total)

		case "LIST":
			fmt.Fprintf(conn, "+OK\r\n")
			for _, m := range opts.Messages {
				if !deleted[m.ID] {
					fmt.Fprintf(conn, "%d %d\r\n", m.ID, len(m.Data))
				}
			}
			fmt.Fprintf(conn, ".\r\n")

		case "UIDL":
			fmt.Fprintf(conn, "+OK\r\n")
			for _, m := range opts.Messages {
				if !deleted[m.ID] {
					fmt.Fprintf(conn, "%d %s\r\n", m.ID, m.UIDL)
				}
			}
			fmt.Fprintf(conn, ".\r\n")

		case "RETR":
			idx := 0
			fmt.Sscanf(line, "RETR %d", &idx)
			if idx < 1 || idx > len(opts.Messages) || deleted[idx] {
				fmt.Fprintf(conn, "-ERR no such message\r\n")
				continue
			}
			fmt.Fprintf(conn, "+OK\r\n")
			fmt.Fprintf(conn, "%s\r\n.\r\n", opts.Messages[idx-1].Data)

		case "TOP":
			idx, numLines := 0, 0
			fmt.Sscanf(line, "TOP %d %d", &idx, &numLines)
			if idx < 1 || idx > len(opts.Messages) {
				fmt.Fprintf(conn, "-ERR no such message\r\n")
				continue
			}
			fmt.Fprintf(conn, "+OK\r\n")
			lines := strings.SplitN(opts.Messages[idx-1].Data, "\r\n\r\n", 2)
			fmt.Fprintf(conn, "%s\r\n", lines[0])
			if len(lines) > 1 && numLines > 0 {
				bodyLines := strings.Split(lines[1], "\r\n")
				for i := 0; i < numLines && i < len(bodyLines); i++ {
					fmt.Fprintf(conn, "%s\r\n", bodyLines[i])
				}
			}
			fmt.Fprintf(conn, ".\r\n")

		case "DELE":
			idx := 0
			fmt.Sscanf(line, "DELE %d", &idx)
			deleted[idx] = true
			fmt.Fprintf(conn, "+OK\r\n")

		case "QUIT":
			fmt.Fprintf(conn, "+OK Bye\r\n")
			return

		default:
			fmt.Fprintf(conn, "-ERR unknown command\r\n")
		}
	}
}
```

### 测试用例

| # | 测试函数名 | 场景 | 关键断言 |
|---|-----------|------|---------|
| 1 | `TestPOP3Connect_SSL` | POP3S (隐式 TLS) | `connect()` 成功 |
| 2 | `TestPOP3Connect_STARTTLS` | STLS 升级 | 升级后认证成功 |
| 3 | `TestPOP3Connect_Plaintext_Rejected` | `SSL=false, StartTLS=false` | 返回 "encryption required" error |
| 4 | `TestPOP3Connect_BadAuth` | 错误密码 | 返回 error |
| 5 | `TestPOP3FetchMessages` | 列出邮件 | `ListResult.Messages` 长度匹配 |
| 6 | `TestPOP3FetchMessage_Single` | 获取单封纯文本邮件 | Subject/TextBody 正确 |
| 7 | `TestPOP3FetchMessage_Multipart` | multipart/mixed 邮件 | TextBody + Attachment 正确 |
| 8 | `TestPOP3DeleteMessage` | DELE + QUIT | 再次连接后 LIST 减少 |
| 9 | `TestPOP3ListMessageIDs` | UIDL 列表 | 返回 `[]POP3MessageID` 正确 |
| 10 | `TestPOP3MailReceiver` | 通过 `MailReceiver` 接口 | 三个方法均正确代理 |

### 示例核心实现

```go
const testMailRFC822 = "From: sender@example.com\r\n" +
	"To: rcpt@example.com\r\n" +
	"Subject: Test Subject\r\n" +
	"Date: Mon, 10 Feb 2026 08:00:00 +0000\r\n" +
	"Message-Id: <test-1@example.com>\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Hello, World!"

func TestPOP3Connect_Plaintext_Rejected(t *testing.T) {
	// 即使服务器就绪，客户端应拒绝不加密的连接
	addr := newTestPOP3Server(t, pop3MockOpts{})
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
		t.Fatal("expected error for plaintext POP3")
	}
	if !strings.Contains(err.Error(), "encryption") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPOP3FetchMessage_Single(t *testing.T) {
	addr := newTestPOP3Server(t, pop3MockOpts{
		TLS: true,
		Messages: []pop3MockMessage{
			{ID: 1, UIDL: "msg-001", Data: testMailRFC822},
		},
	})
	host, port := splitHostPort(t, addr)

	client := NewPOP3Client(POP3Config{
		Host:     host,
		Port:     port,
		Username: "testuser",
		Password: "testpass",
		SSL:      true,
	})

	msg, err := client.FetchMessage(1)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("unexpected subject: %s", msg.Subject)
	}
	if msg.TextBody != "Hello, World!" {
		t.Errorf("unexpected body: %s", msg.TextBody)
	}
}
```

---

## 4. Body 解析器独立测试

### 文件: `pkgs/email/body_test.go`

Body 解析逻辑已统一到 `body.go`，可独立于协议进行测试。

| # | 测试函数名 | 场景 | 关键断言 |
|---|-----------|------|---------|
| 1 | `TestParseEntityBody_PlainText` | 纯 text/plain | `msg.TextBody` 正确 |
| 2 | `TestParseEntityBody_HTML` | 纯 text/html | `msg.HTMLBody` 正确 |
| 3 | `TestParseEntityBody_MultipartMixed` | text/plain + 附件 | TextBody + 1 个 Attachment |
| 4 | `TestParseEntityBody_MultipartAlternative` | text/plain + text/html | 两者都填充 |
| 5 | `TestParseEntityBody_NestedMultipart` | multipart/mixed 嵌套 multipart/alternative | TextBody 取 text/plain，HTMLBody 取 text/html |
| 6 | `TestParseEntityBody_MultipleAttachments` | 3 个不同类型附件 | Attachments 长度 3，Filename/ContentType 正确 |
| 7 | `TestParseEntityBody_EmptyBody` | 空 body | 不 panic，TextBody 为空 |
| 8 | `TestParseEntityBody_LargeAttachment` | 1MB 附件 | Attachment.Size 正确，Data 长度匹配 |

### 示例

```go
func TestParseEntityBody_MultipartMixed(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"BOUNDARY\"\r\n" +
		"\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: text/plain\r\n\r\nHello\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"doc.pdf\"\r\n\r\n" +
		"PDF-DATA\r\n" +
		"--BOUNDARY--\r\n"

	entity, err := gomessage.Read(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	msg := &Message{}
	parseEntityBody(msg, entity)

	if msg.TextBody != "Hello" {
		t.Errorf("unexpected TextBody: %q", msg.TextBody)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	if msg.Attachments[0].Filename != "doc.pdf" {
		t.Errorf("unexpected filename: %s", msg.Attachments[0].Filename)
	}
}
```

---

## 5. MailReceiver 接口一致性测试

### 文件: `pkgs/email/receiver_test.go`

验证 `IMAPClient` 和 `POP3Client` 都正确实现 `MailReceiver` 接口。

```go
func TestIMAPClient_ImplementsMailReceiver(t *testing.T) {
	var _ MailReceiver = (*IMAPClient)(nil) // 编译期检查
}

func TestPOP3Client_ImplementsMailReceiver(t *testing.T) {
	var _ MailReceiver = (*POP3Client)(nil) // 编译期检查
}

// TestMailReceiver_IMAP 测试通过接口调用 IMAP 客户端
func TestMailReceiver_IMAP(t *testing.T) {
	addr, _ := newTestIMAPServer(t, false)
	host, port := splitHostPort(t, addr)

	var receiver MailReceiver = NewIMAPClient(IMAPConfig{
		Host: host, Port: port,
		Username: "testuser", Password: "testpass",
	})
	// ... 通过 receiver 调用 FetchMessages / FetchMessageByID / DeleteMessageByID
	defer receiver.Close()
}
```

---

## 6. 辅助函数

在 `testutil_test.go` 中提供：

```go
import (
	"net"
	"strconv"
)

// splitHostPort 从 "host:port" 拆出 host 和 int port。
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
```

---

## 7. 执行方式

```bash
# 运行全部 email 包测试
go test -v ./pkgs/email/...

# 只跑 IMAP 测试
go test -v -run TestIMAP ./pkgs/email/

# 只跑 SMTP 测试
go test -v -run TestSMTP ./pkgs/email/

# 只跑 POP3 测试
go test -v -run TestPOP3 ./pkgs/email/

# 只跑 body 解析测试
go test -v -run TestParseEntityBody ./pkgs/email/

# 检查覆盖率
go test -coverprofile=cover -covermode=atomic ./pkgs/email/...
go tool cover -func=cover
```

---

## 8. 依赖总结

| 依赖 | 状态 | 用途 |
|------|------|------|
| `github.com/emersion/go-imap/v2/imapserver` | 已在 go.mod | IMAP mock 服务器 |
| `github.com/emersion/go-imap/v2/imapserver/imapmemserver` | 已在 go.mod | IMAP 内存实现 |
| `github.com/emersion/go-smtp` | 已在 go.mod | SMTP mock 服务器 |
| `github.com/emersion/go-sasl` | 已在 go.mod | SMTP 认证 |
| 无新增依赖 | — | POP3 使用 `net`/`bufio` 原生包 |

**不需要添加任何新的第三方依赖。**
