package payload

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultEscalationConfig_Thresholds(t *testing.T) {
	c := DefaultEscalationConfig()

	require.Equal(t, 0.5, c.ChurnRatio)
	require.Equal(t, 4, c.MinHunks)
	require.Equal(t, 10, c.HunkGapLines)
	require.Equal(t, 15, c.MinCyclomatic)
	require.Equal(t, 50, c.MaxFiles)
}

func TestResolveEscalationConfig_UnsetYieldsDefaults(t *testing.T) {
	require.Equal(t, DefaultEscalationConfig(), ResolveEscalationConfig(EscalationOverrides{}))
}

func TestResolveEscalationConfig_HonorsEachOverride(t *testing.T) {
	churn := 0.9
	hunks := 7
	gap := 3
	cyclo := 25
	maxFiles := 200

	got := ResolveEscalationConfig(EscalationOverrides{
		ChurnRatio:    &churn,
		MinHunks:      &hunks,
		HunkGapLines:  &gap,
		MinCyclomatic: &cyclo,
		MaxFiles:      &maxFiles,
	})

	require.Equal(t, EscalationConfig{
		ChurnRatio: 0.9, MinHunks: 7, HunkGapLines: 3, MinCyclomatic: 25, MaxFiles: 200,
	}, got)
}

func TestResolveEscalationConfig_ExplicitZeroDisablesSignal(t *testing.T) {
	// An explicit 0 must survive resolution rather than snapping back to the
	// default — that is the whole reason the override fields are pointers. A
	// zeroed threshold is how an operator turns one signal off.
	zeroI := 0
	zeroF := 0.0

	got := ResolveEscalationConfig(EscalationOverrides{
		ChurnRatio: &zeroF, MinHunks: &zeroI, HunkGapLines: &zeroI, MinCyclomatic: &zeroI, MaxFiles: &zeroI,
	})

	require.Equal(t, EscalationConfig{}, got)
}

func TestEscalationConfig_EnabledRespectsFileCap(t *testing.T) {
	c := DefaultEscalationConfig()

	require.True(t, c.Enabled(1))
	require.True(t, c.Enabled(50), "a change set exactly at the cap still runs")
	require.False(t, c.Enabled(51), "one file past the cap degrades the whole run")
	require.True(t, c.Enabled(0), "an empty change set is trivially under the cap")
}

func TestEscalationConfig_EnabledZeroMaxFilesDisablesFeature(t *testing.T) {
	// MaxFiles == 0 is the operator's kill switch: not "cap of zero files", but
	// "never run the escalation or skeleton passes at all".
	c := DefaultEscalationConfig()
	c.MaxFiles = 0

	require.False(t, c.Enabled(0))
	require.False(t, c.Enabled(1))
}

func TestEscalationConfig_EnabledNegativeCapDisablesFeature(t *testing.T) {
	// Validation rejects a negative cap at load, but Enabled must not depend on
	// validation having run — a hand-built config must fail closed, not open.
	c := EscalationConfig{MaxFiles: -1}

	require.False(t, c.Enabled(1))
}

func TestEscalate_OverlappingHunksFire(t *testing.T) {
	c := DefaultEscalationConfig()

	// Overlapping/abutting ranges yield a negative or zero gap. That is the
	// strongest adjacency evidence there is and must fire, not underflow.
	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 4, headLines: 1000,
		hunks:      []lineRange{{start: 100, end: 110}, {start: 105, end: 115}},
		cyclomatic: 1,
	})

	require.Equal(t, ModeBlocks, got)
}

func TestEscalate_ChurnAboveOneFires(t *testing.T) {
	c := DefaultEscalationConfig()

	// A file that shrank reports more changed lines than it now has: the ratio
	// exceeds 1 and must fire rather than being treated as out of range.
	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 300, headLines: 10,
		hunks:      []lineRange{{start: 1, end: 10}},
		cyclomatic: 1,
	})

	require.Equal(t, ModeBlocks, got)
}

func TestEscalate_SimpleChangeStaysInDiff(t *testing.T) {
	c := DefaultEscalationConfig()

	// One line changed in a 400-line file, single hunk, trivial complexity:
	// the token-budget case the epic exists to protect.
	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 1, headLines: 400,
		hunks:      []lineRange{{start: 40, end: 40}},
		cyclomatic: 3,
	})

	require.Equal(t, ModeDiff, got)
}

func TestEscalate_ChurnRatioFiresToBlocks(t *testing.T) {
	c := DefaultEscalationConfig()

	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 60, headLines: 100,
		hunks:      []lineRange{{start: 1, end: 60}},
		cyclomatic: 2,
	})

	require.Equal(t, ModeBlocks, got)
}

func TestEscalate_HunkCountFiresToBlocks(t *testing.T) {
	c := DefaultEscalationConfig()

	// Four well-separated hunks in a large file: low churn, no adjacency, but
	// the hunk count alone is the architectural-thrashing signal.
	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 8, headLines: 1000,
		hunks: []lineRange{
			{start: 10, end: 11}, {start: 200, end: 201},
			{start: 400, end: 401}, {start: 600, end: 601},
		},
		cyclomatic: 2,
	})

	require.Equal(t, ModeBlocks, got)
}

func TestEscalate_AdjacentHunksFireToBlocks(t *testing.T) {
	c := DefaultEscalationConfig()

	// Two hunks separated by 5 unchanged lines (< 10): a rewrite that churned
	// the same region across commits.
	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 4, headLines: 1000,
		hunks:      []lineRange{{start: 100, end: 101}, {start: 107, end: 108}},
		cyclomatic: 2,
	})

	require.Equal(t, ModeBlocks, got)
}

func TestEscalate_DistantHunksDoNotFire(t *testing.T) {
	c := DefaultEscalationConfig()

	// Gap of exactly 10 unchanged lines is NOT "< 10" — boundary must not fire.
	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 4, headLines: 1000,
		hunks:      []lineRange{{start: 100, end: 101}, {start: 112, end: 113}},
		cyclomatic: 2,
	})

	require.Equal(t, ModeDiff, got)
}

func TestEscalate_CyclomaticAloneFiresToBlocks(t *testing.T) {
	c := DefaultEscalationConfig()

	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 2, headLines: 1000,
		hunks:      []lineRange{{start: 10, end: 11}},
		cyclomatic: 15,
	})

	require.Equal(t, ModeBlocks, got)
}

func TestEscalate_BothSignalsFireToFiles(t *testing.T) {
	c := DefaultEscalationConfig()

	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 80, headLines: 100,
		hunks:      []lineRange{{start: 1, end: 80}},
		cyclomatic: 30,
	})

	require.Equal(t, ModeFiles, got)
}

func TestEscalate_BlocksOnlyEscalatesToFiles(t *testing.T) {
	c := DefaultEscalationConfig()

	// A single signal would target blocks, which the file already is: no change.
	require.Equal(t, ModeBlocks, c.escalate(ModeBlocks, fileSignals{
		changedLines: 60, headLines: 100,
		hunks:      []lineRange{{start: 1, end: 60}},
		cyclomatic: 2,
	}))

	// Both signals target files, which is above blocks: escalate.
	require.Equal(t, ModeFiles, c.escalate(ModeBlocks, fileSignals{
		changedLines: 60, headLines: 100,
		hunks:      []lineRange{{start: 1, end: 60}},
		cyclomatic: 30,
	}))
}

func TestEscalate_FilesNeverDeEscalates(t *testing.T) {
	c := DefaultEscalationConfig()

	require.Equal(t, ModeFiles, c.escalate(ModeFiles, fileSignals{
		changedLines: 1, headLines: 1000,
		hunks:      []lineRange{{start: 5, end: 5}},
		cyclomatic: 1,
	}))
}

func TestEscalate_ZeroHeadLinesDoesNotDivideByZero(t *testing.T) {
	c := DefaultEscalationConfig()

	// An empty or unreadable HEAD file yields headLines == 0. The churn ratio is
	// undefined there and must simply not fire (and must not panic).
	require.NotPanics(t, func() {
		got := c.escalate(ModeDiff, fileSignals{
			changedLines: 5, headLines: 0,
			hunks:      []lineRange{{start: 1, end: 5}},
			cyclomatic: 1,
		})
		require.Equal(t, ModeDiff, got)
	})
}

func TestEscalate_UnknownCyclomaticIsNotASignal(t *testing.T) {
	c := DefaultEscalationConfig()

	// cyclomatic == 0 means "not computed" (non-Go file, or parse failure), not
	// "zero complexity". It must never contribute to the both-signals verdict.
	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 80, headLines: 100,
		hunks:      []lineRange{{start: 1, end: 80}},
		cyclomatic: 0,
	})

	require.Equal(t, ModeBlocks, got, "churn alone must stop at blocks when complexity is unknown")
}

func TestEscalate_DisabledSignalsNeverFire(t *testing.T) {
	// A fully zeroed config is the operator's off switch: no file escalates.
	c := EscalationConfig{}

	got := c.escalate(ModeDiff, fileSignals{
		changedLines: 500, headLines: 500,
		hunks: []lineRange{
			{start: 1, end: 2}, {start: 4, end: 5}, {start: 7, end: 8}, {start: 10, end: 11},
		},
		cyclomatic: 900,
	})

	require.Equal(t, ModeDiff, got)
}
