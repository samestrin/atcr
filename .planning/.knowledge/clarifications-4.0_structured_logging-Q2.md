---
id: mem-2026-06-17-589ed3
question: "Which sensitive roots should the log Redactor's relativizePaths rewrite — review root, home dir, $TMPDIR, ~/.config/atcr scorecard store?"
created: 2026-06-17
last_retrieved: ""
sprints: [4.0_structured_logging]
files: [internal/log/redact.go, cmd/atcr/review.go, internal/scorecard/store.go, internal/tools/snapshot.go]
tags: [clarifications, sprint-4.0_structured_logging, scope, logging, redaction, paths, security]
retrievals: 0
status: active
type: clarifications
---

# Which sensitive roots should the log Redactor's relativizePa

## Decision

Only the review/repo root. AC6 scopes path redaction to absolute repo paths rendered relative to the review root (original-requirements.md:115-116,139; sprint-plan.md:44; sprint-design.md:257) — not general path/PII hygiene. relativizePaths is a single-root relativizer that strips only "<cleanRoot>/" (internal/log/redact.go:139-153), and NewRedactor takes one reviewRoot (cmd/atcr/review.go:168,245). None of the other candidate roots needs a rule: the ~/.config/atcr scorecard store never flows through the Redactor (it writes to its own io.Writer/os.Stderr and self-defends usernames via basePathErr, internal/scorecard/store.go:16-50), and home dir / $TMPDIR are not logged as absolute roots through the sink (the EvalSymlinks(os.TempDir()) at internal/tools/snapshot.go:34-44 is a path-check, not a log emission). Minimal safe fix for the macOS /private/var leak: resolve the review root with filepath.EvalSymlinks after filepath.Abs in resolveRedactRoot (cmd/atcr/review.go:245); add NO new roots. Expanding to home-dir-~ masking or $TMPDIR rewriting is a separate policy decision the user owns — file as its own TD if wanted.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/log/redact.go
- cmd/atcr/review.go
- internal/scorecard/store.go
- internal/tools/snapshot.go
