# Package Recommendations

**Plan:** 19.9 Community Prompt Submissions
**Generated:** 2026-07-10

## Context

`personas submit` needs to fork `samestrin/atcr` under the invoking user's own GitHub identity, push a branch, and open a PR — a flow the existing `internal/ghaction.Client` (a fixed-bot-token REST client built for Epic 17.0's --auto-fix Action) is not designed for. The codebase has no existing `gh` CLI integration (`grep -rn "exec.Command(\"gh\"" --include="*.go"` returns nothing), so this is a new integration point.

## Recommendation

### github.com/cli/go-gh/v2

| Field | Value |
|-------|-------|
| Category | GitHub CLI integration |
| Handles | Structured invocation of the `gh` binary (argument building, JSON-flag output decoding, auth-token lookup) instead of a bare `os/exec.Command("gh", ...)` call |
| Install | `go get github.com/cli/go-gh/v2` |
| Integration point | New `internal/personas` (or `cmd/atcr`) submit-flow file wrapping `gh repo fork`, `gh pr create` |
| Maturity | 9/10 — official GitHub CLI org package, used by the `gh` extension ecosystem |
| Complexity saved | 5/10 — mainly saves JSON-output parsing and auth-token resolution boilerplate; the fork/PR calls themselves are still just a few `gh` invocations |
| Integration risk | 3/10 — small, focused API surface; matches the project's existing pattern of injectable seams (`personasClient`, `personasFixtureRunner`) so it can be wrapped behind an interface for testing |

**Reason:** Avoids hand-rolling `gh` subprocess invocation, JSON-output parsing, and auth-token discovery — all of which `go-gh` already does correctly (respecting the user's `gh auth login` session, `GH_TOKEN`/`GITHUB_TOKEN` env vars, and enterprise host config) rather than reimplementing GitHub CLI auth resolution from scratch.

## Not Recommended

- **A full GitHub REST API Go SDK** (e.g. `google/go-github`) — rejected. `personas submit` operates as the *user*, not as a bot with a fixed token; `gh` CLI already owns that identity resolution, and adding a second GitHub client (on top of the existing `internal/ghaction.Client`) to fetch the current user's token would duplicate what `gh`/`go-gh` already solves.
