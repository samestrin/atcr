package localdebt

// BackfillResult is a stub — see backfill_test.go.
type BackfillResult struct {
	Scanned    int
	Rewritten  int
	Unchanged  int
	Unresolved int
	Ambiguous  int
}

// BackfillJustifications is a stub — see backfill_test.go.
func BackfillJustifications(_ string, _ string, _ bool) (BackfillResult, error) {
	return BackfillResult{}, nil
}
