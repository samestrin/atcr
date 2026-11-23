package main

import (
	"bytes"
	"errors"
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
		"security-z.md":   "- sec. (epic 1)\n",
		"fixed-b.md":      "- fix b. (epic 1)\n",
		"fixed-a.md":      "- fix a. (epic 1)\n- fix a2. (epic 1)\n",
		"added-x.md":      "- add. (epic 1)\n",
		"removed-r.md":    "- rem. (epic 1)\n",
		"deprecated-d.md": "- dep. (epic 1)\n",
		"changed-c.md":    "- chg. (epic 1)\n",
		"README.md":       "not a fragment\n",
		".gitkeep":        "",
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

func TestAssemble_KeepsRemovingAfterAFailedRemove(t *testing.T) {
	dir := setup(t, baseChangelog, map[string]string{
		"added-a.md":   "- a. (epic 1)\n",
		"added-b.md":   "- b. (epic 1)\n",
		"changed-c.md": "- c. (epic 1)\n",
		"fixed-d.md":   "- d. (epic 1)\n",
	})
	stuck := map[string]bool{"added-b.md": true, "fixed-d.md": true}
	orig := removeFragment
	removeFragment = func(path string) error {
		if stuck[filepath.Base(path)] {
			return &os.PathError{Op: "remove", Path: path, Err: os.ErrPermission}
		}
		return orig(path)
	}
	t.Cleanup(func() { removeFragment = orig })

	code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01")
	if code != 1 {
		t.Fatalf("code %d, want 1: %s", code, stderr)
	}
	if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); !strings.Contains(got, "## [1.0.0] - 2026-11-01") {
		t.Errorf("CHANGELOG.md lacks the new section:\n%s", got)
	}
	for _, name := range []string{"added-a.md", "changed-c.md"} {
		if _, err := os.Stat(filepath.Join(dir, "changelog.d", name)); !os.IsNotExist(err) {
			t.Errorf("%s not removed after an earlier failure: %v", name, err)
		}
	}
	for name := range stuck {
		path := filepath.Join(dir, "changelog.d", name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if !strings.Contains(stderr, path) {
			t.Errorf("stderr does not report %s:\n%s", path, stderr)
		}
	}
	for _, want := range []string{"2 fragments not removed", "delete them by hand", "a re-run will refuse until the leftover fragments are deleted"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestAssemble_OneFailedRemoveIsSingular(t *testing.T) {
	dir := setup(t, baseChangelog, map[string]string{"added-a.md": "- a. (epic 1)\n"})
	orig := removeFragment
	removeFragment = func(path string) error { return &os.PathError{Op: "remove", Path: path, Err: os.ErrPermission} }
	t.Cleanup(func() { removeFragment = orig })

	code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01")
	if code != 1 || !strings.Contains(stderr, "1 fragment not removed") {
		t.Errorf("code %d, stderr:\n%s", code, stderr)
	}
}

// TestAssemble_FailedRemoveNamesThePath pins that the recovery list names each
// fragment by path even when the remove error does not: an os.Remove error is
// a *PathError that already carries it, so the seam returns one that is not.
func TestAssemble_FailedRemoveNamesThePath(t *testing.T) {
	dir := setup(t, baseChangelog, map[string]string{"added-a.md": "- a. (epic 1)\n"})
	orig := removeFragment
	removeFragment = func(string) error { return errors.New("device busy") }
	t.Cleanup(func() { removeFragment = orig })

	code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01")
	if code != 1 {
		t.Fatalf("code %d, want 1: %s", code, stderr)
	}
	want := "  " + filepath.Join(dir, "changelog.d", "added-a.md") + ": device busy"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr lacks %q:\n%s", want, stderr)
	}
}

// surviveAssemble assembles version 1.0.0 with removes of the stuck fragments
// failing, so they survive into the next run as a crashed release leaves them.
func surviveAssemble(t *testing.T, dir string, stuck ...string) {
	t.Helper()
	orig := removeFragment
	removeFragment = func(path string) error {
		for _, s := range stuck {
			if filepath.Base(path) == s {
				return errors.New("device busy")
			}
		}
		return orig(path)
	}
	defer func() { removeFragment = orig }()
	if code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01"); code != 1 {
		t.Fatalf("first assemble: code %d, want 1: %s", code, stderr)
	}
}

func TestAssemble_RefusesBulletAlreadyInChangelog(t *testing.T) {
	tests := []struct {
		name    string
		frags   map[string]string
		stuck   string
		extra   string // appended to the survivor before the second run
		wantErr string // the bullet's first line, as the refusal names it
	}{
		{
			name:    "single-line bullet",
			frags:   map[string]string{"added-a.md": "- a. (epic 1)\n", "fixed-b.md": "- b. (epic 1)\n"},
			stuck:   "fixed-b.md",
			wantErr: "- b. (epic 1)",
		},
		{
			name:    "multi-line bullet",
			frags:   map[string]string{"added-a.md": "- a, which wraps\n  onto a second line. (epic 1)\n"},
			stuck:   "added-a.md",
			wantErr: "- a, which wraps",
		},
		{
			name:    "one consumed bullet among new ones",
			frags:   map[string]string{"changed-c.md": "- c. (epic 1)\n"},
			stuck:   "changed-c.md",
			extra:   "- c2, added after the release. (epic 2)\n",
			wantErr: "- c. (epic 1)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setup(t, baseChangelog, tt.frags)
			surviveAssemble(t, dir, tt.stuck)
			survivor := filepath.Join(dir, "changelog.d", tt.stuck)
			if tt.extra != "" {
				writeFile(t, survivor, readFile(t, survivor)+tt.extra)
			}
			before := readFile(t, filepath.Join(dir, "CHANGELOG.md"))

			code, _, stderr := runIn(dir, "--version", "1.1.0", "--date", "2026-11-02")
			if code != 1 {
				t.Fatalf("code %d, want 1: %s", code, stderr)
			}
			for _, want := range []string{survivor, tt.wantErr, "already in", "delete those fragment files by hand, then re-run"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, stderr)
				}
			}
			if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); got != before {
				t.Errorf("CHANGELOG.md changed on refusal:\n%s", got)
			}
			if _, err := os.Stat(survivor); err != nil {
				t.Errorf("survivor removed on refusal: %v", err)
			}

			// Dry-run is unchanged: it still prints the section.
			code, stdout, stderr := runIn(dir, "--version", "1.1.0", "--date", "2026-11-02", "--dry-run")
			if code != 0 || !strings.Contains(stdout, tt.wantErr) {
				t.Errorf("dry-run: code %d, stdout %q, stderr %s", code, stdout, stderr)
			}
		})
	}
}

// TestAssemble_NewBulletsPass is the known-good side of the guard: a bullet
// matches only as a whole, at line boundaries, with its continuation lines.
func TestAssemble_NewBulletsPass(t *testing.T) {
	changelog := "# Changelog\n\n## [Unreleased]\n\n## [0.5.0] - 2026-10-27\n\n### Added\n\n" +
		"- old entry. (epic 1)\n" +
		"- wrapped entry\n  with a second line. (epic 1)\n" +
		"- nested entry. (epic 1)\n  - a sub-point.\n"
	tests := map[string]string{
		"brand new":                 "- new entry. (epic 2)\n",
		"prefix of an old bullet":   "- old entry.\n",
		"old bullet inside a line":  "- see: - old entry. (epic 1)\n",
		"first line of a wrap":      "- wrapped entry\n",
		"old bullet plus a wrap":    "- old entry. (epic 1)\n  now with more. (epic 2)\n",
		"different continuation":    "- wrapped entry\n  with another line. (epic 2)\n",
		"old bullet without nested": "- nested entry. (epic 1)\n",
		"old continuation as text":  "- x\n  with a second line. (epic 1)\n",
		"several new bullets":       "- one. (epic 2)\n- two\n  wrapped. (epic 2)\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			dir := setup(t, changelog, map[string]string{"added-x.md": body})
			if code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01"); code != 0 {
				t.Fatalf("code %d: %s", code, stderr)
			}
		})
	}
}
