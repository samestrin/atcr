package payload

// Built-in per-file escalation thresholds (Epic 35.1, retuned in Epic 35.4).
// They are the resolved values when a registry sets no payload_escalation block.
//
// # Acceptance target for these numbers
//
// These are not chosen by feel — they are tuned against a measured replay of
// real history, and any future change to them must re-validate against the same
// target:
//
//	Target band: 15-25% of changed Go files promoted above their configured
//	mode, at <= +40% payload bytes over that mode. The byte ceiling is the
//	binding constraint; the file-rate band is the readable proxy for it.
//
// The band is anchored to the payload-byte budget rather than to a bare
// percentage because an operator who configures `diff` is buying a token cost,
// not a file count. A tighter byte anchor is not reachable by tuning: disabling
// the adjacency signal outright with min_hunks 10 and min_cyclomatic 25 (the
// sweep's strictest candidate) still measures +25% bytes, because the files
// that promote are the large ones.
//
// Measured over `main`'s last 40 commits (400 changed Go files) with
// TestEscalationReplay_MeasureRepoHistory, 2026-07-28. The window head is
// pinned so the SAME window can be re-derived after main advances — without
// the SHA a re-measurement runs over a different population and a threshold
// regression cannot be separated from window drift:
//
//	window head: 0819bcf105bbcf3e0e213f03ef32fb58bcae16a5
//
//	before (35.1 defaults 4/10/15): 37.0% promoted, +48.0% bytes
//	after  (this block, 8/2/20):    21.5% promoted, +34.1% bytes
//
// Re-measure over the same window with:
//
//	ATCR_REPLAY=1 ATCR_REPLAY_REF=0819bcf105bbcf3e0e213f03ef32fb58bcae16a5 \
//	  ATCR_REPLAY_COMMITS=40 go test ./internal/payload/ -run TestEscalationReplay -v
//
// (ATCR_REPLAY_REF=main re-measures whatever window main then points at — that
// tracks drift but is NOT a re-derivation of the numbers above.)
//
// Damping one signal shifts load onto the others, so re-measure ALL of them
// (the harness reports per-signal rates) rather than only the one being changed.
const (
	// DefaultEscalationChurnRatio fires when at least half a file's HEAD lines
	// are touched — the "the file was substantially rewritten" signal.
	DefaultEscalationChurnRatio = 0.5
	// DefaultEscalationMinHunks fires on scattered edits: this many or more
	// separate hunks in one file is the architectural-thrashing shape a net diff
	// hides. Tuned from 4 to 8 in Epic 35.4: at 4, an ordinary rename or signature
	// change rippling through a file promoted it, and the signal fired on 23% of
	// changed Go files (counted independently — signals overlap, so the
	// per-signal rates sum above the promotion rate).
	DefaultEscalationMinHunks = 8
	// DefaultEscalationHunkGapLines fires when two hunks sit closer than this
	// many unchanged lines apart, i.e. the same region was churned twice. Tuned
	// from 10 to 2 in Epic 35.4: under --unified=0 a single logical change
	// routinely leaves hunks a few lines apart, so a 10-line window measured the
	// ordinary shape of a diff rather than genuine same-region churn — it was the
	// single worst offender at 28.5% of changed Go files (counted independently —
	// signals overlap, so the per-signal rates sum above the promotion rate),
	// and still fires on 13.0% at this setting. At 2, only hunks separated by
	// one unchanged line count as the same region.
	//
	// Note the granularity floor this implies: git does not emit two hunks with
	// ZERO unchanged lines between them under --unified=0 (it merges them into
	// one), so `gap < 1` is unsatisfiable for well-formed ranges and the settings
	// 0 and 1 both mean "off". 2 is therefore the narrowest setting that still
	// fires. This is a property of the `gap < N` formulation, not of this value.
	DefaultEscalationHunkGapLines = 2
	// DefaultEscalationMinCyclomatic is the McCabe floor above which a CHANGED
	// function's control flow is too branchy to review from hunks alone. The score
	// is scoped to the functions the diff touched, not the file-wide maximum.
	// Tuned from 15 to 20 in Epic 35.4: 15 is the conventional "complex function"
	// line, but the score now measures only the changed function, where ordinary
	// validation and dispatch code clears 15 without being unreadable from hunks.
	DefaultEscalationMinCyclomatic = 20
	// DefaultEscalationMaxSkeletonLines caps how many declaration headers a
	// skeleton renders. A generated file with hundreds of declarations would
	// otherwise prepend hundreds of lines to a one-line diff, in every mode
	// payload, for every parallel agent — inverting the token saving the whole
	// feature exists to protect.
	DefaultEscalationMaxSkeletonLines = 60
	// DefaultEscalationMaxFiles caps how many changed files a run will scan.
	// Above it the escalation and skeleton passes are skipped wholesale, because
	// each scanned file costs one `git show` plus one AST parse.
	DefaultEscalationMaxFiles = 50
)

// EscalationConfig is the resolved (defaults-applied) per-file escalation
// configuration the builder acts on. A zero value disables every signal, which
// is how an operator turns the feature off without a code change.
type EscalationConfig struct {
	ChurnRatio    float64 // 0 disables; else fires at >= ratio of HEAD lines changed
	MinHunks      int     // 0 disables; else fires at >= this many hunks
	HunkGapLines  int     // 0 disables; else fires when two hunks are < this many lines apart
	MinCyclomatic int     // 0 disables; else fires at >= this McCabe score
	MaxFiles      int     // 0 disables the whole feature; else the changed-file ceiling
	// MaxSkeletonLines caps rendered declaration headers per file; 0 disables
	// skeleton injection entirely.
	MaxSkeletonLines int
}

// EscalationOverrides is the optional, unset-distinguishable form of
// EscalationConfig: nil means "use the default", a set pointer means "use this
// value" — including an explicit 0 to disable one signal.
//
// It mirrors registry.PayloadEscalationConfig field for field. The two are
// deliberately separate types: internal/payload must not import
// internal/registry (see sprintplan.go), the same boundary that keeps
// validPayloadModes duplicated in registry/payload.go. The single field-copy
// between them lives at the call site and is pinned by a sync test.
type EscalationOverrides struct {
	ChurnRatio       *float64
	MinHunks         *int
	HunkGapLines     *int
	MinCyclomatic    *int
	MaxFiles         *int
	MaxSkeletonLines *int
}

// DefaultEscalationConfig returns the built-in escalation thresholds.
func DefaultEscalationConfig() EscalationConfig {
	return EscalationConfig{
		ChurnRatio:       DefaultEscalationChurnRatio,
		MinHunks:         DefaultEscalationMinHunks,
		HunkGapLines:     DefaultEscalationHunkGapLines,
		MinCyclomatic:    DefaultEscalationMinCyclomatic,
		MaxFiles:         DefaultEscalationMaxFiles,
		MaxSkeletonLines: DefaultEscalationMaxSkeletonLines,
	}
}

// ResolveEscalationConfig turns optional overrides into the resolved config the
// builder acts on, applying a default per unset field. It is pure and total: a
// zero-value EscalationOverrides yields DefaultEscalationConfig.
func ResolveEscalationConfig(o EscalationOverrides) EscalationConfig {
	c := DefaultEscalationConfig()
	if o.ChurnRatio != nil {
		c.ChurnRatio = *o.ChurnRatio
	}
	if o.MinHunks != nil {
		c.MinHunks = *o.MinHunks
	}
	if o.HunkGapLines != nil {
		c.HunkGapLines = *o.HunkGapLines
	}
	if o.MinCyclomatic != nil {
		c.MinCyclomatic = *o.MinCyclomatic
	}
	if o.MaxFiles != nil {
		c.MaxFiles = *o.MaxFiles
	}
	if o.MaxSkeletonLines != nil {
		c.MaxSkeletonLines = *o.MaxSkeletonLines
	}
	return c
}

// Enabled reports whether the escalation and skeleton passes should run at all
// for a change set of n files. A zero MaxFiles disables the feature outright;
// otherwise a change set larger than the cap degrades to plain diff, because
// every scanned file costs a `git show` plus an AST parse and that product is
// paid once per distinct payload mode built for the roster (at most twice, since
// files mode skips analysis), not once per parallel agent.
func (c EscalationConfig) Enabled(n int) bool {
	return c.MaxFiles > 0 && n <= c.MaxFiles
}

// anySignalEnabled reports whether at least one escalation signal or skeleton
// injection is switched on. Zero is the registry's documented "off" per
// threshold, so an all-zero config (with MaxFiles left at its default) could
// only ever reproduce the base mode with an empty skeleton — the analysis
// pass is skipped outright rather than paid for nothing. This gates only the
// analyze expression, never Enabled(n): the degradation report must keep
// meaning strictly "file count exceeded the cap".
func (c EscalationConfig) anySignalEnabled() bool {
	return c.ChurnRatio > 0 || c.MinHunks > 0 || c.HunkGapLines > 0 ||
		c.MinCyclomatic > 0 || c.MaxSkeletonLines > 0
}

// fileSignals are the per-file measurements the escalation heuristic scores.
// cyclomatic is 0 when complexity was not computed — a non-Go file, a parse
// failure, or no changed function to score — and is the max McCabe score over the
// functions the diff TOUCHED, not the file-wide maximum; a 0 there means "unknown",
// never "zero", so it cannot fire a signal on its own. churnApplicable states
// whether the churn ratio is a meaningful signal for this file: it is false for an
// added file (whose diff is definitionally 100% churn and so carries no
// information) and for a file with no numstat entry (churn unmeasurable), so a
// not-applicable file no longer has to be encoded by zeroing the unrelated
// headLines field.
type fileSignals struct {
	changedLines    int
	headLines       int
	hunks           []lineRange
	cyclomatic      int
	churnApplicable bool
}

// escalate returns the payload mode a file should actually be rendered in.
//
// Two independent signals feed a ladder: either one alone promotes the file to
// blocks, and both together promote it to files. The result is never below base,
// so a run configured for files never de-escalates and a blocks-mode file can
// only move up to files.
func (c EscalationConfig) escalate(base PayloadMode, s fileSignals) PayloadMode {
	target := ModeDiff
	switch {
	case c.diffNativeFires(s) && c.complexityFires(s):
		target = ModeFiles
	case c.diffNativeFires(s), c.complexityFires(s):
		target = ModeBlocks
	}
	if modeRank(target) > modeRank(base) {
		return target
	}
	return base
}

// diffNativeFires reports whether the parse-free signals — churn ratio, hunk
// count, hunk adjacency — indicate a structurally confusing diff.
//
// The three clauses are separate predicates rather than inline conditions so
// the escalation-rate measurement can attribute a promotion to the signal that
// actually caused it: this disjunction short-circuits, so a file firing both
// churn and adjacency would otherwise only ever be counted against churn, and
// "which signal is the worst offender" would be unanswerable (Epic 35.4).
func (c EscalationConfig) diffNativeFires(s fileSignals) bool {
	return c.churnFires(s) || c.hunkCountFires(s) || c.hunksAreAdjacent(s.hunks)
}

// churnFires reports whether the file was substantially rewritten: changed
// lines as a fraction of HEAD lines at or above ChurnRatio. changedLines is
// max(added, deleted), not their sum (diff.go), so a pure move does not read as
// double churn. A file whose churn measure is not applicable — an added file,
// or one with no numstat entry — never fires it.
func (c EscalationConfig) churnFires(s fileSignals) bool {
	return c.ChurnRatio > 0 && s.churnApplicable && s.headLines > 0 &&
		float64(s.changedLines)/float64(s.headLines) >= c.ChurnRatio
}

// hunkCountFires reports whether the file's edits are scattered across at least
// MinHunks separate hunks.
func (c EscalationConfig) hunkCountFires(s fileSignals) bool {
	return c.MinHunks > 0 && len(s.hunks) >= c.MinHunks
}

// hunksAreAdjacent reports whether any two consecutive hunks are separated by
// fewer than HunkGapLines unchanged lines. Hunks arrive in ascending head-line
// order from the diff, so a single pass over neighbours is sufficient.
func (c EscalationConfig) hunksAreAdjacent(hunks []lineRange) bool {
	if c.HunkGapLines <= 0 {
		return false
	}
	for i := 1; i < len(hunks); i++ {
		gap := hunks[i].start - hunks[i-1].end - 1
		if gap < c.HunkGapLines {
			return true
		}
	}
	return false
}

// complexityFires reports whether the changed region's McCabe score clears the
// floor — cyclomatic is scoped to the functions the diff touched (analyzeFile),
// so an unchanged branchy function elsewhere in the file cannot fire it. A
// cyclomatic of 0 means the score was never computed (or no changed function
// cleared it) and is not a signal.
func (c EscalationConfig) complexityFires(s fileSignals) bool {
	return c.MinCyclomatic > 0 && s.cyclomatic > 0 && s.cyclomatic >= c.MinCyclomatic
}

// HigherContextMode returns whichever of a and b shows the reviewer more
// context (diff < blocks < files).
//
// A roster whose agents use different payload modes builds one payload per mode,
// so the same file can escalate to blocks in the diff-mode payload and to files
// in the blocks-mode payload. Recording "the mode this file was rendered in"
// then depends on which payload is inspected last — a map-iteration-order
// dependency. Folding with this function makes the recorded value the
// most-context mode any reviewer actually saw, which is both deterministic and
// the honest answer to "what was this file reviewed as".
func HigherContextMode(a, b PayloadMode) PayloadMode {
	if modeRank(b) > modeRank(a) {
		return b
	}
	return a
}

// modeRank orders the modes by how much context they hand the reviewer, so
// escalation can be expressed as a monotonic maximum.
func modeRank(m PayloadMode) int {
	switch m {
	case ModeBlocks:
		return 1
	case ModeFiles:
		return 2
	default:
		return 0
	}
}
