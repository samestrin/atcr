package personas

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

// communityFiles embeds the co-located prompt templates of the model-indexed
// community persona library (community/<slug>.md). These are distributed through
// the community channel and resolved through the same chain as built-ins — they
// are NOT part of the embedded built-in set guarded by the package init().
//
//go:embed community/*.md
var communityFiles embed.FS

// communityFixtures embeds the community personas' patch fixtures
// (community/testdata/<slug>_fixture.patch), the community-layout counterpart of
// the built-in testdata/*.patch fixtures.
//
//go:embed community/testdata/*.patch
var communityFixtures embed.FS

// CommunityNames returns the slugs of the embedded community-library personas
// (each community/<slug>.md), sorted. Distinct from Names(), which returns the
// embedded built-ins.
func CommunityNames() []string {
	entries, err := communityFiles.ReadDir(communityEmbedDir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(out)
	return out
}

// CommunityGet returns the co-located community persona prompt template for name
// (community/<name>.md). Only an embedded library slug resolves; a namespaced or
// unknown name returns an error so callers can treat it as HasFixture: false.
func CommunityGet(name string) (string, error) {
	data, err := communityFiles.ReadFile(communityEmbedDir + "/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("no embedded community persona %q", name)
	}
	return string(data), nil
}

// CommunityFixture returns the embedded patch fixture for community persona name
// (community/testdata/<name>_fixture.patch).
func CommunityFixture(name string) (string, error) {
	data, err := communityFixtures.ReadFile(communityEmbedDir + "/testdata/" + name + "_fixture.patch")
	if err != nil {
		return "", fmt.Errorf("no embedded community fixture for persona %q", name)
	}
	return string(data), nil
}

// communityEmbedDir is the embed-FS root for the community persona layout.
const communityEmbedDir = "community"
