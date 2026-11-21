package fanout

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Epic 35.20 AC8: docs/providers.md and docs/security.md must state the
// secret_guard setting, its default and tiers, the gitleaks vs built-in
// fallback, the fail-closed block behavior, the review prompt and tool-loop
// scope, the debate/verify coverage and the doctor exclusion, and the three-layer model
// (pre-commit scanner, egress guard, presidio).
// CHANGELOG.md's Unreleased section must carry the entry.
//
// Asserted on PHRASES, per max_context_lines_doc_test.go: a bare token such as
// "warn" survives a paragraph that says the opposite, while a phrase carries
// its claim.
func TestSecretGuardDoc_ProvidersPrivacySection(t *testing.T) {
	doc := readRepoFile(t, "../../docs/providers.md")
	const heading = "## Privacy and redaction"
	i := strings.Index(doc, heading)
	require.True(t, i >= 0, "docs/providers.md must keep its Privacy and redaction section")
	section := doc[i:]
	if j := strings.Index(section[len(heading):], "\n## "); j >= 0 {
		section = section[:len(heading)+j]
	}
	section = strings.Join(strings.Fields(section), " ")

	for _, must := range []struct{ phrase, why string }{
		{"`secret_guard`", "the setting must be named as the config key"},
		{"`off`, `warn`, `redact` or `block`", "the four policies must be enumerable from the doc"},
		{"default `warn`", "the default must be stated: an operator must know an unset guard still scans"},
		{"registry tier or per project", "both config tiers must be named"},
		{"project overrides the registry", "the precedence direction must be stated"},
		{"`gitleaks stdin` when the binary is on `PATH` and a built-in ruleset otherwise", "the two scanner paths must be stated"},
		{"fails closed", "block on a scanner failure must be documented, not just block on a hit"},
		{"review prompt and tool-loop results", "the review prompt and subsequent tool-loop traffic must be covered"},
		{"sprint-plan scope constraint", "the guard also scans the scope constraint; the doc must say so"},
		{"Persona text is not scanned", "persona text is rendered after the scan; the doc must not claim it is covered"},
		{"Debate seats and the verify skeptic and fix-executor prompts are scanned too", "debate and verify set the policy (epic 106.3); the doc must say they are covered"},
		{"Doctor probes are not scanned", "doctor sends no policy; the doc must not claim it is covered"},
		{"never the value", "the no-secret-material-in-output property must be stated"},
		{"pre-commit", "layer 1 of the three-layer model"},
		{"egress guard", "layer 2 of the three-layer model"},
		{"presidio", "layer 3 of the three-layer model must tie back to the section's existing advice"},
	} {
		if !strings.Contains(section, must.phrase) {
			t.Errorf("docs/providers.md's Privacy and redaction section must state %q: %s", must.phrase, must.why)
		}
	}
	require.NotContains(t, section, "will be scanned once their callers set the policy",
		"debate and verify already set the policy; the doc must not call that coverage planned")
}

func TestSecretGuardDoc_SecurityPage(t *testing.T) {
	doc := strings.Join(strings.Fields(readRepoFile(t, "../../docs/security.md")), " ")
	for _, must := range []struct{ phrase, why string }{
		{"`secret_guard`", "the security page must name the setting"},
		{"review prompt and tool-loop results", "the security page must state that the review prompt and subsequent tool-loop traffic are covered"},
		{"Persona text is not scanned", "the security page must not claim persona text is covered"},
		{"Debate seats and the verify skeptic and fix-executor prompts are scanned too", "debate and verify set the policy (epic 106.3); the security page must say they are covered"},
		{"Doctor probes are not scanned", "doctor sends no policy; the security page must not claim it is covered"},
		{"pre-commit", "layer 1 of the three-layer model"},
		{"egress guard", "layer 2 of the three-layer model"},
		{"presidio", "layer 3 of the three-layer model"},
		{"providers.md", "the security page must point at the full setting documentation"},
	} {
		if !strings.Contains(doc, must.phrase) {
			t.Errorf("docs/security.md must state %q: %s", must.phrase, must.why)
		}
	}
	require.NotContains(t, doc, "will be scanned once their callers set the policy",
		"debate and verify already set the policy; the security page must not call that coverage planned")
}

// The entry sits in whichever section the release cut put it in, so this reads
// the whole changelog rather than only [Unreleased], which is empty after a cut.
func TestSecretGuardDoc_ChangelogEntry(t *testing.T) {
	changelog := readRepoFile(t, "../../CHANGELOG.md")
	require.Contains(t, changelog, "\n## [Unreleased]\n", "CHANGELOG.md must keep an Unreleased section")
	require.Contains(t, changelog, "`secret_guard`", "the changelog must carry the secret_guard entry")
	require.Contains(t, changelog, "(epic 35.20)", "the entry must carry its source, per the changelog rules")
}
