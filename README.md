# emx-mail - Go CLI 邮件客户端

使用 Go 构建的命令行邮件客户端。

## 项目结构

```
emx-mail/
├── cmd/
│   └── cli/
│       └── main.go          # CLI 入口，命令行参数解析
├── pkgs/
│   ├── config/
│   │   └── config.go        # 配置文件管理
│   └── email/
│       ├── email.go         # 邮件类型定义
│       ├── smtp.go          # SMTP 发信实现
│       ├── imap.go          # IMAP 收信实现
│       └── pop3.go          # POP3 收信实现
├── go.mod
└── README.md
```

## 功能特性

### ✅ 已完成

1. **配置管理** (`pkgs/config/`)
   - JSON 配置文件格式
   - 支持多个账户配置
   - 默认配置路径: `~/.emx-mail/config.json`
   - 账户验证和默认账户设置

2. **邮件类型定义** (`pkgs/email/`)
   - Message 结构：邮件头、正文、附件、标志
   - SendOptions：发送邮件选项
   - FetchOptions：获取邮件选项（支持远端删除）
   - Folder 和 ListResult：文件夹和列表结果

3. **SMTP 发信功能** (`pkgs/smtp/`)
   - 支持 SSL/TLS/StartTLS 连接
   - 纯文本和 HTML 邮件
   - 抄送 (CC)、密送 (BCC)
   - 附件支持
   - 回复邮件（In-Reply-To, References）
   - 连接池和重试机制

4. **CLI 命令行接口** (`cmd/cli/main.go`)
   - `send` - 发送邮件
   - `list` - 列出邮件
   - `fetch` - 获取邮件内容
   - `delete` - 删除邮件（支持 expunge）
   - `folders` - 列出文件夹
   - `init` - 初始化配置文件

5. **依赖管理**
   - 使用最新的 emersion 邮件库

### 🚧 进行中

**IMAP 收信功能** (`pkgs/imap/`)
- 已实现基本结构，但需要修复 API 兼容性问题
- go-imap/v2 API 变化导致类型不匹配

## 使用说明

### 初始化配置

```bash
emx-mail init
```

这会创建示例配置文件 `~/.emx-mail/config.json`：

```json
{
  "accounts": [
    {
      "name": "Example Account",
      "email": "user@example.com",
      "from_name": "Your Name",
      "imap": {
        "host": "imap.example.com",
        "port": 993,
        "username": "user@example.com",
        "ssl": true
      },
      "smtp": {
        "host": "smtp.example.com",
        "port": 587,
        "username": "user@example.com",
        "starttls": true
      }
    }
  ],
  "default_account": ""
}
```

### 命令示例

```bash
# 发送邮件
emx-mail send -to user@example.com -subject "Hello" -text "Hello, World!"

# 列出收件箱
emx-mail list

# 列出特定文件夹
emx-mail list -folder Archive -limit 50

# 获取邮件
emx-mail fetch -uid 12345

# 删除邮件（标记为删除）
emx-mail delete -uid 12345

# 永久删除邮件
emx-mail delete -uid 12345 -expunge

# 列出所有文件夹
emx-mail folders
```

## 技术细节

### 使用的库

- `github.com/emersion/go-imap/v2` - IMAP 协议实现
- `github.com/emersion/go-message` - 邮件消息格式
- `github.com/emersion/go-sasl` - SASL 认证
- `github.com/emersion/go-smtp` - SMTP 协议实现

### 设计原则

1. **标准目录结构**: 使用 `cli + pkgs` 的 Go 标准项目布局
2. **依赖原版**: 使用原版库而非 fork，并更新到最新版本
3. **简洁实用**: CLI 工具，简单易用
4. **可选远端删除**: 收信时可选择是否在服务器删除

## 待完成功能

1. 修复 IMAP API 兼容性问题
2. 实现 POP3 收信支持（可选）
3. 添加更多的邮件操作标志
4. 支持批量操作
5. 添加搜索功能
6. 支持配置文件加密（密码保护）

## 开发说明

### 构建项目

```bash
# 获取依赖（需要代理）
export http_proxy="http://127.0.0.1:49725"
export https_proxy="http://127.0.0.1:49725"
go mod tidy

# 构建
go build -o emx-mail.exe ./cmd/cli
```

### 当前问题

IMAP v2 的 API 与早期版本有很大变化，主要问题：
- `NumSet` 类型是接口，不能直接使用 `len()`
- `Envelope` 结构的字段类型改变（数组 vs 指针数组）
- `FetchBodySection` 结构变化（`Literal` 字段不存在）
- 需要使用 `Collect()` 模式而不是迭代器

## 参考

- go-imap: https://github.com/emersion/go-imap
- go-smtp: https://github.com/emersion/go-smtp

## License

MIT
