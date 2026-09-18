package benchmark

// The per-case INFRASTRUCTURE-FAILURE vocabulary: what went wrong when one case
// could not be reviewed at all. It exists so a repo-state run can survive a late
// single-case failure without either forfeiting every case already paid for or
// recording the failed case as a genuine miss.
//
// IT IS A SEPARATE AXIS FROM Outcome*, AND DELIBERATELY SO. An Outcome* value
// describes one REVIEWER SLOT's result on one case, and every one of them folds
// through OutcomeTallyKey into the published per-reviewer outcomes tally. A
// case-level reason placed on that axis would land in a tally that means something
// else — an infrastructure failure is not a property of any reviewer, since it
// prevents the whole panel from meeting the case. The two vocabularies are checked
// against each other by TestCaseFailureReasonsDoNotCollideWithOutcomes, because both
// appear in the same run-result document and a shared spelling would invite exactly
// the conflation this separation exists to prevent.
//
// WHAT IT DOES NOT COVER. A case that materialized and was reviewed is scored, full
// stop — including one every reviewer failed, which is a measurement rather than an
// infrastructure fault. Nor does it cover a DETERMINISTIC defect: a case that fails
// ValidateAgainstHead is an unwinnable suite, and a case scored twice under one
// realized identity is a configuration bug. Both still abort the run, because
// recording them here would publish a score around a suite known to be broken.
//
// Each value names one failure SITE in executeRepoStateBenchmarkRun's per-case loop,
// one to one. That mapping is the point: the reason tells an operator reading a
// partial run-result which stage the case died at, and therefore whether a re-run is
// likely to help.
const (
	// CaseFailureWorkDir marks a case whose per-case work directory could not be
	// created — a local disk or permissions fault, before any suite content is
	// touched.
	CaseFailureWorkDir = "work_dir"

	// CaseFailureMaterialize marks a case whose base tree and change could not be
	// realized into a git repository.
	CaseFailureMaterialize = "materialize"

	// CaseFailurePrepare marks a case whose review payload could not be built. No
	// completer call has been made yet, so nothing was paid for this case.
	CaseFailurePrepare = "prepare"

	// CaseFailureExecute marks a case whose panel run failed for a reason OTHER than
	// the whole roster failing or being empty. Neither of those is recorded here:
	// both abort the run — the first because scoring around it would let a transient
	// infrastructure failure read as a genuine missed defect (the contract
	// docs/benchmark.md states for this tier), the second because an empty roster is
	// a deterministic configuration defect rather than bad luck.
	//
	// Unlike its siblings, this reason does NOT tell you whether the case was paid
	// for. It covers both a panel that died part-way and one that completed every
	// call and then failed to persist its pool, so treat a case recorded here as
	// possibly-paid and look in the retained work dir before re-running it.
	CaseFailureExecute = "execute"

	// CaseFailurePoolSummary marks a case whose pool summary could not be read back.
	// The panel ran and was paid for; its artifacts are retained in the work dir.
	CaseFailurePoolSummary = "pool_summary"

	// CaseFailureReadFindings marks a case whose pool findings could not be read or
	// parsed. As with CaseFailurePoolSummary, the paid artifacts survive on disk.
	CaseFailureReadFindings = "read_findings"
)

// CaseFailure records one case that could not be scored, and why.
//
// It carries the case id and the reason ONLY — never the underlying error text. A
// Go error on this path routinely embeds the run's $TMPDIR path, and a run-result is
// a file operators hand to other people; the full error is logged at the failure
// site instead, where the operator who owns the machine can read it. The reason is
// what a downstream reader can actually act on.
type CaseFailure struct {
	CaseID string `json:"case_id"`
	Reason string `json:"reason"`
}

// ValidCaseFailureReason reports whether s is a value the failure vocabulary can
// legitimately STORE. It is the EXPORT trust boundary for this channel, the same
// role ValidOutcome plays for the outcome vocabulary at checkpoint resume: a
// run-result is hand-suppliable, and without this gate an arbitrary string would
// reach a published artifact describing why a case went unmeasured.
//
// The empty string is REJECTED, which is the one place this vocabulary parts company
// with ValidOutcome. OutcomeUnknown is the empty string because a checkpoint written
// before that field existed must decode into a representable absence. case_failures
// has no such history: the array is new, omitempty, and every element in it is
// written by the producer together with its reason. An element carrying no reason
// therefore cannot have come from the producer, and admitting it would publish "this
// case went unmeasured, cause unstated" — the one thing the channel exists to
// prevent.
func ValidCaseFailureReason(s string) bool {
	switch s {
	case CaseFailureWorkDir, CaseFailureMaterialize, CaseFailurePrepare,
		CaseFailureExecute, CaseFailurePoolSummary, CaseFailureReadFindings:
		return true
	}
	return false
}
