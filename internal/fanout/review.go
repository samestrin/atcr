package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/cache"
	"github.com/samestrin/atcr/internal/gitexec"
	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/tools"
	"github.com/samestrin/atcr/internal/validation"
)

// ErrPayloadFullyDropped is returned by buildPayloads when a non-empty input
// has every file shed by the byte budget. A too-small --byte-budget silently
// produced zero findings before this guard; it now fails loudly so callers
// can surface a clear diagnostic rather than firing the reviewer pool at an
// empty payload.
var ErrPayloadFullyDropped = errors.New("payload fully dropped by byte budget: every changed file exceeds the configured --byte-budget")

// ErrNoReviewableContent reports a resolved range whose commits changed no
// reviewable files (e.g. only merge or empty commits), so every payload mode
// built empty. gitrange.ErrEmptyRange catches zero-commit ranges earlier;
// this is the complementary guard for commit-bearing ranges with no file
// changes. PrepareReview returns it before scaffolding, so a vacuous review
// never creates a directory, repoints .atcr/latest, or reaches the provider
// pool.
var ErrNoReviewableContent = errors.New("no reviewable content in range")

// ErrAllFilesUnchanged reports a baseline (--all/--dir) re-scan whose every
// in-scope candidate was skipped by the incremental file-hash index — nothing
// changed since the last completed review (TD-010). It is distinct from
// ErrNoReviewableContent (a genuinely empty repository/scope): the CLI maps it
// to a successful exit 0 with a "nothing to review" notice instead of the
// exit-2 usage error, so the common CI re-run path does not fail as if the
// repo were empty.
var ErrAllFilesUnchanged = errors.New("no files changed since last review")

// ReviewConfig bundles the loaded configuration a review needs. Built by
// LoadReviewConfig so both the CLI and the MCP server discover config the same way.
type ReviewConfig struct {
	Registry    *registry.Registry
	Project     *registry.ProjectConfig
	Settings    registry.Settings
	PersonaDirs registry.PersonaDirs
}

// ReviewRange is the resolved git range as plain provenance fields. The engine
// package cannot import gitrange (package-boundary rule), so the caller resolves
// the range and passes the result in.
type ReviewRange struct {
	Base          string
	Head          string
	DetectionMode string
	DefaultBranch string
	CommitCount   int
}

// ReviewRequest is everything RunReview needs beyond the config: the repo/range,
// the branch + date used to derive the review id, the collision suffix, the run
// start time, and an optional id override.
type ReviewRequest struct {
	Repo       string // git work tree to diff
	Root       string // where .atcr lives (usually == Repo)
	Range      ReviewRange
	Branch     string
	Date       string // YYYY-MM-DD
	TimeSuffix string // HHMMSS collision suffix
	StartedAt  time.Time
	IDOverride string
	// OutputDir, when non-empty, redirects the whole review tree to this
	// (absolute) path instead of .atcr/reviews/<id>/, and suppresses the
	// .atcr/latest update. The id is still derived (for provenance/output) but
	// is not used for path construction. Mutually exclusive with IDOverride,
	// enforced by the CLI before the request is built.
	//
	// Security: arbitrary absolute paths (including outside the repo root) are
	// accepted by design; --output-dir is intended for trusted orchestrators that
	// own their output destination. PrepareReview rejects paths inside ReviewsRoot
	// to prevent invisible half-state reviews. Untrusted callers must validate the
	// path before populating this field.
	OutputDir string
	// Force, when true, overwrites an existing review target instead of failing
	// the collision (Epic 4.7 AC2): the prior tree is backed up to <dir>.bak and
	// a fresh directory is scaffolded. It applies to the IDOverride path (a
	// pre-existing .atcr/reviews/<id>/) and the non-empty OutputDir path; derived
	// ids never collide (claimReviewDir auto-suffixes) so Force is a no-op there.
	// Defaulting false preserves the safe fail-on-collision behavior for callers
	// that do not opt in (e.g. the MCP handler).
	Force bool
	// NoCache bypasses diff-cache READS for this run (the --no-cache flag, Epic
	// 5.2) while still WRITING fresh results, so the run refreshes any stale
	// entries and every subsequent run benefits. Defaulting false keeps caching
	// fully active for callers that do not opt out (e.g. the MCP handler).
	NoCache bool
	// NoIgnore bypasses the repo-root .gitignore/.atcrignore payload filter for
	// this run (the --no-ignore flag, Epic 26.0), so a deliberately-ignored file
	// can be reviewed on demand. Defaulting false keeps filtering active for
	// callers that do not opt out (e.g. the MCP handler).
	NoIgnore bool
	// Dir, when non-empty, scopes a baseline (--dir <path>) scan to a subtree: only
	// git-tracked files whose repo-root-relative path is Dir or nested under it (as a
	// full path-segment prefix) enter the payload (Sprint 35.0, Story 2). It is a
	// slash-normalized, repo-root-relative, validated scope (or "." for the whole
	// repo). Empty means an unscoped whole-repository scan (--all) or a diff review.
	// The scope filter lives in internal/payload/fullrepo.go; this field only carries
	// the validated value there. Defaulting empty preserves the whole-repo/diff
	// behavior for callers that do not set it (e.g. the MCP handler).
	Dir string
	// SprintPlanPath, when non-empty, points at a markdown sprint/epic plan whose
	// content is wrapped in a SCOPE CONSTRAINT block and prepended to every
	// reviewer's payload, immediately before the diff (Epic 12.2). It scopes the
	// review to the plan's active work items so reviewers suppress findings for
	// unrelated changes in the diff. A missing or empty file is ignored (the
	// review proceeds diff-wide); an unreadable file warns on stderr but does not
	// abort. The constraint becomes part of the rendered prompt, so the diff-cache
	// key invalidates correctly when the plan changes. Defaulting empty preserves
	// the unconstrained, diff-wide review for callers that do not set it (e.g. the
	// MCP handler).
	SprintPlanPath string
	// PRNumber is the pull-request number this run reviews, stamped onto the
	// run's append-only audit record (Epic 19.1). It is optional: 0 means "no PR
	// context" (a local review, or CI without a PR ref), in which case the audit
	// record omits the PR but is still written. The engine does not use it for
	// review logic — it is pure provenance threaded through to the audit hook.
	PRNumber int
	// Fresh bypasses the incremental file-hash skip for a baseline (--all/--dir)
	// scan (the --fresh flag, Sprint 35.0 Story 5): every in-scope tracked file is
	// reviewed regardless of a matching recorded hash. Only --fresh drives this; the
	// separate --force flag keeps its overwrite-existing-review meaning and is never a
	// skip-bypass alias (AC 05-03 EC1). It has no effect on a diff-range review (the
	// index is neither read nor written there). Defaulting false keeps incremental
	// skipping active for callers that do not opt out.
	Fresh bool
}

// ReviewResult is the outcome of a completed review run.
type ReviewResult struct {
	ID      string
	Dir     string
	Summary Summary
}

// LoadReviewConfig loads the registry and project config under root, validates
// the roster against the registry, resolves shared settings with the CLI
// overlays, and computes the persona search dirs.
func LoadReviewConfig(root string, cli registry.CLIOverrides) (*ReviewConfig, error) {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return nil, err
	}
	// Merge the optional project registry overlay (.atcr/registry.yaml) onto the
	// user registry; the merged loader enforces the project-provider trust gate.
	reg, err := registry.LoadMergedRegistry(regPath, root)
	if err != nil {
		return nil, err
	}
	proj, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath(root))
	if err != nil {
		return nil, err
	}
	if err := proj.ValidateAgainst(reg); err != nil {
		return nil, err
	}
	settings, err := registry.ResolveSettings(cli, proj, reg)
	if err != nil {
		return nil, err
	}
	// Defense-in-depth: every tier validates >= 0 at load time; re-check the
	// resolved value here so a future tier can never smuggle a negative budget
	// into ApplyByteBudget (AC 06-03 Error Scenario 1).
	if err := payload.ValidateBudget(settings.PayloadByteBudget); err != nil {
		return nil, err
	}
	return &ReviewConfig{
		Registry: reg,
		Project:  proj,
		Settings: settings,
		PersonaDirs: registry.PersonaDirs{
			Project:  filepath.Join(root, ".atcr", "personas"),
			Registry: filepath.Join(filepath.Dir(regPath), "personas"),
		},
	}, nil
}

// PreparedReview is a scaffolded-but-not-yet-executed review: the review
// directory exists with its payload artifacts, manifest (Partial=false,
// finalized by ExecuteReview), and .atcr/latest pointer written, and the roster
// is assembled into runnable slots. It is the handoff between the two review
// phases so the MCP server can scaffold synchronously (returning the id/dir/
// agent-count to the client immediately) and run the fan-out in the background,
// while the CLI runs both phases inline. The fields the executor needs are
// exported; manifest is finalized in place by ExecuteReview.
type PreparedReview struct {
	ID          string
	Dir         string
	Slots       []Slot
	TimeoutSec  int
	MaxParallel int
	// Repo and Head locate the read-only snapshot the tool harness reads (Epic
	// 2.0). Set from the request; ExecuteReview builds the snapshot→jail→dispatcher
	// only when a slot is tool-enabled. An empty Head leaves the harness unwired,
	// so a tool agent degrades to single-shot.
	Repo string
	Head string
	// Changed carries the per-file patch grounding data (Epic 14.1): the
	// head-side changed line ranges and changed-line texts for base..head.
	// WritePool uses it to drop findings whose FILE:LINE is not in the patch
	// before they reach the reconciler. nil on the diff-ingestion path (no live
	// base/head) or when the diff could not be computed, which disables the gate
	// (fail open).
	Changed payload.ChangedLines
	// GroundingDisabledReason is the human-readable reason the grounding gate was
	// off for this run (empty when enabled), threaded from computeGroundingData into
	// summary.json so a git-failure or diff-ingestion skip is auditable (Epic 14.1).
	GroundingDisabledReason string
	manifest                *payload.Manifest
	// cache is the diff cache for this review (Epic 5.2), rooted at
	// <root>/.atcr/cache and sized by the resolved cache_max_bytes. nil only if
	// caching could not be set up; ExecuteReview wires it into the engine when
	// non-nil. cacheNoRead carries the --no-cache request (bypass reads, still
	// write).
	cache       reviewCache
	cacheNoRead bool
	// baseline carries the incremental file-hash write-back state for a completed
	// --all/--dir run (Sprint 35.0 Story 4/5), captured at prepare time. nil for
	// diff-range reviews, which never touch the index. See CommitBaselineIndex.
	baseline *baselineWriteback
}

// baselineWriteback is the write-back state captured while a baseline payload is
// prepared, so the post-run index write records EXACTLY the files that were reviewed
// — using their review-time hashes (no second walk, no TOCTOU) and excluding the
// byte-budget-shed files that never reached an agent — and self-trims deleted paths
// without wiping out-of-scope entries.
type baselineWriteback struct {
	indexPath string
	preIndex  *payload.FileHashIndex // pre-run index; unchanged/skipped files keep their prior entry
	reviewed  map[string]string      // path -> review-time sha256 of files ACTUALLY in the payload
	tracked   []string               // full in-scope tracked set, for self-trim on a whole-repo run
	scope     string                 // "" / "." = whole repo (self-trims); non-empty = --dir subtree (no trim)
	// uncovered is the subset of reviewed whose chunk FAILED, so those files went
	// unreviewed and must NOT be recorded (Epic 35.2 / TD-013). Stamped by runEngine
	// from the raw pre-merge results, after which chunk identity is unrecoverable.
	// nil (the default) means full coverage — every reviewed file is recorded, which
	// is the pre-35.2 behavior and the path a resume/direct caller that never runs
	// the engine keeps.
	uncovered map[string]struct{}
	// lastRecorded/lastExcluded are the outcome of the most recent
	// CommitBaselineIndex call, surfaced to the caller through BaselineCoverage so
	// the operator-facing log line can state what the write-back ACTUALLY did
	// instead of inferring it from Summary.UnreviewedChunks (Epic 35.2 TD).
	lastRecorded int
	lastExcluded int
}

// BaselineCoverage reports what the most recent CommitBaselineIndex call did:
// how many reviewed files it recorded in the incremental index, and how many it
// excluded because the chunk carrying them failed. Both are 0 before the first
// commit and for a diff-range review (no baseline state).
//
// This is the coverage signal callers must log against — NOT
// ReviewResult.Summary.UnreviewedChunks, which mergeResultGroup sets only for a
// persona with a MIX of succeeded and failed chunks (internal/fanout/chunker.go).
// A WHOLLY failed persona contributes 0 to that count, so a run can record
// nothing at all while UnreviewedChunks reads 0.
func (p *PreparedReview) BaselineCoverage() (recorded, excluded int) {
	if p == nil || p.baseline == nil {
		return 0, 0
	}
	return p.baseline.lastRecorded, p.baseline.lastExcluded
}

// CommitBaselineIndex persists the incremental file-hash index after a COMPLETED
// baseline review, stamping every reviewed file with runID (Sprint 35.0 Story 4/5,
// AC 04-01). No-op for a diff-range review (p.baseline == nil). It records only the
// files that were actually reviewed (never the byte-budget-shed ones, which would
// otherwise be skipped-though-unreviewed on the next run), leaves unchanged/skipped
// files' prior entries intact, and self-trims paths no longer tracked — but ONLY on a
// whole-repo (--all) run: a scoped (--dir) run does not trim, so it never destroys
// out-of-scope entries recorded by a prior --all run. Returns any write error for the
// caller to log; an index-write failure must never fail an otherwise-successful
// review (AC 04-01 Error Scenario 1).
//
// Partial chunk coverage (Epic 35.2 / TD-013): files whose chunk FAILED are excluded
// via p.baseline.uncovered (stamped by runEngine from the raw pre-merge results), so a
// run where some chunks failed still persists the SUCCEEDED chunks' files and the next
// scan re-reviews only the genuinely uncovered ones. Before 35.2 the caller discarded
// the entire write-back on any unreviewed chunk, re-scanning the whole repository. The
// write is skipped outright only when coverage is zero.
func (p *PreparedReview) CommitBaselineIndex(runID string) error {
	if p == nil || p.baseline == nil {
		return nil
	}
	b := p.baseline
	recorded, excluded := 0, 0
	defer func() { b.lastRecorded, b.lastExcluded = recorded, excluded }()
	for path, hash := range b.reviewed {
		// Epic 35.2 / TD-013: skip the files whose chunk FAILED. They were dispatched
		// but never reviewed, so recording them would make the next scan skip them
		// though unreviewed — the one outcome this index must never produce. b.uncovered
		// is nil for a fully-covered run (and for a caller that never ran the engine),
		// which keeps this loop byte-identical to the pre-35.2 record-everything pass.
		if _, uncovered := b.uncovered[path]; uncovered {
			excluded++
			continue
		}
		b.preIndex.Record(path, hash, runID)
		recorded++
	}
	// Zero coverage → write NOTHING, not even the self-trim (Epic 35.2 AC3). Every
	// chunk failed, so there is no reviewed state to persist; saving here would emit a
	// trimmed-but-empty index that the next scan would read as authoritative. Skipping
	// the write leaves the prior index untouched and the next run does a full re-scan
	// — fail-open toward re-review, mirroring the caller's own Succeeded > 0 guard.
	if recorded == 0 && len(b.reviewed) > 0 {
		return nil
	}
	// The whole-repo test normalizes through payload.NormalizeScope — the SAME
	// helper filterByScope/TrackedInScope use — so a non-CLI caller's raw scope
	// ("./", a trailing slash, backslashes) is interpreted identically here:
	// without it, an effectively-whole-repo scan would skip the self-trim and
	// accumulate stale deleted-file entries.
	if ns := payload.NormalizeScope(b.scope); ns == "" || ns == "." {
		// A nil tracked set means the TrackedInScope keep-set walk hit a transient
		// git failure (it degrades to nil). Pass nil through to Trim so its
		// nil-keep contract — "keep everything; a git hiccup must not wipe the
		// index" — holds. Building an empty-but-non-nil keep map here would read
		// as "nothing tracked, trim all" and delete every entry, including the
		// files just Record()'d above. A non-nil-but-empty tracked set (the walk
		// succeeded; nothing is tracked in scope) still trims everything.
		var keep map[string]struct{}
		if b.tracked != nil {
			keep = make(map[string]struct{}, len(b.tracked))
			for _, tp := range b.tracked {
				keep[tp] = struct{}{}
			}
		}
		b.preIndex.Trim(keep)
	}
	return b.preIndex.Save(b.indexPath)
}

// AgentCount is the number of reviewer slots the prepared review will run.
func (p *PreparedReview) AgentCount() int { return len(p.Slots) }

// validateReviewRequest enforces the invariants shared by both review-preparation
// entry points (PrepareReview and PrepareReviewFromDiff): a non-empty roster, and
// the mutual exclusion of OutputDir and IDOverride. Centralizing them keeps the
// two entry points from drifting (the guard once diverged between them). The error
// names the request FIELDS, not the CLI flags — both functions are library API
// reachable by non-CLI callers (the MCP server, the benchmark harness), and the
// CLI emits its own flag-named usage error earlier at flag-parse time.
func validateReviewRequest(cfg *ReviewConfig, req ReviewRequest) error {
	if len(rosterNames(cfg.Project)) == 0 {
		return ErrEmptyRoster
	}
	if req.OutputDir != "" && req.IDOverride != "" {
		return fmt.Errorf("OutputDir and IDOverride are mutually exclusive")
	}
	return nil
}

// PrepareReview runs phase one of a review: build per-mode payloads, assemble
// the roster into parallel/serial slots (with fallback chains), derive the
// review id, scaffold the review directory, and write the payload artifacts, an
// in-progress manifest, and the .atcr/latest pointer. No agent runs here, so it
// returns quickly; ExecuteReview performs the fan-out.
//
// An empty roster is rejected before scaffolding so a no-op run never creates a
// review directory or repoints .atcr/latest. (LoadReviewConfig also rejects
// this earlier; PrepareReview is defended for direct/MCP callers.)
func PrepareReview(ctx context.Context, cfg *ReviewConfig, req ReviewRequest) (*PreparedReview, error) {
	if err := validateReviewRequest(cfg, req); err != nil {
		return nil, err
	}
	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head, req.NoIgnore)
	if err != nil {
		return nil, err
	}
	// Only a roster that resolved to payload modes can be "empty": a roster of
	// unknown agents builds zero modes and must keep its "not found in
	// registry" diagnostic from buildSlots below.
	empty := len(payloads) > 0
	for _, mp := range payloads {
		if mp.FileCount > 0 {
			empty = false
			break
		}
	}
	if empty {
		// Distinguish "every changed file was ignore-filtered" from a genuinely
		// empty range: the former is recoverable with --no-ignore, so hint at it
		// instead of the misleading "only merge or empty commits?" hypothesis.
		if allIgnored, n := rb.AllIgnored(); allIgnored {
			return nil, fmt.Errorf("%w: all %d changed file(s) in the range were excluded by .gitignore/.atcrignore; re-run with --no-ignore to review them", ErrNoReviewableContent, n)
		}
		return nil, fmt.Errorf("%w: the range contains commits but no changed files (only merge or empty commits?); review a range that changes files", ErrNoReviewableContent)
	}
	// Sprint-plan scope (Epic 12.2): read the plan once here and prepend its
	// SCOPE CONSTRAINT to every reviewer's payload via buildSlots. An unreadable
	// or oversized plan warns but never aborts the review.
	scopeConstraint, scopeWarn := resolveScopeConstraint(req, cfg.Settings.MaxSprintPlanBytes)
	if scopeWarn != "" {
		log.FromContext(ctx).Warn("scope constraint warning", "warn", scopeWarn)
	}
	slots, perAgentMode, err := buildSlots(cfg, payloads, req.Range, "", scopeConstraint, true)
	if err != nil {
		return nil, err
	}
	return finalizePreparedReview(ctx, cfg, req, payloads, perAgentMode, slots, cfg.Settings.PayloadMode, rb, false)
}

// finalizePreparedReview is the shared scaffold-and-assemble tail of the two
// review-preparation entry points (PrepareReview's git-range path and
// PrepareReviewFromDiff's ingestion path): it derives the review id, claims the
// review directory (honoring --output-dir/--id/--force), writes the payload
// artifacts, an in-progress manifest, and the .atcr/latest pointer, and wires the
// diff cache. payloadMode is recorded as the manifest's PayloadMode (the
// configured mode for the git path, "diff" for the ingestion path); the range
// provenance comes from req.Range, which the ingestion caller leaves empty.
func finalizePreparedReview(ctx context.Context, cfg *ReviewConfig, req ReviewRequest, payloads map[string]modePayload, perAgentMode map[string]string, slots []Slot, payloadMode string, rb *payload.RangeBuilder, baseline bool) (*PreparedReview, error) {
	// Derive the id unconditionally: for --output-dir the id is provenance-only
	// (written to the manifest and PreparedReview.ID but not used for the path),
	// while for --id and the default derived case the id IS the path component.
	id, err := ReviewID(req.IDOverride, req.Branch, req.Date, req.TimeSuffix, nil)
	if err != nil {
		return nil, err
	}
	var dir string
	switch {
	case req.OutputDir != "":
		// --output-dir redirects the whole tree to an explicit path. The id is
		// still derived above (for provenance/output) but never used for the
		// path, and .atcr/latest is left untouched below.
		if err = validateOutputDirRoot(req.OutputDir, req.Root); err != nil {
			return nil, err
		}
		// Defense-in-depth: reject system-directory output paths (/etc, /proc, /sys)
		// in the engine, not only the CLI flag parser. PrepareReview is public API
		// reachable by the MCP handler and direct callers; enforcing here means a
		// caller that sets OutputDir to a system path with Force=true is rejected
		// before forceBackupOutputDir performs any destructive backup. The CLI keeps
		// its own check too (exit 2), so this is additive, not a relocation.
		if err = validation.FilePath(req.OutputDir); err != nil {
			return nil, err
		}
		// --force backs up a non-empty target to <dir>.bak before scaffolding;
		// without it, ScaffoldOutputDir rejects a non-empty dir (Epic 4.7 AC2).
		if req.Force {
			backupPath, err := forceBackupOutputDir(ctx, req.OutputDir)
			if err != nil {
				return nil, err
			}
			if backupPath != "" {
				fmt.Fprintf(os.Stderr, "backed up prior review to %s\n", backupPath)
			}
		}
		dir, err = ScaffoldOutputDir(req.OutputDir)
	case req.IDOverride != "":
		// Explicit overrides keep their exact id, but the scaffold is exclusive:
		// a pre-existing directory (e.g. a client retrying atcr_review with the
		// same id while the first run is in flight) is rejected rather than
		// scaffolded into, so two fan-outs never share one review dir. --force
		// instead backs up the existing tree to <dir>.bak and scaffolds fresh
		// (Epic 4.7 AC2).
		if req.Force {
			backupPath, err := forceBackupReviewDir(ctx, req.Root, id)
			if err != nil {
				return nil, err
			}
			if backupPath != "" {
				fmt.Fprintf(os.Stderr, "backed up prior review to %s\n", backupPath)
			}
		}
		dir, err = ScaffoldReviewDir(req.Root, id)
	default:
		// Derived ids claim their directory atomically: creation is the
		// collision check, so two reviews of the same branch in the same second
		// get distinct dirs instead of interleaving writes in one.
		if req.Force {
			fmt.Fprintf(os.Stderr, "--force has no effect without --id or --output-dir; a new review directory was created\n")
		}
		id, dir, err = claimReviewDir(req.Root, id, req.TimeSuffix)
	}
	if err != nil {
		return nil, err
	}
	if err := writePayloadArtifacts(dir, payloads); err != nil {
		return nil, err
	}
	// Epic 12.2 provenance: write the resolved scope constraint to
	// payload/scope-constraint.txt so the on-disk artifact reflects what
	// each reviewer received. resolveScopeConstraint is called again here
	// (second read) rather than threading the result through the function
	// signature of finalizePreparedReview.
	if req.SprintPlanPath != "" {
		if sc, _ := resolveScopeConstraint(req, cfg.Settings.MaxSprintPlanBytes); sc != "" {
			if err := atomicWriteFile(filepath.Join(dir, "payload", "scope-constraint.txt"), []byte(sc)); err != nil {
				return nil, fmt.Errorf("writing scope constraint artifact: %w", err)
			}
		}
	}

	m := &payload.Manifest{
		Base:          req.Range.Base,
		Head:          req.Range.Head,
		DetectionMode: req.Range.DetectionMode,
		DefaultBranch: req.Range.DefaultBranch,
		CommitCount:   req.Range.CommitCount,
		PayloadMode:   payloadMode,
		Baseline:      baseline, // full-repo/dir scan; resume keys on this to skip range validation
		Dir:           req.Dir,  // --dir subtree scope; resume rebuilds the same scoped payload (Sprint 35.0)
		// Absolute repo root, recorded now because now is when it is known correct:
		// review runs with CWD == repo root by documented requirement, while a later
		// reconcile (CLI from another directory, or MCP whose CWD is unrelated to the
		// reviewed repo) has no way to re-derive it. It travels with the artifacts so
		// the .atcr/debt store resolves from the manifest instead of the reader's CWD
		// (Sprint 35.13 T6). req.Root is "where .atcr lives" and is relative ("." from
		// the CLI) at this point, so the Abs conversion is what makes it portable.
		Root:            absRoot(req.Root),
		MaxParallel:     cfg.Settings.MaxParallel,
		TimeoutSecs:     cfg.Settings.TimeoutSecs,
		PerAgentPayload: perAgentMode,
		// Per-file escalation (Epic 35.1): what a reviewer actually saw per file,
		// as opposed to PayloadMode/PerAgentPayload which record what was
		// configured. Both are omitempty, so a review where nothing escalated
		// produces a manifest byte-identical to earlier versions'.
		PerFilePayload:     perFileModes(payloads),
		EscalationDegraded: rb != nil && rb.EscalationDegraded(),
		Roster:             rosterNames(cfg.Project),
		StartedAt:          req.StartedAt,
		Partial:            false, // finalized by ExecuteReview once outcomes are known
		// Persist --no-ignore so a resume recovers the filtering mode from disk
		// rather than the resume request (the completed agents' context is locked).
		NoIgnore: req.NoIgnore,
		Stages:   []string{"review"}, // 1.x runs only the review stage (Epic 1.1 reserved field)
	}
	if err := WriteManifest(dir, m); err != nil {
		return nil, err
	}
	// Point .atcr/latest at the review before fan-out so `atcr status` can find an
	// in-progress run started by the non-blocking MCP handler. Skipped for
	// --output-dir: the pointer tracks interactive runs under .atcr/reviews/, and
	// an external orchestrator owns (and already knows) its output path.
	if req.OutputDir == "" {
		if err := WriteLatest(req.Root, id); err != nil {
			return nil, err
		}
	}
	// Wire the diff cache (Epic 5.2): reviewer outputs are content-addressed
	// under <root>/.atcr/cache (sibling of reviews/, already excluded from git)
	// and capped at the resolved cache_max_bytes. The store is shared across the
	// run's agents; ExecuteReview hands it to the engine.
	revCache := cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes)
	// Epic 14.1 grounding data: compute the per-file changed line ranges for the
	// range so WritePool can drop findings not anchored in the patch (see
	// computeGroundingData for the fail-open contract). The reason string records
	// WHY the gate is off (git failure vs. diff ingestion) in summary.json.
	changed, groundingDisabledReason := computeGroundingData(ctx, req, rb)
	return &PreparedReview{ID: id, Dir: dir, Slots: slots, TimeoutSec: cfg.Settings.TimeoutSecs, MaxParallel: cfg.Settings.MaxParallel, Repo: req.Repo, Head: req.Range.Head, Changed: changed, GroundingDisabledReason: groundingDisabledReason, manifest: m, cache: revCache, cacheNoRead: req.NoCache}, nil
}

// computeGroundingData builds the per-file patch grounding data for the request's
// range (Epic 14.1). Only the git-range path carries a base/head; a range-less
// request (the diff-ingestion path) returns nil, disabling the grounding gate. A
// git failure disables the gate (fail open, logged) rather than aborting the
// review — grounding is a filter, not a correctness gate. It is shared by the
// fresh-review (finalizePreparedReview) and resume (PrepareResume) paths so a
// resumed agent's fresh output is grounded identically to a first-run agent's.
//
// It also returns a human-readable reason the gate is off (empty when enabled),
// recorded in summary.json so a git-failure or diff-ingestion skip is auditable.
func computeGroundingData(ctx context.Context, req ReviewRequest, rb *payload.RangeBuilder) (payload.ChangedLines, string) {
	if req.Range.Base == "" || req.Range.Head == "" {
		// Both the diff-ingestion path and the --all/--dir baseline scan are range-less;
		// name both so a baseline review's summary.json provenance is not mislabeled
		// "diff ingestion" (Sprint 35.0 phase-2 gate LOW).
		return nil, "range-less request (diff ingestion or baseline scan): grounding not applicable"
	}
	// Guard the invariant that rb was constructed from the same req.Range it is
	// grounding. When rb != nil the changed lines come from rb's OWN base/head
	// (BuildChangedLines uses b.base/b.head), not req.Range, so a mismatched pair
	// would silently anchor grounding to the rb's range with no error. Every
	// current caller builds rb from the same req.Range in the same function, so
	// they agree today — but the pairing was implicit. Fail loudly (disable the
	// gate with an audible reason) rather than ground the wrong range. The
	// standalone (rb == nil) path builds from req.Range directly, so it is always
	// matched and skips this check.
	if rb != nil {
		rbb, rbh := rb.Range()
		if rbb != req.Range.Base || rbh != req.Range.Head {
			log.FromContext(ctx).Warn("grounding disabled: range builder range differs from request range",
				"builder_range", rbb+".."+rbh, "request_range", req.Range.Base+".."+req.Range.Head)
			return nil, "range builder range mismatch: builder " + rbb + ".." + rbh + " differs from request " + req.Range.Base + ".." + req.Range.Head
		}
	}
	// Reuse the payload builder's gitRunner (memoized --name-status / --unified=0
	// for this same range) when available (Epic 22.4); fall back to a standalone
	// runner for any caller path that has no builder (defensive — the git-range
	// paths always pass one).
	var (
		cl  payload.ChangedLines
		err error
	)
	if rb != nil {
		cl, err = rb.BuildChangedLines()
	} else {
		// Inherit --no-ignore so the standalone grounding path agrees with the
		// payload: without it, grounding would re-filter ignored files the payload
		// kept and silently drop every finding on them.
		var opts []payload.RangeOption
		if req.NoIgnore {
			opts = append(opts, payload.WithoutIgnoreFilter())
		}
		cl, err = payload.BuildChangedLines(ctx, req.Repo, req.Range.Base, req.Range.Head, opts...)
	}
	if err != nil {
		log.FromContext(ctx).Warn("grounding disabled: could not compute changed lines", "err", err)
		return nil, "changed-lines computation failed: " + err.Error()
	}
	if len(cl) == 0 {
		log.FromContext(ctx).Warn("grounding disabled: empty changed lines map")
		return nil, "empty changed-lines map (no reviewable changed lines)"
	}
	return cl, ""
}

// PrepareReviewFromDiff is the diff-file ingestion counterpart of PrepareReview:
// it builds the payload from a standalone unified diff (via the payload package's
// diff-file primitive) instead of from a git range, then scaffolds the review on
// the exact same path. Because a bare diff is the only available representation,
// every agent reviews it regardless of its configured payload mode — the payloads
// map is keyed solely to "diff" and buildSlots is forced to "diff", so a roster
// whose default mode is blocks/files still resolves cleanly. The resulting
// PreparedReview is accepted unchanged by ExecuteReview (same Slots/modePayload
// wiring); with no repo snapshot, Head is empty so any tool-enabled agent degrades
// to single-shot.
//
// req.Range is provenance-only here and may be left zero (a range-less diff has no
// base/head); req.OutputDir/IDOverride/Force are honored identically to
// PrepareReview, so callers (e.g. a benchmark run) can redirect output.
// PrepareReviewFromRepo is the baseline (--all / --dir) counterpart of
// PrepareReviewFromDiff (Sprint 35.0): it builds the payload from every
// ignore-filtered git-tracked file under req.Repo instead of from a git range or a
// diff, then scaffolds the review on the exact same finalizePreparedReview path.
// req.Range is left zero-valued (a baseline scan has no diff range), so grounding
// disables via computeGroundingData's range-less early return.
//
// Phase 2 scope (Sprint 35.0 TD-004, user-confirmed): the whole repository is
// reviewed as a SINGLE files-mode payload per persona through the UNMODIFIED
// buildSlots (AC 01-04 DoD), exactly mirroring PrepareReviewFromDiff. The
// per-(persona×chunk) multi-chunk fan-out (PartitionByBudget's consumer, the
// buildSlots baseline branch) lands in Phase 5; ApplyByteBudget here sheds to fit
// the window the same way every other prepare path does.
func PrepareReviewFromRepo(ctx context.Context, cfg *ReviewConfig, req ReviewRequest) (*PreparedReview, error) {
	if err := validateReviewRequest(cfg, req); err != nil {
		return nil, err
	}
	// Load the incremental file-hash skip index (Sprint 35.0 Story 4/5) and pass it,
	// with the --fresh bypass, into the candidate build. Load never errors/returns nil
	// (a missing/corrupt index degrades to a full scan), so a first-ever run behaves
	// as before.
	idx := payload.Load(payload.FileHashIndexPath(req.Repo), log.FromContext(ctx))
	payloads, err := buildRepoPayloads(ctx, cfg, req.Repo, req.NoIgnore, req.Dir, idx, req.Fresh)
	if err != nil {
		return nil, err
	}
	scopeConstraint, scopeWarn := resolveScopeConstraint(req, cfg.Settings.MaxSprintPlanBytes)
	if scopeWarn != "" {
		log.FromContext(ctx).Warn("scope constraint warning", "warn", scopeWarn)
	}
	filesMode := string(payload.ModeFiles)
	slots, perAgentMode, err := buildSlots(cfg, payloads, req.Range, filesMode, scopeConstraint, true, true)
	if err != nil {
		return nil, err
	}
	// No git range → no RangeBuilder: computeGroundingData's range-less early return
	// disables grounding (not applicable to a baseline scan).
	prep, err := finalizePreparedReview(ctx, cfg, req, payloads, perAgentMode, slots, filesMode, nil, true)
	if err != nil {
		return nil, err
	}
	// Capture the incremental-rescan write-back state (Sprint 35.0 Story 4/5): the
	// files ACTUALLY in the payload (post byte-budget) with their review-time hashes,
	// plus the full in-scope tracked set for self-trim. idx was only READ by the skip
	// filter, so it still holds the pre-run state — reused as the base the write-back
	// records onto, preserving unchanged/skipped files' prior entries.
	mp := payloads[filesMode]
	// The reviewed set for the write-back is EXACTLY the files the baseline fan-out
	// reviewed. The Phase 5 baseline branch in buildSlots partitions mp.Entries into
	// (persona × chunk) slots via PartitionByBudget, which drops NOTHING — every file
	// reaches a chunk (an oversized file becomes its own chunk), so every agent reviews
	// every file. The single-chunk and over-window fall-throughs to the bulk path also
	// keep the whole payload rather than shedding. So the reviewed set is the full
	// in-scope tracked payload: record every entry with its review-time hash.
	//
	// This SUPERSEDES the Phase 4 per-agent-shed bound (task 4.23): that bound recorded
	// only the files fitting the smallest per-agent budget because baseline then reused
	// the single-payload BULK path, which sheds mp.Entries per model window (Epic 19.10
	// F2). Under Phase 5's partition-based fan-out no file is ever shed per agent, so
	// bounding the recorded set to a per-agent budget UNDER-records every file the
	// multi-chunk scan reviewed beyond one chunk's worth — silently defeating the Story
	// 4/5 incremental skip on exactly the multi-chunk repos it targets. Recording the
	// full reviewed set is both correct and fail-open. [5.2.A HIGH]
	//
	// EXCEPTION — files the GLOBAL byte budget dropped from mp.Text: on the over-window
	// bulk fall-through (a per-agent chunk budget <= 0, e.g. a large --sprint-plan scope
	// reservation), the agent reviews mp.Text (the global-budget-KEPT subset), not the
	// full entry set, so recording a globally-dropped file would skip-it-though-unreviewed
	// next run. The multi-chunk partition path DOES review the full set, so excluding the
	// dropped files there only causes a (rare, global-budget-set) re-review next run —
	// fail-open, never a silent skip. With the default PayloadByteBudget=0 nothing is
	// dropped, so this records the full set unchanged. [5.14 gate MEDIUM]
	prep.baseline = captureBaselineWriteback(ctx, req.Repo, req.Dir, idx, mp)
	return prep, nil
}

// captureBaselineWriteback builds the incremental-rescan write-back state for a
// baseline (--all/--dir) review from the assembled files-mode payload: every
// entry the global byte budget KEPT is recorded with its review-time hash
// (globally-dropped files are excluded so they can never be
// skipped-though-unreviewed on the next run — the EXCEPTION case above), plus
// the full in-scope tracked set for the whole-repo self-trim. idx supplies the
// pre-run index state the write-back records onto (the fresh path's loaded
// index, which the skip filter only READ; the resume path loads the on-disk
// state fresh). Shared by PrepareReviewFromRepo and PrepareResume so the fresh
// and resumed baseline runs record identical state (TD-011).
func captureBaselineWriteback(ctx context.Context, repo, scope string, idx *payload.FileHashIndex, mp modePayload) *baselineWriteback {
	shed := make(map[string]struct{}, len(mp.Truncation.FilesDropped))
	for _, dp := range mp.Truncation.FilesDropped {
		shed[dp] = struct{}{}
	}
	reviewed := make(map[string]string, len(mp.Entries))
	for _, e := range mp.Entries {
		if _, dropped := shed[e.Path]; dropped {
			continue
		}
		reviewed[e.Path] = cache.HashText(e.Body)
	}
	return &baselineWriteback{
		indexPath: payload.FileHashIndexPath(repo),
		preIndex:  idx,
		reviewed:  reviewed,
		tracked:   payload.TrackedInScope(ctx, repo, scope),
		scope:     scope,
	}
}

// buildRepoPayloads assembles the single files-mode whole-repo payload for a
// baseline (--all / --dir) review. It is shared by PrepareReviewFromRepo (fresh)
// and PrepareResume (baseline resume) so a resumed baseline agent sees exactly the
// payload the completed agents saw — the resume "pending agents review what
// completed agents reviewed" invariant, applied to the tracked-repository scan
// instead of a git-range diff. scope is the --dir subtree ("" = whole repo); the
// fresh path passes req.Dir and resume passes the manifest's persisted Dir so both
// build the identical scoped candidate set. Returns a map keyed to the "files" mode.
//
// Errors mirror the diff/range prepare paths: a non-repo / read failure propagates
// verbatim (AC 01-04 ES2); zero reviewable files is ErrNoReviewableContent (Edge
// Case 3) before any scaffolding; an all-dropped byte budget is ErrPayloadFullyDropped.
func buildRepoPayloads(ctx context.Context, cfg *ReviewConfig, repo string, noIgnore bool, scope string, idx *payload.FileHashIndex, fresh bool) (map[string]modePayload, error) {
	// idx + fresh drive the incremental hash-skip (Sprint 35.0 Story 4/5): unchanged
	// files are dropped pre-chunking unless fresh forces a full re-scan or idx is nil.
	// The fresh --all/--dir path passes the loaded index (hash-skip active); the
	// baseline resume path deliberately passes nil (resume.go) to bypass the
	// hash-skip and rebuild the FULL superset of candidates — fail-open, so a
	// resumed run re-reviews everything rather than trusting a stale index.
	entries, stats, err := payload.BuildRepoEntriesWithStats(ctx, repo, log.FromContext(ctx), noIgnore, scope, idx, fresh)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		// Classify WHY the candidate set is empty (TD-008/TD-010) instead of the
		// one-size-fits-all "no reviewable tracked files":
		//   - HashSkipped > 0: the repo has tracked files and every candidate was
		//     skipped by the incremental hash index — nothing changed since the
		//     last completed review. This is the common CI re-run path: a
		//     successful no-op (the CLI maps ErrAllFilesUnchanged to exit 0 with a
		//     notice), NOT a usage error.
		//   - IgnoreFiltered > 0: candidates existed but .gitignore/.atcrignore
		//     dropped them all — recoverable, so hint at --no-ignore, mirroring
		//     the diff/range path's rb.AllIgnored() diagnostic.
		//   - otherwise: a genuinely empty repository/scope (or every file
		//     over-cap) — the original generic error.
		switch {
		case stats.HashSkipped > 0:
			return nil, fmt.Errorf("%w: %d file(s) unchanged since last review", ErrAllFilesUnchanged, stats.HashSkipped)
		case stats.IgnoreFiltered > 0:
			return nil, fmt.Errorf("%w: all %d tracked file(s) in scope were excluded by .gitignore/.atcrignore; re-run with --no-ignore to review them", ErrNoReviewableContent, stats.IgnoreFiltered)
		default:
			return nil, fmt.Errorf("%w: the repository contains no reviewable tracked files", ErrNoReviewableContent)
		}
	}
	kept, trunc := payload.ApplyByteBudget(entries, cfg.Settings.PayloadByteBudget)
	if trunc.AllDropped {
		return nil, fmt.Errorf("%w (mode files, dropped %d file(s))", ErrPayloadFullyDropped, len(trunc.FilesDropped))
	}
	if trunc.Truncated {
		// TD-012: do NOT claim "reviewing a subset of the repository" — the
		// baseline fan-out chunks the full pre-budget Entries via
		// PartitionByBudget, which drops nothing, so every enumerated file is
		// still reviewed across per-model chunks; the global budget only bounds
		// the concatenated payload text / per-chunk sizing. (The sole exception
		// is the over-window bulk fall-through, where an agent reviews the kept
		// subset — see the write-back EXCEPTION note in PrepareReviewFromRepo.)
		log.FromContext(ctx).Warn("full-repo scan: byte budget truncated the concatenated payload text; every enumerated file is still reviewed across per-model chunks",
			"kept", len(kept), "dropped", len(trunc.FilesDropped), "files_dropped", trunc.FilesDropped)
	}
	var totalLen int
	for _, e := range kept {
		totalLen += len(e.Body)
	}
	var b strings.Builder
	b.Grow(totalLen) // preallocate: a whole-repo payload can be large (parity with PrepareReviewFromDiff)
	for _, e := range kept {
		b.WriteString(e.Body)
	}
	// Entries keeps the raw pre-budget files so buildSlots re-sheds them per agent
	// against each model's window (Epic 19.10 F2), identical to buildPayloads.
	return map[string]modePayload{
		string(payload.ModeFiles): {Entries: entries, Kept: kept, Text: b.String(), FileCount: len(kept), Truncation: trunc},
	}, nil
}

func PrepareReviewFromDiff(ctx context.Context, cfg *ReviewConfig, req ReviewRequest, diffText string) (*PreparedReview, error) {
	if err := validateReviewRequest(cfg, req); err != nil {
		return nil, err
	}
	// Bound the in-memory diff, mirroring BuildEntriesFromDiffFile's cap: this
	// exported entry is the production ingestion deliverable (Epic 10.2 feeds it
	// externally-sourced diffs), so a hostile multi-GB diff must be rejected before
	// BuildEntriesFromDiff allocates its per-line index — honoring the epic's
	// MaxDiffBytes memory-exhaustion mitigation. payload.DefaultMaxDiffBytes mirrors
	// benchmark.MaxDiffBytes (10 MiB).
	if int64(len(diffText)) > payload.DefaultMaxDiffBytes {
		return nil, fmt.Errorf("diff ingestion: diff size %d exceeds max %d bytes", len(diffText), payload.DefaultMaxDiffBytes)
	}
	entries, err := payload.BuildEntriesFromDiff(diffText)
	if err != nil {
		return nil, err
	}
	// An empty diff (no reviewable files) must refuse before scaffolding, mirroring
	// PrepareReview's empty-payload guard so a no-op run never creates a directory
	// or repoints .atcr/latest.
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: the diff contains no reviewable files", ErrNoReviewableContent)
	}
	kept, trunc := payload.ApplyByteBudget(entries, cfg.Settings.PayloadByteBudget)
	if trunc.AllDropped {
		return nil, fmt.Errorf("%w (mode diff, dropped %d file(s))", ErrPayloadFullyDropped, len(trunc.FilesDropped))
	}
	// Surface PARTIAL truncation at the ingestion boundary: a subset review built
	// from an oversized diff is otherwise silent here (the per-agent status records
	// it downstream, but an operator gets no signal at the point the files were
	// dropped). AllDropped already returned above, so this is the some-but-not-all
	// case.
	if trunc.Truncated {
		log.FromContext(ctx).Warn("diff ingestion: byte budget truncated the review payload; reviewing a subset of the diff",
			"kept", len(kept), "dropped", len(trunc.FilesDropped), "files_dropped", trunc.FilesDropped)
	}
	var totalLen int
	for _, e := range kept {
		totalLen += len(e.Body)
	}
	var b strings.Builder
	b.Grow(totalLen)
	for _, e := range kept {
		b.WriteString(e.Body)
	}
	diffMode := string(payload.ModeDiff)
	payloads := map[string]modePayload{
		// Entries keeps the raw pre-budget diff files so buildSlots re-sheds them
		// per agent against each model's window (Epic 19.10 F2).
		diffMode: {Entries: entries, Kept: kept, Text: b.String(), FileCount: len(kept), Truncation: trunc},
	}
	// Sprint-plan scope (Epic 12.2): the ingestion path honors --sprint-plan too,
	// prepending the SCOPE CONSTRAINT to every reviewer's payload. An unreadable or
	// oversized plan warns but never aborts the review.
	scopeConstraint, scopeWarn := resolveScopeConstraint(req, cfg.Settings.MaxSprintPlanBytes)
	if scopeWarn != "" {
		log.FromContext(ctx).Warn("scope constraint warning", "warn", scopeWarn)
	}
	slots, perAgentMode, err := buildSlots(cfg, payloads, req.Range, diffMode, scopeConstraint, true)
	if err != nil {
		return nil, err
	}
	// Diff-ingestion has no git range, so no RangeBuilder: computeGroundingData's
	// range-less early return handles it (grounding not applicable).
	return finalizePreparedReview(ctx, cfg, req, payloads, perAgentMode, slots, diffMode, nil, false)
}

// runEngine wires the optional read-only tool harness for p's tool-enabled slots
// (a head snapshot → path jail → dispatcher, shared across the run, plus a
// per-agent transcript writer under poolDir), runs the fan-out under p's timeout,
// and returns the per-agent results together with the manifest review-stage entry
// (snapshot provenance already stamped). Best-effort harness setup: a snapshot or
// jail failure logs and degrades tool agents to single-shot rather than failing
// the review. Extracted from ExecuteReview so ExecuteResume runs the identical
// engine setup; the two differ only in how they persist the results.
func runEngine(ctx context.Context, completer Completer, p *PreparedReview, poolDir string) ([]Result, *payload.ReviewStage) {
	runCtx := ctx
	if p.TimeoutSec > 0 {
		var cancel context.CancelFunc
		// Epic 19.10 F6: a chunked persona needs ~N x the base wall clock — a serial
		// lane runs its N chunk-Slots sequentially, and a parallel lane's N chunks
		// queue behind max_parallel / a slow backend rather than truly overlapping.
		// Scale the overall deadline by the largest per-persona chunk total across ALL
		// lanes (clamped). This aggregate is the load-bearing seam for the production
		// roster (serial_agents: [], so the confirmed greta/vera/brad timeouts are
		// parallel): the per-call deadline in invokeAgent is a child of this runCtx and
		// cannot extend past it, so the parent must carry the room. No-op (max chunk
		// total <= 1) when nothing is chunked, preserving the flat deadline; unrelated
		// non-chunked agents stay bounded by their own unscaled per-call deadline.
		scaled := scaledTimeoutSecs(p.TimeoutSec, aggregateTimeoutFactor(p.Slots, p.MaxParallel))
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(scaled)*time.Second)
		defer cancel()
	}

	// Snapshot provenance for the manifest review stage (AC 03-02 / 03-03). Zero
	// unless a snapshot actually runs and succeeds below.
	var snapMode, snapHeadSHA, snapWorktreePath string
	// Seed the engine with the review_id-correlated context logger so every agent
	// log line is greppable by review (AC9 + AC10). FromContext returns a never-nil
	// discard logger if none is set.
	opts := []EngineOption{WithMaxParallel(p.MaxParallel), WithLogger(log.FromContext(ctx))}
	// Hand the diff cache to the engine (Epic 5.2). Non-tool agents whose
	// payload+model+persona key already has a stored result replay it instead of
	// calling the provider; nil cache (direct construction) leaves caching off.
	if p.cache != nil {
		opts = append(opts, WithCache(p.cache, p.cacheNoRead))
	}
	if anyToolAgent(p.Slots) && p.Head != "" {
		if root, cleanup, err := tools.NewSnapshotManager(p.Repo).SnapshotFor(p.Head); err != nil {
			log.FromContext(ctx).Warn("tool harness disabled (snapshot); tool agents degrade to single-shot", "head", p.Head, "err", err)
			snapMode = "failed" // snapshot attempted but failed; distinguishable from no-snapshot-attempted
		} else {
			defer cleanup()
			// A successful SnapshotFor call fixes the mode/head/path the tool harness
			// reviewed at (AC 03-02 Scenario 5), recorded even if the jail below fails.
			// Resolve the head to a full SHA for the manifest even if the caller passed
			// a symbolic ref or short SHA (e.g., tests constructing PreparedReview directly).
			// A resolution failure is logged but does not abort the review; the original
			// value is preserved as a best-effort fallback.
			headSHA := p.Head
			if resolved, err := resolveHeadSHA(p.Repo, p.Head); err == nil {
				headSHA = resolved
			} else {
				log.FromContext(ctx).Warn("could not resolve head SHA for manifest", "err", err)
			}
			snapMode, snapHeadSHA, snapWorktreePath = snapshotManifestFields(root, p.Repo, headSHA)
			if jail, jerr := tools.NewJail(root); jerr != nil {
				log.FromContext(ctx).Warn("tool harness disabled (jail); tool agents degrade to single-shot", "err", jerr)
			} else {
				disp := tools.NewDispatcher(jail, tools.DefaultLimits())
				rawBase := filepath.Join(poolDir, poolRawAgentDir)
				opts = append(opts, WithDispatcher(disp), WithTranscript(func(agent string) *tools.Transcript {
					dir := filepath.Join(rawBase, transcriptAgentDir(agent))
					if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
						log.FromContext(ctx).Warn("transcript dir creation failed", "agent", agent, "err", mkErr)
					}
					return tools.OpenTranscript(filepath.Join(dir, "transcript.jsonl"), agent)
				}))
			}
		}
	} else if anyToolAgent(p.Slots) {
		// A range-less review (baseline --all/--dir, diff ingestion) has no head
		// to snapshot, so the tool harness stays unwired and tool-enabled personas
		// silently degrade to single-shot. Surface that degradation — it was
		// previously invisible in the log stream (Sprint 35.0 TD-006).
		log.FromContext(ctx).Warn("tool harness unwired (no range head); tool agents degrade to single-shot")
	}

	// Reviewer runs get truncation failover (Epic 19.5): a truncated, zero-finding
	// response fails over to the slot's fallback instead of being recorded as a
	// silent clean review. The executor builds its own engine without this option.
	opts = append(opts, WithTruncationFailover())

	results := NewEngine(completer, opts...).Run(runCtx, p.Slots)

	// Baseline partial-coverage attribution — MUST run before mergeChunkResults below,
	// which discards chunk identity for good. No-op for a diff-range review.
	//
	// RESUME INVARIANT (do not "optimize" this): a resume dispatches only the PENDING
	// personas, so a completed persona's file is still reported uncovered when a
	// pending chunk carrying it fails. Relaxing that over-exclusion from the on-disk
	// agent statuses is UNSOUND — a resume rebuilds the FULL superset payload, so a
	// completed persona's clean record only proves it covered the original
	// hash-skipped subset. Pinned by
	// TestUncoveredBaselineFiles_ResumePartialSlotSetStaysFailOpen.
	if p.baseline != nil {
		p.baseline.uncovered = uncoveredBaselineFiles(ctx, p.Slots, results, p.baseline.reviewed)
	}

	// Chunked strategy (Epic 14.3): a persona fanned out into N chunk-slots comes
	// back as N results under the same Agent name; collapse them into one result
	// per persona BEFORE any downstream step so stage classification, the summary
	// tallies, and writePool (which rejects duplicate agent dirs) all see a single
	// logical source with Reviewer=<persona>. In bulk strategy names are unique, so
	// this is a no-op.
	//
	// Serial-lane personas run their chunk-slots sequentially, so their true
	// wall-clock duration is the sum of chunk durations; parallel-lane personas
	// take the maximum. Pass the serial set so mergeChunkResults can distinguish.
	serialAgents := make(map[string]bool, len(p.Slots))
	for _, s := range p.Slots {
		if s.Serial {
			serialAgents[s.Primary.Name] = true
		}
	}
	results = mergeChunkResults(results, serialAgents)

	// Classify the run into the manifest's review-stage entry and stamp the
	// snapshot provenance (nil when no agent ran with tools).
	stage := reviewStageFor(results)
	if stage != nil {
		stage.SnapshotMode = snapMode
		stage.HeadSHA = snapHeadSHA
		stage.SnapshotWorktreePath = snapWorktreePath
	}
	return results, stage
}

// ExecuteReview runs phase two: fan out the prepared roster under the global
// timeout, then write per-agent artifacts, the merged pool, summary.json, and
// the finalized manifest (Partial reflecting the outcome). The completer is
// injected so the CLI uses the real HTTP client and tests use a fake/httptest.
//
// Artifacts are always persisted, even when every agent fails; in that case the
// populated *ReviewResult is still returned alongside the wrapped
// ErrAllAgentsFailed so the caller can map it to exit 1 while the on-disk review
// remains for inspection. The background MCP path discards the error (status is
// read from disk) while the CLI maps it to the process exit code.
//
// Graceful-shutdown note: cooperative shutdown preserves agents that finished
// before the signal; in-flight agents share the cancelled parent ctx and are cut
// off (classified as timeout). Truly completing in-flight work would require
// running them on an uncancelled child ctx — a deliberate engine change out of
// scope here.
func ExecuteReview(ctx context.Context, completer Completer, p *PreparedReview) (*ReviewResult, error) {
	// Review metrics (Epic 4.4): count this review and time the whole execution
	// (fan-out + artifact persistence). The deferred Observe fires on every exit;
	// the terminal succeeded/failed/interrupted classification is recorded at each
	// return below. Instrumented here (not in the CLI) so the MCP server's
	// background reviews are counted identically.
	metrics.Counter(metrics.NameReviewsTotal).Inc()
	reviewStart := time.Now()
	defer func() {
		metrics.Histogram(metrics.NameReviewDurationSeconds).Observe(time.Since(reviewStart).Seconds())
	}()

	poolDir := filepath.Join(p.Dir, "sources", "pool")

	results, stage := runEngine(ctx, completer, p, poolDir)

	// Detect an external interrupt (SIGINT/SIGTERM cancelled the root context) so
	// the manifest can record it. The check is on the PARENT ctx, not runCtx: a
	// review timeout cancels only the child runCtx (DeadlineExceeded), while a
	// signal cancels the parent (Canceled). The engine has already collapsed both
	// into StatusTimeout per-agent, so the parent ctx is the only signal that still
	// distinguishes a user interrupt from an exhausted time budget.
	// Contract: callers must cancel the parent ctx only via a signal handler;
	// any other cancellation would be misreported as interrupted in the manifest.
	interrupted := errors.Is(ctx.Err(), context.Canceled)

	sum, err := writePool(poolDir, results, p.Changed, p.GroundingDisabledReason)
	if err != nil {
		// Persistence failed after the fan-out ran. Write a best-effort failure
		// marker so the status reader reports `failed` rather than leaving the
		// review stuck in_progress forever (Epic 1.5); if even this cannot be
		// written, stale inference covers it once the timeout elapses.
		writeFailureSummary(poolDir, results)
		// Stamp CompletedAt so the manifest is distinguishable from an unfinished
		// scaffold on disk; the failure-marker summary.json is the authoritative
		// outcome signal, but a zero CompletedAt left duration/partial-deriving
		// tools unable to tell a failed review from one still in flight.
		// Nil guard: PreparedReview may be constructed directly in tests without a manifest.
		if p.manifest != nil {
			p.manifest.CompletedAt = time.Now().UTC()
			p.manifest.Interrupted = interrupted
			_ = WriteManifest(p.Dir, p.manifest) // best-effort; stale inference covers the `failed` outcome but manifest.Interrupted is lost if this write also fails
		}
		recordReviewOutcome(interrupted, true)
		return nil, err
	}

	// Finalize the manifest into a local copy. p.manifest is only updated on a
	// successful write so a caller that retries with the same PreparedReview does
	// not observe stale completion data from a previous failed attempt.
	m := *p.manifest
	m.Partial = sum.Partial
	m.CompletedAt = time.Now().UTC()
	m.Interrupted = interrupted
	// Record the review-stage entry listing the tool-using agents (Epic 2.0, AC
	// 05-04), with snapshot provenance already stamped by runEngine. nil when no
	// agent ran with tools, so a pure 1.x roster's manifest is unchanged.
	m.Review = stage
	if err := WriteManifest(p.Dir, &m); err != nil {
		recordReviewOutcome(interrupted, true)
		return nil, err
	}
	p.manifest = &m

	res := &ReviewResult{ID: p.ID, Dir: p.Dir, Summary: sum}
	// The all-agents-failed gate runs after artifacts are on disk; the result is
	// returned regardless so the caller knows where to look.
	if _, outErr := Outcome(results); outErr != nil {
		recordReviewOutcome(interrupted, true)
		return res, outErr
	}
	recordReviewOutcome(interrupted, false)
	return res, nil
}

// RunReview is the full synchronous review flow used by the CLI: prepare the
// review directory then execute the fan-out inline. The completer is injected so
// the CLI uses the real HTTP client and tests use a fake/httptest.
//
// Artifacts are always persisted, even when every agent fails; in that case the
// populated *ReviewResult is still returned alongside the wrapped
// ErrAllAgentsFailed so the caller can map it to exit 1 while the on-disk review
// remains for inspection.
func RunReview(ctx context.Context, completer Completer, cfg *ReviewConfig, req ReviewRequest) (*ReviewResult, error) {
	p, err := PrepareReview(ctx, cfg, req)
	if err != nil {
		return nil, err
	}
	return ExecuteReview(ctx, completer, p)
}

// modePayload is one payload mode's built content shared by every agent using it.
//
// Entries holds the UNBUDGETED per-file entries for this mode so buildSlots can
// re-shed them to each agent's own model window at dispatch (Epic 19.10 F2) —
// Text/FileCount/Truncation remain the global-budget union used for the on-disk
// audit artifact and the empty-payload guard.
type modePayload struct {
	Entries []payload.FileEntry
	// Kept is Entries after the GLOBAL byte-budget pass (Settings.PayloadByteBudget)
	// — the survivor set behind the on-disk audit artifact, which is what Text,
	// FileCount, and Truncation below are all derived from.
	//
	// It is NOT the set any individual agent received. buildSlots re-sheds
	// Entries (the pre-budget list) against each model's own EffectiveByteBudget
	// at dispatch. That per-agent budget is capped by the same global budget and
	// both passes use the same escalation-aware drop order, which is monotone in
	// the budget — so each agent's delivered set is a SUBSET of Kept: a file
	// listed here may have reached no reviewer at all, while one that reached a
	// reviewer is always present.
	Kept       []payload.FileEntry
	Text       string
	FileCount  int
	Truncation payload.Truncation
}

// escalationOverrides copies the registry's optional payload_escalation block
// into payload's mirror type (Epic 35.1). registry.PayloadEscalationConfig and
// payload.EscalationOverrides are deliberately separate types — payload must not
// import registry — so this is the one place the two are bridged.
//
// It is a named pure function rather than a struct literal inline in
// buildPayloads so the copy is directly testable:
// TestPayloadEscalationMirrorsPayloadOverrides only compares the two shapes by
// reflection and would stay green with a line omitted or crossed
// (MinHunks: pe.MinCyclomatic), silently dropping or misrouting an operator's
// threshold. TestEscalationOverrides_CopiesEveryFieldToItsOwnTarget executes it.
func escalationOverrides(pe registry.PayloadEscalationConfig) payload.EscalationOverrides {
	return payload.EscalationOverrides{
		ChurnRatio:       pe.ChurnRatio,
		MinHunks:         pe.MinHunks,
		HunkGapLines:     pe.HunkGapLines,
		MinCyclomatic:    pe.MinCyclomatic,
		MaxFiles:         pe.MaxFiles,
		MaxSkeletonLines: pe.MaxSkeletonLines,
	}
}

// buildPayloads builds each distinct payload mode the roster uses exactly once.
// It returns the shared payload.RangeBuilder so the caller can compute grounding
// data (computeGroundingData) on the same gitRunner, reusing the memoized
// --name-status / --unified=0 diffs instead of re-spawning them (Epic 22.4).
func buildPayloads(ctx context.Context, cfg *ReviewConfig, repo, base, head string, noIgnore bool) (map[string]modePayload, *payload.RangeBuilder, error) {
	var opts []payload.RangeOption
	if noIgnore {
		opts = append(opts, payload.WithoutIgnoreFilter())
	}
	// Per-file escalation thresholds (Epic 35.1). The registry -> payload copy
	// lives in escalationOverrides, which is exercised directly by
	// TestEscalationOverrides_CopiesEveryFieldToItsOwnTarget.
	opts = append(opts, payload.WithEscalation(
		payload.ResolveEscalationConfig(escalationOverrides(cfg.Registry.PayloadEscalation))))
	rb := payload.NewRangeBuilder(ctx, repo, base, head, opts...)
	out := map[string]modePayload{}
	for _, mode := range neededModes(cfg) {
		entries, err := rb.BuildEntries(payload.PayloadMode(mode))
		if err != nil {
			return nil, nil, fmt.Errorf("building %s payload: %w", mode, err)
		}
		// Same escalation-aware order as the per-agent shed in buildSlots. Keeping
		// the two passes on one policy is what preserves Kept as an upper bound on
		// every agent's delivered set: the per-agent pass re-sheds the SAME
		// pre-budget Entries under a budget capped by this one, so a divergent
		// order here would let an agent be served a file the audit artifact (and
		// the manifest's per_file_payload, derived from Kept) never lists.
		kept, trunc := payload.ApplyByteBudgetPreferEscalated(entries, cfg.Settings.PayloadByteBudget, payload.PayloadMode(mode))
		if trunc.AllDropped {
			return nil, nil, fmt.Errorf("%w (mode %s, dropped %d file(s))", ErrPayloadFullyDropped, mode, len(trunc.FilesDropped))
		}
		var b strings.Builder
		for _, e := range kept {
			b.WriteString(e.Body)
		}
		// FileCount reflects the global-budget survivor set (post-truncation), not
		// the pre-budget total — the dropped files are recorded in trunc. Like Kept
		// it describes the audit artifact, not any one agent's delivered payload.
		// Entries keeps the raw pre-budget files so buildSlots re-sheds them per
		// agent against each model's window (Epic 19.10 F2).
		out[mode] = modePayload{Entries: entries, Kept: kept, Text: b.String(), FileCount: len(kept), Truncation: trunc}
	}
	// Every payload mode's entries are now materialized into out, so the
	// per-mode diff chunk caches (fc/plain/raw) and the line-range cache on the
	// shared gitRunner are dead weight. Grounding (computeGroundingData) reads
	// only the zero-context diff and the --name-status list, both retained, so
	// releasing the per-mode caches here lowers peak heap during grounding for
	// large multi-mode diffs without re-spawning any git process (Epic 22.4).
	rb.ReleaseModeCaches()
	return out, rb, nil
}

// perFileModes maps each file the escalation heuristic promoted above its
// payload's configured mode to the mode it was rendered in for the global-budget
// payload (Epic 35.1). Files left at the configured mode are omitted, so a review
// where nothing escalated returns nil and the manifest field is elided entirely.
//
// A file appearing in several mode payloads folds to the most-context mode any
// mode payload rendered it in, so the result does not depend on map iteration
// order.
func perFileModes(payloads map[string]modePayload) map[string]string {
	var out map[string]string
	for mode, mp := range payloads {
		// Kept, not Entries: Entries is the PRE-byte-budget list, so folding it in
		// would claim an escalated mode for files the global budget dropped from
		// the payload outright.
		//
		// Kept is an UPPER BOUND on the delivered set, not the delivered set
		// itself — buildSlots re-sheds Entries per agent against each model's own
		// (globally capped) window, so this map can name an escalated file that
		// every agent dropped. It never omits one a reviewer saw. Narrowing it to
		// what was actually dispatched requires the per-agent kept sets to come
		// back out of buildSlots; that is tracked as open technical debt against
		// this function.
		for _, e := range mp.Kept {
			if e.Mode == "" || string(e.Mode) == mode {
				continue
			}
			if out == nil {
				out = map[string]string{}
			}
			if prev, ok := out[e.Path]; ok {
				out[e.Path] = string(payload.HigherContextMode(payload.PayloadMode(prev), e.Mode))
				continue
			}
			out[e.Path] = string(e.Mode)
		}
	}
	return out
}

// neededModes returns the distinct payload modes across the whole roster.
func neededModes(cfg *ReviewConfig) []string {
	seen := map[string]bool{}
	var modes []string
	for _, name := range rosterNames(cfg.Project) {
		if ac, ok := cfg.Registry.Agents[name]; ok {
			m := ac.EffectivePayloadMode(cfg.Settings)
			if !seen[m] {
				seen[m] = true
				modes = append(modes, m)
			}
		}
	}
	return modes
}

// resolveScopeConstraint reads the sprint/epic plan named by req.SprintPlanPath
// and returns the formatted SCOPE CONSTRAINT block to prepend to every reviewer's
// payload (Epic 12.2), plus an optional human-readable warning the caller surfaces
// on stderr. The three dispositions:
//
//   - no plan (empty path, missing, or empty/whitespace-only file) → ("", ""):
//     the review proceeds diff-wide, silently (AC2).
//   - unreadable plan (permission error, a directory, IO error) → ("", warning):
//     the review proceeds unconstrained rather than aborting, after the caller
//     warns (AC3).
//   - oversized plan → (capped block, warning): the content is capped at
//     maxSprintPlanBytes (the resolved max_sprint_plan_bytes, plan 19.10 F9)
//     before injection so it cannot inflate every agent prompt past
//     payload_byte_budget, and the truncation is surfaced (AC6).
//
// It is pure (no I/O beyond the file read) and returns the warning rather than
// printing it, so the two prepare entry points can route it to their own stderr.
func resolveScopeConstraint(req ReviewRequest, maxSprintPlanBytes int64) (constraint, warning string) {
	raw, err := payload.ReadSprintPlan(req.SprintPlanPath, maxSprintPlanBytes)
	if err != nil {
		return "", fmt.Sprintf("sprint plan %q is unreadable; proceeding with a diff-wide review: %v", req.SprintPlanPath, err)
	}
	block, truncated := payload.ScopeConstraint(raw, maxSprintPlanBytes)
	if truncated {
		warning = fmt.Sprintf("sprint plan %q exceeded %d bytes and was truncated before injection", req.SprintPlanPath, maxSprintPlanBytes)
	}
	return block, warning
}

// buildSlots assembles the roster into ordered slots (parallel lane first, then
// serial) and returns the per-agent payload-mode map for the manifest. A
// build-time failure (unknown agent/provider, persona resolution, prompt render)
// aborts the whole run fail-fast: these are configuration errors the user must
// fix, not transient per-agent outcomes, so there is nothing useful to preserve
// — unlike the all-agents-failed runtime path, which keeps artifacts on disk.
// capScopeConstraintPlan trims the plan body of a formatted SCOPE CONSTRAINT block
// (the text between the BEGIN/END markers) to at most maxPlanBytes on a UTF-8
// boundary, preserving the wrapper instruction text. It returns the block unchanged
// when it has no markers, the plan already fits, or maxPlanBytes < 0. Extracted from
// buildSlots' one-time global cap so the identical trim is reused for the per-agent
// cap (Epic 19.10): the block is prepended uncounted in renderAgent, so each agent
// must bound the plan against its OWN window, not a single global budget.
func capScopeConstraintPlan(block string, maxPlanBytes int) string {
	if len(block) == 0 || maxPlanBytes < 0 {
		return block
	}
	const beginMark = "----- BEGIN SPRINT PLAN -----\n"
	const endMark = "\n----- END SPRINT PLAN -----"
	bs := strings.Index(block, beginMark)
	if bs < 0 {
		return block
	}
	planStart := bs + len(beginMark)
	rest := strings.Index(block[planStart:], endMark)
	if rest < 0 {
		return block
	}
	planEnd := planStart + rest
	if planEnd-planStart <= maxPlanBytes {
		return block
	}
	cut := planStart + maxPlanBytes
	for cut > planStart && block[cut]&0xC0 == 0x80 {
		cut--
	}
	return block[:cut] + block[planEnd:]
}

// capChunks bounds a baseline chunk set to at most max chunks by coalescing the
// tail (chunks[max-1:]) into a single final chunk — the same ceiling behavior
// chunkDiff applies to diff chunking (chunker.go:130). It never drops a file: the
// coalesced final chunk may exceed a single model window, but the alternative — an
// unbounded slot/goroutine/provider-call count for a huge repository — is the exact
// cost/DoS vector maxChunksPerAgent exists to prevent (AC 06-01 ES2). A set already
// within the cap is returned unchanged.
func capChunks(chunks [][]payload.FileEntry, max int) [][]payload.FileEntry {
	if max <= 0 || len(chunks) <= max {
		return chunks
	}
	capped := make([][]payload.FileEntry, 0, max)
	capped = append(capped, chunks[:max-1]...)
	var tail []payload.FileEntry
	for _, c := range chunks[max-1:] {
		tail = append(tail, c...)
	}
	capped = append(capped, tail)
	return capped
}

func buildSlots(cfg *ReviewConfig, payloads map[string]modePayload, rng ReviewRange, forceMode, scopeConstraint string, warnOversized bool, baselineOpt ...bool) ([]Slot, map[string]string, error) {
	// baseline enables the (--all / --dir) multi-chunk fan-out branch inside add
	// (Sprint 35.0 Phase 5, Decision 2). Variadic-optional so the diff/range callers
	// and the existing test call sites stay unchanged — the same idiom
	// mergeChunkResults uses for its optional serialAgents map.
	baseline := len(baselineOpt) > 0 && baselineOpt[0]
	// Budget-aware plan content cap: scopeConstraint is prepended uncounted in
	// renderAgent (Payload: scopeConstraint + payloadText), so a small PayloadByteBudget
	// causes the constraint alone to inflate the rendered prompt past the budget.
	// Truncate only the plan body (between the BEGIN/END markers) to
	// min(cfg.Settings.MaxSprintPlanBytes, budget/8), preserving the wrapper
	// instruction text (F9: the ceiling is the resolved max_sprint_plan_bytes).
	if budget := cfg.Settings.PayloadByteBudget; budget > 0 && len(scopeConstraint) > 0 {
		scopeConstraint = capScopeConstraintPlan(scopeConstraint, int(min(cfg.Settings.MaxSprintPlanBytes, budget/8)))
	}
	perAgentMode := map[string]string{}
	var slots []Slot
	// Fires at most once per run: set when the chunked strategy is requested over a
	// non-diff payload (no `diff --git` markers), where chunkDiff cannot split and
	// the strategy silently degrades to a single bulk chunk.
	warnedChunkedNoop := false
	// Fires at most once per (primary → fallback) lane: buildChain runs inside the
	// per-chunk loops, so an AC4 overflow warning emitted per call would repeat
	// once per chunk per chain link — up to 128 identical multi-line warnings for a
	// single 64-chunk persona with a 2-deep chain. The condition is a property of
	// the lane's two windows, identical for every chunk, so the operator needs it
	// once.
	warnedFallbackOverflow := map[string]bool{}

	// buildChain resolves the fallback chain for a primary. Extracted so both the
	// bulk one-slot path and the chunked per-chunk path attach identical chains
	// (a fallback reviews the same persona prompt/payload as its primary — here,
	// the same chunk).
	//
	// entries is the slot's payload as the FileEntry list the primary shipped —
	// the re-pack source a fallback needs to shed against its OWN budget (Epic
	// 35.16.5.4). It is passed rather than recovered for the reason documented on
	// Slot.entries, and it is EMPTY for the chunked-diff path, which has no such
	// list; buildFallbackAgent reads empty as "cannot re-fit" and keeps the
	// warn-and-ship behavior unchanged there. The re-render inputs (mode, rng,
	// agentScopeConstraint) travel with it because renderAgent needs all three and
	// this closure captured none of them.
	buildChain := func(name string, primary Agent, ac registry.AgentConfig, mode, agentScopeConstraint string, entries []payload.FileEntry) ([]Agent, error) {
		var fbs []Agent
		seen := map[string]bool{name: true}
		refit := fallbackRefit{
			primaryName:     name,
			primaryConfig:   ac,
			mode:            mode,
			rng:             rng,
			scopeConstraint: agentScopeConstraint,
			entries:         entries,
		}
		for fb := cfg.Registry.Agents[name].Fallback; fb != ""; fb = cfg.Registry.Agents[fb].Fallback {
			if seen[fb] {
				break // registry validation guarantees acyclic; defensive stop
			}
			seen[fb] = true
			// Suppress only after a warning was actually EMITTED, never merely
			// after a call. Packing is greedy, so a lane's first chunk can be the
			// small remainder that fits (silent) while a later one genuinely
			// overflows: marking the lane on every call would swallow that second
			// warning entirely, which is the opposite of the dedup's purpose.
			lane := name + "\x00" + fb
			agent, warned, err := buildFallbackAgent(cfg, primary, fb, warnOversized && !warnedFallbackOverflow[lane], refit)
			if err != nil {
				return nil, err
			}
			if warned {
				warnedFallbackOverflow[lane] = true
			}
			fbs = append(fbs, agent)
		}
		return fbs, nil
	}

	// wholePayloadPaths memoizes, per payload mode, the coverage tag for a baseline
	// bulk slot that ships the WHOLE payload. Every such persona gets an identical list
	// by construction, so they share one slice instead of each retaining a copy:
	// PreparedReview.Slots outlives the fan-out until CommitBaselineIndex, so an
	// 8-persona roster over a 20k-file monorepo would otherwise hold eight independent
	// 20k-element string slices for the entire review. A persona whose per-agent budget
	// SHED files never shares it — its tag must name only what it actually shipped.
	// The shared slice is read-only (uncoveredBaselineFiles only ranges over it).
	wholePayloadPaths := map[string][]string{}

	add := func(name string, serial bool) error {
		ac, ok := cfg.Registry.Agents[name]
		if !ok {
			return fmt.Errorf("agent %q not found in registry", name)
		}
		mode := forceMode
		if mode == "" {
			mode = ac.EffectivePayloadMode(cfg.Settings)
		}
		mp, ok := payloads[mode]
		if !ok {
			return fmt.Errorf("agent %q: no payload built for mode %q", name, mode)
		}
		perAgentMode[name] = mode

		// Per-agent SCOPE CONSTRAINT budgeting (Epic 19.10, HIGH TD). The plan block is
		// prepended UNCOUNTED against this agent's window in renderAgent, so a large plan
		// on a small-window model reintroduces the dax overflow on the --sprint-plan path.
		// (B) cap the plan body to THIS model's own budget — EffectiveByteBudget/8, further
		// bounded by max_sprint_plan_bytes when set — so a big plan cannot starve the diff;
		// (A) the diff/chunk budgets below then reserve len(agentScopeConstraint) so plan +
		// diff together fit the window. Base the cap on eff/8 (not min with a possibly-0
		// max_sprint_plan_bytes, which would blank the plan).
		agentScopeConstraint := scopeConstraint
		agentBudget := payload.EffectiveByteBudget(ac.Model, ac.ContextWindowTokens, defaultMaxTokens)
		// agentWindow is the same resolution, in tokens. Both are resolved ONCE here
		// and referenced everywhere below: this pair was previously recomputed inline
		// at five further call sites, so a signature change had to find nine sites
		// where three suffice — and an incomplete edit across duplicated call sites is
		// exactly the failure this epic was chartered to prevent. Nothing enforced
		// that the copies agreed; hoisting makes disagreement impossible.
		agentWindow := payload.ContextWindowTokens(ac.Model, ac.ContextWindowTokens)
		if len(agentScopeConstraint) > 0 && agentBudget > 0 {
			planCap := agentBudget / 8
			if mspb := cfg.Settings.MaxSprintPlanBytes; mspb > 0 && mspb < planCap {
				planCap = mspb
			}
			agentScopeConstraint = capScopeConstraintPlan(scopeConstraint, int(planCap))
		}

		// DESIGN NOTE (Sprint 35.0, Phase 1 Decision 2 — pinned so Phase 5 task 5.2
		// does not re-litigate it). The baseline (--all / --dir) fan-out gains a NEW
		// branch here, a SIBLING to the reviewStrategyChunked branch below — NOT a
		// modification of it. chunkDiff/diff-marker parsing stays completely
		// untouched: baseline chunks are []FileEntry groups from
		// internal/payload/fullrepo.go's PartitionByBudget (task 2.8) and never pass
		// through chunkDiff (verified 2026-07-24: chunkDiff splits on `diff --git`
		// markers only, which raw file contents lack). Resolves AC 06-01
		// (06-01-chunk-persona-fanout-completeness.md) and AC 06-02
		// (06-02-per-persona-source-merge-collapse.md) without contradiction:
		//
		//   Slot construction (AC 06-01 HP1, Story-Specific DoD): one Slot per
		//     (persona × chunk) pair — for a C-chunk repo and this persona, add C
		//     slots. So a 3-chunk / 2-persona baseline scan yields exactly 6 slots
		//     (C × P), never C + P. Each chunk-slot's Primary is renderAgent'd over
		//     that chunk's payload text, structurally mirroring the reviewStrategy-
		//     Chunked loop's `for _, ct := range chunks { ... slots = append(...) }`.
		//
		//   Unchanged persona name (AC 06-01 DoD, AC 06-02 HP1): every chunk-slot
		//     keeps this persona's plain configured `name` (Primary.Name), so
		//     mergeChunkResults' group-by-Agent collapse and the 14.2 consensus
		//     filter's per-persona counting see the persona as ONE voice with N
		//     chunk-results, not N distinct voices. The collapse key must never
		//     drift from the plain name (Phase 5 task 5.3's top risk).
		//
		//   Per-chunk fallback chain (AC 06-01 EC1): each of this persona's chunk-
		//     slots resolves its fallback chain independently via buildChain(name,
		//     primary) (review.go:934) so a fallback reviews the SAME chunk as the
		//     primary it substitutes for — never a different chunk. buildChain is
		//     reused verbatim; it already attaches identical chains for the bulk and
		//     chunked paths.
		//
		//   Serial-lane duration (AC 06-01 EC2): the serialAgents map (review.go:
		//     650-655) is keyed by this persona's unchanged plain name, so a serial-
		//     lane persona's N chunk-results merge to a duration equal to the SUM of
		//     the N per-chunk durations (not the max) via mergeResultGroup's existing
		//     serial semantics — no baseline-specific duration logic.
		//
		//   Fail-fast on unknown agent (AC 06-01 ES1): the baseline branch runs
		//     inside this same `add` closure, so an agent name absent from
		//     cfg.Registry.Agents aborts the whole review before any chunk dispatch
		//     with `agent "<name>" not found in registry`, matching diff-mode.
		//
		//   maxChunksPerAgent cap (AC 06-01 ES2): the chunker.go:99 cap (64) carries
		//     over unmodified — PartitionByBudget's chunk count is deterministically
		//     bounded (task 1.1 note), and the (persona × chunk) slot count per
		//     persona is capped consistently rather than spawning unbounded slots.
		//     The resulting total slot count is logged pre-dispatch for cost
		//     visibility (AC 06-01 Performance / Throughput).
		//
		//   Collapse reuse — ZERO modification (AC 06-02 HP1/HP2, EC1-EC4, ES1):
		//     baseline (persona × chunk) Result values flow through the SAME
		//     unconditional `results = mergeChunkResults(results, serialAgents)` call
		//     (review.go:656) that diff-mode already runs — no new call site.
		//     mergeChunkResults / mergeResultGroup (chunker.go:154 / :196) and
		//     writePool (artifacts.go:106) need NO changes for baseline provenance:
		//     same-name results collapse to exactly personaCount source dirs (not
		//     C × P), findings union across chunks, any-chunk-succeeded => Status OK,
		//     FallbackUsed/FallbackModel union+modal, token/telemetry accumulate, and
		//     writePool's duplicate-agent-directory guard is never tripped BECAUSE
		//     collapse already ran (AC 06-02 ES1 is a regression assertion, not new
		//     code). Single-chunk baseline scans (AC 06-01 HP2) fall through to the
		//     bulk one-slot-per-persona path exactly like a single-chunk diff.
		//
		// Phase 5 task 5.2 implements this branch against AC 06-01's RED tests (5.1).
		if baseline {
			// Partition THIS persona's whole-repo file entries into byte-budget-bounded
			// chunks sized to its own model window (Epic 19.10 F2 per-agent sizing),
			// building one Slot per (persona × chunk) under the persona's UNCHANGED plain
			// name so mergeChunkResults collapses the chunk-results into one source. The
			// per-agent chunk budget is derived identically to the bulk path below:
			// EffectiveByteBudget capped by the global PayloadByteBudget, less the SCOPE
			// CONSTRAINT reservation.
			chunkBudget := appliedByteBudget(agentBudget, cfg.Settings.PayloadByteBudget, agentScopeConstraint)
			// A non-positive per-agent budget (over-window model, or the scope reservation
			// consumed the whole window) cannot drive PartitionByBudget's machine-budget
			// contract — fall through to the bulk path, which keeps the whole payload and
			// records the honest overflow degradation rather than erroring.
			if chunkBudget > 0 && len(mp.Entries) > 0 {
				chunks, err := payload.PartitionByBudget(mp.Entries, chunkBudget)
				if err != nil {
					return err
				}
				// Bound the slot count at maxChunksPerAgent (chunker.go:99) the same way
				// chunkDiff does: coalesce the tail into the final chunk so the fan-out never
				// spawns an unbounded slot/goroutine/provider-call count while every file is
				// still delivered whole (AC 06-01 ES2 — capped, never dropped).
				chunks = capChunks(chunks, maxChunksPerAgent)
				if len(chunks) > 1 {
					// Per-agent sizing record (Epic 19.10 F6/F8): every chunk-Slot of this
					// persona carries the same window/budget and the persona's full chunk count,
					// mirroring the chunked path. action "chunk" is the default no-loss
					// degradation. A pre-dispatch cost-visibility line (AC 06-01 Performance)
					// is emitted once per persona under warnOversized.
					if warnOversized {
						fmt.Fprintf(os.Stderr, "atcr: baseline scan: agent %q fanned out across %d chunk(s) (%d file(s))\n", name, len(chunks), len(mp.Entries))
					}
					chunkSizing := agentSizing{
						// The budget the partition ACTUALLY used, not the raw per-agent
						// one: effective_budget must mean the same quantity on every
						// payload strategy or a consumer comparing two agents is
						// comparing unlike numbers.
						effectiveBudget: chunkBudget,
						resolvedWindow:  agentWindow,
						chunkTotal:      len(chunks),
						action:          degradationChunk,
					}
					for _, ck := range chunks {
						var pb strings.Builder
						chunkFiles := make([]string, 0, len(ck))
						for _, e := range ck {
							pb.WriteString(e.Body)
							chunkFiles = append(chunkFiles, e.Path)
						}
						// Neutral per-chunk truncation: whole-payload truncation is a scan-wide
						// event decided upstream, not a per-chunk property (mirrors the chunked path).
						primary, rerr := renderAgent(cfg, name, ac, mode, pb.String(), len(ck), payload.Truncation{}, rng, agentScopeConstraint, chunkSizing)
						if rerr != nil {
							return rerr
						}
						// Tag this slot with the files its chunk carries (Epic 35.2 / TD-013).
						// Tagged HERE — at capChunks output, after tail-coalescing — so the
						// identity matches the slot actually dispatched. runEngine reads it
						// pre-merge to attribute a failed chunk to its files, so a partially
						// failed baseline run records the succeeded chunks' files instead of
						// discarding the whole write-back.
						//
						// The Primary is tagged here, and buildFallbackAgent copies this tag
						// onto each fallback because a fallback reviews the SAME chunk as the
						// primary it substitutes for — except when it re-fits that chunk to
						// its own budget (Epic 35.16.5.4), where it carries the smaller set it
						// actually kept. Attribution reads the tag of whichever chain member
						// SERVED the slot, so either way it names the files really reviewed.
						primary.chunkFiles = chunkFiles
						// Carry this chunk's entries (Epic 35.16.5.4 T1) so a fallback whose
						// own budget cannot hold the chunk can re-pack it. ck IS the shipped
						// set — pb above is built from exactly these entries — so the carried
						// list, the rendered payload, and chunkFiles above are the same fact
						// recorded three ways and cannot drift.
						fbs, cerr := buildChain(name, primary, ac, mode, agentScopeConstraint, ck)
						if cerr != nil {
							return cerr
						}
						slots = append(slots, Slot{Primary: primary, Fallbacks: fbs, Serial: serial, entries: ck})
					}
					return nil
				}
				// len(chunks) <= 1 → single-chunk baseline scan: fall through to the bulk
				// one-slot-per-persona path (AC 06-01 HP2), sizing the whole payload to this
				// model's window exactly as a single-chunk diff does.
			}
		}

		// Chunked strategy (Epic 14.3): bin-pack this persona's diff into multiple
		// context-limited calls, one Slot per chunk. Every chunk-slot keeps the
		// SAME persona name, so mergeChunkResults collapses their results into one
		// raw/agent/<persona>/ source with Reviewer=<persona> (AC4) — the 14.2
		// consensus filter still counts the persona once. A run that yields a
		// single chunk (small diff, or one file) falls through to the bulk path so
		// there is nothing to merge.
		//
		// A BASELINE run never enters this branch (Epic 35.2 TD). Its partitioning is
		// owned by the baseline branch above, which splits by FILE against this agent's
		// own byte budget and tags each slot with the files it carries. chunkDiff splits
		// by TEXT instead, on column-0 diff markers — which a files-mode baseline payload
		// really can carry, since tracked content such as a *.patch fixture holds literal
		// `diff --git` lines. The resulting slots are not file-attributable: the markers
		// inside a patch fixture name the patch's OWN targets, not the repo paths being
		// reviewed, so any tag recovered from the chunk text would vouch for the wrong
		// files. Untagged slots in turn vouch for nothing, so a single failure collapsed
		// coverage to zero and the write-back degraded to the pre-35.2 discard-everything
		// behavior on this configuration. Reaching here as a baseline means the byte
		// partition already yielded ONE chunk — the payload fits this model's window —
		// so falling straight through to the bulk path costs no coverage and keeps the
		// slot exactly attributable.
		if cfg.Settings.ReviewStrategy == reviewStrategyChunked && !baseline {
			// A payload with no `diff --git` markers (a whole-file files-mode payload)
			// has nothing for chunked bin-packing — which targets diff hunks — to split
			// on, so the strategy is a no-op for it. Warn once so the operator knows.
			// Keyed off the ABSENCE of git-diff markers, NOT countDiffFiles(mp.Text):
			// the chunker now also recognizes `=== FILE:` markers (to segment escalated
			// entries in MIXED diff payloads), so countDiffFiles no longer returns 0 for
			// a files-mode payload — the git-diff-marker check stays the reliable "is
			// this a diff to bin-pack" signal, and unlike the payloads-map key (which is
			// the AGENT's configured mode) it reflects the payload's actual content.
			// Gated by warnOversized so the resume rebuild path stays quiet.
			if warnOversized && !warnedChunkedNoop && !hasGitDiffMarker(mp.Text) && mp.FileCount > 1 {
				fmt.Fprintf(os.Stderr, "atcr: warning: review_strategy=chunked has no effect for payload mode %q (no diff --git markers to bin-pack)\n", mode)
				warnedChunkedNoop = true
			}
			// Per-chunk line budget: an explicit operator-set max_context_lines wins
			// (least surprise); otherwise derive maxLines from THIS agent's model
			// window (Epic 19.10 F3), so a 32k model gets more, smaller chunks and a
			// 144k model gets fewer — both from the same diff, zero files dropped.
			// chunkDiff itself is unchanged; only the source of ml changes.
			ml := payload.ChunkMaxLines(ac.Model, ac.ContextWindowTokens, defaultMaxTokens)
			// Clamp the model-derived budget to the operator's global byte ceiling.
			// ContextWindowTokensCap admits a 10,000,000-token declaration, which
			// derives ~728,000 lines (~34.9 MB) per chunk — past any real proxy
			// request-body limit. Applied BEFORE the branch below so an explicit
			// max_context_lines still replaces it verbatim (least surprise), and the
			// scope-constraint reservation is taken out of the clamped value rather
			// than the raw one.
			ml = payload.ClampLinesToByteBudget(ml, cfg.Settings.PayloadByteBudget)
			if ac.MaxContextLines != nil && *ac.MaxContextLines > 0 {
				ml = ac.EffectiveMaxContextLines()
			} else if len(agentScopeConstraint) > 0 {
				// (A) reserve per-chunk line headroom for the SCOPE CONSTRAINT block
				// prepended to EVERY chunk in renderAgent. The plan is capped to
				// EffectiveByteBudget/8 above, i.e. at most ml/8 lines, so reserving ml/8
				// covers it without importing the payload byte/line ratio. An explicit
				// operator max_context_lines wins (least surprise) and is left untouched.
				ml -= ml / 8
				if ml < 1 {
					ml = 1
				}
			}
			// The chunked twin of the bulk agentBudget == 0 arm below. A window that
			// reserves no input budget at all is honest degradation on either
			// strategy, but ChunkMaxLines' minChunkLines floor hides it here: the
			// run presents as an ordinary "chunk" plan while shipping chunks the
			// model provably cannot hold. Mark the same action and emit the same
			// warning the bulk path does, so the operator's too-small declaration
			// surfaces regardless of review_strategy. Keyed on agentBudget (not ml),
			// because an explicit max_context_lines overrides the line budget
			// without making the window any bigger.
			chunkAction := degradationChunk
			if agentBudget == 0 {
				chunkAction = degradationOverflow
				if warnOversized {
					fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: resolved window %d tokens leaves no input budget once the %d-token output cap and the fixed prompt overhead are reserved (effective budget 0); chunking at the %d-line floor (may overflow) rather than sizing to the window\n",
						name, agentWindow, defaultMaxTokens, ml)
				}
			}
			chunks := chunkDiff(mp.Text, ml)
			// Warn on any chunk that is a lone file exceeding the budget (it could
			// not be split). This runs over EVERY chunk — not just multi-chunk
			// fan-outs — so a diff that is a single oversized file (which chunkDiff
			// returns as one chunk) still surfaces the documented warning before
			// falling through to the one-slot path. The warning is suppressed on the
			// resume rebuild path because PrepareResume reconstructs pending slots and
			// the operator was already notified during the original preparation.
			if warnOversized {
				for _, ct := range chunks {
					fileCount := countDiffFiles(ct)
					lineCount := countLines(ct)
					// == 1 (not <= 1): a chunk with zero diff-file markers is a non-diff
					// payload, not a single oversized file — labeling it "a single file's
					// diff" would mislabel a whole multi-file files/blocks payload as one
					// file. Only a genuine single-file diff (exactly one marker) qualifies.
					if fileCount == 1 && lineCount > ml {
						fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: a single file's diff (%d lines) exceeds max_context_lines (%d); sent as its own oversized chunk\n", name, lineCount, ml)
					} else if fileCount > 1 && lineCount > ml {
						// A MULTI-file chunk can only exceed ml at the maxChunksPerAgent
						// ceiling: normal packing seals a chunk before it overflows, so the
						// sole way many files land in one over-budget chunk is chunkDiff's
						// coalesce-into-final-chunk cap (chunker.go:130). Flag it pre-dispatch
						// with distinct "ceiling" wording so the broken "each chunk fits the
						// window" invariant is not silent; if the oversized call then fails it
						// is additionally counted in UnreviewedChunks post-dispatch.
						fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: a %d-file chunk (%d lines) exceeds max_context_lines (%d); the %d-chunk ceiling was reached, so remaining files were coalesced into one oversized chunk (may overflow the model)\n", name, fileCount, lineCount, ml, maxChunksPerAgent)
					}
				}
			}
			if len(chunks) > 1 {
				// Per-agent sizing record for the chunked path (Epic 19.10 F6/F8):
				// every chunk-Slot of this persona carries the SAME window/budget and
				// the persona's full chunk count (len(chunks), not 1), so timeout
				// scaling (F6) and the diagnosability fields (F8) see one consistent
				// regime. maxLines is the ml this diff was actually split on (operator
				// override or model-derived). The action is "chunk" — the default,
				// no-loss degradation path.
				chunkSizing := agentSizing{
					// The globally-capped, scope-reserved budget — the same quantity
					// the bulk path records below. The per-chunk LINE budget (maxLines)
					// stays derived from the UNCAPPED window: that asymmetry is
					// deliberate and documented in payload.ChunkMaxLines, and unifying
					// the recorded byte budget does not disturb it.
					effectiveBudget: appliedByteBudget(agentBudget, cfg.Settings.PayloadByteBudget, agentScopeConstraint),
					resolvedWindow:  agentWindow,
					maxLines:        ml,
					chunkTotal:      len(chunks),
					action:          chunkAction,
				}
				for _, ct := range chunks {
					fileCount := countDiffFiles(ct)
					// Truncation is a diff-wide event decided by buildPayloads, not a
					// per-chunk property. Passing the whole-payload truncation into every
					// chunk would make each chunk's prompt/status claim the same dropped
					// files were truncated, which is misleading because the dropped files
					// may not even appear in this chunk. Use a neutral truncation for
					// individual chunks; the single-chunk/bulk path below still carries
					// the real diff-wide truncation.
					primary, err := renderAgent(cfg, name, ac, mode, ct, fileCount, payload.Truncation{}, rng, agentScopeConstraint, chunkSizing)
					if err != nil {
						return err
					}
					// No entries carried (Epic 35.16.5.4 T1): chunkDiff splits TEXT on
					// column-0 diff markers, so there is no FileEntry list for this chunk
					// and no honest way to build one — a list recovered from the chunk text
					// would be the shape-dependent reconstruction Slot.entries exists to
					// avoid. The slot therefore carries EMPTY, and buildFallbackAgent
					// declines to re-fit it and keeps the pre-epic warn-and-ship.
					fbs, err := buildChain(name, primary, ac, mode, agentScopeConstraint, nil)
					if err != nil {
						return err
					}
					slots = append(slots, Slot{Primary: primary, Fallbacks: fbs, Serial: serial})
				}
				return nil
			}
		}

		// Bulk path (or a chunked run that produced a single chunk): one slot over
		// the whole payload, shed to THIS agent's own model window.
		//
		// Epic 19.10 F2: size the payload per agent, reserving the output cap
		// (defaultMaxTokens). Previously every agent shared one global-budget Text,
		// so a 32k-window model overflowed — the confirmed dax boundary 24577 input
		// + 8192 output > 32768 — while a 144k model was starved of context it could
		// safely use. The configured PayloadByteBudget (when > 0) still caps the
		// per-agent budget; a small window is never inflated past what it can hold.
		// Re-shed the UNBUDGETED entries so the budget is genuinely per-model. Do
		// NOT re-hoist this back into one shared value.
		// Per-agent sizing record for the bulk path (Epic 19.10 F8): resolve this
		// model's window and effective input budget up front so they are recorded for
		// EVERY sized agent (the "was this agent sized" signal), independent of
		// whether shedding actually dropped a file. appliedBudget is the byte budget
		// the payload was sized to (per-model, capped by any global PayloadByteBudget).
		appliedBudget := appliedByteBudget(agentBudget, cfg.Settings.PayloadByteBudget, agentScopeConstraint)
		bulkDegradation := ""
		bulkText, bulkFileCount, bulkTrunc := mp.Text, mp.FileCount, mp.Truncation
		// The entries this slot will ACTUALLY ship. It starts as the GLOBAL-budget
		// survivor set — which is what bulkText (mp.Text) contains, and therefore what
		// the no-shed and AllDropped arms genuinely dispatch — and is narrowed to the
		// kept subset when the per-agent budget sheds files. The baseline coverage tag
		// and the fallback re-pack source below both read it, so the tag can never name
		// a file the rendered prompt does not contain and a re-fit can never offer a
		// fallback a file its primary never sent.
		//
		// mp.Kept, not mp.Entries: Entries is the PRE-budget list, so on a run where
		// the global payload_byte_budget dropped files the two differ and mp.Entries
		// would name files absent from mp.Text. Every production payload builder
		// populates Kept from its global budget pass (buildPayloads, and the two
		// decomposed builders in this file), so the Entries fallback is reachable
		// only from hand-built modePayload literals in tests — kept as a defensive
		// default rather than a production path.
		bulkEntries := mp.Kept
		if bulkEntries == nil {
			bulkEntries = mp.Entries
		}
		bulkShed := false
		if agentBudget == 0 && len(mp.Entries) > 0 {
			// Epic 19.10 TD-002: a model whose window <= output cap + prompt overhead
			// makes EffectiveByteBudget return 0. REACHABLE since Epic 35.16.5.1: it was
			// defense-in-depth while ContextWindowTokens floored at 32768 (eff >= 71680),
			// but an agent may now declare context_window_tokens as low as 1, and any
			// declaration at or below defaultMaxTokens + promptOverheadTokens (12288)
			// lands here.
			//
			// This arm used to skip the shed below and dispatch the FULL global-budget
			// payload. That inverted the Conservatism NFR at exactly the wrong moment:
			// honestly declaring a small window sent MORE bytes than leaving the agent
			// undeclared (which resolves the 32768 default and sheds to ~71680). A zero
			// budget is not "no opinion about size" — it is the strongest possible
			// overflow signal, so it is handled as an overflow.
			//
			// Two arms, mirroring how a shed-driven overflow is already handled below:
			//
			//   1. The operator's on_overflow policy decides first. fail/fallback return
			//      their typed errors, which propagate out of add()/buildSlots() and
			//      hard-fail the run PRE-DISPATCH rather than sending a payload known
			//      not to fit.
			//   2. Otherwise ship the SMALLEST single entry. An empty payload is not an
			//      option (it returns a false-clean "no findings" review — the same class
			//      ErrPayloadFullyDropped guards), and one small file is the least this
			//      can send while still being a real review. It is also the arm most
			//      likely to actually succeed: promptOverheadTokens is deliberately
			//      over-estimated and defaultMaxTokens is a cap rather than a floor, so
			//      the true usable window is wider than this arithmetic admits.
			if p := cfg.Settings.OnOverflow; p == OverflowFail || p == OverflowFallback {
				if _, err := applyOverflowPolicy(p, "", 0, mp.Entries, appliedBudget); err != nil {
					return err
				}
			}
			smallest := smallestEntry(mp.Entries)
			bulkEntries = []payload.FileEntry{smallest}
			bulkShed = len(mp.Entries) > 1
			bulkText, bulkFileCount = smallest.Body, 1
			bulkTrunc = payload.Truncation{Truncated: bulkShed, FilesDropped: droppedPathsExcept(mp.Entries, smallest.Path)}
			bulkDegradation = degradationOverflow
			if warnOversized {
				// Name the resolved window and the reservation that consumed it: the
				// operator's next action is to raise or drop the declaration, and
				// "effective budget 0" alone does not say which number to change or
				// what it has to clear.
				fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: resolved window %d tokens leaves no input budget once the %d-token output cap and the fixed prompt overhead are reserved (effective budget 0); sending only the smallest file (%s) instead of the whole payload — raise or drop its context_window_tokens declaration\n",
					name, agentWindow, defaultMaxTokens, smallest.Path)
			}
		}
		if appliedBudget > 0 && len(mp.Entries) > 0 {
			// PreferEscalated, not the plain pass: escalating a file to a
			// higher-context mode makes it the largest entry, so plain largest-first
			// would shed exactly the file the heuristic flagged as hardest to review
			// — on precisely the tight-window agents escalation targets (Epic 35.1).
			// It falls back to largest-first when the escalated bytes alone exceed
			// this agent's budget, so the AllDropped arm below is reached no more
			// often than before.
			kept, trunc := payload.ApplyByteBudgetPreferEscalated(mp.Entries, appliedBudget, payload.PayloadMode(mode))
			// F4 on_overflow dispatch (Epic 19.10 TD-004): the payload overflows THIS
			// agent's window (a file had to be shed). Route the fail/fallback arms through
			// applyOverflowPolicy so their typed errors propagate out of add()/buildSlots()
			// and hard-fail the whole run PRE-DISPATCH — the same precedent as the "agent
			// not found"/"no payload built" errors above — rather than silently degrading.
			// truncate keeps the byte shed below (applyOverflowPolicy's truncate arm
			// delegates to this same ApplyByteBudget), and chunk keeps the whole-payload
			// overflow net below (real bin-packing is the review_strategy=chunked path,
			// unreachable from a single bulk slot). Gated on ACTUAL overflow so a payload
			// that fits is never hard-failed by on_overflow=fail.
			if trunc.Truncated || trunc.AllDropped {
				switch cfg.Settings.OnOverflow {
				case OverflowFail, OverflowFallback:
					if _, err := applyOverflowPolicy(cfg.Settings.OnOverflow, "", 0, mp.Entries, appliedBudget); err != nil {
						return err
					}
				}
			}
			// Never dispatch an EMPTY per-agent payload. If even a single file exceeds
			// this model's window, ApplyByteBudget drops everything (AllDropped);
			// shipping "" would make the agent silently return "no findings" — a
			// false-clean review, the same silent-zero-findings class ErrPayloadFullyDropped
			// guards against on the global path. Keep the whole (global-budget) payload
			// and warn instead; Phase 3's on_overflow policy (chunk/truncate) is the real
			// net for a file larger than a small window. This also keeps a chunked-strategy
			// single oversized-file chunk lossless when it falls through to this bulk path.
			if trunc.AllDropped {
				// The payload is known to exceed the model window, yet we deliberately
				// dispatch the whole thing rather than an empty (false-clean) review.
				// Record this high-risk state so status.json/summary.json can distinguish
				// an at-risk over-window reviewer from a clean comfortable fit.
				bulkDegradation = degradationOverflow
				if warnOversized {
					fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: no file fits its model window (%d-byte budget); sending the whole payload (may overflow) rather than an empty review\n", name, appliedBudget)
				}
			} else {
				var pb strings.Builder
				for _, e := range kept {
					pb.WriteString(e.Body)
				}
				bulkText, bulkFileCount, bulkTrunc = pb.String(), len(kept), trunc
				// Shed only when a file was actually dropped: a no-op budget pass returns
				// the same entry set, and that persona can still share the whole-payload tag.
				bulkEntries, bulkShed = kept, len(kept) != len(mp.Entries)
				// The per-agent shed dropped files to fit this model's window — a lossy
				// degradation. Record it as the diagnosability degradation_action (F8).
				if trunc.Truncated {
					bulkDegradation = degradationTruncate
				}
			}
		}
		bulkSizing := agentSizing{
			effectiveBudget: appliedBudget,
			resolvedWindow:  agentWindow,
			action:          bulkDegradation,
		}
		primary, err := renderAgent(cfg, name, ac, mode, bulkText, bulkFileCount, bulkTrunc, rng, agentScopeConstraint, bulkSizing)
		if err != nil {
			return err
		}
		// Epic 35.2 / TD-013: a BASELINE run reaching the bulk path has exactly one slot
		// for this persona covering the whole payload, so tag it with every entry the
		// write-back could record. Without the tag the slot would contribute no coverage
		// (the deliberate fail-open default for untagged slots — see
		// uncoveredBaselineFiles) and a single-chunk baseline scan whose sibling persona
		// failed would needlessly re-review everything.
		//
		// The tag names bulkEntries — what this slot actually SHIPS — not the pre-shed
		// mp.Entries. Coverage is a UNION across personas, so an over-tagged succeeded
		// slot would vouch for files a sibling persona's failed chunk never reviewed,
		// and those files would be recorded and then silently skipped next scan.
		//
		// BOTH arms below take the tag from bulkEntries for exactly that reason. A
		// global payload_byte_budget shed is the case where mp.Entries and the
		// shipped set diverge, and the no-shed (shared-tag) branch is still reachable
		// after one — the AllDropped whole-payload dispatch leaves bulkShed false
		// while mp.Text carries only the kept subset. When nothing was shed anywhere
		// the two lists coincide, so the memoized shared tag is the same set either
		// way — but it is bulkEntries either way, never the pre-budget list.
		if baseline {
			if bulkShed {
				primary.chunkFiles = entryPaths(bulkEntries)
			} else {
				shared, ok := wholePayloadPaths[mode]
				if !ok {
					// bulkEntries here, not mp.Entries: on the arms that reach
					// this branch after a GLOBAL budget shed (the AllDropped
					// whole-payload dispatch), bulkEntries is mp.Kept — the set
					// mp.Text actually carries — while mp.Entries still names the
					// dropped files. With no shed at all the two are identical.
					shared = entryPaths(bulkEntries)
					wholePayloadPaths[mode] = shared
				}
				primary.chunkFiles = shared
			}
		}
		// bulkEntries is what this slot SHIPS (the kept subset after any per-agent
		// shed, or the single smallest entry on the zero-budget arm) — never the
		// pre-shed mp.Entries. Carrying the pre-shed set would offer a fallback files
		// the primary's prompt does not contain, and the same reasoning that makes the
		// baseline coverage tag read bulkEntries above applies here.
		fbs, err := buildChain(name, primary, ac, mode, agentScopeConstraint, bulkEntries)
		if err != nil {
			return err
		}
		slots = append(slots, Slot{Primary: primary, Fallbacks: fbs, Serial: serial, entries: bulkEntries})
		return nil
	}

	for _, name := range cfg.Project.Agents {
		if err := add(name, false); err != nil {
			return nil, nil, err
		}
	}
	for _, name := range cfg.Project.SerialAgents {
		if err := add(name, true); err != nil {
			return nil, nil, err
		}
	}
	return slots, perAgentMode, nil
}

// defaultMaxTokens is the output-token cap applied to every reviewer call.
// Generous on purpose: reasoning/thinking models spend output budget on
// chain-of-thought before emitting visible content, so a tight cap makes them
// finish mid-reasoning and return an empty review (the doctor self-test warns of
// exactly this). The empty-content case is still caught by the reasoning_content
// fallback in llmclient; this headroom lets the clean Content path win first.
const defaultMaxTokens = 8192

// maxTokensPtr returns a fresh pointer to defaultMaxTokens for an Invocation
// (MaxTokens is a pointer so an explicit value always serializes).
func maxTokensPtr() *int { v := defaultMaxTokens; return &v }

// agentSizing carries the per-agent payload-sizing values buildSlots computed for
// a reviewer from its OWN model window (Epic 19.10). renderAgent folds them into
// the diff-cache key (F7) and records them on the Agent for diagnosability (F8)
// and timeout scaling (F6). The zero value (an unsized/direct-constructed caller)
// collapses the cache sizing token to "0:0" — the pre-F7 key — and leaves every
// diagnosability field absent.
// The degradation actions recorded on agentSizing.action (and surfaced as
// Agent.DegradationAction / status.json degradation_action). Constants rather
// than literals because the value is written at six sites and read by artifact
// consumers, so a typo at any one of them would silently produce an action no
// reader recognizes. The strings deliberately coincide with OverflowChunk /
// OverflowTruncate — the on_overflow POLICY names — but the two vocabularies are
// distinct: a policy is what the operator configured, an action is what actually
// happened, and degradationOverflow has no policy counterpart at all.
const (
	// degradationChunk: the diff was split across window-sized chunks. No loss.
	degradationChunk = "chunk"
	// degradationTruncate: files were shed to fit the budget. Lossy.
	degradationTruncate = "truncate"
	// degradationOverflow: the payload was dispatched knowing it exceeds what the
	// model can hold, because no smaller framing was available.
	degradationOverflow = "overflow"
)

type agentSizing struct {
	effectiveBudget int64 // per-agent input byte budget the payload was sized to (0 = unsized)
	// ContextWindowTokens(model, declared) — the window in tokens, from the
	// agent's own context_window_tokens when it declared one, else the static
	// table or the default.
	resolvedWindow int
	maxLines       int    // per-model chunk line budget (0 = bulk/non-chunked)
	chunkTotal     int    // chunks this persona's diff was split into (0/1 = unchunked)
	action         string // degradation action: one of the degradation* constants above, or "" (none)
}

// sizingToken renders the per-agent effective-budget/chunk-plan identifier folded
// into diffCacheKey (Epic 19.10 F7). "%d:%d" of (effective byte budget, chunk
// maxLines); the bulk path passes maxLines 0, and a fully unsized agent renders
// "0:0" which diffCacheKey treats as "no sizing applied" (pre-F7 key preserved).
func sizingToken(effectiveBudget int64, maxLines int) string {
	return fmt.Sprintf("%d:%d", effectiveBudget, maxLines)
}

// diffCacheKey derives the Epic 5.2 diff-cache key for a review call. It keys on
// the FULL rendered prompt — which already embeds the payload, the resolved
// persona, the per-agent scope focus (Epic 2.2), and the base/head refs, i.e.
// every text input the model receives — plus the model id, the resolved backend
// (baseURL), the temperature (the tuning param that changes the output), and the
// per-agent sizing token (Epic 19.10 F7, below).
// Keying on the rendered prompt rather than the raw payload+persona is what
// guarantees a scope or persona change invalidates the entry instead of silently
// replaying a stale review. The backend is folded in because atcr supports
// arbitrary OpenAI-compatible providers: two roster agents can share an identical
// model id (e.g. "gpt-4o-mini" or a local model name) served by different
// endpoints, and without the backend in the key the second would replay the
// first endpoint's review — a cross-provider cache collision. The sizing token is
// folded in for the same reason: Task 02/03 size the payload per agent, so two
// runs with identical prompt/model/backend/temperature can still differ only in
// how many bytes/lines of context were retained (e.g. a context-window override
// changed between runs while the retained bytes happen to render identical prompt
// text) — without the sizing token in the key the second would replay a review
// produced under a DIFFERENT sizing regime. A cache-regime change SHOULD
// invalidate: an agent whose effective budget previously produced a non-"0:0"
// token gets a new key, which is the intended F7 behavior, not a bug. MaxTokens is
// constant across review agents (defaultMaxTokens), so it is intentionally
// omitted. min_severity/max_findings are deterministic post-LLM filters and are
// correctly NOT in the key.
func diffCacheKey(prompt, model, baseURL string, temperature *float64, sizing string) string {
	temp := "default"
	if temperature != nil {
		temp = strconv.FormatFloat(*temperature, 'g', -1, 64)
	}
	// Fold the backend into the tuning token (NUL-separated so a backend string
	// can never bleed into the temperature) so distinct endpoints never share an
	// entry. An empty baseURL (e.g. direct Agent construction in tests) collapses
	// to the pre-existing temperature-only token, preserving old keys.
	tuning := temp
	if baseURL != "" {
		tuning = baseURL + "\x00" + temp
	}
	// Fold the per-agent sizing token in, NUL-separated, same as baseURL. "0:0" (or
	// empty) means "no per-agent sizing applied" and collapses to the baseURL+temp
	// token above, preserving every pre-F7 on-disk key and existing cache_test
	// assertion for bare/unsized agents.
	if sizing != "" && sizing != "0:0" {
		tuning = tuning + "\x00" + sizing
	}
	return cache.Key(cache.HashText(prompt), model, tuning)
}

// codeContextFor recovers the per-file breakdown of an agent's payload text for
// the model-invocation observation seam (Epic 35.0).
//
// It cannot fail: an unparseable or non-file-shaped payload yields nil, and an
// observer reads that as "no code context". An audit helper must never be able
// to fail the review it is observing.
func codeContextFor(mode, payloadText string) []hookobs.CodeRef {
	entries := payload.EntriesFromRenderedPayload(payload.PayloadMode(mode), payloadText)
	if len(entries) == 0 {
		return nil
	}
	refs := make([]hookobs.CodeRef, len(entries))
	for i, e := range entries {
		refs[i] = hookobs.CodeRef{Path: e.Path, Body: e.Body}
	}
	return refs
}

// entryPaths returns the Path of every entry, in order — the coverage tag shape a
// baseline slot carries.
func entryPaths(entries []payload.FileEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	return paths
}

// renderAgent builds a fully-rendered review Agent for `name` over an explicit
// payload text and its file-count/truncation metadata. buildSlots' bulk path
// uses it for the whole-diff (bulk) payload; the chunked strategy (Epic 14.3)
// calls it once per bin-packed chunk so every chunk-slot carries the SAME persona
// identity but a different diff subset. Passing the payload text in (rather than reading a
// modePayload) is the seam that lets a chunk render its own slice of the diff
// and report its own file count in the prompt.
func renderAgent(cfg *ReviewConfig, name string, ac registry.AgentConfig, mode, payloadText string, fileCount int, trunc payload.Truncation, rng ReviewRange, scopeConstraint string, sz agentSizing) (Agent, error) {
	persona, err := registry.ResolvePersona(name, ac.Persona, nil, cfg.PersonaDirs)
	if err != nil {
		return Agent{}, err
	}
	// Sprint-plan SCOPE CONSTRAINT (Epic 12.2): prepend the formatted constraint
	// to the payload so it lands in EVERY persona — every reviewer renders
	// {{.Payload}} (it carries the diff), so prepending guarantees delivery
	// regardless of the persona template, and places the constraint immediately
	// before the diff (the NFR). Empty when no --sprint-plan was given, leaving the
	// payload unchanged for a diff-wide review. Because the constraint becomes part
	// of the rendered prompt, the diff-cache key (which hashes the full prompt)
	// invalidates correctly when the plan changes (AC5).
	prompt, err := payload.RenderPrompt(persona.Text, payload.PayloadContext{
		AgentName:   name,
		BaseRef:     rng.Base,
		HeadRef:     rng.Head,
		PayloadMode: mode,
		FileCount:   fileCount,
		Payload:     scopeConstraint + payloadText,
		// Per-file escalation (Epic 35.1) can promote individual files above the
		// agent's configured mode, so the scope rule is derived from what the
		// payload actually contains rather than from the mode alone: a payload
		// holding any full-file body gets the wider files-mode rule.
		ScopeRule:    payload.ScopeRuleForPayload(payload.PayloadMode(mode), payloadText),
		ToolsEnabled: ac.Tools,
	})
	if err != nil {
		return Agent{}, fmt.Errorf("agent %q: %w", name, err)
	}
	// Soft per-agent scope focus (Epic 2.2): appended after the persona template
	// renders so it lands in every persona regardless of its template, and feeds
	// both Agent.Prompt and Invocation.Prompt below (a fallback reuses the
	// primary's prompt, so it inherits the focus too). No-op when scope is unset.
	prompt += payload.ScopeFocus(ac.Scope)
	prov, ok := cfg.Registry.Providers[ac.Provider]
	if !ok {
		return Agent{}, fmt.Errorf("agent %q references unknown provider %q", name, ac.Provider)
	}
	// Reserved output cap (Epic 19.10 F8) is recorded only for an agent whose
	// effective budget actually funds it. A bare/unsized caller (agentSizing{})
	// leaves it 0 so its status.json stays byte-identical, and so does a sized
	// agent whose declared window is consumed entirely by the output cap and the
	// prompt overhead — recording 8192 reserved tokens out of a 12288-token window
	// that yielded a zero input budget is a record that contradicts itself.
	// resolvedWindow is the "was this agent sized" signal (status.go); the
	// reservation follows the budget, the quantity that pays for it.
	reservedOut := 0
	if sz.effectiveBudget > 0 {
		reservedOut = defaultMaxTokens
	}
	return Agent{
		Name:     name,
		Provider: ac.Provider,
		Prompt:   prompt,
		// Recover the per-file breakdown of the payload THIS agent was sent
		// (Epic 35.0). It is derived from payloadText — the pre-template payload
		// — rather than from the rendered prompt, so it is unaffected by how a
		// persona template wraps the diff. Deriving it here rather than
		// threading structure through the chunker is exact because the chunker
		// splits on file boundaries, and free because the recovered bodies are
		// substrings of payloadText.
		CodeContext:      codeContextFor(mode, payloadText),
		PayloadMode:      mode,
		Truncation:       trunc,
		TimeoutSecs:      ac.EffectiveTimeoutSecs(cfg.Settings),
		MaxRetries:       ac.EffectiveMaxRetries(cfg.Settings),
		InitialBackoffMs: ac.EffectiveInitialBackoffMs(cfg.Settings),
		Tools:            ac.Tools,
		SupportsFC:       ac.SupportsFC,
		MaxTurns:         derefMaxTurns(ac.MaxTurns),
		ToolBudgetBytes:  derefInt64(ac.ToolBudgetBytes),
		MinSeverity:      ac.MinSeverity,
		MaxFindings:      ac.MaxFindings,
		// Per-agent sizing record (Epic 19.10 F6/F8): threaded from buildSlots so
		// invokeAgent can scale the deadline by ChunkTotal and stamp the
		// diagnosability fields onto the Result. chunkMaxLines is kept for
		// buildFallbackAgent to reuse this slot's chunk regime.
		ChunkTotal:           sz.chunkTotal,
		EffectiveBudget:      sz.effectiveBudget,
		ResolvedWindow:       sz.resolvedWindow,
		ReservedOutputTokens: reservedOut,
		DegradationAction:    sz.action,
		chunkMaxLines:        sz.maxLines,
		// Diff-cache key (Epic 5.2): derived from the full rendered prompt + model
		// + temperature + the per-agent sizing token (Epic 19.10 F7, see
		// diffCacheKey). Tool agents carry a key too but the engine never caches them
		// (they read live code), so setting it unconditionally is safe. A chunked run
		// keys each chunk independently because its prompt (and thus this hash)
		// differs per chunk; the sizing token additionally distinguishes two sizing
		// regimes that render identical prompt text.
		CacheKey: diffCacheKey(prompt, ac.Model, prov.BaseURL, ac.Temperature, sizingToken(sz.effectiveBudget, sz.maxLines)),
		Invocation: llmclient.Invocation{
			BaseURL:     prov.BaseURL,
			APIKeyEnv:   prov.APIKeyEnv,
			Model:       ac.Model,
			Temperature: ac.Temperature,
			MaxTokens:   maxTokensPtr(),
			Prompt:      prompt,
		},
	}, nil
}

// derefMaxTurns resolves the agent's MaxTurns pointer to a value. Registry load
// applies the default (10) when tools=true and it was unset, so a tool agent
// arrives here with a non-nil pointer; a nil pointer (non-tool agent, or direct
// construction) yields 0, which the engine treats as "use the default".
func derefMaxTurns(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// derefInt64 resolves an optional int64 (e.g. ToolBudgetBytes) to its value, with
// nil meaning 0 (unlimited, matching the registry's documented escape hatch).
func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// smallestEntry returns the entry with the fewest body bytes, and the first such
// entry on a tie so the choice is deterministic across runs (the diff-cache key
// and the baseline coverage tag both depend on which file was shipped).
//
// Body length is measured directly rather than trusting FileEntry.Size: Size is
// the pre-render source size, and it is Body that is actually dispatched.
// Callers guarantee a non-empty slice.
func smallestEntry(entries []payload.FileEntry) payload.FileEntry {
	best := entries[0]
	for _, e := range entries[1:] {
		if len(e.Body) < len(best.Body) {
			best = e
		}
	}
	return best
}

// droppedPathsExcept lists the paths of every entry except keep — the shed
// record for an arm that keeps exactly one file. Returns nil (not an empty
// slice) when nothing was dropped, matching the "no truncation" shape callers
// and the status.json omitempty tags expect.
//
// Sorted by path, matching payload.ApplyByteBudget's own contract for
// Truncation.FilesDropped ("the dropped list is returned sorted by path so the
// same input always produces the same Truncation"). Entry order would already be
// deterministic, but a status.json consumer reading FilesDropped should not have
// to know which arm produced it.
func droppedPathsExcept(entries []payload.FileEntry, keep string) []string {
	var dropped []string
	for _, e := range entries {
		if e.Path != keep {
			dropped = append(dropped, e.Path)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// appliedByteBudget narrows a per-agent input byte budget to the one a slot is
// actually sized to: capped by the global payload_byte_budget when configured,
// then reduced by the SCOPE CONSTRAINT block that renderAgent prepends UNCOUNTED
// to every payload (Epic 19.10 HIGH TD), floored at 0.
//
// One definition, three call sites (bulk, chunked record, baseline partition).
// They were three independently-written copies of the same arithmetic, which had
// already drifted: the bulk path applied both narrowings while the chunked and
// baseline sizing records reported the raw budget, so status.json's
// effective_budget meant a different quantity depending on payload strategy.
// A non-positive agent budget stays 0 — the honest "no budget available" state
// callers already branch on.
func appliedByteBudget(agentBudget, globalBudget int64, scopeConstraint string) int64 {
	if agentBudget <= 0 {
		return 0
	}
	applied := agentBudget
	if globalBudget > 0 && globalBudget < applied {
		applied = globalBudget
	}
	if s := int64(len(scopeConstraint)); s > 0 {
		applied -= s
		if applied < 0 {
			applied = 0
		}
	}
	return applied
}

// inheritedPayloadFits reports whether the payload the primary actually shipped
// is KNOWN to fit within budget bytes. It sums primary.CodeContext — the
// per-file bodies recovered from the payload this slot was rendered over — so it
// counts exactly the bytes an effective byte budget governs, unlike the rendered
// prompt (which also carries the uncounted persona wrapper).
//
// An unmeasurable payload reads as "may not fit", never as "fits": a payload
// shape EntriesFromRenderedPayload does not recognize yields no CodeContext, and
// treating a missing measurement as proof of safety would silently drop the AC4
// warning for exactly the payloads nothing else can vouch for. A zero budget is
// covered by the same arithmetic — no non-empty payload fits in 0 bytes.
func inheritedPayloadFits(primary Agent, budget int64) bool {
	if len(primary.CodeContext) == 0 {
		return false
	}
	var total int64
	for _, ref := range primary.CodeContext {
		total += int64(len(ref.Body))
	}
	return total <= budget
}

// entriesFromPrimary reconstructs the shipped payload as a FileEntry list from
// the primary's CodeContext — the per-file breakdown of what that slot actually
// sent, which is the same set inheritedPayloadFits measures.
//
// It exists so the overflow-policy call in buildFallbackAgent is handed the real
// payload rather than nil. The arms reachable there (fail, fallback) ignore the
// entries entirely, so this is defensive. The truncate arm is wired since Epic
// 35.16.5.4, but it re-packs the slot's CARRIED entries (fallbackRefit), never
// this reconstruction — this function's only consumer remains the fail/fallback
// policy call.
func entriesFromPrimary(primary Agent) []payload.FileEntry {
	entries := make([]payload.FileEntry, 0, len(primary.CodeContext))
	for _, ref := range primary.CodeContext {
		entries = append(entries, payload.FileEntry{
			Path: ref.Path,
			Size: int64(len(ref.Body)),
			Body: ref.Body,
		})
	}
	return entries
}

// fallbackRefit carries everything buildFallbackAgent needs to RE-RENDER a slot's
// payload at the fallback's own byte budget (Epic 35.16.5.4 T2): the slot's
// shipped FileEntry list plus the three render inputs renderAgent requires and
// buildChain did not previously capture (mode, rng, scopeConstraint). The primary's
// name and AgentConfig travel with it because a fallback reviews the SAME PERSONA
// as the primary it substitutes for (AC 01-04) — only the model, temperature and
// timeout are the fallback's own — so the re-render must resolve the PRIMARY's
// persona, not the backup's.
//
// It is passed as a variadic option rather than a required parameter so every
// caller that has no re-pack source keeps working, and keeps its pre-epic
// behavior, without a signature change: a bare buildFallbackAgent call cannot
// construct slot-level entries, and "no refit context → warn and ship" is the
// correct answer for such a caller, not a degraded one. Same idiom buildSlots
// itself uses for baselineOpt.
type fallbackRefit struct {
	primaryName     string
	primaryConfig   registry.AgentConfig
	mode            string
	rng             ReviewRange
	scopeConstraint string
	// entries is the payload the primary shipped. EMPTY means the slot's payload
	// was never entry-decomposed (the chunked-diff path), which disables the
	// re-fit — see Slot.entries.
	entries []payload.FileEntry
}

// canRefit reports whether a refit context is usable as a re-pack source. An
// absent context and an entry-less one are the same answer — there is nothing to
// re-pack — and both must leave the pre-epic warn-and-ship path untouched rather
// than re-packing against an empty list, which would shed EVERY file and ship an
// empty (false-clean) review.
func (r fallbackRefit) canRefit() bool { return len(r.entries) > 0 }

// buildFallbackAgent builds a fallback that reviews the SAME persona prompt and
// payload as the primary (AC 01-04: "fallback agent tried (same persona)"), only
// the provider/model/temperature/timeout differ.
//
// warnOversized gates the AC4 overflow warning below, matching every sibling
// warning in buildSlots: resume.go passes false so a resume rebuild does not
// re-emit warnings the operator already saw at prepare time. It gates only the
// stderr line — the degradation RECORD is a property of the slot and is stamped
// either way, so a resumed run's status.json stays truthful.
//
// The second return reports whether the warning was actually emitted, so a
// caller deduplicating it across a lane's chunks suppresses only what the
// operator has really seen.
func buildFallbackAgent(cfg *ReviewConfig, primary Agent, name string, warnOversized bool, refit fallbackRefit) (Agent, bool, error) {
	// refit is a REQUIRED parameter (a zero fallbackRefit declines the re-fit by
	// construction via canRefit): every construction site must say explicitly
	// whether it has a re-pack source, so a future production caller cannot
	// silently get pre-epic warn-and-ship by forgetting an argument.

	ac, ok := cfg.Registry.Agents[name]
	if !ok {
		return Agent{}, false, fmt.Errorf("fallback agent %q not found in registry", name)
	}
	prov, ok := cfg.Registry.Providers[ac.Provider]
	if !ok {
		return Agent{}, false, fmt.Errorf("fallback agent %q references unknown provider %q", name, ac.Provider)
	}
	// A fallback answers in the primary's place, so the primary's review
	// constraints (min_severity, max_findings, scope) govern — the fallback's
	// own are intentionally ignored (Epic 2.2). Surface that override so an
	// operator who set these on a fallback-only agent is not silently ignored.
	if ac.MinSeverity != "" || ac.MaxFindings != nil || len(ac.Scope) > 0 {
		fmt.Fprintf(os.Stderr, "warn: fallback agent %q sets its own min_severity/max_findings/scope; these are ignored — the primary lane's constraints govern\n", name)
	}
	// Epic 19.10 F7/F8: a fallback reviews the SAME (already-sized/chunked) prompt
	// as its primary but on its OWN model, so it re-derives its OWN effective budget
	// and window for the cache sizing token and diagnosability record — its cache
	// entry must not collide with the primary's and must reflect its own model. It
	// reuses the primary's chunk regime (chunkMaxLines / ChunkTotal / degradation
	// action) because the diff was already split for the slot; only the byte budget
	// is model-specific. The truncate re-fit below is the exception: it overrides
	// all three with its own bulk-sized record (ChunkTotal 1, chunkMaxLines 0, and
	// the re-fit's own truncate/overflow action).
	fbBudget := payload.EffectiveByteBudget(ac.Model, ac.ContextWindowTokens, defaultMaxTokens)
	fbWindow := payload.ContextWindowTokens(ac.Model, ac.ContextWindowTokens)
	// Gate the reservation on the BUDGET, not the window. ContextWindowTokens never
	// returns 0 by contract (contextwindow.go), so a window test is a dead branch —
	// and with a declaration as low as 1 token now legal it produced a
	// self-contradictory record: resolved_window 1, reserved_output_tokens 8192,
	// and no effective_budget field at all (omitempty on the zero budget). The
	// budget is the quantity that actually funds the output cap, so an agent whose
	// window cannot fund it now honestly reports reserving nothing.
	fbReserved := 0
	if fbBudget > 0 {
		fbReserved = defaultMaxTokens
	}
	// Epic 35.16.5.1 AC4: resolving the fallback's OWN window above is only half the
	// guarantee. The prompt it inherits was sized to the PRIMARY's window, so a
	// larger-windowed primary hands its smaller backup a payload that backup cannot
	// hold. The mechanism predates this epic — the static table already spreads
	// 200000 against 32768 — but a per-agent declaration makes it reachable on a
	// roster that previously resolved ONE uniform default for every agent, and the
	// epic's own operator step pairs declared primaries with undeclared backups.
	//
	// Re-shedding the payload here is deliberately out of scope: buildFallbackAgent
	// receives the already-rendered prompt, not the FileEntry list it would have to
	// re-pack, and a fallback reviewing a different file set than its primary is a
	// separate design question — chunkFiles and ChunkTotal are SLOT-level and
	// inherited below (the truncate re-fit is the one exception, carrying the
	// smaller set it actually kept — the narrowing the next paragraph describes),
	// so a re-packed fallback would falsify the
	// baseline coverage attribution and require the Slot/Agent model to grow a
	// fallback-owned chunk chain. What AC4 forbids is doing this SILENTLY, so
	// surface it — warn pre-dispatch and record the honest overflow degradation
	// instead of copying the primary's action.
	//
	// EPIC 35.16.5.4 NARROWS THAT, for `truncate` only. The slot now carries the
	// FileEntry list its primary shipped (Slot.entries, threaded here as
	// fallbackRefit), so the truncate arm CAN re-pack — and the coverage-falsifying
	// objection above is answered by T3, which makes baseline attribution read the
	// files the SERVING agent reviewed rather than the primary's tag. The rest of
	// the paragraph still stands verbatim for `chunk` (which needs a
	// fallback-owned chunk chain, still out of scope) and for any slot with no
	// entries to re-pack.
	//
	// The operator's on_overflow policy decides first, mirroring the primary path's
	// own pre-dispatch gate: fail and fallback return their typed errors, which
	// propagate out of buildChain/buildSlots and hard-fail the run BEFORE a prompt
	// the backup provably cannot hold is dispatched. truncate re-fits when it has a
	// source; chunk — and truncate without one — keep the warn-and-ship behavior
	// below, because both would need exactly the FileEntry re-packing ruled out
	// above and routing them here would promise a re-fit this function cannot
	// perform.
	//
	// Measure the PAYLOAD before claiming it overflows. len(primary.Prompt) is the
	// wrong measure — the rendered prompt carries the persona wrapper that the byte
	// budget does not count — but a budget-only comparison is worse: it is true for
	// ANY fallback resolving a smaller window, whether the payload is 5 KB or
	// 500 KB, so the epic's own recommended roster (declared primaries paired with
	// undeclared 32768 backups) would warn on every pair of an ordinary small-diff
	// review and stamp overflow on fallbacks that held the payload with room to
	// spare. primary.CodeContext is the per-file breakdown of what this slot
	// actually shipped, which is exactly what the byte budget governs.
	//
	// The stamp OVERWRITES the inherited action rather than composing with it:
	// "overflow" takes precedence because it is the only action that says the
	// review may not have been read in full, so a fallback serving a chunked slot
	// records "overflow", not "chunk". Nothing is lost — ChunkTotal still carries
	// the chunk plan and Truncation still lists the dropped files. That precedence
	// is part of the status.json contract; see the DegradationAction doc in
	// status.go, which enumerates all four values.
	fbDegradation := primary.DegradationAction
	warned := false
	// The payload-describing record. Every field defaults to the primary's — a
	// fallback that does not re-fit reviews exactly the primary's payload, so its
	// record must stay byte-identical to the pre-epic one (AC4). Only the truncate
	// re-fit arm below overwrites them, and it overwrites ALL of them together:
	// prompt, per-file breakdown, shed record, coverage tag and sizing describe one
	// payload, so a partial overwrite would leave the record describing two.
	fbPrompt := primary.Prompt
	fbTrunc := primary.Truncation
	fbCodeContext := primary.CodeContext
	// The coverage tag follows the payload, so it is copied from the primary here
	// (a non-re-fit fallback DID review the primary's chunk) and replaced by the
	// kept subset on the re-fit arm. Epic 35.16.5.4 T3 reads this off whichever
	// chain member actually served the slot. The copy is belt-and-braces:
	// servedCoverage's (nil, false) fall-back arm would return the primary's tag
	// for an untagged non-re-packed result anyway — copying here keeps the tag on
	// the AGENT that shipped it, so the stamped served tag and the slot's primary
	// tag are the same fact recorded at the source rather than reconstructed two
	// files away.
	fbChunkFiles := primary.chunkFiles
	fbChunkTotal := primary.ChunkTotal
	fbMaxLines := primary.chunkMaxLines
	// The budget the payload was actually sized to, recorded as effective_budget
	// and folded into the cache sizing token. It is the raw per-model budget until
	// a re-fit sizes a payload to the globally-capped, scope-reserved one.
	fbSizingBudget := fbBudget
	// True once the truncate arm has actually re-packed. It gates the overflow
	// warning and the overflow stamp: a re-fit payload FITS, so claiming it may
	// overflow would be the same false record this epic exists to remove.
	refitted := false
	if primary.EffectiveBudget > 0 && fbBudget < primary.EffectiveBudget && !inheritedPayloadFits(primary, fbBudget) {
		// Gated on the SAME condition as the warning — an actual measured overflow,
		// not a bare budget comparison. Enabling on_overflow=fail must not break
		// every ordinary small-diff run that merely has a smaller-windowed backup.
		//
		// The entries passed are the primary's shipped set and the budget is the
		// FALLBACK's own, so the truncate arm (unreachable here) would still be
		// self-consistent if it were ever wired in. ErrFallbackUnavailable is the
		// right error for the fallback arm despite this already being a fallback:
		// the policy means "hand this lane to a backup", and the backup in hand is
		// the one that cannot hold the payload.
		if p := cfg.Settings.OnOverflow; p == OverflowFail || p == OverflowFallback {
			if _, err := applyOverflowPolicy(p, "", 0, entriesFromPrimary(primary), fbBudget); err != nil {
				return Agent{}, false, err
			}
		}
		// The truncate re-fit (Epic 35.16.5.4 T2). Reached only with a real re-pack
		// source: an absent or entry-less refit context falls straight through to the
		// warn-and-ship below, because re-packing against an empty list would shed
		// EVERY file and ship the empty, false-clean payload AC5 forbids.
		//
		// It returns ok=false when the re-pack sheds nothing — the payload measurably
		// fits this budget after all, or it is a single file that cannot be made any
		// smaller. Both keep today's behavior exactly, which is what makes the
		// Conservatism NFR hold by construction: the arm can only be taken when it
		// strictly reduces the bytes sent.
		if cfg.Settings.OnOverflow == OverflowTruncate && refit.canRefit() {
			rp, ok, err := refitFallbackPayload(cfg, refit, fbBudget, fbWindow)
			if err != nil {
				return Agent{}, false, err
			}
			if ok {
				fbPrompt = rp.agent.Prompt
				fbCodeContext = rp.agent.CodeContext
				fbTrunc = rp.trunc
				// Compose with the primary's shed record rather than replacing it:
				// every file the primary dropped before this slot was built is absent
				// from THIS agent's payload too, so the serving agent's files_dropped
				// must name it (AC6) — otherwise the agent that reviewed the LEAST
				// reports the SHORTEST omission list. Dedup + sort by path, matching
				// payload.ApplyByteBudget's Truncation contract.
				if len(primary.Truncation.FilesDropped) > 0 {
					seen := make(map[string]struct{}, len(fbTrunc.FilesDropped)+len(primary.Truncation.FilesDropped))
					for _, p := range fbTrunc.FilesDropped {
						seen[p] = struct{}{}
					}
					for _, p := range primary.Truncation.FilesDropped {
						if _, ok := seen[p]; !ok {
							fbTrunc.FilesDropped = append(fbTrunc.FilesDropped, p)
							seen[p] = struct{}{}
						}
					}
					sort.Strings(fbTrunc.FilesDropped)
				}
				fbChunkFiles = entryPaths(rp.kept)
				// One re-fit payload, not the slot's split: ChunkTotal must be 1 or the
				// per-call deadline (timeout.go) and status.json's chunk_count both
				// describe a partition this agent does not follow, and chunkMaxLines
				// must be the bulk sentinel 0 for the same reason (T4).
				fbChunkTotal, fbMaxLines = 1, 0
				fbSizingBudget = rp.budget
				// truncate when the re-packed payload fits; overflow when even the
				// smallest framing still exceeds this budget. The second case must not
				// be recorded as a successful truncate — it is the "no smaller framing
				// available" state, and it keeps the overflow warning below.
				fbDegradation = degradationTruncate
				if !rp.fits {
					fbDegradation = degradationOverflow
				}
				// The reservation follows the budget that was actually recorded, not
				// the raw per-model one. A re-fit records the APPLIED budget, so
				// gating on fbBudget here would re-create the self-contradictory
				// record 35.16.5.1 removed: effective_budget absent (0, omitempty)
				// beside reserved_output_tokens 8192.
				fbReserved = 0
				if fbSizingBudget > 0 {
					fbReserved = defaultMaxTokens
				}
				// It also marks the agent as re-packed, which is what stops baseline
				// coverage from inferring "every slot succeeded → the whole payload was
				// covered" over a slot that reviewed a strict subset of its tag (T3a).
				// This holds whether or not the result fits: coverage must follow the
				// SMALLER file set either way.
				refitted = true
				// A SUCCESSFUL re-fit is recorded, not warned — exactly how the primary
				// path treats its own byte shed (bulkDegradation = degradationTruncate
				// with no stderr line). The shed is in this agent's own Truncation and
				// degradation_action, and mergeResultGroup carries it through the
				// chunk collapse, so nothing is silent; adding a per-chunk stderr line
				// here would instead have to share the lane's overflow dedup flag,
				// where either message could swallow the other.
				//
				// A re-fit that still does not fit is a different matter: the lane's
				// real problem — a backup too small to hold this review at all — is
				// unsolved and needs operator action, so it keeps the overflow warning
				// below. Leaving `warned` false here is what lets that fire.
				if warnOversized && !rp.fits {
					warned = true
					fmt.Fprintf(os.Stderr, "atcr: warning: fallback agent %q re-packed the payload inherited from primary %q down to %d file(s) (%d shed), but the smallest available framing still exceeds its %d B budget; it may overflow — declare context_window_tokens on %q, or point the lane at a backup with a comparable window\n",
						name, primary.Name, len(rp.kept), len(rp.trunc.FilesDropped), rp.budget, name)
				}
			}
		}
		if !refitted {
			if warnOversized {
				warned = true
				fmt.Fprintf(os.Stderr, "atcr: warning: fallback agent %q resolves a %d-token window (effective budget %d B) but inherits a prompt sized for primary %q's %d-token window (%d B); it may overflow — declare context_window_tokens on %q, or point the lane at a backup with a comparable window\n",
					name, fbWindow, fbBudget, primary.Name, primary.ResolvedWindow, primary.EffectiveBudget, name)
			}
			fbDegradation = degradationOverflow
		}
	}
	return Agent{
		Name: name,
		// A fallback keys on its OWN provider: if it uses a different provider than
		// the primary, it gets that provider's breaker (so a fallback can succeed
		// while the primary's circuit is open).
		Provider:    ac.Provider,
		Prompt:      fbPrompt,
		PayloadMode: primary.PayloadMode,
		Truncation:  fbTrunc,
		// A fallback reviews the primary's already-sized/chunked payload, so it
		// saw exactly the same files and inherits the primary's record of them —
		// unless it re-fit that payload above, in which case every field here
		// describes the smaller payload IT ships.
		CodeContext: fbCodeContext,
		TimeoutSecs: ac.EffectiveTimeoutSecs(cfg.Settings),
		// Retry/backoff follow the fallback's OWN config (Epic 4.6), like
		// TimeoutSecs: the fallback makes its own call to its own provider, so its
		// own resilience budget governs.
		MaxRetries:       ac.EffectiveMaxRetries(cfg.Settings),
		InitialBackoffMs: ac.EffectiveInitialBackoffMs(cfg.Settings),
		// Fallbacks inherit the lane's effective tool settings from the primary,
		// not the fallback's own config (AC 01-05 S4, AC 04-03: "fallbacks inherit
		// the lane's effective tools setting"). Degrade stays per-agent — a
		// fallback whose model cannot do function calling degrades independently
		// (Phase 4), but the requested Tools/MaxTurns/ToolBudgetBytes are the lane's.
		Tools:           primary.Tools,
		MaxTurns:        primary.MaxTurns,
		ToolBudgetBytes: primary.ToolBudgetBytes,
		// SupportsFC is per-agent: the fallback uses its OWN model's capability,
		// NOT the primary's, so the degrade decision is re-evaluated per agent
		// (AC 04-03 EC3 — lane governs Tools, the model governs capability).
		SupportsFC: ac.SupportsFC,
		// Review constraints follow the slot, not the substitute model (Epic 2.2):
		// a fallback answers in the primary's place, so the primary's min_severity
		// and max_findings still govern the output.
		MinSeverity: primary.MinSeverity,
		MaxFindings: primary.MaxFindings,
		// Sizing record (Epic 19.10 F6/F8): chunk regime follows the slot (same
		// split as the primary), byte budget/window are the fallback's OWN model's.
		// A re-fit fallback (Epic 35.16.5.4 T4) instead records ONE payload of its
		// own — ChunkTotal 1, the bulk maxLines sentinel, and the budget its payload
		// was really sized to — because the slot's split is not the one it follows.
		ChunkTotal:           fbChunkTotal,
		EffectiveBudget:      fbSizingBudget,
		ResolvedWindow:       fbWindow,
		ReservedOutputTokens: fbReserved,
		DegradationAction:    fbDegradation,
		chunkMaxLines:        fbMaxLines,
		rePacked:             refitted,
		// The coverage tag of the payload this agent actually reviews (Epic
		// 35.16.5.4 T3): the primary's chunk when it ships the inherited payload,
		// the kept subset when it re-fit.
		chunkFiles: fbChunkFiles,
		// Diff-cache key (Epic 5.2): a fallback reviews the SAME rendered prompt as
		// the primary but on its OWN model and temperature, so it keys on the
		// primary's prompt with the fallback's model/temperature — a substitute
		// model must not collide with the primary's cache entry. Its sizing token
		// (Epic 19.10 F7) uses the fallback's OWN effective budget under the slot's
		// chunk regime, so it also never collides across sizing regimes.
		//
		// The sizing token DOES collapse to the unsized "0:0" when the slot is bulk
		// (chunkMaxLines 0) and this fallback's window funds no input budget
		// (fbBudget 0) — the same token an unsized agent renders. That is not a
		// collision risk: the key also carries the model, the backend and the
		// temperature, so two entries can only meet when a substitute resolves the
		// SAME model on the SAME provider at the SAME temperature over the same
		// prompt — inputs that produce the same review, which is what a shared cache
		// entry is for. Cache invalidation on a prompt-size change is likewise not
		// implicit: the prompt itself is hashed, so a re-sized payload is a different
		// key by construction.
		// A re-fit fallback keys on ITS OWN prompt and sizing token, which is what
		// keeps it off both its primary's cache entry and its own un-refit form's:
		// the prompt is hashed, so a re-sized payload is a different key by
		// construction, and the sizing token additionally separates the two budgets.
		CacheKey: diffCacheKey(fbPrompt, ac.Model, prov.BaseURL, ac.Temperature, sizingToken(fbSizingBudget, fbMaxLines)),
		Invocation: llmclient.Invocation{
			BaseURL:     prov.BaseURL,
			APIKeyEnv:   prov.APIKeyEnv,
			Model:       ac.Model,
			Temperature: ac.Temperature,
			MaxTokens:   maxTokensPtr(),
			Prompt:      fbPrompt,
		},
	}, warned, nil
}

// keepSmallestEntry reduces a payload to its single smallest NON-EMPTY entry with
// the matching shed record — the "no file fits, but an empty payload is not an
// option" answer. Truncated is false for a one-entry input because nothing was
// actually dropped, which is what tells the re-fit caller there is no smaller
// payload to send and the honest overflow record must stand.
//
// Zero-byte entries are skipped rather than preferred, and that is the whole
// point of not reusing smallestEntry here. Empty tracked files are ordinary
// (py.typed, __init__.py, .gitkeep), and "fewest bytes" would pick one every
// time — shipping a payload of exactly 0 bytes, which comes back as a
// false-clean "no findings" review. That is the precise failure AC5 forbids, so
// the entry that survives must carry real content.
//
// The second return is false when every entry is empty: there is no non-empty
// payload to send, so the caller must decline the re-fit rather than invent one.
// Callers guarantee a non-empty slice.
func keepSmallestEntry(entries []payload.FileEntry) ([]payload.FileEntry, payload.Truncation, bool) {
	var smallest payload.FileEntry
	found := false
	for _, e := range entries {
		if len(e.Body) == 0 {
			continue
		}
		if !found || len(e.Body) < len(smallest.Body) {
			smallest, found = e, true
		}
	}
	if !found {
		return nil, payload.Truncation{}, false
	}
	return []payload.FileEntry{smallest}, payload.Truncation{
		Truncated:    len(entries) > 1,
		FilesDropped: droppedPathsExcept(entries, smallest.Path),
	}, true
}

// refitPayload is the outcome of re-packing a slot's payload at a fallback's own
// byte budget: the re-rendered agent plus the records describing what it now
// ships. Grouped in a struct rather than returned as five values because they are
// one fact — the payload this fallback sends — and splitting them invites a caller
// to apply some and not others, leaving the record describing two payloads.
type refitPayload struct {
	agent  Agent
	trunc  payload.Truncation
	kept   []payload.FileEntry
	budget int64
	// fits reports whether the dispatched bytes are within budget — and thereby
	// the degradation this re-fit achieved: truncate when it fits, overflow when
	// even the smallest framing does not (derived at the use site, so one bit is
	// encoded by one field). False keeps the pre-epic overflow warning, because
	// the operator's problem — a backup too small for this lane — is real and
	// unsolved even though the payload shrank.
	fits bool
}

// refitFallbackPayload re-packs refit.entries against the fallback's own budget
// and re-renders the result. The second return is false when NOTHING was shed —
// the caller must then leave the inherited payload and the pre-epic warn-and-ship
// exactly as they were.
//
// Two properties this function is responsible for:
//
//   - It re-renders under the PRIMARY's name and AgentConfig, never the backup's.
//     A fallback answers in the primary's place and reviews the same persona
//     (AC 01-04); only the model, temperature and timeout are its own, and those
//     are applied by the caller. Rendering the backup's persona here would swap
//     the reviewer's identity as a side effect of re-sizing a payload.
//   - It never yields an EMPTY payload. When not even one file fits (AllDropped)
//     it keeps the single smallest entry, matching the zero-budget bulk arm — an
//     empty payload comes back as a false-clean "no findings" review, the same
//     silent-zero class ErrPayloadFullyDropped guards against. The same reroute
//     fires when entries survived but their BODIES total zero bytes — zero-size
//     entries (py.typed, __init__.py) are never dropped by the largest-first
//     shed, so AllDropped alone does not see them.
//
// The budget is the APPLIED one — the fallback's per-model budget narrowed by the
// global payload_byte_budget and by the uncounted SCOPE CONSTRAINT block — not the
// raw per-model figure the non-re-fit path records. That is the quantity the
// payload is genuinely sized to, so it is what effective_budget must report for an
// agent that re-fit (AC6), and it is the more conservative of the two.
func refitFallbackPayload(cfg *ReviewConfig, refit fallbackRefit, fbBudget int64, fbWindow int) (refitPayload, bool, error) {
	// Re-cap the SCOPE CONSTRAINT against the FALLBACK's window before reserving
	// it. It arrives capped to the PRIMARY's budget/8 (see add()), and renderAgent
	// prepends it UNCOUNTED, so a plan sized for a 128k primary would ride whole
	// into a 32k backup and the payload would not really be within this agent's
	// budget. Same arithmetic as the primary path, on this agent's own numbers.
	scopeConstraint := refit.scopeConstraint
	if len(scopeConstraint) > 0 && fbBudget > 0 {
		planCap := fbBudget / 8
		if mspb := cfg.Settings.MaxSprintPlanBytes; mspb > 0 && mspb < planCap {
			planCap = mspb
		}
		if planCap < 1 {
			// A budget that funds not even one byte of plan cannot present a
			// scoped review: capping to 0 would blank the plan body while the
			// wrapper still instructs the model to obey it. Drop the block
			// entirely instead — the review proceeds unconstrained and the
			// prompt says so.
			scopeConstraint = ""
		} else {
			scopeConstraint = capScopeConstraintPlan(scopeConstraint, int(planCap))
		}
	}
	budget := appliedByteBudget(fbBudget, cfg.Settings.PayloadByteBudget, scopeConstraint)
	var kept []payload.FileEntry
	var trunc payload.Truncation
	switch {
	case budget <= 0:
		// A zero budget must NEVER reach ApplyByteBudget, which reads a
		// non-positive budget as UNLIMITED (budget.go: "0 = unlimited") and would
		// hand back the whole payload — the exact inversion the zero-budget bulk
		// arm was written to prevent, since a window that funds no input budget at
		// all is the strongest possible overflow signal, not the absence of one.
		// Keep the smallest entry, the same answer the bulk arm gives.
		var ok bool
		kept, trunc, ok = keepSmallestEntry(refit.entries)
		if !ok {
			return refitPayload{}, false, nil
		}
	default:
		// PreferEscalated, not the plain pass — the same reason the bulk path
		// gives: escalating a file to a higher-context mode makes it the largest
		// entry, so plain largest-first sheds exactly the file the heuristic
		// flagged as hardest to review, on precisely the tight-window agents
		// escalation targets.
		kept, trunc = payload.ApplyByteBudgetPreferEscalated(refit.entries, budget, payload.PayloadMode(refit.mode))
		// AllDropped alone tests the wrong predicate — "no entries survived"
		// rather than "no CONTENT survived". The largest-first shed sizes on
		// FileEntry.Size and zero-size entries sort LAST, so they are never
		// dropped: a payload of one oversized file plus a 0-byte py.typed
		// re-packs to kept=[py.typed] with AllDropped=false, and the
		// concatenation below would dispatch a 0-byte payload — the false-clean
		// "no findings" review AC5 forbids, now tagged as reviewed. Measure the
		// kept BODIES and route a zero-byte result into keepSmallestEntry
		// exactly as the AllDropped arm does.
		keptBodyBytes := 0
		for _, e := range kept {
			keptBodyBytes += len(e.Body)
		}
		if trunc.AllDropped || keptBodyBytes == 0 {
			// Not even one file fits, or nothing with content survived. Shipping
			// "" comes back as a false-clean "no findings" review, so keep the
			// smallest non-empty entry instead (AC5) — or decline when every
			// entry is empty.
			var ok bool
			kept, trunc, ok = keepSmallestEntry(refit.entries)
			if !ok {
				return refitPayload{}, false, nil
			}
		}
	}
	// Nothing shed: either the entries measurably fit this budget after all (the
	// overflow was detected off an UNMEASURABLE CodeContext, which reads as "may
	// not fit" by design), or the slot is a single file that cannot be made any
	// smaller. Re-rendering would produce the same payload, so report no re-fit and
	// let the caller keep the honest overflow record.
	if !trunc.Truncated {
		return refitPayload{}, false, nil
	}
	var pb strings.Builder
	for _, e := range kept {
		pb.WriteString(e.Body)
	}
	// Measure what is actually DISPATCHED. The budget pass sums FileEntry.Size,
	// which is the pre-render source size; the bytes that reach the model are
	// len(Body) — the same distinction smallestEntry is documented to care about.
	// Both smallest-entry arms above also keep their entry whatever its size, so
	// without this the function could report a clean fit while shipping a payload
	// many times the budget.
	fits := int64(pb.Len()) <= budget
	// A re-fit that still does not fit has not truncated its way to safety — it
	// found the smallest framing available and that framing is still too big. That
	// is exactly what degradationOverflow means, and it is what the bulk
	// zero-budget arm records in the identical situation. Claiming "truncate" here
	// would drop the one signal saying the review may not have been read in full,
	// precisely when the agent is worst off.
	action := degradationTruncate
	if !fits {
		action = degradationOverflow
	}
	sz := agentSizing{
		effectiveBudget: budget,
		resolvedWindow:  fbWindow,
		// One re-fit payload: no chunk plan, so the bulk sentinels (T4).
		maxLines:   0,
		chunkTotal: 1,
		action:     action,
	}
	a, err := renderAgent(cfg, refit.primaryName, refit.primaryConfig, refit.mode, pb.String(), len(kept), trunc, refit.rng, scopeConstraint, sz)
	if err != nil {
		return refitPayload{}, false, err
	}
	return refitPayload{agent: a, trunc: trunc, kept: kept, budget: budget, fits: fits}, true, nil
}

// writePayloadArtifacts persists each distinct payload under payload/<mode>.txt
// so the manifest's provenance is backed by what reviewers actually saw.
func writePayloadArtifacts(dir string, payloads map[string]modePayload) error {
	for mode, mp := range payloads {
		path := filepath.Join(dir, "payload", mode+".txt")
		if err := atomicWriteFile(path, []byte(mp.Text)); err != nil {
			return fmt.Errorf("writing payload %s: %w", mode, err)
		}
	}
	return nil
}

// anyToolAgent reports whether any primary slot requested tools, so ExecuteReview
// only pays the snapshot/jail cost when the harness is needed. Fallbacks always
// inherit the lane's effective Tools setting from the primary (AC 01-05 S4), so
// checking fallbacks cannot change the result; the loop is intentionally omitted.
func anyToolAgent(slots []Slot) bool {
	for _, s := range slots {
		if s.Primary.Tools {
			return true
		}
	}
	return false
}

// transcriptAgentDir maps an agent name to the same single-segment directory the
// pool artifacts use (raw/agent/<dir>), so transcript.jsonl lands beside the
// agent's status.json/review.md. An unusable name falls back to a safe constant
// rather than escaping the pool.
func transcriptAgentDir(agent string) string {
	dir, err := agentDirName(agent)
	if err != nil {
		return "transcript-unknown"
	}
	return dir
}

// reviewStageFor classifies fan-out results into the manifest's review-stage
// entry (AC 05-04). An agent is tools-enabled when it requested tools at
// invocation time (ToolsRequested) — preserved across the degrade, budget-trip,
// and provider-error paths, so membership reflects the configured intent, not
// the completion outcome. The degraded subset is the agents that fell back to
// single-shot. Returns nil when no agent ran with tools, so the manifest omits
// the review entry for a pure 1.x roster (Scenario 5).
func reviewStageFor(results []Result) *payload.ReviewStage {
	return reviewStageForAgents(results,
		func(r Result) bool { return r.ToolsRequested },
		func(r Result) bool { return r.ToolsDegraded },
		func(r Result) string { return r.Agent },
		func(r Result) string { return r.ToolsDegradedReason })
}

// reviewStageForAgents is the single manifest review-stage classifier shared by
// the fresh ([]Result via reviewStageFor) and resume ([]AgentStatus via
// reviewStageFromStatuses) paths, so the classification rule lives in exactly
// one place and the two paths cannot silently diverge. An element contributes to
// ToolsEnabled when requested() is true, and additionally to ToolsDegraded when
// degraded() is true. Returns nil when no element ran with tools, so the
// manifest omits the review entry for a pure 1.x roster. Agents is a distinct
// copy of ToolsEnabled so the two slices never alias (a later mutation of one
// must not silently mutate the other).
func reviewStageForAgents[T any](items []T, requested func(T) bool, degraded func(T) bool, name func(T) string, reason func(T) string) *payload.ReviewStage {
	var enabled, deg []string
	var reasons map[string]string
	for _, it := range items {
		if !requested(it) {
			continue
		}
		enabled = append(enabled, name(it))
		if degraded(it) {
			deg = append(deg, name(it))
			// Built lazily so a run with no degradation (and every run predating
			// the field) marshals without the key at all.
			if rsn := reason(it); rsn != "" {
				if reasons == nil {
					reasons = map[string]string{}
				}
				reasons[name(it)] = rsn
			}
		}
	}
	if len(enabled) == 0 {
		return nil
	}
	return &payload.ReviewStage{
		Agents:              append([]string(nil), enabled...),
		ToolsEnabled:        enabled,
		ToolsDegraded:       deg,
		ToolsDegradedReason: reasons,
	}
}

// snapshotManifestFields derives the review-stage snapshot provenance (AC 03-02 /
// 03-03) from the root SnapshotFor returned. root and repo pointing at the same
// directory is the live fast path (head matched HEAD on a clean worktree), so mode
// is "live" and the worktree path is the explicit empty string; any other root is
// a detached worktree at head, so mode is "worktree" and the path is that root.
func snapshotManifestFields(root, repo, head string) (mode, headSHA, worktreePath string) {
	if samePath(root, repo) {
		return "live", head, ""
	}
	return "worktree", head, root
}

// samePath reports whether a and b refer to the same directory, normalizing
// trailing separators and relative vs absolute form so they do not spuriously
// force worktree mode.
func samePath(a, b string) bool {
	absA, err1 := filepath.Abs(a)
	absB, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return absA == absB
}

// resolveHeadSHA resolves a git ref to its full 40-byte SHA. It is a defensive
// guard for callers (including tests) that construct PreparedReview with an
// unresolved head; the production CLI/MCP path already resolves the head through
// gitrange.Resolve before fan-out.
func resolveHeadSHA(repo, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("empty ref")
	}
	cmd := gitexec.CommandFn("-C", repo, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// absRoot resolves a review root to a cleaned absolute path for Manifest.Root. It
// returns "" when the path cannot be resolved (a broken CWD, essentially), because
// recording no root degrades a later reconcile to its pre-field CWD behavior, while
// recording a bad claim would send findings to the wrong store — and the manifest
// root is trusted enough to be written to on the strength of a re-validation, so an
// unresolvable value must never enter it. An empty or blank input returns "" for
// the same reason: filepath.Abs("") does not error — it yields the CWD — so without
// the guard "no root is known" would be recorded as a confident CWD claim.
func absRoot(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	// Record the REAL path, not a pointer at it (TD internal/payload/manifest.go:70).
	// A recorded root is re-validated by a marker check alone, which cannot tell one
	// repository from another now living at the same path — so a root recorded
	// through a symlink re-validates cleanly after the link is repointed, and the
	// findings land in someone else's store. Resolving here is also the policy
	// pathWithin and NewJail already apply; a second path-identity rule is how the
	// two halves drift. On error (the usual case: a root that does not exist yet)
	// the cleaned absolute path stands, exactly as before — an unresolved path is a
	// far smaller regression than no recorded root at all.
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(abs)
}

// rosterNames returns the full roster (parallel lane then serial lane).
func rosterNames(p *registry.ProjectConfig) []string {
	names := make([]string, 0, len(p.Agents)+len(p.SerialAgents))
	names = append(names, p.Agents...)
	names = append(names, p.SerialAgents...)
	return names
}
