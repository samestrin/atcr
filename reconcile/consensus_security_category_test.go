package reconcile

import "testing"

// securityExemptMembers is the vocabulary members securityRelated must treat as
// security concerns, spelled out so a future member cannot silently fall out of
// the consensus-filter exemption.
//
// It is a table over the WHOLE vocabulary rather than a list of positives: the
// exemption decides whether an uncorroborated singleton survives to
// findings.json, so both directions matter — a missing member drops real
// security findings, and a stray one keeps every noisy singleton in its class.
var securityExemptMembers = map[string]bool{
	CategorySecurity:        true,
	CategorySecret:          true,
	CategoryInputValidation: true,
}

// TestSecurityRelated_CoversEveryVocabularyMember pins the exemption against the
// closed vocabulary (epic 35.16.4).
//
// The injected disambiguation rule now tells all 29 reviewers "`secret` for an
// exposed credential, `security` for every other vulnerability", which
// systematically moves credential findings from category security to category
// secret. securityRelated matched CategorySecurity plus the substrings
// vuln/auth/inject but NOT CategorySecret, so a MEDIUM or LOW single-reviewer
// credential finding — "secret in a log", "weak secret handling" — lost its
// consensusExempt() exemption and became a drop candidate where it previously
// survived. The same rule steers "injection" to input-validation, which the
// substring arm also does not catch.
//
// AC7 ("no consumer of Finding.Category changes behavior") stayed true of the
// code and false of the behavior: the constants are value-identical, but the
// prompt change altered the inputs this consumer sees.
func TestSecurityRelated_CoversEveryVocabularyMember(t *testing.T) {
	for _, c := range Categories() {
		want := securityExemptMembers[c]
		if got := securityRelated(c); got != want {
			t.Errorf("securityRelated(%q) = %v, want %v — a member moving in or out of the consensus-filter exemption must be deliberate", c, got, want)
		}
	}
}

// TestSecurityRelated_KeepsSubstringArm proves closing the vocabulary did not
// narrow the exemption for the synonyms reviewers actually emit outside it.
func TestSecurityRelated_KeepsSubstringArm(t *testing.T) {
	for _, c := range []string{"vulnerability", "authz", "sql-injection", " SECRET ", "Input-Validation"} {
		if !securityRelated(c) {
			t.Errorf("securityRelated(%q) = false, want true", c)
		}
	}
	for _, c := range []string{CategoryPerformance, CategoryNaming, CategoryDocs, ""} {
		if securityRelated(c) {
			t.Errorf("securityRelated(%q) = true, want false", c)
		}
	}
}

// TestConsensusExempt_CredentialSingletonSurvives is the behavioral half: a LOW
// single-reviewer credential finding is exactly the case the vocabulary change
// put at risk, and consensusExempt is the predicate that decides whether it
// reaches findings.json.
func TestConsensusExempt_CredentialSingletonSurvives(t *testing.T) {
	for _, cat := range []string{CategorySecret, CategoryInputValidation} {
		f := Finding{Category: cat, Severity: SevLow}
		if !consensusExempt(f) {
			t.Errorf("a LOW singleton with category %q is not consensus-exempt — the disambiguation rule steers credential and untrusted-input findings here, so the filter would now drop them", cat)
		}
	}
}
