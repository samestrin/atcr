---
id: mem-2026-06-25-6e32de
question: "How should \"byte-identical entry parity with a git-sourced equivalent\" be interpreted for diff-file ingestion tests when fixtures are loose diffs without diff --git/index headers?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [internal/payload/builder.go, internal/benchmark/testdata/suite-valid/case-01.diff]
tags: [clarifications, epic-10.1_diff_file_ingestion, testing, diff-ingestion, parity]
retrievals: 0
status: active
type: clarifications
---

# How should "byte-identical entry parity with a git-sourced e

## Decision

Round-trip + structural parity (not literal byte-equality with git output). Git always prepends `diff --git a/<path>` and `index <sha>..<sha> <mode>` header lines that loose fixture diffs intentionally omit (`internal/payload/builder.go:144-148` calls `rawChunks` which shells out to `git diff`). The correct assertion is: `ingest(diff)` produces entries whose Body fields concatenate back to the original diff bytes verbatim, entry count equals file count, and each `FileEntry.Path` is correctly parsed. `joinEntries` (`builder.go:208-217`) already concatenates Body strings, making round-trip parity deterministic and fixture-compatible.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/payload/builder.go
- internal/benchmark/testdata/suite-valid/case-01.diff
