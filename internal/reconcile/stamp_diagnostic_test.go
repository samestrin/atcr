package reconcile

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// captureHandler collects the slog records stampJustifications emits, so the
// zero-match diagnostic — the only user-visible output of the matchOutcome split —
// can be asserted rather than merely presumed.
type captureHandler struct {
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// note returns the "note" attribute of the first record at the given level, plus
// whether such a record was emitted at all.
func (h *captureHandler) note(level slog.Level) (string, bool) {
	for _, r := range h.records {
		if r.Level != level {
			continue
		}
		var got string
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "note" {
				got = a.Value.String()
				return false
			}
			return true
		})
		return got, true
	}
	return "", false
}

func (h *captureHandler) attr(key string) (slog.Value, bool) {
	for _, r := range h.records {
		var got slog.Value
		var found bool
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == key {
				got, found = a.Value, true
				return false
			}
			return true
		})
		if found {
			return got, true
		}
	}
	return slog.Value{}, false
}

func captureStampLogs(t *testing.T, jf []JSONFinding, reviewDir string) *captureHandler {
	t.Helper()
	h := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	stampJustifications(jf, reviewDir)
	return h
}

// A shortfall caused by every anchor's section being a quoted example must NOT be
// reported as possible format drift. The parser worked; the reviewer fenced its
// findings. Sending an operator after a parser problem that is not there is the whole
// cost of conflating the two, and the counter that separates them has no other
// observable — the log line IS the feature.
func TestStampJustifications_AllElidedIsNotReportedAsFormatDrift(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# Host review\n\n"+
		"## Findings\n\n"+
		"```\n"+
		"internal/auth/token.go:42 HIGH JWT signature not verified\n"+
		"```\n")

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Problem: "JWT sig", Reviewers: []string{"host"}}}
	h := captureStampLogs(t, jf, reviewDir)

	require.Empty(t, jf[0].Justification, "a fully fenced section carries no reviewer prose to stamp")

	note, ok := h.note(slog.LevelWarn)
	require.True(t, ok, "a zero-match run must still warn")
	// Matched on the ACCUSATION, not the phrase: the replacement message names
	// "format drift" precisely in order to rule it out, so a bare substring check
	// would fail on the correct wording.
	require.NotContains(t, note, "possible format drift",
		"the anchors matched — accusing format drift sends the operator after a parser bug that is not there")
	require.Contains(t, note, "entirely quoted",
		"the warning must name the condition that actually occurred")

	elided, ok := h.attr("elided")
	require.True(t, ok, "the elided count is what distinguishes this case; it must be reported")
	require.Equal(t, int64(1), elided.Int64())
}

// The genuine no-anchor shortfall keeps the format-drift wording — that message is
// correct for it, and narrowing the elided case must not swallow it.
func TestStampJustifications_NoAnchorStillReportsFormatDrift(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# Host review\n\n"+
		"## Findings\n\n"+
		"Nothing here references the finding's file at all.\n")

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Problem: "JWT sig", Reviewers: []string{"host"}}}
	h := captureStampLogs(t, jf, reviewDir)

	note, ok := h.note(slog.LevelWarn)
	require.True(t, ok, "a zero-match run must warn")
	require.Contains(t, note, "format drift")
}

// A run that stamped something stays at Debug, unchanged by the split.
func TestStampJustifications_MatchedRunDoesNotWarn(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "# Host review\n\n"+
		"## Findings\n\n"+
		"The handler at internal/auth/token.go:42 calls jwt.Parse without jwt.Verify.\n")

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Problem: "JWT sig", Reviewers: []string{"host"}}}
	h := captureStampLogs(t, jf, reviewDir)

	require.NotEmpty(t, jf[0].Justification)
	_, warned := h.note(slog.LevelWarn)
	require.False(t, warned, "a run that stamped a narrative has nothing to warn about")
}

// The synthetic fence marker never reaches Justification as the ONLY content: a block
// that begins inside a released tail always has prose on its first line (released
// lines are not elided), so wroteProse is already true by the time the loop ends.
// That coupling is what makes the marker's deliberate refusal to set wroteProse
// unobservable — pinned here so a change that breaks the coupling surfaces as a failed
// test rather than as a lone "```" in a persisted field.
func TestExtractSection_ReleasedTailAlwaysCarriesProseBesideTheMarker(t *testing.T) {
	lines := []string{
		"## Findings",
		"",
		"```markdown",
		"- internal/auth/token.go:120 HIGH the refresh token is never rotated",
	}

	text, _ := extractSection(lines, 3)

	require.NotEmpty(t, text, "a released tail must never suppress to nothing")
	rest := strings.TrimSpace(strings.TrimPrefix(text, "```"))
	require.NotEmpty(t, rest, "the marker must never be the excerpt's only content")
	require.NotEqual(t, "```", strings.TrimSpace(text))
}

// A shortfall caused by the SALVAGE refusal is a third explanation, and at the
// default log level it is currently invisible: the skip is logged at Debug, and
// when the only source was salvaged the run returns before any diagnostic at all.
// The operator reads "possible format drift" — or, in the every-source case,
// nothing — and goes hunting a parser problem that is not there. This is the same
// defect the elided arm was written to fix, on the newest skip (TD
// internal/reconcile/justification.go:161).
func TestStampJustifications_SalvageSkipIsNamedNotBlamedOnDrift(t *testing.T) {
	t.Run("a salvaged source beside matched findings is counted", func(t *testing.T) {
		reviewDir := t.TempDir()
		// The salvaged source: an unchunked persona whose content is promoted
		// reasoning, so the producer refuses it whole.
		writeSalvagedChunkStatus(t, reviewDir, "dax", nil)
		writeReview(t, reviewDir, "dax", "Maybe **`internal/auth/token.go:42`** is the spot.")
		// A healthy sibling that really does anchor a different finding.
		writeReview(t, reviewDir, "host", "## Findings\n\nThe signature check at `internal/other.go:7` accepts an unsigned token.\n")

		jf := []JSONFinding{{File: "internal/other.go", Line: 7, Problem: "unsigned", Reviewers: []string{"host"}}}
		h := captureStampLogs(t, jf, reviewDir)

		require.NotEmpty(t, jf[0].Justification, "the healthy sibling must still stamp")
		skipped, ok := h.attr("salvage_skipped")
		require.True(t, ok, "the salvage skip must be reported, not left at Debug")
		require.Equal(t, int64(1), skipped.Int64())
	})

	t.Run("every source salvaged still produces a diagnostic", func(t *testing.T) {
		reviewDir := t.TempDir()
		writeSalvagedChunkStatus(t, reviewDir, "dax", nil)
		writeReview(t, reviewDir, "dax", "Maybe **`internal/auth/token.go:42`** is the spot.")

		jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Problem: "JWT sig", Reviewers: []string{"dax"}}}
		h := captureStampLogs(t, jf, reviewDir)

		require.Empty(t, jf[0].Justification)
		require.NotEmpty(t, h.records,
			"an every-source-salvaged run must not return silently: today it reads as 'no narratives at all'")
	})
}

// The salvage-named arm itself — `case salvageSkipped > 0 && elided == 0` — was
// uncovered AND survived mutation: replacing its guard with `case false:` left the
// package green, because the existing cases reach the matched arm and the
// no-narratives early return instead. Nothing proved the arm ever fires, which is
// the one thing it exists to do (TD internal/reconcile/justification.go:168).
//
// Reaching it needs all three conditions at once: a source the salvage skip
// refused, a surviving narrative that anchors NOTHING (so matched is 0), and no
// elided section (so the quoted-example arm does not claim the shortfall first).
func TestStampJustifications_SalvageNamedArmOutranksTheDriftMessage(t *testing.T) {
	reviewDir := t.TempDir()
	// Refused whole: an unchunked persona whose content is promoted reasoning.
	writeSalvagedChunkStatus(t, reviewDir, "dax", nil)
	writeReview(t, reviewDir, "dax", "Maybe **`internal/auth/token.go:42`** is the spot.")
	// A healthy sibling whose prose anchors nothing, and carries no fenced block —
	// so the elided counter stays 0 and this shortfall has exactly one explanation.
	writeReview(t, reviewDir, "host", "# Host review\n\nNothing here references the finding's file at all.\n")

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Problem: "JWT sig", Reviewers: []string{"dax"}}}
	h := captureStampLogs(t, jf, reviewDir)

	require.Empty(t, jf[0].Justification, "precondition: nothing matched")

	note, ok := h.note(slog.LevelWarn)
	require.True(t, ok, "a zero-match run must warn")
	require.Contains(t, note, "was salvaged",
		"the named cause outranks the drift message: the parser worked and the producer refused the reply")
	require.NotContains(t, note, "possible format drift",
		"accusing format drift here sends the operator after a parser bug that is not there — the same cost "+
			"the elided arm exists to avoid")

	skipped, ok := h.attr("salvage_skipped")
	require.True(t, ok, "the counter is the only observable the skip has")
	require.Equal(t, int64(1), skipped.Int64())
}

// The other two arms must carry the salvage_skipped attribute too. Both appends were
// uncovered, so a shortfall that is PARTLY explained by the skip reported only its
// other half — the elided count, or the drift message — and the operator never
// learned a source had been refused at all.
func TestStampJustifications_SalvageCountRidesTheElidedAndDriftArms(t *testing.T) {
	t.Run("elided arm keeps the salvage count beside its own", func(t *testing.T) {
		reviewDir := t.TempDir()
		writeSalvagedChunkStatus(t, reviewDir, "dax", nil)
		writeReview(t, reviewDir, "dax", "Maybe **`internal/auth/token.go:42`** is the spot.")
		// Anchors the finding, but every candidate section is a quoted example, so
		// extractSection suppresses it and the elided counter fires.
		writeReview(t, reviewDir, "host", "# Host review\n\n"+
			"## Findings\n\n"+
			"```\n"+
			"internal/auth/token.go:42 HIGH JWT signature not verified\n"+
			"```\n")

		jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Problem: "JWT sig", Reviewers: []string{"host", "dax"}}}
		h := captureStampLogs(t, jf, reviewDir)

		note, ok := h.note(slog.LevelWarn)
		require.True(t, ok)
		require.Contains(t, note, "entirely quoted", "the elided arm still names the condition that occurred")

		elided, ok := h.attr("elided")
		require.True(t, ok)
		require.Equal(t, int64(1), elided.Int64())

		skipped, ok := h.attr("salvage_skipped")
		require.True(t, ok, "a shortfall with TWO causes must report both, not just the one that won the arm")
		require.Equal(t, int64(1), skipped.Int64())
	})

	t.Run("matched arm keeps the salvage count at Debug", func(t *testing.T) {
		reviewDir := t.TempDir()
		writeSalvagedChunkStatus(t, reviewDir, "dax", nil)
		writeReview(t, reviewDir, "dax", "Maybe **`internal/other.go:7`** is the spot.")
		writeReview(t, reviewDir, "host", "## Findings\n\nThe check at `internal/other.go:7` accepts an unsigned token.\n")

		jf := []JSONFinding{{File: "internal/other.go", Line: 7, Problem: "unsigned", Reviewers: []string{"host"}}}
		h := captureStampLogs(t, jf, reviewDir)

		require.NotEmpty(t, jf[0].Justification, "precondition: the healthy sibling stamped")
		skipped, ok := h.attr("salvage_skipped")
		require.True(t, ok, "the skip is a cause of shortfall an operator can otherwise only find by hand")
		require.Equal(t, int64(1), skipped.Int64())
	})
}
