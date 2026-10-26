package personas

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBase_PinsOutputContract fails if the lines that small local models rely
// on for the reply shape are removed from the shared template.
func TestBase_PinsOutputContract(t *testing.T) {
	base, err := Base()
	require.NoError(t, err)

	for _, want := range []string{
		"inside a single ```json code fence",
		`"severity", "file_line", "problem", "fix", "category", "est_minutes", "evidence"`,
		"file_line is FILE:LINE",
		"reply with exactly this line and nothing else: NO FINDINGS",
		"Small-model checklist",
	} {
		require.Truef(t, strings.Contains(base, want), "_base.md lost output-contract line %q", want)
	}
}
