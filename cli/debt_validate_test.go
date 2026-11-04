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

	axi, err := runDebt(t, "validate", "--dir", dir, "--root", root, "--legacy-pipe")
	assert.Error(t, err)
	_ = axi
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
