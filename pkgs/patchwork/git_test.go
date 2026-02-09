package patchwork

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// skipIfNoGit skips the test if git is not available.
func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}
}

// setupTestRepo creates a temporary git repo with an initial commit.
// Returns the repo path and a cleanup function.
func setupTestRepo(t *testing.T) (string, func()) {
	t.Helper()
	skipIfNoGit(t)

	dir, err := os.MkdirTemp("", "patchwork-test-*")
	if err != nil {
		t.Fatal(err)
	}

	cleanup := func() { os.RemoveAll(dir) }

	g := NewGit(dir)

	// Initialize repo
	if _, err := g.Run("init"); err != nil {
		cleanup()
		t.Fatalf("git init: %v", err)
	}

	// Configure user
	if _, err := g.Run("config", "user.email", "test@example.com"); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if _, err := g.Run("config", "user.name", "Test User"); err != nil {
		cleanup()
		t.Fatal(err)
	}

	// Create initial commit
	initFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(initFile, []byte("# Test\n"), 0644); err != nil {
		cleanup()
		t.Fatal(err)
	}

	if _, err := g.Run("add", "."); err != nil {
		cleanup()
		t.Fatal(err)
	}
	if _, err := g.Run("commit", "-m", "Initial commit"); err != nil {
		cleanup()
		t.Fatal(err)
	}

	return dir, cleanup
}

func TestGitNewGit(t *testing.T) {
	g := NewGit("/tmp/test")
	if g.WorkDir != "/tmp/test" {
		t.Errorf("WorkDir = %q, want %q", g.WorkDir, "/tmp/test")
	}
	if g.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", g.Timeout, DefaultTimeout)
	}
}

func TestGitIsRepo(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)
	if !g.IsRepo() {
		t.Error("IsRepo() = false, want true")
	}

	// Non-repo directory
	tmpDir, err := os.MkdirTemp("", "non-repo-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	g2 := NewGit(tmpDir)
	if g2.IsRepo() {
		t.Error("IsRepo() = true for non-repo, want false")
	}
}

func TestGitTopLevel(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)
	topLevel, err := g.TopLevel()
	if err != nil {
		t.Fatalf("TopLevel() error = %v", err)
	}

	// Normalize paths for comparison
	expected, _ := filepath.EvalSymlinks(dir)
	actual, _ := filepath.EvalSymlinks(topLevel)

	if runtime.GOOS == "windows" {
		expected = strings.ToLower(filepath.ToSlash(expected))
		actual = strings.ToLower(filepath.ToSlash(actual))
	}

	if actual != expected {
		t.Errorf("TopLevel() = %q, want %q", actual, expected)
	}
}

func TestGitCurrentBranch(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)
	branch, err := g.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch() error = %v", err)
	}

	// Branch name should be "main" or "master" depending on git config
	if branch != "main" && branch != "master" {
		t.Errorf("CurrentBranch() = %q, want 'main' or 'master'", branch)
	}
}

func TestGitAMFromBytes(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	// Create a valid git-am-compatible mbox patch
	patch := `From 1234567890abcdef1234567890abcdef12345678 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Add test file

Add a test file for testing.

Signed-off-by: Test Author <test@example.com>
---
 test.txt | 1 +
 1 file changed, 1 insertion(+)
 create mode 100644 test.txt

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..ce01362
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+hello
-- 
2.34.1

`

	err := g.AMFromBytes([]byte(patch), false)
	if err != nil {
		t.Fatalf("AMFromBytes() error = %v", err)
	}

	// Verify the file was created
	testFile := filepath.Join(dir, "test.txt")
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}

	if strings.TrimSpace(string(data)) != "hello" {
		t.Errorf("file content = %q, want %q", string(data), "hello\n")
	}

	// Verify the commit was created
	out, err := g.Log("%s", "-1")
	if err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	if !strings.Contains(out, "Add test file") {
		t.Errorf("commit message = %q, should contain 'Add test file'", out)
	}
}

func TestGitApplyFromBytes(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	// Create a file to patch
	testFile := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("add", "hello.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("commit", "-m", "add hello.txt"); err != nil {
		t.Fatal(err)
	}

	// Create a patch
	patch := `diff --git a/hello.txt b/hello.txt
index ce01362..a042389 100644
--- a/hello.txt
+++ b/hello.txt
@@ -1 +1 @@
-hello
+hello world
`

	// Check first
	err := g.ApplyFromBytes([]byte(patch), true)
	if err != nil {
		t.Fatalf("ApplyFromBytes(check) error = %v", err)
	}

	// Apply for real
	err = g.ApplyFromBytes([]byte(patch), false)
	if err != nil {
		t.Fatalf("ApplyFromBytes() error = %v", err)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(string(data)) != "hello world" {
		t.Errorf("file content after apply = %q", string(data))
	}
}

func TestGitFormatPatch(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	// Get initial commit
	baseRev, err := g.RevParse("HEAD")
	if err != nil {
		t.Fatal(err)
	}

	// Create a new commit
	testFile := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(testFile, []byte("new\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("commit", "-m", "Add new file"); err != nil {
		t.Fatal(err)
	}

	// Generate patches
	outputDir, err := os.MkdirTemp("", "patches-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outputDir)

	paths, err := g.FormatPatch(baseRev+"..HEAD", outputDir)
	if err != nil {
		t.Fatalf("FormatPatch() error = %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("FormatPatch() returned %d paths, want 1", len(paths))
	}

	// Verify the patch file exists and contains the subject
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "Add new file") {
		t.Error("patch file doesn't contain expected subject")
	}
}

func TestGitRevParse(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	rev, err := g.RevParse("HEAD")
	if err != nil {
		t.Fatalf("RevParse() error = %v", err)
	}

	// Should be a 40-char hex string
	if len(rev) != 40 {
		t.Errorf("RevParse() returned %q, want 40-char hex", rev)
	}
}

func TestGitConfig(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	email, err := g.Config("user.email")
	if err != nil {
		t.Fatalf("Config() error = %v", err)
	}

	if email != "test@example.com" {
		t.Errorf("Config(user.email) = %q, want %q", email, "test@example.com")
	}
}

func TestGitErrorFormat(t *testing.T) {
	e := &GitError{
		Args:   []string{"status", "--porcelain"},
		Err:    exec.ErrNotFound,
		Stderr: "some error output",
	}

	msg := e.Error()
	if !strings.Contains(msg, "git status --porcelain") {
		t.Errorf("Error() = %q, should contain command", msg)
	}
	if !strings.Contains(msg, "some error output") {
		t.Errorf("Error() = %q, should contain stderr", msg)
	}
}

func TestSaveMboxToFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "mbox-save-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	data := []byte("From test@test Mon Jan  1 00:00:00 2024\nSubject: test\n\nBody\n\n")

	path, err := SaveMboxToFile(data, dir, "test.mbox")
	if err != nil {
		t.Fatalf("SaveMboxToFile() error = %v", err)
	}

	if !strings.HasSuffix(path, "test.mbox") {
		t.Errorf("path = %q, should end with test.mbox", path)
	}

	// Verify file was written
	readBack, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(readBack) != string(data) {
		t.Error("file content mismatch")
	}
}

func TestSaveMboxToFileDefaults(t *testing.T) {
	dir, err := os.MkdirTemp("", "mbox-save-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Change to temp dir to test default dir
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	path, err := SaveMboxToFile([]byte("test"), "", "")
	if err != nil {
		t.Fatalf("SaveMboxToFile() error = %v", err)
	}

	if !strings.HasSuffix(path, "patches.mbox") {
		t.Errorf("default name should be patches.mbox, got %q", path)
	}
}
