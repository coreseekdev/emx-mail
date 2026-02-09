package patchwork

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout is the default timeout for git commands.
const DefaultTimeout = 30 * time.Second

// Git provides an interface for running git commands.
type Git struct {
	// WorkDir is the working directory for git commands.
	// If empty, the current directory is used.
	WorkDir string

	// Timeout is the maximum duration for a git command.
	// Defaults to DefaultTimeout if zero.
	Timeout time.Duration
}

// NewGit creates a new Git instance for the given working directory.
func NewGit(workDir string) *Git {
	return &Git{
		WorkDir: workDir,
		Timeout: DefaultTimeout,
	}
}

// Run executes a git command and returns stdout.
func (g *Git) Run(args ...string) (string, error) {
	return g.RunContext(context.Background(), args...)
}

// RunContext executes a git command with context and returns stdout.
func (g *Git) RunContext(ctx context.Context, args ...string) (string, error) {
	timeout := g.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	if g.WorkDir != "" {
		cmd.Dir = g.WorkDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", &GitError{
			Args:   args,
			Err:    err,
			Stderr: stderr.String(),
		}
	}

	return stdout.String(), nil
}

// GitError represents an error from running a git command.
type GitError struct {
	Args   []string
	Err    error
	Stderr string
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s: %v\n%s", strings.Join(e.Args, " "), e.Err, e.Stderr)
}

func (e *GitError) Unwrap() error {
	return e.Err
}

// TopLevel returns the path to the top-level directory of the git repository.
func (g *Git) TopLevel() (string, error) {
	out, err := g.Run("rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// IsRepo returns true if the working directory is inside a git repository.
func (g *Git) IsRepo() bool {
	_, err := g.TopLevel()
	return err == nil
}

// CurrentBranch returns the current branch name.
func (g *Git) CurrentBranch() (string, error) {
	out, err := g.Run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// AM applies patches from an mbox file using git am.
func (g *Git) AM(mboxPath string, threeWay bool) error {
	args := []string{"am"}
	if threeWay {
		args = append(args, "--3way")
	}
	args = append(args, mboxPath)

	_, err := g.Run(args...)
	return err
}

// AMFromBytes applies patches from mbox content bytes using git am via stdin.
func (g *Git) AMFromBytes(mboxData []byte, threeWay bool) error {
	args := []string{"am"}
	if threeWay {
		args = append(args, "--3way")
	}

	timeout := g.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	if g.WorkDir != "" {
		cmd.Dir = g.WorkDir
	}

	cmd.Stdin = bytes.NewReader(mboxData)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return &GitError{
			Args:   args,
			Err:    err,
			Stderr: stderr.String(),
		}
	}

	return nil
}

// AMAbort aborts an in-progress git am session.
func (g *Git) AMAbort() error {
	_, err := g.Run("am", "--abort")
	return err
}

// Apply applies a diff/patch via git apply.
func (g *Git) Apply(patchPath string, check bool) error {
	args := []string{"apply"}
	if check {
		args = append(args, "--check")
	}
	args = append(args, patchPath)

	_, err := g.Run(args...)
	return err
}

// ApplyFromBytes applies a diff/patch from bytes via stdin.
func (g *Git) ApplyFromBytes(patchData []byte, check bool) error {
	args := []string{"apply"}
	if check {
		args = append(args, "--check")
	}

	timeout := g.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	if g.WorkDir != "" {
		cmd.Dir = g.WorkDir
	}

	cmd.Stdin = bytes.NewReader(patchData)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return &GitError{
			Args:   args,
			Err:    err,
			Stderr: stderr.String(),
		}
	}

	return nil
}

// Log returns formatted log output.
func (g *Git) Log(format string, args ...string) (string, error) {
	cmdArgs := []string{"log", "--format=" + format}
	cmdArgs = append(cmdArgs, args...)
	return g.Run(cmdArgs...)
}

// FormatPatch generates patches from a commit range using git format-patch.
// Returns the paths to the generated patch files.
func (g *Git) FormatPatch(revRange string, outputDir string) ([]string, error) {
	if outputDir == "" {
		var err error
		outputDir, err = os.MkdirTemp("", "patchwork-")
		if err != nil {
			return nil, fmt.Errorf("creating temp dir: %w", err)
		}
	}

	out, err := g.Run("format-patch", "-o", outputDir, revRange)
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}

	return paths, nil
}

// Diff generates a diff between two references.
func (g *Git) Diff(ref1, ref2 string) (string, error) {
	return g.Run("diff", ref1, ref2)
}

// RangeDiff shows the diff between two commit ranges.
func (g *Git) RangeDiff(range1, range2 string) (string, error) {
	return g.Run("range-diff", range1, range2)
}

// PatchID computes the patch-id for a diff read from stdin.
func (g *Git) PatchID(patchData []byte) (string, error) {
	timeout := g.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "patch-id", "--stable")
	if g.WorkDir != "" {
		cmd.Dir = g.WorkDir
	}

	cmd.Stdin = bytes.NewReader(patchData)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", &GitError{
			Args:   []string{"patch-id", "--stable"},
			Err:    err,
			Stderr: stderr.String(),
		}
	}

	// Output format: "<patch-id> <commit-id>"
	fields := strings.Fields(stdout.String())
	if len(fields) == 0 {
		return "", fmt.Errorf("no patch-id output")
	}
	return fields[0], nil
}

// CreateWorktree creates a temporary worktree at the given commit and returns
// its path. The caller is responsible for removing it with RemoveWorktree.
func (g *Git) CreateWorktree(commit string) (string, error) {
	dir, err := os.MkdirTemp("", "patchwork-worktree-")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	_, err = g.Run("worktree", "add", "--detach", dir, commit)
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}

	return dir, nil
}

// RemoveWorktree removes a previously created worktree.
func (g *Git) RemoveWorktree(path string) error {
	_, err := g.Run("worktree", "remove", "--force", path)
	if err != nil {
		// Fallback: remove the directory and prune
		os.RemoveAll(path)
		g.Run("worktree", "prune") //nolint:errcheck
	}
	return err
}

// Config gets a git config value.
func (g *Git) Config(key string) (string, error) {
	out, err := g.Run("config", "--get", key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// SetConfig sets a git config value.
func (g *Git) SetConfig(key, value string) error {
	_, err := g.Run("config", key, value)
	return err
}

// RevParse resolves a git revision.
func (g *Git) RevParse(rev string) (string, error) {
	out, err := g.Run("rev-parse", rev)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// SaveMboxToFile writes mbox data to a file in the given directory.
func SaveMboxToFile(data []byte, dir, name string) (string, error) {
	if dir == "" {
		dir = "."
	}

	if name == "" {
		name = "patches.mbox"
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("writing mbox file: %w", err)
	}

	return path, nil
}
