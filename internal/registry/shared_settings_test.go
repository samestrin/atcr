package registry

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sharedTierRegistryYAML is a valid user-global registry that sets BOTH shared
// settings, so a single load is enough to answer both questions.
const sharedTierRegistryYAML = "providers:\n  p:\n    api_key_env: K\n    base_url: https://example.invalid/v1\n" +
	"agents:\n  a:\n    provider: p\n    model: m\nfail_on: HIGH\nconsensus: off\n"

// seedUserRegistry writes content to the user-global registry path under a temp
// HOME and returns that HOME.
func seedUserRegistry(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(content), 0o644))
	return home
}

// TestResolveSharedSettings_LoadsUserRegistryOnce pins the single-load contract
// that makes the two shared-setting resolvers cheap and self-consistent. Under
// ATCR_REGISTRY_URL each independent resolver costs its own HTTP GET, and
// because the registry tier is swallowed best-effort one fetch can succeed while
// the other fails — leaving fail_on and consensus resolved from DIFFERENT tiers
// inside one run. Resolving both from one load makes that impossible.
func TestResolveSharedSettings_LoadsUserRegistryOnce(t *testing.T) {
	var fetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&fetches, 1)
		_, _ = w.Write([]byte(sharedTierRegistryYAML))
	}))
	t.Cleanup(srv.Close)

	// A local registry file must exist for the tier to be consulted at all; the
	// bytes come from the URL (see loadRegistryBytes).
	seedUserRegistry(t, sharedTierRegistryYAML)
	t.Setenv("ATCR_REGISTRY_URL", srv.URL+"/registry.yaml")
	root := t.TempDir()

	failOn, consensus, err := ResolveSharedSettings(root, "", "")
	require.NoError(t, err)
	assert.Equal(t, "HIGH", failOn)
	assert.Equal(t, "off", consensus)
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetches),
		"both shared settings must resolve from ONE user-registry load, not one per setting")
}

// TestResolveSharedSettings_ProjectTierWinsForBothSettings is the project-tier
// half: one .atcr/config.yaml parse must answer both settings, with the project
// tier outranking the registry tier for each of them independently.
func TestResolveSharedSettings_ProjectTierWinsForBothSettings(t *testing.T) {
	seedUserRegistry(t, sharedTierRegistryYAML)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root),
		[]byte("agents:\n  - a\nfail_on: LOW\nconsensus: lenient\n"), 0o644))

	failOn, consensus, err := ResolveSharedSettings(root, "", "")
	require.NoError(t, err)
	assert.Equal(t, "LOW", failOn)
	assert.Equal(t, "lenient", consensus)
}

// TestResolveSharedSettings_MatchesIndividualResolvers is the anti-divergence
// guard the duplication created: ResolveGateThreshold and ResolveConsensus must
// stay byte-identical to the combined resolver across every tier combination, so
// a future change to tier semantics cannot be applied to one chain and missed on
// the other.
func TestResolveSharedSettings_MatchesIndividualResolvers(t *testing.T) {
	cases := []struct {
		name              string
		projectYAML       string // "" = no project config at all
		explicitFailOn    string
		explicitConsensus string
	}{
		{name: "nothing configured"},
		{name: "project sets both", projectYAML: "agents:\n  - a\nfail_on: LOW\nconsensus: lenient\n"},
		{name: "project sets neither", projectYAML: "agents:\n  - a\n"},
		{name: "project sets only consensus", projectYAML: "agents:\n  - a\nconsensus: lenient\n"},
		{name: "explicit beats every file tier", projectYAML: "agents:\n  - a\nfail_on: LOW\nconsensus: lenient\n",
			explicitFailOn: "CRITICAL", explicitConsensus: "strict"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seedUserRegistry(t, sharedTierRegistryYAML)
			root := t.TempDir()
			if tc.projectYAML != "" {
				require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
				require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root), []byte(tc.projectYAML), 0o644))
			}

			wantFailOn, err := ResolveGateThreshold(root, tc.explicitFailOn)
			require.NoError(t, err)
			wantConsensus, err := ResolveConsensus(root, tc.explicitConsensus)
			require.NoError(t, err)

			gotFailOn, gotConsensus, err := ResolveSharedSettings(root, tc.explicitFailOn, tc.explicitConsensus)
			require.NoError(t, err)
			assert.Equal(t, wantFailOn, gotFailOn, "fail_on must match the standalone resolver")
			assert.Equal(t, wantConsensus, gotConsensus, "consensus must match the standalone resolver")
		})
	}
}

// TestResolveSharedSettings_BrokenProjectConfigIsAnError preserves the tier
// asymmetry both standalone resolvers document: the repo's own config is an
// error when present-but-broken, never a silent skip.
func TestResolveSharedSettings_BrokenProjectConfigIsAnError(t *testing.T) {
	seedUserRegistry(t, sharedTierRegistryYAML)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root), []byte("agents: []\n"), 0o644))

	_, _, err := ResolveSharedSettings(root, "", "")
	require.Error(t, err)
}

// TestResolveSharedSettings_BrokenUserRegistrySkipped preserves the other half
// of the asymmetry: a broken user-global registry never blocks a run.
func TestResolveSharedSettings_BrokenUserRegistrySkipped(t *testing.T) {
	seedUserRegistry(t, "providers: [this is not a map\n")
	root := t.TempDir()

	failOn, consensus, err := ResolveSharedSettings(root, "", "")
	require.NoError(t, err)
	assert.Equal(t, "", failOn)
	assert.Equal(t, "", consensus)
}

// TestLoadSharedTiers_UnreadableProjectConfigIsFatal pins the distinction an
// os.Stat pre-check cannot make: "absent" and "unreachable" are different
// answers. With .atcr/ permission-denied the stat itself fails, so the project
// tier silently disappears and control passes to the user-global registry —
// precisely the weakening path a consensus resolver must be most careful about.
// Reaching the read instead surfaces the permission error as the failure it is.
func TestLoadSharedTiers_UnreadableProjectConfigIsFatal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	seedUserRegistry(t, sharedTierRegistryYAML)
	root := t.TempDir()
	dir := filepath.Join(root, ".atcr")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root),
		[]byte("agents:\n  - a\nconsensus: lenient\n"), 0o644))
	require.NoError(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, _, err := ResolveSharedSettings(root, "", "")
	require.Error(t, err, "an unreadable project config must not be mistaken for an absent one")

	_, err = ResolveConsensus(root, "")
	require.Error(t, err, "the standalone consensus resolver must agree")
	_, err = ResolveGateThreshold(root, "")
	require.Error(t, err, "the standalone gate resolver must agree")
}

// TestLoadProjectConfig_MissingWrapsErrNotExist is what lets callers skip the
// project tier on absence alone instead of pre-checking with os.Stat. The
// message text is unchanged (AC 01-01 Error Scenario 1); only the sentinel is
// now recoverable.
func TestLoadProjectConfig_MissingWrapsErrNotExist(t *testing.T) {
	_, err := LoadProjectConfig(filepath.Join(t.TempDir(), ".atcr", "config.yaml"))
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist, "absence must be recoverable via errors.Is")
	assert.Contains(t, err.Error(), "no roster found", "the mandated message text must be preserved")
}

// TestLoadSharedTiers_MissingProjectConfigStillSkipped is the control for the
// two tests above: absence stays a silent skip, so an unconfigured repo keeps
// falling through to the user-global registry.
func TestLoadSharedTiers_MissingProjectConfigStillSkipped(t *testing.T) {
	seedUserRegistry(t, sharedTierRegistryYAML)
	root := t.TempDir()

	failOn, consensus, err := ResolveSharedSettings(root, "", "")
	require.NoError(t, err)
	assert.Equal(t, "HIGH", failOn)
	assert.Equal(t, "off", consensus)
}

// TestLoadSharedTiers_RemoteRegistryWithoutLocalFile is the Epic 19.2
// shared-team-registry shape: ATCR_REGISTRY_URL set, no local
// ~/.config/atcr/registry.yaml at all. Gating the tier on an os.Stat of the
// local path skips it without attempting a fetch, so a team's shared fail_on
// and consensus are silently ignored — even though LoadRegistry reads from the
// URL and ignores the local path entirely when the env var is set.
func TestLoadSharedTiers_RemoteRegistryWithoutLocalFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sharedTierRegistryYAML))
	}))
	t.Cleanup(srv.Close)

	// A HOME with NO registry file: the only source is the URL.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ATCR_REGISTRY_URL", srv.URL+"/registry.yaml")
	root := t.TempDir()

	failOn, consensus, err := ResolveSharedSettings(root, "", "")
	require.NoError(t, err)
	assert.Equal(t, "HIGH", failOn, "a shared remote registry's fail_on must be honored with no local file")
	assert.Equal(t, "off", consensus, "a shared remote registry's consensus must be honored with no local file")

	// Both standalone resolvers must agree, or the two chains have forked again.
	v, err := ResolveConsensus(root, "")
	require.NoError(t, err)
	assert.Equal(t, "off", v)
	v, err = ResolveGateThreshold(root, "")
	require.NoError(t, err)
	assert.Equal(t, "HIGH", v)
}

// TestLoadSharedTiers_UnreachableRemoteRegistryStillSkipped keeps the
// best-effort half intact once the stat gate is gone: a set-but-unreachable URL
// must not block a run that does not otherwise need the registry.
func TestLoadSharedTiers_UnreachableRemoteRegistryStillSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("ATCR_REGISTRY_URL", srv.URL+"/registry.yaml")
	root := t.TempDir()

	failOn, consensus, err := ResolveSharedSettings(root, "", "")
	require.NoError(t, err, "an unreachable user registry must never block the resolve")
	assert.Equal(t, "", failOn)
	assert.Equal(t, "", consensus)
}

// TestLoadSharedTiers_NoRegistryAnywhereIsSilent is the control: with neither a
// local file nor a URL, the tier is simply absent.
func TestLoadSharedTiers_NoRegistryAnywhereIsSilent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()

	failOn, consensus, err := ResolveSharedSettings(root, "", "")
	require.NoError(t, err)
	assert.Equal(t, "", failOn)
	assert.Equal(t, "", consensus)
}

// TestResolveSharedSettings_SkipsRegistryWhenProjectAnswers guards the other
// half of the cost story: dropping the local-file stat gate must not make the
// user-registry load EAGER. When .atcr/config.yaml already answers both
// settings the registry tier is never consulted, so under ATCR_REGISTRY_URL a
// fully-configured project pays no HTTP GET (and no 10s timeout on a flaky
// endpoint) for an answer it already had.
func TestResolveSharedSettings_SkipsRegistryWhenProjectAnswers(t *testing.T) {
	var fetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&fetches, 1)
		_, _ = w.Write([]byte(sharedTierRegistryYAML))
	}))
	t.Cleanup(srv.Close)

	seedUserRegistry(t, sharedTierRegistryYAML)
	t.Setenv("ATCR_REGISTRY_URL", srv.URL+"/registry.yaml")
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root),
		[]byte("agents:\n  - a\nfail_on: LOW\nconsensus: lenient\n"), 0o644))

	failOn, consensus, err := ResolveSharedSettings(root, "", "")
	require.NoError(t, err)
	assert.Equal(t, "LOW", failOn)
	assert.Equal(t, "lenient", consensus)
	assert.Equal(t, int32(0), atomic.LoadInt32(&fetches),
		"the registry tier must not be loaded when the project tier answered both settings")
}

// TestResolveSharedSettings_LoadsRegistryOnceForPartialFallthrough is the mixed
// case: the project config answers only one setting, so the registry is needed
// — once, for the other.
func TestResolveSharedSettings_LoadsRegistryOnceForPartialFallthrough(t *testing.T) {
	var fetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&fetches, 1)
		_, _ = w.Write([]byte(sharedTierRegistryYAML))
	}))
	t.Cleanup(srv.Close)

	seedUserRegistry(t, sharedTierRegistryYAML)
	t.Setenv("ATCR_REGISTRY_URL", srv.URL+"/registry.yaml")
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root),
		[]byte("agents:\n  - a\nfail_on: LOW\n"), 0o644))

	failOn, consensus, err := ResolveSharedSettings(root, "", "")
	require.NoError(t, err)
	assert.Equal(t, "LOW", failOn, "project tier still wins for the setting it names")
	assert.Equal(t, "off", consensus, "the other setting falls through to the registry")
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetches),
		"the fallthrough must cost exactly one registry load")
}
