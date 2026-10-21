package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// context_prefetch.semantic (Epic 35.16.12 T2) is the opt-in block for
// similarity-retrieved snippets. It is OFF unless an operator asks: an absent or
// blank block must resolve to disabled, because the enabled path makes network
// calls to a service atcr does not own. It sits at the registry (user) and
// project tiers, like max_prefetch_bytes, with the project tier winning field by
// field.

const semanticBlock = `context_prefetch:
  semantic:
    enabled: true
    endpoint: https://qdrant.example.com:6333
    collection: llm_semantic
    top_k: 7
    allow_insecure_http: false
`

func TestSemanticPrefetch_AbsentBlockResolvesToDisabled(t *testing.T) {
	s, err := ResolveSettings(CLIOverrides{}, nil, nil)
	require.NoError(t, err)
	require.False(t, s.ResolvedSemanticPrefetch().Enabled, "no config at all must mean no semantic retrieval")

	reg := loadRegistryWith(t, "")
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"))
	require.NoError(t, err)
	s = resolve(t, CLIOverrides{}, proj, reg)
	require.Equal(t, SemanticPrefetch{}, s.ResolvedSemanticPrefetch(),
		"an absent block at both tiers resolves to the zero (disabled) value")

	require.False(t, Settings{}.ResolvedSemanticPrefetch().Enabled,
		"a hand-built Settings{} must never switch the feature on")
}

func TestSemanticPrefetch_BlankBlockResolvesToDisabled(t *testing.T) {
	for name, block := range map[string]string{
		"null context_prefetch": "context_prefetch:\n",
		"empty context_prefetch": "context_prefetch: {}\n",
		"null semantic":          "context_prefetch:\n  semantic:\n",
		"empty semantic":         "context_prefetch:\n  semantic: {}\n",
		"settings without enabled": "context_prefetch:\n  semantic:\n" +
			"    endpoint: https://qdrant.example.com\n    collection: c\n    top_k: 3\n",
	} {
		t.Run(name, func(t *testing.T) {
			proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"+block))
			require.NoError(t, err)
			reg := loadRegistryWith(t, block)
			s := resolve(t, CLIOverrides{}, proj, reg)
			require.Equal(t, SemanticPrefetch{}, s.ResolvedSemanticPrefetch(),
				"a blank block (or one that never says enabled: true) is disabled, and a disabled block exposes no endpoint")
		})
	}
}

func TestSemanticPrefetch_EnabledBlockResolvesFromYAML(t *testing.T) {
	t.Run("project tier", func(t *testing.T) {
		proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"+semanticBlock))
		require.NoError(t, err)
		s := resolve(t, CLIOverrides{}, proj, nil)
		require.Equal(t, SemanticPrefetch{
			Enabled:    true,
			Endpoint:   "https://qdrant.example.com:6333",
			Collection: "llm_semantic",
			TopK:       7,
		}, s.ResolvedSemanticPrefetch())
	})
	t.Run("registry tier", func(t *testing.T) {
		reg := loadRegistryWith(t, semanticBlock)
		s := resolve(t, CLIOverrides{}, nil, reg)
		require.Equal(t, SemanticPrefetch{
			Enabled:    true,
			Endpoint:   "https://qdrant.example.com:6333",
			Collection: "llm_semantic",
			TopK:       7,
		}, s.ResolvedSemanticPrefetch())
	})
	t.Run("allow_insecure_http survives", func(t *testing.T) {
		proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ncontext_prefetch:\n  semantic:\n"+
			"    enabled: true\n    endpoint: http://qdrant.lan:6333\n    collection: c\n    allow_insecure_http: true\n"))
		require.NoError(t, err)
		got := resolve(t, CLIOverrides{}, proj, nil).ResolvedSemanticPrefetch()
		require.True(t, got.AllowInsecureHTTP)
		require.Equal(t, DefaultSemanticTopK, got.TopK, "an unset top_k resolves to the embedded default")
	})
}

func TestSemanticPrefetch_ProjectOverridesUser(t *testing.T) {
	reg := loadRegistryWith(t, semanticBlock)

	t.Run("each project field wins", func(t *testing.T) {
		proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ncontext_prefetch:\n  semantic:\n"+
			"    endpoint: http://127.0.0.1:6333\n    collection: atcr-code\n    top_k: 3\n    allow_insecure_http: true\n"))
		require.NoError(t, err)
		got := resolve(t, CLIOverrides{}, proj, reg).ResolvedSemanticPrefetch()
		require.Equal(t, SemanticPrefetch{
			Enabled:           true, // inherited: the project did not say
			Endpoint:          "http://127.0.0.1:6333",
			Collection:        "atcr-code",
			TopK:              3,
			AllowInsecureHTTP: true,
		}, got)
	})
	t.Run("project enabled false switches a user-enabled block off", func(t *testing.T) {
		proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ncontext_prefetch:\n  semantic:\n    enabled: false\n"))
		require.NoError(t, err)
		require.Equal(t, SemanticPrefetch{}, resolve(t, CLIOverrides{}, proj, reg).ResolvedSemanticPrefetch())
	})
	t.Run("blank project fields inherit the user value", func(t *testing.T) {
		proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ncontext_prefetch:\n  semantic:\n"+
			"    endpoint: \"  \"\n    collection: \"\"\n"))
		require.NoError(t, err)
		got := resolve(t, CLIOverrides{}, proj, reg).ResolvedSemanticPrefetch()
		require.Equal(t, "https://qdrant.example.com:6333", got.Endpoint)
		require.Equal(t, "llm_semantic", got.Collection)
	})
	t.Run("project enables a user block that was off", func(t *testing.T) {
		off := loadRegistryWith(t, "context_prefetch:\n  semantic:\n    enabled: false\n"+
			"    endpoint: https://qdrant.example.com\n    collection: c\n")
		proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ncontext_prefetch:\n  semantic:\n    enabled: true\n"))
		require.NoError(t, err)
		require.True(t, resolve(t, CLIOverrides{}, proj, off).ResolvedSemanticPrefetch().Enabled)
	})
}

func TestSemanticPrefetch_InvalidValuesRejectedAtLoad(t *testing.T) {
	cases := map[string]struct {
		field, value, wantErr string
	}{
		"top_k zero":           {"top_k", "0", "top_k"},
		"top_k negative":       {"top_k", "-1", "top_k"},
		"top_k over cap":       {"top_k", "101", "top_k"},
		"endpoint no scheme":   {"endpoint", "qdrant.example.com:6333", "endpoint"},
		"endpoint bad scheme":  {"endpoint", "ftp://qdrant.example.com", "endpoint"},
		"endpoint no host":     {"endpoint", "https://", "endpoint"},
		"endpoint relative":    {"endpoint", "/collections", "endpoint"},
		"endpoint userinfo":    {"endpoint", "https://user:secret@qdrant.example.com", "credential"},
		"endpoint query":       {"endpoint", "https://qdrant.example.com/?api-key=x", "endpoint"},
		"endpoint fragment":    {"endpoint", "https://qdrant.example.com/#x", "endpoint"},
		"endpoint unparseable": {"endpoint", "\"http://[::1\"", "endpoint"},
		"collection slash":     {"collection", "a/b", "collection"},
		"collection dotdot":    {"collection", "..", "collection"},
		"collection space":     {"collection", "\"a b\"", "collection"},
	}
	for name, tc := range cases {
		block := "context_prefetch:\n  semantic:\n    " + tc.field + ": " + tc.value + "\n"
		t.Run("project/"+name, func(t *testing.T) {
			_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"+block))
			require.Error(t, err)
			require.Contains(t, err.Error(), "context_prefetch.semantic")
			require.Contains(t, err.Error(), tc.wantErr)
		})
		t.Run("registry/"+name, func(t *testing.T) {
			_, err := LoadRegistry(writeRegistry(t, "providers:\n  p:\n    api_key_env: KEY\n"+
				"agents:\n  bruce: {provider: p, model: m}\n"+block))
			require.Error(t, err)
			require.Contains(t, err.Error(), "context_prefetch.semantic")
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestSemanticPrefetch_UnknownKeyRejected(t *testing.T) {
	// The roster load is strict: a typo in the block (here api_key, which must
	// never live in a repo file) fails loudly instead of being ignored.
	_, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\ncontext_prefetch:\n  semantic:\n    api_key: x\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key")
}

func TestSemanticPrefetch_EnabledWithoutEndpointOrCollectionRejected(t *testing.T) {
	// Each tier may legitimately carry half the block (the other tier supplying
	// the rest), so completeness is checked on the RESOLVED value: enabled with
	// nowhere to send the query is a configuration error, not a silent off.
	tr := true
	_, err := ResolveSettings(CLIOverrides{}, &ProjectConfig{ContextPrefetch: &ContextPrefetchConfig{
		Semantic: &SemanticPrefetchConfig{Enabled: &tr, Collection: "c"},
	}}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "endpoint")

	_, err = ResolveSettings(CLIOverrides{}, nil, &Registry{ContextPrefetch: &ContextPrefetchConfig{
		Semantic: &SemanticPrefetchConfig{Enabled: &tr, Endpoint: "https://q.example.com"},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "collection")
}

func TestSemanticPrefetch_ResolverRejectsDirectlyConstructedBadValues(t *testing.T) {
	// A hand-built ProjectConfig/Registry bypasses the file loaders, so the
	// resolver re-checks the resolved values (the max_prefetch_bytes idiom).
	bad := 0
	_, err := ResolveSettings(CLIOverrides{}, &ProjectConfig{ContextPrefetch: &ContextPrefetchConfig{
		Semantic: &SemanticPrefetchConfig{TopK: &bad},
	}}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "top_k")

	_, err = ResolveSettings(CLIOverrides{}, nil, &Registry{ContextPrefetch: &ContextPrefetchConfig{
		Semantic: &SemanticPrefetchConfig{Endpoint: "gopher://x"},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "endpoint")
}

func TestLoadProjectRegistry_ContextPrefetchKeyHint(t *testing.T) {
	root := t.TempDir()
	writeProjectRegistry(t, root, "providers: {}\nagents: {}\ncontext_prefetch: {}\n")
	_, err := LoadProjectRegistry(DefaultProjectRegistryPath(root))
	require.Error(t, err)
	require.Contains(t, err.Error(), "context_prefetch")
	require.Contains(t, err.Error(), "config.yaml", "the error must point at the file the key belongs in")
}
