package main

import (
	"encoding/json"
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

// keep encoding/json + httptest referenced until 5.4 / 5.10 use them directly.
var _ = json.Marshal
var _ = httptest.NewServer
var _ = http.StatusOK
