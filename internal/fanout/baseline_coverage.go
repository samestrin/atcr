package fanout

// uncoveredBaselineFiles is a RED-stage stub: it deliberately returns the wrong
// answer ("nothing is uncovered") so the Epic 35.2 tests fail for the right reason
// while `go vet` still compiles the package.
func uncoveredBaselineFiles(slots []Slot, results []Result, reviewed map[string]string) map[string]struct{} {
	return nil
}
