package reconcile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/require"
)

// docs/registry.md's response_format row is the only operator-facing statement of
// what the flag does and how far the doctor probe can be trusted (Epic 35.16.11.2.1).
// The legal value is read from registry.ResponseFormatJSONObject, not restated here,
// so renaming it without touching the doc fails this test; widening the enum is
// caught by TestRegistryDoc_ResponseFormatRejectsWhatTheDocExcludes below.
//
// Asserted on PHRASES, per max_context_lines_doc_test.go: a bare token such as
// "proof" survives a row that says the opposite, while a phrase carries its claim.
// The row is a single table line, so no phrase can straddle a hard wrap.
func TestRegistryDoc_ResponseFormatRow(t *testing.T) {
	row := responseFormatRow(t)

	for _, must := range []struct{ token, why string }{
		{"`" + registry.ResponseFormatJSONObject + "`", "the row must name the one value validateAgent accepts, spelled as the registry constant"},
		{"`json_schema` is out of scope", "json_schema strict mode is deliberately not accepted; the row must say so rather than leave it implied"},
		{"never inherited through `fallback:`", "a fallback sends its OWN declaration, never the primary's (AC 03-01); the row must say so like supports_function_calling does"},
		{"only the judge seat sends it", "the debate proposer and challenger seats never send the field, even when their agent is declared"},
		{"strong evidence, not proof", "a model can return a bare object without JSON mode, so a doctor pass does not prove the provider honored the field"},
		{"never changes the ok/failed count or the exit code", "a response_format mismatch is a warning; a CI gate reading the exit code must not expect it to fail"},
		{"`## Output Format` section is swapped", "a declared review agent's persona Output Format is replaced at render time; an operator reading the prompt must not be surprised"},
		{"skeptic and debate judge lanes need no swap", "those lanes already ask for their own JSON object; the row must not imply they are rewritten"},
		{"known-good models", "the list is a positive-evidence allowlist seeded from a real doctor run, not a compatibility claim"},
	} {
		if !strings.Contains(row, must.token) {
			t.Errorf("docs/registry.md's response_format row must state %q: %s\nrow was: %s", must.token, must.why, row)
		}
	}

	// The models the live run found not honoring the field must never drift into
	// the allowlist, which runs from its heading to the "Not honored" sentence.
	start := strings.Index(row, "known-good models")
	end := strings.Index(row, "Not honored in those runs")
	require.True(t, start >= 0 && end > start, "the row must list known-good models before the not-honored ones")
	for _, bad := range []string{"`nemotron-3-nano`", "`bob2-model`"} {
		if strings.Contains(row[start:end], bad) {
			t.Errorf("%s did not honor response_format in the seeding run and must not be listed as known-good", bad)
		}
	}
}

// The row says json_schema, and every value but the constant, is rejected at load.
// That is a claim about validateAgent, not about the constant, so it is checked
// against a real load: widening the enum makes this fail even though the constant
// and the doc are unchanged.
func TestRegistryDoc_ResponseFormatRejectsWhatTheDocExcludes(t *testing.T) {
	require.Contains(t, responseFormatRow(t), "rejected at load")

	load := func(value string) error {
		path := filepath.Join(t.TempDir(), "registry.yaml")
		body := "providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n    response_format: " + value + "\n"
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		_, err := registry.LoadRegistry(path)
		return err
	}

	require.NoError(t, load(registry.ResponseFormatJSONObject), "the documented value must load")
	for _, value := range []string{"json_schema", "JSON_OBJECT", "text"} {
		require.Errorf(t, load(value), "the doc says %q is rejected at load", value)
	}
}

func responseFormatRow(t *testing.T) string {
	t.Helper()
	return docRow(t, readRepoFile(t, "../../docs/registry.md"), "`response_format`")
}
