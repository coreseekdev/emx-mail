package patchwork

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewPrepBranch(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	pb, err := NewPrepBranch(g, "my-feature", "")
	if err != nil {
		t.Fatalf("NewPrepBranch() error = %v", err)
	}

	if pb.Slug != "my-feature" {
		t.Errorf("Slug = %q, want %q", pb.Slug, "my-feature")
	}
	if pb.Revision != 1 {
		t.Errorf("Revision = %d, want 1", pb.Revision)
	}
	if pb.BranchName() != "b4/my-feature" {
		t.Errorf("BranchName() = %q, want %q", pb.BranchName(), "b4/my-feature")
	}
}

func TestNewPrepBranchEmptySlug(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	_, err := NewPrepBranch(g, "", "")
	if err == nil {
		t.Error("NewPrepBranch() with empty slug should error")
	}
}

func TestPrepBranchCreateAndLoad(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	// Create a prep branch
	pb, err := NewPrepBranch(g, "fix-bug", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.Create(); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify we're on the new branch
	branch, err := g.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "b4/fix-bug" {
		t.Errorf("CurrentBranch() = %q, want %q", branch, "b4/fix-bug")
	}

	// Load the prep branch
	loaded, err := LoadPrepBranch(g)
	if err != nil {
		t.Fatalf("LoadPrepBranch() error = %v", err)
	}

	if loaded.Slug != "fix-bug" {
		t.Errorf("loaded Slug = %q, want %q", loaded.Slug, "fix-bug")
	}
	if loaded.Revision != 1 {
		t.Errorf("loaded Revision = %d, want 1", loaded.Revision)
	}
}

func TestPrepBranchCreateDuplicate(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	pb, err := NewPrepBranch(g, "dup-test", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.Create(); err != nil {
		t.Fatal(err)
	}

	// Try to create the same branch again
	pb2, err := NewPrepBranch(g, "dup-test", "")
	if err != nil {
		t.Fatal(err)
	}
	err = pb2.Create()
	if err == nil {
		t.Error("Create() duplicate branch should error")
	}
}

func TestPrepBranchReroll(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	pb, err := NewPrepBranch(g, "reroll-test", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.Create(); err != nil {
		t.Fatal(err)
	}

	if pb.Revision != 1 {
		t.Fatalf("initial Revision = %d, want 1", pb.Revision)
	}

	// Reroll
	if err := pb.Reroll(); err != nil {
		t.Fatalf("Reroll() error = %v", err)
	}

	if pb.Revision != 2 {
		t.Errorf("Revision after reroll = %d, want 2", pb.Revision)
	}

	// Reload and verify persisted
	loaded, err := LoadPrepBranch(g)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Revision != 2 {
		t.Errorf("loaded Revision = %d, want 2", loaded.Revision)
	}
}

func TestPrepBranchCover(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	pb, err := NewPrepBranch(g, "cover-test", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.Create(); err != nil {
		t.Fatal(err)
	}

	// Save cover letter
	err = pb.SaveCover("Fix null pointer issues", "This series fixes various null pointer dereferences.")
	if err != nil {
		t.Fatalf("SaveCover() error = %v", err)
	}

	// Reload and verify
	loaded, err := LoadPrepBranch(g)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.CoverSubject != "Fix null pointer issues" {
		t.Errorf("CoverSubject = %q", loaded.CoverSubject)
	}
	if loaded.CoverBody != "This series fixes various null pointer dereferences." {
		t.Errorf("CoverBody = %q", loaded.CoverBody)
	}
}

func TestPrepBranchEnumerateCommits(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	// Get the current branch as base
	baseBranch, _ := g.CurrentBranch()

	pb, err := NewPrepBranch(g, "commits-test", baseBranch)
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.Create(); err != nil {
		t.Fatal(err)
	}

	// Add two commits
	for _, name := range []string{"a.txt", "b.txt"} {
		f := filepath.Join(dir, name)
		os.WriteFile(f, []byte("content\n"), 0644)
		g.Run("add", name)
		g.Run("commit", "-m", "Add "+name)
	}

	commits, err := pb.EnumerateCommits()
	if err != nil {
		t.Fatalf("EnumerateCommits() error = %v", err)
	}

	if len(commits) != 2 {
		t.Errorf("len(commits) = %d, want 2", len(commits))
	}
}

func TestPrepBranchGetPatches(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	baseBranch, _ := g.CurrentBranch()

	pb, err := NewPrepBranch(g, "patches-test", baseBranch)
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.Create(); err != nil {
		t.Fatal(err)
	}

	// Add a commit
	f := filepath.Join(dir, "new-file.txt")
	os.WriteFile(f, []byte("new content\n"), 0644)
	g.Run("add", "new-file.txt")
	g.Run("commit", "-m", "Add new file")

	// Generate patches
	outputDir, err := os.MkdirTemp("", "patches-out-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outputDir)

	paths, err := pb.GetPatches(outputDir)
	if err != nil {
		t.Fatalf("GetPatches() error = %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("len(paths) = %d, want 1", len(paths))
	}
}

func TestPrepBranchDiffStat(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	baseBranch, _ := g.CurrentBranch()

	pb, err := NewPrepBranch(g, "stat-test", baseBranch)
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.Create(); err != nil {
		t.Fatal(err)
	}

	// Add a commit
	f := filepath.Join(dir, "stat-file.txt")
	os.WriteFile(f, []byte("stat content\n"), 0644)
	g.Run("add", "stat-file.txt")
	g.Run("commit", "-m", "Add stat file")

	stat, err := pb.DiffStat()
	if err != nil {
		t.Fatalf("DiffStat() error = %v", err)
	}

	if stat == "" {
		t.Error("DiffStat() returned empty string")
	}
}

func TestFormatSeriesSubject(t *testing.T) {
	pb := &PrepBranch{
		Slug:     "test",
		Revision: 3,
	}

	result := pb.FormatSeriesSubject(2, 5, "Fix null pointer")
	if result != "[PATCH v3 2/5] Fix null pointer" {
		t.Errorf("FormatSeriesSubject() = %q, want %q", result, "[PATCH v3 2/5] Fix null pointer")
	}
}

func TestFormatSeriesSubjectV1(t *testing.T) {
	pb := &PrepBranch{
		Slug:     "test",
		Revision: 1,
	}

	result := pb.FormatSeriesSubject(1, 3, "Some change")
	if result != "[PATCH 1/3] Some change" {
		t.Errorf("FormatSeriesSubject() = %q, want %q", result, "[PATCH 1/3] Some change")
	}
}

func TestFormatSeriesSubjectRFC(t *testing.T) {
	pb := &PrepBranch{
		Slug:     "test",
		Revision: 2,
		Prefixes: []string{"RFC"},
	}

	result := pb.FormatSeriesSubject(1, 3, "Draft change")
	if result != "[PATCH RFC v2 1/3] Draft change" {
		t.Errorf("FormatSeriesSubject() = %q, want %q", result, "[PATCH RFC v2 1/3] Draft change")
	}
}

func TestLoadPrepBranchNotOnPrepBranch(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	_, err := LoadPrepBranch(g)
	if err == nil {
		t.Error("LoadPrepBranch() on non-prep branch should error")
	}
}

func TestListPrepBranches(t *testing.T) {
	dir, cleanup := setupTestRepo(t)
	defer cleanup()

	g := NewGit(dir)

	// Initially no prep branches
	branches, err := ListPrepBranches(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 0 {
		t.Errorf("initial branches = %v, want empty", branches)
	}

	// Create a prep branch
	pb, _ := NewPrepBranch(g, "list-test", "")
	pb.Create()

	// Go back to main branch to create another
	g.Run("checkout", "-")
	pb2, _ := NewPrepBranch(g, "list-test-2", "")
	pb2.Create()

	branches, err = ListPrepBranches(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 {
		t.Errorf("branches after create = %v, want 2 entries", branches)
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		slice []string
		val   string
		want  bool
	}{
		{[]string{"RFC", "PATCH"}, "rfc", true},
		{[]string{"RFC", "PATCH"}, "RFC", true},
		{[]string{"PATCH"}, "RFC", false},
		{nil, "RFC", false},
		{[]string{}, "RFC", false},
	}

	for _, tt := range tests {
		got := containsIgnoreCase(tt.slice, tt.val)
		if got != tt.want {
			t.Errorf("containsIgnoreCase(%v, %q) = %v, want %v", tt.slice, tt.val, got, tt.want)
		}
	}
}
