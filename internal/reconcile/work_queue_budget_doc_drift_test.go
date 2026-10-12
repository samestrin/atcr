package reconcile

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Drift guard for the baseline work queue's budgets (Epic 35.16.14 T6).
// docs/payload-modes.md publishes them as one table an operator sizes a run
// (and a resume's cost) from, and the README restates the two an operator meets
// first. The values live as constants in internal/fanout and
// internal/registry; the fanout ones are unexported, so they are read from the
// package source, the way metrics_doc_drift_test.go reads this package's
// counters. A constant whose declaration no longer matches fails the test
// rather than pinning the doc against nothing.

const (
	payloadModesDocPath = "../../docs/payload-modes.md"
	readmeDocPath       = "../../README.md"
	fanoutRequeuePath   = "../fanout/requeue.go"
	fanoutChunkerPath   = "../fanout/chunker.go"
)

// fanoutConst reads `name = <int>` or `name = <int> * time.<Unit>` from a
// const declaration in path.
func fanoutConst(t *testing.T, path, name string) time.Duration {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	re := regexp.MustCompile(`(?m)^\s*(?:const\s+)?` + regexp.QuoteMeta(name) + `\s*=\s*(\d+)(?:\s*\*\s*time\.(Second|Minute))?\s*$`)
	m := re.FindStringSubmatch(string(src))
	require.NotNilf(t, m, "%s: the declaration of %s drifted from the shape this guard reads", path, name)
	n, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	switch m[2] {
	case "Second":
		return time.Duration(n) * time.Second
	case "Minute":
		return time.Duration(n) * time.Minute
	}
	return time.Duration(n)
}

// queueDocValue returns the first backticked value of the work-queue table row
// whose first cell is label.
func queueDocValue(t *testing.T, doc, label string) string {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "| "+label+" |") {
			m := regexp.MustCompile("`([^`]+)`").FindStringSubmatch(line)
			require.NotNilf(t, m, "row %q states no backticked value: %s", label, line)
			return m[1]
		}
	}
	t.Fatalf("docs/payload-modes.md has no work-queue row %q", label)
	return ""
}

func durationText(d time.Duration) string {
	if d%time.Minute == 0 {
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return strconv.Itoa(int(d/time.Second)) + "s"
}

func TestWorkQueueBudgets_DocMatchesCode(t *testing.T) {
	data, err := os.ReadFile(payloadModesDocPath)
	require.NoError(t, err)
	doc := string(data)

	attempts := int(fanoutConst(t, fanoutRequeuePath, "unitMaxAttempts"))
	diffCeiling := int(fanoutConst(t, fanoutChunkerPath, "maxChunksPerAgent"))
	for label, want := range map[string]string{
		"Unit budget":        strconv.Itoa(registry.DefaultMaxUnits),
		"Attempt budget":     strconv.Itoa(attempts),
		"Backoff budget":     durationText(fanoutConst(t, fanoutRequeuePath, "unitBackoffBudget")),
		"Rate-limit backoff": durationText(fanoutConst(t, fanoutRequeuePath, "rateLimitBackoff")),
		"Outage backoff":     durationText(fanoutConst(t, fanoutRequeuePath, "outageBackoff")),
		"Diff chunk ceiling": strconv.Itoa(diffCeiling),
	} {
		assert.Equalf(t, want, queueDocValue(t, doc, label), "docs/payload-modes.md %q must state the code's default", label)
	}

	// The per-unit call ceiling the doc derives: outer sends × the per-call
	// attempts at the registry default (one try plus max_retries).
	calls := attempts * (registry.DefaultMaxRetries + 1)
	assert.Containsf(t, doc, strconv.Itoa(attempts)+" × "+strconv.Itoa(registry.DefaultMaxRetries+1)+" = "+strconv.Itoa(calls)+" calls",
		"docs/payload-modes.md must state the per-unit call ceiling as %d × %d = %d", attempts, registry.DefaultMaxRetries+1, calls)

	readme, err := os.ReadFile(readmeDocPath)
	require.NoError(t, err)
	assert.Containsf(t, string(readme), "**"+strconv.Itoa(registry.DefaultMaxUnits)+" bins**",
		"README must state the unit budget default (%d)", registry.DefaultMaxUnits)
	assert.Containsf(t, string(readme), "**"+strconv.Itoa(attempts)+" sends**",
		"README must state the attempt budget (%d)", attempts)
}
