package email

import (
	"time"
)

// Message represents an email message
type Message struct {
	// Envelope
	From    []Address
	To      []Address
	Cc      []Address
	Bcc     []Address
	Subject string
	Date    time.Time

	// Content
	TextBody string
	HTMLBody string

	// Metadata
	MessageID   string
	References  []string
	InReplyTo   string
	Flags       MessageFlag
	Labels      []string
	Attachments []Attachment

	// Server-specific
	UID      uint32
	SeqNum   uint32
	Size     uint32
	Internal bool // Internal flag for POP3
}

// Address represents an email address
type Address struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
}

// Attachment represents an email attachment
type Attachment struct {
	Filename    string
	ContentType string
	Size        int64
	ContentID   string
	// Part contains the actual attachment data (can be large)
	Part interface{}
}

// MessageFlag represents message flags
type MessageFlag struct {
	Seen      bool
	Flagged   bool
	Answered  bool
	Draft     bool
	Deleted   bool
	Recent    bool
}

// SendOptions represents options for sending an email
type SendOptions struct {
	From        Address
	To          []Address
	Cc          []Address
	Bcc         []Address
	Subject     string
	TextBody    string
	HTMLBody    string
	Attachments []AttachmentPath
	InReplyTo   string
	References  []string
}

// AttachmentPath represents a file attachment
type AttachmentPath struct {
	Filename string
	Path     string
}

// FetchOptions represents options for fetching emails
type FetchOptions struct {
	Folder     string
	Limit      int
	MarkAsSeen bool
	DeleteAfterRetrieve bool // For POP3
}

// Folder represents an email folder
type Folder struct {
	Name     string
	ReadOnly bool
	Flags    []string
}

// ListResult represents the result of listing emails
type ListResult struct {
	Messages  []*Message
	Total     int
	Unread    int
	Folder    string
}
