package patchwork

import (
	"bytes"
	"net/mail"
	"strings"
	"testing"
)

// buildTestMbox creates a simple mbox string from messages for testing.
func buildTestMbox(messages ...string) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString("From test@test Mon Jan  1 00:00:00 2024\n")
		b.WriteString(msg)
		b.WriteString("\n\n")
	}
	return b.String()
}

func TestMailboxReadSinglePatch(t *testing.T) {
	mboxData := buildTestMbox(
		`From: Author <author@example.com>
Date: Mon, 01 Jan 2024 00:00:00 +0000
Subject: [PATCH] Fix null pointer dereference
Message-Id: <patch1@example.com>

Fix a null pointer dereference in foo().

Signed-off-by: Author <author@example.com>
---
 foo.c | 1 +
 1 file changed, 1 insertion(+)

diff --git a/foo.c b/foo.c
index 1234567..abcdefg 100644
--- a/foo.c
+++ b/foo.c
@@ -10,6 +10,7 @@ void foo(struct bar *b)
+	if (!b) return;
`)

	mb := NewMailbox()
	err := mb.ReadMbox(strings.NewReader(mboxData))
	if err != nil {
		t.Fatalf("ReadMbox() error = %v", err)
	}

	if len(mb.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(mb.Messages))
	}

	series := mb.GetSeries(0)
	if series == nil {
		t.Fatal("GetSeries(0) returned nil")
	}

	if len(series.Patches) != 1 {
		t.Fatalf("len(Patches) = %d, want 1", len(series.Patches))
	}

	patch := series.Patches[0]
	if patch.Parsed.Subject != "Fix null pointer dereference" {
		t.Errorf("Subject = %q, want %q", patch.Parsed.Subject, "Fix null pointer dereference")
	}

	if !patch.HasDiff {
		t.Error("HasDiff should be true")
	}

	if patch.MessageID != "patch1@example.com" {
		t.Errorf("MessageID = %q, want %q", patch.MessageID, "patch1@example.com")
	}
}

func TestMailboxReadPatchSeries(t *testing.T) {
	mboxData := buildTestMbox(
		// Cover letter
		`From: Author <author@example.com>
Date: Mon, 01 Jan 2024 00:00:00 +0000
Subject: [PATCH v2 0/2] Fix null pointer issues
Message-Id: <cover@example.com>

This series fixes null pointer dereferences.

Author (2):
  Fix null pointer in foo
  Fix null pointer in bar

 foo.c | 1 +
 bar.c | 1 +
 2 files changed, 2 insertions(+)

Signed-off-by: Author <author@example.com>`,
		// Patch 1
		`From: Author <author@example.com>
Date: Mon, 01 Jan 2024 00:00:01 +0000
Subject: [PATCH v2 1/2] Fix null pointer in foo
Message-Id: <patch1@example.com>
In-Reply-To: <cover@example.com>

Fix null pointer in foo().

Signed-off-by: Author <author@example.com>
---
diff --git a/foo.c b/foo.c
index 1234567..abcdefg 100644
--- a/foo.c
+++ b/foo.c
@@ -1 +1,2 @@
+	if (!ptr) return;`,
		// Patch 2
		`From: Author <author@example.com>
Date: Mon, 01 Jan 2024 00:00:02 +0000
Subject: [PATCH v2 2/2] Fix null pointer in bar
Message-Id: <patch2@example.com>
In-Reply-To: <cover@example.com>

Fix null pointer in bar().

Signed-off-by: Author <author@example.com>
---
diff --git a/bar.c b/bar.c
index 1234567..abcdefg 100644
--- a/bar.c
+++ b/bar.c
@@ -1 +1,2 @@
+	if (!ptr) return;`,
	)

	mb := NewMailbox()
	err := mb.ReadMbox(strings.NewReader(mboxData))
	if err != nil {
		t.Fatalf("ReadMbox() error = %v", err)
	}

	if len(mb.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(mb.Messages))
	}

	series := mb.GetSeries(2)
	if series == nil {
		t.Fatal("GetSeries(2) returned nil")
	}

	if series.Revision != 2 {
		t.Errorf("Revision = %d, want 2", series.Revision)
	}

	if series.CoverLetter == nil {
		t.Fatal("CoverLetter is nil")
	}

	if series.CoverLetter.Parsed.Subject != "Fix null pointer issues" {
		t.Errorf("CoverLetter subject = %q, want %q",
			series.CoverLetter.Parsed.Subject, "Fix null pointer issues")
	}

	if len(series.Patches) != 2 {
		t.Fatalf("len(Patches) = %d, want 2", len(series.Patches))
	}

	if !series.Complete {
		t.Error("series should be Complete")
	}

	// Check ordering
	if series.Patches[0].Parsed.Subject != "Fix null pointer in foo" {
		t.Errorf("Patches[0] subject = %q", series.Patches[0].Parsed.Subject)
	}
	if series.Patches[1].Parsed.Subject != "Fix null pointer in bar" {
		t.Errorf("Patches[1] subject = %q", series.Patches[1].Parsed.Subject)
	}
}

func TestMailboxFollowupTrailers(t *testing.T) {
	mboxData := buildTestMbox(
		// Original patch
		`From: Author <author@example.com>
Date: Mon, 01 Jan 2024 00:00:00 +0000
Subject: [PATCH 1/1] Fix bug
Message-Id: <patch@example.com>

Fix a bug.

Signed-off-by: Author <author@example.com>
---
diff --git a/a.c b/a.c
index 1234567..abcdefg 100644
--- a/a.c
+++ b/a.c
@@ -1 +1,2 @@
+fix`,
		// Follow-up review
		`From: Reviewer <reviewer@example.com>
Date: Mon, 01 Jan 2024 01:00:00 +0000
Subject: Re: [PATCH 1/1] Fix bug
Message-Id: <review@example.com>
In-Reply-To: <patch@example.com>

Looks good!

Reviewed-by: Reviewer <reviewer@example.com>`,
	)

	mb := NewMailbox()
	err := mb.ReadMbox(strings.NewReader(mboxData))
	if err != nil {
		t.Fatalf("ReadMbox() error = %v", err)
	}

	series := mb.GetLatestSeries()
	if series == nil {
		t.Fatal("GetLatestSeries() returned nil")
	}

	if len(series.Patches) != 1 {
		t.Fatalf("len(Patches) = %d, want 1", len(series.Patches))
	}

	patch := series.Patches[0]
	totalTrailers := len(patch.BodyParts.Trailers)

	// Should have original Signed-off-by plus follow-up Reviewed-by
	if totalTrailers != 2 {
		t.Errorf("total trailers = %d, want 2", totalTrailers)
		for _, tr := range patch.BodyParts.Trailers {
			t.Logf("  trailer: %s", tr.String())
		}
	}
}

func TestMailboxMultipleRevisions(t *testing.T) {
	mboxData := buildTestMbox(
		// v1 patch
		`From: Author <author@example.com>
Date: Mon, 01 Jan 2024 00:00:00 +0000
Subject: [PATCH 1/1] Fix bug v1
Message-Id: <v1@example.com>

First version

Signed-off-by: Author <author@example.com>
---
diff --git a/a.c b/a.c
--- a/a.c
+++ b/a.c
@@ -1 +1 @@
-old
+new`,
		// v2 patch
		`From: Author <author@example.com>
Date: Tue, 02 Jan 2024 00:00:00 +0000
Subject: [PATCH v2 1/1] Fix bug v2
Message-Id: <v2@example.com>

Second version, improved

Signed-off-by: Author <author@example.com>
---
diff --git a/a.c b/a.c
--- a/a.c
+++ b/a.c
@@ -1 +1 @@
-old
+better_new`,
	)

	mb := NewMailbox()
	if err := mb.ReadMbox(strings.NewReader(mboxData)); err != nil {
		t.Fatalf("ReadMbox() error = %v", err)
	}

	// Should have 2 series
	if len(mb.Series) != 2 {
		t.Fatalf("len(Series) = %d, want 2", len(mb.Series))
	}

	// Latest should be v2
	latest := mb.GetSeries(0)
	if latest == nil || latest.Revision != 2 {
		t.Errorf("latest series revision = %v, want 2", latest)
	}

	// v1 should also be available
	v1 := mb.GetSeries(1)
	if v1 == nil || v1.Revision != 1 {
		t.Errorf("v1 series = %v", v1)
	}
}

func TestParseMailMessage(t *testing.T) {
	raw := `From: 测试者 <test@example.com>
Date: Mon, 01 Jan 2024 00:00:00 +0000
Subject: [PATCH v3 1/5] drivers: fix regression
Message-Id: <test-msg-id@example.com>
In-Reply-To: <cover@example.com>
References: <cover@example.com> <parent@example.com>

Fix a regression introduced in commit abc123.

Signed-off-by: 测试者 <test@example.com>
---
 drivers/test.c | 2 +-
 1 file changed, 1 insertion(+), 1 deletion(-)

diff --git a/drivers/test.c b/drivers/test.c
index 1234567..abcdefg 100644
--- a/drivers/test.c
+++ b/drivers/test.c
@@ -10,7 +10,7 @@ static int test_init(void)
-	old_code();
+	new_code();
`

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}

	pm, err := parseMailMessage(msg)
	if err != nil {
		t.Fatalf("parseMailMessage() error = %v", err)
	}

	if pm.MessageID != "test-msg-id@example.com" {
		t.Errorf("MessageID = %q", pm.MessageID)
	}

	if pm.InReplyTo != "cover@example.com" {
		t.Errorf("InReplyTo = %q", pm.InReplyTo)
	}

	if len(pm.References) != 2 {
		t.Errorf("len(References) = %d, want 2", len(pm.References))
	}

	if pm.Parsed.Revision != 3 {
		t.Errorf("Revision = %d, want 3", pm.Parsed.Revision)
	}

	if pm.Parsed.Counter != 1 {
		t.Errorf("Counter = %d, want 1", pm.Parsed.Counter)
	}

	if pm.Parsed.Expected != 5 {
		t.Errorf("Expected = %d, want 5", pm.Parsed.Expected)
	}

	if !pm.HasDiff {
		t.Error("HasDiff should be true")
	}

	if pm.Parsed.Subject != "drivers: fix regression" {
		t.Errorf("Subject = %q", pm.Parsed.Subject)
	}
}

func TestCleanMessageID(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"<test@example.com>", "test@example.com"},
		{"test@example.com", "test@example.com"},
		{"  <test@example.com>  ", "test@example.com"},
		{"", ""},
	}

	for _, tt := range tests {
		got := cleanMessageID(tt.input)
		if got != tt.want {
			t.Errorf("cleanMessageID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseReferences(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"<a@b.com> <c@d.com> <e@f.com>", 3},
		{"<single@ref.com>", 1},
		{"", 0},
	}

	for _, tt := range tests {
		refs := parseReferences(tt.input)
		if len(refs) != tt.want {
			t.Errorf("parseReferences(%q) len = %d, want %d", tt.input, len(refs), tt.want)
		}
	}
}

func TestExtractDiff(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		hasDiff bool
	}{
		{
			name: "has diff",
			body: `Some text

diff --git a/foo.c b/foo.c
index 1234567..abcdefg 100644
--- a/foo.c
+++ b/foo.c
@@ -1 +1,2 @@
+new line`,
			hasDiff: true,
		},
		{
			name:    "no diff",
			body:    "Just a regular message body\nwith multiple lines",
			hasDiff: false,
		},
		{
			name:    "empty body",
			body:    "",
			hasDiff: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, hasDiff := extractDiff(tt.body)
			if hasDiff != tt.hasDiff {
				t.Errorf("extractDiff() hasDiff = %v, want %v", hasDiff, tt.hasDiff)
			}
		})
	}
}

func TestAMReadyOutput(t *testing.T) {
	mboxData := buildTestMbox(
		`From: Author <author@example.com>
Date: Mon, 01 Jan 2024 00:00:00 +0000
Subject: [PATCH 1/1] Fix bug
Message-Id: <patch@example.com>

Fix a critical bug.

Signed-off-by: Author <author@example.com>
---
 foo.c | 1 +

diff --git a/foo.c b/foo.c
--- a/foo.c
+++ b/foo.c
@@ -1 +1 @@
+fix`,
	)

	mb := NewMailbox()
	if err := mb.ReadMbox(strings.NewReader(mboxData)); err != nil {
		t.Fatalf("ReadMbox() error = %v", err)
	}

	series := mb.GetSeries(0)
	if series == nil {
		t.Fatal("no series found")
	}

	opts := AMReadyOptions{
		AddLink:    true,
		LinkPrefix: "https://lore.kernel.org/r/",
	}

	data, err := series.GetAMReady(opts)
	if err != nil {
		t.Fatalf("GetAMReady() error = %v", err)
	}

	output := string(data)

	// Verify key components are present
	if !strings.Contains(output, "From: Author <author@example.com>") {
		t.Error("missing From header")
	}
	if !strings.Contains(output, "Subject:") {
		t.Error("missing Subject header")
	}
	if !strings.Contains(output, "Signed-off-by: Author <author@example.com>") {
		t.Error("missing Signed-off-by trailer")
	}
	if !strings.Contains(output, "Link: https://lore.kernel.org/r/patch@example.com") {
		t.Error("missing Link trailer")
	}

	// Verify it's valid mbox (can be re-parsed)
	mb2 := NewMailbox()
	if err := mb2.ReadMbox(bytes.NewReader(data)); err != nil {
		t.Errorf("AM-ready output is not valid mbox: %v", err)
	}
}

func TestWriteSeries(t *testing.T) {
	mboxData := buildTestMbox(
		`From: Author <author@example.com>
Date: Mon, 01 Jan 2024 00:00:00 +0000
Subject: [PATCH 1/1] Test patch
Message-Id: <test@example.com>

Test commit message.

Signed-off-by: Author <author@example.com>
---
diff --git a/a.c b/a.c
--- a/a.c
+++ b/a.c
@@ -1 +1 @@
+test`,
	)

	mb := NewMailbox()
	if err := mb.ReadMbox(strings.NewReader(mboxData)); err != nil {
		t.Fatalf("ReadMbox() error = %v", err)
	}

	series := mb.GetSeries(0)
	if series == nil {
		t.Fatal("no series found")
	}

	var buf bytes.Buffer
	err := WriteSeries(&buf, series, AMReadyOptions{})
	if err != nil {
		t.Fatalf("WriteSeries() error = %v", err)
	}

	if buf.Len() == 0 {
		t.Error("WriteSeries() produced empty output")
	}
}
