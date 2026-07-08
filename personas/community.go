package personas

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
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

// communityMeta embeds the community personas' structured metadata YAML
// (community/<slug>.yaml), carrying the bound provider/model that the fixture
// runner asserts against (AC 06-03). Built-ins carry no such YAML — they are
// model-agnostic per C2 — so this embed covers the community layer only.
//
//go:embed community/*.yaml
var communityMeta embed.FS

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

// CommunityModel returns the bound model id from the embedded community persona's
// structured metadata (community/<name>.yaml `model:` field). Only an embedded
// library slug resolves; an unknown name errors so callers can treat it as
// carrying no structured metadata.
func CommunityModel(name string) (string, error) {
	data, err := communityMeta.ReadFile(communityEmbedDir + "/" + name + ".yaml")
	if err != nil {
		return "", fmt.Errorf("no embedded community persona metadata %q", name)
	}
	var meta struct {
		Model string `yaml:"model"`
	}
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("decode community persona metadata %q: %w", name, err)
	}
	return meta.Model, nil
}

// communityEmbedDir is the embed-FS root for the community persona layout.
const communityEmbedDir = "community"
