package personas

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Resolve (embedded manifests) -------------------------------------------

func TestBundleResolve_Django(t *testing.T) {
	got, err := Resolve("django")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"framework/django-orm",
		"language/python-types",
		"security/owasp",
		"security/secrets",
	}, got)
}

func TestBundleResolve_GoProduction(t *testing.T) {
	got, err := Resolve("go-production")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"security/owasp",
		"security/secrets",
		"performance/memory",
	}, got)
}

func TestBundleResolve_Unknown(t *testing.T) {
	got, err := Resolve("nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownBundle)
	assert.Nil(t, got)
}

func TestBundleResolve_EmptyName(t *testing.T) {
	got, err := Resolve("")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownBundle)
	assert.Nil(t, got)
}

func TestBundleResolve_MixedCaseIsUnknown(t *testing.T) {
	// Names are case-sensitive; no normalization (AC 04-03 EC1).
	_, err := Resolve("Django")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownBundle)
}

func TestBundleResolve_PathTraversalIsUnknown(t *testing.T) {
	// A traversal name must resolve to ErrUnknownBundle with no filesystem
	// access — the embedded-manifest lookup is the only gate (AC 04-03 EC3).
	for _, name := range []string{"../etc/passwd", "security/../../etc", "../go-production"} {
		_, err := Resolve(name)
		require.Errorf(t, err, "name %q must be rejected", name)
		assert.ErrorIs(t, err, ErrUnknownBundle)
	}
}

// --- parseManifest ----------------------------------------------------------

func TestParseManifest_Valid(t *testing.T) {
	data := []byte(`name: django
description: Django application review panel
personas:
  - framework/django-orm
  - security/owasp
`)
	m, err := parseManifest(data)
	require.NoError(t, err)
	assert.Equal(t, "django", m.Name)
	assert.Equal(t, "Django application review panel", m.Description)
	assert.Equal(t, []string{"framework/django-orm", "security/owasp"}, m.Personas)
}

func TestParseManifest_ExtraFieldsIgnored(t *testing.T) {
	data := []byte(`name: x
personas: [security/owasp]
tags: [web, orm]
`)
	m, err := parseManifest(data)
	require.NoError(t, err)
	assert.Equal(t, []string{"security/owasp"}, m.Personas)
}

func TestParseManifest_SingleEntryValid(t *testing.T) {
	data := []byte("name: solo\npersonas: [security/owasp]\n")
	m, err := parseManifest(data)
	require.NoError(t, err)
	assert.Len(t, m.Personas, 1)
}

func TestParseManifest_MissingName(t *testing.T) {
	data := []byte("description: no name\npersonas: [security/owasp]\n")
	_, err := parseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required field: name")
}

func TestParseManifest_EmptyPersonas(t *testing.T) {
	data := []byte("name: empty\npersonas: []\n")
	_, err := parseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `bundle manifest "empty" has no personas`)
}

func TestParseManifest_MalformedYAML(t *testing.T) {
	data := []byte("name: x\npersonas: [unclosed\n")
	_, err := parseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse bundle manifest")
}

func TestParseManifest_MissingDescriptionValid(t *testing.T) {
	data := []byte("name: x\npersonas: [security/owasp]\n")
	m, err := parseManifest(data)
	require.NoError(t, err)
	assert.Empty(t, m.Description)
}

// --- InstallBundle ----------------------------------------------------------

// countingServer serves the routes map (200) / 404 and tracks request count.
func countingServer(t *testing.T, routes map[string]string, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if body, ok := routes[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func djangoRoutes() map[string]string {
	return map[string]string{
		"/framework/django-orm.yaml":  validPersonaYAML,
		"/language/python-types.yaml": validPersonaYAML,
		"/security/owasp.yaml":        validPersonaYAML,
		"/security/secrets.yaml":      validPersonaYAML,
	}
}

func TestInstallBundle_UnknownReturnsTyped(t *testing.T) {
	out, err := InstallBundle(http.DefaultClient, "http://unused", "nonexistent", t.TempDir())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownBundle)
	assert.Nil(t, out)
}

func TestInstallBundle_CleanInstall(t *testing.T) {
	var hits int32
	srv := countingServer(t, djangoRoutes(), &hits)
	dir := t.TempDir()

	out, err := InstallBundle(srv.Client(), srv.URL, "django", dir)
	require.NoError(t, err)
	require.Len(t, out, 4)
	for _, o := range out {
		assert.NoError(t, o.Err, "member %s", o.Name)
		assert.False(t, o.AlreadyPresent, "member %s", o.Name)
	}
	assert.FileExists(t, filepath.Join(dir, "framework", "django-orm.yaml"))
	assert.FileExists(t, filepath.Join(dir, "security", "secrets.yaml"))
	assert.Equal(t, int32(4), atomic.LoadInt32(&hits))
}

func TestInstallBundle_PartialSkip(t *testing.T) {
	var hits int32
	srv := countingServer(t, djangoRoutes(), &hits)
	dir := t.TempDir()
	// Pre-populate two of the four members.
	installFixture(t, dir, "framework/django-orm", validPersonaYAML)
	installFixture(t, dir, "language/python-types", validPersonaYAML)

	out, err := InstallBundle(srv.Client(), srv.URL, "django", dir)
	require.NoError(t, err)
	require.Len(t, out, 4)

	present := map[string]bool{}
	for _, o := range out {
		require.NoError(t, o.Err, "member %s", o.Name)
		present[o.Name] = o.AlreadyPresent
	}
	assert.True(t, present["framework/django-orm"])
	assert.True(t, present["language/python-types"])
	assert.False(t, present["security/owasp"])
	assert.False(t, present["security/secrets"])
	// Only the two missing members are fetched.
	assert.Equal(t, int32(2), atomic.LoadInt32(&hits))
}

func TestInstallBundle_AllPresentNoFetch(t *testing.T) {
	var hits int32
	srv := countingServer(t, djangoRoutes(), &hits)
	dir := t.TempDir()
	for _, m := range []string{"framework/django-orm", "language/python-types", "security/owasp", "security/secrets"} {
		installFixture(t, dir, m, validPersonaYAML)
	}

	out, err := InstallBundle(srv.Client(), srv.URL, "django", dir)
	require.NoError(t, err)
	require.Len(t, out, 4)
	for _, o := range out {
		assert.True(t, o.AlreadyPresent, "member %s", o.Name)
	}
	assert.Equal(t, int32(0), atomic.LoadInt32(&hits))
}

func TestInstallBundle_MemberFailureContinues(t *testing.T) {
	var hits int32
	routes := djangoRoutes()
	delete(routes, "/security/owasp.yaml") // this member 404s
	srv := countingServer(t, routes, &hits)
	dir := t.TempDir()

	out, err := InstallBundle(srv.Client(), srv.URL, "django", dir)
	require.NoError(t, err) // function returns outcomes; per-member error is in the outcome
	require.Len(t, out, 4)

	var failed []string
	for _, o := range out {
		if o.Err != nil {
			failed = append(failed, o.Name)
		}
	}
	assert.Equal(t, []string{"security/owasp"}, failed)
	// The other three still installed despite the mid-bundle failure.
	assert.FileExists(t, filepath.Join(dir, "framework", "django-orm.yaml"))
	assert.FileExists(t, filepath.Join(dir, "security", "secrets.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
}

// --- Install rejects bundle/ names (defense in depth) -----------------------

func TestInstall_RejectsBundlePrefix(t *testing.T) {
	// A bundle/ name must never round-trip through the single-persona fetch path.
	err := Install(http.DefaultClient, "http://unused", "bundle/django", t.TempDir())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrPersonaNotFound)
}
