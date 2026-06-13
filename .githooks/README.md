# Git hooks (`.githooks/`)

Tracked git hooks that mirror the GitHub **Go CI / "Go Lint & Test"** workflow,
so CI failures surface locally before a push.

## Enable (one-time, per clone)

```sh
git config core.hooksPath .githooks
```

`core.hooksPath` is a local config value, not stored in the repo, so each fresh
clone must run this once. Verify with `git config --get core.hooksPath`.

## What runs

| Hook | Gates | Speed |
|------|-------|-------|
| `pre-commit` | `gofmt` (auto-fixes & re-stages staged Go files), `go vet`, `go build` | fast — every commit |
| `pre-push` | `golangci-lint run`, `go test -race ./...` | slower — once per push |
| `post-commit` | semantic index update (`llm-semantic`) | background, non-blocking |

## Bypass

Skip hooks for a single operation when needed:

```sh
git commit --no-verify
git push --no-verify
```

## Notes

- The `pre-commit` `gofmt` step auto-formats and re-stages unformatted Go files.
  If a file is only partially staged, the re-stage may pull in its other pending
  edits — review the commit if you stage hunks selectively.
- `go test -race` in `pre-push` matches CI. Bypass with `--no-verify` if a known
  flaky test (tracked in `.planning/technical-debt/`) blocks an unrelated push.
