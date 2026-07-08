package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commpersonas "github.com/samestrin/atcr/internal/personas"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/personas"
)

var personaNames = []string{"bruce", "greta", "kai", "mira", "dax", "sasha", "penny", "ingrid", "otto"}

// initDir runs a fresh init into dir, failing the test on error.
func initDir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, runInit(dir, false, &bytes.Buffer{}, &bytes.Buffer{}))
}

func TestInit_FreshDirectory(t *testing.T) {
	dir := t.TempDir()
	out := &bytes.Buffer{}
	require.NoError(t, runInit(dir, false, out, &bytes.Buffer{}))

	// Config exists, parses strictly, and carries the documented defaults.
	cfg, err := registry.LoadProjectConfig(filepath.Join(dir, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, personaNames, cfg.Agents, "default roster lists all nine personas")
	assert.Equal(t, "blocks", cfg.PayloadMode)
	require.NotNil(t, cfg.TimeoutSecs)
	assert.Equal(t, 600, *cfg.TimeoutSecs)
	assert.Equal(t, "HIGH", cfg.FailOn)
	require.NotNil(t, cfg.MaxParallel, "template must carry max_parallel so the knob is visible")
	assert.Equal(t, registry.DefaultMaxParallel, *cfg.MaxParallel)

	// Nine persona files plus the base template.
	for _, name := range append([]string{"_base"}, personaNames...) {
		path := filepath.Join(dir, ".atcr", "personas", name+".md")
		assert.FileExists(t, path)
	}

	// Success message lists created files.
	for _, want := range []string{"config.yaml", "bruce.md", "_base.md"} {
		assert.Contains(t, out.String(), want)
	}
}

// TestInit_WritesGitignore: `atcr init` drops a .atcr/.gitignore so the runtime
// outputs atcr writes under .atcr/ (the diff cache, up to cache_max_bytes, and
// reviewer outputs) are ignored by git out of the box — even for end users who
// never manually ignore .atcr/. The editable config.yaml and personas/ alongside
// it stay tracked.
func TestInit_WritesGitignore(t *testing.T) {
	dir := t.TempDir()
	initDir(t, dir)

	data, err := os.ReadFile(filepath.Join(dir, ".atcr", ".gitignore"))
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "cache/", "the diff cache dir must be ignored")
	assert.Contains(t, content, "reviews/", "the reviews dir must be ignored")
}

func TestInit_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := t.TempDir()
	initDir(t, dir)

	dirInfo, err := os.Stat(filepath.Join(dir, ".atcr", "personas"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), dirInfo.Mode().Perm(), "directories use 0755")

	fileInfo, err := os.Stat(filepath.Join(dir, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), fileInfo.Mode().Perm(), "files use 0644")
}

func TestInit_PersonaContent(t *testing.T) {
	dir := t.TempDir()
	initDir(t, dir)

	for _, name := range append([]string{"_base"}, personaNames...) {
		data, err := os.ReadFile(filepath.Join(dir, ".atcr", "personas", name+".md"))
		require.NoError(t, err)
		content := string(data)
		for _, header := range []string{"## Role", "## Focus", "## Severity Rubric", "## Output Format"} {
			assert.Contains(t, content, header, "%s.md must contain %s", name, header)
		}
		assert.Contains(t, content, "{{.Payload}}", "%s.md must reference the payload placeholder", name)
		assert.Contains(t, content, "{{.AgentName}}", "%s.md must reference the agent-name placeholder", name)
	}
}

func TestInit_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	initDir(t, dir)

	// Tamper with a persona so we can prove nothing is modified.
	brucePath := filepath.Join(dir, ".atcr", "personas", "bruce.md")
	require.NoError(t, os.WriteFile(brucePath, []byte("EDITED"), 0o644))

	err := runInit(dir, false, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "existing files would be overwritten: .atcr/config.yaml")
	assert.Contains(t, err.Error(), "--force")

	data, err := os.ReadFile(brucePath)
	require.NoError(t, err)
	assert.Equal(t, "EDITED", string(data), "existing files must not be modified without --force")
}

func TestInit_GuardCoversPersonasWithoutConfig(t *testing.T) {
	// Customized personas must survive even when config.yaml is missing
	// (e.g. deleted, or a previous init failed midway).
	dir := t.TempDir()
	initDir(t, dir)
	require.NoError(t, os.Remove(filepath.Join(dir, ".atcr", "config.yaml")))

	brucePath := filepath.Join(dir, ".atcr", "personas", "bruce.md")
	require.NoError(t, os.WriteFile(brucePath, []byte("CUSTOMIZED"), 0o644))

	err := runInit(dir, false, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err, "existing persona files must trigger the overwrite guard")
	assert.Contains(t, err.Error(), "bruce.md", "error should name the actual existing file")
	assert.NotContains(t, err.Error(), "config already exists at .atcr/config.yaml", "error must not falsely claim config.yaml exists")

	data, err := os.ReadFile(brucePath)
	require.NoError(t, err)
	assert.Equal(t, "CUSTOMIZED", string(data))
}

// TestInit_Force_PreservesEditedPersonas covers AC 01-05 Scenario 1 (LOCKED via
// Phase 3 Clarification Q1): `atcr init --force` regenerates config.yaml but must
// NEVER overwrite an existing persona file. --force only bypasses the top-level
// "config already exists" gate.
func TestInit_Force_PreservesEditedPersonas(t *testing.T) {
	dir := t.TempDir()
	initDir(t, dir)

	brucePath := filepath.Join(dir, ".atcr", "personas", "bruce.md")
	require.NoError(t, os.WriteFile(brucePath, []byte("EDITED"), 0o644))

	require.NoError(t, runInit(dir, true, &bytes.Buffer{}, &bytes.Buffer{}))

	data, err := os.ReadFile(brucePath)
	require.NoError(t, err)
	assert.Equal(t, "EDITED", string(data), "--force must NOT overwrite an existing persona file")
	// Non-persona targets are still regenerated under --force.
	assert.FileExists(t, filepath.Join(dir, ".atcr", "config.yaml"))
}

// TestInit_ForcePreservesExistingSymlinkPersona: an existing persona that is a
// symlink is preserved (skipped) under --force — never written through — so an
// external target is untouched (the security invariant) and the user's file
// stays as-is (the preservation invariant).
func TestInit_ForcePreservesExistingSymlinkPersona(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	dir := t.TempDir()
	initDir(t, dir)

	// Replace a persona with a symlink to an external file.
	external := filepath.Join(t.TempDir(), "external.md")
	require.NoError(t, os.WriteFile(external, []byte("EXTERNAL"), 0o644))
	brucePath := filepath.Join(dir, ".atcr", "personas", "bruce.md")
	require.NoError(t, os.Remove(brucePath))
	require.NoError(t, os.Symlink(external, brucePath))

	require.NoError(t, runInit(dir, true, &bytes.Buffer{}, &bytes.Buffer{}))

	data, err := os.ReadFile(external)
	require.NoError(t, err)
	assert.Equal(t, "EXTERNAL", string(data), "symlink target must never be written through")

	info, err := os.Lstat(brucePath)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "existing persona (a symlink) is preserved, not overwritten")
}

func TestInit_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("requires POSIX permissions and non-root user")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := runInit(dir, false, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create .atcr/")
}

func TestInit_CommandWiring(t *testing.T) {
	// `atcr init` must run against the working directory; the test only
	// verifies the flag plumbing by running in a temp cwd. The command also
	// fetch-and-pins community personas, so point it at a mock registry and a
	// throwaway pin dir to keep the test network-free.
	dir := t.TempDir()
	t.Chdir(dir)

	index := `[{"name":"bruce","version":"1.0.0","description":"d","path":"bruce.yaml"}]`
	srv := unitServer(t, index, map[string]string{"/bruce.yaml": communityUnitYAML})
	t.Setenv("ATCR_PERSONAS_URL", srv.URL)
	pinDir := t.TempDir()
	oldDir := personasDir
	personasDir = func() (string, error) { return pinDir, nil }
	t.Cleanup(func() { personasDir = oldDir })

	_, err := execute(t, "init")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, ".atcr", "config.yaml"))

	_, err = execute(t, "init")
	require.Error(t, err, "second init without --force must fail")

	_, err = execute(t, "init", "--force")
	require.NoError(t, err)
}

// --- AC 01-03: --offline flag fallback --------------------------------------

// failingHTTPClient fails the test if any HTTP request is attempted. It proves a
// code path (the --offline fallback) makes zero network calls.
type failingHTTPClient struct{ t *testing.T }

func (c failingHTTPClient) Do(*http.Request) (*http.Response, error) {
	c.t.Helper()
	c.t.Fatal("unexpected network call: the offline path must make zero network calls")
	return nil, nil
}

// TestInit_OfflineFlag_Registered: `atcr init` exposes an --offline flag.
func TestInit_OfflineFlag_Registered(t *testing.T) {
	require.NotNil(t, newInitCmd().Flags().Lookup("offline"), "--offline registered on init")
}

// TestInit_Offline_ZeroNetworkFallsBackToBuiltins covers AC 01-03: `atcr init
// --offline` skips the community fetch entirely (zero network) and still writes
// the embedded built-in scaffold.
func TestInit_Offline_ZeroNetworkFallsBackToBuiltins(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	oldClient := personasClient
	personasClient = failingHTTPClient{t}
	t.Cleanup(func() { personasClient = oldClient })

	_, err := execute(t, "init", "--offline")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, ".atcr", "config.yaml"))
	assert.FileExists(t, filepath.Join(dir, ".atcr", "personas", "bruce.md"),
		"embedded built-in personas still scaffolded offline")
}

// --- AC 01-02: init/quickstart fetch-and-pin --------------------------------

// unitServer serves a mock community registry: index.json plus the per-path
// bodies given (persona .yaml / .md). Anything else 404s.
func unitServer(t *testing.T, index string, files map[string]string) *httptest.Server {
	t.Helper()
	routes := map[string]string{"/index.json": index}
	for p, body := range files {
		routes[p] = body
	}
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

const communityUnitYAML = `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
language:
  - go
version: "1.2.0"
description: "OWASP Top-10 security reviewer"
`

const communityUnitMD = "# Security Reviewer\nReview the diff for OWASP Top-10 issues.\n"

// TestInstallCommunityPersonas_FetchAndPin covers AC 01-02 Scenario 1: a roster
// persona present in the community index installs as a co-located unit
// (<name>.yaml + <name>.md) into the community pin dir, pinned to the fetched
// YAML's version and labeled community by `personas list`.
func TestInstallCommunityPersonas_FetchAndPin(t *testing.T) {
	index := `[{"name":"owasp","version":"1.2.0","description":"sec","path":"owasp.yaml","provider":"anthropic","model":"claude-sonnet-4-6"}]`
	srv := unitServer(t, index, map[string]string{
		"/owasp.yaml": communityUnitYAML,
		"/owasp.md":   communityUnitMD,
	})
	dest := t.TempDir()
	out := &bytes.Buffer{}

	require.NoError(t, installCommunityPersonas(srv.Client(), srv.URL, dest, []string{"owasp"}, out, &bytes.Buffer{}))

	assert.FileExists(t, filepath.Join(dest, "owasp.yaml"))
	assert.FileExists(t, filepath.Join(dest, "owasp.md"))

	metas, err := commpersonas.List(dest)
	require.NoError(t, err)
	var found bool
	for _, m := range metas {
		if m.Name == "owasp" {
			found = true
			assert.Equal(t, "1.2.0", m.Version, "pin is the fetched YAML version")
			assert.Equal(t, "community", m.Source)
		}
	}
	assert.True(t, found, "owasp is listed as a community persona")
	assert.Contains(t, out.String(), "owasp", "install progress reported per persona")
}

// TestInstallCommunityPersonas_EmptyIndexErrors covers AC 01-02 Scenario 5: an
// empty index is a hard, non-silent error and nothing is written.
func TestInstallCommunityPersonas_EmptyIndexErrors(t *testing.T) {
	srv := unitServer(t, "[]", nil)
	dest := t.TempDir()

	err := installCommunityPersonas(srv.Client(), srv.URL, dest, []string{"owasp"}, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)

	entries, rerr := os.ReadDir(dest)
	require.NoError(t, rerr)
	assert.Empty(t, entries, "no persona files written when the index is empty")
}

// TestInstallCommunityPersonas_MissingRosterSkipsWithWarning covers AC 01-02
// Edge Case 1: a roster persona the index does not advertise is skipped with a
// warning; present ones still install and the call succeeds (exit 0).
func TestInstallCommunityPersonas_MissingRosterSkipsWithWarning(t *testing.T) {
	index := `[{"name":"owasp","version":"1.2.0","description":"sec","path":"owasp.yaml"}]`
	srv := unitServer(t, index, map[string]string{"/owasp.yaml": communityUnitYAML})
	dest := t.TempDir()
	errOut := &bytes.Buffer{}

	err := installCommunityPersonas(srv.Client(), srv.URL, dest, []string{"owasp", "penny"}, &bytes.Buffer{}, errOut)
	require.NoError(t, err, "a missing roster persona is skip-with-warning, not a hard failure")

	assert.FileExists(t, filepath.Join(dest, "owasp.yaml"))
	assert.NoFileExists(t, filepath.Join(dest, "tracer.yaml"))
	assert.Contains(t, errOut.String(), "penny", "the skipped persona is named in the warning")
}

// TestInstallCommunityPersonas_SkipsExistingUnit covers AC 01-05: the
// fetch-and-pin path never overwrites an existing on-disk persona; a missing one
// still installs alongside it.
func TestInstallCommunityPersonas_SkipsExistingUnit(t *testing.T) {
	index := `[
	  {"name":"owasp","version":"1.2.0","description":"d","path":"owasp.yaml"},
	  {"name":"sans","version":"1.0.0","description":"d","path":"sans.yaml"}
	]`
	srv := unitServer(t, index, map[string]string{
		"/owasp.yaml": communityUnitYAML,
		"/sans.yaml":  communityUnitYAML,
	})
	dest := t.TempDir()
	// A hand-edited persona already on disk must survive untouched.
	require.NoError(t, os.WriteFile(filepath.Join(dest, "owasp.yaml"), []byte("HANDEDITED"), 0o644))

	require.NoError(t, installCommunityPersonas(srv.Client(), srv.URL, dest, []string{"owasp", "sans"}, &bytes.Buffer{}, &bytes.Buffer{}))

	got, err := os.ReadFile(filepath.Join(dest, "owasp.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "HANDEDITED", string(got), "existing persona not overwritten by fetch-and-pin")
	assert.FileExists(t, filepath.Join(dest, "sans.yaml"), "missing persona still installed alongside")
}

// TestInstallCommunityPersonas_SkipsLoneExistingMD: a hand-edited <name>.md with
// no sibling .yaml is still treated as "already installed" and left untouched —
// the fetch-and-pin path never clobbers user prompt content.
func TestInstallCommunityPersonas_SkipsLoneExistingMD(t *testing.T) {
	index := `[{"name":"owasp","version":"1.2.0","description":"d","path":"owasp.yaml"}]`
	srv := unitServer(t, index, map[string]string{
		"/owasp.yaml": communityUnitYAML,
		"/owasp.md":   communityUnitMD,
	})
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dest, "owasp.md"), []byte("HANDEDITED PROMPT"), 0o644))

	require.NoError(t, installCommunityPersonas(srv.Client(), srv.URL, dest, []string{"owasp"}, &bytes.Buffer{}, &bytes.Buffer{}))

	got, err := os.ReadFile(filepath.Join(dest, "owasp.md"))
	require.NoError(t, err)
	assert.Equal(t, "HANDEDITED PROMPT", string(got), "lone hand-edited .md not overwritten")
	assert.NoFileExists(t, filepath.Join(dest, "owasp.yaml"), "install skipped entirely for an existing unit")
}

// --- AC 01-04: fetch-failure error handling ---------------------------------

// statusServer serves the given path→body map (HTTP 200), path→status overrides
// with the given status code and no body, and 404 for anything else.
func statusServer(t *testing.T, bodies map[string]string, statuses map[string]int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if code, ok := statuses[r.URL.Path]; ok {
			w.WriteHeader(code)
			return
		}
		if body, ok := bodies[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestInstallCommunityPersonas_FetchFailure_SuggestsOffline covers AC 01-04
// Error Scenario 2: a non-2xx index fetch aborts with a descriptive error that
// names the failure and suggests --offline — never a silent fallback.
func TestInstallCommunityPersonas_FetchFailure_SuggestsOffline(t *testing.T) {
	srv := statusServer(t, nil, map[string]int{"/index.json": 500})
	dest := t.TempDir()

	err := installCommunityPersonas(srv.Client(), srv.URL, dest, []string{"owasp"}, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--offline", "error guides the user to the offline fallback")

	entries, rerr := os.ReadDir(dest)
	require.NoError(t, rerr)
	assert.Empty(t, entries, "no persona files written on fetch failure")
}

// TestInstallCommunityPersonas_MidRosterFailure_RollsBack covers AC 01-04 Edge
// Case 1: when a persona fails mid-roster, the whole roster install is rolled
// back — no partial persona files remain on disk.
func TestInstallCommunityPersonas_MidRosterFailure_RollsBack(t *testing.T) {
	index := `[
	  {"name":"a","version":"1.0.0","description":"d","path":"a.yaml"},
	  {"name":"b","version":"1.0.0","description":"d","path":"b.yaml"},
	  {"name":"c","version":"1.0.0","description":"d","path":"c.yaml"}
	]`
	srv := statusServer(t,
		map[string]string{
			"/index.json": index,
			"/a.yaml":     communityUnitYAML,
			"/b.yaml":     communityUnitYAML,
		},
		map[string]int{"/c.yaml": 500},
	)
	dest := t.TempDir()

	err := installCommunityPersonas(srv.Client(), srv.URL, dest, []string{"a", "b", "c"}, &bytes.Buffer{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "c", "the failing persona is named")

	entries, rerr := os.ReadDir(dest)
	require.NoError(t, rerr)
	assert.Empty(t, entries, "personas installed before the failure are rolled back (all-or-nothing)")
}

func TestPersonas_EmbeddedSetComplete(t *testing.T) {
	assert.ElementsMatch(t, personaNames, personas.Names())
	for _, name := range personaNames {
		content, err := personas.Get(name)
		require.NoError(t, err)
		assert.NotEmpty(t, content)
	}
	base, err := personas.Base()
	require.NoError(t, err)
	assert.NotEmpty(t, base)
}

func TestPersonas_UnknownName(t *testing.T) {
	_, err := personas.Get("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}
