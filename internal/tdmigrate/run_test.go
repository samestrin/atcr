package tdmigrate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRun_NoArgsAndUnknown(t *testing.T) {
	if code, _, _ := run(t); code != 2 {
		t.Errorf("no args: want exit 2, got %d", code)
	}
	if code, _, errb := run(t, "frobnicate"); code != 2 || !strings.Contains(errb, "unknown subcommand") {
		t.Errorf("unknown: got code=%d err=%q", code, errb)
	}
	if code, out, _ := run(t, "--help"); code != 0 || !strings.Contains(out, "td-migrate") {
		t.Errorf("help: got code=%d out=%q", code, out)
	}
}

// TestRun_SubcommandHelpExitsZeroToStdout proves `-h` on each subcommand exits 0
// with usage on stdout (the conventional flag.ErrHelp behavior), not exit 2
// with usage on stderr like a genuine parse error.
func TestRun_SubcommandHelpExitsZeroToStdout(t *testing.T) {
	for _, sub := range []string{"migrate", "generate", "validate"} {
		code, out, errb := run(t, sub, "-h")
		if code != 0 {
			t.Errorf("%s -h: want exit 0, got %d (stderr=%q)", sub, code, errb)
		}
		if out == "" {
			t.Errorf("%s -h: want usage on stdout, got empty stdout (stderr=%q)", sub, errb)
		}
	}
}

// writeREADME drops a minimal valid README into a temp dir for CLI tests.
func writeREADME(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "README.md")
	if err := os.WriteFile(path, []byte(sample9Col+"\n"+sample11Col), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun_MigrateGenerateValidate_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	readme := writeREADME(t, dir)
	items := filepath.Join(dir, "items")

	code, out, errb := run(t, "migrate", "--readme", readme, "--items", items)
	if code != 0 {
		t.Fatalf("migrate failed: code=%d err=%q", code, errb)
	}
	if !strings.Contains(out, "shard(s)") {
		t.Errorf("migrate output unexpected: %q", out)
	}

	code, _, errb = run(t, "validate", "--items", items)
	if code != 0 {
		t.Fatalf("validate failed: code=%d err=%q", code, errb)
	}

	code, gen, errb := run(t, "generate", "--items", items)
	if code != 0 {
		t.Fatalf("generate failed: code=%d err=%q", code, errb)
	}
	// generate emits the regenerated ToC to stdout and must be re-parseable.
	if _, err := ParseREADME(gen); err != nil {
		t.Errorf("generated table not parseable: %v", err)
	}
	if !strings.Contains(gen, "From Sprint: epic-11.2") {
		t.Errorf("generated table missing section: %q", gen)
	}
}

// TestDroppedNotesWarnings verifies that a non-empty Notes field — which the ToC
// table has no column for (kept round-trip-equal to the source README, AC2) — is
// surfaced as a warning rather than silently dropped, one message per item, with
// the shard reference so the note can be found in items/.
func TestDroppedNotesWarnings(t *testing.T) {
	shards := []Shard{{
		Date: "2026-06-26", SourceType: "Sprint", Label: "x",
		Items: []Item{
			{Group: "1", Status: StatusOpen, Severity: "LOW", File: "f.go:1", Problem: "p", Fix: "fix", Category: "correctness", EstMinutes: 5, Source: "s", Notes: "remember the edge case"},
			{Group: "1", Status: StatusOpen, Severity: "LOW", File: "g.go:2", Problem: "p", Fix: "fix", Category: "correctness", EstMinutes: 5, Source: "s"},
		},
	}}
	warns := droppedNotesWarnings(shards)
	if len(warns) != 1 {
		t.Fatalf("want 1 warning (only one item has notes), got %d: %v", len(warns), warns)
	}
	if !strings.Contains(warns[0], "remember the edge case") || !strings.Contains(warns[0], "2026-06-26/x") {
		t.Errorf("warning missing note text or shard ref: %q", warns[0])
	}
	if len(droppedNotesWarnings(nil)) != 0 {
		t.Error("no shards must yield no warnings")
	}
}

// TestRun_GenerateValidateRejectReadmeFlag locks that --readme is migrate-only:
// generate and validate operate on shards, so accepting (and silently ignoring)
// --readme misleads callers. Passing it must be a usage error (exit 2).
func TestRun_GenerateValidateRejectReadmeFlag(t *testing.T) {
	items := filepath.Join(t.TempDir(), "items")
	for _, sub := range []string{"generate", "validate"} {
		if code, _, errb := run(t, sub, "--readme", "x", "--items", items); code != 2 {
			t.Errorf("%s --readme: want exit 2 (flag is migrate-only), got code=%d err=%q", sub, code, errb)
		}
	}
}

func TestRun_MigrateMissingREADME(t *testing.T) {
	if code, _, _ := run(t, "migrate", "--readme", "/no/such/readme.md", "--items", t.TempDir()); code != 1 {
		t.Errorf("want exit 1 for missing README, got %d", code)
	}
}

func TestRun_MigrateRefusesInvalidData(t *testing.T) {
	dir := t.TempDir()
	// An unknown severity normalizes to "MED" which fails the enum check; migrate
	// must refuse loudly rather than write a schema-invalid shard.
	bad := `### [2026-06-26] From Sprint: x

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|---|---|---|---|---|---|---|---|---|
| 1 | [ ] | Med | f.go:1 | p | fix | cat | 5 | src |
`
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	items := filepath.Join(dir, "items")
	if code, _, errb := run(t, "migrate", "--readme", readme, "--items", items); code != 1 || !strings.Contains(errb, "invalid shard") {
		t.Errorf("want exit 1 refusing invalid data, got code=%d err=%q", code, errb)
	}
}

func TestRun_MigrateRefusesEmptyWipe(t *testing.T) {
	dir := t.TempDir()
	// A README with no recognized sections must not silently wipe the store.
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# Technical Debt\n\nNo sections here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	items := filepath.Join(dir, "items")
	if err := os.MkdirAll(items, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(items, "2026-01-01_keep.yaml")
	if err := os.WriteFile(keep, []byte("date: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errb := run(t, "migrate", "--readme", readme, "--items", items)
	if code != 1 || !strings.Contains(errb, "refusing to wipe") {
		t.Errorf("want exit 1 refusing empty wipe, got code=%d err=%q", code, errb)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("existing shard was wiped despite refusal: %v", err)
	}
	// --allow-empty overrides the guard.
	if code, _, _ := run(t, "migrate", "--readme", readme, "--items", items, "--allow-empty"); code != 0 {
		t.Errorf("--allow-empty should permit empty migrate, got code=%d", code)
	}
}

func TestRun_ValidateCatchesBadShard(t *testing.T) {
	items := t.TempDir()
	// Unknown field -> strict-load rejection.
	bad := "date: 2026-06-26\nsource_type: Sprint\nlabel: x\nbogus_field: nope\nitems: []\n"
	if err := os.WriteFile(filepath.Join(items, "bad.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errb := run(t, "validate", "--items", items); code != 1 || !strings.Contains(errb, "validate:") {
		t.Errorf("want exit 1 with validate error, got code=%d err=%q", code, errb)
	}
}
