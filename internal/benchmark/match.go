package benchmark

// ReportedFinding is one finding a reviewer raised, located.
type ReportedFinding struct {
	File     string
	Line     int
	Category string
}

// FindingMatch is one expected finding's outcome against a reviewer's report.
type FindingMatch struct {
	Expected      ExpectedFinding
	Matched       bool
	ReportedIndex int
}

// MatchFindings pairs reported findings with expected ones positionally.
func MatchFindings(expected []ExpectedFinding, reported []ReportedFinding, lm DiffLineMap) []FindingMatch {
	return nil
}
