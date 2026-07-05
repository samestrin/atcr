package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runAuditReportIn(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(root)
	cmd := newAuditReportCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// writeAuditLedger lays down .atcr/audit.log.jsonl with the given records.
func writeAuditLedger(t *testing.T, root string, lines ...map[string]any) {
	t.Helper()
	dir := filepath.Join(root, ".atcr")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, l := range lines {
		require.NoError(t, enc.Encode(l))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "audit.log.jsonl"), buf.Bytes(), 0o644))
}

func TestAuditReportCmd_RendersReportForPR(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 7, 4, 9, 30, 0, 0, time.UTC).Format(time.RFC3339)
	writeAuditLedger(t, root,
		map[string]any{"ts": ts, "pr": 1234, "base": "basesha0001", "head": "headsha0002", "findings": map[string]int{"HIGH": 1}},
		map[string]any{"ts": ts, "pr": 5, "base": "otherbase", "head": "otherhead", "findings": map[string]int{"LOW": 1}},
	)
	out, err := runAuditReportIn(t, root, "--pr", "1234")
	require.NoError(t, err)
	assert.Contains(t, out, "# Audit Report")
	assert.Contains(t, out, "1234")
	assert.Contains(t, out, "basesha0001")
	assert.NotContains(t, out, "otherbase") // the PR-5 run must not leak into the PR-1234 report
}

func TestAuditReportCmd_UnknownPRExitsNonZero(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 7, 4, 9, 30, 0, 0, time.UTC).Format(time.RFC3339)
	writeAuditLedger(t, root,
		map[string]any{"ts": ts, "pr": 5, "base": "b", "head": "h"},
	)
	_, err := runAuditReportIn(t, root, "--pr", "9999")
	require.Error(t, err) // AC3: unknown --pr exits non-zero
	assert.NotEqual(t, 0, exitCode(err))
	assert.Contains(t, err.Error(), "9999")
}

func TestAuditReportCmd_AbsentLedgerExitsNonZero(t *testing.T) {
	root := t.TempDir()
	_, err := runAuditReportIn(t, root, "--pr", "1")
	require.Error(t, err) // no ledger at all => nothing for this PR => non-zero
	assert.Contains(t, err.Error(), "1")
}

func TestAuditReportCmd_MissingPRFlagIsError(t *testing.T) {
	root := t.TempDir()
	_, err := runAuditReportIn(t, root)
	require.Error(t, err) // --pr is required
}

func TestAuditReportCmd_ResolvesRepoRootFromSubdir(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 7, 4, 9, 30, 0, 0, time.UTC).Format(time.RFC3339)
	writeAuditLedger(t, root,
		map[string]any{"ts": ts, "pr": 1234, "base": "basesha0001", "head": "headsha0002"},
	)
	sub := filepath.Join(root, "nested", "dir")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	out, err := runAuditReportIn(t, sub, "--pr", "1234")
	require.NoError(t, err) // walks up to the .atcr ledger
	assert.Contains(t, out, "# Audit Report")
}
