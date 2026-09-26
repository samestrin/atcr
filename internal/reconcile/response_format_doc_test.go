package reconcile

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
)

// docs/registry.md's response_format row is the only operator-facing statement of
// what the flag does and how far the doctor probe can be trusted (Epic 35.16.11.2.1).
// The legal value is read from registry.ResponseFormatJSONObject, not restated here,
// so widening or renaming the enum without touching the doc fails this test.
//
// Asserted on PHRASES, per max_context_lines_doc_test.go: a bare token such as
// "proof" survives a row that says the opposite, while a phrase carries its claim.
// The row is a single table line, so no phrase can straddle a hard wrap.
func TestRegistryDoc_ResponseFormatRow(t *testing.T) {
	row := docRow(t, readRepoFile(t, "../../docs/registry.md"), "`response_format`")

	for _, must := range []struct{ token, why string }{
		{"`" + registry.ResponseFormatJSONObject + "`", "the row must name the one value validateAgent accepts, spelled as the registry constant"},
		{"`json_schema` is out of scope", "json_schema strict mode is deliberately not accepted; the row must say so rather than leave it implied"},
		{"never inherited through `fallback:`", "a fallback sends its OWN declaration, never the primary's (AC 03-01); the row must say so like supports_function_calling does"},
		{"strong evidence, not proof", "a model can return a bare object without JSON mode, so a doctor pass does not prove the provider honored the field"},
		{"`## Output Format` section is swapped", "a declared review agent's persona Output Format is replaced at render time; an operator reading the prompt must not be surprised"},
		{"skeptic and debate judge lanes need no swap", "those lanes already ask for their own JSON object; the row must not imply they are rewritten"},
		{"known-good models", "the list is a positive-evidence allowlist seeded from a real doctor run, not a compatibility claim"},
	} {
		if !strings.Contains(row, must.token) {
			t.Errorf("docs/registry.md's response_format row must state %q: %s\nrow was: %s", must.token, must.why, row)
		}
	}
}
