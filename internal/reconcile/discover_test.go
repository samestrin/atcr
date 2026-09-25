package reconcile

import (
	"os"
	"path/filepath"
	"strings"
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

// writeToon writes a v2 findings.toon at sourcesDir/relPath.
func writeToon(t *testing.T, sourcesDir, relPath string, findings []stream.Finding) {
	t.Helper()
	var b strings.Builder
	require.NoError(t, stream.WriteSourceV2(&b, findings))
	full := filepath.Join(sourcesDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(b.String()), 0o644))
}

var lossless = stream.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x | y", Fix: "a | b", Category: "correctness", EstMinutes: 5, Evidence: "l1\nl2", Reviewer: "greta"}

// AC 04-02 Scenario 1: one leaf holding both files yields the .toon findings
// once, never the .txt as well.
func TestDiscover_LeafWithBothFilesReadsToonOnce(t *testing.T) {
	dir := t.TempDir()
	writeToon(t, dir, "pool/raw/agent/greta/findings.toon", []stream.Finding{lossless})
	writeFindings(t, dir, "pool/raw/agent/greta/findings.txt", "LOW|b.go:2|from txt|f|style|1|e|greta\n")

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	pool, ok := sourceByName(sources, "pool")
	require.True(t, ok)
	assert.Equal(t, []stream.Finding{lossless}, pool.Findings)
}

func TestDiscover_ToonOnlyLeaf(t *testing.T) {
	dir := t.TempDir()
	writeToon(t, dir, "pool/raw/agent/greta/findings.toon", []stream.Finding{lossless})

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	pool, ok := sourceByName(sources, "pool")
	require.True(t, ok, "a directory holding only findings.toon is a leaf")
	assert.Equal(t, []stream.Finding{lossless}, pool.Findings)
}

// Nesting is format-blind: a parent .txt above a child .toon is not a leaf.
func TestDiscover_NestedMixedFormatsDeepestWins(t *testing.T) {
	dir := t.TempDir()
	writeFindings(t, dir, "pool/findings.txt", "LOW|b.go:2|merged|f|style|1|e|greta\n")
	writeToon(t, dir, "pool/raw/agent/greta/findings.toon", []stream.Finding{lossless})

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	pool, _ := sourceByName(sources, "pool")
	assert.Equal(t, []stream.Finding{lossless}, pool.Findings)
}

// A corrupt .toon is skipped and reported; the sibling .txt is never read.
func TestDiscover_CorruptToonSkippedNotMaskedByTxt(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "pool", "raw", "agent", "greta", "findings.toon")
	require.NoError(t, os.MkdirAll(filepath.Dir(bad), 0o755))
	require.NoError(t, os.WriteFile(bad, []byte(stream.VersionV2+"\nnot a table\n"), 0o644))
	writeFindings(t, dir, "pool/raw/agent/greta/findings.txt", "LOW|b.go:2|from txt|f|style|1|e|greta\n")

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	pool, ok := sourceByName(sources, "pool")
	require.True(t, ok)
	assert.Empty(t, pool.Findings, "the .txt sibling must not stand in for a corrupt .toon")
	assert.Equal(t, []string{bad}, pool.SkippedFiles)
}

// A non-regular findings.toon is not a leaf marker; a regular .txt beside it
// still makes the directory a leaf and is the file read.
func TestDiscover_SymlinkToonIgnoredTxtRead(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.toon")
	var b strings.Builder
	require.NoError(t, stream.WriteSourceV2(&b, []stream.Finding{lossless}))
	require.NoError(t, os.WriteFile(outside, []byte(b.String()), 0o644))

	writeFindings(t, dir, "ci/findings.txt", "LOW|b.go:2|from txt|f|style|1|e|ci\n")
	if err := os.Symlink(outside, filepath.Join(dir, "ci", "findings.toon")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lone"), 0o755))
	if err := os.Symlink(outside, filepath.Join(dir, "lone", "findings.toon")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	ci, ok := sourceByName(sources, "ci")
	require.True(t, ok)
	require.Len(t, ci.Findings, 1)
	assert.Equal(t, "from txt", ci.Findings[0].Problem)
	_, ok = sourceByName(sources, "lone")
	assert.False(t, ok, "a symlinked findings.toon alone is not a source")
}

// AC 04-04: a host-review-shaped .txt-only source keeps reconciling beside a
// dual-written pool, each resolved on its own.
func TestDiscover_TxtOnlyHostBesideDualWrittenPool(t *testing.T) {
	dir := t.TempDir()
	hostBody := "HIGH|h.go:3|host finding|f|security|10|ev|host\n"
	writeFindings(t, dir, "host/findings.txt", hostBody)
	writeToon(t, dir, "pool/raw/agent/greta/findings.toon", []stream.Finding{lossless})
	writeFindings(t, dir, "pool/raw/agent/greta/findings.txt", "LOW|b.go:2|from txt|f|style|1|e|greta\n")

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	host, _ := sourceByName(sources, "host")
	assert.Equal(t, mustFindings(t, strings.TrimSuffix(hostBody, "\n")), host.Findings)
	pool, _ := sourceByName(sources, "pool")
	assert.Equal(t, []stream.Finding{lossless}, pool.Findings)
}

// A leaf whose file cannot be selected after the walk still reaches
// SkippedFiles, so summary.json's skipped_sources reports it.
func TestDiscover_UnselectableLeafRecordedInSkippedFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	writeFindings(t, dir, "pool/raw/agent/greta/findings.txt", "LOW|b.go:2|p|f|style|1|e|greta\n")
	leafDir := filepath.Join(dir, "pool", "raw", "agent", "greta")
	// Search permission off: the walk still lists the entry, but Lstat/Stat
	// inside the directory fail, so selection errors.
	require.NoError(t, os.Chmod(leafDir, 0o600))
	t.Cleanup(func() { _ = os.Chmod(leafDir, 0o755) })

	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	pool, ok := sourceByName(sources, "pool")
	require.True(t, ok, "the source must still be reported, not silently dropped")
	// The .toon probe failed with a permission error, not "absent", so the
	// skipped path names the .toon: a failed .toon never becomes a .txt read.
	assert.Equal(t, []string{filepath.Join(leafDir, "findings.toon")}, pool.SkippedFiles)
}

func TestDiscover_V1HeaderInToonIsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFindings(t, dir, "pool/raw/agent/greta/findings.toon", "LOW|b.go:2|p|f|style|1|e|greta\n")
	sources, err := Discover(dir, nil)
	require.NoError(t, err)
	pool, ok := sourceByName(sources, "pool")
	require.True(t, ok)
	assert.Empty(t, pool.Findings)
	assert.Len(t, pool.SkippedFiles, 1)
}
