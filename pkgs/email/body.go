package email

import (
	"io"
	"strings"

	gomessage "github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
)

// parseEntityBody parses a go-message Entity into the Message's TextBody,
// HTMLBody and Attachments fields. It handles both single-part and multipart
// messages (including nested multipart).
//
// This function is used by both IMAPClient and POP3Client to avoid
// duplicating the parsing logic.
func parseEntityBody(msg *Message, entity *gomessage.Entity) {
	if mr := entity.MultipartReader(); mr != nil {
		parseMultipart(msg, mr)
	} else {
		parseSinglePart(msg, entity)
	}
}

// parseMultipart iterates over parts of a multipart message.
func parseMultipart(msg *Message, mr gomessage.MultipartReader) {
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		ct, _, _ := part.Header.ContentType()

		switch {
		case strings.HasPrefix(ct, "text/plain") && msg.TextBody == "":
			if body, err := io.ReadAll(part.Body); err == nil {
				msg.TextBody = string(body)
			}

		case strings.HasPrefix(ct, "text/html") && msg.HTMLBody == "":
			if body, err := io.ReadAll(part.Body); err == nil {
				msg.HTMLBody = string(body)
			}

		case strings.HasPrefix(ct, "multipart/"):
			// Nested multipart — recurse
			if nested := part.MultipartReader(); nested != nil {
				parseMultipart(msg, nested)
			}

		default:
			// Treat as attachment
			body, err := io.ReadAll(part.Body)
			if err != nil {
				continue
			}
			h := mail.AttachmentHeader{Header: part.Header}
			filename, _ := h.Filename()
			msg.Attachments = append(msg.Attachments, Attachment{
				Filename:    filename,
				ContentType: ct,
				Size:        int64(len(body)),
				Data:        body,
			})
		}
	}
}

// parseSinglePart reads the body of a non-multipart entity.
func parseSinglePart(msg *Message, entity *gomessage.Entity) {
	ct, _, _ := entity.Header.ContentType()
	body, err := io.ReadAll(entity.Body)
	if err != nil {
		return
	}
	if strings.HasPrefix(ct, "text/html") {
		msg.HTMLBody = string(body)
	} else {
		msg.TextBody = string(body)
	}
}
