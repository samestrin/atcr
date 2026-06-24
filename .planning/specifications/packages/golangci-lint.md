# golangci-lint

**Version:** v2.12.2 (May 6, 2026)
**Registry:** [github.com/golangci/golangci-lint](https://github.com/golangci/golangci-lint/releases)
**Official Docs:** [https://golangci-lint.run/](https://golangci-lint.run/)
**Tier:** Important
**Last Updated:** June 23, 2026

---

## Overview

Golangci-lint is a fast, parallel linters runner for Go that executes multiple code analysis tools simultaneously on Go projects. It combines over a hundred built-in linters without requiring separate installation, reducing false positives through tuned default settings, and supporting multiple output formats for CI/CD integration.

The tool is widely adopted in the Go ecosystem for automated code quality checks and is particularly valuable in projects with strict linting requirements.

## Installation

### Local Installation

```bash
# Download and install the latest version
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

# Or using Homebrew (macOS)
brew install golangci-lint

# Verify installation
golangci-lint --version
```

### CI/CD Integration

Golangci-lint provides GitHub Actions integration via a dedicated [GitHub Action](https://github.com/golangci/golangci-lint-action) for automated linting on every commit.

## Key APIs

### Command Line Interface

**Basic usage:**
```bash
golangci-lint run ./...
golangci-lint run ./cmd/... ./internal/...
```

**Common flags:**
- `--timeout` — Timeout for analysis (default: 1m)
- `--out-format` — Output format (text, json, tab, html, checkstyle, code-climate, junit-xml, teamcity, sarif)
- `--exclude` — Exclude specific linting rules or patterns
- `--new-from-rev` — Only analyze changed files since a git revision
- `--fix` — Fix found issues automatically (when supported by linter)

**Configuration:**
```bash
golangci-lint run --config .golangci.yml
```

### YAML Configuration

`.golangci.yml` file enables linter selection and tuning:

```yaml
linters:
  enable:
    - vet           # Go vet
    - ineffassign   # Detects ineffective assignments
    - staticcheck   # Advanced static analysis
    - errcheck      # Unchecked error returns

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - gocyclo
        - errcheck

run:
  timeout: 5m
  tests: true
```

## Common Patterns

### GitHub Actions Integration

```yaml
name: Lint
on: [push, pull_request]

jobs:
  golangci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      - uses: golangci/golangci-lint-action@v3
        with:
          version: v2.12.2
          args: --timeout 5m
```

### Local Pre-commit Hook

```bash
#!/bin/bash
golangci-lint run ./... || exit 1
```

Place in `.git/hooks/pre-commit` and `chmod +x` to run before each commit.

## Integration Notes (atcr)

- Critical for module CI/CD pipelines — required for `./reconcile` module independent testing
- Typically configured via `.golangci.yml` at repository root
- Supports parallel test execution and incremental analysis (caches previous results)
- Can be pinned to a specific version in GitHub Actions workflows for reproducibility
- Integrates with most Go IDEs via language server plugins

---

**Source:** Extracted from official sources on June 23, 2026.

**Reference Links:**
1. [Golangci-lint Documentation](https://golangci-lint.run/)
2. [GitHub Repository](https://github.com/golangci/golangci-lint)
3. [GitHub Actions](https://github.com/golangci/golangci-lint-action)
4. [Linters Documentation](https://golangci-lint.run/docs/linters/)
