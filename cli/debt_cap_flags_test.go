package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// `atcr debt` carries two differently-named list caps that both default to 10:
// `debt resolve --max` and `debt dashboard --top`. They are genuinely distinct —
// an action cap versus a ranked-display cutoff, and their zero values mean
// OPPOSITE things (`--max 0` = no cap, `--top 0` = suppress the list) — but
// neither help string said so, leaving the split reading as drift rather than
// design. The distinction is only useful if it is visible at `--help`, which is
// the only place a user looks before typing the flag.
func TestDebtListCapFlagsDocumentTheirDistinction(t *testing.T) {
	t.Run("dashboard --top", func(t *testing.T) {
		_, out := execCmdCapture(t, "debt", "dashboard", "--help")
		assert.Contains(t, out, "--top", "help must document --top")
		assert.Contains(t, out, "0 suppresses",
			"--top help must state that 0 suppresses the list (the opposite of --max 0)")
		assert.Contains(t, out, "--max",
			"--top help must name --max so the two caps are not read as drift")
	})

	t.Run("resolve --max", func(t *testing.T) {
		_, out := execCmdCapture(t, "debt", "resolve", "--help")
		assert.Contains(t, out, "--max", "help must document --max")
		assert.Contains(t, out, "0 = no cap",
			"--max help must state that 0 means no cap (the opposite of --top 0)")
		assert.Contains(t, out, "--top",
			"--max help must name --top so the two caps are not read as drift")
	})
}
