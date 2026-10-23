package registry

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The context_prefetch.semantic example in docs/registry.md (Epic 35.16.12 T3)
// is the block an operator copy-pastes to switch similarity retrieval on. It
// must stay in lockstep with what this package accepts: a key rename, a
// validation tightening, or a default change in the registry must fail here —
// before it becomes a copy-paste trap in the documentation. The example is
// extracted from the doc BY MARKER and fed through the real loaders and the
// real resolver at both tiers, exactly like the operator's own file would be.
//
// The markers are HTML comments around the fenced yaml block:
//
//	<!-- atcr-doc-example: context_prefetch.semantic -->
//	```yaml
//	context_prefetch:
//	  semantic: ...
//	```
//	<!-- /atcr-doc-example -->
var semanticDocExampleRE = regexp.MustCompile("(?s)<!--\\s*atcr-doc-example:\\s*context_prefetch\\.semantic\\s*-->\\s*```yaml\\s*\n(.*?)```")

// extractSemanticDocExample pulls the ONE marked example block out of the doc
// text. Zero matches means the doc lost the block; more than one means the
// test cannot tell which block an operator copied.
func extractSemanticDocExample(t *testing.T, doc string) string {
	t.Helper()
	matches := semanticDocExampleRE.FindAllStringSubmatch(doc, -1)
	require.Len(t, matches, 1, "docs/registry.md must carry exactly one "+
		"<!-- atcr-doc-example: context_prefetch.semantic --> fenced yaml block; found %d", len(matches))
	return matches[0][1]
}

func TestSemanticPrefetch_DocExampleLoadsAtBothTiers(t *testing.T) {
	raw, err := os.ReadFile("../../docs/registry.md")
	require.NoError(t, err)
	example := extractSemanticDocExample(t, string(raw))

	// Project tier: the block goes in .atcr/config.yaml beside the roster.
	proj, err := LoadProjectConfig(writeProject(t, "agents: [bruce]\n"+example))
	require.NoError(t, err, "the documented example must load at the project tier")
	projSP := resolve(t, CLIOverrides{}, proj, nil).ResolvedSemanticPrefetch()
	require.True(t, projSP.Enabled, "the documented example enables the feature")

	// Registry (user) tier: the same block goes in ~/.config/atcr/registry.yaml
	// beside providers and agents.
	reg := loadRegistryWith(t, example)
	regSP := resolve(t, CLIOverrides{}, nil, reg).ResolvedSemanticPrefetch()
	require.Equal(t, projSP, regSP, "the same example block must resolve identically at both tiers")
}

// The example must exercise every key the block accepts — a documented key
// list that omits one the loader takes, or names one it does not, drifts
// silently. The registry load is strict (KnownFields), so an undocumented-key
// typo in the doc is caught by the load above; this checks the complement:
// every accepted key appears in the example.
func TestSemanticPrefetch_DocExampleNamesEveryAcceptedKey(t *testing.T) {
	raw, err := os.ReadFile("../../docs/registry.md")
	require.NoError(t, err)
	example := extractSemanticDocExample(t, string(raw))
	for _, key := range []string{"enabled", "endpoint", "embedding_endpoint", "collection", "top_k", "allow_insecure_http"} {
		require.Contains(t, example, key+":",
			"the documented example must name accepted key %q (the strict loader accepts it, so the doc must show it)", key)
	}
}

// A documentation example that shows a LAN http endpoint without the
// allow_insecure_http key would produce a run that fails at call time, not at
// load: the registry accepts the URL, and the retrieval client is the one that
// refuses plain http to a non-loopback host (AC10 — verified in T1 against
// internal/semindex). The example's own endpoint may stay http only while the
// example also shows the key that permits it.
func TestSemanticPrefetch_DocExampleHttpEndpointShowsAllowance(t *testing.T) {
	raw, err := os.ReadFile("../../docs/registry.md")
	require.NoError(t, err)
	example := extractSemanticDocExample(t, string(raw))
	if strings.Contains(example, "endpoint: http://") {
		require.Contains(t, example, "allow_insecure_http: true",
			"an example whose endpoint is plain http must set allow_insecure_http: true, or the block loads and then fails at call time")
	}
}
