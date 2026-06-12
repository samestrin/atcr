---
plan: 1.0_atcr_core
date: 2026-06-10
type: feature
---

# Package Recommendations

## Summary

This greenfield Go project requires several battle-tested packages for CLI, YAML, MCP, and testing. The epic plan explicitly specifies cobra, yaml.v3, and go-sdk. Additional recommendations focus on testing infrastructure.

## Specified in Epic (Required)

| Package | Category | Purpose | Install |
|---------|----------|---------|---------|
| `github.com/spf13/cobra` | CLI Framework | Subcommands, flags, help generation | `go get github.com/spf13/cobra` |
| `gopkg.in/yaml.v3` | YAML Parsing | Registry and config file parsing | `go get gopkg.in/yaml.v3` |
| `github.com/modelcontextprotocol/go-sdk` | MCP Server | MCP stdio server implementation | `go get github.com/modelcontextprotocol/go-sdk` |

## Recommended Additions

### 1. github.com/stretchr/testify (Testing)

**Category:** Testing  
**Handles:** Assertions, mocking, suite management  
**Install:** `go get github.com/stretchr/testify`  
**Integration:** All `*_test.go` files  
**Reason:** Industry standard for Go testing. Provides `assert` and `require` for clear test failures, `mock` for LLM client testing, `suite` for organized test cases. High ROI for the extensive test coverage required (range resolution, payload builders, reconciler clustering/dedupe).  
**Scores:** maturity=10, complexity_saved=8, integration_risk=1

### 2. Stdlib Only (Recommended)

**HTTP Client:** Use `net/http` from stdlib  
**Reason:** OpenAI-compatible API calls are straightforward HTTP POST with JSON. No need for external HTTP clients. Retry logic can be implemented in ~50 lines.

**Logging:** Use `log/slog` from stdlib (Go 1.21+)  
**Reason:** Structured logging is built-in. No need for zap or logrus for a tool of this scope.

**Git Operations:** Use `os/exec` to call `git` directly  
**Reason:** The requirements need `git diff --function-context`, `git rev-parse`, `git merge-base`, etc. Using exec avoids CGO issues with go-git and gives access to all git features. Simpler and more reliable for this use case.

## Not Recommended

- **go-git (github.com/go-git/go-git):** Adds CGO complexity, doesn't support all diff options needed (--function-context). Use exec git instead.
- **zap/logrus:** Overkill when slog is in stdlib.
- **resty/heimdall:** Unnecessary abstraction over net/http for simple API calls.

## Package Matrix

| Need | Package | Source |
|------|---------|--------|
| CLI framework | spf13/cobra | Epic-specified |
| YAML parsing | gopkg.in/yaml.v3 | Epic-specified |
| MCP server | modelcontextprotocol/go-sdk | Epic-specified |
| Testing | stretchr/testify | Recommended |
| HTTP client | net/http | Stdlib |
| Logging | log/slog | Stdlib |
| Git operations | os/exec | Stdlib |
