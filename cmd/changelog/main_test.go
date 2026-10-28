package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const baseChangelog = "# Changelog\n\nintro\n\n## [Unreleased]\n\nSee changelog.d/.\n\n## [0.5.0] - 2026-10-27\n\n### Added\n\n- old entry. (epic 1)\n"

// setup writes CHANGELOG.md and the fragments into a temp dir.
func setup(t *testing.T, changelog string, frags map[string]string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "changelog.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if changelog != "" {
		writeFile(t, filepath.Join(dir, "CHANGELOG.md"), changelog)
	}
	for name, body := range frags {
		writeFile(t, filepath.Join(dir, "changelog.d", name), body)
	}
	return dir
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runIn(dir string, args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	full := append([]string{"assemble", "--changelog", filepath.Join(dir, "CHANGELOG.md"), "--dir", filepath.Join(dir, "changelog.d")}, args...)
	code = run(full, &o, &e)
	return code, o.String(), e.String()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAssemble_EachCategory(t *testing.T) {
	for _, cat := range []string{"added", "changed", "deprecated", "removed", "fixed", "security"} {
		t.Run(cat, func(t *testing.T) {
			dir := setup(t, baseChangelog, map[string]string{cat + "-thing.md": "- did " + cat + ". (epic 1)\n"})
			code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01")
			if code != 0 {
				t.Fatalf("code %d: %s", code, stderr)
			}
			got := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
			title := strings.ToUpper(cat[:1]) + cat[1:]
			want := "## [1.0.0] - 2026-11-01\n\n### " + title + "\n\n- did " + cat + ". (epic 1)\n\n## [0.5.0]"
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
			if _, err := os.Stat(filepath.Join(dir, "changelog.d", cat+"-thing.md")); !os.IsNotExist(err) {
				t.Errorf("fragment not deleted: %v", err)
			}
		})
	}
}

func TestAssemble_OrderAndPlacement(t *testing.T) {
	dir := setup(t, baseChangelog, map[string]string{
		"security-z.md": "- sec. (epic 1)\n",
		"fixed-b.md":    "- fix b. (epic 1)\n",
		"fixed-a.md":    "- fix a. (epic 1)\n- fix a2. (epic 1)\n",
		"added-x.md":    "- add. (epic 1)\n",
		"removed-r.md":  "- rem. (epic 1)\n",
		"deprecated-d.md": "- dep. (epic 1)\n",
		"changed-c.md":  "- chg. (epic 1)\n",
		"README.md":     "not a fragment\n",
		".gitkeep":      "",
	})
	code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01")
	if code != 0 {
		t.Fatalf("code %d: %s", code, stderr)
	}
	want := "## [Unreleased]\n\nSee changelog.d/.\n\n" +
		"## [1.0.0] - 2026-11-01\n\n" +
		"### Added\n\n- add. (epic 1)\n\n" +
		"### Changed\n\n- chg. (epic 1)\n\n" +
		"### Deprecated\n\n- dep. (epic 1)\n\n" +
		"### Removed\n\n- rem. (epic 1)\n\n" +
		"### Fixed\n\n- fix a. (epic 1)\n- fix a2. (epic 1)\n- fix b. (epic 1)\n\n" +
		"### Security\n\n- sec. (epic 1)\n\n" +
		"## [0.5.0] - 2026-10-27\n"
	got := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	if !strings.Contains(got, want) {
		t.Errorf("got:\n%s\nwant to contain:\n%s", got, want)
	}
	for _, keep := range []string{"README.md", ".gitkeep"} {
		if _, err := os.Stat(filepath.Join(dir, "changelog.d", keep)); err != nil {
			t.Errorf("%s was removed: %v", keep, err)
		}
	}
}

func TestAssemble_InvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		frags   map[string]string
		args    []string
		changes string
		wantErr string
	}{
		{"invalid category", map[string]string{"bogus-x.md": "- a\n"}, nil, baseChangelog, "bogus-x.md"},
		{"no slug", map[string]string{"added.md": "- a\n"}, nil, baseChangelog, "added.md"},
		{"not markdown", map[string]string{"added-x.txt": "- a\n"}, nil, baseChangelog, "added-x.txt"},
		{"empty fragment", map[string]string{"added-x.md": "\n"}, nil, baseChangelog, "no bullet"},
		{"no bullet", map[string]string{"added-x.md": "prose\n"}, nil, baseChangelog, "must start with"},
		{"bad version", map[string]string{"added-x.md": "- a\n"}, []string{"--version", "v1"}, baseChangelog, "version"},
		{"bad date", map[string]string{"added-x.md": "- a\n"}, []string{"--date", "11/01/2026"}, baseChangelog, "date"},
		{"no unreleased", map[string]string{"added-x.md": "- a\n"}, nil, "# Changelog\n", "[Unreleased]"},
		{"version exists", map[string]string{"added-x.md": "- a\n"}, []string{"--version", "0.5.0"}, baseChangelog, "already"},
		{"empty dir", nil, nil, baseChangelog, "no fragments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setup(t, tt.changes, tt.frags)
			args := []string{"--version", "1.0.0", "--date", "2026-11-01"}
			if tt.args != nil {
				args = append(args, tt.args...) // later flags win
			}
			code, _, stderr := runIn(dir, args...)
			if code == 0 {
				t.Fatalf("expected failure")
			}
			if !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("stderr %q lacks %q", stderr, tt.wantErr)
			}
			if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); got != tt.changes {
				t.Errorf("CHANGELOG.md changed on failure:\n%s", got)
			}
			for name := range tt.frags {
				if _, err := os.Stat(filepath.Join(dir, "changelog.d", name)); err != nil {
					t.Errorf("fragment %s removed on failure", name)
				}
			}
		})
	}
}

func TestAssemble_DryRun(t *testing.T) {
	dir := setup(t, baseChangelog, map[string]string{"added-x.md": "- add. (epic 1)\n", "fixed-y.md": "- fix. (epic 1)\n"})
	code, stdout, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01", "--dry-run")
	if code != 0 {
		t.Fatalf("code %d: %s", code, stderr)
	}
	want := "## [1.0.0] - 2026-11-01\n\n### Added\n\n- add. (epic 1)\n\n### Fixed\n\n- fix. (epic 1)\n"
	if stdout != want {
		t.Errorf("stdout:\n%q\nwant:\n%q", stdout, want)
	}
	if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); got != baseChangelog {
		t.Errorf("dry-run wrote CHANGELOG.md")
	}
	for _, n := range []string{"added-x.md", "fixed-y.md"} {
		if _, err := os.Stat(filepath.Join(dir, "changelog.d", n)); err != nil {
			t.Errorf("dry-run removed %s", n)
		}
	}
}

func TestAssemble_SkipsSymlinksAndDirs(t *testing.T) {
	dir := setup(t, baseChangelog, map[string]string{"added-x.md": "- add. (epic 1)\n"})
	writeFile(t, filepath.Join(dir, "outside.md"), "- secret\n")
	if err := os.Symlink(filepath.Join(dir, "outside.md"), filepath.Join(dir, "changelog.d", "fixed-link.md")); err != nil {
		t.Skip("symlinks unsupported")
	}
	if err := os.Mkdir(filepath.Join(dir, "changelog.d", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01")
	if code != 0 {
		t.Fatalf("code %d: %s", code, stderr)
	}
	if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); strings.Contains(got, "secret") {
		t.Errorf("symlink target was read")
	}
}

func TestAssemble_AppendsWhenNoLaterSection(t *testing.T) {
	dir := setup(t, "# Changelog\n\n## [Unreleased]\n", map[string]string{"added-x.md": "- add. (epic 1)"})
	if code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01"); code != 0 {
		t.Fatalf("code %d: %s", code, stderr)
	}
	want := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-11-01\n\n### Added\n\n- add. (epic 1)\n"
	if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRun_Usage(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"assemble"}, {"assemble", "--version", "1.0.0"}, {"assemble", "--bad"}, {"assemble", "--version", "1.0.0", "--date", "2026-11-01", "extra"}} {
		var o, e bytes.Buffer
		if code := run(args, &o, &e); code != 2 {
			t.Errorf("run(%v) = %d, want 2 (%s)", args, code, e.String())
		}
	}
}
