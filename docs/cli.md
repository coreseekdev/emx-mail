# emx-mail CLI 使用说明

命令行邮件客户端，支持 IMAP/POP3 收件、SMTP 发件，支持附件和中文。

## 安装

```bash
cd emx-mail
go build -o emx-mail ./cmd/cli/
```

## 全局选项

| 选项 | 说明 |
|------|------|
| `-account <名称>` | 使用指定账户（按名称或邮箱匹配） |
| `-v` | 详细输出 |
| `-version` | 显示版本 |

## 配置读取优先级

1. 如果系统存在 `emx-config`，则通过 `emx-config list --json` 读取配置。
2. 如果不存在 `emx-config`，则从环境变量 `EMX_MAIL_CONFIG_JSON` 指定的 JSON 文件读取。

## 命令一览

```
emx-mail [全局选项] <命令> [命令选项]
```

---

### init — 初始化配置

生成新版格式的示例配置。

```bash
emx-mail init
```

当系统存在 `emx-config` 时，`init` 会输出示例 JSON（用于写入 emx-config 的配置文件）。
当系统不存在 `emx-config` 时，请先设置环境变量：

```bash
set EMX_MAIL_CONFIG_JSON= C:\path\to\emx-mail.json
```

配置文件结构（JSON，包含 `mail` 根节点）：

```json
{
  "mail": {
    "default_account": "work",
    "accounts": {
      "work": {
        "name": "工作邮箱",
        "email": "user@example.com",
        "from_name": "张三",
        "imap": { "host": "imap.example.com", "port": 993, "username": "user", "password": "pass", "ssl": true },
        "smtp": { "host": "smtp.example.com", "port": 587, "username": "user", "password": "pass", "starttls": true },
        "pop3": { "host": "pop3.example.com", "port": 995, "username": "user", "password": "pass", "ssl": true }
      }
    }
  }
}
```

> POP3 和 IMAP 配置一个即可。两者都配时默认使用 IMAP。

---

### send — 发送邮件

```bash
# 纯文本邮件
emx-mail send -to user@example.com -subject "你好" -text "这是正文"

# 带抄送
emx-mail send -to a@x.com -cc b@x.com,c@x.com -subject "会议通知" -text "明天下午3点"

# 带附件
emx-mail send -to user@example.com -subject "报告" -text "请查收" -attachment report.pdf

# 回复邮件
emx-mail send -to user@example.com -subject "Re: 原始主题" -text "收到" \
  -in-reply-to "<original-msg-id@example.com>"

# HTML 邮件
emx-mail send -to user@example.com -subject "通知" -html "<h1>标题</h1><p>内容</p>"
```

| 选项 | 必须 | 说明 |
|------|------|------|
| `-to <邮箱>` | ✓ | 收件人，逗号分隔多个 |
| `-subject <主题>` | ✓ | 邮件主题 |
| `-text <正文>` | ✓* | 纯文本正文（与 `-html` 二选一） |
| `-html <HTML>` | ✓* | HTML 正文 |
| `-cc <邮箱>` | | 抄送 |
| `-attachment <路径>` | | 附件文件路径 |
| `-in-reply-to <ID>` | | 回复的 Message-ID |

---

### list — 列出邮件

```bash
# 列出收件箱（默认 20 封）
emx-mail list

# 显示详细预览
emx-mail -v list

# 限制数量
emx-mail list -limit 5

# 指定文件夹（仅 IMAP）
emx-mail list -folder "Sent"

# 仅未读
emx-mail list -unread-only

# 强制使用 POP3
emx-mail list -protocol pop3
```

输出示例：

```
Protocol: IMAP | Folder: INBOX
Total: 128, Unread: 3

[1] UID:4567 ✗ From: 李四 <lisi@example.com>
    Subject: 项目进展汇报
    Date: Mon, 09 Feb 2026 10:30:00 CST
    Message-ID: <abc123@example.com>

[2] UID:4566 ✓ From: admin@example.com
    Subject: System notification
    Date: Sun, 08 Feb 2026 22:00:00 CST
    Message-ID: <def456@example.com>
```

> `✗` = 未读, `✓` = 已读

---

### fetch — 查看邮件

```bash
# 查看邮件内容
emx-mail fetch -uid 4567

# 查看 HTML 版本
emx-mail fetch -uid 4567 -format html

# 保存到文件
emx-mail fetch -uid 4567 -output email.txt

# 指定文件夹
emx-mail fetch -uid 4567 -folder "Archive"

# 保存附件到目录
emx-mail fetch -uid 4567 -save-attachments ./attachments/

# POP3 方式
emx-mail fetch -uid 3 -protocol pop3
```

| 选项 | 必须 | 说明 |
|------|------|------|
| `-uid <UID>` | ✓ | 邮件 UID（IMAP）或序号（POP3） |
| `-folder <名称>` | | 文件夹（默认 INBOX） |
| `-format <格式>` | | `text`（默认）或 `html` |
| `-output <路径>` | | 输出到文件（默认 stdout） |
| `-save-attachments <目录>` | | 保存附件到指定目录 |
| `-protocol <协议>` | | 强制 `imap` 或 `pop3` |

---

### delete — 删除邮件

```bash
# 标记删除
emx-mail delete -uid 4567

# 永久删除（IMAP expunge）
emx-mail delete -uid 4567 -expunge

# POP3 删除（立即生效）
emx-mail delete -uid 3 -protocol pop3
```

| 选项 | 必须 | 说明 |
|------|------|------|
| `-uid <UID>` | ✓ | 邮件 UID |
| `-folder <名称>` | | 文件夹（默认 INBOX） |
| `-expunge` | | 永久删除（仅 IMAP） |
| `-protocol <协议>` | | 强制 `imap` 或 `pop3` |

---

### folders — 列出文件夹

```bash
emx-mail folders
```

输出示例：

```
Folders:
  INBOX
  Sent
  Drafts
  Trash
  Archive
```

> POP3 不支持文件夹，仅有 INBOX。

---

## 多账户使用

```bash
# 使用默认账户
emx-mail list

# 按名称选择
emx-mail -account "工作邮箱" list

# 按邮箱选择
emx-mail -account user@example.com list
```

## 典型工作流

```bash
# 1. 初始化
emx-mail init
# 编辑 ~/.emx-mail/config.json

# 2. 查看收件箱
emx-mail list

# 3. 阅读某封邮件
emx-mail fetch -uid 100

# 4. 下载附件
emx-mail fetch -uid 100 -save-attachments ./downloads/

# 5. 回复
emx-mail send -to sender@example.com -subject "Re: 原标题" -text "已收到，谢谢"

# 6. 删除垃圾邮件
emx-mail delete -uid 99 -expunge
```
