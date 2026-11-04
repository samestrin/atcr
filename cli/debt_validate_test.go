package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

func TestDebtValidate_Grounding(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc Real() {}\n"), 0o644))
	mk := func(file string, line int, problem string) localdebt.Record {
		r := localdebt.Record{
			SchemaVersion: localdebt.SchemaVersion, RunID: "2026-10-01T10:00:00Z-x", Timestamp: "2026-10-01T10:00:00Z",
			Severity: "HIGH", File: file, Line: line, Problem: problem, Fix: "f", Category: "c", Confidence: "high",
		}
		r.StampID()
		return r
	}
	dir := writeLocalDebt(t,
		mk("a.go", 3, "(Real) is wrong"),
		mk("gone.go", 1, "p gone"),
		mk("a.go", 3, "`Missing` is wrong"),
		mk("", 0, "p nofile"),
		mk("a.go", 99, "p past end"),
		mk("a.go", 1, "no symbol named"),
		mk("../x.go", 1, "p escape"),
	)
	out, err := runDebt(t, "validate", "--dir", dir, "--root", root)
	require.NoError(t, err)
	var doc debtValidateDoc
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "not-requested"}, doc.Coherence)

	got := map[string]debtValidateItem{}
	for _, it := range doc.Items {
		got[it.File+":"+itoa(it.Line)+":"+it.Symbol] = it
	}
	assert.Equal(t, validateValid, got["a.go:3:Real"].Status)
	assert.Equal(t, validateFileMissing, got["gone.go:1:"].Status)
	assert.Equal(t, validateSymbolNotFound, got["a.go:3:Missing"].Status)
	assert.Equal(t, validateNoFile, got[":0:"].Status)
	assert.Equal(t, validateFileMissing, got["a.go:99:"].Status)
	assert.Contains(t, got["a.go:99:"].Reason, "past the end")
	assert.Equal(t, validateValid, got["a.go:1:"].Status)
	assert.Equal(t, validateFileMissing, got["../x.go:1:"].Status)
}

func debtValidateStore(t *testing.T) (dir, root string) {
	t.Helper()
	root = t.TempDir()
	// Line 3 has no trailing newline; line 0 is "unknown line".
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("a\nb\nc"), 0o644))
	mk := func(line int) localdebt.Record {
		r := localdebt.Record{
			SchemaVersion: localdebt.SchemaVersion, RunID: "2026-10-01T10:00:00Z-x", Timestamp: "2026-10-01T10:00:00Z",
			Severity: "HIGH", File: "a.go", Line: line, Problem: "p", Fix: "f", Category: "c", Confidence: "high",
		}
		r.StampID()
		return r
	}
	return writeLocalDebt(t, mk(0), mk(3), mk(4)), root
}

func TestDebtValidate_LineBoundaries(t *testing.T) {
	dir, root := debtValidateStore(t)
	out, err := runDebt(t, "validate", "--dir", dir, "--root", root)
	require.NoError(t, err)
	var doc debtValidateDoc
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	got := map[int]string{}
	for _, it := range doc.Items {
		got[it.Line] = it.Status
	}
	assert.Equal(t, validateValid, got[0], "line 0 means unknown, not checked")
	assert.Equal(t, validateValid, got[3], "last line without trailing newline counts")
	assert.Equal(t, validateFileMissing, got[4])
}

func TestDebtValidate_InvalidStatus(t *testing.T) {
	dir, root := debtValidateStore(t)
	code, _, _ := execCmdSplit(t, "debt", "validate", "--dir", dir, "--root", root, "--status", "bogus")
	assert.Equal(t, 2, code)
}

func TestDebtValidate_EmptyStoreDefaultRoot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "none")
	out, err := runDebt(t, "validate", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, `"items": []`)
}

func TestDebtValidate_AXI(t *testing.T) {
	dir, root := debtValidateStore(t)
	code, stdout, _ := execCmdSplit(t, "debt", "validate", "--dir", dir, "--root", root, "--axi")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "items[3]{id,file,line,symbol,status,reason}:")
	assert.Contains(t, stdout, "coherence:")
	assert.Contains(t, stdout, "status: skipped")
	assert.Contains(t, stdout, "reason: not-requested")
}

func TestDebtValidate_LegacyPipeRejected(t *testing.T) {
	dir, root := debtValidateStore(t)
	code, _, _ := execCmdSplit(t, "debt", "validate", "--dir", dir, "--root", root, "--axi", "--legacy-pipe")
	assert.Equal(t, 2, code)

	t.Setenv("ATCR_LEGACY_PIPE", "1")
	code, stdout, stderr := execCmdSplit(t, "debt", "validate", "--dir", dir, "--root", root, "--axi")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "items[")
	assert.NotContains(t, stderr, "deprecated")
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
