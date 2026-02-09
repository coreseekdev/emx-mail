# emx-mail - 邮件客户端与补丁管理工具

使用 Go 构建的命令行邮件客户端和补丁管理工作流工具。

## 项目结构

```
emx-mail/
├── cmd/
│   ├── cli/
│   │   └── main.go          # 邮件客户端入口 (send, list, fetch, delete, folders)
│   └── b4/
│       └── main.go          # 补丁管理工具入口 (am, shazam, prep, diff, mbox)
├── pkgs/
│   ├── config/
│   │   └── config.go        # 配置文件管理
│   ├── email/               # 邮件客户端功能
│   │   ├── email.go         # 邮件类型定义
│   │   ├── smtp.go          # SMTP 发信实现
│   │   ├── imap.go          # IMAP 收信实现
│   │   └── pop3.go          # POP3 收信实现
│   └── patchwork/           # 补丁管理工作流
│       ├── subject.go       # 补丁主题行解析
│       ├── trailer.go       # Trailer 解析与分类
│       ├── message.go       # 邮件消息解析与补丁系列管理
│       ├── amready.go       # 生成 git-am 可用 mbox
│       ├── git.go           # Git 操作封装
│       └── prep.go          # Prep 分支管理
├── docs/
│   ├── cli.md               # 邮件客户端使用说明
│   └── b4.md                # 补丁管理工具使用说明
├── go.mod
└── README.md
```

## 功能特性

### 邮件客户端 (`emx-mail`)

**已完成功能**：
- 多账户配置管理
- SMTP 发送（纯文本、HTML、附件、抄送/密送、中文支持）
- IMAP 收信（文件夹、列表、获取、删除、附件下载）
- POP3 收信（列表、获取、删除、附件下载）
- 附件下载（支持中文文件名）
- MIME 消息解析
- 协议自动检测

**命令**：
- `init` - 初始化配置
- `send` - 发送邮件
- `list` - 列出邮件
- `fetch` - 获取邮件内容和附件
- `delete` - 删除邮件
- `folders` - 列出所有文件夹（仅 IMAP）

详细使用说明见 [docs/cli.md](docs/cli.md)

### 补丁管理工具 (`emx-b4`)

**已完成功能**：
- 补丁主题行解析（`[PATCH v3 2/5]` 格式）
- Trailer 解析与分类（Signed-off-by、Reviewed-by 等）
- Mbox 文件读取和解析
- 补丁系列管理和版本提取
- Follow-up trailer 收集
- Git-am 兼容 mbox 生成
- Prep 分支工作流（创建/reroll/封面/补丁生成）
- Git 操作封装

**命令**：
- `am` - 生成 git-am 可用 mbox
- `shazam` - 直接应用补丁
- `prep` - 管理补丁系列工作流
- `diff` - 比较补丁版本
- `mbox` - 解析 mbox 信息

详细使用说明见 [docs/b4.md](docs/b4.md)

## 快速开始

### 构建项目

```bash
# 构建邮件客户端
go build -o emx-mail.exe ./cmd/cli

# 构建补丁管理工具
go build -o emx-b4.exe ./cmd/b4
```

### 邮件客户端使用

```bash
# 初始化配置
./emx-mail.exe init

# 发送邮件
./emx-mail.exe send -to user@example.com -subject "测试" -text "你好"

# 列出邮件
./emx-mail.exe list

# 获取邮件（包括附件）
./emx-mail.exe fetch -uid 8 -save-attachments ./downloads/
```

### 补丁管理使用

```bash
# 从 mbox 生成 git-am 可用补丁
./emx-b4.exe am -m patches.mbox -o ready.mbox

# 直接应用补丁
./emx-b4.exe shazam -m patches.mbox

# 创建补丁分支
./emx-b4.exe prep new my-feature -b main
```

## 文档

- [邮件客户端详细说明](docs/cli.md)
- [补丁管理工具详细说明](docs/b4.md)

## 依赖

- `github.com/emersion/go-imap/v2` - IMAP 协议实现
- `github.com/emersion/go-message` - 邮件消息格式
- `github.com/emersion/go-sasl` - SASL 认证
- `github.com/emersion/go-smtp` - SMTP 协议实现
- `github.com/emersion/go-mbox` - Mbox 文件格式

## License

MIT
