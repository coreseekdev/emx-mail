package patchwork

import (
	"testing"
)

func TestParseSubject(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		counter  int
		expected int
		revision int
		isReply  bool
		isRFC    bool
		isPull   bool
		isResend bool
		subject  string
	}{
		{
			name:     "simple patch",
			input:    "[PATCH] fix null pointer",
			counter:  0,
			expected: 0,
			revision: 1,
			subject:  "fix null pointer",
		},
		{
			name:     "numbered patch",
			input:    "[PATCH 2/5] drivers: fix bug",
			counter:  2,
			expected: 5,
			revision: 1,
			subject:  "drivers: fix bug",
		},
		{
			name:     "versioned patch",
			input:    "[PATCH v3 2/5] drivers: fix bug",
			counter:  2,
			expected: 5,
			revision: 3,
			subject:  "drivers: fix bug",
		},
		{
			name:     "cover letter",
			input:    "[PATCH v2 0/3] My patch series",
			counter:  0,
			expected: 3,
			revision: 2,
			subject:  "My patch series",
		},
		{
			name:     "RFC patch",
			input:    "[PATCH RFC v3 3/12] This is a patch",
			counter:  3,
			expected: 12,
			revision: 3,
			isRFC:    true,
			subject:  "This is a patch",
		},
		{
			name:     "reply to patch",
			input:    "Re: [PATCH 1/3] some fix",
			counter:  1,
			expected: 3,
			revision: 1,
			isReply:  true,
			subject:  "some fix",
		},
		{
			name:     "Aw reply",
			input:    "Aw: [PATCH v2 1/5] another fix",
			counter:  1,
			expected: 5,
			revision: 2,
			isReply:  true,
			subject:  "another fix",
		},
		{
			name:     "Fwd prefix",
			input:    "Fwd: [PATCH] forwarded patch",
			counter:  0,
			expected: 0,
			revision: 1,
			isReply:  true,
			subject:  "forwarded patch",
		},
		{
			name:     "RESEND patch",
			input:    "[PATCH RESEND 1/2] fix regression",
			counter:  1,
			expected: 2,
			revision: 1,
			isResend: true,
			subject:  "fix regression",
		},
		{
			name:     "no bracket",
			input:    "Just a normal subject",
			counter:  0,
			expected: 0,
			revision: 1,
			subject:  "Just a normal subject",
		},
		{
			name:     "PATCHv3 merged",
			input:    "[PATCHv3 2/5] merged version",
			counter:  2,
			expected: 5,
			revision: 3,
			subject:  "merged version",
		},
		{
			name:     "large series",
			input:    "[PATCH v2 03/12] padded counter",
			counter:  3,
			expected: 12,
			revision: 2,
			subject:  "padded counter",
		},
		{
			name:     "PULL request",
			input:    "[PULL] Please pull my changes",
			counter:  0,
			expected: 0,
			revision: 1,
			isPull:   true,
			subject:  "Please pull my changes",
		},
		{
			name:     "extra whitespace",
			input:    "  [PATCH   v2   1/3]   spaced  out  ",
			counter:  1,
			expected: 3,
			revision: 2,
			subject:  "spaced out",
		},
		{
			name:     "中文主题",
			input:    "[PATCH 1/2] 修复空指针问题",
			counter:  1,
			expected: 2,
			revision: 1,
			subject:  "修复空指针问题",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := ParseSubject(tt.input)

			if ps.Counter != tt.counter {
				t.Errorf("Counter = %d, want %d", ps.Counter, tt.counter)
			}
			if ps.Expected != tt.expected {
				t.Errorf("Expected = %d, want %d", ps.Expected, tt.expected)
			}
			if ps.Revision != tt.revision {
				t.Errorf("Revision = %d, want %d", ps.Revision, tt.revision)
			}
			if ps.IsReply != tt.isReply {
				t.Errorf("IsReply = %v, want %v", ps.IsReply, tt.isReply)
			}
			if ps.IsRFC != tt.isRFC {
				t.Errorf("IsRFC = %v, want %v", ps.IsRFC, tt.isRFC)
			}
			if ps.IsPull != tt.isPull {
				t.Errorf("IsPull = %v, want %v", ps.IsPull, tt.isPull)
			}
			if ps.IsResend != tt.isResend {
				t.Errorf("IsResend = %v, want %v", ps.IsResend, tt.isResend)
			}
			if ps.Subject != tt.subject {
				t.Errorf("Subject = %q, want %q", ps.Subject, tt.subject)
			}
		})
	}
}

func TestParseSubjectRebuild(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple patch rebuilds",
			input:    "[PATCH] fix bug",
			expected: "[PATCH] fix bug",
		},
		{
			name:     "numbered patch zero-pads",
			input:    "[PATCH 3/12] fix bug",
			expected: "[PATCH 03/12] fix bug",
		},
		{
			name:     "RFC patch",
			input:    "[PATCH RFC v3 3/12] fix bug",
			expected: "[PATCH RFC v3 03/12] fix bug",
		},
		{
			name:     "version 1 omitted",
			input:    "[PATCH 1/3] something",
			expected: "[PATCH 1/3] something",
		},
		{
			name:     "version 2 included",
			input:    "[PATCH v2 1/3] something",
			expected: "[PATCH v2 1/3] something",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := ParseSubject(tt.input)
			rebuilt := ps.Rebuild()
			if rebuilt != tt.expected {
				t.Errorf("Rebuild() = %q, want %q", rebuilt, tt.expected)
			}
		})
	}
}

func TestIsCoverLetter(t *testing.T) {
	tests := []struct {
		input  string
		isCL   bool
	}{
		{"[PATCH 0/3] Cover letter", true},
		{"[PATCH v2 0/5] Cover letter", true},
		{"[PATCH 1/3] Not cover", false},
		{"[PATCH] Single patch", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ps := ParseSubject(tt.input)
			if ps.IsCoverLetter() != tt.isCL {
				t.Errorf("IsCoverLetter() = %v, want %v", ps.IsCoverLetter(), tt.isCL)
			}
		})
	}
}

func TestIsPatch(t *testing.T) {
	tests := []struct {
		input   string
		isPatch bool
	}{
		{"[PATCH 1/3] A patch", true},
		{"[PATCH] Single patch", true},
		{"[PATCH RFC 1/2] RFC patch", true},
		{"Re: [PATCH 1/3] reply", true},
		{"Not a patch at all", false},
		{"[RFC] Just an RFC", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ps := ParseSubject(tt.input)
			if ps.IsPatch() != tt.isPatch {
				t.Errorf("IsPatch() = %v, want %v", ps.IsPatch(), tt.isPatch)
			}
		})
	}
}
