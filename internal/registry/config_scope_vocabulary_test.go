package registry

import (
	"bytes"
	"strings"
	"testing"

	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scopeWarnFor runs validateAgent over an agent carrying scope and returns what
// the scope-vocabulary warning sink received.
func scopeWarnFor(t *testing.T, scope []string) string {
	t.Helper()
	var buf bytes.Buffer
	orig := scopeVocabularyWarnWriter
	scopeVocabularyWarnWriter = &buf
	t.Cleanup(func() { scopeVocabularyWarnWriter = orig })

	r := &Registry{
		Providers: map[string]Provider{"p": {APIKeyEnv: "KEY"}},
		Agents:    map[string]AgentConfig{"a": {Provider: "p", Model: "m", Scope: scope}},
	}
	errs := r.validateAgent("a", r.Agents["a"])
	require.Empty(t, errs, "scope membership must not be a load error — pre-existing configs keep loading")
	return buf.String()
}

// TestValidateAgent_WarnsOnOffVocabularyScope covers the epic 35.16.4 gap:
// ScopeFocus concatenates operator scope entries verbatim into the same prompt
// that now declares a closed CATEGORY vocabulary, and the entries land AFTER the
// rule at the position of maximum recency. An entry outside the vocabulary
// therefore ends the prompt by naming a word the prompt earlier declared
// illegal. Load-time validation only checked blank and control-character
// entries, so nothing surfaced it.
//
// It WARNS rather than errors: scope is a soft focus hint, and a pre-existing
// config naming a non-member must keep loading.
func TestValidateAgent_WarnsOnOffVocabularyScope(t *testing.T) {
	out := scopeWarnFor(t, []string{"efficiency"})
	assert.Contains(t, out, "efficiency", "the warning must name the offending entry")
	assert.Contains(t, strings.ToLower(out), "vocabulary",
		"the warning must say the entry is outside the closed CATEGORY vocabulary")
}

// TestValidateAgent_SilentOnVocabularyScope keeps the warning non-vacuous: a
// scope built entirely from members must produce no output at all, or the
// warning is noise every operator learns to ignore.
func TestValidateAgent_SilentOnVocabularyScope(t *testing.T) {
	out := scopeWarnFor(t, []string{reclib.CategoryPerformance, reclib.CategorySecurity})
	assert.Empty(t, out, "a scope of vocabulary members must warn about nothing")
}

// TestValidateAgent_ScopeWarningSuggestsNearestMember checks the actionable half:
// a near-miss spelling gets pointed at the member it almost is, while a word with
// no close member is reported without a misleading suggestion.
func TestValidateAgent_ScopeWarningSuggestsNearestMember(t *testing.T) {
	near := scopeWarnFor(t, []string{"performace"})
	assert.Contains(t, near, reclib.CategoryPerformance,
		"a one-typo entry should name the member it almost matches")

	far := scopeWarnFor(t, []string{"efficiency"})
	assert.NotContains(t, far, "did you mean",
		"an entry with no close member must not be given a fabricated suggestion")
}
