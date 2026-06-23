---
id: mem-2026-06-22-6a19bf
question: "How should the magic string literals \"failure\"/\"success\"/\"neutral\" used as GitHub conclusion values be replaced in the ghaction package?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/ghaction/render.go, cmd/atcr/github.go]
tags: [td-clarification, td-only, ghaction, typed-constants, conclusion-values, github-action, maintainability]
retrievals: 0
status: active
type: clarifications skill 2026-06-22
---

# How should the magic string literals "failure"/"success"/"ne

## Decision

Define package-level typed constants in internal/ghaction/render.go: `const (conclusionFailure = "failure"; conclusionSuccess = "success"; conclusionNeutral = "neutral")`. Replace all raw literals at render.go:62 (return "neutral"), 71 (return "failure"), 73 (return "success"), 127/129/137/139 (switch arms), and github.go:155 (if conclusion == "failure"). Verify with grep that no raw comparison or emission sites remain. Test files may keep raw literals if they test wire-format values, but production comparison sites must use constants. The risk without constants is a silent typo at any site that breaks the merge gate without a compile error.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/ghaction/render.go
- cmd/atcr/github.go
