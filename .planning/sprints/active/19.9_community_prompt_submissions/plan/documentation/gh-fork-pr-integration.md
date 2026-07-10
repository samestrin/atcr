# GitHub Fork + PR Integration via go-gh

`[CRITICAL]`

## Overview

The `atcr personas submit <name>` subcommand needs to fork `samestrin/atcr`, push a branch, and open a pull request — all under the *invoking user's own* GitHub identity, using whatever credentials their local `gh auth login` session already established. `go-gh` (module `github.com/cli/go-gh/v2`) is the GitHub CLI team's library for exactly this: its top-level `gh` package shells out to the real `gh` binary and inherits the caller's environment, so `gh.ExecContext(ctx, "repo", "fork", ...)` or `gh.ExecContext(ctx, "pr", "create", ...)` naturally resolve the user's own session — no separately-managed bot token, no custom OAuth flow to build or maintain.

> Source: .planning/specifications/packages/go-gh.md#Overview

The codebase already has a token-based Git Data API wrapper — `internal/ghaction.Client.CreateBranch` (internal/ghaction/client.go:192) and `Client.CreatePullRequest` (internal/ghaction/client.go:345) — built for the `--auto-fix` bot flow (Epic 17.0). That client authenticates with one fixed bot Token against one configured repo, which is the wrong shape for `personas submit`: this command must act as the submitter, forking the canonical repo into the submitter's own account, not as a bot pushing to a repo it already owns. Reusing `internal/ghaction.Client` here would mean either hard-coding a bot identity for a personal contribution (wrong ownership model) or building a parallel per-user OAuth token flow from scratch — both of which `go-gh`'s shell-out-to-`gh` approach avoids entirely by deferring to the user's existing `gh auth login` session.

> Source: codebase-discovery.json — internal/ghaction/client.go:192, internal/ghaction/client.go:345

Because no existing code in the repository shells out to the `gh` CLI (`grep -rn "exec.Command(\"gh\"" --include="*.go"` returns nothing), `personas submit` is the first integration point of this kind. It should wrap the `gh` invocations behind an injectable seam (interface or package var), following the same testability pattern already used by `personasClient`/`personasFixtureRunner`, rather than calling `exec.Command`/`gh.Exec` bare and inline inside the cobra `RunE`. Before any fork/branch/commit work begins, the command must verify `gh` is on `PATH` and authenticated and halt with a clear, actionable error otherwise — mirroring the precondition-check pattern already documented in `skill/SKILL.md` for PR resolution.

> Source: codebase-discovery.json — architecture_notes ("No existing code shells out to the `gh` CLI...")

## Key Concepts

- **Shell out via `gh.ExecContext`, not `gh.Exec`.** `ExecContext` accepts a `context.Context` and is the variant to use for anything cancellable/timeout-bound — fork, push, and PR-create calls inside a cobra command with a context deadline all qualify.
  > Source: .planning/specifications/packages/go-gh.md#Key APIs

- **Credential resolution is automatic and user-scoped.** `gh.Exec`/`gh.ExecContext` resolve the `gh` binary on `PATH` and inherit the caller's environment, so they pick up whatever `gh auth login` (or `GH_TOKEN`/`GITHUB_TOKEN`) the invoking user already has — this is precisely why `personas submit` should not reuse `internal/ghaction.Client`'s single fixed bot token.
  > Source: .planning/specifications/packages/go-gh.md#Quick Start

- **`pkg/repository` can validate the fork target before shelling out.** `repository.Parse`/`repository.Current` parse `OWNER/REPO`-style references, useful for confirming the canonical repo reference (`samestrin/atcr`) is well-formed before invoking `gh repo fork`.
  > Source: .planning/specifications/packages/go-gh.md#Key APIs

- **`internal/ghaction.Client` exists but is architecturally unsuited to this flow.** It's a token-based Git Data API wrapper authenticating as one fixed bot against one configured repo (built for the `--auto-fix` flow in Epic 17.0), whereas `personas submit` must fork under the submitting user's own identity.
  > Source: codebase-discovery.json — internal/ghaction/client.go:192, internal/ghaction/client.go:345

- **This is a new integration point requiring an injectable seam.** No code in the repository currently shells out to `gh` (confirmed via `grep -rn "exec.Command(\"gh\"" --include="*.go"`), so the wrapper around `gh.ExecContext` calls should be an interface or package var — matching the existing `personasClient`/`personasFixtureRunner` testability pattern — rather than a bare inline `exec.Command`/`gh.Exec` call in the cobra `RunE`.
  > Source: codebase-discovery.json — architecture_notes

- **Precondition check must run before any fork/branch/commit work.** `cmd/atcr/personas.go:newPersonasSubmitCmd` should verify `gh` is on `PATH` and authenticated first, halting with a clear, actionable error message if not — mirroring the precondition pattern documented in `skill/SKILL.md` for PR resolution.
  > Source: codebase-discovery.json — integration_points ("cmd/atcr/personas.go:newPersonasSubmitCmd (gh precondition check)")

## Code Examples

### Basic `gh.Exec` usage

```go
import "github.com/cli/go-gh/v2"

// Shell out to a gh command and read its output, exactly as if the user typed it.
stdout, stderr, err := gh.Exec("issue", "list", "--repo", "cli/cli", "--limit", "5")
if err != nil {
    log.Fatal(err)
}
fmt.Println(stdout.String())
```

### Up-front precondition check

Mirror `skill/SKILL.md`'s pattern before any fork/branch/commit work:

```go
func checkGH(ctx context.Context) error {
    // Confirm gh is on PATH.
    if _, err := gh.Path(); err != nil {
        return fmt.Errorf("gh CLI not found on PATH; install it from https://cli.github.com")
    }
    // Confirm an active auth session. Non-zero exit + stderr has the reason.
    _, stderr, err := gh.ExecContext(ctx, "auth", "status")
    if err != nil {
        return fmt.Errorf("gh auth check failed: %s", stderr.String())
    }
    return nil
}
```

> Source: .planning/specifications/packages/go-gh.md#Quick Start

```go
func Exec(args ...string) (stdout, stderr bytes.Buffer, err error)
func ExecContext(ctx context.Context, args ...string) (stdout, stderr bytes.Buffer, err error)
func ExecInteractive(ctx context.Context, args ...string) error
func Path() (string, error)
```

> Source: .planning/specifications/packages/go-gh.md#Key APIs

```go
type Repository struct { Host, Name, Owner string }

func Current() (Repository, error)                    // detect repo from git remotes / GH_REPO
func Parse(s string) (Repository, error)               // "OWNER/REPO", "HOST/OWNER/REPO", or full URL
func ParseWithHost(s, host string) (Repository, error) // same, with an explicit host fallback
```

> Source: .planning/specifications/packages/go-gh.md#Key APIs

## Quick Reference

| Package | Function | Signature |
|---------|----------|-----------|
| `gh` (top-level) | `Exec` | `func Exec(args ...string) (stdout, stderr bytes.Buffer, err error)` |
| `gh` (top-level) | `ExecContext` | `func ExecContext(ctx context.Context, args ...string) (stdout, stderr bytes.Buffer, err error)` |
| `gh` (top-level) | `ExecInteractive` | `func ExecInteractive(ctx context.Context, args ...string) error` |
| `gh` (top-level) | `Path` | `func Path() (string, error)` |
| `pkg/api` | `DefaultRESTClient` | `func DefaultRESTClient() (*RESTClient, error)` |
| `pkg/api` | `NewRESTClient` | `func NewRESTClient(opts ClientOptions) (*RESTClient, error)` |
| `pkg/api` | `DefaultGraphQLClient` | `func DefaultGraphQLClient() (*GraphQLClient, error)` |
| `pkg/api` | `NewGraphQLClient` | `func NewGraphQLClient(opts ClientOptions) (*GraphQLClient, error)` |
| `pkg/api` (RESTClient) | `Get` | `func (c *RESTClient) Get(path string, resp interface{}) error` |
| `pkg/api` (RESTClient) | `Post` | `func (c *RESTClient) Post(path string, body io.Reader, resp interface{}) error` |
| `pkg/api` (RESTClient) | `DoWithContext` | `func (c *RESTClient) DoWithContext(ctx context.Context, method, path string, body io.Reader, response interface{}) error` |
| `pkg/api` (GraphQLClient) | `Query` | `func (c *GraphQLClient) Query(name string, q interface{}, variables map[string]interface{}) error` |
| `pkg/api` (GraphQLClient) | `Mutate` | `func (c *GraphQLClient) Mutate(name string, m interface{}, variables map[string]interface{}) error` |
| `pkg/repository` | `Current` | `func Current() (Repository, error)` |
| `pkg/repository` | `Parse` | `func Parse(s string) (Repository, error)` |
| `pkg/repository` | `ParseWithHost` | `func ParseWithHost(s, host string) (Repository, error)` |

## Related Documentation

- [go-gh Package Specification](../../../specifications/packages/go-gh.md) — full API reference for the top-level `gh` package, `pkg/api`, and `pkg/repository`
- `internal/ghaction/client.go` (`Client.CreateBranch` at line 192, `Client.CreatePullRequest` at line 345) — the existing token-based Git Data API wrapper used by the `--auto-fix` bot flow (Epic 17.0); noted as an existing-but-unsuitable alternative since it authenticates as one fixed bot against one configured repo rather than as the submitting user forking the canonical repo
