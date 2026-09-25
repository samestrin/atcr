package stream

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// poolLossless has every field v1 damages: pipes, a quote, and a multi-line
// EVIDENCE.
var poolLossless = Finding{Severity: "HIGH", File: "flags.go", Line: 7, Problem: `mode "x | y"`, Fix: "a | b", Category: "correctness", EstMinutes: 5, Evidence: "l1\n    l2", Reviewer: "greta"}

func v2Of(t *testing.T, findings []Finding) string {
	t.Helper()
	var b strings.Builder
	require.NoError(t, WriteSourceV2(&b, findings))
	return b.String()
}

func v1Of(t *testing.T, findings []Finding) string {
	t.Helper()
	var b strings.Builder
	require.NoError(t, WriteSource(&b, findings))
	return b.String()
}

func TestReadPoolFindings_PrefersToonLossless(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "findings.toon", v2Of(t, []Finding{poolLossless}))
	writeFile(t, dir, "findings.txt", v1Of(t, []Finding{poolLossless}))

	res, err := ReadPoolFindings(dir)
	require.NoError(t, err)
	assert.Equal(t, []Finding{poolLossless}, res.Findings, "the .toon is read, so no field is flattened")
}

// A .txt-only pool reads exactly as ParseSource reads it today (AC 07-03).
func TestReadPoolFindings_TxtOnlyMatchesGolden(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "per_source.txt"))
	require.NoError(t, err)
	dir := t.TempDir()
	writeFile(t, dir, "findings.txt", string(golden))

	want, err := ParseSource(golden)
	require.NoError(t, err)
	got, err := ReadPoolFindings(dir)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.NotEmpty(t, got.Skipped, "the golden fixture's skipped row survives the helper")
}

func TestReadPoolFindings_ToonOnlyAndEnvelope(t *testing.T) {
	t.Run("toon only", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "findings.toon", v2Of(t, []Finding{poolLossless}))
		res, err := ReadPoolFindings(dir)
		require.NoError(t, err)
		assert.Equal(t, []Finding{poolLossless}, res.Findings)
	})
	t.Run("json envelope", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "findings.toon", VersionV2+"\n"+
			`{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"HIGH","file_line":"flags.go:7","problem":"mode \"x | y\"","fix":"a | b","category":"correctness","est_minutes":5,"evidence":"l1\n    l2","reviewer":"greta"}]}}`+"\n")
		res, err := ReadPoolFindings(dir)
		require.NoError(t, err)
		assert.Equal(t, []Finding{poolLossless}, res.Findings)
	})
}

func TestReadPoolFindings_NeitherFileIsNotExist(t *testing.T) {
	for name, dir := range map[string]string{
		"empty dir":   t.TempDir(),
		"missing dir": filepath.Join(t.TempDir(), "sources", "pool"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ReadPoolFindings(dir)
			require.Error(t, err)
			assert.True(t, errors.Is(err, fs.ErrNotExist))
			var pe *FindingsParseError
			assert.False(t, errors.As(err, &pe))
		})
	}
}

// A corrupt .toon is a parse error and never falls back to the valid .txt
// (AC 07-02): falling back would hide a v2 writer bug behind lossy data.
func TestReadPoolFindings_CorruptToonNeverFallsBack(t *testing.T) {
	for name, body := range map[string]string{
		"bad table":          VersionV2 + "\nnot a table\n",
		"truncated envelope": VersionV2 + "\n" + `{"axi_format":"json","data":{"findings":[`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "findings.toon", body)
			writeFile(t, dir, "findings.txt", v1Of(t, []Finding{poolLossless}))

			res, err := ReadPoolFindings(dir)
			require.Error(t, err)
			var pe *FindingsParseError
			require.True(t, errors.As(err, &pe), "a parse failure is a *FindingsParseError, got %T", err)
			assert.Equal(t, filepath.Join(dir, "findings.toon"), pe.Path)
			assert.Empty(t, res.Findings)
			assert.False(t, errors.Is(err, fs.ErrNotExist))
		})
	}
}

// A read failure on the selected .toon is returned as the OS error, not a parse
// error and not "missing", and the .txt is never read.
func TestReadPoolFindings_UnreadableToonIsReadError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-0 file")
	}
	dir := t.TempDir()
	writeFile(t, dir, "findings.toon", v2Of(t, []Finding{poolLossless}))
	writeFile(t, dir, "findings.txt", v1Of(t, []Finding{poolLossless}))
	toon := filepath.Join(dir, "findings.toon")
	require.NoError(t, os.Chmod(toon, 0o000))
	t.Cleanup(func() { _ = os.Chmod(toon, 0o644) })

	_, err := ReadPoolFindings(dir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrPermission), "got %v", err)
	var pe *FindingsParseError
	assert.False(t, errors.As(err, &pe))
}

// The parse error's message is the parser's own, so callers that wrap it keep
// today's text ("parsing pool findings: <parser error>").
func TestFindingsParseError_MessageIsTheParserError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "findings.txt", "garbage header\n")
	_, want := ParseSource([]byte("garbage header\n"))
	require.Error(t, want)

	_, err := ReadPoolFindings(dir)
	require.Error(t, err)
	assert.Equal(t, want.Error(), err.Error())
	assert.True(t, errors.Is(err, ErrMissingHeader), "the wrapper unwraps to the parser's sentinel")
}
