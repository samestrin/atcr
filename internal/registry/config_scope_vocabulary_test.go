package registry

import (
	"bytes"
	"os"
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

// TestDocsRegistryDocumentsOffVocabularyScopeAsWarning pins docs/registry.md to
// the load behaviour the tests above assert. The scope row described validation
// as "every entry must be non-empty" and the section footer declared every
// out-of-range value a load error — so the doc said the single most likely scope
// misconfiguration could not happen, and a reader had no reason to stop writing
// arbitrary focus words.
//
// The phrase is shared with the live warning rather than written out twice, so a
// reword of the warning without a doc update trips this test instead of silently
// drifting.
func TestDocsRegistryDocumentsOffVocabularyScopeAsWarning(t *testing.T) {
	const phrase = "not a member of the closed CATEGORY vocabulary"
	require.Contains(t, scopeWarnFor(t, []string{"efficiency"}), phrase,
		"guard: the load warning must still use the phrase this test pins the doc to")

	data, err := os.ReadFile("../../docs/registry.md")
	require.NoError(t, err)
	doc := string(data)

	require.Contains(t, doc, phrase,
		"registry.md must document that an off-vocabulary scope entry is reported as a non-member")
	require.Contains(t, doc, "reconcile.Categories()",
		"registry.md must point at reconcile.Categories() as the scope vocabulary authority")
	require.Contains(t, doc, "is not a load error",
		"registry.md must correct the claim that every out-of-range scope value is caught at load")
}
