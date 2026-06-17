---
id: mem-2026-06-17-d7bc8a
question: "Is ignoring the error from cobra GetString/GetBool acceptable in cmd/atcr?"
created: 2026-06-17
last_retrieved: ""
sprints: []
files: []
tags: []
retrievals: 0
status: active
type: project
---

# Is ignoring the error from cobra GetString/GetBool acceptabl

## Decision

Yes — the `value, _ := cmd.Flags().GetString(...)` / GetBool ignore is the deliberate, pervasive repo convention (30+ sites across cmd/atcr: review.go, leaderboard.go, doctor.go, reconcile.go, range.go, report.go, verify.go, trust.go, main.go). cobra's GetString/GetBool only error when the flag is UNREGISTERED — a programmer error that is structurally impossible for a flag the command itself registers (e.g. log-format registered at main.go:117, read at main.go:156). Do NOT return usageError (misclassifies a programmer error as a user error, contradicting the transient/permanent/user/system taxonomy). Do NOT add a repo-wide stringFlag helper (multi-file rewrite, violates incremental-migration out-of-scope). boolFlag (review.go:266) panics on undefined flags but is a near-orphan helper used only in review.go, so it is not the norm — panicking at one bare site would create a third inconsistent style. Leave bare-ignore sites as-is by design.</answer>
<tags>clarifications, sprint-4.0_structured_logging, convention, error-handling, cobra</tags>
<sprints>4.0_structured_logging</sprints>
<files>cmd/atcr/main.go, cmd/atcr/review.go</files>
<source>clarifications</source>


## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
