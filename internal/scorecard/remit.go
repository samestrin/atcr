package scorecard

import (
	"strings"

	reclib "github.com/samestrin/atcr/reconcile"
)

// personaRemit maps a reviewer persona to the CATEGORY values its declared
// remit covers. It is the denominator half of opportunity-set scoring: a lens is
// judged only on cases where some reviewer raised a category in this list, so a
// specialist that is correctly silent on an out-of-remit diff is neither
// credited nor penalised.
//
// WHY A TABLE AND NOT A RUNTIME RESOLUTION (sprint-plan.md C9/C10). The sprint's
// D7 called for resolving each persona's remit through "atcr's existing
// persona-resolution path". That is not buildable as written, for three verified
// reasons:
//
//   - registry.ResolvePersona (internal/registry/persona.go) is the ONLY
//     resolution chain in the tree, and the only wrapper package
//     (internal/personas) imports internal/registry itself. D7's own ban on
//     importing internal/registry — which restates the epic's "no task reads or
//     writes registry code" — therefore contradicts its resolution requirement.
//     The ban wins: it is the older and higher rule.
//   - The resolver returns persona prompt TEXT, not categories. Deriving
//     "api-contract" from a ## Focus list would be heuristic prose parsing.
//   - It would be UNSOUND for exactly the lenses it was meant to cover. On a
//     fresh clone or in CI, vera/pace/brad/archer/ronin have no
//     personas/<name>.md, so resolution falls through to _base.md and returns
//     GENERIC TEXT WITH NO ERROR. A resolution-based mapping would read that
//     fallback as vera's declared focus and mint a fabricated remit whose values
//     are all real vocabulary members — invisible to the membership guard in
//     remit_test.go, which is the only automated check on this table.
//
// So the table covers the nine personas that ship in this repo and ONLY those
// nine. Each entry is grounded in that persona's own personas/<name>.md ##
// Focus list, cited below, so a reviewer can check the mapping against its
// source rather than by eye. The five registry-only lenses are unmapped by
// design and take the unmapped path (see RemitCategories).
//
// Every value is a reclib.Category* CONSTANT, never a hand-typed string: a
// near-miss spelling then fails to compile instead of silently never matching.
// remit_test.go additionally asserts each value is a live reclib.Categories()
// member, which catches a constant that is removed from the offered vocabulary
// while still existing in Go.
var personaRemit = map[string][]string{
	// personas/bruce.md ## Focus — the generalist, and the widest entry here by
	// design. Item 1 logic errors, 2 error handling, 3 contract violations,
	// 4 state bugs, 5 resource handling, 6 predicate exhaustiveness (whose own
	// text says "with CATEGORY invariant" — the member most easily lost by
	// reading only the first five Focus lines).
	"bruce": {
		reclib.CategoryCorrectness,
		reclib.CategoryLogic,
		reclib.CategoryErrorHandling,
		reclib.CategoryContract,
		reclib.CategoryState,
		reclib.CategoryResourceLeak,
		reclib.CategoryInvariant,
	},
	// personas/dax.md ## Focus — test coverage and error paths. Items 2/3/5 are
	// test-shaped, item 1 is untested ERROR paths, item 6 is invariant.
	"dax": {
		reclib.CategoryTesting,
		reclib.CategoryErrorHandling,
		reclib.CategoryInvariant,
	},
	// personas/greta.md ## Focus — algorithmic correctness. Item 1 boundaries and
	// 2 loop correctness are correctness/logic, 3 numeric truncation is type,
	// 4 slice aliasing and mutation-during-iteration is state, 5 accidental
	// O(n^2) is complexity/performance, 6 is invariant.
	"greta": {
		reclib.CategoryCorrectness,
		reclib.CategoryLogic,
		reclib.CategoryType,
		reclib.CategoryState,
		reclib.CategoryComplexity,
		reclib.CategoryPerformance,
		reclib.CategoryInvariant,
	},
	// personas/ingrid.md ## Focus — language idioms. Item 1 error handling,
	// 2 resource and task leaks, 3 abstraction misuse and over-broad dynamic
	// typing is type, 4 concurrency misuse, 5 standard-library reinvention is
	// duplication plus idiom misuse (style), 6 is invariant.
	"ingrid": {
		reclib.CategoryErrorHandling,
		reclib.CategoryResourceLeak,
		reclib.CategoryType,
		reclib.CategoryConcurrency,
		reclib.CategoryDuplication,
		reclib.CategoryStyle,
		reclib.CategoryInvariant,
	},
	// personas/kai.md ## Focus — architecture and design fit. Items 1/2 boundary
	// violations and coupling, 3 contract design ("APIs that lie") covers both
	// the published-interface and the honours-its-own-name senses, 4 duplication
	// of responsibility, 5 extensibility traps, 6 invariant.
	"kai": {
		reclib.CategoryCoupling,
		reclib.CategoryAPIContract,
		reclib.CategoryContract,
		reclib.CategoryDuplication,
		reclib.CategoryExtensibility,
		reclib.CategoryInvariant,
	},
	// personas/mira.md ## Focus — production feasibility. Item 1 failure
	// handling, 2 resource exhaustion, 3 observability, 4 race-prone
	// startup/shutdown is concurrency, 5 configuration, 6 invariant.
	"mira": {
		reclib.CategoryErrorHandling,
		reclib.CategoryResourceLeak,
		reclib.CategoryObservability,
		reclib.CategoryConcurrency,
		reclib.CategoryConfiguration,
		reclib.CategoryInvariant,
	},
	// personas/otto.md ## Focus — style, naming, readability. Item 1 misleading
	// names, 2 idiom violations, 3 structure is maintainability, 4 comments is
	// docs, 5 consistency is naming again, 6 invariant.
	"otto": {
		reclib.CategoryNaming,
		reclib.CategoryStyle,
		reclib.CategoryMaintainability,
		reclib.CategoryDocs,
		reclib.CategoryInvariant,
	},
	// personas/penny.md ## Focus — performance. Items 1/3/4 are performance,
	// 2 memory leaks is leak, 4 hidden O(n^2) is complexity, 5 missing
	// close/release is resource-leak, 6 invariant.
	"penny": {
		reclib.CategoryPerformance,
		reclib.CategoryLeak,
		reclib.CategoryComplexity,
		reclib.CategoryResourceLeak,
		reclib.CategoryInvariant,
	},
	// personas/sasha.md ## Focus — dedicated security. Item 1 injection is
	// security plus input-validation, 2 broken auth is security, 3 secrets
	// leakage is secret, 4 insecure defaults is security plus validation,
	// 5 sensitive data exposure is security, 6 invariant.
	"sasha": {
		reclib.CategorySecurity,
		reclib.CategoryInputValidation,
		reclib.CategorySecret,
		reclib.CategoryValidation,
		reclib.CategoryInvariant,
	},
}

// RemitCategories returns the CATEGORY values persona's declared remit covers.
//
// ok == false means UNMAPPED — no remit is known for this name — and the
// returned slice is nil, never a non-nil empty one. The caller must be able to
// tell "unmapped" from "mapped to zero categories": the first is a cold start
// that must not cost the lens anything, the second would be a lens that can
// never be in scope. Nothing in this package can produce the second, and
// returning nil keeps it that way.
//
// The five registry-only lenses (vera, pace, brad, archer, ronin) are unmapped
// by design — see personaRemit's comment. They are NOT opportunity-scoped, which
// opportunitySetRuns handles by passing their records through untouched rather
// than dropping them.
//
// Lookup is case-insensitive and trims surrounding whitespace, matching
// trust.go's strings.ToLower(row.Reviewer) key convention and EmitForReconcile's
// trim-once-at-the-boundary rule. An empty, over-long, or control-character name
// simply fails the lookup.
//
// The returned slice is a COPY. The table is package-level and read on the
// scoring path for every record in the store, so handing out the backing array
// would let one caller corrupt every later lookup in the same process — the same
// defence reclib.Categories() documents for its own return.
func RemitCategories(persona string) ([]string, bool) {
	cats, ok := personaRemit[strings.ToLower(strings.TrimSpace(persona))]
	if !ok {
		return nil, false
	}
	out := make([]string, len(cats))
	copy(out, cats)
	return out, true
}

// vocabulary is the closed CATEGORY set, built once from the published module.
// A map rather than a repeated linear scan of reclib.Categories(): the set is
// consulted per finding per reviewer per run over the whole store.
var vocabulary = func() map[string]struct{} {
	all := reclib.Categories()
	m := make(map[string]struct{}, len(all))
	for _, c := range all {
		m[c] = struct{}{}
	}
	return m
}()

// inVocabulary reports whether c is a literal member of reclib.Categories().
//
// It is an exact-match test, NOT a normalising one. "Correctness" does not match
// "correctness" here on purpose: reclib.Categories()' own doc records that
// canonicalisation at the parse boundary is a separate epic and that nothing in
// the current epic rewrites an emitted category. Accepting a mis-cased value
// here would be that rewrite, performed quietly at the durable-write boundary
// where it is hardest to notice.
func inVocabulary(c string) bool {
	_, ok := vocabulary[c]
	return ok
}
