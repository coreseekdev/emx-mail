package patchwork

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PrepBranch represents a prepared patch series branch for mailing list submission.
type PrepBranch struct {
	// Slug is the short name for the prep branch (used in branch name).
	Slug string

	// Revision is the current version number.
	Revision int

	// BaseBranch is the upstream branch to diff against.
	BaseBranch string

	// ChangeID is the unique identifier for this patch series.
	ChangeID string

	// Prefixes contains extra subject prefixes (e.g., ["RFC"]).
	Prefixes []string

	// CoverSubject is the cover letter subject.
	CoverSubject string

	// CoverBody is the cover letter body text.
	CoverBody string

	// git is the Git instance.
	git *Git
}

// PrepTrackingData is the JSON structure stored in the tracking file.
type PrepTrackingData struct {
	Series struct {
		Revision   int      `json:"revision"`
		ChangeID   string   `json:"change-id"`
		BaseBranch string   `json:"base-branch"`
		Prefixes   []string `json:"prefixes,omitempty"`
	} `json:"series"`
}

const (
	// prepBranchPrefix is the prefix for prep branch names.
	prepBranchPrefix = "b4/"

	// trackingDir is the directory within the repo for tracking data.
	trackingDir = ".b4"

	// trackingFile is the file name for series tracking data.
	trackingFile = "series.json"

	// coverFile is the file name for cover letter content.
	coverFile = "cover"
)

// NewPrepBranch creates a new prep branch for a patch series.
func NewPrepBranch(git *Git, slug, baseBranch string) (*PrepBranch, error) {
	if !git.IsRepo() {
		return nil, fmt.Errorf("not in a git repository")
	}

	if slug == "" {
		return nil, fmt.Errorf("slug is required")
	}

	pb := &PrepBranch{
		Slug:       slug,
		Revision:   1,
		BaseBranch: baseBranch,
		git:        git,
	}

	// Generate a change-id
	pb.ChangeID = generateChangeID(slug)

	return pb, nil
}

// BranchName returns the full git branch name.
func (pb *PrepBranch) BranchName() string {
	return prepBranchPrefix + pb.Slug
}

// Create creates the prep branch and initializes tracking data.
func (pb *PrepBranch) Create() error {
	branchName := pb.BranchName()

	// Check if branch already exists
	_, err := pb.git.Run("rev-parse", "--verify", branchName)
	if err == nil {
		return fmt.Errorf("branch %s already exists", branchName)
	}

	// Get the base commit
	base := pb.BaseBranch
	if base == "" {
		base = "HEAD"
	}

	// Create the branch
	_, err = pb.git.Run("checkout", "-b", branchName, base)
	if err != nil {
		return fmt.Errorf("creating branch: %w", err)
	}

	// Initialize tracking
	return pb.saveTracking()
}

// LoadPrepBranch loads an existing prep branch.
func LoadPrepBranch(git *Git) (*PrepBranch, error) {
	branch, err := git.CurrentBranch()
	if err != nil {
		return nil, err
	}

	if !strings.HasPrefix(branch, prepBranchPrefix) {
		return nil, fmt.Errorf("not on a b4 prep branch (current: %s)", branch)
	}

	slug := strings.TrimPrefix(branch, prepBranchPrefix)

	pb := &PrepBranch{
		Slug: slug,
		git:  git,
	}

	if err := pb.loadTracking(); err != nil {
		return nil, fmt.Errorf("loading tracking data: %w", err)
	}

	pb.loadCover()

	return pb, nil
}

// saveTracking saves tracking data to the tracking file.
func (pb *PrepBranch) saveTracking() error {
	topLevel, err := pb.git.TopLevel()
	if err != nil {
		return err
	}

	dir := filepath.Join(topLevel, trackingDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating tracking dir: %w", err)
	}

	data := PrepTrackingData{}
	data.Series.Revision = pb.Revision
	data.Series.ChangeID = pb.ChangeID
	data.Series.BaseBranch = pb.BaseBranch
	data.Series.Prefixes = pb.Prefixes

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tracking data: %w", err)
	}

	path := filepath.Join(dir, trackingFile)
	return os.WriteFile(path, jsonData, 0644)
}

// loadTracking reads tracking data from the tracking file.
func (pb *PrepBranch) loadTracking() error {
	topLevel, err := pb.git.TopLevel()
	if err != nil {
		return err
	}

	path := filepath.Join(topLevel, trackingDir, trackingFile)
	jsonData, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading tracking data: %w", err)
	}

	var data PrepTrackingData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return fmt.Errorf("parsing tracking data: %w", err)
	}

	pb.Revision = data.Series.Revision
	pb.ChangeID = data.Series.ChangeID
	pb.BaseBranch = data.Series.BaseBranch
	pb.Prefixes = data.Series.Prefixes

	return nil
}

// loadCover reads the cover letter from the cover file.
func (pb *PrepBranch) loadCover() {
	topLevel, err := pb.git.TopLevel()
	if err != nil {
		return
	}

	path := filepath.Join(topLevel, trackingDir, coverFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	content := string(data)
	lines := strings.SplitN(content, "\n", 2)
	if len(lines) >= 1 {
		pb.CoverSubject = strings.TrimSpace(lines[0])
	}
	if len(lines) >= 2 {
		pb.CoverBody = strings.TrimSpace(lines[1])
	}
}

// SaveCover saves the cover letter.
func (pb *PrepBranch) SaveCover(subject, body string) error {
	topLevel, err := pb.git.TopLevel()
	if err != nil {
		return err
	}

	pb.CoverSubject = subject
	pb.CoverBody = body

	dir := filepath.Join(topLevel, trackingDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating tracking dir: %w", err)
	}

	content := subject + "\n" + body
	path := filepath.Join(dir, coverFile)
	return os.WriteFile(path, []byte(content), 0644)
}

// GetPatches generates patches from the prep branch using git format-patch.
func (pb *PrepBranch) GetPatches(outputDir string) ([]string, error) {
	if pb.BaseBranch == "" {
		return nil, fmt.Errorf("no base branch set")
	}

	revRange := pb.BaseBranch + "..HEAD"
	return pb.git.FormatPatch(revRange, outputDir)
}

// Reroll bumps the revision number for a new version of the series.
func (pb *PrepBranch) Reroll() error {
	pb.Revision++
	return pb.saveTracking()
}

// generateChangeID creates a unique change identifier from the slug.
func generateChangeID(slug string) string {
	return slug
}

// ListPrepBranches lists all prep branches in the repository.
func ListPrepBranches(git *Git) ([]string, error) {
	out, err := git.Run("branch", "--list", prepBranchPrefix+"*")
	if err != nil {
		return nil, err
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "* ")
		if line != "" {
			branches = append(branches, line)
		}
	}

	return branches, nil
}

// FormatSeriesSubject builds the series subject prefix for a given patch.
func (pb *PrepBranch) FormatSeriesSubject(patchNum, totalPatches int, subject string) string {
	ps := &PatchSubject{
		Subject:  subject,
		Prefixes: []string{"PATCH"},
		Counter:  patchNum,
		Expected: totalPatches,
		Revision: pb.Revision,
		IsRFC:    containsIgnoreCase(pb.Prefixes, "RFC"),
	}

	return ps.Rebuild()
}

// containsIgnoreCase checks if a string slice contains a value (case-insensitive).
func containsIgnoreCase(slice []string, val string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, val) {
			return true
		}
	}
	return false
}

// EnumerateCommits lists commit subjects between base and HEAD.
func (pb *PrepBranch) EnumerateCommits() ([]string, error) {
	if pb.BaseBranch == "" {
		return nil, fmt.Errorf("no base branch set")
	}

	out, err := pb.git.Log("%s", pb.BaseBranch+"..HEAD")
	if err != nil {
		return nil, err
	}

	var subjects []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			subjects = append(subjects, line)
		}
	}

	return subjects, nil
}

// DiffStat returns the diffstat between base and HEAD.
func (pb *PrepBranch) DiffStat() (string, error) {
	if pb.BaseBranch == "" {
		return "", fmt.Errorf("no base branch set")
	}

	return pb.git.Run("diff", "--stat", pb.BaseBranch+"..HEAD")
}

// ShortLog returns the shortlog between base and HEAD.
func (pb *PrepBranch) ShortLog() (string, error) {
	if pb.BaseBranch == "" {
		return "", fmt.Errorf("no base branch set")
	}

	return pb.git.Run("shortlog", pb.BaseBranch+"..HEAD")
}

// ParseIntRange parses a range like "1-3,5,7-9" into a list of integers.
func ParseIntRange(s string) ([]int, error) {
	var result []int
	parts := strings.Split(s, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if idx := strings.Index(part, "-"); idx >= 0 {
			start, err := strconv.Atoi(strings.TrimSpace(part[:idx]))
			if err != nil {
				return nil, fmt.Errorf("invalid range start: %s", part[:idx])
			}
			end, err := strconv.Atoi(strings.TrimSpace(part[idx+1:]))
			if err != nil {
				return nil, fmt.Errorf("invalid range end: %s", part[idx+1:])
			}
			for i := start; i <= end; i++ {
				result = append(result, i)
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid number: %s", part)
			}
			result = append(result, n)
		}
	}

	return result, nil
}
