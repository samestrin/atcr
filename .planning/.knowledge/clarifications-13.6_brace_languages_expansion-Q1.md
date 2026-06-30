---
id: mem-2026-06-30-ea8ed3
question: "Build-tag convention for braceparser active_*.go files: should //go:build wasip1 && &lt;lang&gt; tightening be applied across all active_*.go files?"
created: 2026-06-30
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/braceparser/active_java.go, internal/astgroup/parsers/build.sh, internal/astgroup/parsers/src/braceparser/go.mod]
tags: [clarifications, epic-13.6_brace_languages_expansion, implementation, braceparser, go:build, wasip1]
retrievals: 0
status: active
type: clarifications
---

# Build-tag convention for braceparser active_*.go files: shou

## Decision

Leave the single-tag convention (`//go:build &lt;lang&gt;`) as-is across all `active_*.go` files in internal/astgroup/parsers/src/braceparser/ — do not add a `&& wasip1` clause. build.sh always sets `GOOS=wasip1` for every invocation of the build() function, and Go treats GOOS as an implicit build constraint, so the extra clause would be a true no-op. The package's nested go.mod documents it as "built only for GOOS=wasip1 via build.sh, once per target language (-tags &lt;lang&gt;)" — there is no other build entry point where the missing wasip1 clause could matter. This convention should be followed for any future brace-language parser additions.

Justification:
- All active_*.go files (ts, php, rust, bash, java, kotlin, cpp, csharp) use the identical single-tag form with no wasip1 clause.
- build.sh unconditionally forces GOOS=wasip1 GOARCH=wasm for every language build invocation.
- The nested go.mod confirms there is no alternate build path (e.g. plain `go build ./...`) where the missing constraint could cause incorrect compilation.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/braceparser/active_java.go
- internal/astgroup/parsers/build.sh
- internal/astgroup/parsers/src/braceparser/go.mod
