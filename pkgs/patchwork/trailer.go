package patchwork

import (
	"net/mail"
	"regexp"
	"strings"
)

// TrailerType classifies what kind of value a trailer has.
type TrailerType int

const (
	// TrailerPerson is a trailer with a person's name and email (e.g., Signed-off-by).
	TrailerPerson TrailerType = iota
	// TrailerUtility is a link, bug reference, or other automated trailer.
	TrailerUtility
	// TrailerUnknown is a trailer that could not be classified.
	TrailerUnknown
)

// Trailer represents a single email/git trailer line like
// "Signed-off-by: Author Name <author@example.com>".
type Trailer struct {
	// Name is the trailer key (e.g., "Signed-off-by", "Reviewed-by").
	Name string

	// Value is the trailer value (e.g., "Author Name <author@example.com>").
	Value string

	// Email is the email address extracted from the value, if present.
	Email string

	// Type classifies the trailer value.
	Type TrailerType

	// Extinfo contains extra info appended with # or [].
	Extinfo string
}

// Well-known utility trailer names (lowercase for matching).
var utilityTrailers = map[string]bool{
	"fixes":        true,
	"link":         true,
	"buglink":      true,
	"closes":       true,
	"obsoleted-by": true,
	"change-id":    true,
	"based-on":     true,
	"depends-on":   true,
}

// Well-known person trailer names (lowercase for matching).
var personTrailers = map[string]bool{
	"signed-off-by":   true,
	"acked-by":        true,
	"reviewed-by":     true,
	"tested-by":       true,
	"co-developed-by": true,
	"suggested-by":    true,
	"reported-by":     true,
	"cc":              true,
}

var (
	// reTrailerLine matches a trailer line: "Key: value"
	reTrailerLine = regexp.MustCompile(`^\s*([A-Za-z][\w-]+)\s*:\s+(\S.*)$`)

	// reEmailAddr matches something that looks like an email address.
	reEmailAddr = regexp.MustCompile(`\S+@\S+\.\S+`)

	// reExtinfoHash matches # extinfo at end of value.
	reExtinfoHash = regexp.MustCompile(`(.*\S)\s+(#\S+.*)$`)

	// reExtinfoBracket matches [extinfo] on a standalone line.
	reExtinfoBracket = regexp.MustCompile(`^\s*\[[^\]]+\]\s*$`)
)

// ParseTrailer parses a single trailer line into a Trailer struct.
// Returns nil if the line is not a valid trailer.
func ParseTrailer(line string) *Trailer {
	m := reTrailerLine.FindStringSubmatch(line)
	if m == nil {
		return nil
	}

	t := &Trailer{
		Name:  m[1],
		Value: strings.TrimSpace(m[2]),
	}

	// Check for # extinfo
	if em := reExtinfoHash.FindStringSubmatch(t.Value); em != nil {
		t.Value = em[1]
		t.Extinfo = em[2]
	}

	// Classify the trailer
	nameLower := strings.ToLower(t.Name)

	if utilityTrailers[nameLower] || strings.Contains(t.Value, "://") {
		t.Type = TrailerUtility
	} else if personTrailers[nameLower] || reEmailAddr.MatchString(t.Value) {
		t.Type = TrailerPerson
		// Try to extract email
		addr, err := mail.ParseAddress(t.Value)
		if err == nil {
			t.Email = addr.Address
		} else if m := reEmailAddr.FindString(t.Value); m != "" {
			t.Email = strings.Trim(m, "<>")
		}
	} else {
		t.Type = TrailerUnknown
	}

	return t
}

// ParseTrailers extracts all trailers from a block of text.
// It scans line by line and collects consecutive trailer lines.
func ParseTrailers(text string) []*Trailer {
	var trailers []*Trailer
	lines := strings.Split(text, "\n")

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		t := ParseTrailer(line)
		if t == nil {
			continue
		}

		// Check for bracket extinfo on next line
		if i+1 < len(lines) && reExtinfoBracket.MatchString(lines[i+1]) {
			t.Extinfo = strings.TrimSpace(lines[i+1])
			i++
		}

		trailers = append(trailers, t)
	}

	return trailers
}

// String formats the trailer back into its canonical form.
func (t *Trailer) String() string {
	s := t.Name + ": " + t.Value
	if t.Extinfo != "" {
		s += " " + t.Extinfo
	}
	return s
}

// Equal returns true if two trailers have the same name and value
// (case-insensitive name comparison, exact value comparison).
func (t *Trailer) Equal(other *Trailer) bool {
	return strings.EqualFold(t.Name, other.Name) && t.Value == other.Value
}

// MessageBodyParts holds the parsed components of a patch email body,
// following the convention used in git-format-patch output.
type MessageBodyParts struct {
	// Preamble contains From:/Subject: pseudo-headers at the top.
	Preamble string

	// Body is the commit message body.
	Body string

	// Trailers are the trailers from the last paragraph before ---.
	Trailers []*Trailer

	// Below contains the diffstat and diff content below the --- marker.
	Below string

	// Signature is the email signature after "-- \n".
	Signature string
}

// ParseMessageBody splits a patch email body into its component parts:
// preamble, body, trailers, below-the-cut content, and signature.
func ParseMessageBody(body string) *MessageBodyParts {
	parts := &MessageBodyParts{}

	// Normalize line endings
	body = strings.ReplaceAll(body, "\r\n", "\n")

	// Split off signature (-- \n)
	if idx := strings.Index(body, "\n-- \n"); idx >= 0 {
		parts.Signature = body[idx+5:]
		body = body[:idx]
	}

	// Split off below-the-cut content (---\n)
	if idx := findCutLine(body); idx >= 0 {
		parts.Below = body[idx+4:]
		body = body[:idx]
	}

	// Split into paragraphs
	body = strings.TrimSpace(body)
	paragraphs := splitParagraphs(body)

	if len(paragraphs) == 0 {
		return parts
	}

	// Check if first paragraph is preamble (all lines are pseudo-headers)
	if len(paragraphs) > 1 && isPreamble(paragraphs[0]) {
		parts.Preamble = paragraphs[0]
		paragraphs = paragraphs[1:]
	}

	// Check if last paragraph is all trailers
	if len(paragraphs) > 0 {
		lastPara := paragraphs[len(paragraphs)-1]
		trailers := tryParseTrailerBlock(lastPara)
		if len(trailers) > 0 {
			parts.Trailers = trailers
			paragraphs = paragraphs[:len(paragraphs)-1]
		}
	}

	// Remaining paragraphs form the body
	if len(paragraphs) > 0 {
		parts.Body = strings.Join(paragraphs, "\n\n")
	}

	return parts
}

// findCutLine finds the position of a "---" line that marks the cut point.
// Returns -1 if not found. The cut line must be on its own line.
func findCutLine(body string) int {
	lines := strings.Split(body, "\n")
	pos := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			return pos
		}
		pos += len(line) + 1
	}
	return -1
}

// splitParagraphs splits text into paragraphs separated by blank lines.
func splitParagraphs(text string) []string {
	var paragraphs []string
	var current strings.Builder
	blank := true

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if !blank && current.Len() > 0 {
				paragraphs = append(paragraphs, strings.TrimRight(current.String(), "\n"))
				current.Reset()
			}
			blank = true
		} else {
			if blank && current.Len() > 0 {
				paragraphs = append(paragraphs, strings.TrimRight(current.String(), "\n"))
				current.Reset()
			}
			blank = false
			if current.Len() > 0 {
				current.WriteString("\n")
			}
			current.WriteString(line)
		}
	}

	if current.Len() > 0 {
		paragraphs = append(paragraphs, strings.TrimRight(current.String(), "\n"))
	}

	return paragraphs
}

// isPreamble returns true if all lines in the text look like pseudo-headers
// (e.g., "From: ...", "Subject: ...", "Date: ...").
func isPreamble(text string) bool {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		if !reTrailerLine.MatchString(line) {
			return false
		}
	}
	return true
}

// tryParseTrailerBlock tries to parse a paragraph as a block of trailers.
// Returns nil if fewer than half the non-blank lines are trailers.
func tryParseTrailerBlock(text string) []*Trailer {
	lines := strings.Split(text, "\n")
	var trailers []*Trailer
	nonBlank := 0

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		nonBlank++
		if t := ParseTrailer(line); t != nil {
			trailers = append(trailers, t)
		}
	}

	// At least half of non-blank lines should be trailers
	if nonBlank > 0 && len(trailers)*2 >= nonBlank {
		return trailers
	}
	return nil
}
