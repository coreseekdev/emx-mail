package patchwork

import (
	"bytes"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-mbox"
)

// AMReadyOptions controls the output of GetAMReady.
type AMReadyOptions struct {
	// AddLink adds a Link: trailer with the message URL.
	AddLink bool

	// LinkPrefix is the URL prefix for Link trailers (e.g., "https://lore.kernel.org/r/").
	LinkPrefix string

	// AddMessageID adds a Message-Id trailer.
	AddMessageID bool

	// ApplyCoverTrailers copies cover letter trailers to all patches.
	ApplyCoverTrailers bool
}

// GetAMReady produces a git-am-ready mbox from the patch series.
// It returns the mbox content as bytes.
func (series *PatchSeries) GetAMReady(opts AMReadyOptions) ([]byte, error) {
	if len(series.Patches) == 0 {
		return nil, fmt.Errorf("no patches in series")
	}

	// Collect cover letter trailers to apply to patches
	var coverTrailers []*Trailer
	if opts.ApplyCoverTrailers && series.CoverLetter != nil {
		coverTrailers = series.CoverLetter.BodyParts.Trailers
	}

	var buf bytes.Buffer
	w := mbox.NewWriter(&buf)

	for _, patch := range series.Patches {
		fromAddr := "unknown@unknown"
		if patch.From != nil {
			fromAddr = patch.From.Address
		}

		msgDate := patch.Date
		if msgDate.IsZero() {
			msgDate = time.Now()
		}

		mw, err := w.CreateMessage(fromAddr, msgDate)
		if err != nil {
			return nil, fmt.Errorf("creating message: %w", err)
		}

		// Build the AM-ready message
		amMsg := buildAMMessage(patch, coverTrailers, opts)
		if _, err := io.WriteString(mw, amMsg); err != nil {
			return nil, fmt.Errorf("writing message: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing mbox writer: %w", err)
	}

	return buf.Bytes(), nil
}

// buildAMMessage constructs a single git-am-ready message from a patch.
func buildAMMessage(patch *PatchMessage, coverTrailers []*Trailer, opts AMReadyOptions) string {
	var b strings.Builder

	// Write headers
	if patch.From != nil {
		b.WriteString(fmt.Sprintf("From: %s\n", formatAddress(patch.From)))
	}
	if !patch.Date.IsZero() {
		b.WriteString(fmt.Sprintf("Date: %s\n", patch.Date.Format(time.RFC1123Z)))
	}
	b.WriteString(fmt.Sprintf("Subject: %s\n", patch.Parsed.Rebuild()))
	if patch.MessageID != "" {
		b.WriteString(fmt.Sprintf("Message-Id: <%s>\n", patch.MessageID))
	}
	b.WriteString("\n")

	// Write preamble if present
	if patch.BodyParts.Preamble != "" {
		b.WriteString(patch.BodyParts.Preamble)
		b.WriteString("\n\n")
	}

	// Write body
	if patch.BodyParts.Body != "" {
		b.WriteString(patch.BodyParts.Body)
		b.WriteString("\n")
	}

	// Collect all trailers
	allTrailers := make([]*Trailer, 0)

	// Original trailers from the patch
	allTrailers = append(allTrailers, patch.BodyParts.Trailers...)

	// Follow-up trailers
	allTrailers = append(allTrailers, patch.FollowupTrailers...)

	// Cover letter trailers
	for _, ct := range coverTrailers {
		found := false
		for _, t := range allTrailers {
			if t.Equal(ct) {
				found = true
				break
			}
		}
		if !found {
			allTrailers = append(allTrailers, ct)
		}
	}

	// Add Link trailer
	if opts.AddLink && patch.MessageID != "" && opts.LinkPrefix != "" {
		linkTrailer := &Trailer{
			Name:  "Link",
			Value: opts.LinkPrefix + patch.MessageID,
			Type:  TrailerUtility,
		}
		allTrailers = append(allTrailers, linkTrailer)
	}

	// Add Message-Id trailer
	if opts.AddMessageID && patch.MessageID != "" {
		msgIdTrailer := &Trailer{
			Name:  "Message-Id",
			Value: fmt.Sprintf("<%s>", patch.MessageID),
			Type:  TrailerUtility,
		}
		allTrailers = append(allTrailers, msgIdTrailer)
	}

	// Write trailers
	if len(allTrailers) > 0 {
		b.WriteString("\n")
		for _, t := range allTrailers {
			b.WriteString(t.String())
			b.WriteString("\n")
		}
	}

	// Write below-the-cut content
	if patch.BodyParts.Below != "" {
		b.WriteString("---\n")
		b.WriteString(patch.BodyParts.Below)
	}

	return b.String()
}

// formatAddress formats a mail.Address to a string.
func formatAddress(addr *mail.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s>", addr.Name, addr.Address)
	}
	return addr.Address
}

// WriteSeries writes a patch series as a git-am-ready mbox to the writer.
func WriteSeries(w io.Writer, series *PatchSeries, opts AMReadyOptions) error {
	data, err := series.GetAMReady(opts)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
