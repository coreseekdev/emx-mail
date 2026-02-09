package patchwork

import (
	"testing"
)

func TestParseTrailer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantNil  bool
		wantName string
		wantVal  string
		wantType TrailerType
		wantMail string
	}{
		{
			name:     "signed-off-by",
			input:    "Signed-off-by: Author Name <author@example.com>",
			wantName: "Signed-off-by",
			wantVal:  "Author Name <author@example.com>",
			wantType: TrailerPerson,
			wantMail: "author@example.com",
		},
		{
			name:     "reviewed-by",
			input:    "Reviewed-by: Reviewer <reviewer@kernel.org>",
			wantName: "Reviewed-by",
			wantVal:  "Reviewer <reviewer@kernel.org>",
			wantType: TrailerPerson,
			wantMail: "reviewer@kernel.org",
		},
		{
			name:     "acked-by",
			input:    "Acked-by: Maintainer <maint@kernel.org>",
			wantName: "Acked-by",
			wantVal:  "Maintainer <maint@kernel.org>",
			wantType: TrailerPerson,
			wantMail: "maint@kernel.org",
		},
		{
			name:     "link trailer",
			input:    "Link: https://lore.kernel.org/r/12345@host",
			wantName: "Link",
			wantVal:  "https://lore.kernel.org/r/12345@host",
			wantType: TrailerUtility,
		},
		{
			name:     "fixes trailer",
			input:    "Fixes: abc1234 (\"some commit message\")",
			wantName: "Fixes",
			wantVal:  "abc1234 (\"some commit message\")",
			wantType: TrailerUtility,
		},
		{
			name:     "cc trailer",
			input:    "Cc: stable@vger.kernel.org",
			wantName: "Cc",
			wantVal:  "stable@vger.kernel.org",
			wantType: TrailerPerson,
			wantMail: "stable@vger.kernel.org",
		},
		{
			name:     "closes URL",
			input:    "Closes: https://github.com/repo/issues/123",
			wantName: "Closes",
			wantVal:  "https://github.com/repo/issues/123",
			wantType: TrailerUtility,
		},
		{
			name:    "not a trailer",
			input:   "This is just a regular line",
			wantNil: true,
		},
		{
			name:    "empty line",
			input:   "",
			wantNil: true,
		},
		{
			name:    "colon without space",
			input:   "NotTrailer:value",
			wantNil: true,
		},
		{
			name:     "with extinfo hash",
			input:    "Signed-off-by: Author <a@b.com> #extra-info",
			wantName: "Signed-off-by",
			wantVal:  "Author <a@b.com>",
			wantType: TrailerPerson,
			wantMail: "a@b.com",
		},
		{
			name:     "change-id utility",
			input:    "Change-Id: I1234567890abcdef",
			wantName: "Change-Id",
			wantVal:  "I1234567890abcdef",
			wantType: TrailerUtility,
		},
		{
			name:     "中文名 trailer",
			input:    "Signed-off-by: 张三 <zhangsan@example.com>",
			wantName: "Signed-off-by",
			wantVal:  "张三 <zhangsan@example.com>",
			wantType: TrailerPerson,
			wantMail: "zhangsan@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := ParseTrailer(tt.input)

			if tt.wantNil {
				if tr != nil {
					t.Errorf("ParseTrailer() = %v, want nil", tr)
				}
				return
			}

			if tr == nil {
				t.Fatal("ParseTrailer() = nil, want non-nil")
			}

			if tr.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", tr.Name, tt.wantName)
			}
			if tr.Value != tt.wantVal {
				t.Errorf("Value = %q, want %q", tr.Value, tt.wantVal)
			}
			if tr.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", tr.Type, tt.wantType)
			}
			if tt.wantMail != "" && tr.Email != tt.wantMail {
				t.Errorf("Email = %q, want %q", tr.Email, tt.wantMail)
			}
		})
	}
}

func TestParseTrailers(t *testing.T) {
	input := `Signed-off-by: Author <author@example.com>
Reviewed-by: Reviewer <reviewer@kernel.org>
Acked-by: Maintainer <maint@kernel.org>
Link: https://lore.kernel.org/r/12345
`

	trailers := ParseTrailers(input)

	if len(trailers) != 4 {
		t.Fatalf("ParseTrailers() returned %d trailers, want 4", len(trailers))
	}

	expected := []struct {
		name  string
		ttype TrailerType
	}{
		{"Signed-off-by", TrailerPerson},
		{"Reviewed-by", TrailerPerson},
		{"Acked-by", TrailerPerson},
		{"Link", TrailerUtility},
	}

	for i, exp := range expected {
		if trailers[i].Name != exp.name {
			t.Errorf("trailer[%d].Name = %q, want %q", i, trailers[i].Name, exp.name)
		}
		if trailers[i].Type != exp.ttype {
			t.Errorf("trailer[%d].Type = %v, want %v", i, trailers[i].Type, exp.ttype)
		}
	}
}

func TestTrailerEqual(t *testing.T) {
	t1 := &Trailer{Name: "Signed-off-by", Value: "Author <a@b.com>"}
	t2 := &Trailer{Name: "signed-off-by", Value: "Author <a@b.com>"}
	t3 := &Trailer{Name: "Signed-off-by", Value: "Other <o@b.com>"}

	if !t1.Equal(t2) {
		t.Error("Equal() should be true for same name (case-insensitive) and value")
	}
	if t1.Equal(t3) {
		t.Error("Equal() should be false for different values")
	}
}

func TestTrailerString(t *testing.T) {
	tests := []struct {
		trailer  Trailer
		expected string
	}{
		{
			trailer:  Trailer{Name: "Signed-off-by", Value: "Author <a@b.com>"},
			expected: "Signed-off-by: Author <a@b.com>",
		},
		{
			trailer:  Trailer{Name: "Link", Value: "https://example.com", Extinfo: "#v2"},
			expected: "Link: https://example.com #v2",
		},
	}

	for _, tt := range tests {
		result := tt.trailer.String()
		if result != tt.expected {
			t.Errorf("String() = %q, want %q", result, tt.expected)
		}
	}
}

func TestParseMessageBody(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantBody     string
		wantTrailers int
		wantBelow    string
		wantSig      string
		wantPreamble string
	}{
		{
			name: "simple patch body",
			body: `Fix a null pointer dereference in foo_bar().

The function did not check for NULL before accessing
the structure member.

Signed-off-by: Author <author@example.com>
Reviewed-by: Reviewer <reviewer@kernel.org>
---
 drivers/foo.c | 2 ++
 1 file changed, 2 insertions(+)`,
			wantBody:     "Fix a null pointer dereference in foo_bar().\n\nThe function did not check for NULL before accessing\nthe structure member.",
			wantTrailers: 2,
			wantBelow:    " drivers/foo.c | 2 ++\n 1 file changed, 2 insertions(+)",
		},
		{
			name: "body with signature",
			body: `Some change

Signed-off-by: Author <a@b.com>
-- 
Best regards,
Author`,
			wantBody:     "Some change",
			wantTrailers: 1,
			wantSig:      "Best regards,\nAuthor",
		},
		{
			name: "body with preamble",
			body: `From: Real Author <real@example.com>
Subject: Real Subject

Actual commit message

Signed-off-by: Author <a@b.com>`,
			wantPreamble: "From: Real Author <real@example.com>\nSubject: Real Subject",
			wantBody:     "Actual commit message",
			wantTrailers: 1,
		},
		{
			name:         "empty body",
			body:         "",
			wantBody:     "",
			wantTrailers: 0,
		},
		{
			name:         "only trailers",
			body:         "Signed-off-by: Author <a@b.com>",
			wantBody:     "",
			wantTrailers: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := ParseMessageBody(tt.body)

			if parts.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", parts.Body, tt.wantBody)
			}
			if len(parts.Trailers) != tt.wantTrailers {
				t.Errorf("len(Trailers) = %d, want %d", len(parts.Trailers), tt.wantTrailers)
			}
			if tt.wantBelow != "" && parts.Below != tt.wantBelow {
				t.Errorf("Below = %q, want %q", parts.Below, tt.wantBelow)
			}
			if tt.wantSig != "" && parts.Signature != tt.wantSig {
				t.Errorf("Signature = %q, want %q", parts.Signature, tt.wantSig)
			}
			if tt.wantPreamble != "" && parts.Preamble != tt.wantPreamble {
				t.Errorf("Preamble = %q, want %q", parts.Preamble, tt.wantPreamble)
			}
		})
	}
}

func TestParseIntRange(t *testing.T) {
	tests := []struct {
		input   string
		want    []int
		wantErr bool
	}{
		{"1-3", []int{1, 2, 3}, false},
		{"1,3,5", []int{1, 3, 5}, false},
		{"1-3,7,9-11", []int{1, 2, 3, 7, 9, 10, 11}, false},
		{"5", []int{5}, false},
		{"", nil, false},
		{"abc", nil, true},
		{"1-abc", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseIntRange(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseIntRange(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err == nil {
				if len(got) != len(tt.want) {
					t.Errorf("ParseIntRange(%q) = %v, want %v", tt.input, got, tt.want)
					return
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("ParseIntRange(%q)[%d] = %d, want %d", tt.input, i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}
