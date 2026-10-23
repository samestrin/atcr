package reconcile

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestParseIgnore_RejectsInvalidRules is the Epic 40.0 T1 / AC6 guardrail
// table: each malformed rule is rejected by ParseIgnore with an error that
// names the offending rule (its 1-based position and its scope), so a user
// with a dozen rules can find the bad one without bisecting the file.
func TestParseIgnore_RejectsInvalidRules(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		wantErrs []string
	}{
		{
			name:     "missing reason",
			yaml:     "rules:\n  - file: internal/x.go\n    match: boom\n",
			wantErrs: []string{"rule 1", `file "internal/x.go"`, "reason"},
		},
		{
			name:     "whitespace-only reason",
			yaml:     "rules:\n  - file: internal/x.go\n    reason: \"   \"\n",
			wantErrs: []string{"rule 1", `file "internal/x.go"`, "reason"},
		},
		{
			name:     "neither file nor package",
			yaml:     "rules:\n  - file: ok.go\n    reason: fine\n  - match: unsafe\n    reason: intentional\n",
			wantErrs: []string{"rule 2", `match "unsafe"`, "file", "package"},
		},
		{
			name:     "dotdot file path",
			yaml:     "rules:\n  - file: ../x.go\n    reason: intentional\n",
			wantErrs: []string{"rule 1", `file "../x.go"`, ".."},
		},
		{
			name:     "dotdot inside file path",
			yaml:     "rules:\n  - file: a/../../x.go\n    reason: intentional\n",
			wantErrs: []string{"rule 1", `file "a/../../x.go"`, ".."},
		},
		{
			name:     "dotdot package",
			yaml:     "rules:\n  - package: ../internal\n    reason: intentional\n",
			wantErrs: []string{"rule 1", `package "../internal"`, ".."},
		},
		{
			name:     "absolute file path",
			yaml:     "rules:\n  - file: /abs/x.go\n    reason: intentional\n",
			wantErrs: []string{"rule 1", `file "/abs/x.go"`, "absolute"},
		},
		{
			name:     "absolute package path",
			yaml:     "rules:\n  - package: /abs\n    reason: intentional\n",
			wantErrs: []string{"rule 1", `package "/abs"`, "absolute"},
		},
		{
			name:     "invalid regex",
			yaml:     "rules:\n  - file: x.go\n    match: \"(unclosed\"\n    reason: intentional\n",
			wantErrs: []string{"rule 1", `file "x.go"`, "match", "(unclosed"},
		},
		{
			name:     "PCRE-only regex is not RE2",
			yaml:     "rules:\n  - file: x.go\n    match: \"foo(?=bar)\"\n    reason: intentional\n",
			wantErrs: []string{"rule 1", `file "x.go"`, "match"},
		},
		{
			name:     "malformed file glob",
			yaml:     "rules:\n  - file: \"a[.go\"\n    reason: intentional\n",
			wantErrs: []string{"rule 1", `file "a[.go"`, "glob"},
		},
		{
			name:     "unknown key (typo) is rejected, not silently dropped",
			yaml:     "rules:\n  - file: x.go\n    line: 12\n    reason: intentional\n",
			wantErrs: []string{"line"},
		},
		{
			name:     "not yaml",
			yaml:     "rules: [\n",
			wantErrs: []string{"yaml"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := ParseIgnore([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("ParseIgnore accepted an invalid rule set: %+v", rules)
			}
			for _, want := range tc.wantErrs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
			if rules != nil {
				t.Errorf("rejected parse returned rules %+v, want nil", rules)
			}
		})
	}
}

// TestParseIgnore_AcceptsValidRules pins the accepted shapes: a file path, a
// file glob, a package (history.PackageOf form, "." for the repo root), and
// an optional category and match.
func TestParseIgnore_AcceptsValidRules(t *testing.T) {
	in := `rules:
  - file: internal/reconcile/gate.go
    category: correctness
    match: '\(stampFindingIDs\) '
    reason: the splice is index-aligned on purpose
  - file: internal/**/*_gen.go
    reason: generated code
  - package: .
    match: unsafe\.Pointer
    reason: the root package needs it
`
	rules, err := ParseIgnore([]byte(in))
	if err != nil {
		t.Fatalf("ParseIgnore: %v", err)
	}
	want := []IgnoreRule{
		{File: "internal/reconcile/gate.go", Category: "correctness", Match: `\(stampFindingIDs\) `, Reason: "the splice is index-aligned on purpose"},
		{File: "internal/**/*_gen.go", Reason: "generated code"},
		{Package: ".", Match: `unsafe\.Pointer`, Reason: "the root package needs it"},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules = %+v\nwant   %+v", rules, want)
	}
}

// TestParseIgnore_RejectsCatchAllScope is the F2 guardrail: ignore.yaml is
// read from the reviewed checkout, so a PR could add one rule whose scope is
// every file and turn the gate green with no findings. A scope that names no
// file or package (a wildcard-only glob, ".", or package ".") is refused
// unless match or category narrows it, and the error names the rule.
func TestParseIgnore_RejectsCatchAllScope(t *testing.T) {
	for _, tc := range []struct {
		scope string
		label string
	}{
		{"file: \"**\"", `file "**"`},
		{"file: \"*\"", `file "*"`},
		{"file: .", `file "."`},
		{"file: ./", `file "./"`},
		{"file: \"./**\"", `file "./**"`},
		{"file: \"**/*\"", `file "**/*"`},
		{"file: \"**/**\"", `file "**/**"`},
		{"package: .", `package "."`},
		{"package: ./", `package "./"`},
		{"file: \"**\"\n    package: .", `file "**"`},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			in := "rules:\n  - file: ok.go\n    reason: fine\n  - " + tc.scope + "\n    reason: everything is fine\n"
			rules, err := ParseIgnore([]byte(in))
			if err == nil {
				t.Fatalf("ParseIgnore accepted a catch-all rule: %+v", rules)
			}
			for _, want := range []string{"rule 2", tc.label, "catch-all", "match", "category"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// TestParseIgnore_AcceptsNarrowedCatchAllScope shows a wildcard scope is still
// fine once something else narrows it: a match, a category, or a real
// package alongside the glob.
func TestParseIgnore_AcceptsNarrowedCatchAllScope(t *testing.T) {
	for _, in := range []string{
		"rules:\n  - file: \"**\"\n    match: unsafe\\.Pointer\n    reason: r\n",
		"rules:\n  - file: \"**\"\n    category: style\n    reason: r\n",
		"rules:\n  - file: \"**\"\n    package: internal/gen\n    reason: r\n",
		"rules:\n  - package: .\n    category: style\n    reason: r\n",
		"rules:\n  - file: \"*.go\"\n    reason: r\n",
		"rules:\n  - file: \"**/*_gen.go\"\n    reason: r\n",
	} {
		if _, err := ParseIgnore([]byte(in)); err != nil {
			t.Errorf("ParseIgnore(%q): %v", in, err)
		}
	}
}

// TestAppendIgnoreRule_RejectsCatchAllScope shows the writer path enforces the
// same guard, so `atcr ignore` (or any caller) cannot put one on disk.
func TestAppendIgnoreRule_RejectsCatchAllScope(t *testing.T) {
	p := filepath.Join(t.TempDir(), IgnoreFileName)
	err := AppendIgnoreRule(p, IgnoreRule{File: "**", Reason: "r"})
	if err == nil || !strings.Contains(err.Error(), "catch-all") {
		t.Fatalf("AppendIgnoreRule err = %v, want a catch-all rejection", err)
	}
	if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
		t.Errorf("rejected append created the file (stat err %v)", statErr)
	}
}

func TestParseIgnore_EmptyIsNoRules(t *testing.T) {
	for _, in := range []string{"", "\n", "# only a comment\n", "rules: []\n"} {
		rules, err := ParseIgnore([]byte(in))
		if err != nil {
			t.Fatalf("ParseIgnore(%q): %v", in, err)
		}
		if len(rules) != 0 {
			t.Fatalf("ParseIgnore(%q) = %+v, want no rules", in, rules)
		}
	}
}

// TestIgnoreRules_RoundTripByteForByte is the T1 success criterion's second
// half: a valid rule set written by MarshalIgnore parses back to the same
// rules, and re-marshalling those yields the identical bytes. Regex
// metacharacters and the "(symbol) " anchor are the parts a YAML writer most
// easily mangles, so the fixture carries both.
func TestIgnoreRules_RoundTripByteForByte(t *testing.T) {
	rules := []IgnoreRule{
		{
			File:     "internal/reconcile/gate.go",
			Category: "correctness",
			Match:    `^\(RunReconcile\) Avoid using ` + "`unsafe.Pointer`" + ` here: "x" \d+ #1$`,
			Reason:   "this package requires it: see #40",
		},
		{Package: "cli", Reason: "  leading space and trailing: colon:"},
		{File: "*.pb.go", Category: "style", Reason: "generated"},
	}
	first, err := MarshalIgnore(rules)
	if err != nil {
		t.Fatalf("MarshalIgnore: %v", err)
	}
	parsed, err := ParseIgnore(first)
	if err != nil {
		t.Fatalf("ParseIgnore(MarshalIgnore(rules)): %v\n%s", err, first)
	}
	if !reflect.DeepEqual(parsed, rules) {
		t.Fatalf("round trip changed rules:\n got %+v\nwant %+v", parsed, rules)
	}
	second, err := MarshalIgnore(parsed)
	if err != nil {
		t.Fatalf("MarshalIgnore(parsed): %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round trip not byte-for-byte:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestMarshalIgnore_RefusesInvalidRule keeps the writer from ever putting a
// rule on disk that the parser would then reject (which would turn the next
// reconcile into a hard error).
func TestMarshalIgnore_RefusesInvalidRule(t *testing.T) {
	_, err := MarshalIgnore([]IgnoreRule{{File: "x.go", Reason: "ok"}, {File: "/abs/x.go", Reason: "no"}})
	if err == nil || !strings.Contains(err.Error(), "rule 2") || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("MarshalIgnore error = %v, want one naming rule 2 as absolute", err)
	}
}

func TestReadIgnoreFile_MissingIsNoRules(t *testing.T) {
	rules, err := ReadIgnoreFile(filepath.Join(t.TempDir(), ".atcr", IgnoreFileName))
	if err != nil {
		t.Fatalf("ReadIgnoreFile(missing): %v", err)
	}
	if rules != nil {
		t.Fatalf("ReadIgnoreFile(missing) = %+v, want nil", rules)
	}
}

func TestReadIgnoreFile_MalformedNamesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), IgnoreFileName)
	if err := os.WriteFile(path, []byte("rules:\n  - file: x.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadIgnoreFile(path)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "rule 1") {
		t.Fatalf("ReadIgnoreFile error = %v, want one naming %s and rule 1", err, path)
	}
}

// TestAppendIgnoreRule_CreatesThenAppends covers the writer path T6's
// `atcr ignore` uses: the first append creates .atcr/ and the file, a second
// keeps the first rule ahead of the new one, and an invalid rule leaves the
// file byte-identical.
func TestAppendIgnoreRule_CreatesThenAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".atcr", IgnoreFileName)
	r1 := IgnoreRule{File: "a.go", Match: `\(f\) `, Reason: "one"}
	r2 := IgnoreRule{Package: "cli", Category: "style", Reason: "two"}
	if err := AppendIgnoreRule(path, r1); err != nil {
		t.Fatalf("AppendIgnoreRule(r1): %v", err)
	}
	if err := AppendIgnoreRule(path, r2); err != nil {
		t.Fatalf("AppendIgnoreRule(r2): %v", err)
	}
	got, err := ReadIgnoreFile(path)
	if err != nil {
		t.Fatalf("ReadIgnoreFile: %v", err)
	}
	if want := []IgnoreRule{r1, r2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rules = %+v, want %+v", got, want)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := AppendIgnoreRule(path, IgnoreRule{File: "../x.go", Reason: "no"}); err == nil {
		t.Fatal("AppendIgnoreRule accepted a .. path")
	}
	// A malformed glob would pass the path checks and then fail every later
	// reconcile in NewIgnoreMatcher, so the writer refuses it up front.
	if err := AppendIgnoreRule(path, IgnoreRule{File: "a[.go", Reason: "no"}); err == nil {
		t.Fatal("AppendIgnoreRule accepted a malformed glob")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("rejected append changed the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestAppendIgnoreRule_RefusesMalformedExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), IgnoreFileName)
	bad := []byte("rules:\n  - file: /abs.go\n    reason: x\n")
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendIgnoreRule(path, IgnoreRule{File: "a.go", Reason: "ok"}); err == nil {
		t.Fatal("AppendIgnoreRule appended to a malformed file")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bad, after) {
		t.Fatalf("malformed file was rewritten:\n%s", after)
	}
}

// TestAppendIgnoreRule_KeepsHandWrittenComments: `atcr ignore` edits a file a
// human also edits, so an append must not strip the comments they wrote.
func TestAppendIgnoreRule_KeepsHandWrittenComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), IgnoreFileName)
	seed := "# team conventions\nrules:\n  # generated protobufs\n  - file: '*.pb.go'\n    reason: generated\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendIgnoreRule(path, IgnoreRule{Package: "cli", Reason: "new"}); err != nil {
		t.Fatalf("AppendIgnoreRule: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"# team conventions", "# generated protobufs"} {
		if !strings.Contains(string(out), c) {
			t.Errorf("append dropped comment %q:\n%s", c, out)
		}
	}
	got, err := ReadIgnoreFile(path)
	if err != nil {
		t.Fatalf("ReadIgnoreFile: %v\n%s", err, out)
	}
	if want := []IgnoreRule{{File: "*.pb.go", Reason: "generated"}, {Package: "cli", Reason: "new"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rules = %+v, want %+v", got, want)
	}
}

// TestAppendIgnoreRule_SeedShapes covers the degenerate files a user might
// start from: empty, comment-only, an empty flow list, a null `rules:`, and a
// document with no rules key.
func TestAppendIgnoreRule_SeedShapes(t *testing.T) {
	r := IgnoreRule{File: "a.go", Reason: "r"}
	for _, seed := range []string{"", "# nothing yet\n", "rules: []\n", "rules:\n", "~\n", "{}\n"} {
		path := filepath.Join(t.TempDir(), IgnoreFileName)
		if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := AppendIgnoreRule(path, r); err != nil {
			t.Fatalf("seed %q: AppendIgnoreRule: %v", seed, err)
		}
		got, err := ReadIgnoreFile(path)
		if err != nil {
			out, _ := os.ReadFile(path)
			t.Fatalf("seed %q: ReadIgnoreFile: %v\n%s", seed, err, out)
		}
		if want := []IgnoreRule{r}; !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %q: rules = %+v, want %+v", seed, got, want)
		}
		if out, _ := os.ReadFile(path); strings.Contains(string(out), "[") {
			t.Errorf("seed %q: rules left in flow style:\n%s", seed, out)
		}
	}
}

func TestParseIgnore_RejectsSecondDocument(t *testing.T) {
	_, err := ParseIgnore([]byte("rules: []\n---\nrules:\n  - file: /abs.go\n    reason: x\n"))
	if err == nil || !strings.Contains(err.Error(), "one document") {
		t.Fatalf("ParseIgnore error = %v, want a one-document rejection", err)
	}
}

func TestCheckRulePath_BackslashAndDriveForms(t *testing.T) {
	for _, p := range []string{`..\x.go`, `a\..\..\x.go`, `C:\x.go`, `C:/x.go`, `\\host\share\x.go`} {
		if checkRulePath(p) == nil {
			t.Errorf("checkRulePath(%q) accepted an escaping path", p)
		}
	}
	for _, p := range []string{"x.go", "a/b/x.go", ".", "**/*.go", "a/..b/x.go"} {
		if err := checkRulePath(p); err != nil {
			t.Errorf("checkRulePath(%q) = %v, want accepted", p, err)
		}
	}
}
