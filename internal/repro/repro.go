// Package repro turns a sandboxed execution into adversarial-verification
// evidence (Epic 11.0). A repro/skeptic agent proposes a command or script that
// should reproduce a finding; this package runs it under the determinism rule
// and, when the failure reproduces deterministically, stamps the finding as
// confirmed (VERIFIED) and attaches an executable-evidence block.
//
// A reproduced finding is a categorically stronger deliverable: it cannot be a
// hallucination, and it hands the resolver a failing command to start from.
package repro

import (
	"context"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/sandbox"
	reclib "github.com/samestrin/atcr/reconcile"
)

// ReproSkeptic is the skeptic name stamped on a finding confirmed by execution.
const ReproSkeptic = "repro"

// Verdict applies the two-run determinism rule to a pair of runs of the SAME
// repro:
//
//   - either run timed out            => unverifiable (no clean signal)
//   - exit codes disagree             => unverifiable (flaky; evidence poisoned)
//   - both non-zero with equal code   => confirmed   (the failure reproduces)
//   - both zero                       => unverifiable (deterministic pass: the
//     failure did NOT reproduce — but a weak repro must not bury the finding,
//     so prior confidence is preserved, not refuted)
//
// Only a deterministic, reproduced FAILURE earns confirmed. This is the gate
// against flaky tests poisoning execution evidence (Risks: flaky => unverifiable).
func Verdict(r1, r2 sandbox.RunResult) string {
	if r1.TimedOut || r2.TimedOut {
		return reclib.VerdictUnverifiable
	}
	if r1.ExitCode != r2.ExitCode {
		return reclib.VerdictUnverifiable
	}
	if r1.ExitCode != 0 {
		if isInfraExit(r1.ExitCode) {
			return reclib.VerdictUnverifiable
		}
		return reclib.VerdictConfirmed
	}
	return reclib.VerdictUnverifiable
}

// isInfraExit reports whether code is a Docker-reserved infrastructure exit
// code (125/126/127) or a signal death (128+n). These reflect environment
// problems, not a reproduced defect, so they must not earn confirmed.
func isInfraExit(code int) bool {
	return code == 125 || code == 126 || code == 127 || code >= 129
}

// Reproduce runs spec twice on backend and returns the determinism verdict plus
// the executable-evidence block built from the (second, confirming) run. A
// backend fault on either run is returned as an error; a non-zero program exit
// is not an error (it is the signal).
func Reproduce(ctx context.Context, backend sandbox.Backend, spec sandbox.RunSpec) (verdict string, ev *reconcile.EvidenceExec, err error) {
	r1, err := backend.Run(ctx, spec)
	if err != nil {
		return "", nil, err
	}
	r2, err := backend.Run(ctx, spec)
	if err != nil {
		return "", nil, err
	}
	verdict = Verdict(r1, r2)
	ev = &reconcile.EvidenceExec{
		Command:       r2.Command,
		ExitCode:      r2.ExitCode,
		OutputExcerpt: r2.Output,
	}
	return verdict, ev, nil
}

// Stamp attaches the execution evidence to f and records the repro verdict. The
// evidence block is always attached (the run happened); the verdict is applied
// only when it does not DOWNGRADE an existing one — a prior skeptic's confirmed
// verdict is authoritative and is never weakened by a repro that merely could
// not reproduce. A reproduced (confirmed) verdict always wins, since confirmed
// is the top of the merge precedence (confirmed > unverifiable > refuted).
func Stamp(f *reconcile.JSONFinding, verdict string, ev *reconcile.EvidenceExec) {
	f.EvidenceExec = ev
	if f.Verification == nil {
		f.Verification = &reclib.Verification{Verdict: verdict, Skeptic: ReproSkeptic}
		return
	}
	if verdictRank(verdict) > verdictRank(f.Verification.Verdict) {
		f.Verification.Verdict = verdict
		f.Verification.Skeptic = ReproSkeptic
	}
}

// verdictRank mirrors internal/reconcile/merge.go precedence so Stamp never
// downgrades a stronger prior verdict (confirmed > unverifiable > refuted).
func verdictRank(verdict string) int {
	switch verdict {
	case reclib.VerdictConfirmed:
		return 3
	case reclib.VerdictUnverifiable:
		return 2
	case reclib.VerdictRefuted:
		return 1
	default:
		return 0
	}
}
