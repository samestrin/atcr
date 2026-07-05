---
id: mem-2026-07-04-09851f
question: "How should atcr's --package filter match a finding's file path — directory prefix or exact match?"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [reconcile/finding.go, internal/reconcile/discover.go, internal/scorecard/aggregate.go]
tags: [clarifications, epic-19.0_finding_history, implementation]
retrievals: 0
status: active
type: clarifications
---

# How should atcr's --package filter match a finding's file pa

## Decision

Store package = filepath.Dir(file) and match --package with a separator-aware prefix check (path == package || strings.HasPrefix(package, path+string(os.PathSeparator))), not a naive string prefix. This aligns with the codebase's existing convention for directory-tree containment checks and avoids "internal/registry" spuriously matching a sibling like "internal/registry2".

Justification:
- Findings carry a plain repo-relative filesystem path (reconcile/finding.go:20, File string), not a Go import-path concept — "package" is necessarily derived via filepath.Dir(file).
- internal/reconcile/discover.go:154-156 already implements this exact separator-aware containment pattern (strings.HasPrefix(other, d+string(os.PathSeparator))), with a comment explaining the trailing separator prevents sibling dirs with shared name prefixes (pool vs pool2) from falsely matching.
- No other atcr command implements a --package flag to cross-check against; no exact-match convention exists elsewhere for "package" scoping in this repo.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/finding.go
- internal/reconcile/discover.go
- internal/scorecard/aggregate.go
