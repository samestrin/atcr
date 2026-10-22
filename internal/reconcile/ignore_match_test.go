package reconcile

import (
	"regexp"
	"strings"
	"testing"
)

// anchoredFinding is a reconciled finding as RunReconcile holds it after
// stampSymbolAnchors: the Problem leads with the "(symbol) " anchor.
func anchoredFinding(file string, line int, symbol, category, problem string) JSONFinding {
	return JSONFinding{
		Severity: "MEDIUM",
		File:     file,
		Line:     line,
		Problem:  "(" + symbol + ") " + problem,
		Category: category,
	}
}

func mustMatcher(t *testing.T, rules ...IgnoreRule) *IgnoreMatcher {
	t.Helper()
	m, err := NewIgnoreMatcher(rules)
	if err != nil {
		t.Fatalf("NewIgnoreMatcher: %v", err)
	}
	return m
}

// TestIgnoreMatcher_ToleratesLineDrift is the Epic 40.0 T2 success line: a
// rule derived from one finding keeps matching it after its line moves by 40,
// because the line is never compared and the Problem still carries the same
// "(symbol) " anchor; a different symbol, file, package or category does not.
func TestIgnoreMatcher_ToleratesLineDrift(t *testing.T) {
	orig := anchoredFinding("internal/mem/pool.go", 112, "Pool.Get", "correctness",
		"Avoid using unsafe.Pointer here")
	rule := IgnoreRule{
		File:     "internal/mem/pool.go",
		Category: "correctness",
		Match:    regexp.QuoteMeta(orig.Problem),
		Reason:   "this package requires unsafe.Pointer",
	}
	pkgRule := IgnoreRule{
		Package:  "internal/mem",
		Category: "correctness",
		Match:    regexp.QuoteMeta(orig.Problem),
		Reason:   "this package requires unsafe.Pointer",
	}

	moved := orig
	moved.Line += 40

	otherSymbol := moved
	otherSymbol.Problem = "(Pool.Put) Avoid using unsafe.Pointer here"

	otherFile := moved
	otherFile.File = "internal/mem/arena.go"

	otherPackage := moved
	otherPackage.File = "internal/memx/pool.go"

	otherCategory := moved
	otherCategory.Category = "security"

	for _, tc := range []struct {
		name string
		rule IgnoreRule
	}{{"file rule", rule}, {"package rule", pkgRule}} {
		m := mustMatcher(t, tc.rule)
		cases := []struct {
			name string
			f    JSONFinding
			want bool
		}{
			{"original line", orig, true},
			{"line moved by 40", moved, true},
			{"different symbol", otherSymbol, false},
			{"different package", otherPackage, false},
			{"different category", otherCategory, false},
		}
		if tc.rule.File != "" {
			cases = append(cases, struct {
				name string
				f    JSONFinding
				want bool
			}{"different file", otherFile, false})
		} else {
			// A package rule covers every file in the directory, so a sibling
			// file with the same anchor and text is the same suppression.
			cases = append(cases, struct {
				name string
				f    JSONFinding
				want bool
			}{"sibling file in the package", otherFile, true})
		}
		for _, c := range cases {
			got, ok := m.Match(c.f)
			if ok != c.want {
				t.Errorf("%s / %s: Match = %v, want %v", tc.name, c.name, ok, c.want)
			}
			if ok && got.Reason != tc.rule.Reason {
				t.Errorf("%s / %s: matched rule reason %q, want %q", tc.name, c.name, got.Reason, tc.rule.Reason)
			}
		}
	}
}

// TestIgnoreMatcher_GlobCoversOnlyItsFiles shows a glob rule matches exactly
// the files its pattern covers: `*` stays inside one path segment and `**`
// spans any number of them, including none.
func TestIgnoreMatcher_GlobCoversOnlyItsFiles(t *testing.T) {
	cases := []struct {
		glob string
		hit  []string
		miss []string
	}{
		{
			glob: "internal/gen/*.go",
			hit:  []string{"internal/gen/a.go", "internal/gen/b_test.go", "./internal/gen/a.go"},
			miss: []string{"internal/gen/sub/a.go", "internal/gen/a.txt", "internal/generated/a.go", "x/internal/gen/a.go"},
		},
		{
			glob: "internal/gen/**",
			hit:  []string{"internal/gen/a.go", "internal/gen/sub/deep/a.txt"},
			miss: []string{"internal/generated/a.go", "internal/a.go"},
		},
		{
			glob: "**/*_mock.go",
			hit:  []string{"a_mock.go", "pkg/a_mock.go", "pkg/sub/b_mock.go"},
			miss: []string{"pkg/a.go", "pkg/a_mock.go.bak"},
		},
		{
			glob: "cmd/*/main.go",
			hit:  []string{"cmd/atcr/main.go"},
			miss: []string{"cmd/main.go", "cmd/a/b/main.go"},
		},
		{
			glob: "internal/x/[ab].go",
			hit:  []string{"internal/x/a.go", "internal/x/b.go"},
			miss: []string{"internal/x/c.go"},
		},
	}
	for _, c := range cases {
		m := mustMatcher(t, IgnoreRule{File: c.glob, Reason: "generated"})
		for _, f := range c.hit {
			if _, ok := m.Match(JSONFinding{File: f, Problem: "p"}); !ok {
				t.Errorf("glob %q: %q should match", c.glob, f)
			}
		}
		for _, f := range c.miss {
			if _, ok := m.Match(JSONFinding{File: f, Problem: "p"}); ok {
				t.Errorf("glob %q: %q should not match", c.glob, f)
			}
		}
	}
}

// TestIgnoreMatcher_ScopeAndOptionalFields covers how the fields combine: a
// rule with both file and package must satisfy both, an unset category or
// match narrows nothing, category compares the way merge normalises it, and
// the first matching rule wins.
func TestIgnoreMatcher_ScopeAndOptionalFields(t *testing.T) {
	f := anchoredFinding("pkg/a.go", 7, "Do", "style", "naming is off")

	if _, ok := mustMatcher(t, IgnoreRule{File: "pkg/a.go", Package: "other", Reason: "r"}).Match(f); ok {
		t.Error("file and package must both hold: package mismatch matched")
	}
	if _, ok := mustMatcher(t, IgnoreRule{File: "pkg/a.go", Package: "pkg", Reason: "r"}).Match(f); !ok {
		t.Error("file and package both hold but did not match")
	}
	if _, ok := mustMatcher(t, IgnoreRule{Package: "pkg/", Reason: "r"}).Match(f); !ok {
		t.Error("a trailing slash on package should not change its meaning")
	}
	if _, ok := mustMatcher(t, IgnoreRule{Package: ".", Reason: "r"}).Match(JSONFinding{File: "main.go"}); !ok {
		t.Error(`package "." should cover a repo-root file`)
	}
	if _, ok := mustMatcher(t, IgnoreRule{Package: ".", Reason: "r"}).Match(f); ok {
		t.Error(`package "." must not cover a file in a subdirectory`)
	}
	if _, ok := mustMatcher(t, IgnoreRule{File: "pkg/a.go", Category: " Style ", Reason: "r"}).Match(f); !ok {
		t.Error("category should compare case- and space-insensitively")
	}
	if _, ok := mustMatcher(t, IgnoreRule{File: "pkg/a.go", Match: "naming", Reason: "r"}).Match(f); !ok {
		t.Error("an unanchored match should find a substring of the Problem")
	}
	if _, ok := mustMatcher(t, IgnoreRule{File: "pkg/a.go", Match: "^naming", Reason: "r"}).Match(f); ok {
		t.Error("^naming must not match a Problem that starts with the anchor")
	}
	if _, ok := mustMatcher(t, IgnoreRule{File: `pkg\a.go`, Reason: "r"}).Match(f); !ok {
		t.Error("a backslash-separated rule file should match its slash-separated finding")
	}
	if _, ok := mustMatcher(t, IgnoreRule{File: "pkg/a.go", Reason: "r"}).Match(JSONFinding{File: `pkg\a.go`}); !ok {
		t.Error("a backslash-separated finding file should match its slash-separated rule")
	}

	m := mustMatcher(t,
		IgnoreRule{File: "pkg/b.go", Reason: "first"},
		IgnoreRule{File: "pkg/*.go", Reason: "second"},
		IgnoreRule{Package: "pkg", Reason: "third"},
	)
	got, ok := m.Match(f)
	if !ok || got.Reason != "second" {
		t.Errorf("first matching rule should win: got %q, %v", got.Reason, ok)
	}
}

// TestIgnoreMatcher_NoRules shows an empty or nil matcher matches nothing, so
// T3 can build one unconditionally from a missing ignore file.
func TestIgnoreMatcher_NoRules(t *testing.T) {
	f := anchoredFinding("pkg/a.go", 1, "Do", "style", "x")
	if _, ok := mustMatcher(t).Match(f); ok {
		t.Error("an empty matcher matched")
	}
	var nilM *IgnoreMatcher
	if _, ok := nilM.Match(f); ok {
		t.Error("a nil matcher matched")
	}
}

// TestNewIgnoreMatcher_RejectsBadRules shows the matcher refuses a rule the
// T1 validator would refuse and a malformed glob, naming the rule, so a bad
// file is a hard error in reconcile instead of a rule that never matches.
func TestNewIgnoreMatcher_RejectsBadRules(t *testing.T) {
	for _, c := range []struct {
		name string
		rule IgnoreRule
		want []string
	}{
		{"malformed glob", IgnoreRule{File: "pkg/[a.go", Reason: "r"}, []string{"rule 1", "pkg/[a.go", "glob"}},
		{"no reason", IgnoreRule{File: "pkg/a.go"}, []string{"rule 1", "reason"}},
		{"bad regex", IgnoreRule{File: "pkg/a.go", Match: "(", Reason: "r"}, []string{"rule 1", "match"}},
	} {
		_, err := NewIgnoreMatcher([]IgnoreRule{c.rule})
		if err == nil {
			t.Errorf("%s: want an error", c.name)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q does not mention %q", c.name, err, w)
			}
		}
	}
}
