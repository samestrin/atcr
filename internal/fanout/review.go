package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/cache"
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
	payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
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
	return finalizePreparedReview(ctx, cfg, req, payloads, perAgentMode, slots, cfg.Settings.PayloadMode)
}

// finalizePreparedReview is the shared scaffold-and-assemble tail of the two
// review-preparation entry points (PrepareReview's git-range path and
// PrepareReviewFromDiff's ingestion path): it derives the review id, claims the
// review directory (honoring --output-dir/--id/--force), writes the payload
// artifacts, an in-progress manifest, and the .atcr/latest pointer, and wires the
// diff cache. payloadMode is recorded as the manifest's PayloadMode (the
// configured mode for the git path, "diff" for the ingestion path); the range
// provenance comes from req.Range, which the ingestion caller leaves empty.
func finalizePreparedReview(ctx context.Context, cfg *ReviewConfig, req ReviewRequest, payloads map[string]modePayload, perAgentMode map[string]string, slots []Slot, payloadMode string) (*PreparedReview, error) {
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
		Base:            req.Range.Base,
		Head:            req.Range.Head,
		DetectionMode:   req.Range.DetectionMode,
		DefaultBranch:   req.Range.DefaultBranch,
		CommitCount:     req.Range.CommitCount,
		PayloadMode:     payloadMode,
		MaxParallel:     cfg.Settings.MaxParallel,
		TimeoutSecs:     cfg.Settings.TimeoutSecs,
		PerAgentPayload: perAgentMode,
		Roster:          rosterNames(cfg.Project),
		StartedAt:       req.StartedAt,
		Partial:         false,              // finalized by ExecuteReview once outcomes are known
		Stages:          []string{"review"}, // 1.x runs only the review stage (Epic 1.1 reserved field)
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
	changed, groundingDisabledReason := computeGroundingData(ctx, req)
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
func computeGroundingData(ctx context.Context, req ReviewRequest) (payload.ChangedLines, string) {
	if req.Range.Base == "" || req.Range.Head == "" {
		return nil, "range-less request (diff ingestion): grounding not applicable"
	}
	cl, err := payload.BuildChangedLines(ctx, req.Repo, req.Range.Base, req.Range.Head)
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
		diffMode: {Entries: entries, Text: b.String(), FileCount: len(kept), Truncation: trunc},
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
	return finalizePreparedReview(ctx, cfg, req, payloads, perAgentMode, slots, diffMode)
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
	}

	// Reviewer runs get truncation failover (Epic 19.5): a truncated, zero-finding
	// response fails over to the slot's fallback instead of being recorded as a
	// silent clean review. The executor builds its own engine without this option.
	opts = append(opts, WithTruncationFailover())

	results := NewEngine(completer, opts...).Run(runCtx, p.Slots)

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
	Entries    []payload.FileEntry
	Text       string
	FileCount  int
	Truncation payload.Truncation
}

// buildPayloads builds each distinct payload mode the roster uses exactly once.
func buildPayloads(ctx context.Context, cfg *ReviewConfig, repo, base, head string) (map[string]modePayload, error) {
	out := map[string]modePayload{}
	for _, mode := range neededModes(cfg) {
		entries, err := payload.BuildEntries(ctx, payload.PayloadMode(mode), repo, base, head)
		if err != nil {
			return nil, fmt.Errorf("building %s payload: %w", mode, err)
		}
		kept, trunc := payload.ApplyByteBudget(entries, cfg.Settings.PayloadByteBudget)
		if trunc.AllDropped {
			return nil, fmt.Errorf("%w (mode %s, dropped %d file(s))", ErrPayloadFullyDropped, mode, len(trunc.FilesDropped))
		}
		var b strings.Builder
		for _, e := range kept {
			b.WriteString(e.Body)
		}
		// FileCount reflects what the reviewer actually saw (post-truncation), not
		// the pre-budget total — the dropped files are recorded in trunc. Entries
		// keeps the raw pre-budget files so buildSlots re-sheds them per agent
		// against each model's window (Epic 19.10 F2).
		out[mode] = modePayload{Entries: entries, Text: b.String(), FileCount: len(kept), Truncation: trunc}
	}
	return out, nil
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

func buildSlots(cfg *ReviewConfig, payloads map[string]modePayload, rng ReviewRange, forceMode, scopeConstraint string, warnOversized bool) ([]Slot, map[string]string, error) {
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

	// buildChain resolves the fallback chain for a primary. Extracted so both the
	// bulk one-slot path and the chunked per-chunk path attach identical chains
	// (a fallback reviews the same persona prompt/payload as its primary — here,
	// the same chunk).
	buildChain := func(name string, primary Agent) ([]Agent, error) {
		var fbs []Agent
		seen := map[string]bool{name: true}
		for fb := cfg.Registry.Agents[name].Fallback; fb != ""; fb = cfg.Registry.Agents[fb].Fallback {
			if seen[fb] {
				break // registry validation guarantees acyclic; defensive stop
			}
			seen[fb] = true
			agent, err := buildFallbackAgent(cfg, primary, fb)
			if err != nil {
				return nil, err
			}
			fbs = append(fbs, agent)
		}
		return fbs, nil
	}

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
		agentEff := payload.EffectiveByteBudget(ac.Model, defaultMaxTokens)
		if len(agentScopeConstraint) > 0 && agentEff > 0 {
			planCap := agentEff / 8
			if mspb := cfg.Settings.MaxSprintPlanBytes; mspb > 0 && mspb < planCap {
				planCap = mspb
			}
			agentScopeConstraint = capScopeConstraintPlan(scopeConstraint, int(planCap))
		}

		// Chunked strategy (Epic 14.3): bin-pack this persona's diff into multiple
		// context-limited calls, one Slot per chunk. Every chunk-slot keeps the
		// SAME persona name, so mergeChunkResults collapses their results into one
		// raw/agent/<persona>/ source with Reviewer=<persona> (AC4) — the 14.2
		// consensus filter still counts the persona once. A run that yields a
		// single chunk (small diff, or one file) falls through to the bulk path so
		// there is nothing to merge.
		if cfg.Settings.ReviewStrategy == reviewStrategyChunked {
			// A non-diff payload (files/blocks mode) carries no `diff --git` markers,
			// so chunkDiff returns a single chunk and the chunked strategy is a silent
			// no-op. Warn once so the operator knows the strategy had no effect for
			// this payload mode rather than assuming the diff was bin-packed. Gated by
			// warnOversized so the resume rebuild path stays quiet (already notified).
			if warnOversized && !warnedChunkedNoop && countDiffFiles(mp.Text) == 0 && mp.FileCount > 1 {
				fmt.Fprintf(os.Stderr, "atcr: warning: review_strategy=chunked has no effect for payload mode %q (no diff --git markers to split on); the whole payload is sent as one chunk\n", mode)
				warnedChunkedNoop = true
			}
			// Per-chunk line budget: an explicit operator-set max_context_lines wins
			// (least surprise); otherwise derive maxLines from THIS agent's model
			// window (Epic 19.10 F3), so a 32k model gets more, smaller chunks and a
			// 144k model gets fewer — both from the same diff, zero files dropped.
			// chunkDiff itself is unchanged; only the source of ml changes.
			ml := payload.ChunkMaxLines(ac.Model, defaultMaxTokens)
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
					effectiveBudget: payload.EffectiveByteBudget(ac.Model, defaultMaxTokens),
					resolvedWindow:  payload.ContextWindowTokens(ac.Model),
					maxLines:        ml,
					chunkTotal:      len(chunks),
					action:          "chunk",
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
					fbs, err := buildChain(name, primary)
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
		bulkWindow := payload.ContextWindowTokens(ac.Model)
		var appliedBudget int64
		if agentEff > 0 {
			appliedBudget = agentEff
			if global := cfg.Settings.PayloadByteBudget; global > 0 && global < appliedBudget {
				appliedBudget = global
			}
			// (A) reserve room for the per-agent SCOPE CONSTRAINT block, prepended
			// uncounted in renderAgent, so plan + budgeted diff together fit this model's
			// window (Epic 19.10 HIGH TD). Floor at 0; the AllDropped guard below is the
			// net when the reservation leaves too little for even one file.
			if s := int64(len(agentScopeConstraint)); s > 0 {
				appliedBudget -= s
				if appliedBudget < 0 {
					appliedBudget = 0
				}
			}
		}
		bulkDegradation := ""
		bulkText, bulkFileCount, bulkTrunc := mp.Text, mp.FileCount, mp.Truncation
		if agentEff == 0 && len(mp.Entries) > 0 {
			// Epic 19.10 TD-002: a model whose window <= output cap + prompt overhead makes
			// EffectiveByteBudget return 0, so the shed below is skipped and the agent keeps
			// the FULL global-budget payload. A positive byte floor is meaningless here (zero
			// room for any input regardless of value), so mark the same honest-degradation
			// state the AllDropped arm records instead of leaving the action unmarked while
			// silently shipping an over-window payload. Currently unreachable — ContextWindowTokens
			// floors at 32768 (eff >= 71680) — so this is defense-in-depth for a future
			// sub-overhead window or a lowered default.
			bulkDegradation = "overflow"
			if warnOversized {
				fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: model window too small to reserve output headroom (effective budget 0); sending the whole payload (may overflow) rather than sizing it\n", name)
			}
		}
		if appliedBudget > 0 && len(mp.Entries) > 0 {
			kept, trunc := payload.ApplyByteBudget(mp.Entries, appliedBudget)
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
				bulkDegradation = "overflow"
				if warnOversized {
					fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: no file fits its model window (%d-byte budget); sending the whole payload (may overflow) rather than an empty review\n", name, appliedBudget)
				}
			} else {
				var pb strings.Builder
				for _, e := range kept {
					pb.WriteString(e.Body)
				}
				bulkText, bulkFileCount, bulkTrunc = pb.String(), len(kept), trunc
				// The per-agent shed dropped files to fit this model's window — a lossy
				// degradation. Record it as the diagnosability degradation_action (F8).
				if trunc.Truncated {
					bulkDegradation = "truncate"
				}
			}
		}
		bulkSizing := agentSizing{
			effectiveBudget: appliedBudget,
			resolvedWindow:  bulkWindow,
			action:          bulkDegradation,
		}
		primary, err := renderAgent(cfg, name, ac, mode, bulkText, bulkFileCount, bulkTrunc, rng, agentScopeConstraint, bulkSizing)
		if err != nil {
			return err
		}
		fbs, err := buildChain(name, primary)
		if err != nil {
			return err
		}
		slots = append(slots, Slot{Primary: primary, Fallbacks: fbs, Serial: serial})
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
type agentSizing struct {
	effectiveBudget int64  // per-agent input byte budget the payload was sized to (0 = unsized)
	resolvedWindow  int    // ContextWindowTokens(model) — the model's context window in tokens
	maxLines        int    // per-model chunk line budget (0 = bulk/non-chunked)
	chunkTotal      int    // chunks this persona's diff was split into (0/1 = unchunked)
	action          string // degradation action: "chunk"/"truncate"/"" (none)
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
		AgentName:    name,
		BaseRef:      rng.Base,
		HeadRef:      rng.Head,
		PayloadMode:  mode,
		FileCount:    fileCount,
		Payload:      scopeConstraint + payloadText,
		ScopeRule:    payload.ScopeRule(payload.PayloadMode(mode)),
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
	// Reserved output cap (Epic 19.10 F8) is recorded only for an agent that
	// actually went through per-model sizing (resolvedWindow > 0). A bare/unsized
	// caller (agentSizing{}) leaves it 0 so its status.json stays byte-identical.
	reservedOut := 0
	if sz.resolvedWindow > 0 {
		reservedOut = defaultMaxTokens
	}
	return Agent{
		Name:             name,
		Provider:         ac.Provider,
		Prompt:           prompt,
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

// buildFallbackAgent builds a fallback that reviews the SAME persona prompt and
// payload as the primary (AC 01-04: "fallback agent tried (same persona)"), only
// the provider/model/temperature/timeout differ.
func buildFallbackAgent(cfg *ReviewConfig, primary Agent, name string) (Agent, error) {
	ac, ok := cfg.Registry.Agents[name]
	if !ok {
		return Agent{}, fmt.Errorf("fallback agent %q not found in registry", name)
	}
	prov, ok := cfg.Registry.Providers[ac.Provider]
	if !ok {
		return Agent{}, fmt.Errorf("fallback agent %q references unknown provider %q", name, ac.Provider)
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
	// is model-specific.
	fbBudget := payload.EffectiveByteBudget(ac.Model, defaultMaxTokens)
	fbWindow := payload.ContextWindowTokens(ac.Model)
	fbReserved := 0
	if fbWindow > 0 {
		fbReserved = defaultMaxTokens
	}
	return Agent{
		Name: name,
		// A fallback keys on its OWN provider: if it uses a different provider than
		// the primary, it gets that provider's breaker (so a fallback can succeed
		// while the primary's circuit is open).
		Provider:    ac.Provider,
		Prompt:      primary.Prompt,
		PayloadMode: primary.PayloadMode,
		Truncation:  primary.Truncation,
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
		ChunkTotal:           primary.ChunkTotal,
		EffectiveBudget:      fbBudget,
		ResolvedWindow:       fbWindow,
		ReservedOutputTokens: fbReserved,
		DegradationAction:    primary.DegradationAction,
		chunkMaxLines:        primary.chunkMaxLines,
		// Diff-cache key (Epic 5.2): a fallback reviews the SAME rendered prompt as
		// the primary but on its OWN model and temperature, so it keys on the
		// primary's prompt with the fallback's model/temperature — a substitute
		// model must not collide with the primary's cache entry. Its sizing token
		// (Epic 19.10 F7) uses the fallback's OWN effective budget under the slot's
		// chunk regime, so it also never collides across sizing regimes.
		CacheKey: diffCacheKey(primary.Prompt, ac.Model, prov.BaseURL, ac.Temperature, sizingToken(fbBudget, primary.chunkMaxLines)),
		Invocation: llmclient.Invocation{
			BaseURL:     prov.BaseURL,
			APIKeyEnv:   prov.APIKeyEnv,
			Model:       ac.Model,
			Temperature: ac.Temperature,
			MaxTokens:   maxTokensPtr(),
			Prompt:      primary.Prompt,
		},
	}, nil
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
		func(r Result) string { return r.Agent })
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
func reviewStageForAgents[T any](items []T, requested func(T) bool, degraded func(T) bool, name func(T) string) *payload.ReviewStage {
	var enabled, deg []string
	for _, it := range items {
		if !requested(it) {
			continue
		}
		enabled = append(enabled, name(it))
		if degraded(it) {
			deg = append(deg, name(it))
		}
	}
	if len(enabled) == 0 {
		return nil
	}
	return &payload.ReviewStage{Agents: append([]string(nil), enabled...), ToolsEnabled: enabled, ToolsDegraded: deg}
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
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// rosterNames returns the full roster (parallel lane then serial lane).
func rosterNames(p *registry.ProjectConfig) []string {
	names := make([]string, 0, len(p.Agents)+len(p.SerialAgents))
	names = append(names, p.Agents...)
	names = append(names, p.SerialAgents...)
	return names
}
