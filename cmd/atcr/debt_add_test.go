package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/debt"
)

// emptyTDRepo writes a minimal authoritative README and returns its path plus
// the (not-yet-created) items dir.
func emptyTDRepo(t *testing.T) (readme, items string) {
	t.Helper()
	dir := t.TempDir()
	readme = filepath.Join(dir, "README.md")
	items = filepath.Join(dir, "items")
	require.NoError(t, os.WriteFile(readme, []byte("# Technical Debt\n\nstaging area\n"), 0o644))
	return readme, items
}

func TestDebtAdd_Wiring(t *testing.T) {
	cmd := newDebtCmd()
	var hasAdd bool
	for _, c := range cmd.Commands() {
		if c.Name() == "add" {
			hasAdd = true
		}
	}
	assert.True(t, hasAdd, "debt has an add subcommand")
}

func TestDebtAdd_FlagMode(t *testing.T) {
	readme, items := emptyTDRepo(t)
	_, err := runDebt(t, "add",
		"--readme", readme, "--items", items,
		"--date", "2026-07-03", "--label", "manual", "--source-type", "Sprint",
		"--severity", "HIGH", "--file", "internal/x/y.go:12",
		"--problem", "boom", "--fix", "guard it", "--category", "correctness",
		"--est", "30", "--group", "1", "--source", "manual",
	)
	require.NoError(t, err)

	got, err := os.ReadFile(readme)
	require.NoError(t, err)
	assert.Contains(t, string(got), "internal/x/y.go:12")

	recs, err := debt.Load(items)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "HIGH", recs[0].Severity)
	assert.Equal(t, "boom", recs[0].Problem)
}

func TestDebtAdd_MissingRequiredNonTTYIsUsageError(t *testing.T) {
	readme, items := emptyTDRepo(t)
	// Missing --severity/--file/etc. and stdin is not a TTY (bytes buffer).
	_, err := runDebt(t, "add", "--readme", readme, "--items", items, "--problem", "only this")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

func TestDebtAdd_InvalidSeverityIsError(t *testing.T) {
	readme, items := emptyTDRepo(t)
	_, err := runDebt(t, "add",
		"--readme", readme, "--items", items, "--date", "2026-07-03",
		"--severity", "URGENT", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c",
	)
	require.Error(t, err)
}

func TestPromptEntry_ReadsFieldsAndDefaults(t *testing.T) {
	// Answers, in order: date (default), source-type (default), label,
	// group (default), severity, file, problem, fix, category, est, status
	// (default), source (default). Empty lines take the default.
	answers := strings.Join([]string{
		"",            // date -> default
		"",            // source-type -> default
		"my-label",    // label
		"",            // group -> default
		"MEDIUM",      // severity
		"pkg/a.go:9",  // file
		"leaky",       // problem
		"close it",    // fix
		"correctness", // category
		"15",          // est
		"",            // status -> default
		"",            // source -> default
	}, "\n") + "\n"

	var out bytes.Buffer
	def := wizardDefaults{Date: "2026-07-03", SourceType: "Sprint", Label: "manual",
		Group: "U", Status: "open", Source: "manual", Est: 0}
	sec, it, err := promptEntry(strings.NewReader(answers), &out, def)
	require.NoError(t, err)

	assert.Equal(t, "2026-07-03", sec.Date)
	assert.Equal(t, "Sprint", sec.SourceType)
	assert.Equal(t, "my-label", sec.Label)
	assert.Equal(t, "MEDIUM", it.Severity)
	assert.Equal(t, "pkg/a.go:9", it.File)
	assert.Equal(t, "leaky", it.Problem)
	assert.Equal(t, 15, it.EstMinutes)
	assert.Equal(t, "open", it.Status)   // default
	assert.Equal(t, "manual", it.Source) // default
	assert.Equal(t, "U", it.Group)       // default
}

func TestPromptEntry_RequiredFieldMissingAtEOFErrors(t *testing.T) {
	// Provide date/source-type/label then EOF before severity (required).
	answers := "\n\nlbl\n\n" // date, source-type, label, group -> then EOF
	var out bytes.Buffer
	def := wizardDefaults{Date: "2026-07-03", SourceType: "Sprint", Label: "manual", Group: "U", Status: "open", Source: "manual"}
	_, _, err := promptEntry(strings.NewReader(answers), &out, def)
	require.Error(t, err)
}

func TestDebtAdd_InteractiveEndToEnd(t *testing.T) {
	readme, items := emptyTDRepo(t)

	// Force the interactive path without a real TTY.
	orig := debtStdinIsTTY
	debtStdinIsTTY = func(_ io.Reader) bool { return true }
	t.Cleanup(func() { debtStdinIsTTY = orig })

	answers := strings.Join([]string{
		"2026-07-03", // date
		"Sprint",     // source-type
		"wizard",     // label
		"2",          // group
		"LOW",        // severity
		"z.go:3",     // file
		"typo",       // problem
		"fix typo",   // fix
		"docs",       // category
		"5",          // est
		"open",       // status
		"manual",     // source
	}, "\n") + "\n"

	cmd := newDebtCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(answers))
	cmd.SetArgs([]string{"add", "--readme", readme, "--items", items})
	require.NoError(t, cmd.Execute())

	recs, err := debt.Load(items)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "LOW", recs[0].Severity)
	assert.Equal(t, "z.go:3", recs[0].File)
}
