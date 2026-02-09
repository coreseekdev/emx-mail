// Package patchwork implements email-based patch workflow tools,
// inspired by b4 (https://github.com/mricon/b4).
//
// It provides functionality for parsing patch emails, extracting trailers,
// managing patch series, and applying patches via git.
package patchwork

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// PatchSubject holds the parsed components of an email subject line
// that follows the [PATCH ...] convention used in mailing list workflows.
type PatchSubject struct {
	// Subject is the actual commit subject text after all prefixes.
	Subject string

	// Prefixes contains all text from bracket prefixes (e.g., "PATCH", "RFC").
	Prefixes []string

	// Counter is the patch number in the series (e.g., 2 in [PATCH 2/5]).
	Counter int

	// Expected is the total number of patches (e.g., 5 in [PATCH 2/5]).
	Expected int

	// Revision is the version (e.g., 3 for [PATCH v3]).
	Revision int

	// IsReply indicates the subject had Re:/Aw:/Fwd: prefix.
	IsReply bool

	// IsRFC indicates the [RFC] prefix was present.
	IsRFC bool

	// IsPull indicates the subject was a pull request.
	IsPull bool

	// IsResend indicates the [RESEND] prefix was present.
	IsResend bool
}

var (
	// reReply matches Re:/Aw:/Fwd: prefixes (case-insensitive).
	reReply = regexp.MustCompile(`(?i)^(Re|Aw|Fwd)\s*:`)

	// reGenericReply matches generic 2-3 letter reply prefixes before [.
	reGenericReply = regexp.MustCompile(`(?i)^\w{2,3}:\s*\[`)

	// reBracket matches a [...] prefix block.
	reBracket = regexp.MustCompile(`^\s*\[([^\]]*)\]\s*`)

	// reCounter matches N/M style counter (e.g., "2/5", "02/12").
	reCounter = regexp.MustCompile(`^(\d{1,4})/(\d{1,4})$`)

	// reRevision matches version prefix (e.g., "v3", "V12").
	reRevision = regexp.MustCompile(`(?i)^v(\d+)$`)

	// rePatchVersion matches PATCHvN without space (e.g., "PATCHv3").
	rePatchVersion = regexp.MustCompile(`(?i)^PATCH(v\d+)$`)

	// reNestedBracketInner flattens nested [foo[bar]] → [foobar].
	reNestedBracketInner = regexp.MustCompile(`\[([^\]]*)\[([^\[\]]*)\]`)

	// reNestedBracketOuter flattens [foo]bar] → [foobar].
	reNestedBracketOuter = regexp.MustCompile(`\[([^\]]*)\]([^\[\]]*)\]`)
)

// ParseSubject parses a patch email subject line into its components.
// It handles subjects like:
//
//	"[PATCH v3 RFC 2/5] drivers: fix null pointer dereference"
//	"Re: [PATCH 1/3] some fix"
//	"[PATCH] single patch"
func ParseSubject(subject string) *PatchSubject {
	ps := &PatchSubject{
		Revision: 1,
	}

	// Normalize whitespace
	s := strings.Join(strings.Fields(subject), " ")

	// Detect and strip reply prefix
	if reReply.MatchString(s) {
		ps.IsReply = true
		s = reReply.ReplaceAllString(s, "")
		s = strings.TrimSpace(s)
	} else if reGenericReply.MatchString(s) {
		ps.IsReply = true
		idx := strings.Index(s, "[")
		if idx >= 0 {
			s = s[idx:]
		}
	}

	// Flatten nested brackets
	for i := 0; i < 5; i++ {
		prev := s
		s = reNestedBracketInner.ReplaceAllString(s, "[$1$2]")
		s = reNestedBracketOuter.ReplaceAllString(s, "[$1$2]")
		if s == prev {
			break
		}
	}

	// Parse bracket prefixes
	for {
		loc := reBracket.FindStringSubmatchIndex(s)
		if loc == nil || loc[0] != 0 {
			break
		}

		content := s[loc[2]:loc[3]]
		s = s[loc[1]:]

		// Parse each chunk inside the brackets
		chunks := strings.Fields(content)
		for _, chunk := range chunks {
			upper := strings.ToUpper(chunk)

			switch {
			case reCounter.MatchString(chunk):
				m := reCounter.FindStringSubmatch(chunk)
				ps.Counter, _ = strconv.Atoi(m[1])
				ps.Expected, _ = strconv.Atoi(m[2])

			case reRevision.MatchString(chunk):
				m := reRevision.FindStringSubmatch(chunk)
				ps.Revision, _ = strconv.Atoi(m[1])

			case rePatchVersion.MatchString(chunk):
				// e.g., "PATCHv3" → treat as PATCH + v3
				m := rePatchVersion.FindStringSubmatch(chunk)
				vStr := strings.TrimPrefix(strings.ToLower(m[1]), "v")
				ps.Revision, _ = strconv.Atoi(vStr)
				ps.Prefixes = append(ps.Prefixes, "PATCH")

			case upper == "RFC":
				ps.IsRFC = true
				ps.Prefixes = append(ps.Prefixes, "RFC")

			case upper == "PULL":
				ps.IsPull = true
				ps.Prefixes = append(ps.Prefixes, "PULL")

			case upper == "RESEND":
				ps.IsResend = true
				ps.Prefixes = append(ps.Prefixes, "RESEND")

			default:
				ps.Prefixes = append(ps.Prefixes, chunk)
			}
		}
	}

	ps.Subject = strings.TrimSpace(s)
	return ps
}

// Rebuild reconstructs the subject line with properly formatted prefixes.
// Counter is zero-padded to match the width of Expected (e.g., "02/12").
func (ps *PatchSubject) Rebuild() string {
	var parts []string

	// Collect non-counter, non-revision prefixes
	for _, p := range ps.Prefixes {
		upper := strings.ToUpper(p)
		if upper != "PATCH" {
			continue
		}
		parts = append(parts, p)
	}

	// If no PATCH prefix found, add one
	hasPatch := false
	for _, p := range parts {
		if strings.EqualFold(p, "PATCH") {
			hasPatch = true
			break
		}
	}
	if !hasPatch {
		parts = []string{"PATCH"}
	}

	// Add non-PATCH prefixes (RFC, RESEND, etc.)
	for _, p := range ps.Prefixes {
		upper := strings.ToUpper(p)
		if upper == "PATCH" || upper == "RFC" || upper == "RESEND" || upper == "PULL" {
			continue
		}
		parts = append(parts, p)
	}

	// Add RFC/RESEND/PULL if set
	if ps.IsRFC {
		parts = append(parts, "RFC")
	}
	if ps.IsResend {
		parts = append(parts, "RESEND")
	}
	if ps.IsPull {
		parts = append(parts, "PULL")
	}

	// Add version
	if ps.Revision > 1 {
		parts = append(parts, fmt.Sprintf("v%d", ps.Revision))
	}

	// Add counter
	if ps.Expected > 0 {
		width := len(strconv.Itoa(ps.Expected))
		parts = append(parts, fmt.Sprintf("%0*d/%d", width, ps.Counter, ps.Expected))
	}

	prefix := "[" + strings.Join(parts, " ") + "]"
	if ps.Subject != "" {
		return prefix + " " + ps.Subject
	}
	return prefix
}

// IsCoverLetter returns true if this is a cover letter (patch 0/N).
func (ps *PatchSubject) IsCoverLetter() bool {
	return ps.Counter == 0 && ps.Expected > 0
}

// IsPatch returns true if the subject represents a patch
// (has PATCH prefix and either is a single patch or has a counter > 0).
func (ps *PatchSubject) IsPatch() bool {
	for _, p := range ps.Prefixes {
		if strings.EqualFold(p, "PATCH") {
			return true
		}
	}
	return false
}
