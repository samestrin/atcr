---
id: mem-2026-06-28-2d6b37
question: "What stable per-finding discriminator should be used in the DBSCAN noise ID when the Finding struct has no ID field and Reviewer is attacker-controlled?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [reconcile/dedupe.go, reconcile/ambiguous.go, reconcile/finding.go]
tags: [clarifications, epic-13.2_resolution_bipartite_dbscan, implementation, dbscan, noise-id, finding-struct, ambiguous-id]
retrievals: 0
status: active
type: clarifications epic 13.2_resolution_bipartite_dbscan 2026-06-28
---

# What stable per-finding discriminator should be used in the 

## Decision

The `Problem` text is the correct stable discriminator. Pass `cluster[i].Problem` twice to `AmbiguousID` (as both problemA and problemB): `AmbiguousID(cluster[i].File, cluster[i].Line, cluster[i].Problem, cluster[i].Problem)`. This is already the implemented approach at dedupe.go:218. The double-Problem call creates a content-addressed ID stable across runs, collision-resistant (SHA-256 truncated to 128 bits), and structurally distinct from any two-problem gray-pair ID where problemA != problemB. `Reviewer` must never be included in content-addressed IDs — it is externally supplied, omitempty, and can be spoofed. The Finding struct's stable fields are: Severity, File, Line, Problem, Fix, Category, EstMinutes, Evidence (reconcile/finding.go:18-32).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/dedupe.go
- reconcile/ambiguous.go
- reconcile/finding.go
