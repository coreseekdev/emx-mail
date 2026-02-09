package patchwork

import (
	"fmt"
	"io"
	"mime"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-mbox"
)

// PatchMessage represents a single parsed email message from a patch thread.
type PatchMessage struct {
	// MessageID is the Message-ID header value.
	MessageID string

	// InReplyTo is the In-Reply-To header value.
	InReplyTo string

	// References contains all message IDs from the References header.
	References []string

	// From is the parsed sender address.
	From *mail.Address

	// Date is the parsed date.
	Date time.Time

	// Subject is the raw subject line.
	RawSubject string

	// Parsed is the parsed subject components.
	Parsed *PatchSubject

	// Body is the raw message body text.
	Body string

	// BodyParts is the parsed body structure.
	BodyParts *MessageBodyParts

	// FollowupTrailers contains trailers found in follow-up replies.
	FollowupTrailers []*Trailer

	// Diff contains the patch diff, if present.
	Diff string

	// HasDiff indicates whether the message contains a diff.
	HasDiff bool
}

// PatchSeries represents a collection of related patches at a specific revision.
type PatchSeries struct {
	// Revision is the patch series version (v1, v2, etc.).
	Revision int

	// CoverLetter is the cover letter message, if present.
	CoverLetter *PatchMessage

	// Patches contains the ordered patch messages (index 0 = patch 1).
	Patches []*PatchMessage

	// Expected is the total expected number of patches.
	Expected int

	// Followups contains reply messages with trailers.
	Followups []*PatchMessage

	// Complete indicates whether all expected patches are present.
	Complete bool
}

// Mailbox holds all messages from a patch thread and organizes them
// into series by revision.
type Mailbox struct {
	// Messages contains all parsed messages.
	Messages []*PatchMessage

	// Series maps revision number to the patch series.
	Series map[int]*PatchSeries

	// Unknowns contains messages that couldn't be classified.
	Unknowns []*PatchMessage
}

// NewMailbox creates a new empty Mailbox.
func NewMailbox() *Mailbox {
	return &Mailbox{
		Series: make(map[int]*PatchSeries),
	}
}

// AddMessage parses and adds an email message to the mailbox.
func (mb *Mailbox) AddMessage(msg *mail.Message) error {
	pm, err := parseMailMessage(msg)
	if err != nil {
		return fmt.Errorf("parsing message: %w", err)
	}

	mb.Messages = append(mb.Messages, pm)

	// Classify the message
	if pm.Parsed.IsReply && !pm.HasDiff {
		// It's a follow-up reply — extract trailers for the referenced patch
		pm.FollowupTrailers = ParseTrailers(pm.Body)
		// Find which series this reply belongs to and add as followup
		for _, series := range mb.Series {
			series.Followups = append(series.Followups, pm)
		}
		return nil
	}

	rev := pm.Parsed.Revision
	series, ok := mb.Series[rev]
	if !ok {
		series = &PatchSeries{Revision: rev}
		mb.Series[rev] = series
	}

	if pm.Parsed.IsCoverLetter() {
		series.CoverLetter = pm
		if pm.Parsed.Expected > series.Expected {
			series.Expected = pm.Parsed.Expected
		}
		return nil
	}

	if pm.HasDiff || pm.Parsed.IsPatch() {
		if pm.Parsed.Expected > series.Expected {
			series.Expected = pm.Parsed.Expected
		}
		series.Patches = append(series.Patches, pm)
		return nil
	}

	mb.Unknowns = append(mb.Unknowns, pm)
	return nil
}

// ReadMbox reads an mbox file and adds all messages to the mailbox.
func (mb *Mailbox) ReadMbox(r io.Reader) error {
	mr := mbox.NewReader(r)

	for {
		msgReader, err := mr.NextMessage()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading mbox message: %w", err)
		}

		msg, err := mail.ReadMessage(msgReader)
		if err != nil {
			return fmt.Errorf("parsing mail message: %w", err)
		}

		if err := mb.AddMessage(msg); err != nil {
			return err
		}
	}

	return nil
}

// GetSeries returns the patch series for the given revision.
// If revision is 0, returns the latest revision.
func (mb *Mailbox) GetSeries(revision int) *PatchSeries {
	if revision == 0 {
		// Find the latest revision
		maxRev := 0
		for rev := range mb.Series {
			if rev > maxRev {
				maxRev = rev
			}
		}
		revision = maxRev
	}

	series := mb.Series[revision]
	if series == nil {
		return nil
	}

	// Sort patches by counter
	sort.Slice(series.Patches, func(i, j int) bool {
		return series.Patches[i].Parsed.Counter < series.Patches[j].Parsed.Counter
	})

	// Check completeness
	if series.Expected > 0 {
		series.Complete = len(series.Patches) == series.Expected
	} else if len(series.Patches) == 1 {
		series.Complete = true
	}

	return series
}

// GetLatestSeries returns the latest revision of the patch series
// with follow-up trailers applied.
func (mb *Mailbox) GetLatestSeries() *PatchSeries {
	series := mb.GetSeries(0)
	if series == nil {
		return nil
	}

	// Apply follow-up trailers
	mb.applyFollowupTrailers(series)

	return series
}

// applyFollowupTrailers matches follow-up replies to their target patches
// and appends any new trailers.
func (mb *Mailbox) applyFollowupTrailers(series *PatchSeries) {
	// Build a map from message-id to patch
	patchByMsgID := make(map[string]*PatchMessage)
	for _, p := range series.Patches {
		patchByMsgID[p.MessageID] = p
	}
	if series.CoverLetter != nil {
		patchByMsgID[series.CoverLetter.MessageID] = series.CoverLetter
	}

	// For each followup, walk the in-reply-to chain to find the target patch
	for _, fu := range series.Followups {
		if len(fu.FollowupTrailers) == 0 {
			continue
		}

		// Find the target patch by walking in-reply-to
		targetID := fu.InReplyTo
		target := patchByMsgID[targetID]

		if target == nil {
			// If we can't find it, try checking References
			for _, ref := range fu.References {
				if p, ok := patchByMsgID[ref]; ok {
					target = p
					break
				}
			}
		}

		if target == nil {
			continue
		}

		// Add new trailers that don't already exist
		for _, ft := range fu.FollowupTrailers {
			found := false
			for _, et := range target.BodyParts.Trailers {
				if et.Equal(ft) {
					found = true
					break
				}
			}
			if !found {
				target.BodyParts.Trailers = append(target.BodyParts.Trailers, ft)
			}
		}
	}
}

// parseMailMessage converts a standard library mail.Message into a PatchMessage.
func parseMailMessage(msg *mail.Message) (*PatchMessage, error) {
	pm := &PatchMessage{}

	// Parse headers
	pm.MessageID = cleanMessageID(msg.Header.Get("Message-Id"))
	pm.InReplyTo = cleanMessageID(msg.Header.Get("In-Reply-To"))
	pm.References = parseReferences(msg.Header.Get("References"))

	// Parse From
	fromStr := msg.Header.Get("From")
	if fromStr != "" {
		// Decode MIME-encoded words
		dec := new(mime.WordDecoder)
		decoded, err := dec.DecodeHeader(fromStr)
		if err == nil {
			fromStr = decoded
		}
		addr, err := mail.ParseAddress(fromStr)
		if err == nil {
			pm.From = addr
		}
	}

	// Parse Date
	dateStr := msg.Header.Get("Date")
	if dateStr != "" {
		if t, err := mail.ParseDate(dateStr); err == nil {
			pm.Date = t
		}
	}

	// Parse Subject (decode MIME-encoded words)
	pm.RawSubject = msg.Header.Get("Subject")
	if pm.RawSubject != "" {
		dec := new(mime.WordDecoder)
		decoded, err := dec.DecodeHeader(pm.RawSubject)
		if err == nil {
			pm.RawSubject = decoded
		}
	}
	pm.Parsed = ParseSubject(pm.RawSubject)

	// Read body
	bodyBytes, err := io.ReadAll(msg.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}
	pm.Body = string(bodyBytes)

	// Parse body parts
	pm.BodyParts = ParseMessageBody(pm.Body)

	// Detect diff
	pm.Diff, pm.HasDiff = extractDiff(pm.Body)

	return pm, nil
}

// extractDiff extracts the unified diff content from a message body.
func extractDiff(body string) (string, bool) {
	lines := strings.Split(body, "\n")
	var diffLines []string
	inDiff := false

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			inDiff = true
		}
		if inDiff {
			diffLines = append(diffLines, line)
		}
	}

	if len(diffLines) > 0 {
		return strings.Join(diffLines, "\n"), true
	}
	return "", false
}

// cleanMessageID strips angle brackets from a Message-ID.
func cleanMessageID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "<")
	id = strings.TrimSuffix(id, ">")
	return id
}

// parseReferences splits a References header into individual message IDs.
func parseReferences(refs string) []string {
	if refs == "" {
		return nil
	}

	var result []string
	for _, ref := range strings.Fields(refs) {
		cleaned := cleanMessageID(ref)
		if cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}
