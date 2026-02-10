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

## 三、其他注意事项

| 类别 | 说明 |
|------|------|
| 测试覆盖 | `pkgs/email/` 下没有任何测试文件，IMAP/POP3/SMTP 客户端完全没有单元测试 |
| 错误处理 | `pop3Conn.stat()` 中 `strconv.Atoi` 失败被静默忽略，返回 0 值 |
| 资源管理 | `SMTPClient.Send()` 中 attachment 文件在 `io.Copy` 出错时通过 `f.Close()` 关闭，但后续的 `w.Close()` 可能仍被调用，建议使用 `defer` |
| 日志一致性 | `cmd/b4/main.go` 使用中文错误消息和 `fatal(msg string)`（单参数），而 `cmd/cli/main.go` 使用英文和 `fatal(format string, args ...interface{})`（格式化参数），两者签名不同且均命名为 `fatal` |
| 文档 | `handleInit` 中的配置路径引导在 `emx-config` 不存在时依赖 `GetEnvConfigPath()`，如果环境变量未设置会直接报错，缺少友好的引导提示 |
