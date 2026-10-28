package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// missingChangelogSections returns the release tags (vX.Y.Z) that have no
// "## [X.Y.Z]" heading in the changelog text.
func missingChangelogSections(changelog string, tags []string) []string {
	var missing []string
	for _, tag := range tags {
		if !strings.Contains(changelog, "\n## ["+strings.TrimPrefix(tag, "v")+"]") {
			missing = append(missing, tag)
		}
	}
	return missing
}

// TestChangelogHasSectionForEveryReleaseTag: v0.4.0 was tagged without
// renaming [Unreleased], so its entries landed in the next release's section.
// Every v* tag must keep a matching heading in CHANGELOG.md. Skips when the
// checkout carries no tags (a shallow clone).
func TestChangelogHasSectionForEveryReleaseTag(t *testing.T) {
	out, err := exec.Command("git", "tag", "--list", "v*").Output()
	if err != nil {
		t.Skipf("git tag --list unavailable: %v", err)
	}
	semver := regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	var tags []string
	for _, tag := range strings.Fields(string(out)) {
		if semver.MatchString(tag) {
			tags = append(tags, tag)
		}
	}
	if len(tags) == 0 {
		if os.Getenv("CI") != "" {
			t.Fatal("no v* release tags in this CI checkout: the checkout step must fetch tags (fetch-depth: 0 and fetch-tags: true), or this guard checks nothing")
		}
		t.Skip("no v* release tags in this checkout (shallow clone?)")
	}
	b, err := os.ReadFile(filepath.Join("..", "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if missing := missingChangelogSections("\n"+string(b), tags); len(missing) > 0 {
		t.Fatalf("release tags with no `## [X.Y.Z]` heading in CHANGELOG.md: %v; run `go run ./cmd/changelog assemble` before the tag is pushed", missing)
	}
}

func TestMissingChangelogSections(t *testing.T) {
	doc := "\n## [Unreleased]\n\n## [0.5.0] - 2026-10-27\n\n## [0.3.0] - 2026-09-24\n"
	got := missingChangelogSections(doc, []string{"v0.3.0", "v0.4.0", "v0.5.0"})
	if len(got) != 1 || got[0] != "v0.4.0" {
		t.Fatalf("want [v0.4.0] missing, got %v", got)
	}
	if got := missingChangelogSections(doc, []string{"v0.3.0", "v0.5.0"}); len(got) != 0 {
		t.Fatalf("want none missing, got %v", got)
	}
}
