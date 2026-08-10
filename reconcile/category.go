package reconcile

// STUB — deliberately wrong (epic 35.16.4 T1, RED stage). The constants exist
// so the test compiles; the vocabulary itself is still the old six-word
// personas/_base.md:44 list with no recorded merges, which is the defect.
const (
	CategoryCorrectness     = "correctness"
	CategorySecurity        = "security"
	CategoryPerformance     = "performance"
	CategoryConcurrency     = "concurrency"
	CategoryRace            = "race"
	CategoryErrorHandling   = "error-handling"
	CategoryAPIContract     = "api-contract"
	CategoryContract        = "contract"
	CategoryState           = "state"
	CategoryInputValidation = "input-validation"
	CategoryValidation      = "validation"
	CategoryResourceLeak    = "resource-leak"
	CategoryLeak            = "leak"
	CategoryCoupling        = "coupling"
	CategoryDuplication     = "duplication"
	CategoryComplexity      = "complexity"
	CategoryBloat           = "bloat"
	CategoryInvariant       = "invariant"
	CategoryType            = "type"
	CategoryDependency      = "dependency"
	CategoryObservability   = "observability"
	CategoryConfiguration   = "configuration"
	CategoryExtensibility   = "extensibility"
	CategoryNaming          = "naming"
	CategoryStyle           = "style"
	CategoryMaintainability = "maintainability"
	CategoryTesting         = "testing"
	CategoryDocs            = "docs"
	CategoryOther           = "other"
)

var categoryMerges = map[string]string{}

var categories = []string{
	CategorySecurity, CategoryCorrectness, CategoryPerformance,
	CategoryTesting, CategoryStyle, CategoryDocs,
}

// Categories returns the closed reviewer CATEGORY vocabulary.
func Categories() []string { return categories }
