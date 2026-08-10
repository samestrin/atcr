package reconcile

// The closed reviewer CATEGORY vocabulary (epic 35.16.4).
//
// Before this epic no enumeration reached any reviewer: every persona prompt
// asked only for "one lowercase word", and the sole list in the tree
// (personas/_base.md:44, six words) sits at persona-resolution levels 4-5, a
// fallback no shipped persona reaches. The 35.16.2 dry-run measured the
// consequence — 72.3% of findings (154 of 213) used a word the scorer did not
// recognise, across 34 distinct categories, while `maintainability` appeared in
// 17 of 17 cases' ground truth and was emitted once. The reviewers were finding
// real problems and labelling them with words nothing had offered them.
//
// The fix is therefore to OFFER MORE, not to offer less. This set is closed but
// deliberately not small: it is the union of three sources — the words the
// dry-run actually emitted, each persona's declared Focus list, and the 14
// category words personas/community_test.go:117-132 binds in code and requires
// to appear in each community persona's own prompt. Narrowing is what caused
// the drift; a member is removed only when a developer would act on it
// identically to another member (see categoryMerges).
//
// This is a GENERATION-TIME vocabulary. It is rendered into every prompt through
// internal/payload's ScopeRule (the one channel present in all 29 prompts), so
// no prompt file is edited and none can drift. It deliberately does NOT
// normalize anything at ingestion: no consumer of Finding.Category reads this
// list yet, and Category never enters FindingID, so adding it changes no
// existing finding identity. Canonicalization at the parse boundary is epic
// 35.16.6.
const (
	// Defect classes — what is wrong with the code's behaviour.
	CategoryCorrectness   = "correctness"    // the code produces a wrong result: off-by-one, inverted condition, unreachable branch
	CategoryLogic         = "logic"          // accepted equivalent spelling of correctness; a member because personas/community/sonny.md:49's worked example emits it
	CategorySecurity      = "security"       // injection, auth bypass, traversal — every vulnerability class except credential exposure
	CategorySecret        = "secret"         // an exposed credential specifically: hardcoded key, secret in a log, weak secret handling (personas/community/gerald.md's whole lens)
	CategoryPerformance   = "performance"    // hot-path cost: N+1 calls, needless allocation, accidental O(n^2)
	CategoryConcurrency   = "concurrency"    // synchronization and lifecycle misuse: lock discipline, channel/WaitGroup hazards, goroutines with no exit path
	CategoryRace          = "race"           // a specific unsynchronized access to specific shared state, including check-then-act (TOCTOU)
	CategoryErrorHandling = "error-handling" // swallowed or ignored errors, missing timeouts, unbounded retries, partial-failure states
	CategoryState         = "state"          // stale caches, mutation of shared data, ordering assumptions
	CategoryInvariant     = "invariant"      // a property the code assumes but never establishes or enforces
	CategoryType          = "type"           // type-safety gaps: over-broad any/dynamic typing, unchecked assertions, lossy conversion

	// Contract and interface — what the code promises its callers.
	CategoryAPIContract     = "api-contract"     // the published interface itself is wrong: signature, error type, or ownership semantics that callers must code around
	CategoryContract        = "contract"         // this change violates an existing contract — the function does not honour its own name, docs, or signature
	CategoryValidation      = "validation"       // a validation rule that is missing, too weak, or applied on the wrong side of the boundary
	CategoryInputValidation = "input-validation" // untrusted input reaching logic unchecked (the trust-boundary subset of validation)

	// Resource and dependency handling.
	CategoryResourceLeak  = "resource-leak" // an acquired resource with no release path: unclosed handle, connection churn, unbounded cache
	CategoryLeak          = "leak"          // a leak of something other than a held resource: memory retained by a live reference, a leaked secret or internal detail
	CategoryDependency    = "dependency"    // a dependency that is unnecessary, unpinned, misused, or points the wrong way
	CategoryConfiguration = "configuration" // dangerous defaults, unvalidated config, undocumented environment dependence (personas/mira.md:11)

	// Structure and design — cost the change imposes on future work.
	CategoryCoupling        = "coupling"        // hidden dependencies, layer violations, config reach-through, two sources of truth
	CategoryComplexity      = "complexity"      // this code is harder to follow than the problem requires
	CategoryBloat           = "bloat"           // code that should not exist at all: dead branches, unused abstraction, speculative generality
	CategoryDuplication     = "duplication"     // parallel implementations that will drift apart
	CategoryExtensibility   = "extensibility"   // a hardcoded assumption the known roadmap already contradicts
	CategoryMaintainability = "maintainability" // structural readability cost not captured above: functions doing three jobs, misleading structure, comments that lie
	CategoryNaming          = "naming"          // an identifier that promises something the code does not do, or one concept spelled three ways
	CategoryStyle           = "style"           // formatting and idiom only — no behavioural or structural claim

	// Cross-cutting concerns.
	CategoryObservability = "observability" // if this breaks in production nobody will know: no log, metric, or error context
	CategoryTesting       = "testing"       // absent, vacuous, or non-isolated tests
	CategoryDocs          = "docs"          // documentation or comments made wrong by this change

	// Control values. These are not defect classes; they route a finding.
	// CategoryOutOfScope is declared in merge.go, where its annotate-don't-promote
	// semantics live.
	CategoryOther = "other" // a real finding that genuinely fits no member above — the escape hatch that makes the set closed rather than lossy
)

// categories is the vocabulary in the order it is offered to a reviewer:
// defect classes first (what is broken), then contract, resource, and
// structural concerns (what will break later), then cross-cutting concerns,
// with the two routing values last. Order is part of the rendered prompt, so it
// is deliberate rather than alphabetical — a reviewer scanning the list should
// meet the categories in roughly the order they scan the code.
//
// Callers receive a copy via Categories(); this slice is never handed out.
var categories = []string{
	CategoryCorrectness,
	CategoryLogic,
	CategorySecurity,
	CategorySecret,
	CategoryPerformance,
	CategoryConcurrency,
	CategoryRace,
	CategoryErrorHandling,
	CategoryState,
	CategoryInvariant,
	CategoryType,
	CategoryAPIContract,
	CategoryContract,
	CategoryValidation,
	CategoryInputValidation,
	CategoryResourceLeak,
	CategoryLeak,
	CategoryDependency,
	CategoryConfiguration,
	CategoryCoupling,
	CategoryComplexity,
	CategoryBloat,
	CategoryDuplication,
	CategoryExtensibility,
	CategoryMaintainability,
	CategoryNaming,
	CategoryStyle,
	CategoryObservability,
	CategoryTesting,
	CategoryDocs,
	CategoryOutOfScope,
	CategoryOther,
}

// categoryMerges records every word from the three derivation sources that a
// consumer should treat as another word, and the member it folds into. Almost
// every key is a non-member; the one exception is documented in its own block
// below. A merge is only justified when a developer would act on both words
// identically — "coupling is not maintainability; race is not concurrency" is
// the standing rule, and when in doubt the distinction is kept. Each entry below
// states why the two are the same thing, not merely similar.
//
// This is derivation provenance, not a runtime normalizer: nothing in this epic
// rewrites an emitted category. Epic 35.16.6 owns parse-boundary
// canonicalization and is the intended consumer.
var categoryMerges = map[string]string{
	// Trivially identical variants — the only collapse the epic pre-approved.
	"resource":  CategoryResourceLeak, // singular/plural of the same emitted word; both meant an unreleased resource
	"resources": CategoryResourceLeak,

	// A member that is ALSO a merge — the single case, and it is deliberate.
	//
	// Membership and canonicalization answer different questions. Membership is
	// what the PROMPT offers: `logic` stays a member because
	// personas/community/sonny.md:49 ships a worked example that emits it, and a
	// prompt whose own example contradicts its own vocabulary is the defect this
	// epic exists to remove. Canonicalization is what INGESTION should do with the
	// word once emitted: the rendered rule tells every reviewer that `logic` is
	// accepted as the equivalent of `correctness`, so leaving the fold unrecorded
	// would split identical findings across every category-keyed consumer
	// (ModalCategory clustering, SARIF rule ids, the 35.16.5 scorer) and re-create
	// the unscoreability this epic closes. Offering both words and recording that
	// they are one is the only combination that is true on both sides.
	//
	// `secret` is NOT folded, despite gerald.md:50 emitting it for the same reason
	// sonny emits `logic`. The difference is the gloss: it draws a real triage line
	// ("`secret` for an exposed credential, `security` for every other
	// vulnerability"), so the two are distinct rather than equivalent, and
	// reconcile/consensus.go treats both as security-related on their own terms.
	CategoryLogic: CategoryCorrectness,

	// Same concept, different word. No reviewer would triage these differently.
	"bug":       CategoryCorrectness, // an unqualified restatement of "the code is wrong"
	"input":     CategoryInputValidation,
	"failure":   CategoryErrorHandling, // mira's "failure handling" — missing timeouts, unbounded retries, partial-failure states
	"stability": CategoryErrorHandling,

	// Readability variants. These three describe the same complaint — the code
	// reads worse than it needs to — and split only by which word the model
	// reached for. `naming` stays a member because an identifier that lies is a
	// specific, separately actionable defect; these are not.
	"clarity":     CategoryMaintainability,
	"cleanliness": CategoryMaintainability,
	"consistency": CategoryNaming, // every observed use was "same concept spelled three ways", which is otto's naming dimension
	"structure":   CategoryMaintainability,
}

// Categories returns the closed reviewer CATEGORY vocabulary in offer order.
//
// The returned slice is a copy: this module is published and embedded by
// external tools, and a shared backing array would let one consumer corrupt
// every prompt rendered afterwards in the same process.
func Categories() []string {
	out := make([]string, len(categories))
	copy(out, categories)
	return out
}
