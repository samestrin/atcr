# go-gh

**Version:** v2.13.0
**Registry:** [https://pkg.go.dev/github.com/cli/go-gh/v2](https://pkg.go.dev/github.com/cli/go-gh/v2)
**Official Docs:** [https://pkg.go.dev/github.com/cli/go-gh/v2](https://pkg.go.dev/github.com/cli/go-gh/v2)
**Tier:** Important
**Last Updated:** July 10, 2026 10:43:02AM

---

## Overview

`go-gh` (module `github.com/cli/go-gh/v2`) is a Go library published by the GitHub CLI team (`cli` org) to make authoring `gh` CLI extensions easier. It gives a Go program the same conventions the `gh` binary itself uses: repository detection via `GH_REPO`/git remotes, authentication via `GH_TOKEN`/`GH_HOST`/the user's `gh auth login` session, terminal capability detection (`GH_FORCE_TTY`, `NO_COLOR`), and shared output formatting. For atcr's `personas submit` flow, the relevant pieces are the top-level `gh` package (shell out to the real `gh` binary) and `pkg/api` / `pkg/repository` (talk to the GitHub API and parse repo references) — all of which resolve credentials the same way the user's own `gh auth login` session does, rather than requiring a separately-managed bot token. License: MIT.

## Quick Start

```bash
go get github.com/cli/go-gh/v2
```

```go
import "github.com/cli/go-gh/v2"

// Shell out to a gh command and read its output, exactly as if the user typed it.
stdout, stderr, err := gh.Exec("issue", "list", "--repo", "cli/cli", "--limit", "5")
if err != nil {
    log.Fatal(err)
}
fmt.Println(stdout.String())
```

`gh.Exec` resolves the `gh` binary on `PATH` and inherits the caller's environment, so it naturally picks up whatever credentials the user's `gh auth login` (or `GH_TOKEN`/`GITHUB_TOKEN`) already established — no separate auth plumbing needed for fork/push/PR operations.

## Key APIs

**Top-level `gh` package** — shell out to the `gh` CLI (fork, push, PR create/list, etc.):
```go
func Exec(args ...string) (stdout, stderr bytes.Buffer, err error)
func ExecContext(ctx context.Context, args ...string) (stdout, stderr bytes.Buffer, err error)
func ExecInteractive(ctx context.Context, args ...string) error
func Path() (string, error)
```
`ExecContext` is the one to use for anything cancellable/timeout-bound (e.g. `gh repo fork`, `gh pr create` inside a CLI command with a context deadline).

**`pkg/api`** — typed REST/GraphQL clients that reuse the same host/token resolution as `gh` itself:
```go
func DefaultRESTClient() (*RESTClient, error)
func NewRESTClient(opts ClientOptions) (*RESTClient, error)
func DefaultGraphQLClient() (*GraphQLClient, error)
func NewGraphQLClient(opts ClientOptions) (*GraphQLClient, error)

func (c *RESTClient) Get(path string, resp interface{}) error
func (c *RESTClient) Post(path string, body io.Reader, resp interface{}) error
func (c *RESTClient) DoWithContext(ctx context.Context, method, path string, body io.Reader, response interface{}) error

func (c *GraphQLClient) Query(name string, q interface{}, variables map[string]interface{}) error
func (c *GraphQLClient) Mutate(name string, m interface{}, variables map[string]interface{}) error
```
`ClientOptions{AuthToken, Host, Timeout, ...}` lets a caller override the token/host explicitly; leaving `AuthToken` empty falls back to the same `GH_TOKEN`/`GITHUB_TOKEN`/keyring lookup `gh` uses.

**`pkg/repository`** — parse/resolve `OWNER/REPO` style references (useful for validating the fork target before shelling out):
```go
type Repository struct { Host, Name, Owner string }

func Current() (Repository, error)                    // detect repo from git remotes / GH_REPO
func Parse(s string) (Repository, error)               // "OWNER/REPO", "HOST/OWNER/REPO", or full URL
func ParseWithHost(s, host string) (Repository, error) // same, with an explicit host fallback
```

Full API reference (all subpackages — `pkg/auth`, `pkg/browser`, `pkg/config`, `pkg/tableprinter`, `pkg/term`, `pkg/markdown`, etc.): [pkg.go.dev/github.com/cli/go-gh/v2](https://pkg.go.dev/github.com/cli/go-gh/v2). Source and extended examples: [github.com/cli/go-gh](https://github.com/cli/go-gh).

---
**Source:** Extracted from official sources on July 10, 2026 10:43:02AM.
