package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	commpersonas "github.com/samestrin/atcr/internal/personas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeCommunityPersona installs a fixture community persona YAML (model + optional
// binding) under dir so `models check` can read its resolved lock. Names must not
// collide with a built-in persona (listCommunity skips those).
func writeCommunityPersona(t *testing.T, dir, name, model, binding string) {
	t.Helper()
	body := "provider: acme\nmodel: " + model + "\nrole: reviewer\nlanguage:\n  - go\nversion: \"1.0.0\"\ndescription: \"fixture persona\"\n"
	if binding != "" {
		body += "binding: " + binding + "\n"
	}
	path := filepath.Join(dir, name+".yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

// withCatalogSnapshot writes a temp catalog snapshot ({"data":[...]}) and points
// ATCR_CATALOG_SNAPSHOT at it for the test's duration, returning the path.
func withCatalogSnapshot(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog_snapshot.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"data":`+data+`}`), 0o644))
	t.Setenv("ATCR_CATALOG_SNAPSHOT", path)
	return path
}

// withEmptyPersonasDir points personasDir at a fresh temp dir (no community
// personas) and restores it after the test.
func withEmptyPersonasDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := personasDir
	personasDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { personasDir = old })
	return dir
}

// driftFixtureCatalog covers a newer-member family (anthony), a deprecation-only
// family (gene), and (via absence) a missing slug (milo).
const driftFixtureCatalog = `[
  {"id":"anthropic/claude-opus-4.8","canonical_slug":"anthropic/claude-opus-4.8","created":100,"expiration_date":null},
  {"id":"anthropic/claude-opus-5.0","canonical_slug":"anthropic/claude-opus-5.0","created":200,"expiration_date":null},
  {"id":"google/gemini-pro-1.5","canonical_slug":"google/gemini-pro-1.5","created":100,"expiration_date":"2026-09-01"}
]`

// installDriftFixture installs anthony (newer member available), gene
// (deprecated lock), and milo (slug missing from catalog) against
// driftFixtureCatalog, returning the personas dir.
func installDriftFixture(t *testing.T) string {
	dir := withEmptyPersonasDir(t)
	withCatalogSnapshot(t, driftFixtureCatalog)
	writeCommunityPersona(t, dir, "anthony", "anthropic/claude-opus-4.8", "")
	writeCommunityPersona(t, dir, "gene", "google/gemini-pro-1.5", "")
	writeCommunityPersona(t, dir, "milo", "openai/gpt-4o-missing", "")
	return dir
}

func TestModelsCheck_IsRegistered(t *testing.T) {
	withEmptyPersonasDir(t)
	// A registered command produces no "unknown command" error.
	out, err := execute(t, "models", "check")
	require.NoError(t, err)
	assert.NotContains(t, out, "unknown command")
}

func TestModelsCheck_HumanReadable_AllThreeConditions(t *testing.T) {
	installDriftFixture(t)
	out, err := execute(t, "models", "check")
	// Conditions found → exit 1.
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err))
	assert.Contains(t, out, "anthony: anthropic/claude-opus-4.8 → anthropic/claude-opus-5.0 (newer member)")
	assert.Contains(t, out, "gene: google/gemini-pro-1.5 has expiration 2026-09-01 (deprecation)")
	assert.Contains(t, out, "milo: openai/gpt-4o-missing no longer in catalog (missing)")
}

func TestModelsCheck_NoConditions_CanonicalMessage(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	withCatalogSnapshot(t, driftFixtureCatalog)
	// anthony locked to the newest member → no drift, not deprecated, present.
	writeCommunityPersona(t, dir, "anthony", "anthropic/claude-opus-5.0", "")

	out, err := execute(t, "models", "check")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode(err))
	assert.Contains(t, out, "No drift, deprecation, or missing-slug conditions found.")
}

func TestModelsCheck_NoCommunityPersonas_NothingToCheck(t *testing.T) {
	withEmptyPersonasDir(t)
	withCatalogSnapshot(t, driftFixtureCatalog)

	out, err := execute(t, "models", "check")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode(err))
	assert.NotEmpty(t, out)
	assert.Contains(t, out, "nothing to check")
}

func TestModelsCheck_MultiCondition_OneLinePerCondition(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	// gene's lock is both deprecated AND behind a newer stable member.
	withCatalogSnapshot(t, `[
  {"id":"google/gemini-pro-1.5","canonical_slug":"google/gemini-pro-1.5","created":100,"expiration_date":"2026-09-01"},
  {"id":"google/gemini-pro-2.0","canonical_slug":"google/gemini-pro-2.0","created":200,"expiration_date":null}
]`)
	writeCommunityPersona(t, dir, "gene", "google/gemini-pro-1.5", "")

	out, err := execute(t, "models", "check")
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err))
	assert.Contains(t, out, "gene: google/gemini-pro-1.5 → google/gemini-pro-2.0 (newer member)")
	assert.Contains(t, out, "gene: google/gemini-pro-1.5 has expiration 2026-09-01 (deprecation)")
}

func TestModelsCheck_PerPersonaReadFailure_ExcludedNotAborted(t *testing.T) {
	dir := installDriftFixture(t)
	// Corrupt one persona's YAML so its lock cannot be read.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gene.yaml"), []byte("::: not yaml :::\n"), 0o644))

	out, _, _ := executeSplit(t, "models", "check")
	// The other personas are still checked.
	assert.Contains(t, out, "anthony: anthropic/claude-opus-4.8 → anthropic/claude-opus-5.0 (newer member)")
}

// TestModelsCheck_FilterNoMatch_DistinctMessage covers the 5.2.A LOW: a name
// filter matching no community persona reports a distinct message, not the
// misleading "nothing to check" reserved for an empty install.
func TestModelsCheck_FilterNoMatch_DistinctMessage(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	withCatalogSnapshot(t, driftFixtureCatalog)
	writeCommunityPersona(t, dir, "anthony", "anthropic/claude-opus-5.0", "")

	out, err := execute(t, "models", "check", "nonexistent")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode(err))
	assert.Contains(t, out, `No community persona named "nonexistent" to check.`)
	assert.NotContains(t, out, "nothing to check")
}

// TestModelsCheck_BindinglessFallback_NoCrossTierBleed covers the 5.2.A MEDIUM:
// a bindingless lock's family-prefix fallback must not float a sibling tier
// (openai/gpt-* must not be suggested a "-mini" tier member, and vice versa).
func TestModelsCheck_BindinglessFallback_NoCrossTierBleed(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	// gpt-6-mini is a NEWER, higher-versioned sibling TIER; it must not be
	// suggested as a newer member of the gpt (non-mini) family.
	withCatalogSnapshot(t, `[
  {"id":"openai/gpt-5.5","canonical_slug":"openai/gpt-5.5","created":100,"expiration_date":null},
  {"id":"openai/gpt-6-mini","canonical_slug":"openai/gpt-6-mini","created":900,"expiration_date":null}
]`)
	writeCommunityPersona(t, dir, "milo", "openai/gpt-5.5", "")

	out, err := execute(t, "models", "check")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode(err))
	assert.NotContains(t, out, "openai/gpt-6-mini")
	assert.Contains(t, out, "No drift, deprecation, or missing-slug conditions found.")
}

// TestDriftLine_StripsControlChars covers the 5.2.A LOW: displayed slug values are
// control-char-sanitized so a crafted lock cannot inject terminal escapes.
func TestDriftLine_StripsControlChars(t *testing.T) {
	line := driftLine(commpersonas.DriftFinding{
		Persona:     "evil",
		Condition:   commpersonas.ConditionMissing,
		CurrentSlug: "vendor/model\x1b[31m\n\u2028",
	})
	assert.NotContains(t, line, "\x1b")
	assert.NotContains(t, line, "\u2028")
	assert.Equal(t, "evil: vendor/model[31m no longer in catalog (missing)", line)
}

// jsonFinding decodes one --json object; condition-inapplicable fields stay at
// their zero value (proving omitempty omitted them).
type jsonFinding struct {
	Persona        string `json:"persona"`
	Condition      string `json:"condition"`
	CurrentSlug    string `json:"current_slug"`
	SuggestedSlug  string `json:"suggested_slug"`
	Family         string `json:"family"`
	Channel        string `json:"channel"`
	ExpirationDate string `json:"expiration_date"`
}

func decodeFindings(t *testing.T, stdout string) []jsonFinding {
	t.Helper()
	var fs []jsonFinding
	require.NoError(t, json.Unmarshal([]byte(stdout), &fs))
	return fs
}

func TestModelsCheckJSON_ArrayOneObjectPerCondition(t *testing.T) {
	installDriftFixture(t)
	out, _, err := executeSplit(t, "models", "check", "--json")
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err))

	fs := decodeFindings(t, out)
	require.Len(t, fs, 3)
	byPersona := map[string]jsonFinding{}
	for _, f := range fs {
		byPersona[f.Persona] = f
	}
	assert.Equal(t, "newer-member", byPersona["anthony"].Condition)
	assert.Equal(t, "deprecation", byPersona["gene"].Condition)
	assert.Equal(t, "missing", byPersona["milo"].Condition)
}

func TestModelsCheckJSON_NewerMemberFields(t *testing.T) {
	installDriftFixture(t)
	out, _, _ := executeSplit(t, "models", "check", "--json")
	fs := decodeFindings(t, out)

	var anthony jsonFinding
	for _, f := range fs {
		if f.Persona == "anthony" {
			anthony = f
		}
	}
	assert.Equal(t, "newer-member", anthony.Condition)
	assert.Equal(t, "anthropic/claude-opus-4.8", anthony.CurrentSlug)
	assert.Equal(t, "anthropic/claude-opus-5.0", anthony.SuggestedSlug)
	assert.Equal(t, "anthropic/claude-opus", anthony.Family)
	assert.Equal(t, "stable", anthony.Channel)
	assert.Empty(t, anthony.ExpirationDate)
}

func TestModelsCheckJSON_ConditionSpecificFieldsOmitted(t *testing.T) {
	installDriftFixture(t)
	out, _, _ := executeSplit(t, "models", "check", "--json")

	// gene (deprecation): carries expiration_date, omits newer-member fields.
	// milo (missing): carries only persona/condition/current_slug.
	assert.Contains(t, out, `"expiration_date": "2026-09-01"`)
	assert.NotContains(t, out, `"suggested_slug": ""`)
	assert.NotContains(t, out, `"expiration_date": ""`)
	assert.NotContains(t, out, `"family": ""`)
	assert.NotContains(t, out, `null`)

	fs := decodeFindings(t, out)
	for _, f := range fs {
		switch f.Persona {
		case "gene":
			assert.Equal(t, "2026-09-01", f.ExpirationDate)
			assert.Empty(t, f.SuggestedSlug)
		case "milo":
			assert.Empty(t, f.SuggestedSlug)
			assert.Empty(t, f.Family)
			assert.Empty(t, f.ExpirationDate)
		}
	}
}

func TestModelsCheckJSON_EmptyResultIsEmptyArray(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	withCatalogSnapshot(t, driftFixtureCatalog)
	writeCommunityPersona(t, dir, "anthony", "anthropic/claude-opus-5.0", "") // clean

	out, _, err := executeSplit(t, "models", "check", "--json")
	require.NoError(t, err)
	assert.Equal(t, "[]\n", out)
	// Must decode unconditionally.
	var fs []jsonFinding
	require.NoError(t, json.Unmarshal([]byte(out), &fs))
	assert.Empty(t, fs)
}

func TestModelsCheckJSON_NoCommunityPersonas_EmptyArray(t *testing.T) {
	withEmptyPersonasDir(t)
	withCatalogSnapshot(t, driftFixtureCatalog)

	out, _, err := executeSplit(t, "models", "check", "--json")
	require.NoError(t, err)
	assert.Equal(t, "[]\n", out)
}

func TestModelsCheckJSON_ParityWithHumanReadable(t *testing.T) {
	installDriftFixture(t)
	jsonOut, _, _ := executeSplit(t, "models", "check", "--json")
	humanOut, _, _ := executeSplit(t, "models", "check")

	// Same (persona, condition) set in both modes.
	type pc struct{ p, c string }
	want := map[pc]bool{}
	for _, f := range decodeFindings(t, jsonOut) {
		want[pc{f.Persona, f.Condition}] = true
	}
	got := map[pc]bool{}
	for _, line := range []struct{ persona, cond, needle string }{
		{"anthony", "newer-member", "anthony:"},
		{"gene", "deprecation", "gene:"},
		{"milo", "missing", "milo:"},
	} {
		if assert.Contains(t, humanOut, line.needle) {
			got[pc{line.persona, line.cond}] = true
		}
	}
	assert.Equal(t, want, got)
}

// TestModelsCheckJSON_MultiCondition_TwoObjects covers the 5.5.A LOW: a single
// persona with two conditions must emit TWO array objects in JSON mode.
func TestModelsCheckJSON_MultiCondition_TwoObjects(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	withCatalogSnapshot(t, `[
  {"id":"google/gemini-pro-1.5","canonical_slug":"google/gemini-pro-1.5","created":100,"expiration_date":"2026-09-01"},
  {"id":"google/gemini-pro-2.0","canonical_slug":"google/gemini-pro-2.0","created":200,"expiration_date":null}
]`)
	writeCommunityPersona(t, dir, "gene", "google/gemini-pro-1.5", "")

	out, _, err := executeSplit(t, "models", "check", "--json")
	require.Error(t, err)
	fs := decodeFindings(t, out)
	require.Len(t, fs, 2)
	conds := map[string]bool{}
	for _, f := range fs {
		assert.Equal(t, "gene", f.Persona)
		conds[f.Condition] = true
	}
	assert.True(t, conds["newer-member"])
	assert.True(t, conds["deprecation"])
}

// TestRenderDriftJSON_EscapesControlChars covers the 5.5.A LOW: the --json path
// relies solely on stdlib escaping (it opts out of sanitizeDisplay), so a value
// with quotes/control chars must be safely escaped and round-trip cleanly.
func TestRenderDriftJSON_EscapesControlChars(t *testing.T) {
	var buf bytes.Buffer
	err := renderDriftJSON(&buf, []commpersonas.DriftFinding{{
		Persona:     "evil\x1b",
		Condition:   commpersonas.ConditionMissing,
		CurrentSlug: "vendor/model\x1b\"x\u2028",
	}})
	require.NoError(t, err)
	out := buf.String()
	assert.NotContains(t, out, "\x1b") // raw ESC never emitted
	assert.Contains(t, out, "\\u001b") // ESC escaped, not raw
	var fs []jsonFinding
	require.NoError(t, json.Unmarshal([]byte(out), &fs))
	require.Len(t, fs, 1)
	assert.Equal(t, "vendor/model\x1b\"x\u2028", fs[0].CurrentSlug) // round-trips
}

// --- Exit-code contract (AC 05-03): 0 clean / 1 conditions found / 2 failure ---

func TestModelsCheckExit_Clean_Zero(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	withCatalogSnapshot(t, driftFixtureCatalog)
	writeCommunityPersona(t, dir, "anthony", "anthropic/claude-opus-5.0", "")

	for _, args := range [][]string{{"models", "check"}, {"models", "check", "--json"}} {
		_, err := execute(t, args...)
		require.NoError(t, err, args)
		assert.Equal(t, 0, exitCode(err), args)
	}
}

func TestModelsCheckExit_ConditionsFound_One(t *testing.T) {
	installDriftFixture(t)
	for _, args := range [][]string{{"models", "check"}, {"models", "check", "--json"}} {
		_, err := execute(t, args...)
		require.Error(t, err, args)
		assert.Equal(t, exitFailure, exitCode(err), args) // 1, not 2
	}
}

func TestModelsCheckExit_UsageError_Two_NoReport(t *testing.T) {
	installDriftFixture(t)
	out, err := execute(t, "models", "check", "--not-a-real-flag")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err)) // 2
	// A usage error must not compute or print a drift report.
	assert.NotContains(t, out, "newer member")
	assert.NotContains(t, out, "No drift")
}

func TestModelsCheckExit_FindingsPlusReadFailure_StillOne(t *testing.T) {
	dir := installDriftFixture(t)
	// gene's lock is unreadable (an internal per-persona failure, not a usage
	// error); anthony + milo still yield findings.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gene.yaml"), []byte("::: not yaml :::\n"), 0o644))

	_, err := execute(t, "models", "check")
	require.Error(t, err)
	assert.Equal(t, exitFailure, exitCode(err)) // 1, not 2
}

func TestModelsCheckExit_MissingSnapshot_Two(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	writeCommunityPersona(t, dir, "anthony", "anthropic/claude-opus-4.8", "")
	t.Setenv("ATCR_CATALOG_SNAPSHOT", filepath.Join(t.TempDir(), "does-not-exist.json"))

	_, err := execute(t, "models", "check")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err)) // 2, a command failure — not "conditions found"
	assert.Contains(t, err.Error(), "failed to load catalog snapshot")
}

func TestModelsCheckExit_MalformedSnapshot_Two(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	writeCommunityPersona(t, dir, "anthony", "anthropic/claude-opus-4.8", "")
	bad := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(bad, []byte("{not valid json"), 0o644))
	t.Setenv("ATCR_CATALOG_SNAPSHOT", bad)

	_, err := execute(t, "models", "check")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "failed to parse catalog snapshot")
}

// TestModelsCheckExit_ReadFailureOnly_Zero covers the 5.8.A LOW: a per-persona
// read failure with NO valid findings still exits 0 (the check completed; the
// failure is advisory on stderr), identical in default and --json modes.
func TestModelsCheckExit_ReadFailureOnly_Zero(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	withCatalogSnapshot(t, driftFixtureCatalog)
	// The only community persona is unreadable → zero findings.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "anthony.yaml"), []byte("::: not yaml :::\n"), 0o644))

	out, _, err := executeSplit(t, "models", "check")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode(err))
	assert.Contains(t, out, "No drift, deprecation, or missing-slug conditions found.")

	jsonOut, _, jerr := executeSplit(t, "models", "check", "--json")
	require.NoError(t, jerr)
	assert.Equal(t, 0, exitCode(jerr))
	assert.Equal(t, "[]\n", jsonOut)
}

// --- Determinism + zero-network default path (AC 05-04) ---

// failRoundTripper fails the test if any HTTP request is attempted.
type failRoundTripper struct{ t *testing.T }

func (f failRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Error("unexpected network call in models check default path")
	return nil, fmt.Errorf("network blocked in test")
}

func TestModelsCheck_Deterministic_RepeatedRuns(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	// No ATCR_CATALOG_SNAPSHOT override → the embedded snapshot is used.
	// z-ai/glm-4.5 is deprecated AND behind glm-5.2 in the embedded snapshot.
	writeCommunityPersona(t, dir, "glenna", "z-ai/glm-4.5", "")

	out1, err1 := execute(t, "models", "check")
	out2, err2 := execute(t, "models", "check")
	assert.Equal(t, out1, out2, "repeated default runs must produce identical stdout")
	assert.Equal(t, exitCode(err1), exitCode(err2))
	assert.Equal(t, exitFailure, exitCode(err1)) // drift present

	j1, je1 := execute(t, "models", "check", "--json")
	j2, je2 := execute(t, "models", "check", "--json")
	assert.Equal(t, j1, j2, "repeated --json runs must produce identical stdout")
	assert.Equal(t, exitCode(je1), exitCode(je2))
}

func TestModelsCheck_DefaultPath_ZeroNetwork(t *testing.T) {
	dir := withEmptyPersonasDir(t)
	// anthropic/claude-opus-4.8 is present + newest in its (non-alias) family in the
	// embedded snapshot → a clean, no-drift run.
	writeCommunityPersona(t, dir, "anthony", "anthropic/claude-opus-4.8", "")

	oldClient := personasClient
	personasClient = &http.Client{Transport: failRoundTripper{t}}
	t.Cleanup(func() { personasClient = oldClient })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("catalog/personas endpoint hit in default check path")
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ATCR_CATALOG_URL", srv.URL)
	t.Setenv("ATCR_PERSONAS_URL", srv.URL)

	out, err := execute(t, "models", "check")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode(err))
	assert.Contains(t, out, "No drift, deprecation, or missing-slug conditions found.")
}
