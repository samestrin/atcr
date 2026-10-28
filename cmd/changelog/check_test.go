package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/gitexec"
)

const (
	logEmpty  = "# Changelog\n\n## [Unreleased]\n\nSee changelog.d/.\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
	logEdited = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- new thing. (epic 1)\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
	logHeader = "# Changelog\n\nnew rules\n\n## [Unreleased]\n\nSee changelog.d/.\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
	// logListed still lists entries under Unreleased, as before the migration.
	logListed = "# Changelog\n\n## [Unreleased]\n\nSee changelog.d/.\n\n### Added\n\n- a thing. (epic 1)\n- b thing\n  wrapped. (epic 2)\n\n### Fixed\n\n- c fixed. (epic 3)\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
)

// TestEvaluate_Migration covers the one Unreleased edit the gate allows: moving
// listed bullets out of Unreleased into fragments the same PR adds.
func TestEvaluate_Migration(t *testing.T) {
	fragA := change{'A', "changelog.d/added-a.md"}
	fragB := change{'A', "changelog.d/added-b.md"}
	fragC := change{'A', "changelog.d/fixed-c.md"}
	all := map[string]string{
		"changelog.d/added-a.md": "- a thing. (epic 1)\n",
		"changelog.d/added-b.md": "- b thing\r\n  wrapped. (epic 2)\r\n",
		"changelog.d/fixed-c.md": "- c fixed. (epic 3)\n",
	}
	without := func(drop string) map[string]string {
		m := map[string]string{}
		for k, v := range all {
			if k != drop {
				m[k] = v
			}
		}
		return m
	}
	const (
		partial    = "# Changelog\n\n## [Unreleased]\n\nSee changelog.d/.\n\n### Added\n\n- b thing\n  wrapped. (epic 2)\n\n### Fixed\n\n- c fixed. (epic 3)\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
		addedOne   = "# Changelog\n\n## [Unreleased]\n\nSee changelog.d/.\n\n- d new. (epic 4)\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
		halfBullet = "# Changelog\n\n## [Unreleased]\n\nSee changelog.d/.\n\n  wrapped. (epic 2)\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
		pointerCut = "# Changelog\n\n## [Unreleased]\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
		headingCut = "# Changelog\n\n## [Unreleased]\n\nSee changelog.d/.\n\n- b thing\n  wrapped. (epic 2)\n\n### Fixed\n\n- c fixed. (epic 3)\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
		blankCut   = "# Changelog\n\n## [Unreleased]\n\nSee changelog.d/.\n### Added\n\n- a thing. (epic 1)\n- b thing\n  wrapped. (epic 2)\n\n### Fixed\n\n- c fixed. (epic 3)\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
		reordered  = "# Changelog\n\n## [Unreleased]\n\nSee changelog.d/.\n\n### Added\n\n- b thing\n  wrapped. (epic 2)\n- a thing. (epic 1)\n\n### Fixed\n\n- c fixed. (epic 3)\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
	)
	tests := []struct {
		name    string
		changes []change
		frags   map[string]string
		head    string
		fail    bool
	}{
		{"all removed bullets in added fragments passes", []change{fragA, fragB, fragC}, all, logEmpty, false},
		{"partial move passes", []change{fragA}, map[string]string{"changelog.d/added-a.md": all["changelog.d/added-a.md"]}, partial, false},
		{"bullets split across one fragment pass", []change{fragA}, map[string]string{"changelog.d/added-a.md": "- c fixed. (epic 3)\n- a thing. (epic 1)\n- b thing\n  wrapped. (epic 2)\n"}, logEmpty, false},
		{"removed bullet missing from fragments fails", []change{fragA, fragC}, without("changelog.d/added-b.md"), logEmpty, true},
		{"removed bullet only in a modified fragment fails", []change{fragA, {'M', "changelog.d/added-b.md"}, fragC}, without("changelog.d/added-b.md"), logEmpty, true},
		{"bullet reworded in fragment fails", []change{fragA, fragB, fragC}, map[string]string{"changelog.d/added-a.md": "- a thing (epic 1)\n", "changelog.d/added-b.md": all["changelog.d/added-b.md"], "changelog.d/fixed-c.md": all["changelog.d/fixed-c.md"]}, logEmpty, true},
		{"bullet first line only in fragment fails", []change{fragA, fragB, fragC}, map[string]string{"changelog.d/added-a.md": all["changelog.d/added-a.md"], "changelog.d/added-b.md": "- b thing\n", "changelog.d/fixed-c.md": all["changelog.d/fixed-c.md"]}, logEmpty, true},
		{"bullet text as a substring of a fragment line fails", []change{fragA, fragB, fragC}, map[string]string{"changelog.d/added-a.md": "- a thing. (epic 1) more\n", "changelog.d/added-b.md": all["changelog.d/added-b.md"], "changelog.d/fixed-c.md": all["changelog.d/fixed-c.md"]}, logEmpty, true},
		{"bullet added to Unreleased fails", []change{fragA, fragB, fragC}, all, addedOne, true},
		{"half a bullet removed fails", []change{fragA, fragB, fragC}, all, halfBullet, true},
		{"pointer line removed fails", []change{fragA, fragB, fragC}, all, pointerCut, true},
		{"reorder only fails", nil, nil, reordered, true},
		{"heading removed over a kept bullet fails", []change{fragA}, all, headingCut, true},
		{"only a blank line removed fails", []change{fragA, fragB, fragC}, all, blankCut, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(evaluate(tc.changes, tc.frags, logListed, tc.head, "", nil), "\n")
			if tc.fail != strings.Contains(got, "edits the \"## [Unreleased]\"") {
				t.Fatalf("fail=%v, got %q", tc.fail, got)
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	code := change{'M', "internal/fanout/review.go"}
	frag := change{'A', "changelog.d/fixed-thing.md"}
	tests := []struct {
		name     string
		changes  []change
		head     string
		body     string
		labels   []string
		wantSubs []string
	}{
		{"code without fragment fails", []change{code}, logEmpty, "", nil, []string{"adds no fragment"}},
		{"code with fragment passes", []change{code, frag}, logEmpty, "", nil, nil},
		{"each code dir needs one", []change{{'A', "personas/x.md"}}, logEmpty, "", nil, []string{"adds no fragment"}},
		{"cli dir", []change{{'M', "cli/review.go"}}, logEmpty, "", nil, []string{"adds no fragment"}},
		{"cmd dir", []change{{'M', "cmd/x/main.go"}}, logEmpty, "", nil, []string{"adds no fragment"}},
		{"reconcile dir", []change{{'M', "reconcile/x.go"}}, logEmpty, "", nil, []string{"adds no fragment"}},
		{"test-only passes", []change{{'M', "internal/fanout/review_test.go"}}, logEmpty, "", nil, nil},
		{"docs only passes", []change{{'M', "docs/x.md"}}, logEmpty, "", nil, nil},
		{"label opts out", []change{code}, logEmpty, "", []string{"bug", "no-changelog"}, nil},
		{"body line opts out", []change{code}, logEmpty, "Refactor.\r\n\r\nChangelog: none\r\n", nil, nil},
		{"body line inside a comment does not", []change{code}, logEmpty, "<!-- changelog: none -->", nil, []string{"adds no fragment"}},
		{"body line mid-sentence does not", []change{code}, logEmpty, "set changelog: none to skip", nil, []string{"adds no fragment"}},
		{"README fragment does not count", []change{code, {'A', "changelog.d/README.md"}}, logEmpty, "", nil, []string{"adds no fragment"}},
		{"bad category does not count", []change{code, {'A', "changelog.d/misc-thing.md"}}, logEmpty, "", nil, []string{"adds no fragment"}},
		{"nested fragment does not count", []change{code, {'A', "changelog.d/sub/fixed-a.md"}}, logEmpty, "", nil, []string{"adds no fragment"}},
		{"modified fragment does not count", []change{code, {'M', "changelog.d/fixed-thing.md"}}, logEmpty, "", nil, []string{"adds no fragment"}},
		{"direct Unreleased edit fails", []change{frag}, logEdited, "", nil, []string{"edits the \"## [Unreleased]\""}},
		{"opt-out does not excuse Unreleased edit", []change{code}, logEdited, "changelog: none", []string{"no-changelog"}, []string{"edits the \"## [Unreleased]\""}},
		{"header edit passes", []change{frag}, logHeader, "", nil, nil},
		{"both failures reported", []change{code}, logEdited, "", nil, []string{"adds no fragment", "edits the"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(evaluate(tc.changes, nil, logEmpty, tc.head, tc.body, tc.labels), "\n")
			if len(tc.wantSubs) == 0 && got != "" {
				t.Fatalf("want pass, got %q", got)
			}
			for _, s := range tc.wantSubs {
				if !strings.Contains(got, s) {
					t.Errorf("want %q in %q", s, got)
				}
			}
		})
	}
}

func TestParseLabels(t *testing.T) {
	if l, err := parseLabels(""); err != nil || l != nil {
		t.Fatalf("empty: %v %v", l, err)
	}
	if l, err := parseLabels(`["a","no-changelog"]`); err != nil || len(l) != 2 {
		t.Fatalf("json: %v %v", l, err)
	}
	if _, err := parseLabels("a,b"); err == nil {
		t.Fatal("want error for non-JSON")
	}
}

func fx(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitexec.FixtureCommandContext(context.Background(), dir, args...)
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestCheck_GitBranches runs `check` against real branches: one without a
// fragment fails, the same change with a fragment passes.
func TestCheck_GitBranches(t *testing.T) {
	dir := t.TempDir()
	fx(t, dir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), logEmpty)
	if err := os.MkdirAll(filepath.Join(dir, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "internal", "a.go"), "package internal\n")
	fx(t, dir, "add", "-A")
	fx(t, dir, "commit", "-q", "-m", "base")
	base := fx(t, dir, "rev-parse", "HEAD")

	fx(t, dir, "checkout", "-q", "-b", "nofrag")
	writeFile(t, filepath.Join(dir, "internal", "a.go"), "package internal\n\nvar X = 1\n")
	fx(t, dir, "commit", "-q", "-am", "code")
	noFrag := fx(t, dir, "rev-parse", "HEAD")

	fx(t, dir, "checkout", "-q", "-b", "frag")
	if err := os.MkdirAll(filepath.Join(dir, "changelog.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "changelog.d", "fixed-x.md"), "- Fixed x. (epic 1)\n")
	fx(t, dir, "add", "-A")
	fx(t, dir, "commit", "-q", "-m", "frag")
	withFrag := fx(t, dir, "rev-parse", "HEAD")

	fx(t, dir, "checkout", "-q", "-b", "direct", noFrag)
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), logEdited)
	fx(t, dir, "commit", "-q", "-am", "direct")
	direct := fx(t, dir, "rev-parse", "HEAD")

	do := func(head string) (int, string) {
		var o, e bytes.Buffer
		t.Setenv(envBody, "")
		t.Setenv(envLabels, "")
		c := run([]string{"check", "--repo", dir, "--base", base, "--head", head}, &o, &e)
		return c, e.String()
	}
	if c, msg := do(noFrag); c != 1 || !strings.Contains(msg, "adds no fragment") {
		t.Errorf("no fragment: code %d, %q", c, msg)
	}
	if c, msg := do(withFrag); c != 0 {
		t.Errorf("fragment: code %d, %q", c, msg)
	}
	if c, msg := do(direct); c != 1 || !strings.Contains(msg, "directly") {
		t.Errorf("direct edit: code %d, %q", c, msg)
	}
	t.Setenv(envLabels, `["no-changelog"]`)
	var o, e bytes.Buffer
	if c := run([]string{"check", "--repo", dir, "--base", base, "--head", noFrag}, &o, &e); c != 0 {
		t.Errorf("label opt-out: code %d, %q", c, e.String())
	}
}

func TestCheck_Usage(t *testing.T) {
	var o, e bytes.Buffer
	if c := run([]string{"check"}, &o, &e); c != 2 {
		t.Fatalf("code %d", c)
	}
}

// TestCheck_IgnoresExternalDiff: a diff.external in the repo's config must not run.
func TestCheck_IgnoresExternalDiff(t *testing.T) {
	dir := t.TempDir()
	fx(t, dir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), logEmpty)
	fx(t, dir, "add", "-A")
	fx(t, dir, "commit", "-q", "-m", "base")
	base := fx(t, dir, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), logHeader)
	fx(t, dir, "commit", "-q", "-am", "next")
	head := fx(t, dir, "rev-parse", "HEAD")

	marker := filepath.Join(dir, "PWNED")
	script := filepath.Join(t.TempDir(), "ext.sh")
	writeFile(t, script, "#!/bin/sh\ntouch '"+marker+"'\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	fx(t, dir, "config", "diff.external", script)

	var o, e bytes.Buffer
	t.Setenv(envBody, "")
	t.Setenv(envLabels, "")
	if c := run([]string{"check", "--repo", dir, "--base", base, "--head", head}, &o, &e); c != 0 {
		t.Fatalf("code %d: %s", c, e.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("diff.external ran")
	}
}

// TestEvaluate_MigrationKeepsHeading: moving every bullet out must not also
// rename or drop the "## [Unreleased]" heading, which assemble relies on.
func TestEvaluate_MigrationKeepsHeading(t *testing.T) {
	const (
		base     = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- a thing. (epic 1)\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
		kept     = "# Changelog\n\n## [Unreleased]\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
		renamed  = "# Changelog\n\n## [Released]\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
		headless = "# Changelog\n\n## [0.5.0] - 2026-10-27\n\n- old\n"
	)
	changes := []change{{'A', "changelog.d/added-a.md"}}
	frags := map[string]string{"changelog.d/added-a.md": "- a thing. (epic 1)\n"}
	for _, tc := range []struct {
		name, head string
		fail       bool
	}{
		{"heading kept passes", kept, false},
		{"heading renamed fails", renamed, true},
		{"heading deleted fails", headless, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(evaluate(changes, frags, base, tc.head, "", nil), "\n")
			if tc.fail != strings.Contains(got, "edits the \"## [Unreleased]\"") {
				t.Fatalf("fail=%v, got %q", tc.fail, got)
			}
		})
	}
}

// TestCheck_GitMigration runs `check` on a branch that moves the Unreleased
// bullets into fragments, and on one that drops a bullet without a fragment.
func TestCheck_GitMigration(t *testing.T) {
	dir := t.TempDir()
	fx(t, dir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), logListed)
	fx(t, dir, "add", "-A")
	fx(t, dir, "commit", "-q", "-m", "base")
	base := fx(t, dir, "rev-parse", "HEAD")

	fx(t, dir, "checkout", "-q", "-b", "migrate")
	if err := os.MkdirAll(filepath.Join(dir, "changelog.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "CHANGELOG.md"), logEmpty)
	writeFile(t, filepath.Join(dir, "changelog.d", "added-a.md"), "- a thing. (epic 1)\n")
	writeFile(t, filepath.Join(dir, "changelog.d", "added-b.md"), "- b thing\n  wrapped. (epic 2)\n")
	writeFile(t, filepath.Join(dir, "changelog.d", "fixed-c.md"), "- c fixed. (epic 3)\n")
	fx(t, dir, "add", "-A")
	fx(t, dir, "commit", "-q", "-m", "migrate")
	migrate := fx(t, dir, "rev-parse", "HEAD")

	fx(t, dir, "rm", "-q", "changelog.d/fixed-c.md")
	fx(t, dir, "commit", "-q", "-m", "lose one")
	lossy := fx(t, dir, "rev-parse", "HEAD")

	t.Setenv(envBody, "")
	t.Setenv(envLabels, "")
	var o, e bytes.Buffer
	if c := run([]string{"check", "--repo", dir, "--base", base, "--head", migrate}, &o, &e); c != 0 {
		t.Errorf("migration: code %d, %q", c, e.String())
	}
	e.Reset()
	if c := run([]string{"check", "--repo", dir, "--base", base, "--head", lossy}, &o, &e); c != 1 || !strings.Contains(e.String(), "directly") {
		t.Errorf("lossy migration: code %d, %q", c, e.String())
	}
}
