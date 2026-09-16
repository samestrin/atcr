package benchmark

import (
	"errors"
)

// FormatRepoStateV1 is the `suite` discriminator and per-case `format` literal of
// the repo-state tier (benchmarks/repo-state-v1/FORMAT.md).
const FormatRepoStateV1 = "repo-state-v1"

// DefaultLineTolerance is the match window applied when a case's expected finding
// omits line_tolerance.
const DefaultLineTolerance = 3

// ExpectedFinding is one planted defect, located.
type ExpectedFinding struct {
	ID            string `json:"id"`
	File          string `json:"file"`
	LineStart     int    `json:"line_start"`
	LineEnd       int    `json:"line_end"`
	LineTolerance *int   `json:"line_tolerance,omitempty"`
	OutsideDiff   *bool  `json:"outside_diff"`
	Category      string `json:"category"`
	Summary       string `json:"summary"`
}

// RepoStateCase is one loaded repo-state case.
type RepoStateCase struct {
	ID               string            `json:"id"`
	Format           string            `json:"format"`
	BaseTree         string            `json:"base_tree"`
	CommitMessage    string            `json:"commit_message"`
	Diff             string            `json:"diff"`
	ExpectedFindings []ExpectedFinding `json:"expected_findings"`
	Dir              string            `json:"-"`
}

// RepoStateManifest is a loaded repo-state suite.
type RepoStateManifest struct {
	Suite        string          `json:"suite"`
	SuiteVersion string          `json:"suite_version"`
	Cases        []RepoStateCase `json:"cases"`
}

// errNotImplemented is the deliberate wrong answer this stub returns so the RED
// test compiles and fails for a real reason.
var errNotImplemented = errors.New("repo-state loader not implemented")

// Tolerance returns the finding's match window.
func (f ExpectedFinding) Tolerance() int { return 0 }

// DetectSuiteFormat returns the `suite` discriminator declared by suitePath's manifest.
func DetectSuiteFormat(suitePath string) (string, error) { return "", errNotImplemented }

// LoadRepoState reads, parses and validates a repo-state-v1 suite.
func LoadRepoState(suitePath string) (*RepoStateManifest, error) { return nil, errNotImplemented }

// Validate enforces the structural contract of a repo-state suite.
func (m *RepoStateManifest) Validate() error { return errNotImplemented }
