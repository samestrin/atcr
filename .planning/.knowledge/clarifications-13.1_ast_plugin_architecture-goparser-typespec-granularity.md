---
id: mem-2026-06-28-964d74
question: "Should goparser's structural() function include ast.TypeSpec nodes to add per-type block granularity, or is gendecl granularity intentional?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/goparser/main.go, internal/astgroup/parsers/src/pyparser/main.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, goparser, typespec, gendecl, dead-code, merkle-hash, structural]
retrievals: 0
status: active
type: clarifications
---

# Should goparser's structural() function include ast.TypeSpec

## Decision

Gendecl granularity is intentional. Do NOT add TypeSpec to structural(). Remove the dead code instead.

Reasoning:
1. structural() documents its policy at goparser/main.go:111-118: "Declarations, statements, and function literals carry structure." ast.TypeSpec is an ast.Spec (sub-component of GenDecl), not an ast.Decl — its exclusion is consistent with the documented rule.

2. AC3 is served at gendecl level: a finding at line 5 and one at line 7 inside the same `type Foo struct { }` both map to the containing GenDecl node, grouping correctly regardless of whitespace drift.

3. Python parallel (pyparser/main.go:249-268): Python emits class/func as structural nodes but NOT individual fields/attributes. This is the gendecl-level analogue — cross-language consistency argues against TypeSpec.

4. Adding TypeSpec would be a Merkle-hash-breaking change (existing findings re-keyed) with no AC3 justification.

Dead code to remove:
- The *ast.TypeSpec case in nodeKind() at goparser/main.go:128 (returns "type")
- The *ast.TypeSpec case in nodeName() at goparser/main.go:143-147 (returns type name)
Both are unreachable because build() only calls them when structural() returns true, and TypeSpec never passes that gate.

Fix: delete the dead cases; add inline comment to structural() stating TypeSpec is intentionally excluded; rebuild go.wasm. No host test change needed (hash unchanged).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/goparser/main.go
- internal/astgroup/parsers/src/pyparser/main.go
