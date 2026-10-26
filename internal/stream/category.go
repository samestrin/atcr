package stream

import (
	"strings"

	reclib "github.com/samestrin/atcr/reconcile"
)

// Parse-boundary category canonicalization (epic 51.0). Every parse path that
// builds a Finding sets CanonicalCategory beside the raw Category, which is never
// rewritten. Nothing reads CanonicalCategory yet: every consumer keeps reading the
// raw word until a follow-up migrates it, one consumer at a time.
//
// Only spelling variants normalize — case, surrounding whitespace, and
// singular/plural — never meaning. A word the closed vocabulary
// (reclib.Categories) does not recognise becomes `other`, and the reviewer's own
// word stays visible in Category.
//
// `out-of-scope` is a control token, not a descriptive category: the CI gate,
// the grounding discard, and technical-debt persistence all branch on it. It
// passes through only when the raw text is exactly the token, and no variant
// ("out of scope", "Out-Of-Scope", " out-of-scope") is ever mapped into it, so
// canonicalization cannot turn a blocking finding into a non-blocking one.

var (
	// canonicalMembers is the vocabulary minus the reserved token.
	canonicalMembers = func() map[string]bool {
		m := make(map[string]bool)
		for _, c := range reclib.Categories() {
			if c != reclib.CategoryOutOfScope {
				m[c] = true
			}
		}
		return m
	}()

	// pluralFolds is the part of reclib.CategoryMerges that is a singular/plural
	// pair: a key whose singular or plural is also a key with the same target
	// (today resource/resources -> resource-leak). The rest of that table folds
	// one word's meaning into another (bug -> correctness), which this
	// canonicalizer must not do.
	pluralFolds = func() map[string]string {
		merges := reclib.CategoryMerges()
		folds := make(map[string]string)
		for k, target := range merges {
			if target == reclib.CategoryOutOfScope {
				continue
			}
			if merges[k+"s"] == target || (strings.HasSuffix(k, "s") && merges[strings.TrimSuffix(k, "s")] == target) {
				folds[k] = target
			}
		}
		return folds
	}()
)

// CanonicalCategory returns the canonical form of a reviewer's raw CATEGORY: a
// vocabulary member, `other` for an unrecognised word, or "" when the raw text
// holds no word at all. See the comment above for the rules.
func CanonicalCategory(raw string) string {
	if raw == reclib.CategoryOutOfScope {
		return raw
	}
	w := strings.ToLower(strings.Join(strings.Fields(raw), " "))
	if w == "" {
		return ""
	}
	forms := numberForms(w)
	for _, f := range forms {
		if canonicalMembers[f] {
			return f
		}
	}
	for _, f := range forms {
		if target, ok := pluralFolds[f]; ok {
			return target
		}
	}
	return reclib.CategoryOther
}

// numberForms returns w followed by its plausible singular and plural
// spellings, in the order they are tried.
func numberForms(w string) []string {
	forms := []string{w, w + "s"}
	if s, ok := strings.CutSuffix(w, "ies"); ok {
		forms = append(forms, s+"y")
	}
	if s, ok := strings.CutSuffix(w, "es"); ok {
		forms = append(forms, s)
	}
	if s, ok := strings.CutSuffix(w, "s"); ok {
		forms = append(forms, s)
	}
	return forms
}
