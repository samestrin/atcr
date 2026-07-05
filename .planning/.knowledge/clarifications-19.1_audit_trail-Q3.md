---
id: mem-2026-07-05-78d81d
question: "atcr append-only ledgers live at repo-level .atcr/, not inside per-run output dirs"
created: 2026-07-05
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go, cmd/atcr/resume.go, internal/history/record.go, internal/history/capture.go]
tags: [clarifications, epic-19.1_audit_trail, architecture, ledger-design]
retrievals: 0
status: active
type: clarifications
---

# atcr append-only ledgers live at repo-level .atcr/, not insi

## Decision

Cross-run accumulating ledgers (e.g. findings-history.jsonl, and by the same precedent audit.log.jsonl) always target `<repo-root>/.atcr/<name>.jsonl` regardless of `--output-dir` — they are repo-level accumulators, not part of the redirected per-run review tree. See cmd/atcr/review.go:335-339 (`histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")`), mirrored for resumed runs in cmd/atcr/resume.go:206-214, and documented in internal/history/record.go:5. `.atcr/` is the established repo-level convention already holding config.yaml, cache, registry.yaml, and latest-review pointers. Prefer this pattern over per-review-dir ledger files, which would force report commands to glob across every review directory.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go
- cmd/atcr/resume.go
- internal/history/record.go
- internal/history/capture.go
