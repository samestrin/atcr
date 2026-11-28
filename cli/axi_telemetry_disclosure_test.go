package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// telemetryDisclosureElements are the disclosure facts every one of the three
// documents (catalog entry exception text, audit Telemetry disclosure section,
// README Limits copy) must state so an agent or human reading any single one
// learns the whole opt-out story (epic 165 T4, plan AC4):
//   - the ping is on by default,
//   - the env opt-out (ATCR_TELEMETRY=0),
//   - the config opt-out (telemetry: false),
//   - the AXI home view reports the resolved state (telemetry: on|off).
var telemetryDisclosureElements = []string{
	"on by default",
	"ATCR_TELEMETRY=0",
	"telemetry: false",
	"telemetry: on|off",
}

// telemetryDisclosureGaps returns the disclosure elements missing from text.
// Backticks and quotes are stripped first (the audit and README backtick code
// spans where the catalog entry writes them bare), mirroring exceptionWords'
// normalization so the three documents' different markup cannot mask a gap.
func telemetryDisclosureGaps(text string) []string {
	normalized := strings.NewReplacer("`", "", "'", "", `"`, "").Replace(text)
	var gaps []string
	for _, want := range telemetryDisclosureElements {
		if !strings.Contains(normalized, want) {
			gaps = append(gaps, want)
		}
	}
	return gaps
}

// TestTelemetryDisclosureGaps is the both-ways test for the disclosure checker:
// a complete disclosure passes, and removing any single element is caught — so
// the guard can never flag a complete disclosure nor pass an incomplete one.
func TestTelemetryDisclosureGaps(t *testing.T) {
	good := "The anonymous usage ping is on by default; opt out with ATCR_TELEMETRY=0 " +
		"or config telemetry: false; the AXI home view reports telemetry: on|off."
	require.Empty(t, telemetryDisclosureGaps(good), "a complete disclosure has no gaps")

	for _, el := range telemetryDisclosureElements {
		bad := strings.Replace(good, el, "", 1)
		gaps := telemetryDisclosureGaps(bad)
		assert.Contains(t, gaps, el, "removing %q must be caught", el)
	}

	// Markup differences are not gaps: the same facts backticked pass too.
	backticked := "The ping is `on by default`; opt out with `ATCR_TELEMETRY=0` or " +
		"config `telemetry: false`; the home view reports `telemetry: on|off`."
	assert.Empty(t, telemetryDisclosureGaps(backticked))
}

// TestAXITelemetryDisclosure_EntryAuditReadmeAgree is the epic 165 T4 agreement
// guard: the catalog entry's exception/disclosure text, the audit's Telemetry
// disclosure section and the README Limits copy each state the default-on
// anonymous usage ping, both opt-outs and the home-view field. A disclosure
// dropped from any one of the three turns CI red instead of shipping documents
// that disagree about the ping.
func TestAXITelemetryDisclosure_EntryAuditReadmeAgree(t *testing.T) {
	root := repoRootDir(t)
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, rel))
		require.NoError(t, err)
		return string(b)
	}

	// The catalog entry: the exception scalar carries the disclosure text.
	var entries []struct {
		Admission struct {
			Exception string `yaml:"exception"`
		} `yaml:"admission"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(read("docs/axi-catalog-entry.yaml")), &entries))
	require.Len(t, entries, 1)
	for _, gap := range telemetryDisclosureGaps(entries[0].Admission.Exception) {
		t.Errorf("docs/axi-catalog-entry.yaml exception text omits the telemetry disclosure element %q", gap)
	}

	// The audit: a dedicated Telemetry disclosure section.
	auditSection, ok := markdownSection(read("docs/axi-audit.md"), regexp.MustCompile(`^Telemetry disclosure$`))
	require.True(t, ok, "docs/axi-audit.md has no `## Telemetry disclosure` section (epic 165 T4)")
	for _, gap := range telemetryDisclosureGaps(auditSection) {
		t.Errorf("docs/axi-audit.md Telemetry disclosure section omits %q", gap)
	}

	// The README Limits copy.
	limits, ok := markdownSection(read("README.md"), regexp.MustCompile(`^Limits$`))
	require.True(t, ok, "README.md has no `## Limits` section")
	for _, gap := range telemetryDisclosureGaps(limits) {
		t.Errorf("README.md Limits section omits the telemetry disclosure element %q", gap)
	}
}
