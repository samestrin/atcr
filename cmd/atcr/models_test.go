package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// --- Story 08 (AC 08-02): `atcr models refresh` regenerates the snapshot from a
// live fetch; maintainer-initiated, never CI-invoked -------------------------

// fakeCatalogServer serves catalogBody at every path and points ATCR_CATALOG_URL
// at it (the resolver/refresh catalog injection seam), so `models refresh` fetches
// the fake instead of live OpenRouter and CI stays zero-live-network. A non-2xx
// status models a fetch failure. Setting ATCR_CATALOG_URL also marks the run as a
// test/override (not the live default), which bypasses the API-key gate.
func fakeCatalogServer(t *testing.T, status int, catalogBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != 0 && status/100 != 2 {
			http.Error(w, catalogBody, status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(catalogBody))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ATCR_CATALOG_URL", srv.URL)
	return srv
}

// refreshCatalogBody is a small but structurally complete {"data":[...]} catalog:
// a pinned slug (null expiration), an alias entry, and an expiring z-ai/ entry
// (non-null expiration) — enough to prove the written fixture round-trips through
// the resolver's parser with both expiration_date shapes preserved.
const refreshCatalogBody = `{"data":[
  {"id":"anthropic/claude-opus-4.8","canonical_slug":"anthropic/claude-opus-4.8","created":1776000000,"expiration_date":null},
  {"id":"~openai/gpt-latest","canonical_slug":"openai/gpt-latest","created":1783000002,"expiration_date":null},
  {"id":"z-ai/glm-4.5","canonical_slug":"z-ai/glm-4.5","created":1760000000,"expiration_date":"2026-12-31"}
]}`

func TestModelsRefresh_IsRegistered(t *testing.T) {
	out, err := execute(t, "models", "refresh", "--help")
	require.NoError(t, err)
	assert.NotContains(t, out, "unknown command")
	assert.Contains(t, out, "refresh")
}

// TestModelsRefresh_WritesFixtureFromLiveFetch covers AC 08-02 Scenario 1 + 2: the
// command fetches /models, writes the array to --output preserving readable
// indentation, prints a confirmation naming the path + model count, and the
// written file round-trips through the resolver's parser (SnapshotModels).
func TestModelsRefresh_WritesFixtureFromLiveFetch(t *testing.T) {
	fakeCatalogServer(t, http.StatusOK, refreshCatalogBody)
	out := filepath.Join(t.TempDir(), "catalog_snapshot.json")

	stdout, err := execute(t, "models", "refresh", "--output", out)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode(err))

	// Confirmation names the output path and the number of models written.
	assert.Contains(t, stdout, out, "confirmation must name the output path")
	assert.Contains(t, stdout, "3", "confirmation must report the model count")

	written, rerr := os.ReadFile(out)
	require.NoError(t, rerr, "the command must write the snapshot file")
	assert.Contains(t, string(written), "anthropic/claude-opus-4.8")
	assert.Contains(t, string(written), "  ", "written JSON must preserve readable indentation")
	// The refreshed file re-emits the self-documenting provenance header (ignored on
	// read) so a refresh does not silently strip the checked-in fixture's note.
	assert.Contains(t, string(written), "_fixture_meta", "written snapshot must carry a provenance header")
	assert.Contains(t, string(written), "/models", "provenance must name the catalog source")

	// Round-trip: the written file must parse through the same code path the
	// resolver tests use, proving structural compatibility (AC 08-02 Scenario 2).
	// Assert field fidelity — not just the count — including nil vs non-null
	// expiration_date survival, so a MarshalSnapshot regression cannot pass silently.
	t.Setenv("ATCR_CATALOG_SNAPSHOT", out)
	models, serr := commpersonas.SnapshotModels()
	require.NoError(t, serr)
	require.Len(t, models, 3)
	byID := make(map[string]commpersonas.CatalogModel, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}
	opus, ok := byID["anthropic/claude-opus-4.8"]
	require.True(t, ok)
	assert.Equal(t, "anthropic/claude-opus-4.8", opus.CanonicalSlug, "canonical_slug survives")
	assert.Equal(t, int64(1776000000), opus.Created, "created survives")
	assert.Nil(t, opus.ExpirationDate, "null expiration_date survives as nil")
	glm, ok := byID["z-ai/glm-4.5"]
	require.True(t, ok)
	require.NotNil(t, glm.ExpirationDate, "non-null expiration_date survives as non-nil")
	assert.Equal(t, "2026-12-31", *glm.ExpirationDate, "expiration_date value survives")
}

// TestModelsRefresh_RefusesEmptyCatalog covers AC 08-02 Edge Case 1: an empty data
// array — and a substanceless blank-entry payload — are refused with a clear error
// and exit 2; no file is written.
func TestModelsRefresh_RefusesEmptyCatalog(t *testing.T) {
	for name, body := range map[string]string{
		"empty array":    `{"data":[]}`,
		"blank entry":    `{"data":[{}]}`,
		"empty-id entry": `{"data":[{"id":"","canonical_slug":"","created":0,"expiration_date":null}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			fakeCatalogServer(t, http.StatusOK, body)
			out := filepath.Join(t.TempDir(), "catalog_snapshot.json")

			stdout, err := execute(t, "models", "refresh", "--output", out)
			require.Error(t, err)
			assert.Equal(t, exitUsage, exitCode(err))
			assert.Contains(t, err.Error()+stdout, "empty")
			_, statErr := os.Stat(out)
			assert.True(t, os.IsNotExist(statErr), "no file must be written on an empty catalog")
		})
	}
}

// TestModelsRefresh_FetchFailure_LeavesFixtureUntouched covers AC 08-02 Edge Case 2:
// a non-2xx fetch leaves any existing fixture unchanged and exits 2.
func TestModelsRefresh_FetchFailure_LeavesFixtureUntouched(t *testing.T) {
	fakeCatalogServer(t, http.StatusInternalServerError, "upstream boom")
	out := filepath.Join(t.TempDir(), "catalog_snapshot.json")
	require.NoError(t, os.WriteFile(out, []byte("PRIOR-CONTENT"), 0o644))

	_, err := execute(t, "models", "refresh", "--output", out)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))

	after, rerr := os.ReadFile(out)
	require.NoError(t, rerr)
	assert.Equal(t, "PRIOR-CONTENT", string(after), "existing fixture must be untouched on fetch failure")
}

// TestModelsRefresh_MissingAPIKey_LiveDefault_Exit2 covers AC 08-02 Error Scenario 1:
// on the live default path (no ATCR_CATALOG_URL override, not under CI) a missing
// OPENROUTER_API_KEY fails closed with exit 2 and the pinned message — no file
// written, no network.
func TestModelsRefresh_MissingAPIKey_LiveDefault_Exit2(t *testing.T) {
	// No ATCR_CATALOG_URL → the live default path; clear CI so the key gate (not the
	// CI guard) is the one exercised; ensure no key is present.
	t.Setenv("ATCR_CATALOG_URL", "")
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	out := filepath.Join(t.TempDir(), "catalog_snapshot.json")

	_, err := execute(t, "models", "refresh", "--output", out)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "OPENROUTER_API_KEY is required to refresh the catalog snapshot")
	_, statErr := os.Stat(out)
	assert.True(t, os.IsNotExist(statErr), "no file must be written when the key is missing")
}

// TestModelsRefresh_RefusesInCI covers the never-CI-invoked guard: on the live path
// with CI set, refresh fails closed (exit 2) even if a key is present — so CI that
// exports OPENROUTER_API_KEY can still never fetch live. No file written.
func TestModelsRefresh_RefusesInCI(t *testing.T) {
	t.Setenv("ATCR_CATALOG_URL", "") // live path
	t.Setenv("CI", "true")
	t.Setenv("OPENROUTER_API_KEY", "present-but-ignored")
	out := filepath.Join(t.TempDir(), "catalog_snapshot.json")

	_, err := execute(t, "models", "refresh", "--output", out)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "must not run in CI")
	_, statErr := os.Stat(out)
	assert.True(t, os.IsNotExist(statErr), "no file must be written under CI")
}

// TestModelsRefresh_DefaultOutputUnderOverride_Requires_Output covers the
// default-output guard: with an ATCR_CATALOG_URL override set and no --output, the
// command refuses (exit 2) rather than rewriting the checked-in default fixture from
// an override catalog.
func TestModelsRefresh_DefaultOutputUnderOverride_Requires_Output(t *testing.T) {
	fakeCatalogServer(t, http.StatusOK, refreshCatalogBody) // sets ATCR_CATALOG_URL

	_, err := execute(t, "models", "refresh") // no --output
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "--output is required")
}

// TestModelsRefresh_WriteFailure_PreservesExistingFixture covers AC 08-02
// requirement (3): a failed write leaves an EXISTING fixture byte-for-byte intact
// (the atomic temp+rename never truncates the target). The write is forced to fail
// by making the output's directory read-only after seeding a prior fixture.
func TestModelsRefresh_WriteFailure_PreservesExistingFixture(t *testing.T) {
	fakeCatalogServer(t, http.StatusOK, refreshCatalogBody)
	dir := t.TempDir()
	out := filepath.Join(dir, "catalog_snapshot.json")
	const prior = `{"data":[{"id":"old/model","canonical_slug":"old/model","created":1,"expiration_date":null}]}`
	require.NoError(t, os.WriteFile(out, []byte(prior), 0o644))

	require.NoError(t, os.Chmod(dir, 0o500)) // read-only dir → temp create fails
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := execute(t, "models", "refresh", "--output", out)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))

	require.NoError(t, os.Chmod(dir, 0o700))
	after, rerr := os.ReadFile(out)
	require.NoError(t, rerr)
	assert.Equal(t, prior, string(after), "existing fixture must survive a failed write byte-for-byte")
}

// TestModelsRefresh_UnwritablePath_Exit2 covers AC 08-02 Error Scenario 2: an
// unwritable output path surfaces a wrapped write error naming the path, exits 2.
func TestModelsRefresh_UnwritablePath_Exit2(t *testing.T) {
	fakeCatalogServer(t, http.StatusOK, refreshCatalogBody)
	// A path whose parent is a regular file, so os.WriteFile cannot create it.
	parent := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(parent, []byte("x"), 0o644))
	out := filepath.Join(parent, "catalog_snapshot.json")

	_, err := execute(t, "models", "refresh", "--output", out)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

// mockCatalogRoundTripper returns a canned 200 OK catalog body, keeping the
// `models refresh` live-path tests zero-network.
type mockCatalogRoundTripper struct {
	t    *testing.T
	body string
}

func (m mockCatalogRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(m.body)),
	}, nil
}

// TestModelsRefresh_CIFalse_AllowsRun covers the never-CI-invoked guard:
// a CI variable set to a falsy value ("false", "0", or empty) must NOT be
// treated as "in CI", so a maintainer exporting CI=false can still refresh.
func TestModelsRefresh_CIFalse_AllowsRun(t *testing.T) {
	t.Setenv("ATCR_CATALOG_URL", "") // live path, so the CI gate is exercised
	t.Setenv("OPENROUTER_API_KEY", "present")

	oldClient := personasClient
	personasClient = &http.Client{Transport: mockCatalogRoundTripper{t: t, body: refreshCatalogBody}}
	t.Cleanup(func() { personasClient = oldClient })

	for _, ci := range []string{"false", "0", ""} {
		t.Run("CI="+ci, func(t *testing.T) {
			t.Setenv("CI", ci)
			t.Setenv("GITHUB_ACTIONS", "")

			out := filepath.Join(t.TempDir(), "catalog_snapshot.json")
			stdout, err := execute(t, "models", "refresh", "--output", out)
			require.NoError(t, err)
			assert.Equal(t, 0, exitCode(err))
			assert.Contains(t, stdout, "3", "confirmation must report the model count")
		})
	}
}
