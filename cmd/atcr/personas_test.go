package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/personas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cmdValidPersonaYAML = `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
language:
  - go
version: "1.0.0"
description: "OWASP Top-10 security reviewer"
`

const cmdIndexJSON = `[
  {"name":"security/owasp","version":"1.0.0","description":"OWASP Top-10 security reviewer","path":"security/owasp.yaml"},
  {"name":"performance/tracer","version":"1.1.0","description":"Hot-path allocation finder","path":"performance/tracer.yaml"}
]`

// personasTestServer serves the given path→body map (200) and 404 otherwise.
func personasTestServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := routes[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// withPersonasEnv points the CLI at srv and a temp personas dir for the duration
// of the test, restoring the overrides afterward. Returns the temp dir.
func withPersonasEnv(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ATCR_PERSONAS_URL", srv.URL)
	oldDir := personasDir
	personasDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { personasDir = oldDir })
	return dir
}

func TestPersonas_HelpListsSubcommands(t *testing.T) {
	out, err := execute(t, "personas", "--help")
	require.NoError(t, err)
	for _, sub := range []string{"install", "list", "search", "remove", "test", "upgrade"} {
		assert.Contains(t, out, sub)
	}
}

func TestPersonasInstall_Integration(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/security/owasp.yaml": cmdValidPersonaYAML})
	dir := withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "install", "security/owasp")
	require.NoError(t, err)
	assert.Contains(t, out, "security/owasp")
	assert.FileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
}

func TestPersonasInstall_NotFoundExitsNonZero(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)

	_, err := execute(t, "personas", "install", "security/owasp")
	require.Error(t, err)
}

func TestPersonasList_Integration(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/security/owasp.yaml": cmdValidPersonaYAML})
	dir := withPersonasEnv(t, srv)
	require.NoError(t, personas.Install(http.DefaultClient, srv.URL, "security/owasp", dir))

	out, err := execute(t, "personas", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "bruce")          // built-in
	assert.Contains(t, out, "security/owasp") // community
	assert.Contains(t, out, "built-in")
	assert.Contains(t, out, "community")
}

func TestPersonasList_ScoresFlagAccepted(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	_, err := execute(t, "personas", "list", "--scores")
	require.NoError(t, err)
}

func TestPersonasSearch_Integration(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdIndexJSON})
	withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "search", "security")
	require.NoError(t, err)
	assert.Contains(t, out, "security/owasp")
	assert.NotContains(t, out, "performance/tracer")
}

func TestPersonasSearch_NoMatch(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/index.json": cmdIndexJSON})
	withPersonasEnv(t, srv)

	out, err := execute(t, "personas", "search", "quantum")
	require.NoError(t, err)
	assert.Contains(t, out, "No personas found")
}

func TestPersonasRemove_Integration(t *testing.T) {
	srv := personasTestServer(t, map[string]string{"/security/owasp.yaml": cmdValidPersonaYAML})
	dir := withPersonasEnv(t, srv)
	require.NoError(t, personas.Install(http.DefaultClient, srv.URL, "security/owasp", dir))

	out, err := execute(t, "personas", "remove", "security/owasp")
	require.NoError(t, err)
	assert.Contains(t, out, "Removed")
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
}

func TestPersonasRemove_BuiltinRejected(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	_, err := execute(t, "personas", "remove", "bruce")
	require.Error(t, err)
}

func TestPersonasUpgrade_Integration(t *testing.T) {
	newer := `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
version: "1.1.0"
`
	srv := personasTestServer(t, map[string]string{"/security/owasp.yaml": newer})
	dir := withPersonasEnv(t, srv)
	// Pre-install v1.0.0.
	p := filepath.Join(dir, "security", "owasp.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(cmdValidPersonaYAML), 0o644))

	out, err := execute(t, "personas", "upgrade", "security/owasp")
	require.NoError(t, err)
	assert.Contains(t, out, "1.1.0")
	got, _ := os.ReadFile(p)
	assert.Equal(t, newer, string(got))
}

// stubFixtureRunner lets the test drive the `test` subcommand's outcome without
// a live LLM.
type stubFixtureRunner struct{ outcome personas.FixtureOutcome }

func (s stubFixtureRunner) RunFixture(string) (personas.FixtureOutcome, error) {
	return s.outcome, nil
}

func withFixtureRunner(t *testing.T, r personas.FixtureRunner) {
	t.Helper()
	old := personasFixtureRunner
	personasFixtureRunner = r
	t.Cleanup(func() { personasFixtureRunner = old })
}

func TestPersonasTest_Pass(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 3, Total: 3}})

	out, err := execute(t, "personas", "test", "sentinel")
	require.NoError(t, err)
	assert.Contains(t, out, "PASS")
}

func TestPersonasTest_FailExitsNonZero(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: true, Passed: 2, Total: 3}})

	out, err := execute(t, "personas", "test", "sentinel")
	require.Error(t, err)
	assert.Contains(t, out, "FAIL")
}

func TestPersonasTest_NoFixture(t *testing.T) {
	srv := personasTestServer(t, map[string]string{})
	withPersonasEnv(t, srv)
	withFixtureRunner(t, stubFixtureRunner{personas.FixtureOutcome{HasFixture: false}})

	out, err := execute(t, "personas", "test", "sentinel")
	require.NoError(t, err)
	assert.Contains(t, out, "No fixture")
}
