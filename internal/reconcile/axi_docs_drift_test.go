package reconcile

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These drift tests pin the AXI consumer docs against the code they describe:
// the go-axi version pinned in go.mod, the standard TOON findings header frozen
// by the report.axi golden, the pagination keys, and the legacy pipe fallback's
// flags and deprecation notice. The code sides are read as source files because
// this package cannot import cli or internal/report (both import it).

// findingsFormatAXISection returns only the "AXI TOON encoding" section of
// docs/findings-format.md — the atcr-findings/v1 pipe streams earlier in the
// file legitimately keep their pipe grammar.
func findingsFormatAXISection(t *testing.T) string {
	t.Helper()
	doc := readRepoFile(t, "../../docs/findings-format.md")
	start := strings.Index(doc, "\n## AXI TOON encoding")
	require.GreaterOrEqual(t, start, 0, "docs/findings-format.md must keep its AXI TOON encoding section")
	rest := doc[start+1:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func mustMatch(t *testing.T, src, pattern, what string) string {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(src)
	require.NotNilf(t, m, "could not find %s", what)
	return m[1]
}

func axiDocs(t *testing.T) map[string]string {
	return map[string]string{
		"docs/agentic-consumption.md":                 readRepoFile(t, "../../docs/agentic-consumption.md"),
		"docs/findings-format.md (AXI TOON encoding)": findingsFormatAXISection(t),
	}
}

// TestAXIDocs_GoAxiPinMatchesGoMod fails when go.mod bumps go-axi without the
// docs naming the new version (or the reverse).
func TestAXIDocs_GoAxiPinMatchesGoMod(t *testing.T) {
	pin := mustMatch(t, readRepoFile(t, "../../go.mod"), `(?m)^\s*github\.com/samestrin/go-axi (v\S+)`, "go-axi require in go.mod")
	for name, doc := range axiDocs(t) {
		require.Containsf(t, doc, "go-axi` "+pin, "%s must name the pinned go-axi version %s", name, pin)
	}
}

// TestAXIDocs_StandardHeaderMatchesGolden pins each doc's example findings
// header to the standard TOON header frozen by the report.axi golden.
func TestAXIDocs_StandardHeaderMatchesGolden(t *testing.T) {
	golden := readRepoFile(t, "../../internal/report/testdata/report.axi")
	header := strings.SplitN(golden, "\n", 2)[0]
	require.Truef(t, strings.HasPrefix(header, "findings[2]{"), "golden header must be standard TOON: %q", header)
	for name, doc := range axiDocs(t) {
		require.Containsf(t, doc, header+"\n", "%s must show the standard TOON header exactly as emitted", name)
	}
}

// TestAXIDocs_PaginationKeys pins the `total` / `truncated` sibling keys in
// the consumer docs.
func TestAXIDocs_PaginationKeys(t *testing.T) {
	doc := readRepoFile(t, "../../docs/agentic-consumption.md")
	for _, key := range []string{"\ntotal: 2\ntruncated: false\n", "`total: <int>`", "`truncated: <bool>`"} {
		require.Containsf(t, doc, key, "docs/agentic-consumption.md must document %q", key)
	}
	require.Contains(t, findingsFormatAXISection(t), "`total`")
}

// TestAXIDocs_LegacyPipeFallback pins the fallback surface: the format name from
// internal/report, the flag and env var from cli, and the exact deprecation
// notice from cli/axi.go.
func TestAXIDocs_LegacyPipeFallback(t *testing.T) {
	format := mustMatch(t, readRepoFile(t, "../../internal/report/render.go"), `FormatPipe = "([^"]+)"`, "FormatPipe constant")
	notice := mustMatch(t, readRepoFile(t, "../../cli/axi.go"), `const legacyPipeDeprecation = "([^"]+)"`, "legacyPipeDeprecation constant")
	require.Contains(t, readRepoFile(t, "../../cli/review.go"), `Bool("legacy-pipe"`, "review registers --legacy-pipe")
	require.Contains(t, readRepoFile(t, "../../cli/main.go"), `Bool("legacy-pipe"`, "the root command registers --legacy-pipe")
	require.Contains(t, readRepoFile(t, "../../cli/axi.go"), `os.LookupEnv("ATCR_LEGACY_PIPE")`, "cli reads ATCR_LEGACY_PIPE")
	for name, doc := range axiDocs(t) {
		for _, want := range []string{"--format " + format, "--legacy-pipe", "ATCR_LEGACY_PIPE=1", notice} {
			require.Containsf(t, doc, want, "%s must document %q", name, want)
		}
	}
}
