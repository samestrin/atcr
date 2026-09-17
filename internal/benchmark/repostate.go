package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// FormatRepoStateV1 is the `suite` discriminator and the per-case `format` literal
// of the repo-state tier. It is spelled once here because three separate checks —
// the suite manifest, each case manifest, and the CLI dispatcher — must agree on
// it, and a second copy is a way for them to stop agreeing.
const FormatRepoStateV1 = "repo-state-v1"

// DefaultLineTolerance is the match window applied when an expected finding omits
// line_tolerance. It equals reconcile.LineProximity, the window
// llm_support_td_dedupe already uses to decide two FILE:LINE reports are about the
// same place — one convention for "same place" across the toolchain
// (benchmarks/repo-state-v1/FORMAT.md, "The matching rule").
//
// It is not imported from the reconcile module: that is a separately versioned
// published module, and importing it here to share an integer would tie this
// loader's compilation to a version pin for no behavioral gain.
const DefaultLineTolerance = 3

// ExpectedFinding is one planted defect, LOCATED — the difference that makes this
// tier scoreable where standard-v1's bare category list is not. File is
// repository-relative in the HEAD state (after the diff applies), which is the same
// coordinate space a reviewer cites, so the matcher compares like with like.
type ExpectedFinding struct {
	ID        string `json:"id"`
	File      string `json:"file"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`

	// LineTolerance is a POINTER so an omitted key is distinguishable from an
	// explicit 0. They mean opposite things: omitted takes the ±3 default, while an
	// explicit 0 demands an exact line. A plain int would silently convert the
	// strictest possible case into the default one.
	LineTolerance *int `json:"line_tolerance,omitempty"`

	// OutsideDiff is a POINTER because FORMAT.md makes it REQUIRED and false is a
	// meaningful value. As a plain bool an author who forgot the key would get
	// `false` — the permissive setting — and the case would silently stop measuring
	// the one thing this tier exists for. Validate rejects nil.
	OutsideDiff *bool `json:"outside_diff"`

	Category string `json:"category"`
	Summary  string `json:"summary"`
}

// Tolerance returns the finding's match window, applying the default for an
// omitted line_tolerance. Call this rather than reading LineTolerance directly;
// the matcher must never hardcode a window (AC3).
func (f ExpectedFinding) Tolerance() int {
	if f.LineTolerance == nil {
		return DefaultLineTolerance
	}
	return *f.LineTolerance
}

// IsOutsideDiff reports the finding's outside_diff value. Validate guarantees the
// pointer is non-nil on any loaded case, so a nil here can only come from a
// hand-constructed value that bypassed it — and returning the permissive false
// would silently stop measuring the one thing this tier exists for. The nil arm
// is therefore LOUD: it panics, naming the finding, rather than inventing a
// default the case's author never wrote.
func (f ExpectedFinding) IsOutsideDiff() bool {
	if f.OutsideDiff == nil {
		panic(fmt.Sprintf("expected finding %q has a nil outside_diff; Validate rejects this on any loaded case, so the value was hand-constructed and unvalidated", f.ID))
	}
	return *f.OutsideDiff
}

// RepoStateCase is one loaded repo-state case: the contents of its case.json plus
// the resolved directory those relative paths are anchored to.
type RepoStateCase struct {
	ID               string            `json:"id"`
	Format           string            `json:"format"`
	BaseTree         string            `json:"base_tree"`
	CommitMessage    string            `json:"commit_message"`
	Diff             string            `json:"diff"`
	ExpectedFindings []ExpectedFinding `json:"expected_findings"`

	// Dir is the absolute-or-suite-relative path to the case directory, filled by
	// LoadRepoState from the suite manifest's `dir`. It is not a case.json field, so
	// it is excluded from JSON on both sides: a case.json carrying a `dir` key must
	// not be able to point the loader somewhere else.
	Dir string `json:"-"`
}

// RepoStateManifest is a loaded repo-state suite: the suite identity plus every
// case FULLY loaded. Cases are loaded eagerly rather than lazily so a defective
// case fails at load, where the remedy is free, instead of part-way through a paid
// panel run.
type RepoStateManifest struct {
	Suite        string          `json:"suite"`
	SuiteVersion string          `json:"suite_version"`
	Cases        []RepoStateCase `json:"cases"`
}

// repoStateCaseRef is a suite.json `cases[]` entry. It is separate from
// RepoStateCase because the two documents carry different fields under the same
// name: suite.json says WHERE a case is, case.json says what it contains.
type repoStateCaseRef struct {
	ID  string `json:"id"`
	Dir string `json:"dir"`
}

type repoStateSuiteFile struct {
	Suite        string             `json:"suite"`
	SuiteVersion string             `json:"suite_version"`
	Cases        []repoStateCaseRef `json:"cases"`
}

// suiteDiscriminator is the minimal projection of any suite manifest: just enough
// to decide which loader owns the document. Parsing the whole thing under the
// wrong shape first is what produced the old "diff path is required" error for a
// format that never had a diff field.
type suiteDiscriminator struct {
	Suite string `json:"suite"`
}

// DetectSuiteFormat returns the `suite` discriminator declared by suitePath's
// manifest, without committing to either suite's shape.
//
// It exists so the CLI can ROUTE. `benchmark run --suite-path X` cannot know which
// loader to call until it has read X, and the two loaders return different types,
// so the choice cannot be deferred into a single Load. Reading only the
// discriminator keeps the routing decision independent of whether the rest of the
// document is valid — a malformed repo-state case must produce a repo-state error,
// not a standard-v1 one.
func DetectSuiteFormat(suitePath string) (string, error) {
	manifestPath := filepath.Join(suitePath, "suite.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", fmt.Errorf("reading suite manifest %s: %w", manifestPath, err)
	}
	var d suiteDiscriminator
	if err := json.Unmarshal(data, &d); err != nil {
		return "", fmt.Errorf("parsing suite manifest %s: %w", manifestPath, err)
	}
	suite := strings.TrimSpace(d.Suite)
	if suite == "" {
		return "", fmt.Errorf("suite manifest %s declares no suite name", manifestPath)
	}
	return suite, nil
}

// LoadRepoState reads <suitePath>/suite.json and every case it references,
// validates the whole suite structurally, and confirms each case's base tree,
// commit message and diff exist on disk.
//
// It returns a clear error rather than a half-built manifest, the same contract
// Load makes, and for a sharper reason here: a case whose base tree is missing
// would otherwise materialize an EMPTY tree and score every reviewer zero. A tier
// whose whole purpose is detecting unwinnable cases must not ship one silently, so
// every filesystem precondition is checked at load.
func LoadRepoState(suitePath string) (*RepoStateManifest, error) {
	manifestPath := filepath.Join(suitePath, "suite.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("reading suite manifest %s: %w", manifestPath, err)
	}
	var sf repoStateSuiteFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parsing suite manifest %s: %w", manifestPath, err)
	}
	if got := strings.TrimSpace(sf.Suite); got != FormatRepoStateV1 {
		return nil, fmt.Errorf("suite manifest %s declares suite %q, not %q; this loader implements %s only",
			manifestPath, got, FormatRepoStateV1, FormatRepoStateV1)
	}
	if strings.TrimSpace(sf.SuiteVersion) == "" {
		return nil, fmt.Errorf("invalid suite manifest %s: suite_version is required", manifestPath)
	}
	if len(sf.Cases) == 0 {
		return nil, fmt.Errorf("invalid suite manifest %s: suite must define at least one case", manifestPath)
	}

	m := &RepoStateManifest{Suite: FormatRepoStateV1, SuiteVersion: sf.SuiteVersion}
	seen := make(map[string]bool, len(sf.Cases))
	for i, ref := range sf.Cases {
		id := strings.TrimSpace(ref.ID)
		if id == "" {
			return nil, fmt.Errorf("invalid suite manifest %s: case %d: id is required", manifestPath, i)
		}
		if seen[id] {
			return nil, fmt.Errorf("invalid suite manifest %s: case %q: duplicate id", manifestPath, id)
		}
		seen[id] = true
		if strings.TrimSpace(ref.Dir) == "" {
			return nil, fmt.Errorf("invalid suite manifest %s: case %q: dir is required", manifestPath, id)
		}
		if !isSafeRelPath(ref.Dir) {
			return nil, fmt.Errorf("invalid suite manifest %s: case %q: dir %q must be relative and within the suite directory",
				manifestPath, id, ref.Dir)
		}
		caseDir := filepath.Join(suitePath, ref.Dir)
		// Lstat the case directory for the same reason checkFiles Lstats the base
		// tree: the string check above proves `dir` does not SAY it escapes, and says
		// nothing about whether the directory on disk is a symlink pointing out of
		// the suite. Closing that hole one level down and leaving it open here would
		// mean a case could escape by being a link rather than by containing one.
		if fi, serr := os.Lstat(caseDir); serr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("invalid suite manifest %s: case %q directory %q is a symlink; a case must not reference a path outside the suite directory",
				manifestPath, id, ref.Dir)
		}
		c, err := loadRepoStateCase(caseDir)
		if err != nil {
			return nil, err
		}
		// The manifest and the case must agree on the id. They are two documents an
		// author edits separately, and a disagreement means the suite runs a case the
		// manifest does not name — so a result would be filed against the wrong id.
		if c.ID != id {
			return nil, fmt.Errorf("suite manifest %s lists case %q but %s declares id %q; the two must match (mismatch)",
				manifestPath, id, filepath.Join(ref.Dir, "case.json"), c.ID)
		}
		m.Cases = append(m.Cases, *c)
	}
	return m, nil
}

// loadRepoStateCase reads and validates one case directory.
func loadRepoStateCase(caseDir string) (*RepoStateCase, error) {
	casePath := filepath.Join(caseDir, "case.json")
	data, err := os.ReadFile(casePath)
	if err != nil {
		return nil, fmt.Errorf("reading case manifest %s: %w", casePath, err)
	}
	var c RepoStateCase
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parsing case manifest %s: %w", casePath, err)
	}
	c.Dir = caseDir
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid case manifest %s: %w", casePath, err)
	}
	if err := c.checkFiles(); err != nil {
		return nil, err
	}
	// The loader's "every filesystem precondition is checked at load" contract
	// includes the diff's PARSEABILITY: ParseDiffLineMap rejects a malformed diff
	// (a body line outside any hunk, an unparseable header, a negative range), and
	// the only alternative gate was the per-case parse in the run loop — after
	// every earlier case's panel had been paid for. Parsing here makes a broken
	// diff a load error, where the remedy is free. The parsed map is not cached on
	// the case: its only consumer is the CLI runner's pre-flight pass, which
	// already parses (and runs under the paid-run clock), and an exported field
	// would widen the struct's public surface for a consumer that does not exist.
	diffData, rerr := os.ReadFile(filepath.Join(caseDir, c.Diff))
	if rerr != nil {
		return nil, fmt.Errorf("case %q: reading diff %s: %w", c.ID, c.Diff, rerr)
	}
	if _, perr := ParseDiffLineMap(diffData); perr != nil {
		return nil, fmt.Errorf("case %q: invalid diff %s: %w", c.ID, c.Diff, perr)
	}
	return &c, nil
}

// Validate enforces the structural contract of one case: the identity fields, the
// three case-relative file references, and every expected finding. It does NOT
// touch the filesystem (checkFiles does), so it is usable on an in-memory case —
// the same split Manifest.Validate makes.
func (c *RepoStateCase) Validate() error {
	id := strings.TrimSpace(c.ID)
	if id == "" {
		return fmt.Errorf("id is required")
	}
	if err := validCaseToken("id", id); err != nil {
		return err
	}
	// FORMAT.md: the id must equal the case directory name. Enforced here rather
	// than left to convention because the id is what a run-result files a score
	// under, and a case whose directory says one thing and whose manifest says
	// another makes an on-disk result untraceable to the case that produced it.
	if base := filepath.Base(c.Dir); c.Dir != "" && base != id {
		return fmt.Errorf("id %q must equal the case directory name %q", id, base)
	}
	if got := strings.TrimSpace(c.Format); got != FormatRepoStateV1 {
		return fmt.Errorf("format is %q, but every case in this tier declares format %q", got, FormatRepoStateV1)
	}
	for _, f := range []struct{ field, value string }{
		{"base_tree", c.BaseTree},
		{"commit_message", c.CommitMessage},
		{"diff", c.Diff},
	} {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("%s path is required", f.field)
		}
		// AC7: no case may reference a path outside its own directory. Checked on
		// every file-reference field, not only the one an author is likeliest to get
		// wrong — a guard with a gap is a guard an author will find the gap in.
		if !isSafeRelPath(f.value) {
			return fmt.Errorf("%s path %q must be relative and within the case directory", f.field, f.value)
		}
	}
	if len(c.ExpectedFindings) == 0 {
		return fmt.Errorf("at least one expected_finding is required; a case with nothing to find scores nothing")
	}
	seen := make(map[string]bool, len(c.ExpectedFindings))
	for i, f := range c.ExpectedFindings {
		if err := validateExpectedFinding(i, f, seen); err != nil {
			return err
		}
	}
	return c.validateCategoryEquivalence()
}

// validateCategoryEquivalence rejects a case whose expected categories let ONE
// raised finding satisfy TWO of them.
//
// A repo-state case reaches CorroborationRate through the same category-recall
// scorer standard-v1 uses, which resolves each expected category through the
// equivalence families in equivalence.go. So a case expecting both a coarse
// category and a member of that category's family — "maintainability" beside
// "style" — has a recall denominator of 2 that a single finding can fill,
// reporting 1.0 where exact matching gave 0.5.
//
// Manifest.Validate applies exactly this rule to standard-v1, where the
// categories sit in one visible list per case. Here they are spread one per
// finding, so an author is LESS likely to notice the pairing, not more — which is
// why the check matters at least as much on this side.
//
// Two findings sharing one category stay legal: a case may plant two correctness
// defects, and the deduped set is then a single category with a denominator of 1.
func (c *RepoStateCase) validateCategoryEquivalence() error {
	norm := make([]string, 0, len(c.ExpectedFindings))
	seen := make(map[string]bool, len(c.ExpectedFindings))
	for _, f := range c.ExpectedFindings {
		n := normalize(f.Category)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		norm = append(norm, n)
	}
	for i, a := range norm {
		for j, b := range norm {
			if i == j {
				continue
			}
			for _, member := range familyOf(b) {
				if member == a {
					return fmt.Errorf("expected category %q is already satisfied by %q's equivalence family; "+
						"one raised finding would satisfy both and inflate recall", a, b)
				}
			}
		}
	}
	return nil
}

// validateExpectedFinding enforces one expected finding's contract.
func validateExpectedFinding(i int, f ExpectedFinding, seen map[string]bool) error {
	fid := strings.TrimSpace(f.ID)
	if fid == "" {
		return fmt.Errorf("expected_findings[%d]: id is required", i)
	}
	if err := validCaseToken(fmt.Sprintf("expected_findings[%d].id", i), fid); err != nil {
		return err
	}
	if seen[fid] {
		return fmt.Errorf("expected_findings[%d]: duplicate id %q", i, fid)
	}
	seen[fid] = true

	file := strings.TrimSpace(f.File)
	if file == "" {
		return fmt.Errorf("expected_finding %q: file is required", fid)
	}
	// file is repository-relative in the head state rather than case-relative — the
	// deliberate exception FORMAT.md names — so it is checked against the POSIX
	// rule rather than isSafeRelPath, which is filepath-separator-dependent. It
	// still must not escape: the matcher compares it to a reviewer-cited path, and
	// a `..` segment there would let a case claim a finding about a file outside
	// the materialized tree entirely.
	if !isSafeRelPOSIXPath(file) {
		return fmt.Errorf("expected_finding %q: file %q must be a relative repository path that does not escape the tree", fid, file)
	}
	if f.LineStart < 1 {
		return fmt.Errorf("expected_finding %q: line_start must be 1-based and positive, got %d", fid, f.LineStart)
	}
	if f.LineEnd < f.LineStart {
		return fmt.Errorf("expected_finding %q: line_end %d is before line_start %d", fid, f.LineEnd, f.LineStart)
	}
	if f.LineTolerance != nil && *f.LineTolerance < 0 {
		return fmt.Errorf("expected_finding %q: line_tolerance must not be negative, got %d", fid, *f.LineTolerance)
	}
	// outside_diff is REQUIRED, and its absence is exactly the defect a plain bool
	// would hide: the key omitted reads as false, the permissive value, and the case
	// silently stops measuring out-of-diff recall.
	if f.OutsideDiff == nil {
		return fmt.Errorf("expected_finding %q: outside_diff is required and must be written explicitly; "+
			"an omitted key would default to false and silently stop measuring the out-of-diff case", fid)
	}
	if strings.TrimSpace(f.Category) == "" {
		return fmt.Errorf("expected_finding %q: category is required", fid)
	}
	if strings.TrimSpace(f.Summary) == "" {
		return fmt.Errorf("expected_finding %q: summary is required; it is what lets a third party confirm the defect is really present", fid)
	}
	return nil
}

// checkFiles confirms the case's on-disk preconditions. Separate from Validate so
// the structural contract stays filesystem-free, and so these errors can name the
// resolved path an author has to go look at.
func (c *RepoStateCase) checkFiles() error {
	baseDir := filepath.Join(c.Dir, c.BaseTree)
	// Lstat, not Stat. The declared-path guard in Validate proves the STRING does
	// not escape; it cannot see that "base" is a symlink to somewhere else. Stat
	// follows the link and reports a perfectly ordinary directory, so the case would
	// materialize a tree from outside its own directory and AC7 would hold only
	// against authors who escape the obvious way. An author controls both the string
	// and the filesystem, so both have to be checked.
	fi, err := os.Lstat(baseDir)
	if err != nil {
		return fmt.Errorf("case %q base tree %q: %w", c.ID, c.BaseTree, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("case %q base tree %q is a symlink; a case must not reference a path outside its own directory", c.ID, c.BaseTree)
	}
	if !fi.IsDir() {
		return fmt.Errorf("case %q base tree %q is not a directory", c.ID, c.BaseTree)
	}
	// FORMAT.md: "base/ must not contain a .git directory. The loader creates the
	// repository; the case supplies only the working tree." A vendored .git would
	// make materialization depend on whatever history the author happened to copy
	// in, which is exactly the non-reproducibility committed diffs exist to avoid.
	if _, err := os.Stat(filepath.Join(baseDir, ".git")); err == nil {
		return fmt.Errorf("case %q base tree %q contains a .git directory; the case supplies only the working tree and the loader creates the repository",
			c.ID, c.BaseTree)
	}
	for _, f := range []struct{ field, rel string }{
		{"commit message", c.CommitMessage},
		{"diff", c.Diff},
	} {
		p := filepath.Join(c.Dir, f.rel)
		fi, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("case %q %s file: %w", c.ID, f.field, err)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("case %q %s file %q is not a regular file", c.ID, f.field, f.rel)
		}
	}
	return nil
}

// validCaseToken enforces FORMAT.md's id charset: lowercase letters, digits and
// '-' only. Case ids reach a PUBLISHED document through suite_case_ids, where the
// publication scrub rejects anything it would rewrite; restricting the charset at
// authoring time is what keeps that rejection from arriving after a paid run.
func validCaseToken(field, v string) error {
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("%s %q contains %q; ids use lowercase letters, digits and '-' only", field, v, string(r))
	}
	return nil
}

// isSafeRelPOSIXPath is isSafeRelPath for a path the FORMAT defines as POSIX
// regardless of host: expected_findings[].file is a repository path compared
// against a reviewer-cited path, both of which use '/' on every platform. Running
// it through filepath.Clean would make the check pass or fail differently on
// Windows for a suite whose bytes never changed.
func isSafeRelPOSIXPath(p string) bool {
	if strings.HasPrefix(p, "/") {
		return false
	}
	clean := path.Clean(p)
	if clean == "." {
		return false
	}
	return clean != ".." && !strings.HasPrefix(clean, "../")
}
