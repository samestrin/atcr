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

// validateRows runs debt validate over recs and returns the items by Problem.
func validateRows(t *testing.T, root string, recs ...localdebt.Record) map[string]debtValidateItem {
	t.Helper()
	for i := range recs {
		recs[i].SchemaVersion, recs[i].RunID, recs[i].Timestamp = localdebt.SchemaVersion, "2026-10-01T10:00:00Z-x", "2026-10-01T10:00:00Z"
		recs[i].Severity, recs[i].Fix, recs[i].Category, recs[i].Confidence = "HIGH", "f", "c", "high"
		recs[i].StampID()
	}
	out, err := runDebt(t, "validate", "--dir", writeLocalDebt(t, recs...), "--root", root)
	require.NoError(t, err)
	var doc debtValidateDoc
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	got := map[string]debtValidateItem{}
	for _, it := range doc.Items {
		for _, r := range recs {
			if r.ID == it.ID {
				got[r.Problem] = it
			}
		}
	}
	require.Len(t, got, len(recs))
	return got
}

// Row ff0159d978cb59ad: the first backticked identifier need not be the row's
// symbol. Without an anchor any backticked identifier in the file grounds it.
func TestDebtValidate_SymbolAnyBacktickedIdentifier(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc Real() {}\n"), 0o644))
	got := validateRows(t, root,
		localdebt.Record{File: "a.go", Line: 3, Problem: "`os.Exit` style: `Missing` is called from `Real`"},
		localdebt.Record{File: "a.go", Line: 3, Problem: "`Missing` and `Other` are wrong"},
		localdebt.Record{File: "a.go", Line: 3, Problem: "(Missing) calls `Real`"},
		localdebt.Record{File: "gone.go", Line: 1, Problem: "`Missing` then `Real`"},
	)

	it := got["`os.Exit` style: `Missing` is called from `Real`"]
	assert.Equal(t, validateValid, it.Status)
	assert.Equal(t, "Real", it.Symbol, "the symbol reported is the one found")

	it = got["`Missing` and `Other` are wrong"]
	assert.Equal(t, validateSymbolNotFound, it.Status)
	assert.Equal(t, "Missing", it.Symbol, "none found: report the first")

	it = got["(Missing) calls `Real`"]
	assert.Equal(t, validateSymbolNotFound, it.Status, "the anchor is the row's symbol; backticks do not override it")
	assert.Equal(t, "Missing", it.Symbol)

	it = got["`Missing` then `Real`"]
	assert.Equal(t, validateFileMissing, it.Status)
	assert.Equal(t, "Missing", it.Symbol)
}

// Row 0663a2da4f1da0e0: a symlink inside the root that resolves outside it is
// outside the repository, whether the link is the file or a parent directory.
func TestDebtValidate_SymlinkOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root, outside := filepath.Join(base, "repo"), filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "in"), 0o755))
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.go"), []byte("package s\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "in", "real.go"), []byte("package r\n"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(root, "file-link.go")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "dir-link")))
	require.NoError(t, os.Symlink(filepath.Join("in", "real.go"), filepath.Join(root, "inside-link.go")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "absent.go"), filepath.Join(root, "dangling.go")))

	got := validateRows(t, root,
		localdebt.Record{File: "file-link.go", Line: 1, Problem: "file link"},
		localdebt.Record{File: "dir-link/secret.go", Line: 1, Problem: "dir link"},
		localdebt.Record{File: "inside-link.go", Line: 1, Problem: "inside link"},
		localdebt.Record{File: "dangling.go", Line: 1, Problem: "dangling"},
	)
	for _, p := range []string{"file link", "dir link"} {
		assert.Equal(t, validateFileMissing, got[p].Status, p)
		assert.Equal(t, "path is outside the repository", got[p].Reason, p)
	}
	assert.Equal(t, validateValid, got["inside link"].Status, "a link that stays inside the root is fine")
	assert.Equal(t, validateFileMissing, got["dangling"].Status)
	assert.Equal(t, "file not found", got["dangling"].Reason)
}

// The root itself may sit behind a symlink (macOS /tmp is /private/tmp).
func TestDebtValidate_SymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	require.NoError(t, os.MkdirAll(real, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(real, "a.go"), []byte("package a\n"), 0o644))
	link := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(real, link))
	got := validateRows(t, link, localdebt.Record{File: "a.go", Line: 1, Problem: "p"})
	assert.Equal(t, validateValid, got["p"].Status)
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
