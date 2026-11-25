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
			name:    "loose-list bullet",
			frags:   map[string]string{"added-a.md": "- a, with a loose list\n\n  and a second paragraph. (epic 1)\n"},
			stuck:   "added-a.md",
			wantErr: "- a, with a loose list",
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

			// Dry-run still prints the section, and warns about the bullet the
			// real run refuses (debt row 5e512b789302f4f8).
			code, stdout, stderr := runIn(dir, "--version", "1.1.0", "--date", "2026-11-02", "--dry-run")
			if code != 0 || !strings.Contains(stdout, tt.wantErr) {
				t.Errorf("dry-run: code %d, stdout %q, stderr %s", code, stdout, stderr)
			}
			for _, want := range []string{"warning:", survivor, tt.wantErr, "already in"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("dry-run stderr lacks %q:\n%s", want, stderr)
				}
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
		"- nested entry. (epic 1)\n  - a sub-point.\n" +
		"- loose entry.\n\n  with a second paragraph. (epic 1)\n"
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
		// Loose lists: a blank line inside a bullet must not end it, or two
		// bullets sharing a first paragraph compare equal (debt row 760c60b5e12e54e8).
		"loose list, different paragraph": "- loose entry.\n\n  with another paragraph. (epic 2)\n",
		"first paragraph of a loose list": "- loose entry.\n",
		"tight version of a loose bullet": "- loose entry.\n  with a second paragraph. (epic 1)\n",
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

// TestAssemble_AllowConsumedReleasesARepeatedBullet is debt row
// bcc4c5e9e4d3fa67: a bullet that legitimately repeats an earlier release's
// entry is refused by default, the refusal names both causes, and
// --allow-consumed=<fragment-path> releases it.
func TestAssemble_AllowConsumedReleasesARepeatedBullet(t *testing.T) {
	const repeated = "- old entry. (epic 1)"
	dir := setup(t, baseChangelog, map[string]string{"added-again.md": repeated + "\n"})
	frag := filepath.Join(dir, "changelog.d", "added-again.md")
	before := readFile(t, filepath.Join(dir, "CHANGELOG.md"))

	code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01")
	if code != 1 {
		t.Fatalf("default: code %d, want 1: %s", code, stderr)
	}
	for _, want := range []string{
		frag, repeated,
		"an assemble that could not delete", "delete those fragment files by hand, then re-run",
		"repeats an entry from an earlier release", "re-run with --allow-consumed",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "to recover, delete") {
		t.Errorf("refusal still tells the operator only to delete the fragments:\n%s", stderr)
	}
	if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); got != before {
		t.Errorf("CHANGELOG.md changed on refusal:\n%s", got)
	}

	code, _, stderr = runIn(dir, "--version", "1.0.0", "--date", "2026-11-01", "--allow-consumed="+frag)
	if code != 0 {
		t.Fatalf("--allow-consumed: code %d, want 0: %s", code, stderr)
	}
	got := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	want := "## [1.0.0] - 2026-11-01\n\n### Added\n\n" + repeated + "\n\n## [0.5.0]"
	if !strings.Contains(got, want) {
		t.Errorf("CHANGELOG.md lacks the released repeat %q:\n%s", want, got)
	}
	if _, err := os.Stat(frag); !os.IsNotExist(err) {
		t.Errorf("fragment not removed after --allow-consumed: %v", err)
	}
}

// consumedSetup writes a changelog whose 0.5.0 section holds two entries, and
// three fragments: two repeating those entries and one new.
func consumedSetup(t *testing.T) (dir, again1, again2, fresh string) {
	t.Helper()
	changelog := baseChangelog + "- other old entry. (epic 1)\n"
	dir = setup(t, changelog, map[string]string{
		"added-again1.md": "- old entry. (epic 1)\n",
		"fixed-again2.md": "- other old entry. (epic 1)\n",
		"changed-new.md":  "- new entry. (epic 2)\n",
	})
	d := filepath.Join(dir, "changelog.d")
	return dir, filepath.Join(d, "added-again1.md"), filepath.Join(d, "fixed-again2.md"), filepath.Join(d, "changed-new.md")
}

// assertUntouched fails when CHANGELOG.md differs from before or a fragment is gone.
func assertUntouched(t *testing.T, dir, before string, frags ...string) {
	t.Helper()
	if got := readFile(t, filepath.Join(dir, "CHANGELOG.md")); got != before {
		t.Errorf("CHANGELOG.md changed:\n%s", got)
	}
	for _, f := range frags {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("fragment %s removed: %v", f, err)
		}
	}
}

// TestAssemble_AllowConsumedBareFlagIsAUsageError is debt rows c39c000c46770bfd
// and 9b9305bb3f3afb31: the bare flag released every fragment, so it is now a
// usage error that writes and deletes nothing. Rows 439a3ad68980281a and
// f0813cb41f90f007: the other boolean spellings (=false, =1, =0) are the same
// mistake and get the same usage error naming --allow-consumed=<fragment-path>;
// none of them can be a fragment name (<category>-<slug>.md). Rows
// cddedaf7d2ccf90b and 3a33081d52f27a43: every spelling strconv.ParseBool
// accepts (=True, =TRUE, =t, =F, ...) is the same mistake, not just the
// enumerated literals. Rows ae190369b5f3a6a9, da48c87f7085d471,
// 14bdb8cf2204f389 and 891b8efc33eb3ee0 (owner decision EST-8570: shape
// gate): a value whose base name is not <category>-<slug>.md is refused, so
// all twelve ParseBool spellings and yes/on/y get the same usage error.
func TestAssemble_AllowConsumedBareFlagIsAUsageError(t *testing.T) {
	for name, args := range map[string][]string{
		"bare last":          {"--allow-consumed"},
		"bare before a flag": {"--allow-consumed", "--dry-run"},
		"explicit true":      {"--allow-consumed=true"},
		"explicit false":     {"--allow-consumed=false"},
		"numeric one":        {"--allow-consumed=1"},
		"numeric zero":       {"--allow-consumed=0"},
		"capitalised True":   {"--allow-consumed=True"},
		"uppercase TRUE":     {"--allow-consumed=TRUE"},
		"short t":            {"--allow-consumed=t"},
		"short F":            {"--allow-consumed=F"},
		"short T":            {"--allow-consumed=T"},
		"short f":            {"--allow-consumed=f"},
		"capitalised False":  {"--allow-consumed=False"},
		"uppercase FALSE":    {"--allow-consumed=FALSE"},
		"yes":                {"--allow-consumed=yes"},
		"on":                 {"--allow-consumed=on"},
		"short y":            {"--allow-consumed=y"},
		"no category":        {"--allow-consumed=changelog.d/notes.md"},
		"no .md suffix":      {"--allow-consumed=changelog.d/added-x"},
		"directory":          {"--allow-consumed=changelog.d"},
		"empty value":        {"--allow-consumed="},
		"space-separated":    {"--allow-consumed", "changelog.d/added-again1.md"},
	} {
		t.Run(name, func(t *testing.T) {
			dir, a1, a2, fresh := consumedSetup(t)
			before := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
			code, stdout, stderr := runIn(dir, append([]string{"--version", "1.0.0", "--date", "2026-11-01"}, args...)...)
			if code != 2 {
				t.Fatalf("code %d, want 2: %s", code, stderr)
			}
			if !strings.Contains(stderr, "--allow-consumed=<fragment-path>") {
				t.Errorf("stderr does not name the path form:\n%s", stderr)
			}
			if stdout != "" {
				t.Errorf("stdout not empty: %q", stdout)
			}
			assertUntouched(t, dir, before, a1, a2, fresh)
		})
	}
}

// TestAssemble_AllowConsumedFragmentShapedValuePassesTheGate: a value shaped
// like a fragment path is not a usage error; it reaches the fragment lookup,
// which refuses it (exit 1, not a fragment) because it names no fragment here.
func TestAssemble_AllowConsumedFragmentShapedValuePassesTheGate(t *testing.T) {
	dir, a1, a2, fresh := consumedSetup(t)
	before := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	code, stdout, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01", "--allow-consumed=changelog.d/added-x.md")
	if code != 1 || !strings.Contains(stderr, "changelog.d/added-x.md: not a fragment") {
		t.Fatalf("code %d, want 1 naming no fragment:\n%s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout not empty: %q", stdout)
	}
	assertUntouched(t, dir, before, a1, a2, fresh)
}

// TestAssemble_AllowConsumedRefusesAnUnnamedFragment: naming one consumed
// fragment does not release another.
func TestAssemble_AllowConsumedRefusesAnUnnamedFragment(t *testing.T) {
	dir, a1, a2, fresh := consumedSetup(t)
	before := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01", "--allow-consumed="+a1)
	if code != 1 {
		t.Fatalf("code %d, want 1: %s", code, stderr)
	}
	if !strings.Contains(stderr, a2) || !strings.Contains(stderr, "1 fragment bullet is already in") {
		t.Errorf("refusal does not name only the unnamed fragment %s:\n%s", a2, stderr)
	}
	if strings.Contains(stderr, a1+":") {
		t.Errorf("refusal names the released fragment %s:\n%s", a1, stderr)
	}
	assertUntouched(t, dir, before, a1, a2, fresh)
}

// TestAssemble_AllowConsumedReleasesExactlyTheNamedFragments: a repeated flag
// releases each named fragment, in any spelling of its path.
func TestAssemble_AllowConsumedReleasesExactlyTheNamedFragments(t *testing.T) {
	dir, a1, a2, fresh := consumedSetup(t)
	rel, err := filepath.Rel(mustGetwd(t), a2)
	if err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01",
		"--allow-consumed="+a1, "--allow-consumed="+filepath.Join(".", rel))
	if code != 0 {
		t.Fatalf("code %d, want 0: %s", code, stderr)
	}
	got := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	want := "## [1.0.0] - 2026-11-01\n\n### Added\n\n- old entry. (epic 1)\n\n### Changed\n\n- new entry. (epic 2)\n\n### Fixed\n\n- other old entry. (epic 1)\n\n## [0.5.0]"
	if !strings.Contains(got, want) {
		t.Errorf("CHANGELOG.md lacks %q:\n%s", want, got)
	}
	for _, f := range []string{a1, a2, fresh} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("fragment %s not removed: %v", f, err)
		}
	}
}

// TestAssemble_AllowConsumedRelativePathResolvesLikeDir is debt row
// 022caa5a9600ed5d: a relative --allow-consumed path resolves against the
// working directory exactly like the fragment paths read from a relative
// --dir, so the same relative spelling that names a fragment releases it,
// and a path relative to --dir itself names no fragment. Rows 1d439c278c963dd9
// and f105e358b5c5421e: the refused call must leave CHANGELOG.md and every
// fragment untouched, so a write-before-validate regression fails here.
func TestAssemble_AllowConsumedRelativePathResolvesLikeDir(t *testing.T) {
	dir, a1, a2, fresh := consumedSetup(t)
	before := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	t.Chdir(dir)

	// A name relative to --dir itself resolves against the working directory,
	// not against --dir, so it names no fragment and is refused.
	var o, e bytes.Buffer
	code := run([]string{"assemble", "--changelog", "CHANGELOG.md", "--dir", "changelog.d",
		"--version", "1.0.0", "--date", "2026-11-01", "--allow-consumed=added-again1.md"}, &o, &e)
	if code != 1 || !strings.Contains(e.String(), "not a fragment") {
		t.Fatalf("dir-relative name: code %d, want 1 naming no fragment:\n%s", code, e.String())
	}
	assertUntouched(t, dir, before, a1, a2, fresh)

	// The working-directory-relative spelling matches the fragment paths
	// read from --dir and releases exactly the named fragments.
	o.Reset()
	e.Reset()
	code = run([]string{"assemble", "--changelog", "CHANGELOG.md", "--dir", "changelog.d",
		"--version", "1.0.0", "--date", "2026-11-01",
		"--allow-consumed=changelog.d/added-again1.md", "--allow-consumed=./changelog.d/fixed-again2.md"}, &o, &e)
	if code != 0 {
		t.Fatalf("relative paths: code %d, want 0: %s", code, e.String())
	}
	got := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	want := "## [1.0.0] - 2026-11-01\n\n### Added\n\n- old entry. (epic 1)\n\n### Changed\n\n- new entry. (epic 2)\n\n### Fixed\n\n- other old entry. (epic 1)\n\n## [0.5.0]"
	if !strings.Contains(got, want) {
		t.Errorf("CHANGELOG.md lacks %q:\n%s", want, got)
	}
	for _, f := range []string{a1, a2, fresh} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("fragment %s not removed: %v", f, err)
		}
	}
}

// TestAssemble_AllowConsumedUnknownPath: a path that is no fragment (a typo)
// is refused before any write, so it cannot silently release nothing.
func TestAssemble_AllowConsumedUnknownPath(t *testing.T) {
	dir, a1, a2, fresh := consumedSetup(t)
	before := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	typo := filepath.Join(dir, "changelog.d", "added-agian1.md")
	for _, extra := range [][]string{nil, {"--dry-run"}} {
		args := append([]string{"--version", "1.0.0", "--date", "2026-11-01", "--allow-consumed=" + a1, "--allow-consumed=" + a2, "--allow-consumed=" + typo}, extra...)
		code, stdout, stderr := runIn(dir, args...)
		if code != 1 || !strings.Contains(stderr, typo) || !strings.Contains(stderr, "not a fragment") {
			t.Errorf("%v: code %d, want 1 naming %s:\n%s", extra, code, typo, stderr)
		}
		if stdout != "" {
			t.Errorf("%v: stdout not empty: %q", extra, stdout)
		}
		assertUntouched(t, dir, before, a1, a2, fresh)
	}
}

// TestAssemble_DryRunWarnsOnlyAboutUnreleasedBullets is debt row
// 5e512b789302f4f8: the dry run previews the consumed check the real run makes,
// so it warns about the unnamed fragment and not the released one.
func TestAssemble_DryRunWarnsOnlyAboutUnreleasedBullets(t *testing.T) {
	dir, a1, a2, fresh := consumedSetup(t)
	before := readFile(t, filepath.Join(dir, "CHANGELOG.md"))
	code, stdout, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01", "--dry-run", "--allow-consumed="+a1)
	if code != 0 {
		t.Fatalf("code %d, want 0: %s", code, stderr)
	}
	if !strings.Contains(stdout, "## [1.0.0] - 2026-11-01") {
		t.Errorf("dry run printed no section:\n%s", stdout)
	}
	for _, want := range []string{"warning:", "1 fragment bullet is already in", a2 + ": - other old entry. (epic 1)", "--allow-consumed=<fragment-path>"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, a1+":") {
		t.Errorf("warning names the released fragment %s:\n%s", a1, stderr)
	}
	assertUntouched(t, dir, before, a1, a2, fresh)

	// Every consumed fragment named: the dry run is clean.
	code, _, stderr = runIn(dir, "--version", "1.0.0", "--date", "2026-11-01", "--dry-run", "--allow-consumed="+a1, "--allow-consumed="+a2)
	if code != 0 || stderr != "" {
		t.Errorf("code %d, stderr %q, want 0 and empty", code, stderr)
	}
}

// TestAssemble_DryRunWithoutChangelog: the consumed preview reads CHANGELOG.md
// only when it exists; a dry run before it is created still prints the section.
func TestAssemble_DryRunWithoutChangelog(t *testing.T) {
	dir := setup(t, "", map[string]string{"added-x.md": "- add. (epic 1)\n"})
	code, stdout, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01", "--dry-run")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "- add. (epic 1)") {
		t.Errorf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "CHANGELOG.md")); !os.IsNotExist(err) {
		t.Errorf("dry run created CHANGELOG.md: %v", err)
	}
}

// TestAssemble_DryRunUnreadableChangelog: an error other than a missing file
// fails the dry run rather than hiding the preview.
func TestAssemble_DryRunUnreadableChangelog(t *testing.T) {
	dir := setup(t, "", map[string]string{"added-x.md": "- add. (epic 1)\n"})
	if err := os.Mkdir(filepath.Join(dir, "CHANGELOG.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runIn(dir, "--version", "1.0.0", "--date", "2026-11-01", "--dry-run")
	if code != 1 || stdout != "" {
		t.Errorf("code %d, want 1, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}
