---
id: mem-2026-06-17-3c4ed9
question: "Which user inputs echoed into error messages need length-clamping (clampForError)?"
created: 2026-06-17
last_retrieved: ""
sprints: []
files: []
tags: []
retrievals: 0
status: active
type: project
---

# Which user inputs echoed into error messages need length-cla

## Decision

Any user-supplied value echoed verbatim into an error message must be length-clamped via clampForError (32-rune bound) to prevent stderr flooding. LOG_LEVEL was already capped at internal/log/log.go:43 (commit b9aade8). The remaining unbounded site is log.New echoing the --log-format flag value with %q at internal/log/log.go:81 ("log: invalid format %q (want text or json)") with NO clampForError — fix by wrapping with clampForError(format), mirroring line 43. NOTE: the TD item citing cmd/atcr/main.go:95 is a misattribution — line 94 is `Args: usageArgs(cobra.NoArgs)` (validates positional args, no length check applies) and line 95 is a comment; the LOG_LEVEL fix lives in internal/log, not main.go. The actionable, non-duplicate gap is the --log-format echo in log.New.</answer>
<tags>clarifications, sprint-4.0_structured_logging, input-validation, error-handling, convention</tags>
<sprints>4.0_structured_logging</sprints>
<files>internal/log/log.go, cmd/atcr/main.go</files>
<source>clarifications</source>


## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
