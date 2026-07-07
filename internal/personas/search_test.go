package personas

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// verifyCommunityIndex is the enforcement engine behind the AC7 go-test gate
// (AC 02-02): every entry in the community index at indexPath must carry non-empty
// provider/model equal to the entry's source persona YAML, resolved relative to
// personasRoot via the entry's Path. It returns human-readable problem messages
// (empty means the contract holds) and an error only when the index itself cannot
// be read/parsed. It is deliberately test-only — the contract is a build-time gate
// over in-repo files (LOCKED decision Q4: the index is authored in-repo, not
// generated), not a runtime path. Embedded built-ins are exempt because they are
// never enumerated in the community index.
//
// Scope limit: it verifies provider/model equality only. It does not cross-check an
// entry's name/version/description against the YAML — AC 02-02 scopes the gate to
// the model-discovery metadata (provider/model), which is what discovery depends on.
func verifyCommunityIndex(indexPath, personasRoot string) ([]string, error) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	var entries []PersonaIndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse %s: %w", indexPath, err)
	}
	var problems []string
	for _, e := range entries {
		// Empty checks are independent so a partial regression is detectable.
		if e.Provider == "" {
			problems = append(problems, fmt.Sprintf("%s: index entry has empty provider", e.Path))
		}
		if e.Model == "" {
			problems = append(problems, fmt.Sprintf("%s: index entry has empty model", e.Path))
		}

		// Resolve the source YAML, refusing an absolute path or a `..` escape out
		// of personasRoot (defense in depth; the index is in-repo, but the join is
		// otherwise unvalidated).
		rel := filepath.FromSlash(e.Path)
		if filepath.IsAbs(rel) {
			problems = append(problems, fmt.Sprintf("%s: entry path must be relative to the personas root", e.Path))
			continue
		}
		full := filepath.Join(personasRoot, rel)
		if inside, err := filepath.Rel(personasRoot, full); err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			problems = append(problems, fmt.Sprintf("%s: entry path escapes the personas root", e.Path))
			continue
		}

		raw, err := os.ReadFile(full)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: cannot read source persona YAML: %v", e.Path, err))
			continue
		}
		var pm struct {
			Provider string `yaml:"provider"`
			Model    string `yaml:"model"`
		}
		if err := yaml.Unmarshal(raw, &pm); err != nil {
			problems = append(problems, fmt.Sprintf("%s: cannot parse source persona YAML: %v", e.Path, err))
			continue
		}
		if pm.Provider != e.Provider {
			problems = append(problems, fmt.Sprintf(
				"%s: provider mismatch — index=%q yaml=%q", e.Path, e.Provider, pm.Provider))
		}
		if pm.Model != e.Model {
			problems = append(problems, fmt.Sprintf(
				"%s: model mismatch — index=%q yaml=%q", e.Path, e.Model, pm.Model))
		}
	}
	return problems, nil
}

// --- AC 02-01: PersonaIndexEntry schema extension ---------------------------

// TestPersonaIndexEntry_DecodesFullNewShape covers AC 02-01 Scenario 1: a new-shape
// entry with all eight fields decodes each field to the expected value.
func TestPersonaIndexEntry_DecodesFullNewShape(t *testing.T) {
	const entry = `{
		"name":"security/owasp",
		"version":"1.0.0",
		"description":"OWASP reviewer",
		"path":"security/owasp.yaml",
		"provider":"anthropic",
		"model":"claude-sonnet-4-6",
		"tasks":["security-review"],
		"tags":["owasp","security"]
	}`
	var got PersonaIndexEntry
	require.NoError(t, json.Unmarshal([]byte(entry), &got))

	assert.Equal(t, "security/owasp", got.Name)
	assert.Equal(t, "1.0.0", got.Version)
	assert.Equal(t, "OWASP reviewer", got.Description)
	assert.Equal(t, "security/owasp.yaml", got.Path)
	assert.Equal(t, "anthropic", got.Provider)
	assert.Equal(t, "claude-sonnet-4-6", got.Model)
	assert.Equal(t, []string{"security-review"}, got.Tasks)
	assert.Equal(t, []string{"owasp", "security"}, got.Tags)
}

// TestPersonaIndexEntry_OriginalTagsUnchanged covers AC 02-01 Scenario 2: the four
// original fields keep their exact json tags with no omitempty added.
func TestPersonaIndexEntry_OriginalTagsUnchanged(t *testing.T) {
	rt := reflect.TypeOf(PersonaIndexEntry{})
	cases := map[string]string{
		"Name":        "name",
		"Version":     "version",
		"Description": "description",
		"Path":        "path",
	}
	for field, wantTag := range cases {
		f, ok := rt.FieldByName(field)
		require.Truef(t, ok, "field %s must exist", field)
		assert.Equalf(t, wantTag, f.Tag.Get("json"),
			"field %s must keep json:%q byte-for-byte (no omitempty)", field, wantTag)
	}

	// The four new fields must carry omitempty.
	for field, wantTag := range map[string]string{
		"Provider": "provider,omitempty",
		"Model":    "model,omitempty",
		"Tasks":    "tasks,omitempty",
		"Tags":     "tags,omitempty",
	} {
		f, ok := rt.FieldByName(field)
		require.Truef(t, ok, "field %s must exist", field)
		assert.Equalf(t, wantTag, f.Tag.Get("json"),
			"field %s must carry json:%q", field, wantTag)
	}
}

// TestPersonaIndexEntry_MarshalRoundTrip covers AC 02-01 Scenario 3: a fully populated
// entry marshals to JSON containing all eight keys, original keys unchanged.
func TestPersonaIndexEntry_MarshalRoundTrip(t *testing.T) {
	in := PersonaIndexEntry{
		Name:        "security/owasp",
		Version:     "1.0.0",
		Description: "OWASP reviewer",
		Path:        "security/owasp.yaml",
		Provider:    "anthropic",
		Model:       "claude-sonnet-4-6",
		Tasks:       []string{"security-review"},
		Tags:        []string{"owasp", "security"},
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	for _, key := range []string{"name", "version", "description", "path", "provider", "model", "tasks", "tags"} {
		assert.Containsf(t, raw, key, "marshaled JSON must contain key %q", key)
	}

	// Round-trips back to an equal value.
	var back PersonaIndexEntry
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, in, back)
}

// TestPersonaIndexEntry_AbsentOptionalFieldsAreNil covers AC 02-01 Edge Case 1:
// tasks/tags omitted decode as nil slices (not empty-but-non-nil), no error.
func TestPersonaIndexEntry_AbsentOptionalFieldsAreNil(t *testing.T) {
	const entry = `{"name":"a","version":"1","description":"d","path":"a.yaml","provider":"anthropic","model":"claude-sonnet-4-6"}`
	var got PersonaIndexEntry
	require.NoError(t, json.Unmarshal([]byte(entry), &got))
	assert.Nil(t, got.Tasks, "absent tasks must decode as nil, not []string{}")
	assert.Nil(t, got.Tags, "absent tags must decode as nil, not []string{}")

	// omitempty means these keys must not appear when re-marshaled. Assert on the
	// decoded key set (not a fragile whole-blob substring) so a value that merely
	// contains "tasks"/"tags" cannot false-pass.
	data, err := json.Marshal(got)
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.NotContains(t, raw, "tasks")
	assert.NotContains(t, raw, "tags")
}

// TestPersonaIndexEntry_BareOldShapeDecodes asserts the core additive contract:
// an old-shape entry carrying ONLY the four original keys decodes with zero-value
// new fields and no error. (AC 02-03 exercises this end-to-end via FetchIndex too.)
func TestPersonaIndexEntry_BareOldShapeDecodes(t *testing.T) {
	const entry = `{"name":"security/owasp","version":"1.0.0","description":"OWASP reviewer","path":"security/owasp.yaml"}`
	var got PersonaIndexEntry
	require.NoError(t, json.Unmarshal([]byte(entry), &got))
	assert.Equal(t, "security/owasp", got.Name)
	assert.Empty(t, got.Provider)
	assert.Empty(t, got.Model)
	assert.Nil(t, got.Tasks)
	assert.Nil(t, got.Tags)
}

// TestPersonaIndexEntry_UnknownKeysIgnored proves the index decode path stays
// permissive: an unrecognized key does not cause a decode error (no KnownFields).
func TestPersonaIndexEntry_UnknownKeysIgnored(t *testing.T) {
	const entry = `{"name":"a","version":"1","description":"d","path":"a.yaml","future_field":"x"}`
	var got PersonaIndexEntry
	require.NoError(t, json.Unmarshal([]byte(entry), &got), "unknown keys must be silently ignored")
	assert.Equal(t, "a", got.Name)
}

// TestPersonaIndexEntry_EmptyProviderModel covers AC 02-01 Edge Case 2: empty-string
// provider/model decode without error (no non-empty validation at this layer).
func TestPersonaIndexEntry_EmptyProviderModel(t *testing.T) {
	const entry = `{"name":"a","version":"1","description":"d","path":"a.yaml","provider":"","model":""}`
	var got PersonaIndexEntry
	require.NoError(t, json.Unmarshal([]byte(entry), &got))
	assert.Empty(t, got.Provider)
	assert.Empty(t, got.Model)
}

// TestPersonaIndexEntry_MalformedJSONErrors covers AC 02-01 Error Scenario 1: a
// syntactically invalid entry surfaces a decode error (error handling unchanged).
func TestPersonaIndexEntry_MalformedJSONErrors(t *testing.T) {
	const entry = `{"name":"a","version":"1",}` // trailing comma
	var got PersonaIndexEntry
	require.Error(t, json.Unmarshal([]byte(entry), &got))
}

// --- AC 02-02: index.json field population contract (AC7 enforcement gate) ---

// TestCommunityIndex_ProviderModelMatchesYAML is the AC7 enforcement gate: every
// entry in personas/community/index.json must carry non-empty provider/model equal
// to its source persona YAML (resolved via the entry's path). An empty index (the
// state through Phase 2, before content is authored in Phase 5) passes vacuously;
// once real entries land, any missing/drifted metadata fails `go test`. Embedded
// built-ins are exempt — they are not enumerated in the community index.
func TestCommunityIndex_ProviderModelMatchesYAML(t *testing.T) {
	root := filepath.Join("..", "..", "personas", "community")
	problems, err := verifyCommunityIndex(filepath.Join(root, "index.json"), root)
	require.NoError(t, err, "community index.json must exist and be readable")
	assert.Empty(t, problems,
		"every index entry's provider/model must be non-empty and equal its source YAML:\n%s",
		strings.Join(problems, "\n"))
}

// TestVerifyCommunityIndex_FailsOnMismatch proves the gate catches every distinct
// drift mode independently. The testdata index isolates one failure per entry
// (provider-only mismatch, model-only mismatch, empty provider, empty model) so a
// partial regression in any single check fails exactly one assertion rather than
// being masked by a co-occurring problem. Assertions pin the discriminating reason
// substring, not the entry filename (which prefixes every message for that entry).
func TestVerifyCommunityIndex_FailsOnMismatch(t *testing.T) {
	root := filepath.Join("testdata", "badindex")
	problems, err := verifyCommunityIndex(filepath.Join(root, "index.json"), root)
	require.NoError(t, err)
	joined := strings.Join(problems, "\n")

	assert.Contains(t, joined, "provmismatch.yaml: provider mismatch", "provider-only drift must be caught")
	assert.Contains(t, joined, "modelmismatch.yaml: model mismatch", "model-only drift must be caught")
	assert.Contains(t, joined, "emptyprov.yaml: index entry has empty provider", "empty provider must be caught")
	assert.Contains(t, joined, "emptymodel.yaml: index entry has empty model", "empty model must be caught")

	// The provider-only fixture must NOT also report a model mismatch (its model
	// matches), proving the checks are independent.
	assert.NotContains(t, joined, "provmismatch.yaml: model mismatch")
}
