package history

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTable_Empty(t *testing.T) {
	assert.Equal(t, "", RenderTable(nil))
}

func TestRenderTable_CountsBySeverityPerPackage(t *testing.T) {
	ts := time.Now()
	recs := []Record{
		{Timestamp: ts, Package: "internal/registry", Severity: "HIGH", ID: "1"},
		{Timestamp: ts, Package: "internal/registry", Severity: "HIGH", ID: "2"},
		{Timestamp: ts, Package: "internal/registry", Severity: "MEDIUM", ID: "3"},
		{Timestamp: ts, Package: "cmd/atcr", Severity: "low", ID: "4"}, // lowercase normalized
		{Timestamp: ts, Package: "cmd/atcr", Severity: "LOW", ID: "5"},
	}
	out := RenderTable(recs)

	// Header carries the canonical severity columns and a total.
	assert.Contains(t, out, "| Package |")
	assert.Contains(t, out, "CRITICAL")
	assert.Contains(t, out, "HIGH")
	assert.Contains(t, out, "MEDIUM")
	assert.Contains(t, out, "LOW")
	assert.Contains(t, out, "Total")

	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Packages are sorted: cmd/atcr before internal/registry.
	var cmdRow, regRow string
	for _, l := range lines {
		if strings.Contains(l, "cmd/atcr") {
			cmdRow = l
		}
		if strings.Contains(l, "internal/registry") {
			regRow = l
		}
	}
	require.NotEmpty(t, cmdRow)
	require.NotEmpty(t, regRow)
	assert.Less(t, indexOfLine(lines, cmdRow), indexOfLine(lines, regRow))

	// cmd/atcr: 2 LOW, total 2.
	assert.Regexp(t, `cmd/atcr.*\|\s*0\s*\|\s*0\s*\|\s*0\s*\|\s*2\s*\|\s*2\s*\|`, cmdRow)
	// internal/registry: 2 HIGH, 1 MEDIUM, total 3.
	assert.Regexp(t, `internal/registry.*\|\s*0\s*\|\s*2\s*\|\s*1\s*\|\s*0\s*\|\s*3\s*\|`, regRow)

	// A grand-total row summing every package.
	assert.Contains(t, out, "Total")
	var totalRow string
	for _, l := range lines {
		if strings.Contains(l, "**Total**") {
			totalRow = l
		}
	}
	require.NotEmpty(t, totalRow)
	assert.Regexp(t, `\|\s*0\s*\|\s*2\s*\|\s*1\s*\|\s*2\s*\|\s*5\s*\|`, totalRow)
}

func TestRenderTable_UnknownSeverityGetsColumn(t *testing.T) {
	ts := time.Now()
	recs := []Record{
		{Timestamp: ts, Package: "a", Severity: "INFO", ID: "1"},
		{Timestamp: ts, Package: "a", Severity: "HIGH", ID: "2"},
	}
	out := RenderTable(recs)
	assert.Contains(t, out, "INFO")
	// Total per package still counts the unknown severity.
	assert.Regexp(t, `\|\s*a\s*\|.*\|\s*2\s*\|`, out)
}

func indexOfLine(lines []string, target string) int {
	for i, l := range lines {
		if l == target {
			return i
		}
	}
	return -1
}
