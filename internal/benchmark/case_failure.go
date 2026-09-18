package benchmark

// RED stub — deliberately wrong answers so the failing test compiles.
const (
	CaseFailureWorkDir      = "work_dir"
	CaseFailureMaterialize  = "materialize"
	CaseFailurePrepare      = "prepare"
	CaseFailureExecute      = "execute"
	CaseFailurePoolSummary  = "pool_summary"
	CaseFailureReadFindings = "read_findings"
)

// CaseFailure is the RED stub shape.
type CaseFailure struct {
	CaseID string `json:"case_id"`
	Reason string `json:"reason"`
}

// ValidCaseFailureReason is a RED stub.
func ValidCaseFailureReason(string) bool { return false }
