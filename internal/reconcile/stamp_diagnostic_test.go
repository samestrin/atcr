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
