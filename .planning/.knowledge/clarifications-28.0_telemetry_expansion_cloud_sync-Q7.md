---
id: mem-2026-07-16-7ba936
question: "yaml.v3 v3.0.1 discards comments on comment-only-file Unmarshal into yaml.Node"
created: 2026-07-16
last_retrieved: ""
sprints: []
files: [internal/registry/telemetry_setting.go, go.mod]
tags: [clarifications, epic-28.0_telemetry_expansion_cloud_sync, implementation]
retrievals: 0
status: active
type: clarifications
---

# yaml.v3 v3.0.1 discards comments on comment-only-file Unmars

## Decision

Empirically verified: gopkg.in/yaml.v3 v3.0.1 Unmarshal of a comment-only YAML file into a yaml.Node DISCARDS the comments entirely — the resulting node has Kind=0, empty HeadComment, and empty Content. There is nothing on the node to re-attach after synthesizing a fresh mapping. Preserving such comments would require retaining raw comment lines OUTSIDE the Node round-trip (a materially larger change), which is disproportionate for a LOW-severity edge case (a config file containing only comments, no keys). Close such TD items as won't-fix rather than attempting the "re-attach HeadComment/FootComment" approach as originally scoped — that premise does not hold against this yaml.v3 version.

Justification:
- Reproduced directly: yaml.Unmarshal([]byte("# comment\n# comment\n"), &node) yields kind=0, headComment="", content_len=0.
- go.mod confirms gopkg.in/yaml.v3 v3.0.1 is the exact pinned version tested.
- internal/registry/telemetry_setting.go:190-198 (configMapping) unconditionally synthesizes a fresh empty mapping when doc.Kind==0 — there is no HeadComment/FootComment field being dropped in that branch because the parse never populated one.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/telemetry_setting.go
- go.mod
