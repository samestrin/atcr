package cli

import (
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/telemetry"
)

// --- Epic 36.1 T5: the docs name the same flags and keys the code ships ---

const (
	technicalDebtDocForRecluster = "../docs/technical-debt.md"
	telemetryDocForRecluster     = "../docs/telemetry.md"
)

// TestDebtRecluster_DocumentsEveryFlagAndKind locks the operator doc to the
// command's own flag set and kind vocabulary: a flag or kind added to
// `atcr debt recluster` without documenting it (or renamed in only one place)
// fails here.
func TestDebtRecluster_DocumentsEveryFlagAndKind(t *testing.T) {
	b, err := os.ReadFile(technicalDebtDocForRecluster)
	require.NoError(t, err)
	doc := string(b)

	assert.Contains(t, doc, "### `atcr debt recluster`",
		"docs/technical-debt.md must have a section for the subcommand")

	cmd := newDebtReclusterCmd()
	for _, name := range []string{"kind", "with", "reason", "store"} {
		flag := cmd.Flags().Lookup(name)
		require.NotNil(t, flag, "the command is expected to keep its --%s flag", name)
		assert.Contains(t, doc, "`--"+name+"`",
			"docs/technical-debt.md names no --%s flag, but the command still takes it", name)
	}

	for _, kind := range []string{
		localdebt.FeedbackWrongMerge,
		localdebt.FeedbackWrongSplit,
		localdebt.FeedbackWrongCategory,
	} {
		assert.Contains(t, doc, "`"+kind+"`",
			"docs/technical-debt.md does not name the %q kind the command accepts", kind)
	}
}

// TestTelemetryDoc_DocumentsEveryQualitySignalKey locks docs/telemetry.md to
// the telemetry.QualitySignal wire schema: the backend validates the payload by
// strict key-set equality, so a documented set that drifts from the shipped set
// tells operators the wrong contract.
func TestTelemetryDoc_DocumentsEveryQualitySignalKey(t *testing.T) {
	b, err := os.ReadFile(telemetryDocForRecluster)
	require.NoError(t, err)
	doc := string(b)

	typ := reflect.TypeOf(telemetry.QualitySignal{})
	for i := 0; i < typ.NumField(); i++ {
		key := typ.Field(i).Tag.Get("json")
		require.NotEmpty(t, key, "every QualitySignal field must carry a json tag")
		assert.Contains(t, doc, "`"+key+"`",
			"docs/telemetry.md does not document the %q payload key the CLI sends", key)
	}
}
