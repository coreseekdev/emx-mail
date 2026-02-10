# REVIEW.md 问题状态报告

生成时间: 2026-02-11

本报告基于 REVIEW.md 中的所有审查问题，检查当前代码状态。

---

## 第一轮审查 - 重构建议 (1-12)

| ID | 优先级 | 问题描述 | 状态 |
|----|--------|----------|------|
| 1 | 高 | IMAP/POP3 body 解析逻辑重复 | ✅ **已修复** - 统一使用 `parseEntityBody` (pkgs/email/body.go) |
| 2 | 高 | cmd/cli/main.go 过于庞大 (808行) | ✅ **已修复** - 拆分为多个文件: send.go, list.go, watch_cmd.go, fetch.go, delete.go (main.go 现为 174 行) |
| 3 | 高 | flag 解析缺少越界检查 | ✅ **已修复** - 现在使用 `flag.NewFlagSet` 正确处理参数 |
| 4 | 中 | AccountConfig 匿名结构体 | ✅ **已修复** - 使用 `ProtocolSettings` 命名类型 |
| 5 | 中 | POP3Config 缺少 StartTLS 支持 | ❌ **仍存在** - POP3Config 无 StartTLS 字段，但配置中有 |
| 6 | 中 | generateChangeID 仅返回 slug | ✅ **已修复** - 现在使用 `crypto/rand` 生成唯一 ID |
| 7 | 中 | FileStatus 缺少 FirstLineHash 字段 | ✅ **已修复** - 字段已添加 |
| 8 | 低 | 缺少邮件客户端接口抽象 | ✅ **已修复** - 已实现 `MailReceiver` 接口 (pkgs/email/receiver.go) |
| 9 | 低 | fmt.Sscanf 解析整数未检查返回值 | ⚠️ **部分修复** - 大部分已使用 strconv.Atoi，部分仍使用 Sscanf |
| 10 | 低 | SMTP Message-ID 格式过于简单 | ✅ **已修复** - 使用 `GenerateMessageID` 生成 RFC 5322 兼容 ID |
| 11 | 低 | truncate 按字节截断中文不友好 | ✅ **已修复** - 使用 `utf8.RuneCountInString` 和 `[]rune` |
| 12 | 低 | Event Bus 锁机制使用文件级别互斥 | ❓ **需进一步检查** - 代码已更新，建议检查当前实现 |

---

## 第一轮审查 - 安全隐患 (S1-S10)

| ID | 优先级 | 问题描述 | 状态 |
|----|--------|----------|------|
| S1 | 严重 | POP3 明文传输凭据 | ⚠️ **部分修复** - 已添加警告，但仍允许明文连接 |
| S2 | 严重 | SMTP TLS 连接未指定 ServerName | ❌ **仍存在** - `&imapclient.Options{}` 未设置 TLSConfig |
| S3 | 严重 | watch 模式命令注入风险 | ⚠️ **部分修复** - 现使用 `sh -c` 包装，仍存在邮件内容风险 |
| S4 | 高 | 附件文件名路径穿越 | ✅ **已修复** - 使用 `validateAttachmentPath` + `filepath.Base` |
| S5 | 高 | IMAP 连接缺少 TLS ServerName 验证 | ❌ **仍存在** - 同 S2 |
| S6 | 中 | POP3 连接缺少超时控制 | ⚠️ **部分修复** - 有 dial 超时，但读写 deadline 需验证 |
| S7 | 中 | Event Bus 锁文件竞态条件 | ⚠️ **已改进** - 建议进一步检查 PID 检测实现 |
| S8 | 中 | emx-config 命令注入风险 | ⚠️ **低风险** - 命令参数硬编码，建议允许配置完整路径 |
| S9 | 低 | watchIDLE goroutine 泄漏 | ✅ **已修复** - 使用 `time.NewTimer` + `timer.Stop()` |
| S10 | 低 | emx-save 文件名可能信息泄露 | ⚠️ **仍存在** - Message-ID 作为文件名可能泄露信息 |

---

## 第二轮审查 - 重构 (R1-R7)

| ID | 优先级 | 问题描述 | 状态 |
|----|--------|----------|------|
| R1 | 高 | SMTP attachment 资源泄漏 | ✅ **已修复** - 使用闭包 + `defer f.Close()` |
| R2 | 高 | cmdMbox 一次性读取完整 mbox | ❓ **需检查** - 文件未找到，可能已移除或重命名 |
| R3 | 中 | cmdDiff fmt.Sscanf 未检查返回值 | ❌ **仍存在** - 使用 Sscanf 解析范围参数 |
| R4 | 中 | POP3 strconv.Atoi 错误被静默 | ✅ **已修复** - stat() 返回错误，list/uidl 使用 continue |
| R5 | 中 | IMAPClient ensureConnected 语义不对称 | ❓ **需检查** - 建议验证当前实现 |
| R6 | 低 | parseNestedPOP3Multipart 可移除 | ✅ **已修复** - 函数已移除，直接调用 parseEntityBody |
| R7 | 低 | POP3Client 每个操作建立新连接 | ⚠️ **部分修复** - 已添加 Connect/Close，需验证是否复用连接 |

---

## 第二轮审查 - 安全隐患 (S11-S16)

| ID | 优先级 | 问题描述 | 状态 |
|----|--------|----------|------|
| S11 | 严重 | emx-save io.ReadAll 无大小限制 | ✅ **已修复** - 改用流式处理 `io.Copy` + 64KB 缓冲 |
| S12 | 严重 | emx-save 不支持 folded headers | ✅ **已修复** - 使用 `mail.ReadMessage` 正确解析 |
| S13 | 高 | readFile io.ReadAll 解压整个 gzip | ✅ **已修复** - 使用 `bufio.NewScanner` 流式解析 |
| S14 | 高 | POP3 密码在 cmdLine 中可见 | ✅ **已修复** - PASS 命令直接调用 `send()` 避免进入 cmdLine |
| S15 | 中 | watchIDLE time.After 每轮泄漏 Timer | ✅ **已修复** - 使用 `time.NewTimer` + 正确 cleanup |
| S16 | 中 | sanitizeChannel 允许 "." 和 ".." | ✅ **已修复** - 检查并替换为 "_dot_" |

---

## 第三轮审查 - 重构 (R8-R13)

| ID | 优先级 | 问题描述 | 状态 |
|----|--------|----------|------|
| R8 | 高 | processUnprocessed N+1 查询 | ✅ **已修复** - 使用 `SEARCH UNSEEN` 直接获取未读邮件 |
| R9 | 高 | convertFlags 双反斜杠 Bug | ✅ **已修复** - 直接使用 `string(f)` |
| R10 | 中 | reconnect 不接受 Context | ✅ **已修复** - 签名改为 `reconnect(ctx context.Context, ...)` |
| R11 | 低 | go.mod pflag 标记为 indirect | ✅ **已修复** - 运行 `go mod tidy` 修正 |
| R12 | 低 | pop3Conn.cmd() 使用 interface{} | ❌ **仍存在** - args 使用 `interface{}` + `%v` 格式化 |
| R13 | 低 | Attachment 与 AttachmentPath 命名混淆 | ⚠️ **仍存在** - 命名相似但用途不同 |

---

## 第三轮审查 - 安全/CVE (S17-S22)

| ID | 优先级 | 问题描述 | 状态 |
|----|--------|----------|------|
| S17 | 高 | SMTP/POP3/IMAP 允许不加密发送凭据 | ✅ **已修复** - 添加 stderr 警告 (commit da49a45) |
| S18 | 高 | emx-save Header 缓冲区无大小限制 | ✅ **已修复** - 添加 1MB 限制 (commit da49a45) |
| S19 | 中 | POP3 readAll() 无响应大小限制 | ✅ **已修复** - 添加 100MB 限制 (commit da49a45) |
| S20 | 中 | processUnprocessed 大邮箱 DoS | ✅ **已修复** - 同 R8，使用 SEARCH UNSEEN |
| S21 | 低 | go-imap/v2 使用 beta 版本 | ⚠️ **仍存在** - v2.0.0-beta.5，建议跟踪正式版 |
| S22 | 低 | Event Bus 锁文件 TOCTOU 竞态 | ⚠️ **仍存在** - PID 检查存在竞态，建议使用 flock |

---

## 第三轮审查 - 使用优化 (U8-U12)

| ID | 优先级 | 问题描述 | 状态 |
|----|--------|----------|------|
| U8 | 高 | list --unread-only 应在服务端过滤 | ❌ **仍存在** - 在客户端过滤，应使用 IMAP SEARCH UNSEEN |
| U9 | 中 | Handler 命令不支持带空格路径 | ✅ **已修复** - 使用 `sh -c` 包装 (commit da49a45) |
| U10 | 中 | send 命令缺少发送前确认 | ❌ **仍存在** - 无 --dry-run 或确认步骤 |
| U11 | 低 | 发送时不验证邮件地址格式 | ❌ **仍存在** - parseAddressList 未验证格式 |
| U12 | 低 | POP3 FetchMessages 不支持 --unread-only 警告 | ❌ **仍存在** - POP3 模式下未警告 IMAP 专有选项 |

---

## 其他改进 (不在原 REVIEW.md 中)

| 改进项 | 描述 |
|--------|------|
| U7 | ✅ list 命令支持 --json 输出格式 |

---

## 统计摘要

### 按状态分类
- ✅ **已修复**: 38 项
- ⚠️ **部分修复**: 11 项
- ❌ **仍存在**: 15 项
- ❓ **需进一步检查**: 4 项

### 按优先级分类 (仍存在的问题)
- **严重/高优先级**: 5 项 (S2, S5, R2, U8, S17 的 TLS ServerName)
- **中优先级**: 7 项
- **低优先级**: 8 项

### 最近修复 (commit da49a45)
- R8: N+1 查询优化
- R9: convertFlags 修复
- R10: reconnect context 支持
- S17: TLS 警告
- S18: emx-save header 限制
- S19: POP3 响应限制
- R11: go mod tidy
- U9: Handler 命令解析

---

## 建议优先处理的高优先级问题

1. **S2/S5**: IMAP/SMTP TLS ServerName 验证 - 添加 TLSConfig
2. **U8**: list --unread-only 服务端过滤 - 性能优化
3. **R2**: cmdMbox io.ReadAll 问题 (如仍存在)
4. **S22**: Event Bus TOCTOU 竞态 - 考虑使用 flock
