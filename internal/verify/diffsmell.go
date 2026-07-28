package verify

// RED stub — deliberately wrong answers so diffsmell_test.go compiles and FAILS.
// Replaced by the real ported analyzer in the GREEN stage.

const (
	smellVerdictClean    = "clean"
	smellVerdictSoftOnly = "soft_only"
	smellVerdictHard     = "hard"

	smellTestOnly          = "test_only"
	smellWeakenedAssertion = "weakened_assertion"
	smellSuppression       = "suppression"
	smellEmptyCatch        = "empty_catch"
	smellStubBody          = "stub_body"
)

type smell struct {
	Type     string
	Severity string
	File     string
	Line     int
	Evidence string
}

type smellFiles struct {
	Test []string
	Impl []string
}

type smellSummary struct {
	TestFiles int
	ImplFiles int
	Hard      int
	Soft      int
	ByType    map[string]int
	Verdict   string
}

type smellResult struct {
	Files   smellFiles
	Smells  []smell
	Summary smellSummary
}

func analyzeDiff(string) *smellResult {
	return &smellResult{Summary: smellSummary{ByType: map[string]int{}, Verdict: smellVerdictClean}}
}

func isSmellTestPath(string) bool { return false }

func looksLikeUnifiedDiff(string) bool { return false }

func smellFeedback(*smellResult) string { return "" }

func smellTypes(*smellResult) []string { return nil }
