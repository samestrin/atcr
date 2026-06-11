package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustFindings parses pipe-delimited rows (header prepended) into findings.
func mustFindings(t *testing.T, rows ...string) []stream.Finding {
	t.Helper()
	data := v1Header
	for _, r := range rows {
		data += r + "\n"
	}
	res, err := stream.ParseSource([]byte(data))
	require.NoError(t, err)
	return res.Findings
}

const v1Header = "# atcr-findings/v1\n"

// writeFindings writes a findings.txt (with header) at sourcesDir/relPath.
func writeFindings(t *testing.T, sourcesDir, relPath, body string) {
	t.Helper()
	full := filepath.Join(sourcesDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(v1Header+body), 0o644))
}

func sourceByName(sources []Source, name string) (Source, bool) {
	for _, s := range sources {
		if s.Name == name {
			return s, true
		}
	}
	return Source{}, false
}

func TestDiscover_LeafPreferenceSkipsMergedPoolFile(t *testing.T) {
	dir := t.TempDir()
	// Per-agent leaf files under pool.
	writeFindings(t, dir, "pool/raw/agent/greta/findings.txt",
		"CRITICAL|a.go:1|p|f|security|10|ev|greta\n")
	writeFindings(t, dir, "pool/raw/agent/kai/findings.txt",
		"HIGH|b.go:2|p|f|design|20|ev|kai\n")
	// Merged aggregate at the pool root — must NOT be re-counted.
	writeFindings(t, dir, "pool/findings.txt",
		"CRITICAL|a.go:1|p|f|security|10|ev|greta\nHIGH|b.go:2|p|f|design|20|ev|kai\n")

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	pool, ok := sourceByName(sources, "pool")
	require.True(t, ok)
	// Two leaf rows (greta + kai), NOT four (the merged file is ignored).
	require.Len(t, pool.Findings, 2)
	reviewers := []string{pool.Findings[0].Reviewer, pool.Findings[1].Reviewer}
	assert.ElementsMatch(t, []string{"greta", "kai"}, reviewers)
}

func TestDiscover_HostReadDirectly(t *testing.T) {
	dir := t.TempDir()
	writeFindings(t, dir, "host/findings.txt", "MEDIUM|c.go:3|p|f|test|5|ev|host\n")
	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	host, ok := sourceByName(sources, "host")
	require.True(t, ok)
	require.Len(t, host.Findings, 1)
	assert.Equal(t, "host", host.Findings[0].Reviewer)
}

func TestDiscover_ReconciledNeverAnInput(t *testing.T) {
	dir := t.TempDir()
	writeFindings(t, dir, "host/findings.txt", "LOW|x.go:1|p|f|style|2|ev|host\n")
	// A reconciled/findings.txt must be excluded.
	writeFindings(t, dir, "reconciled/findings.txt", "CRITICAL|x.go:1|p|f|sec|9|ev|host\n")
	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	_, ok := sourceByName(sources, "reconciled")
	assert.False(t, ok, "reconciled/ is output, never a source")
	assert.Len(t, sources, 1)
}

func TestDiscover_SourcesAllowlistFiltersImmediateChildren(t *testing.T) {
	dir := t.TempDir()
	writeFindings(t, dir, "pool/raw/agent/greta/findings.txt", "HIGH|a.go:1|p|f|sec|10|ev|greta\n")
	writeFindings(t, dir, "host/findings.txt", "HIGH|b.go:2|p|f|sec|10|ev|host\n")
	writeFindings(t, dir, "ci-extras/findings.txt", "HIGH|c.go:3|p|f|sec|10|ev|ci\n")

	sources, err := Discover(dir, []string{"pool", "host"})
	require.NoError(t, err)
	names := []string{}
	for _, s := range sources {
		names = append(names, s.Name)
	}
	assert.ElementsMatch(t, []string{"pool", "host"}, names, "ci-extras filtered out")
}

func TestDiscover_EmptyAllowlistIsOpenDiscovery(t *testing.T) {
	dir := t.TempDir()
	writeFindings(t, dir, "pool/raw/agent/greta/findings.txt", "HIGH|a.go:1|p|f|sec|10|ev|greta\n")
	writeFindings(t, dir, "host/findings.txt", "HIGH|b.go:2|p|f|sec|10|ev|host\n")
	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	assert.Len(t, sources, 2)
}

func TestDiscover_NormalizationPadsAndSkips(t *testing.T) {
	dir := t.TempDir()
	body := "" +
		"# a comment line\n" +
		"\n" +
		"This is prose, not a finding.\n" +
		"LOW|short.go:1|only a problem\n" + // short row → padded
		"HIGH|x.go:1|p|f|sec|10|ev|too|many|cols\n" // too many cols → skipped
	writeFindings(t, dir, "host/findings.txt", body)

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	host, _ := sourceByName(sources, "host")
	require.Len(t, host.Findings, 1, "the short row is padded and kept")
	assert.Equal(t, "only a problem", host.Findings[0].Problem)
	assert.Empty(t, host.Findings[0].Fix, "missing columns padded empty")
	assert.Len(t, host.Skipped, 1, "the over-long row is recorded as skipped")
}

func TestDiscover_BadHeaderFileSkippedNotFatal(t *testing.T) {
	dir := t.TempDir()
	// A dropped file with no version header must not abort discovery.
	full := filepath.Join(dir, "ci", "findings.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte("HIGH|a.go:1|p|f|sec|10|ev|ci\n"), 0o644))
	writeFindings(t, dir, "host/findings.txt", "HIGH|b.go:2|p|f|sec|10|ev|host\n")

	sources, err := Discover(dir, nil)
	require.NoError(t, err, "a bad-header file is skipped, not fatal")
	// ci source had its only file skipped → no findings, but host still works.
	host, ok := sourceByName(sources, "host")
	require.True(t, ok)
	assert.Len(t, host.Findings, 1)
}

func TestDiscover_SkippedFilesTrackedPerSource(t *testing.T) {
	dir := t.TempDir()
	// ci's only findings.txt has no version header → skipped on parse; the skip
	// must be recorded on the Source (not just warned to stderr) so the summary
	// can carry it (TD-020: skipped-source count in summary.json).
	badFile := filepath.Join(dir, "ci", "findings.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(badFile), 0o755))
	require.NoError(t, os.WriteFile(badFile, []byte("HIGH|a.go:1|p|f|sec|10|ev|ci\n"), 0o644))
	writeFindings(t, dir, "host/findings.txt", "HIGH|b.go:2|p|f|sec|10|ev|host\n")

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	ci, ok := sourceByName(sources, "ci")
	require.True(t, ok)
	require.Len(t, ci.SkippedFiles, 1, "bad-header file recorded as skipped")
	assert.Equal(t, badFile, ci.SkippedFiles[0])
	host, _ := sourceByName(sources, "host")
	assert.Empty(t, host.SkippedFiles)
}

func TestDiscover_SymlinkFindingsFileSkipped(t *testing.T) {
	dir := t.TempDir()
	// A secret file outside the review tree, with a valid header so it WOULD
	// parse if read — proving the skip is structural, not parse-driven.
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte(v1Header+"CRITICAL|secret.go:1|leaked|f|sec|99|ev|x\n"), 0o644))

	srcDir := filepath.Join(dir, "ci")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	if err := os.Symlink(outside, filepath.Join(srcDir, "findings.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	writeFindings(t, dir, "host/findings.txt", "HIGH|b.go:2|p|f|sec|10|ev|host\n")

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	// ci's only findings.txt is a symlink → skipped → ci is not a source.
	_, ok := sourceByName(sources, "ci")
	assert.False(t, ok, "a symlinked findings.txt must not be read")
	host, _ := sourceByName(sources, "host")
	assert.Len(t, host.Findings, 1)
}

func TestDiscover_MissingSourcesDirErrors(t *testing.T) {
	_, err := Discover(filepath.Join(t.TempDir(), "nope"), nil)
	assert.Error(t, err)
}

func TestAllFindings_FlattensInSourceOrder(t *testing.T) {
	sources := []Source{
		{Name: "host", Findings: mustFindings(t, "HIGH|a.go:1|p|f|sec|10|ev|host")},
		{Name: "pool", Findings: mustFindings(t, "LOW|b.go:2|p|f|style|2|ev|greta")},
	}
	all := AllFindings(sources)
	require.Len(t, all, 2)
	assert.Equal(t, "host", all[0].Reviewer)
	assert.Equal(t, "greta", all[1].Reviewer)
}
