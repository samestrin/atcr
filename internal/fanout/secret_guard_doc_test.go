package fanout

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Epic 35.20 AC8: docs/providers.md and docs/security.md must state the
// secret_guard setting, its default and tiers, the gitleaks vs built-in
// fallback, the fail-closed block behavior, the review-prompt-only scope, and
// the three-layer model (pre-commit scanner, egress guard, presidio).
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

	for _, must := range []struct{ phrase, why string }{
		{"`secret_guard`", "the setting must be named as the config key"},
		{"`off`, `warn`, `redact` or `block`", "the four policies must be enumerable from the doc"},
		{"default `warn`", "the default must be stated: an operator must know an unset guard still scans"},
		{"registry tier or per project", "both config tiers must be named"},
		{"project overrides the registry", "the precedence direction must be stated"},
		{"`gitleaks stdin` when the binary is on `PATH` and a built-in ruleset otherwise", "the two scanner paths must be stated"},
		{"fails closed", "block on a scanner failure must be documented, not just block on a hit"},
		{"review prompt only", "the scope limit must be stated"},
		{"not the tool-loop results, debate, verify or doctor", "the excluded lanes must be named so nobody expects the guard there"},
		{"never the value", "the no-secret-material-in-output property must be stated"},
		{"pre-commit", "layer 1 of the three-layer model"},
		{"egress guard", "layer 2 of the three-layer model"},
		{"presidio", "layer 3 of the three-layer model must tie back to the section's existing advice"},
	} {
		if !strings.Contains(section, must.phrase) {
			t.Errorf("docs/providers.md's Privacy and redaction section must state %q: %s", must.phrase, must.why)
		}
	}
}

func TestSecretGuardDoc_SecurityPage(t *testing.T) {
	doc := readRepoFile(t, "../../docs/security.md")
	for _, must := range []struct{ phrase, why string }{
		{"`secret_guard`", "the security page must name the setting"},
		{"review prompt only", "the security page must state the scope limit"},
		{"pre-commit", "layer 1 of the three-layer model"},
		{"egress guard", "layer 2 of the three-layer model"},
		{"presidio", "layer 3 of the three-layer model"},
		{"providers.md", "the security page must point at the full setting documentation"},
	} {
		if !strings.Contains(doc, must.phrase) {
			t.Errorf("docs/security.md must state %q: %s", must.phrase, must.why)
		}
	}
}

func TestSecretGuardDoc_ChangelogEntry(t *testing.T) {
	changelog := readRepoFile(t, "../../CHANGELOG.md")
	const marker = "\n## [Unreleased]\n"
	i := strings.Index(changelog, marker)
	require.True(t, i >= 0, "CHANGELOG.md must keep an Unreleased section")
	unreleased := changelog[i+1:]
	if j := strings.Index(unreleased[len(marker):], "\n## ["); j >= 0 {
		unreleased = unreleased[:len(marker)+j]
	}
	require.Contains(t, unreleased, "`secret_guard`", "the Unreleased section must carry the secret_guard entry")
	require.Contains(t, unreleased, "(epic 35.20)", "the entry must carry its source, per the changelog rules")
}
