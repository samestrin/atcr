package personas

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validPersonaYAML is a community persona document: a registry AgentConfig
// (provider/model/role/language) plus persona-file metadata (version,
// description) that the strict registry decoder does not know — ValidateAgentYAML
// must tolerate the metadata while still validating the agent fields.
const validPersonaYAML = `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
language:
  - go
version: "1.0.0"
description: "OWASP Top-10 security reviewer"
`

// invalidPersonaYAML omits the required model field, so validateAgent rejects it.
const invalidPersonaYAML = `provider: anthropic
role: reviewer
`

// fakeIndexJSON is a community index.json with entries across categories.
const fakeIndexJSON = `[
  {"name":"security/owasp","version":"1.0.0","description":"OWASP Top-10 security reviewer","path":"security/owasp.yaml"},
  {"name":"security/sans","version":"0.9.0","description":"SANS Top 25 patterns","path":"security/sans.yaml"},
  {"name":"performance/tracer","version":"1.1.0","description":"Hot-path allocation finder","path":"performance/tracer.yaml"}
]`

// testServer returns an httptest.Server that serves the given path→body map with
// HTTP 200, and 404 for everything else. Paths are matched on r.URL.Path.
func testServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- BaseURL / env override -------------------------------------------------

func TestBaseURL_DefaultWhenUnset(t *testing.T) {
	t.Setenv(envPersonasURL, "")
	assert.Equal(t, RegistryBaseURL, BaseURL())
}

func TestBaseURL_EnvOverride(t *testing.T) {
	t.Setenv(envPersonasURL, "http://localhost:9999/repo")
	assert.Equal(t, "http://localhost:9999/repo", BaseURL())
}

// --- validatePersonaName ----------------------------------------------------

func TestValidatePersonaName(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"security/owasp", false},
		{"idiomatic", false},
		{"a_b-c/d", false},
		{"../etc/passwd", true},
		{"security/../../etc", true},
		{"/abs/path", true},
		{"has space", true},
		{"", true},
		{"weird;rm", true},
	}
	for _, tc := range cases {
		err := validatePersonaName(tc.name)
		if tc.wantErr {
			assert.Errorf(t, err, "name %q should be rejected", tc.name)
		} else {
			assert.NoErrorf(t, err, "name %q should be accepted", tc.name)
		}
	}
}

// --- Install ----------------------------------------------------------------

func TestInstall_Success(t *testing.T) {
	srv := testServer(t, map[string]string{"/security/owasp.yaml": validPersonaYAML})
	dir := t.TempDir()

	err := Install(srv.Client(), srv.URL, "security/owasp", dir)
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(dir, "security", "owasp.yaml"))
	require.NoError(t, err)
	assert.Equal(t, validPersonaYAML, string(got))
}

func TestInstall_CreatesMissingDir(t *testing.T) {
	srv := testServer(t, map[string]string{"/security/owasp.yaml": validPersonaYAML})
	dir := filepath.Join(t.TempDir(), "does", "not", "exist")

	err := Install(srv.Client(), srv.URL, "security/owasp", dir)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
}

func TestInstall_NotFound(t *testing.T) {
	srv := testServer(t, map[string]string{})
	err := Install(srv.Client(), srv.URL, "security/owasp", t.TempDir())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPersonaNotFound)
}

func TestInstall_InvalidYAMLNotWritten(t *testing.T) {
	srv := testServer(t, map[string]string{"/bad.yaml": invalidPersonaYAML})
	dir := t.TempDir()

	err := Install(srv.Client(), srv.URL, "bad", dir)
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "bad.yaml"))
}

func TestInstall_RejectsTraversalName(t *testing.T) {
	srv := testServer(t, map[string]string{})
	err := Install(srv.Client(), srv.URL, "../escape", t.TempDir())
	require.Error(t, err)
}

func TestInstall_OverwritesExisting(t *testing.T) {
	srv := testServer(t, map[string]string{"/security/owasp.yaml": validPersonaYAML})
	dir := t.TempDir()
	require.NoError(t, Install(srv.Client(), srv.URL, "security/owasp", dir))
	// Second install with the same name succeeds (re-install / overwrite).
	require.NoError(t, Install(srv.Client(), srv.URL, "security/owasp", dir))
}

// --- List -------------------------------------------------------------------

func TestList_BuiltinsOnlyWhenDirMissing(t *testing.T) {
	metas, err := List(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	// All nine built-ins present, source built-in.
	var builtins int
	for _, m := range metas {
		if m.Source == "built-in" {
			builtins++
		}
	}
	assert.Equal(t, 9, builtins)
}

func TestList_IncludesCommunityWithMetadata(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "security", "owasp.yaml"), []byte(validPersonaYAML), 0o644))
	// A non-YAML file must be skipped.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("junk"), 0o644))

	metas, err := List(dir)
	require.NoError(t, err)

	var owasp *PersonaMeta
	for i := range metas {
		if metas[i].Name == "security/owasp" {
			owasp = &metas[i]
		}
	}
	require.NotNil(t, owasp, "community persona must be listed")
	assert.Equal(t, "community", owasp.Source)
	assert.Equal(t, "1.0.0", owasp.Version)
	assert.Equal(t, []string{"go"}, owasp.Language)
}

// --- Search -----------------------------------------------------------------

func TestSearch_FiltersByKeyword(t *testing.T) {
	srv := testServer(t, map[string]string{"/index.json": fakeIndexJSON})
	got, err := Search(srv.Client(), srv.URL, "security")
	require.NoError(t, err)
	require.Len(t, got, 2)
	names := []string{got[0].Name, got[1].Name}
	assert.Contains(t, names, "security/owasp")
	assert.Contains(t, names, "security/sans")
}

func TestSearch_CaseInsensitiveAndDescription(t *testing.T) {
	srv := testServer(t, map[string]string{"/index.json": fakeIndexJSON})
	got, err := Search(srv.Client(), srv.URL, "HOT-PATH")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "performance/tracer", got[0].Name)
}

func TestSearch_NoMatchEmpty(t *testing.T) {
	srv := testServer(t, map[string]string{"/index.json": fakeIndexJSON})
	got, err := Search(srv.Client(), srv.URL, "quantum")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSearch_IndexNotFound(t *testing.T) {
	srv := testServer(t, map[string]string{})
	_, err := Search(srv.Client(), srv.URL, "x")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrIndexNotFound)
}

func TestSearch_MalformedJSON(t *testing.T) {
	srv := testServer(t, map[string]string{"/index.json": "{not json"})
	_, err := Search(srv.Client(), srv.URL, "x")
	require.Error(t, err)
}

// --- Remove -----------------------------------------------------------------

func TestRemove_Success(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "security", "owasp.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(validPersonaYAML), 0o644))

	require.NoError(t, Remove("security/owasp", dir))
	assert.NoFileExists(t, p)
}

func TestRemove_NotInstalled(t *testing.T) {
	err := Remove("security/owasp", t.TempDir())
	require.Error(t, err)
}

func TestRemove_RejectsBuiltin(t *testing.T) {
	err := Remove("bruce", t.TempDir())
	require.Error(t, err)
}

func TestRemove_RejectsTraversal(t *testing.T) {
	err := Remove("../etc/passwd", t.TempDir())
	require.Error(t, err)
}

// --- Upgrade ----------------------------------------------------------------

func installFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name)+".yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

func TestUpgrade_RemoteNewer(t *testing.T) {
	remote := `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
version: "1.1.0"
`
	srv := testServer(t, map[string]string{"/security/owasp.yaml": remote})
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML) // v1.0.0

	res, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", false)
	require.NoError(t, err)
	assert.True(t, res.Upgraded)
	assert.Equal(t, "1.0.0", res.FromVersion)
	assert.Equal(t, "1.1.0", res.ToVersion)

	got, _ := os.ReadFile(filepath.Join(dir, "security", "owasp.yaml"))
	assert.Equal(t, remote, string(got))
}

func TestUpgrade_AlreadyCurrent(t *testing.T) {
	srv := testServer(t, map[string]string{"/security/owasp.yaml": validPersonaYAML})
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML) // same v1.0.0

	res, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", false)
	require.NoError(t, err)
	assert.True(t, res.UpToDate)
	assert.False(t, res.Upgraded)
}

func TestUpgrade_DryRunNoWrite(t *testing.T) {
	remote := `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
version: "1.1.0"
`
	srv := testServer(t, map[string]string{"/security/owasp.yaml": remote})
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML)

	res, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", true)
	require.NoError(t, err)
	assert.True(t, res.Upgraded) // would upgrade
	got, _ := os.ReadFile(filepath.Join(dir, "security", "owasp.yaml"))
	assert.Equal(t, validPersonaYAML, string(got)) // unchanged on disk
}

func TestUpgrade_InvalidRemoteNotWritten(t *testing.T) {
	srv := testServer(t, map[string]string{"/security/owasp.yaml": invalidPersonaYAML})
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML)

	_, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", false)
	require.Error(t, err)
	got, _ := os.ReadFile(filepath.Join(dir, "security", "owasp.yaml"))
	assert.Equal(t, validPersonaYAML, string(got)) // local untouched
}

func TestUpgrade_NotInstalled(t *testing.T) {
	srv := testServer(t, map[string]string{"/security/owasp.yaml": validPersonaYAML})
	_, err := Upgrade(srv.Client(), srv.URL, t.TempDir(), "security/owasp", false)
	require.Error(t, err)
}

// --- TestPersona (fixture runner delegation) --------------------------------

type stubRunner struct {
	outcome FixtureOutcome
	err     error
}

func (s stubRunner) RunFixture(string) (FixtureOutcome, error) { return s.outcome, s.err }

func TestTestPersona_PassDelegates(t *testing.T) {
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML)
	out, err := TestPersona(dir, "security/owasp", stubRunner{outcome: FixtureOutcome{HasFixture: true, Passed: 3, Total: 3}})
	require.NoError(t, err)
	assert.Equal(t, 3, out.Passed)
	assert.Equal(t, 3, out.Total)
}

func TestTestPersona_BuiltinResolves(t *testing.T) {
	out, err := TestPersona(t.TempDir(), "sentinel", stubRunner{outcome: FixtureOutcome{HasFixture: true, Passed: 1, Total: 1}})
	require.NoError(t, err)
	assert.True(t, out.HasFixture)
}

func TestTestPersona_UnknownPersona(t *testing.T) {
	_, err := TestPersona(t.TempDir(), "security/nope", stubRunner{})
	require.Error(t, err)
}

// --- fetch timeout ----------------------------------------------------------

func TestFetch_TimesOutOnSlowServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client context is cancelled (e.g. due to timeout) so
		// srv.Close() can complete without a 5-second hang.
		<-r.Context().Done()
	}))
	defer srv.Close()

	old := fetchTimeout
	fetchTimeout = 50 * time.Millisecond
	defer func() { fetchTimeout = old }()

	_, err := fetch(srv.Client(), srv.URL+"/test.yaml", errors.New("not found"))
	require.Error(t, err, "fetch must return an error when the server does not respond")
}

// --- fetch body size limit --------------------------------------------------

func TestFetch_RejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]byte, fetchBodyLimit+2)
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	_, err := fetch(srv.Client(), srv.URL+"/big.yaml", errors.New("not found"))
	require.Error(t, err, "fetch must reject a body larger than fetchBodyLimit")
	assert.Contains(t, err.Error(), "limit")
}
