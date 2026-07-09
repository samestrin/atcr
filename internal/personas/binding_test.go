package personas

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AC 02-01: Family/Channel Binding schema extension ----------------------
//
// These tests lock in the additive `Binding` field on PersonaIndexEntry: it
// decodes permissively (like Provider/Model), is omitempty on the wire, and its
// presence or absence never perturbs the existing fields. The field is inert in
// Phase 2 — no code reads it on any path (see the review-path AC 02-02 guard).

// TestPersonaIndexEntry_BindingDecodes covers AC 02-01 Scenario 1: a new-shape
// entry carrying `binding` decodes into Binding, with every other field unchanged.
func TestPersonaIndexEntry_BindingDecodes(t *testing.T) {
	const entry = `{
		"name":"anthony",
		"version":"1.0.0",
		"description":"architecture reviewer",
		"path":"anthony.yaml",
		"provider":"openrouter",
		"model":"anthropic/claude-opus-4.8",
		"binding":"anthropic/claude-opus@stable",
		"tasks":["architecture-review"],
		"tags":["architecture"]
	}`
	var got PersonaIndexEntry
	require.NoError(t, json.Unmarshal([]byte(entry), &got))

	assert.Equal(t, "anthropic/claude-opus@stable", got.Binding)
	// Every other field decodes exactly as before the field was added.
	assert.Equal(t, "anthony", got.Name)
	assert.Equal(t, "1.0.0", got.Version)
	assert.Equal(t, "architecture reviewer", got.Description)
	assert.Equal(t, "anthony.yaml", got.Path)
	assert.Equal(t, "openrouter", got.Provider)
	assert.Equal(t, "anthropic/claude-opus-4.8", got.Model)
	assert.Equal(t, []string{"architecture-review"}, got.Tasks)
	assert.Equal(t, []string{"architecture"}, got.Tags)
}

// TestPersonaIndexEntry_BindingOmitemptyTag pins the json tag: `binding,omitempty`,
// matching the additive convention used for Provider/Model/Tasks/Tags.
func TestPersonaIndexEntry_BindingOmitemptyTag(t *testing.T) {
	rt := reflect.TypeOf(PersonaIndexEntry{})
	f, ok := rt.FieldByName("Binding")
	require.True(t, ok, "PersonaIndexEntry must have a Binding field")
	assert.Equal(t, "binding,omitempty", f.Tag.Get("json"),
		"Binding must carry json:\"binding,omitempty\"")
}

// TestPersonaIndexEntry_BindingAbsentDecodesEmpty covers AC 02-01 Edge Case 1: an
// old-shape entry (no `binding` key) decodes Binding as "" with no error.
func TestPersonaIndexEntry_BindingAbsentDecodesEmpty(t *testing.T) {
	const entry = `{"name":"anthony","version":"1.0.0","description":"d","path":"anthony.yaml","provider":"openrouter","model":"anthropic/claude-opus-4.8"}`
	var got PersonaIndexEntry
	require.NoError(t, json.Unmarshal([]byte(entry), &got))
	assert.Equal(t, "", got.Binding, "absent binding must decode as the zero value")
}

// TestPersonaIndexEntry_BindingMixedShape covers AC 02-01 Edge Case 2: an array
// mixing one entry with `binding` and one without decodes each independently.
func TestPersonaIndexEntry_BindingMixedShape(t *testing.T) {
	const mixed = `[
	  {"name":"old","version":"1.0.0","description":"legacy","path":"old.yaml","provider":"openrouter","model":"deepseek/deepseek-v4-pro"},
	  {"name":"new","version":"2.0.0","description":"modern","path":"new.yaml","provider":"openrouter","model":"anthropic/claude-opus-4.8","binding":"anthropic/claude-opus@stable"}
	]`
	var got []PersonaIndexEntry
	require.NoError(t, json.Unmarshal([]byte(mixed), &got))
	require.Len(t, got, 2)
	assert.Equal(t, "", got[0].Binding, "old-shape entry has empty Binding")
	assert.Equal(t, "anthropic/claude-opus@stable", got[1].Binding, "new-shape entry keeps its own Binding")
}

// TestPersonaIndexEntry_BindingMarshalOmitsWhenEmpty covers AC 02-01 Scenario 3:
// a populated Binding marshals to the `binding` key; a zero Binding omits it.
func TestPersonaIndexEntry_BindingMarshalOmitsWhenEmpty(t *testing.T) {
	// Populated: key present.
	set := PersonaIndexEntry{Name: "a", Version: "1", Description: "d", Path: "a.yaml", Binding: "anthropic/claude-opus@stable"}
	data, err := json.Marshal(set)
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Contains(t, raw, "binding")

	// Zero value: key omitted.
	unset := PersonaIndexEntry{Name: "a", Version: "1", Description: "d", Path: "a.yaml"}
	data, err = json.Marshal(unset)
	require.NoError(t, err)
	raw = map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.NotContains(t, raw, "binding", "empty Binding must be omitted (omitempty)")
}
