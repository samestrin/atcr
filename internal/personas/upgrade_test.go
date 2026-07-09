package personas

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingClient wraps an HTTPClient and records the path of every request it
// makes, so a test can assert exactly which endpoints a code path touches.
type recordingClient struct {
	inner HTTPClient
	paths *[]string
}

func (r recordingClient) Do(req *http.Request) (*http.Response, error) {
	*r.paths = append(*r.paths, req.URL.Path)
	return r.inner.Do(req)
}

func TestVersionOf_ValidYAML(t *testing.T) {
	data := []byte("version: \"1.2.3\"\n")
	v, err := versionOf(data)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", v)
}

func TestVersionOf_MissingVersion(t *testing.T) {
	data := []byte("provider: anthropic\n")
	v, err := versionOf(data)
	require.NoError(t, err)
	assert.Equal(t, "-", v)
}

func TestVersionOf_CorruptYAMLReturnsError(t *testing.T) {
	data := []byte("version: [unclosed\n")
	_, err := versionOf(data)
	require.Error(t, err, "corrupt local YAML must surface a parse error")
	assert.Contains(t, err.Error(), "parse")
}

func TestIsNewer_MixedValidityTreatsAsUpToDate(t *testing.T) {
	cases := []struct {
		local, remote string
	}{
		{"v1.0.0", "latest"},
		{"latest", "v1.0.0"},
		{"v2.0.0", "v1.0.0"},
	}
	for _, c := range cases {
		assert.False(t, isNewer(c.local, c.remote), "isNewer(%q, %q) should treat mixed/ambiguous validity as up-to-date", c.local, c.remote)
	}
}

// --- Upgrade paired-write behavior (TD-007) ---------------------------------

func TestUpgrade_WritesMarkdownWhenRemoteHasOne(t *testing.T) {
	remoteYAML := `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
version: "1.1.0"
`
	remoteMD := "# Upgraded OWASP reviewer\n"
	srv := testServer(t, map[string]string{
		"/security/owasp.yaml": remoteYAML,
		"/security/owasp.md":   remoteMD,
	})
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML) // v1.0.0, no .md

	res, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", false)
	require.NoError(t, err)
	assert.True(t, res.Upgraded)

	gotMD, err := os.ReadFile(filepath.Join(dir, "security", "owasp.md"))
	require.NoError(t, err)
	assert.Equal(t, remoteMD, string(gotMD))
}

func TestUpgrade_RemovesStaleMarkdownWhenBindingOnly(t *testing.T) {
	remoteYAML := `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
version: "1.1.0"
`
	// Remote no longer ships a co-located .md; a stale local .md must be removed.
	srv := testServer(t, map[string]string{"/security/owasp.yaml": remoteYAML})
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML)
	staleMD := filepath.Join(dir, "security", "owasp.md")
	require.NoError(t, os.WriteFile(staleMD, []byte("# stale"), 0o644))

	res, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", false)
	require.NoError(t, err)
	assert.True(t, res.Upgraded)
	assert.NoFileExists(t, staleMD, "stale .md must be removed when remote is binding-only")
}

func TestUpgrade_StrictDecodeRejectsUnknownField(t *testing.T) {
	badYAML := validPersonaYAML + "unknown_strict_field: value\n"
	srv := testServer(t, map[string]string{"/security/owasp.yaml": badYAML})
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML)

	_, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in type")

	// Local unit must remain untouched.
	got, _ := os.ReadFile(filepath.Join(dir, "security", "owasp.yaml"))
	assert.Equal(t, validPersonaYAML, string(got))
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.md"))
}

// --- Phase 4: binding-string parser (AC 04-01) ------------------------------

func TestParseBinding(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantPresent bool
		want        Binding
		wantErr     bool
	}{
		{"empty is absent", "", false, Binding{}, false},
		{"whitespace is absent", "   ", false, Binding{}, false},
		{"pin sigil", "pin:anthropic/claude-opus-4.8", true, Binding{Pin: "anthropic/claude-opus-4.8"}, false},
		{"family and stable channel", "anthropic/claude-opus@stable", true, Binding{Family: "anthropic/claude-opus", Channel: "@stable"}, false},
		{"family and latest channel", "anthropic/claude-opus@latest", true, Binding{Family: "anthropic/claude-opus", Channel: "@latest"}, false},
		{"scan family with channel", "deepseek@stable", true, Binding{Family: "deepseek", Channel: "@stable"}, false},
		{"bare alias family defaults channel", "anthropic/claude-opus", true, Binding{Family: "anthropic/claude-opus"}, false},
		{"bare scan family defaults channel", "deepseek", true, Binding{Family: "deepseek"}, false},
		// Typo gap CLOSED: an alias-shaped family typo is neither a known family
		// nor a pin (no pin: prefix), so it errors rather than being silently
		// accepted as a pin that only fails downstream at the API.
		{"alias-shaped typo errors", "anthropic/claude-opu", false, Binding{}, true},
		{"bare unknown token errors", "totallybogus", false, Binding{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, present, err := parseBinding(c.in)
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.wantPresent, present)
			assert.Equal(t, c.want, b)
		})
	}
}

func TestVersionFromSlug(t *testing.T) {
	cases := []struct{ slug, want string }{
		{"anthropic/claude-opus-4.8", "4.8"},
		{"openai/gpt-5.5", "5.5"},
		{"z-ai/glm-5.2", "5.2"},
		{"anthropic/claude-sonnet-5", "5"},
		{"deepseek/deepseek-v4.1", "v4.1"},
		{"deepseek/deepseek-v4-pro", "v4"},
		{"~anthropic/claude-opus-latest", ""}, // no numeric version segment
	}
	for _, c := range cases {
		assert.Equal(t, c.want, versionFromSlug(c.slug), "versionFromSlug(%q)", c.slug)
	}
}

// --- Phase 4: Upgrade resolves & advances the lock (AC 04-01) ----------------

// bindingPersonaYAML is an installed community persona carrying a family/channel
// binding plus a concrete resolved-slug lock (Model), used to drive the resolver
// path in Upgrade. deepseek is a created-timestamp (scan) family, so its lock can
// genuinely advance when a newer catalog member appears.
const bindingPersonaYAML = `provider: openrouter
model: deepseek/deepseek-v4.0
role: reviewer
binding: deepseek@stable
version: "1.0.0"
`

// catalogJSON builds a minimal /models envelope from id→created pairs.
func catalogJSON(entries ...[2]string) string {
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i, e := range entries {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"id":"` + e[0] + `","canonical_slug":"` + e[0] + `","created":` + e[1] + `,"expiration_date":null}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

func TestUpgrade_BindingResolvesAndAdvancesLock(t *testing.T) {
	// Catalog has a newer deepseek member than the persona's current lock.
	cat := catalogJSON(
		[2]string{"deepseek/deepseek-v4.0", "1700000000"},
		[2]string{"deepseek/deepseek-v4.1", "1780000000"},
	)
	srv := testServer(t, map[string]string{"/models": cat})
	t.Setenv(envCatalogURL, srv.URL)
	dir := t.TempDir()
	installFixture(t, dir, "vendor/delia", bindingPersonaYAML)

	res, err := Upgrade(srv.Client(), srv.URL, dir, "vendor/delia", false)
	require.NoError(t, err)
	assert.True(t, res.Resolved, "a binding is present, so resolution must run")
	assert.True(t, res.SlugChanged, "a newer catalog member must advance the lock")
	assert.Equal(t, "deepseek/deepseek-v4.0", res.FromSlug)
	assert.Equal(t, "deepseek/deepseek-v4.1", res.ToSlug)

	// The lock (model field) is advanced on disk; other fields are preserved.
	got, _ := os.ReadFile(filepath.Join(dir, "vendor", "delia.yaml"))
	assert.Contains(t, string(got), "deepseek/deepseek-v4.1")
	assert.NotContains(t, string(got), "deepseek/deepseek-v4.0")
	assert.Contains(t, string(got), "binding: deepseek@stable", "binding must be preserved")
}

func TestUpgrade_BindingResolvedUnchanged(t *testing.T) {
	// Catalog's newest deepseek equals the current lock → no advance, no write.
	cat := catalogJSON([2]string{"deepseek/deepseek-v4.0", "1700000000"})
	srv := testServer(t, map[string]string{"/models": cat})
	t.Setenv(envCatalogURL, srv.URL)
	dir := t.TempDir()
	installFixture(t, dir, "vendor/delia", bindingPersonaYAML)

	res, err := Upgrade(srv.Client(), srv.URL, dir, "vendor/delia", false)
	require.NoError(t, err)
	assert.True(t, res.Resolved)
	assert.False(t, res.SlugChanged, "an identical resolved slug must not advance the lock")
	assert.True(t, res.UpToDate)

	got, _ := os.ReadFile(filepath.Join(dir, "vendor", "delia.yaml"))
	assert.Equal(t, bindingPersonaYAML, string(got), "unchanged persona must be byte-for-byte identical")
}

func TestUpgrade_BindingResolveFailAbortsCleanly(t *testing.T) {
	// Catalog has no deepseek-prefixed member → resolver fails closed. The lock
	// must be left unchanged (no partial advance, no silent stale fallback).
	cat := catalogJSON([2]string{"qwen/qwen3.7-plus", "1780000000"})
	srv := testServer(t, map[string]string{"/models": cat})
	t.Setenv(envCatalogURL, srv.URL)
	dir := t.TempDir()
	installFixture(t, dir, "vendor/delia", bindingPersonaYAML)

	_, err := Upgrade(srv.Client(), srv.URL, dir, "vendor/delia", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve model binding")

	got, _ := os.ReadFile(filepath.Join(dir, "vendor", "delia.yaml"))
	assert.Equal(t, bindingPersonaYAML, string(got), "failed resolution must leave the lock unchanged")
}

// TestUpgrade_BindingEstablishesLockFromEmpty covers AC 04-01 Edge Case 2: a
// persona with a binding but no prior lock (empty Model) adopts the resolved slug
// to establish the lock, reporting "(none)" as the before side — the version-
// advance gate applies only once a lock exists.
func TestUpgrade_BindingEstablishesLockFromEmpty(t *testing.T) {
	cat := catalogJSON([2]string{"deepseek/deepseek-v4.1", "1780000000"})
	srv := testServer(t, map[string]string{"/models": cat})
	t.Setenv(envCatalogURL, srv.URL)
	dir := t.TempDir()
	noLock := `provider: openrouter
model: ""
role: reviewer
binding: deepseek@stable
version: "1.0.0"
`
	installFixture(t, dir, "vendor/delia", noLock)

	res, err := Upgrade(srv.Client(), srv.URL, dir, "vendor/delia", false)
	require.NoError(t, err)
	assert.True(t, res.SlugChanged, "an empty lock must be established, not reported up-to-date")
	assert.Equal(t, "(none)", res.FromSlug)
	assert.Equal(t, "deepseek/deepseek-v4.1", res.ToSlug)

	got, _ := os.ReadFile(filepath.Join(dir, "vendor", "delia.yaml"))
	assert.Contains(t, string(got), "deepseek/deepseek-v4.1")
}

// TestUpgrade_AliasFamilyDoesNotAdvance encodes the documented alias semantic: an
// alias family resolves to the constant provider alias slug (no numeric version),
// which is non-comparable to the concrete lock, so the lock is retained and the
// persona is reported unchanged — real model movement is provider-side.
func TestUpgrade_AliasFamilyDoesNotAdvance(t *testing.T) {
	srv := testServer(t, map[string]string{"/models": `{"data":[]}`})
	t.Setenv(envCatalogURL, srv.URL)
	dir := t.TempDir()
	aliasPersona := `provider: openrouter
model: anthropic/claude-opus-4.8
role: reviewer
binding: anthropic/claude-opus@stable
version: "1.0.0"
`
	installFixture(t, dir, "vendor/anthony", aliasPersona)

	res, err := Upgrade(srv.Client(), srv.URL, dir, "vendor/anthony", false)
	require.NoError(t, err)
	assert.True(t, res.Resolved)
	assert.False(t, res.SlugChanged, "an alias family resolves to a constant, non-version slug — no advance")
	assert.True(t, res.UpToDate)

	got, _ := os.ReadFile(filepath.Join(dir, "vendor", "anthony.yaml"))
	assert.Equal(t, aliasPersona, string(got), "an unchanged alias persona must be byte-for-byte identical")
}

// --- Phase 4: dry-run reports without writing (AC 04-03) --------------------

// TestUpgrade_BindingDryRunReportsWithoutWriting: a dry-run resolution reports the
// before→after it WOULD apply, leaving the persona byte-for-byte unchanged.
func TestUpgrade_BindingDryRunReportsWithoutWriting(t *testing.T) {
	cat := catalogJSON(
		[2]string{"deepseek/deepseek-v4.0", "1700000000"},
		[2]string{"deepseek/deepseek-v4.1", "1780000000"},
	)
	srv := testServer(t, map[string]string{"/models": cat})
	t.Setenv(envCatalogURL, srv.URL)
	dir := t.TempDir()
	installFixture(t, dir, "vendor/delia", bindingPersonaYAML)

	res, err := Upgrade(srv.Client(), srv.URL, dir, "vendor/delia", true)
	require.NoError(t, err)
	assert.True(t, res.SlugChanged, "dry-run must still report the would-be advance")
	assert.Equal(t, "deepseek/deepseek-v4.0", res.FromSlug)
	assert.Equal(t, "deepseek/deepseek-v4.1", res.ToSlug)

	got, _ := os.ReadFile(filepath.Join(dir, "vendor", "delia.yaml"))
	assert.Equal(t, bindingPersonaYAML, string(got), "dry-run must not change the persona on disk")
}

// TestUpgrade_BindingDryRunParity: a dry-run produces the identical UpgradeResult
// (from/to slugs, changed flag) a real run would, differing only in the write —
// the shared-computation guarantee AC 04-03 requires.
func TestUpgrade_BindingDryRunParity(t *testing.T) {
	cat := catalogJSON(
		[2]string{"deepseek/deepseek-v4.0", "1700000000"},
		[2]string{"deepseek/deepseek-v4.1", "1780000000"},
	)
	srv := testServer(t, map[string]string{"/models": cat})
	t.Setenv(envCatalogURL, srv.URL)

	dryDir := t.TempDir()
	installFixture(t, dryDir, "vendor/delia", bindingPersonaYAML)
	dryRes, err := Upgrade(srv.Client(), srv.URL, dryDir, "vendor/delia", true)
	require.NoError(t, err)

	realDir := t.TempDir()
	installFixture(t, realDir, "vendor/delia", bindingPersonaYAML)
	realRes, err := Upgrade(srv.Client(), srv.URL, realDir, "vendor/delia", false)
	require.NoError(t, err)

	assert.Equal(t, realRes.FromSlug, dryRes.FromSlug)
	assert.Equal(t, realRes.ToSlug, dryRes.ToSlug)
	assert.Equal(t, realRes.SlugChanged, dryRes.SlugChanged)

	dryGot, _ := os.ReadFile(filepath.Join(dryDir, "vendor", "delia.yaml"))
	assert.Equal(t, bindingPersonaYAML, string(dryGot), "dry-run leaves disk unchanged")
	realGot, _ := os.ReadFile(filepath.Join(realDir, "vendor", "delia.yaml"))
	assert.Contains(t, string(realGot), "deepseek/deepseek-v4.1", "real run advances the lock")
}

// --- Phase 4: resolution isolated to the upgrade path (AC 04-02) ------------

// TestUpgrade_CatalogFetchedOnlyOnBindingPath proves the catalog/models endpoint
// is fetched exactly once on the resolution path and NEVER on the bindingless
// version path — the load-bearing C3 guarantee that resolution is confined to
// Upgrade's binding branch.
func TestUpgrade_CatalogFetchedOnlyOnBindingPath(t *testing.T) {
	cat := catalogJSON([2]string{"deepseek/deepseek-v4.1", "1780000000"})
	remote := `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
version: "1.1.0"
`
	srv := testServer(t, map[string]string{
		"/models":              cat,
		"/security/owasp.yaml": remote,
	})
	t.Setenv(envCatalogURL, srv.URL)

	// Bindingless persona: version path only — the catalog must never be touched.
	t.Run("bindingless never fetches catalog", func(t *testing.T) {
		var paths []string
		client := recordingClient{inner: srv.Client(), paths: &paths}
		dir := t.TempDir()
		installFixture(t, dir, "security/owasp", validPersonaYAML)

		_, err := Upgrade(client, srv.URL, dir, "security/owasp", false)
		require.NoError(t, err)
		for _, p := range paths {
			assert.NotEqual(t, "/models", p, "bindingless upgrade must never fetch the catalog")
		}
	})

	// Bound persona: the catalog is fetched exactly once, on the resolution path.
	t.Run("binding fetches catalog exactly once", func(t *testing.T) {
		var paths []string
		client := recordingClient{inner: srv.Client(), paths: &paths}
		dir := t.TempDir()
		installFixture(t, dir, "vendor/delia", bindingPersonaYAML)

		_, err := Upgrade(client, srv.URL, dir, "vendor/delia", false)
		require.NoError(t, err)
		n := 0
		for _, p := range paths {
			if p == "/models" {
				n++
			}
		}
		assert.Equal(t, 1, n, "a bound upgrade must fetch the catalog exactly once")
	})
}

// TestReviewAndResolvePathsCannotReachCatalog is a structural regression lock: the
// catalog/resolver lives in internal/personas, so no file in internal/registry
// (ResolvePersona — the review-time resolve path) or internal/fanout (review
// fan-out) may import internal/personas. Enforcing zero import is stronger than
// checking a single file and catches any future convenience call that would put
// catalog resolution on the review hot path (AC 04-02 Edge Case 1).
func TestReviewAndResolvePathsCannotReachCatalog(t *testing.T) {
	const forbidden = `"github.com/samestrin/atcr/internal/personas"`
	// Walk recursively so a future SUBpackage under these trees cannot evade the
	// guard (hardened per the 4.5.A adversarial caveat).
	for _, root := range []string{"../registry", "../fanout"} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotContains(t, string(data), forbidden,
				"%s must not import internal/personas — resolution must stay off the review path", path)
			return nil
		})
		require.NoError(t, err)
	}
}

// TestUpgrade_EmptyBindingUsesVersionPath is the bindingless-persona case both
// the maintainer and the gate reviewer flagged as unspecified: a persona with no
// binding must fall through to 19.6's unchanged version-based upgrade path and
// must NOT fetch the catalog. The server serves no /models route, so any catalog
// fetch would 404 and surface — its absence proves the resolver was skipped.
func TestUpgrade_EmptyBindingUsesVersionPath(t *testing.T) {
	remote := `provider: anthropic
model: claude-sonnet-4-6
role: reviewer
version: "1.1.0"
`
	srv := testServer(t, map[string]string{"/security/owasp.yaml": remote})
	t.Setenv(envCatalogURL, srv.URL) // present but must never be consulted
	dir := t.TempDir()
	installFixture(t, dir, "security/owasp", validPersonaYAML) // v1.0.0, no binding

	res, err := Upgrade(srv.Client(), srv.URL, dir, "security/owasp", false)
	require.NoError(t, err)
	assert.False(t, res.Resolved, "no binding → resolver must be skipped entirely")
	assert.True(t, res.Upgraded)
	assert.Equal(t, "1.0.0", res.FromVersion)
	assert.Equal(t, "1.1.0", res.ToVersion)
}
