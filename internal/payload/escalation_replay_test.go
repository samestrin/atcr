package payload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// replaySignal names the four independent escalation signals the replay harness
// attributes promotions to. diffNativeFires short-circuits across the first
// three, so per-signal attribution has to evaluate each predicate on its own —
// otherwise a file that fires churn AND adjacency would only ever be counted
// against churn, and the "which lever is the worst offender" question the
// measurement exists to answer could not be asked.
type replaySignal int

const (
	sigChurn replaySignal = iota
	sigHunkCount
	sigAdjacency
	sigComplexity
	sigCount
)

// replayStats accumulates escalation outcomes over a replay window. It is the
// arithmetic the AC2 measurement and the AC3 band decision rest on, so it is
// tested directly rather than trusted: a wrong denominator here would silently
// invalidate every number this epic reports.
type replayStats struct {
	commits  int
	files    int
	fired    [sigCount]int
	promoted int
	toBlocks int
	toFiles  int

	// baseBytes/escalatedBytes are the rendered payload sizes for THIS
	// population, so a Go-only byte delta is never reported against an
	// all-files byte total.
	baseBytes      int
	escalatedBytes int
}

// addBytes folds one file's rendered sizes in.
func (r *replayStats) addBytes(base, escalated int) {
	r.baseBytes += base
	r.escalatedBytes += escalated
}

// byteDelta is the percentage increase in payload bytes escalation caused over
// this population. This is the figure the AC3 band is anchored to: a bare
// promotion percentage says nothing about what an operator actually pays.
func (r *replayStats) byteDelta() float64 {
	if r.baseBytes <= 0 {
		return 0
	}
	return float64(r.escalatedBytes-r.baseBytes) / float64(r.baseBytes) * 100
}

// record measures one changed file against cfg and folds the outcome in. The
// promotion decision goes through the production escalate(), so the harness
// measures the shipped heuristic rather than a parallel reimplementation of it.
func (r *replayStats) record(cfg EscalationConfig, base PayloadMode, s fileSignals) {
	r.files++
	if cfg.churnFires(s) {
		r.fired[sigChurn]++
	}
	if cfg.hunkCountFires(s) {
		r.fired[sigHunkCount]++
	}
	if cfg.hunksAreAdjacent(s.hunks) {
		r.fired[sigAdjacency]++
	}
	if cfg.complexityFires(s) {
		r.fired[sigComplexity]++
	}
	switch got := cfg.escalate(base, s); got {
	case base:
		return
	case ModeFiles:
		r.promoted++
		r.toFiles++
	default:
		r.promoted++
		r.toBlocks++
	}
}

// promotionRate is the percentage of analyzed files promoted above their
// configured mode.
func (r *replayStats) promotionRate() float64 { return percentOf(r.promoted, r.files) }

// signalRate is the percentage of analyzed files for which sig fired, counted
// independently of whether another signal fired on the same file.
func (r *replayStats) signalRate(sig replaySignal) float64 {
	if sig < 0 || sig >= sigCount {
		return 0
	}
	return percentOf(r.fired[sig], r.files)
}

// percentOf is n/total as a percentage, defined as 0 for an empty denominator
// so a window that analyzed nothing reports 0% rather than NaN.
func percentOf(n, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

// TestReplayStats_RatesAndAttribution pins the harness arithmetic against a
// hand-computed fixture: five files with known signals, so every rate the
// measurement reports has an independently-derived expected value.
func TestReplayStats_RatesAndAttribution(t *testing.T) {
	cfg := DefaultEscalationConfig()
	var r replayStats

	// A: churn only (60/100 >= 0.5). One hunk, no complexity.
	r.record(cfg, ModeDiff, fileSignals{
		changedLines: 60, headLines: 100, churnApplicable: true,
		hunks: []lineRange{{start: 1, end: 60}},
	})
	// B: complexity only. Churn not applicable (added file), single hunk.
	r.record(cfg, ModeDiff, fileSignals{
		headLines: 100, hunks: []lineRange{{start: 1, end: 4}}, cyclomatic: 20,
	})
	// C: churn AND complexity -> the only file that reaches files mode.
	r.record(cfg, ModeDiff, fileSignals{
		changedLines: 90, headLines: 100, churnApplicable: true,
		hunks: []lineRange{{start: 1, end: 90}}, cyclomatic: 20,
	})
	// D: nothing fires. 5/100 churn, one hunk, trivial complexity.
	r.record(cfg, ModeDiff, fileSignals{
		changedLines: 5, headLines: 100, churnApplicable: true,
		hunks: []lineRange{{start: 1, end: 5}}, cyclomatic: 3,
	})
	// E: adjacency only. Gap between hunks is 14-12-1 = 1 < 2.
	r.record(cfg, ModeDiff, fileSignals{
		changedLines: 10, headLines: 100, churnApplicable: true,
		hunks: []lineRange{{start: 10, end: 12}, {start: 14, end: 20}},
	})
	// F: hunk count only. Eight hunks ten lines apart — reaches MinHunks=8 with
	// no adjacency (gap 8 >= 2), churn not applicable, trivial complexity.
	r.record(cfg, ModeDiff, fileSignals{
		headLines: 100,
		hunks: []lineRange{
			{start: 10, end: 11}, {start: 20, end: 21}, {start: 30, end: 31},
			{start: 40, end: 41}, {start: 50, end: 51}, {start: 60, end: 61},
			{start: 70, end: 71}, {start: 80, end: 81},
		},
	})

	require.Equal(t, 6, r.files)
	require.Equal(t, 5, r.promoted, "A, B, C, E and F promote; D does not")
	require.Equal(t, 4, r.toBlocks, "A, B, E and F reach blocks")
	require.Equal(t, 1, r.toFiles, "only C fires both sides of the ladder")

	require.InDelta(t, 83.333, r.promotionRate(), 0.001)
	require.InDelta(t, 33.333, r.signalRate(sigChurn), 0.001, "A and C")
	require.InDelta(t, 16.667, r.signalRate(sigHunkCount), 0.001, "F only — regression guard on the hunks= sweep column")
	require.InDelta(t, 16.667, r.signalRate(sigAdjacency), 0.001, "E only")
	require.InDelta(t, 33.333, r.signalRate(sigComplexity), 0.001, "B and C")

	// Byte accounting: 200 extra bytes over a 1500-byte base = +13.33%.
	r.addBytes(1000, 1200)
	r.addBytes(500, 500)
	require.Equal(t, 1500, r.baseBytes)
	require.Equal(t, 1700, r.escalatedBytes)
	require.InDelta(t, 13.333, r.byteDelta(), 0.001)
}

// TestReplayStats_EmptyWindowIsNotADivideByZero guards the degenerate case: a
// window that analyzed nothing must report 0%, not NaN, or the reported rate
// would be unusable as an acceptance number.
func TestReplayStats_EmptyWindowIsNotADivideByZero(t *testing.T) {
	var r replayStats
	require.Equal(t, 0.0, r.promotionRate())
	require.Equal(t, 0.0, r.signalRate(sigChurn))
	require.Equal(t, 0.0, r.byteDelta(), "baseBytes <= 0 must report 0, not NaN")
}

// Replay-harness knobs. The window defaults to 40 commits because that is the
// exact window the original ~59% measurement was taken over (Epic 35.4 source
// TD). Since replayCommits passes --no-merges, the window is the last 40
// NON-MERGE commits: the direct comparability to the original measurement
// holds only on this repo's merge-free squash history — on a history with
// merges the window spans a different stretch of history.
const (
	defaultReplayCommits = 40
	replayCommitsEnv     = "ATCR_REPLAY_COMMITS"
	replayRefEnv         = "ATCR_REPLAY_REF"
	replayEnableEnv      = "ATCR_REPLAY"
)

// replayWindow reads the configurable commit window. A malformed or
// non-positive value falls back to the default rather than failing: this is a
// measurement knob, not an assertion input.
func replayWindow(t *testing.T) int {
	t.Helper()
	raw := os.Getenv(replayCommitsEnv)
	if raw == "" {
		return defaultReplayCommits
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		t.Logf("replay: ignoring malformed %s=%q, using default %d", replayCommitsEnv, raw, defaultReplayCommits)
		return defaultReplayCommits
	}
	return n
}

// replayRepoRoot walks up from the test's working directory to the enclosing
// git work tree. Returns ok=false (rather than failing) when there is none, so
// the harness degrades to a skip in an exported tarball or a vendored copy.
func replayRepoRoot(t *testing.T) (string, bool) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// replayCommits lists up to window single-parent commit SHAs reachable from
// ref, newest first. --end-of-options stops an env-supplied ref beginning with
// '-' from being read as a git option, the same guard verifyRef applies in
// diff.go.
//
// --no-merges is load-bearing, not tidiness: the harness measures each commit
// as parent..commit, and for a merge commit that first-parent range is the
// WHOLE merged branch — every file in it would be counted a second time, once
// here and once in the individual commits, skewing both the rate and the byte
// delta. This repo squash-merges so its history is merge-free, but the harness
// is documented for general use.
func replayCommits(t *testing.T, root, ref string, window int) ([]string, error) {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "log", "--format=%H", "--no-merges",
		"-n", strconv.Itoa(window), "--end-of-options", ref).Output()
	if err != nil {
		// ExitError.Error() is only the process state ("exit status 128"); the
		// actionable message is git's own stderr ("fatal: bad revision ..."),
		// which Output() captures into ExitError.Stderr. Surface it so a typo'd
		// ref reads as a typo'd ref.
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	var shas []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			shas = append(shas, line)
		}
	}
	return shas, nil
}

// replayReport is one measured window: the all-files and Go-only populations,
// each carrying its own rates and payload-byte delta.
type replayReport struct {
	all      replayStats
	goOnly   replayStats
	walked   int // commits the window walked
	degraded int // commits whose change set exceeded MaxFiles, so nothing escalated
	skipped  int // files analyzeFile declined to measure (binary, deleted, unreadable)
	unbilled int // files whose payload render failed, so they carry no byte measurement
	errored  int // commits whose change-set lookup failed outright
	// unresolved counts commits dropped at the parent rev-parse: root commits
	// and the shallow-clone boundary are legitimate, but a systematically
	// failing resolution (e.g. non-SHA lines injected by a global gitconfig)
	// would otherwise empty the measurement window silently.
	unresolved int
}

// TestEscalationReplay_MeasureRepoHistory is the AC1 measurement harness: it
// replays a window of real commits through the SHIPPED escalation heuristic and
// reports the per-signal and overall promotion rates plus the payload-byte cost.
//
// It is deliberately REPORT-ONLY and asserts no threshold band. A band assertion
// against live repo history would drift as new commits land and would break on a
// shallow clone — the AC4 hard gate lives in the deterministic synthetic-fixture
// test instead. It is also opt-in (ATCR_REPLAY=1) because it costs one `git show`
// plus one AST parse per changed file across the whole window, which is far too
// slow for the default unit-test path.
func TestEscalationReplay_MeasureRepoHistory(t *testing.T) {
	if os.Getenv(replayEnableEnv) == "" {
		t.Skipf("replay measurement is opt-in: set %s=1 (window %s, ref %s)",
			replayEnableEnv, replayCommitsEnv, replayRefEnv)
	}
	root, ref, window, shas := replaySetup(t)

	cfg := DefaultEscalationConfig()
	rep := replayEvaluate(t, root, cfg, shas)

	logReplayReport(t, ref, window, cfg, rep)

	require.Greater(t, rep.all.files, 0,
		"an opted-in measurement over a non-empty window must analyze at least one file — a zero here means every commit fell out of the window (check the unresolved/errored counts above), and a 0.0% report would pass silently")
}

// replaySetup resolves the work tree, ref, window and commit list shared by the
// measurement and the sweep, skipping (never failing) when history is
// unavailable. A git error is reported verbatim so a typo'd ref does not read
// as "shallow clone".
func replaySetup(t *testing.T) (root, ref string, window int, shas []string) {
	t.Helper()
	root, ok := replayRepoRoot(t)
	if !ok {
		t.Skip("replay measurement needs a git work tree")
	}
	ref = os.Getenv(replayRefEnv)
	if ref == "" {
		ref = "HEAD"
	}
	window = replayWindow(t)
	shas, err := replayCommits(t, root, ref, window)
	if err != nil {
		t.Skipf("replay measurement could not list commits at %s: %v", ref, err)
	}
	if len(shas) == 0 {
		t.Skipf("replay measurement found no commits at %s (shallow clone?)", ref)
	}
	return root, ref, window, shas
}

// TestEscalationReplay_SweepCandidateThresholds reports what each candidate
// threshold set would have measured over the same window. Tuning a default
// without this is guesswork: the plan's own risk register notes that damping one
// signal shifts load onto another, which is only visible by re-measuring every
// signal per candidate. Report-only and opt-in, for the same reasons as the
// measurement above.
func TestEscalationReplay_SweepCandidateThresholds(t *testing.T) {
	if os.Getenv(replayEnableEnv) == "" {
		t.Skipf("threshold sweep is opt-in: set %s=1", replayEnableEnv)
	}
	root, _, _, shas := replaySetup(t)

	// Candidates are built from an EXPLICIT pre-tuning baseline, not from
	// DefaultEscalationConfig(): the shipped defaults are themselves an output of
	// this sweep, so rooting the candidates on them would silently re-label every
	// row the next time a default moves. Every field is literal for the same
	// reason — inheriting ChurnRatio, MaxFiles or MaxSkeletonLines from the
	// shipped defaults would re-label the "pre-tuning" row just as surely.
	preTuning := EscalationConfig{
		ChurnRatio:       0.5,
		MinHunks:         4,
		HunkGapLines:     10,
		MinCyclomatic:    15,
		MaxFiles:         50,
		MaxSkeletonLines: 60,
	}

	for _, c := range []struct {
		label string
		cfg   EscalationConfig
	}{
		{"pre-tuning (35.1 defaults)", preTuning},
		{"gap 3", withHunkGap(preTuning, 3)},
		{"gap 2", withHunkGap(preTuning, 2)},
		{"gap 0 (adjacency off)", withHunkGap(preTuning, 0)},
		{"gap 3 + hunks 8", withMinHunks(withHunkGap(preTuning, 3), 8)},
		{"gap 2 + hunks 8", withMinHunks(withHunkGap(preTuning, 2), 8)},
		{"gap 3 + hunks 8 + cyclo 20", withCyclo(withMinHunks(withHunkGap(preTuning, 3), 8), 20)},
		{"gap 2 + hunks 8 + cyclo 20", withCyclo(withMinHunks(withHunkGap(preTuning, 2), 8), 20)},
		{"gap 2 + hunks 10 + cyclo 25", withCyclo(withMinHunks(withHunkGap(preTuning, 2), 10), 25)},
		{"gap 0 + hunks 10 + cyclo 25", withCyclo(withMinHunks(withHunkGap(preTuning, 0), 10), 25)},
		{"SHIPPED defaults", DefaultEscalationConfig()},
	} {
		rep := replayEvaluate(t, root, c.cfg, shas)
		t.Logf("candidate %-28s go_files=%d promoted=%.1f%% churn=%.1f%% hunks=%.1f%% adj=%.1f%% cyclo=%.1f%% go_bytes=%+.1f%%",
			c.label, rep.goOnly.files, rep.goOnly.promotionRate(),
			rep.goOnly.signalRate(sigChurn), rep.goOnly.signalRate(sigHunkCount),
			rep.goOnly.signalRate(sigAdjacency), rep.goOnly.signalRate(sigComplexity),
			rep.goOnly.byteDelta())
	}
}

func withHunkGap(c EscalationConfig, n int) EscalationConfig {
	c.HunkGapLines = n
	return c
}

func withMinHunks(c EscalationConfig, n int) EscalationConfig {
	c.MinHunks = n
	return c
}

func withCyclo(c EscalationConfig, n int) EscalationConfig {
	c.MinCyclomatic = n
	return c
}

// replayEvaluate runs the escalation heuristic over every changed file in each
// commit's own diff (parent..commit) and folds the outcomes into a report.
func replayEvaluate(t *testing.T, root string, cfg EscalationConfig, shas []string) replayReport {
	t.Helper()
	ctx := context.Background()
	var rep replayReport

	for _, sha := range shas {
		parent := sha + "^"
		if err := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", "--end-of-options", parent+"^{commit}").Run(); err != nil {
			rep.unresolved++
			continue // root commit, or the shallow-clone boundary
		}
		rep.walked++
		g := &gitRunner{ctx: ctx, dir: root, escalation: cfg}
		files, err := g.changedFilesMemo(parent, sha)
		if err != nil {
			// Distinguished from an empty change set: a systematically failing
			// lookup (bad object, corrupt pack) would otherwise shrink the sample
			// silently, as if those commits were legitimately empty.
			rep.errored++
			t.Logf("replay: skipping %s, change-set lookup failed: %v", sha[:8], err)
			continue
		}
		if len(files) == 0 {
			continue
		}
		// A change set above MaxFiles skips escalation wholesale, so nothing in it
		// can promote. Counting its files in the denominator would understate the
		// rate for the commits where the heuristic actually ran.
		if !cfg.Enabled(len(files)) {
			rep.degraded++
			continue
		}
		rep.all.commits++
		rep.goOnly.commits++
		for _, f := range files {
			fc, ok := g.analyzeFile(parent, sha, f)
			if !ok {
				rep.skipped++
				continue
			}
			isGo := strings.EqualFold(filepath.Ext(f.path), ".go")
			rep.all.record(cfg, ModeDiff, fc.signals)
			if isGo {
				rep.goOnly.record(cfg, ModeDiff, fc.signals)
			}
			accumulateBytes(g, cfg, parent, sha, f, fc, isGo, &rep)
		}
	}
	return rep
}

// accumulateBytes measures what the promotion actually costs: the rendered
// payload body at the configured mode versus at the escalated mode.
//
// It mirrors buildEntriesValidated's render exactly, skeleton included — a
// non-promoted file still gets a skeleton prepended, and a files-promoted file
// deliberately loses it because the whole HEAD file is already present. Measuring
// bare fileBody on both sides would overstate the delta by charging escalation
// for bytes the configured mode was already paying.
//
// A render failure drops the file from BOTH sides (so the ratio never counts one
// without the other) and is counted, so a shrunken byte population is visible in
// the report rather than silent.
func accumulateBytes(g *gitRunner, cfg EscalationConfig, base, head string, f changedFile, fc fileContext, isGo bool, rep *replayReport) {
	promoted := cfg.escalate(ModeDiff, fc.signals)

	baseBody, err := g.fileBody(ModeDiff, base, head, f)
	if err != nil {
		rep.unbilled++
		return
	}
	baseBytes := len(injectSkeleton(baseBody, fc.skeleton))

	escalatedBytes := baseBytes
	if promoted != ModeDiff {
		promotedBody, err := g.fileBody(promoted, base, head, f)
		if err != nil {
			rep.unbilled++
			return
		}
		skel := fc.skeleton
		if promoted == ModeFiles {
			skel = ""
		}
		escalatedBytes = len(injectSkeleton(promotedBody, skel))
	}

	rep.all.addBytes(baseBytes, escalatedBytes)
	if isGo {
		rep.goOnly.addBytes(baseBytes, escalatedBytes)
	}
}

// logReplayReport prints the measurement. Sample size is reported alongside every
// rate so an unrepresentative window is visible rather than implied.
func logReplayReport(t *testing.T, ref string, window int, cfg EscalationConfig, rep replayReport) {
	t.Helper()
	t.Logf("escalation replay: ref=%s window=%d commits_walked=%d commits_measured=%d degraded=%d errored=%d unresolved=%d files_analyzed=%d files_skipped=%d files_unbilled=%d",
		ref, window, rep.walked, rep.all.commits, rep.degraded, rep.errored, rep.unresolved, rep.all.files, rep.skipped, rep.unbilled)
	t.Logf("thresholds: churn_ratio=%.2f min_hunks=%d hunk_gap_lines=%d min_cyclomatic=%d max_files=%d",
		cfg.ChurnRatio, cfg.MinHunks, cfg.HunkGapLines, cfg.MinCyclomatic, cfg.MaxFiles)

	for _, s := range []struct {
		label string
		st    replayStats
	}{
		{"all changed files", rep.all},
		{"changed .go files", rep.goOnly},
	} {
		t.Logf("%s: n=%d promoted=%.1f%% (blocks=%d files=%d)",
			s.label, s.st.files, s.st.promotionRate(), s.st.toBlocks, s.st.toFiles)
		t.Logf("%s per-signal: churn=%.1f%% hunk_count=%.1f%% adjacency=%.1f%% complexity=%.1f%%",
			s.label,
			s.st.signalRate(sigChurn), s.st.signalRate(sigHunkCount),
			s.st.signalRate(sigAdjacency), s.st.signalRate(sigComplexity))
		t.Logf("%s payload bytes: base=%d escalated=%d delta=%+.1f%%",
			s.label, s.st.baseBytes, s.st.escalatedBytes, s.st.byteDelta())
	}
}
