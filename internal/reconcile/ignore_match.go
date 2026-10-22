package reconcile

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/samestrin/atcr/internal/history"
)

// IgnoreMatcher tests reconciled findings against validated ignore rules
// (Epic 40.0 T2). Each rule's regex and glob are compiled once, so matching a
// large finding set costs no recompilation.
//
// A finding's Line is never compared: a rule keys on the file (or glob), the
// package, the category and the Problem text, and the Problem keeps its
// "(symbol) " anchor wherever the line drifts to.
type IgnoreMatcher struct {
	rules []compiledIgnoreRule
}

type compiledIgnoreRule struct {
	rule IgnoreRule
	// file is the slash-normalized File, split into glob segments; nil when
	// the rule has no file scope.
	file []string
	// pkg is the cleaned Package; "" when the rule has no package scope.
	pkg      string
	category string
	match    *regexp.Regexp
}

// NewIgnoreMatcher validates and compiles rules. A rule the T1 validator
// rejects, or whose file glob is malformed, is an error naming the rule, so a
// bad rule fails reconcile loudly instead of silently never matching.
func NewIgnoreMatcher(rules []IgnoreRule) (*IgnoreMatcher, error) {
	m := &IgnoreMatcher{rules: make([]compiledIgnoreRule, 0, len(rules))}
	for i, r := range rules {
		if err := r.validate(i); err != nil {
			return nil, err
		}
		c := compiledIgnoreRule{
			rule:     r,
			category: normalizeIgnoreCategory(r.Category),
		}
		if f := strings.TrimSpace(r.File); f != "" {
			c.file = strings.Split(cleanIgnorePath(f), "/")
			for _, seg := range c.file {
				if _, err := path.Match(seg, ""); err != nil {
					return nil, fmt.Errorf("%s: file is not a valid glob: %w", r.label(i), err)
				}
			}
		}
		if p := strings.TrimSpace(r.Package); p != "" {
			c.pkg = cleanIgnorePath(p)
		}
		if r.Match != "" {
			// validate has already compiled it once; MustCompile cannot panic.
			c.match = regexp.MustCompile(r.Match)
		}
		m.rules = append(m.rules, c)
	}
	return m, nil
}

// Match returns the first rule that covers f. A rule covers f when every
// field it sets holds: File (exact path or glob) and Package (exact
// history.PackageOf directory, not a prefix) scope it, Category compares as
// merge normalizes it, and Match finds the Problem text unanchored. A nil or
// empty matcher matches nothing, and so does a finding with no file: every
// rule is scoped by file or package, and neither can cover a file-less one.
func (m *IgnoreMatcher) Match(f JSONFinding) (IgnoreRule, bool) {
	if m == nil || strings.TrimSpace(f.File) == "" {
		return IgnoreRule{}, false
	}
	file := cleanIgnorePath(f.File)
	for _, c := range m.rules {
		if c.file != nil && !matchGlobSegments(c.file, strings.Split(file, "/")) {
			continue
		}
		if c.pkg != "" && history.PackageOf(file) != c.pkg {
			continue
		}
		if c.category != "" && normalizeIgnoreCategory(f.Category) != c.category {
			continue
		}
		if c.match != nil && !c.match.MatchString(f.Problem) {
			continue
		}
		return c.rule, true
	}
	return IgnoreRule{}, false
}

// cleanIgnorePath folds a rule or finding path to one slash-separated form, so
// `pkg\a.go`, `./pkg/a.go` and `pkg//a.go` all compare as `pkg/a.go`. Folding
// backslashes means a rule cannot escape a glob metacharacter; a repo path
// that needs one is rare enough to scope by package instead.
func cleanIgnorePath(p string) string {
	return path.Clean(strings.ReplaceAll(strings.TrimSpace(p), `\`, "/"))
}

// normalizeIgnoreCategory mirrors reconcile/merge.go, which lowercases and
// trims every merged category, so a rule written as "Security" still matches.
func normalizeIgnoreCategory(c string) string {
	return strings.ToLower(strings.TrimSpace(c))
}

// matchGlobSegments reports whether the path segments name match the glob
// segments pat. A "**" segment matches zero or more whole segments; any other
// segment is a path.Match pattern, so `*` never crosses a slash. Every
// pattern segment was checked by NewIgnoreMatcher, so path.Match cannot fail.
func matchGlobSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchGlobSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if ok, _ := path.Match(pat[0], name[0]); !ok {
		return false
	}
	return matchGlobSegments(pat[1:], name[1:])
}
