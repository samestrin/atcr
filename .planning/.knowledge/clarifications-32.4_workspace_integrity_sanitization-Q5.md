---
id: mem-2026-07-22-76f614
question: "An advisory/warning check (e.g. a PR-body risk flag) reads diff-declared metadata (like a mode change) that a downstream write path silently normalizes away before it ever lands. Should the advisory check reflect the diff's intent, or what actually lands?"
created: 2026-07-22
last_retrieved: ""
sprints: [32.4_workspace_integrity_sanitization]
files: [internal/autofix/apply.go, internal/security/pathguard.go, internal/atomicfs/atomic.go, internal/ghaction/client.go]
tags: [clarifications, sprint-32.4_workspace_integrity_sanitization, architecture, advisory-design, go]
retrievals: 0
status: active
type: clarifications
---

# An advisory/warning check (e.g. a PR-body risk flag) reads d

## Decision

An advisory check must reflect what actually lands, not what the diff merely declares — otherwise it both cries wolf (flags changes the pipeline silently discards) and misses the real, always-happening side effect (the pipeline's own normalization quietly stripping a property on every affected file). When the write/commit path unconditionally normalizes a property (e.g. a file-write helper hardcoding a fixed permission mode regardless of the diff's declared mode), don't try to make the advisory "smarter" about the diff — remove the check for that property entirely, since no diff-derived signal can be accurate when the outcome is deterministic and diff-independent. Reserve a bigger fix (making the write/commit path honor the diff's declared property) only if the underlying behavior itself is the actual target of the epic — not proportionate for a purely advisory, non-blocking warning riding on a shared, multi-caller utility. Example: `internal/security.FlagsForReview`'s executable-bit condition flagged chmod-+x patches that `internal/atomicfs.WriteFileAtomic` (hardcoded `Chmod(0o644)`) and the GitHub commit blob (hardcoded `"mode":"100644"`) always silently discard — the fix was to drop the exec-bit condition from the advisory (not to "fix" the normalization logic or rewrite the shared write path), keeping unrelated advisory conditions (like build-script path matches) untouched.
File_path: internal/autofix/apply.go:171-177, internal/security/pathguard.go:227-242, internal/atomicfs/atomic.go:36, internal/ghaction/client.go:269

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/autofix/apply.go
- internal/security/pathguard.go
- internal/atomicfs/atomic.go
- internal/ghaction/client.go
