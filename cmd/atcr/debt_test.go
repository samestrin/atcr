package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/tdmigrate"
)

// debtSampleShards mirrors the internal/debt corpus for command-level tests.
func debtSampleShards() []tdmigrate.Shard {
	return []tdmigrate.Shard{
		{
			Date: "2026-06-13", SourceType: tdmigrate.SourceTypeSprint, Label: "old-sprint",
			Items: []tdmigrate.Item{
				{Group: "1", Status: "open", Severity: "HIGH", File: "internal/autofix/apply.go:108",
					Problem: "clobber on create", Fix: "stat first", Category: "correctness", EstMinutes: 60, Source: "code-review"},
			},
		},
		{
			Date: "2026-06-26", SourceType: tdmigrate.SourceTypeReview, Label: "new-review",
			Items: []tdmigrate.Item{
				{Group: "2", Status: "open", Severity: "CRITICAL", File: "cmd/atcr/autofix.go:248",
					Problem: "remote leftover", Fix: "message op", Category: "docs", EstMinutes: 15, Source: "claude"},
				{Group: "2", Status: "deferred", Severity: "MEDIUM", File: "cmd/atcr/review.go",
					Problem: "exit gate surprise", Fix: "document it", Category: "docs", EstMinutes: 0, Source: "execute-sprint"},
			},
		},
	}
}

func writeItems(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	items := filepath.Join(dir, "items")
	_, err := tdmigrate.WriteShards(items, debtSampleShards())
	require.NoError(t, err)
	return items
}

func runDebt(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newDebtCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// Default to a non-TTY stdin so add's interactive path is not triggered by a
	// real terminal under the test runner; tests exercising the wizard set their
	// own reader and force debtStdinIsTTY.
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestDebt_CommandWiring(t *testing.T) {
	cmd := newDebtCmd()
	assert.Equal(t, "debt", cmd.Name())

	var hasList bool
	for _, c := range cmd.Commands() {
		if c.Name() == "list" {
			hasList = true
		}
	}
	assert.True(t, hasList, "debt has a list subcommand")

	root := newRootCmd()
	var registered bool
	for _, c := range root.Commands() {
		if c.Name() == "debt" {
			registered = true
		}
	}
	assert.True(t, registered, "debt is registered on the root command")
}

func TestDebtList_RendersTable(t *testing.T) {
	items := writeItems(t)
	out, err := runDebt(t, "list", "--items", items)
	require.NoError(t, err)

	assert.Contains(t, out, "SEVERITY")
	assert.Contains(t, out, "CRITICAL")
	assert.Contains(t, out, "HIGH")
	assert.Contains(t, out, "cmd/atcr/autofix.go:248")
	// Default sort is severity: CRITICAL row appears before HIGH row.
	assert.Less(t, strings.Index(out, "CRITICAL"), strings.Index(out, "HIGH"))
}

func TestDebtList_SeverityFilter(t *testing.T) {
	items := writeItems(t)
	out, err := runDebt(t, "list", "--items", items, "--severity", "high")
	require.NoError(t, err)
	assert.Contains(t, out, "HIGH")
	assert.NotContains(t, out, "CRITICAL")
}

func TestDebtList_ComponentFilter(t *testing.T) {
	items := writeItems(t)
	out, err := runDebt(t, "list", "--items", items, "--component", "cmd/atcr")
	require.NoError(t, err)
	assert.Contains(t, out, "cmd/atcr")
	assert.NotContains(t, out, "internal/autofix")
}

func TestDebtList_EmptyResultMessage(t *testing.T) {
	items := writeItems(t)
	out, err := runDebt(t, "list", "--items", items, "--severity", "LOW")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "no matching")
}

func TestDebtList_UnknownSortIsUsageError(t *testing.T) {
	items := writeItems(t)
	_, err := runDebt(t, "list", "--items", items, "--sort", "bogus")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

func TestDebtList_SyncRegeneratesFromREADME(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")
	items := filepath.Join(dir, "items")
	content := "# TD\n\n" +
		"### [2026-07-01] From Sprint: demo\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
		"|-------|---|----------|------|---------|-----|----------|-------------|--------|\n" +
		"| 1 | [ ] | HIGH | pkg/x.go:1 | boom | fixit | correctness | 15 | code-review |\n"
	require.NoError(t, os.WriteFile(readme, []byte(content), 0o644))

	out, err := runDebt(t, "list", "--items", items, "--readme", readme, "--sync")
	require.NoError(t, err)
	assert.Contains(t, out, "pkg/x.go:1")
}

func TestDebtList_SanitizesCellWhitespace(t *testing.T) {
	dir := t.TempDir()
	items := filepath.Join(dir, "items")
	shards := []tdmigrate.Shard{
		{
			Date: "2026-07-01", SourceType: tdmigrate.SourceTypeReview, Label: "evil",
			Items: []tdmigrate.Item{
				// Severity/Status are schema-validated by the shard loader, so the
				// whitespace attack arrives via the free-text fields instead.
				{Group: "1\n2", Status: "open", Severity: "HIGH", File: "pkg/x.go:1\npkg/y.go:2",
					Problem: "boom\tkaboom", Fix: "fix", Category: "cor\trectness", EstMinutes: 15, Source: "code-review"},
			},
		},
	}
	_, err := tdmigrate.WriteShards(items, shards)
	require.NoError(t, err)

	out, err := runDebt(t, "list", "--items", items)
	require.NoError(t, err)

	// Header plus exactly one data row: a literal newline or tab in any cell
	// must not tear the row into extra lines or split its columns.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 2, "each record must render as a single table row; got:\n%s", out)
	assert.Contains(t, lines[1], "HIGH")
	assert.Contains(t, lines[1], "1 2")
	assert.Contains(t, lines[1], "pkg/x.go:1 pkg/y.go:2")
	assert.Contains(t, lines[1], "cor rectness")
	assert.Contains(t, lines[1], "boom kaboom")
}

func TestTruncate_GuardNonPositiveN(t *testing.T) {
	assert.Equal(t, "", truncate("abc", 0), "n==0 must not panic")
	assert.Equal(t, "", truncate("abc", -1), "negative n must not panic")
}
