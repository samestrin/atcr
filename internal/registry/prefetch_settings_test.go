package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// max_prefetch_bytes mirrors max_claim_bytes' contract (Epic 35.16.8): it
// resolves at the registry and project tiers only, 0 DISABLES the feature rather
// than meaning unlimited, and a negative value is rejected at load and fails
// safe to disabled if it reaches the resolver anyway. These tests are the
// registry-side half; the cross-package default agreement and the published doc
// contract are pinned in internal/reconcile/prefetch_default_test.go.

func TestPrecedence_MaxPrefetchBytesDefault(t *testing.T) {
	s, err := ResolveSettings(CLIOverrides{}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, DefaultMaxPrefetchBytes, s.ResolvedMaxPrefetchBytes(),
		"an unconfigured project must pre-fetch at the embedded default")
}

func TestPrecedence_MaxPrefetchBytesChain(t *testing.T) {
	// Project overrides registry overrides embedded, the same chain every
	// registry-and-project-tier setting follows.
	regVal := int64(4096)
	projVal := int64(2048)

	s, err := ResolveSettings(CLIOverrides{},
		&ProjectConfig{MaxPrefetchBytes: &projVal},
		&Registry{MaxPrefetchBytes: &regVal})
	require.NoError(t, err)
	require.Equal(t, int64(2048), s.ResolvedMaxPrefetchBytes(), "the project tier wins")

	s, err = ResolveSettings(CLIOverrides{}, nil, &Registry{MaxPrefetchBytes: &regVal})
	require.NoError(t, err)
	require.Equal(t, int64(4096), s.ResolvedMaxPrefetchBytes(), "the registry tier beats the embedded default")
}

func TestPrecedence_MaxPrefetchBytesZeroDisablesAtEitherTier(t *testing.T) {
	// 0 is the operator's refusal to send source from outside the diff to a
	// provider. It must survive resolution rather than being read as "unset".
	zero := int64(0)

	s, err := ResolveSettings(CLIOverrides{}, nil, &Registry{MaxPrefetchBytes: &zero})
	require.NoError(t, err)
	require.Zero(t, s.ResolvedMaxPrefetchBytes(), "an explicit 0 at the registry tier disables pre-fetching")

	s, err = ResolveSettings(CLIOverrides{}, &ProjectConfig{MaxPrefetchBytes: &zero}, nil)
	require.NoError(t, err)
	require.Zero(t, s.ResolvedMaxPrefetchBytes(), "an explicit 0 at the project tier disables pre-fetching")
}

func TestSettings_ZeroValueDoesNotSilentlyDisablePreFetching(t *testing.T) {
	// The reason the field is a pointer: a hand-built Settings{} (an embedder's,
	// a test roster's) must fall back to the default, not read Go's zero value as
	// "the operator turned it off".
	require.Equal(t, DefaultMaxPrefetchBytes, Settings{}.ResolvedMaxPrefetchBytes(),
		"an unresolved field must mean 'use the default', never 'disabled'")
}

func TestSettings_NegativeMaxPrefetchBytesResolvesToDisabledNotUnbounded(t *testing.T) {
	// Fail-safe direction: a mis-resolved negative must switch the feature OFF,
	// never make it unbounded — the section is exempt from every byte budget, so
	// unbounded here means unbounded prompt text nothing downstream can shed.
	neg := int64(-1)
	require.Zero(t, Settings{MaxPrefetchBytes: &neg}.ResolvedMaxPrefetchBytes())
}

func TestResolveSettings_MaxPrefetchBytesNegativeRejected(t *testing.T) {
	neg := int64(-1)

	t.Run("project tier", func(t *testing.T) {
		_, err := ResolveSettings(CLIOverrides{}, &ProjectConfig{MaxPrefetchBytes: &neg}, nil)
		require.Error(t, err, "a negative ceiling is a configuration error, not a silent default")
		require.Contains(t, err.Error(), "max_prefetch_bytes")
	})
	t.Run("registry tier", func(t *testing.T) {
		// A directly-constructed Registry bypasses LoadRegistry's validate, so
		// the resolver's own post-resolution guard has to reject it too.
		_, err := ResolveSettings(CLIOverrides{}, nil, &Registry{MaxPrefetchBytes: &neg})
		require.Error(t, err, "a negative ceiling from the registry tier is rejected as well")
		require.Contains(t, err.Error(), "max_prefetch_bytes")
	})
}

// The registry tier carries its own copy of the guard (Registry.validate), so
// the project-tier and resolver cases above prove nothing about it — a
// registry.yaml with a negative value has to be rejected at load by its own
// check.
// The YAML half of the contract: the precedence tests above build Registry and
// ProjectConfig structs by hand, so nothing else exercised the yaml tags on
// either tier — a typo in a tag would ship green with the setting silently
// ignored at every tier.
func TestPrecedence_MaxPrefetchBytesChainFromYAML(t *testing.T) {
	reg := loadRegistryWith(t, "max_prefetch_bytes: 4096\n")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_prefetch_bytes: 2048\n"))
	require.NoError(t, err)

	s := resolve(t, CLIOverrides{}, proj, reg)
	require.Equal(t, int64(2048), s.ResolvedMaxPrefetchBytes(), "project config wins over registry")

	proj, err = LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)
	s = resolve(t, CLIOverrides{}, proj, reg)
	require.Equal(t, int64(4096), s.ResolvedMaxPrefetchBytes(), "registry wins over the embedded default")
}

// 0 is the operator escape hatch at BOTH tiers, and it must survive a real YAML
// parse rather than being read as "unset" — that is the whole reason the
// config fields are pointers.
func TestPrecedence_MaxPrefetchBytesZeroDisablesAtEitherTierFromYAML(t *testing.T) {
	t.Run("project tier", func(t *testing.T) {
		reg := loadRegistryWith(t, "max_prefetch_bytes: 4096\n")
		proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_prefetch_bytes: 0\n"))
		require.NoError(t, err)

		s := resolve(t, CLIOverrides{}, proj, reg)
		require.Zero(t, s.ResolvedMaxPrefetchBytes(), "an explicit 0 disables pre-fetching; it is not 'unset'")
	})
	t.Run("registry tier", func(t *testing.T) {
		reg := loadRegistryWith(t, "max_prefetch_bytes: 0\n")
		proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
		require.NoError(t, err)

		s := resolve(t, CLIOverrides{}, proj, reg)
		require.Zero(t, s.ResolvedMaxPrefetchBytes())
	})
}

// max_prefetch_bytes in the DEFINITIONS overlay (.atcr/registry.yaml) is a
// misplaced key — it is a shared setting, and the strict decoder must reject
// it with the targeted hint the sharedSettingsKeys list powers.
func TestLoadProjectRegistry_MaxPrefetchBytesKeyHint(t *testing.T) {
	root := t.TempDir()
	writeProjectRegistry(t, root, "providers: {}\nagents: {}\nmax_prefetch_bytes: 2048\n")
	_, err := LoadProjectRegistry(DefaultProjectRegistryPath(root))
	require.Error(t, err, "strict decoder must reject shared-settings keys in the overlay")
	require.Contains(t, err.Error(), "max_prefetch_bytes", "the error must name the offending key")
	require.Contains(t, err.Error(), "config.yaml", "the error must point at the file the key belongs in")
}

// DefaultProjectConfigYAML — the config `atcr init` installs — emits and
// documents every other byte budget. max_prefetch_bytes must appear too:
// pre-fetching is ON by default and ships source from files the diff never
// touched to third-party providers, so an operator auditing the generated
// config for what leaves their repo must see the feature and its 0 escape
// hatch there.
func TestDefaultProjectConfigYAML_DocumentsMaxPrefetchBytes(t *testing.T) {
	out := DefaultProjectConfigYAML([]string{"bruce"})
	require.Contains(t, out, "max_prefetch_bytes:", "the knob must appear in the generated config")
	require.Contains(t, out, "# max_prefetch_bytes:", "it must carry a help comment like its siblings")
	require.Contains(t, out, "0 disables", "the comment must state the operator escape hatch")
	require.Contains(t, out, "outside the diff", "the comment must disclose that the feature transmits source from outside the diff")
}

func TestProjectConfig_MaxPrefetchBytesNegativeRejected(t *testing.T) {
	// Every sibling budget is validated inline in LoadProjectConfig; the
	// registry tier got its copy of this guard, the project tier did not — so
	// .atcr/config.yaml carrying -1 loaded clean, and atcr doctor (which never
	// resolves settings) green-lit a config the review path later rejected.
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\nmax_prefetch_bytes: -1\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_prefetch_bytes")
	require.Contains(t, err.Error(), "config.yaml",
		"every sibling attributes the load-time error to the offending file; the resolver fallback does not")
}

func TestRegistry_MaxPrefetchBytesNegativeRejected(t *testing.T) {
	_, err := LoadRegistry(writeRegistry(t, `
providers:
  p:
    api_key_env: KEY
agents:
  bruce: {provider: p, model: m}
max_prefetch_bytes: -1
`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_prefetch_bytes")
}
