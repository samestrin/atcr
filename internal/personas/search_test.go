package personas

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
