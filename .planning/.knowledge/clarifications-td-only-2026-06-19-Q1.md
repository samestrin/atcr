---
id: mem-2026-06-19-4db340
question: "Should the output-path denylist in validation.FilePath be tightened (allowlist) or is the permissive denylist the intended final policy?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/validation/validation.go, .planning/epics/completed/4.3_input_validation.md]
tags: [td-clarification, td-only, security, architecture, validation, FilePath]
retrievals: 0
status: active
type: clarifications skill — td-only 2026-06-19
---

# Should the output-path denylist in validation.FilePath be ti

## Decision

The permissive denylist (/etc, /proc, /sys, /private/etc, /private/var) deliberately omitting /boot, /dev, /root, /var is the intended final policy for epic 4.3. The epic's Open Questions explicitly chose "Option B: permissive — only reject clearly invalid inputs." The TD row documents this as a "deliberate Option-B permissive choice." Tightening to an allowlist anchored at the repo/.atcr root is conditional only ("if stronger isolation is later required"). No immediate fix is warranted without a new security requirement.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/validation/validation.go
- .planning/epics/completed/4.3_input_validation.md
