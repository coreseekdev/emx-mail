# 代码审查报告

日期: 2026-02-10

---

## 一、重构建议

### 1. [高] IMAP / POP3 body 解析逻辑重复

**文件**: `pkgs/email/imap.go` (L378-L445), `pkgs/email/pop3.go` (L461-L535)

`parseIMAPMessageBody` / `parsePOP3EntityBody` 以及 `parseNestedIMAPMultipart` / `parseNestedPOP3Multipart` 几乎完全相同，只是入参类型略有不同。应抽取为一个统一的 body 解析函数，减少代码重复。

**建议**: 编写一个通用的 `parseEntityBody(msg *Message, entity *gomessage.Entity)` 函数，让 IMAP 和 POP3 共享。

---

### 2. [高] cmd/cli/main.go 过于庞大 (808 行单文件)

`cmd/cli/main.go` 包含了所有命令的 flag 解析、处理逻辑和工具函数，达 808 行。建议按 subcommand 拆分到多个文件：

- `cmd/cli/send.go` — send 命令
- `cmd/cli/list.go` — list 命令
- `cmd/cli/fetch.go` — fetch / delete 命令
- `cmd/cli/watch.go` — watch 命令
- `cmd/cli/init.go` — init 命令
- `cmd/cli/util.go` — 通用工具函数

`cmd/b4/main.go` (745 行) 也有同样的问题，建议类似拆分。

---

### 3. [高] flag 解析中缺少越界检查

多处 flag 解析在读取参数值时未检查 `i+1` 是否越界，例如 `cmd/cli/main.go` 中的 `parseSendFlags`:

```go
case "-to":
    i++
    f.to = args[i]  // 若 -to 是最后一个参数，此处 panic
```

所有 `parseXxxFlags` 函数均有此问题。`cmd/b4/main.go` 中的 `cmdAM` 等函数在遇到需要参数的 flag 时做了检查 (`len(args) < 2`)，但 `cmd/cli/main.go` 中的 `parseListFlags`, `parseSendFlags`, `parseFetchFlags`, `parseDeleteFlags`, `parseWatchFlags` 均未检查。

**建议**: 统一使用 "先检查长度再取值" 模式，或引入一个简单的 flag 解析辅助函数。

---

### 4. [中] AccountConfig 中 IMAP/POP3/SMTP 使用匿名结构体

`pkgs/config/config.go` 中 `AccountConfig` 的 `IMAP`、`POP3`、`SMTP` 字段使用匿名内嵌结构体，导致：
- `ExampleRootConfig()` 中初始化时结构体定义需要重复完整的类型签名（包括所有 JSON tag）。
- 无法在其他地方复用这些类型。

**建议**: 抽取为命名类型 `IMAPSettings`、`POP3Settings`、`SMTPSettings`，减少重复。

---

### 5. [中] POP3Config 缺少 StartTLS 支持

`pkgs/email/pop3.go` 中 `POP3Config` 有 `SSL` 字段，但没有 `StartTLS`。而 `pkgs/config/config.go` 中 `AccountConfig.POP3` 有 `StartTLS` 字段。`cmd/cli/main.go` 中 `newPOP3Client` 也没有传递 `StartTLS`：

```go
func newPOP3Client(acc *config.AccountConfig) (*email.POP3Client, error) {
    return email.NewPOP3Client(email.POP3Config{
        ...
        SSL: acc.POP3.SSL,
        // 缺少 StartTLS
    }), nil
}
```

同时 `pop3Conn.connect()` 也没有 StartTLS 的逻辑。如果不打算支持 POP3 STLS，应从配置中移除 `StartTLS` 字段以避免混淆。

---

### 6. [中] `generateChangeID` 仅返回 slug

`pkgs/patchwork/prep.go` 中 `generateChangeID` 的实现是 `return slug`，没有生成真正唯一的 ID。应使用 UUID 或 hash 机制。

---

### 7. [中] FileStatus 缺少 FirstLineHash 字段

`pkgs/event/event.go` 中 `FileStatus` 结构体没有 `FirstLineHash` 字段，但 `cmd/event/main.go` (L329-L330) 引用了 `st.FirstLineHash`。这会导致编译错误（如果类型检查未被跳过）或该功能永远无法工作。

**建议**: 在 `FileStatus` 中添加 `FirstLineHash string` 字段，并在 `Status()` 方法中填充它。

---

### 8. [低] 缺少接口抽象 — 邮件客户端

`IMAPClient` 和 `POP3Client` 提供相似的功能 (FetchMessages / FetchMessage / DeleteMessage)，但没有共同接口。`cmd/cli/main.go` 中通过 `switch proto` 手动分派。

**建议**: 定义 `MailReceiver` 接口:
```go
type MailReceiver interface {
    FetchMessages(opts FetchOptions) (*ListResult, error)
    FetchMessage(uid uint32) (*Message, error)
    DeleteMessage(uid uint32) error
    Close() error
}
```

---

### 9. [低] 多处使用 `fmt.Sscanf` 解析整数

`cmd/cli/main.go` 和 `cmd/b4/main.go` 中多次使用 `fmt.Sscanf(args[i], "%d", &f.limit)` 来解析整数，未检查返回值（匹配数和错误）。应使用 `strconv.Atoi` 并正确处理错误。

---

### 10. [低] SMTP `buildMessage` 中 Message-ID 格式过于简单

`pkgs/email/smtp.go` L155:
```go
header.Set("Message-ID", fmt.Sprintf("<%d@emx-mail>", time.Now().UnixNano()))
```
- 域名 `emx-mail` 不是有效的 FQDN
- 多个并发调用可能在同一纳秒内产生重复 ID

**建议**: 使用 `uuid` 或加入随机后缀 + 用户域名。

---

### 11. [低] `truncate` 按字节截断，对中文不友好

`cmd/cli/main.go` 中 `truncate` 函数按 `len(s)` 截断（字节数），可能在 UTF-8 多字节序列中间截断，产生乱码。

**建议**: 使用 `[]rune(s)` 或 `utf8.RuneCountInString` 按字符截断。

---

### 12. [低] Event Bus 锁机制使用文件级别互斥

`pkgs/event/bus.go` 中的 `lock()` 使用 `O_CREATE|O_EXCL` 文件锁，依赖 30 秒超时来判定 stale lock。这在进程崩溃后可能导致最多 30 秒的无法使用。考虑使用 `os.Getpid()` 写入 lock 文件以支持 stale 检测。

---

## 二、安全隐患

### S1. [严重] POP3 协议明文传输凭据

**文件**: `pkgs/email/pop3.go` L168-L175 (`connect`) 及 L248-L255 (`auth`)

当 `SSL` 为 `false` 时，POP3 使用明文 TCP 连接，用户名和密码通过 `USER` / `PASS` 命令以明文发送:
```go
func (c *pop3Conn) auth(user, password string) error {
    if _, err := c.cmd("USER", false, user); err != nil {
        return err
    }
    if _, err := c.cmd("PASS", false, password); err != nil {
        return err
    }
    ...
}
```

网络中间人可以截获完整凭据。

**建议**: 非 SSL/TLS 连接时打印警告或直接拒绝连接。同样适用于 IMAP 和 SMTP 的 `DialInsecure` 路径。

---

### S2. [严重] SMTP SSL/TLS 连接未指定 TLS 配置

**文件**: `pkgs/email/smtp.go` L48-L54

```go
client, err := dialFn(addr, nil)  // tlsConfig 为 nil
```

传入 `nil` 作为 `tls.Config`，虽然 Go 标准库会使用合理的默认值，但丢失了 `ServerName` 的显式设置。在某些场景下可能导致证书验证不充分。POP3 (`pop3.go` L178) 正确设置了 `ServerName`:

```go
netConn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
    ServerName: c.config.Host,
})
```

**建议**: SMTP 和 IMAP 连接也应传入明确的 `tls.Config{ServerName: host}`。

---

### S3. [严重] watch 模式命令注入风险

**文件**: `pkgs/email/watch.go` L398-L410 (`runHandler`)

```go
func (c *IMAPClient) runHandler(cmd string, emailData []byte) (int, error) {
    parts := strings.Fields(cmd)
    cmdExec := parts[0]
    args := parts[1:]
    cmdObj := exec.Command(cmdExec, args...)
    cmdObj.Stdin = strings.NewReader(string(emailData))
    ...
}
```

`HandlerCmd` 来自配置文件，通过 `strings.Fields` 分割参数。如果配置被恶意篡改或参数中包含特殊字符，可能导致非预期行为。此外：

- **邮件内容直接传递给外部进程**: 恶意邮件内容通过 stdin 传递给 handler，如果 handler 不安全，可能导致任意代码执行。
- ~~**无输入大小限制**: 巨大邮件会完整加载到内存并传递给 handler。~~ **[已修复]** `fetchRawEmail` 改为 `fetchRawEmailReader`，使用 go-imap/v2 的 `FetchMessageData` 流式 API（`LiteralReader`）返回 `io.Reader`；`runHandler` 通过 `cmd.StdinPipe()` (OS pipe) 将数据流式传递给 handler 进程，内核管道缓冲区（~64 KB）自动控制背压，峰值内存不再与邮件大小成正比。

**建议**:
1. ~~对传入的邮件数据设置大小上限~~ **[已修复]** 改用流式传输，无需硬编码大小上限
2. 文档中明确说明 handler 安全要求
3. 考虑使用 `exec.Command(shell, "-c", cmd)` 统一语义或限制仅允许可执行文件路径

---

### S4. [高] 附件文件名可能导致路径穿越

**文件**: `cmd/cli/main.go` L480-L495

```go
filePath := filepath.Join(f.saveAttachments, att.Filename)
if err := os.WriteFile(filePath, att.Data, 0644); err != nil {
```

`att.Filename` 来自邮件内容，如果包含 `../` 等路径穿越字符，可能覆盖用户目录中的任意文件。

**建议**: 使用 `filepath.Base(att.Filename)` 仅保留文件名部分，或验证 `filePath` 的前缀是目标目录。

---

### S5. [高] IMAP 连接缺少 TLS ServerName 验证

**文件**: `pkgs/email/imap.go` L47-L52

```go
if c.config.SSL {
    client, err = imapclient.DialTLS(addr, &imapclient.Options{})
} else if c.config.StartTLS {
    client, err = imapclient.DialStartTLS(addr, &imapclient.Options{})
}
```

传入空的 `imapclient.Options{}`，没有设置 TLS 配置。应检查 `go-imap` 库是否在其 Options 中需要显式设置 `TLSConfig`，以确保证书正确验证 hostname。

---

### S6. [中] POP3 连接缺少超时控制（读写超时）

**文件**: `pkgs/email/pop3.go` L167-L181

POP3 连接有 10 秒的拨号超时，但建立连接后没有设置 read/write deadline，可能导致恶意服务器通过慢响应进行 DoS。

```go
dialer := &net.Dialer{Timeout: 10 * time.Second}
// 连接建立后不再有超时设置
```

**建议**: 连接后调用 `netConn.SetDeadline()` 或为每个操作设置合理的超时。

---

### S7. [中] Event Bus lock 文件竞态条件

**文件**: `pkgs/event/bus.go` L273-L297

```go
if fi, serr := os.Stat(lockPath); serr == nil {
    if time.Since(fi.ModTime()) > 30*time.Second {
        os.Remove(lockPath)  // 删除后另一个进程也可能在此刻删除或创建
        continue
    }
}
```

在检查 stale lock 后的 `os.Remove` 和下一轮 `OpenFile(O_EXCL)` 之间存在 TOCTOU 竞态条件。两个进程可能同时检测到 stale lock 并同时获取锁。

**建议**: 考虑使用 `fcntl` / `flock` 系统级文件锁，或在 lock 文件中写入 PID 并检查进程是否存活。

---

### S8. [中] emx-config 输出的命令注入风险

**文件**: `pkgs/config/config.go` L287-L298 (`loadFromEmxConfig`)

```go
cmd := exec.Command("emx-config", "list", "--json")
```

虽然此处命令参数是硬编码的不受用户输入影响，但如果 `PATH` 中存在恶意的 `emx-config` 二进制文件，将被执行。`HasEmxConfig()` 用 `exec.LookPath` 查找，信任 PATH 中的任何匹配项。

**建议**: 如果安全性要求较高，应允许用户配置 `emx-config` 的完整路径，而非依赖 PATH 搜索。

---

### S9. [低] `watchIDLE` 中 goroutine 可能泄漏

**文件**: `pkgs/email/watch.go` L475-L519

```go
go func() {
    idleCmd.Wait()
    done <- struct{}{}
}()

select {
case <-time.After(29 * time.Minute):
    idleCmd.Close()
case <-done:
    idleCmd.Close()
}
```

如果 `time.After` 触发并关闭 `idleCmd`，goroutine 中的 `idleCmd.Wait()` 可能立即返回也可能挂起，取决于底层实现。如果挂起，goroutine 永远无法退出，导致泄漏。此外 channel buffer 大小为 1 且在 select timeout 分支不消费，实际上不会阻塞发送方，但每次循环都会启动新的 goroutine。

**建议**: 使用 `context.WithCancel` 或确保 `idleCmd.Close()` 能让 `Wait()` 立即返回。

---

### S10. [低] emx-save 生成的文件名可能信息泄露

**文件**: `cmd/emx-save/main.go` L100-L110

`sanitizeFilename` 保留了大量 Message-ID 原始内容作为文件名。Message-ID 有时包含内部域名或用户信息。

**建议**: 如果邮件保存目录对外可访问，考虑使用 hash 或序号作为文件名。

---

## 四、第二轮审查 (2026-02-10)

> 基于第一轮重构后的代码状态进行二次审查。前一轮已标记 [已修复] 的条目不再重复列出。

### R1. [高] SMTP attachment 资源泄漏 — 缺少 `defer f.Close()`

**文件**: `pkgs/email/smtp.go` L228-L245

```go
f, err := os.Open(att.Path)
if err != nil {
    return nil, fmt.Errorf("failed to open attachment %s: %w", att.Path, err)
}

if _, err := io.Copy(w, f); err != nil {
    f.Close()
    return nil, fmt.Errorf("failed to copy attachment %s: %w", att.Path, err)
}
f.Close()
w.Close()
```

如果 `io.Copy` 成功但后续的 `w.Close()` 触发 panic 或某处提前 return，`f` 不会被关闭。应改为 `defer f.Close()`:

```go
f, err := os.Open(att.Path)
if err != nil {
    return nil, ...
}
defer f.Close()
```

由于在 **循环内** 使用 `defer` 会延迟到函数返回才执行，对于多附件场景可能同时持有多个 fd。更优方案是将单个附件的处理逻辑抽取为闭包或辅助函数：

```go
if err := func() error {
    f, err := os.Open(att.Path)
    if err != nil { return err }
    defer f.Close()
    ...
}(); err != nil {
    return nil, err
}
```

---

### R2. [高] `cmdMbox` 一次性读取完整 mbox 到内存后再解析

**文件**: `cmd/b4/mbox_cmd.go` L45-L51

```go
data, err := io.ReadAll(f)
if err != nil { ... }
mb := patchwork.NewMailbox()
if err := mb.ReadMbox(bytes.NewReader(data)); err != nil { ... }
```

先 `io.ReadAll` 完整读入内存，再用 `bytes.NewReader` 传给 `ReadMbox`。`ReadMbox` 接受 `io.Reader`，可以直接传 `f`：

```go
mb := patchwork.NewMailbox()
if err := mb.ReadMbox(f); err != nil { ... }
```

消除一次完整的内存副本；大 mbox 文件 (数百 MB 补丁集) 峰值内存可减半。

---

### R3. [中] `cmdDiff` 中 `fmt.Sscanf` 解析整数未检查返回值

**文件**: `cmd/b4/am.go` L180-L181 (`cmdDiff`)

```go
fmt.Sscanf(parts[0], "%d", &rev1)
fmt.Sscanf(parts[1], "%d", &rev2)
```

如果用户传入 `--range abc..def`，`rev1` / `rev2` 默认保持 0，无报错，行为等同 `--range 0..0`，使 `GetSeries(0)` 返回最新版本——可能令人困惑。应使用 `strconv.Atoi` 并报错。

---

### R4. [中] `pop3Conn.stat()` / `list()` / `uidl()` 中 `strconv.Atoi` 解析失败被静默吞掉

**文件**: `pkgs/email/pop3.go` L359-L363, L383-L386, L403-L406

```go
count, _ = strconv.Atoi(string(f[0]))   // stat()
id, _ := strconv.Atoi(string(f[0]))     // list()
id, _ := strconv.Atoi(string(f[0]))     // uidl()
```

所有 `_ =` 均忽略错误。恶意或异常 POP3 服务器返回非数字字段时，`count`/`id`/`sz` 默认为 0，可能导致后续逻辑混乱（如 `count=0` 被认为信箱为空而跳过所有邮件）。应至少记录日志，或在 `stat()` 中返回错误。

---

### R5. [中] `IMAPClient.ensureConnected` 的 cleanup 函数语义不对称

**文件**: `pkgs/email/imap.go` L75-L84

```go
func (c *IMAPClient) ensureConnected() (func(), error) {
    if c.client != nil {
        return func() {}, nil          // 已连接 → cleanup 什么都不做
    }
    if err := c.Connect(); err != nil {
        return nil, err
    }
    return func() { c.Close() }, nil  // 新连接 → cleanup 关闭连接
}
```

当调用者通过外部 `Connect()` 预连接、然后多次调用 `FetchMessages` → 第一次 cleanup 为 noop，第二次也为 noop，正确。但如果 `ensureConnected` 自行连接，每次调用后 `defer cleanup()` 就会关闭连接，导致下一次 `ensureConnected` 又要重新连接。在循环场景中（如 `Watch` 模式依次 `processEmail` 每封邮件）存在效率浪费。

**建议**: 让 `Watch` / 批量场景预先 `Connect()` 并保持连接存活，而不是每封邮件一次连接/断开。

---

### R6. [低] `parseNestedPOP3Multipart` 和 `parsePOP3EntityBody` 可以移除

**文件**: `pkgs/email/pop3.go` L520-L530

```go
func parsePOP3EntityBody(msg *Message, entity *gomessage.Entity) {
    parseEntityBody(msg, entity)
}
func parseNestedPOP3Multipart(msg *Message, entity *gomessage.Entity) {
    if nested := entity.MultipartReader(); nested != nil {
        parseMultipart(msg, nested)
    }
}
```

这两个函数是第一轮重构后保留的 "signature compat" 包装，但现在只有 `parsePOP3EntityBody` 在 `FetchMessage` 中被调用，且它只是简单转发到 `parseEntityBody`。`parseNestedPOP3Multipart` 完全没有调用者。可直接删除这两个包装函数，在 `FetchMessage` 中直接调用 `parseEntityBody`。

---

### R7. [低] `POP3Client` 每个操作都建立新连接

**文件**: `pkgs/email/pop3.go` — `FetchMessages()`, `FetchMessage()`, `DeleteMessage()`, `ListMessageIDs()`

每个公共方法都 `connect() → 操作 → quit()`。POP3 每次连接需要完整的 TCP + TLS 握手 + AUTH，延迟高。如果用户先 `list` 再 `fetch`，总共会建立两条独立连接。

**建议**: 引入 `Connect()` / `Close()` 生命周期方法，在 `POP3Client` 上复用底层 `pop3Conn`，类似 `IMAPClient` 的模式。`Close()` 方法已存在但目前是 no-op。

---

## 五、安全隐患 (第二轮)

### S11. [严重] `emx-save` 的 `io.ReadAll(os.Stdin)` 无大小限制 — OOM 风险

**文件**: `cmd/emx-save/main.go` L39

```go
data, err := io.ReadAll(os.Stdin)
```

`emx-save` 作为 `--handler` 被外部调用时，stdin 来自 `runHandler` 的管道，而 `runHandler` 现在已改为流式传输——不再有内存限制。如果来源是一封极大邮件（或恶意输入），`io.ReadAll` 会将整个内容加载到内存。

**建议**: 使用 `io.LimitReader` 设置合理上限 (如 256 MB)，或改为流式写入：

```go
out, err := os.Create(path)
if err != nil { fatal(...) }
defer out.Close()
if _, err := io.Copy(out, os.Stdin); err != nil { fatal(...) }
```

这样也消除了 `extractMessageID` 必须持有完整 `[]byte` 的限制——可改为在前 8KB 中扫描 `Message-ID`，然后流式写入文件。

---

### S12. [严重] `emx-save` 的 `extractMessageID` 使用原始字节扫描，不支持 folded headers

**文件**: `cmd/emx-save/main.go` L72-L93

```go
lines := bytes.Split(headers, []byte("\n"))
for _, line := range lines {
    line = bytes.TrimLeft(line, "\r")
    if bytes.HasPrefix(bytes.ToLower(line), []byte("message-id:")) {
```

RFC 5322 允许 header folding（长 header 在 CRLF + WSP 处换行）。如果 `Message-ID` 值换行到下一行，此解析器会提取不完整的值，导致文件名截断。

**建议**: 使用 `net/mail.ReadMessage()` 或 `go-message` 库正确解析 MIME headers，替代手写的逐行扫描。

---

### S13. [高] `readFile` 在一次 `io.ReadAll` 中解压整个 gzip 到内存

**文件**: `pkgs/event/bus.go` — `readFile()`, `getFileStats()`

```go
gr.Multistream(true)
data, err := io.ReadAll(gr)
```

事件文件最大允许 64 MB 未压缩。两处 (`readFile` 和 `getFileStats`) 都一次性解压完整文件。如果同时持锁读多个文件 (`List` 的循环)，内存峰值可达 **N × 64 MB**。

**建议**: 改用 `bufio.Scanner` / `json.Decoder` 流式逐行解析，从 `gzip.Reader` 直接读取，避免 `io.ReadAll`。

---

### S14. [高] POP3 密码在 `USER`/`PASS` 命令中被构造为日志可见的字符串

**文件**: `pkgs/email/pop3.go` L280-L295

```go
func (c *pop3Conn) cmd(cmd string, isMulti bool, args ...interface{}) (*bytes.Buffer, error) {
    cmdLine := cmd
    if len(args) > 0 {
        parts := make([]string, len(args))
        for i, a := range args {
            parts[i] = fmt.Sprintf("%v", a)
        }
        cmdLine = cmd + " " + strings.Join(parts, " ")
    }
    if err := c.send(cmdLine); err != nil { ... }
```

`auth()` 调用 `c.cmd("PASS", false, password)` 时，密码被拼接到 `cmdLine` 字符串中。虽然目前没有日志输出，但如果将来在 `cmd()` 中添加 debug 日志（如 `log.Printf("POP3 > %s", cmdLine)`），密码会被明文记录。

**建议**: 为 `PASS` 命令添加掩码参数，或在 `auth()` 中直接调用 `send()`/`readOne()` 而不走通用 `cmd()` 路径。

---

### S15. [中] `watchIDLE` — `time.After` 每轮循环泄漏 Timer

**文件**: `pkgs/email/watch.go`

```go
case <-time.After(29 * time.Minute):
```

`time.After` 创建的 Timer 在 `done` channel 先返回时**不会被回收**，直到其 29 分钟超时过期。在频繁接收邮件的场景中（IDLE 每收到一封就返回一次），每轮循环泄漏一个 29 分钟的 Timer。

**建议**:
```go
timer := time.NewTimer(29 * time.Minute)
defer timer.Stop()
select {
case <-timer.C:
    ...
case <-done:
    timer.Stop()
    ...
}
```

---

### S16. [中] `sanitizeChannel` 允许 `.` 和 `..` 作为 channel name

**文件**: `pkgs/event/marker.go` L76-L89

`sanitizeChannel` 替换了 `/\:*?"<>|` 和空格，但没有禁止 `.` / `..`。如果 channel name 为 `..`，生成的 marker 路径为:

```
~/.emx-mail/events/markers/...json
```

在 Unix 上这会创建一个名为 `...json` 的文件（合法但怪异）。在更极端场景下，如果未来扩展支持子目录的 channel，`../evil` 会穿越到 `events/` 目录外。

**建议**: 在 `sanitizeChannel` 结尾检查 `safe == "." || safe == ".."` 时替换为 `_dot_` / `_dotdot_` 或直接 reject。

---

## 六、使用优化建议

### U1. [高] 邮件列表分页 — 目前无法翻页

`FetchMessages` 只有 `Limit` 参数，始终取 **最新的 N 条**。用户无法查看更早的消息。

**建议**: 添加 `Offset` 或 `Before` 参数支持分页，例如:

```go
type FetchOptions struct {
    Folder  string
    Limit   int
    Offset  int  // 跳过最新的 Offset 条
    // 或: BeforeUID uint32  // 只取 UID < BeforeUID 的消息
}
```

CLI 层提供 `--page` 或 `--before` flag。

---

### U2. [高] `emx-mail send` 不支持从文件/stdin 读取正文

当前 `--text` 和 `--html` 只接受命令行参数字符串。长邮件需用户在命令行中内联所有文本。

**建议**: 支持 `--text-file` / `--html-file`，以及当 `--text -` 时从 stdin 读取正文。

---

### U3. [中] `emx-mail send --attachment` 只支持单个附件

`parseSendFlags` 中 `attachment` 是 `string` 而非 `[]string`。

**建议**: 使用 `pflag.StringArrayVar` 或将 `--attachment` 改为可重复参数:

```go
fs.StringArrayVar(&f.attachments, "attachment", nil, "Attachment file path (repeatable)")
```

---

### U4. [中] `handleInit` 在 `emx-config` 不可用且 `EMX_MAIL_CONFIG_JSON` 未设时直接报错

**文件**: `cmd/cli/init_cmd.go` L33-L35

```go
configPath, err := config.GetEnvConfigPath()
if err != nil {
    return err  // 直接返回 "EMX_MAIL_CONFIG_JSON is not set"
}
```

用户首次使用 `emx-mail init` 时通常没有设置环境变量。应提供默认路径 (如 `~/.emx-mail/config.json`) 并提示用户设置环境变量。

---

### U5. [中] Watch 模式没有 graceful shutdown 机制

`Watch` 中的 `watchIDLE` / `watchPoll` 都是无限循环，不接受 `context.Context` 或 OS signal。用户只能 Ctrl+C 强杀。

**建议**: 注册 `SIGINT` / `SIGTERM` 信号处理器，执行 `Close()` + 退出。或将 `Watch` 改为接受 `context.Context`，由 CLI 层负责 cancel。

---

### U6. [低] 配置文件中密码以明文存储

`ProtocolSettings.Password` 字段直接持有明文密码并写入 JSON 文件。

**建议**: 支持从环境变量引用 (`$ENV_VAR`)、系统 keyring、或 `gpg --decrypt` 获取密码。至少在 `ExampleRootConfig` 中将 Password 留空并以注释说明推荐做法。

---

### U7. [低] `list` 输出格式不支持 JSON / 可编程消费

`handleList` 输出人类友好的格式化文本。自动化工具无法可靠解析输出。

**建议**: 添加 `--json` flag 输出 JSON lines 格式，方便管道和脚本集成。
