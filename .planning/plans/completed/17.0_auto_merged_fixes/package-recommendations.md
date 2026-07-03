# Package Recommendations

Generated: 2026-07-02
Plan: 17.0_auto_merged_fixes

## Context

Existing `go.mod` dependencies checked: `cobra`, `testify`, `jsonschema-go`, `wazero`, `golang.org/x/mod`, `modelcontextprotocol/go-sdk`, `golang.org/x/oauth2`. Recommendations below only cover a gap none of these fill.

## Recommended

### 1. go-gitdiff (patch application)

- **Category:** Diff/patch application
- **Handles:** Parsing a unified diff into structured hunks AND applying those hunks to file content byte-for-byte, including fuzzy context matching when line numbers have drifted slightly — the actual "apply a patch to a file" mechanics AC2 needs.
- **Install command:** `go get github.com/bluekeyes/go-gitdiff`
- **Integration point:** `internal/autofix/apply.go` (new). `internal/payload.BuildEntriesFromDiff` (Epic 10.1, reused per AC1) splits a diff into per-file text bodies but does not apply hunks to content — that gap is exactly what this library closes. Feed each `FileEntry.Body` to `gitdiff.Parse` + `gitdiff.Apply` to produce the patched file content, then write it via `atomicfs.WriteFileAtomic`.
- **Reason:** Hand-rolling a hunk applier (context-line matching, offset tolerance for missing/shifted context, fuzzy matching for slightly-stale diffs) is a well-known correctness trap — subtle off-by-one or whitespace-handling bugs silently corrupt files. AC1 already calls out "handling missing context lines" as a requirement; that is precisely go-gitdiff's fuzzy-apply behavior.
- **Scores:** maturity 7/10 (stable, used in production tooling), complexity_saved 7/10, integration_risk 3/10 (isolated to the new apply path; does not touch existing diff-parsing or atomicfs code).

## Explicitly not recommended

### go-github (official GitHub API client)

AC5's new half (create branch, commit, open/update PR) could use `github.com/google/go-github` instead of extending the existing hand-rolled `internal/ghaction.Client`. Not recommended: the existing client already has retry/backoff, secret redaction, and typed `APIError` handling tailored to this codebase's conventions (Epic 7.3), and the new surface needed is only 3-4 REST calls (create ref, create commit, create/update PR). Pulling in a second, heavier HTTP client would fragment auth/error handling for a modest handful of endpoints. Extend `internal/ghaction/client.go` with new methods on the existing `Client` instead.
