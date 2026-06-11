## Coding Standards

#### 1. Syntax & Conventions (Go)

###### Naming
- **Packages**: Lowercase, single-word, no underscores or mixedCaps (e.g., `mcp`, `review`, `config`).
- **Variables / Struct Fields**:
  - Exported (public): `PascalCase` (e.g., `ConfigPath`, `MCPMessage`).
  - Unexported (private): `camelCase` (e.g., `currentSession`, `hasAccess`).
- **Functions / Methods**: `PascalCase` (e.g., `NewServer`, `RunReview`).
- **Constants**: `PascalCase` or standard Go uppercase (avoid raw UPPER_SNAKE_CASE in Go, prefer typed constants like `const MaxRetries = 3`).
- **Files**: Snake case (e.g., `code_review.go`, `mcp_server.go`) or simple lowercase (e.g., `server.go`).
- **Interfaces**: Exported interfaces should end with "-er" if they represent a single-action behavior (e.g., `Reviewer`, `Formatter`).

###### Code Layout
- **Imports**: Group imports as follows:
  1. Standard library packages
  2. Third-party packages
  3. Internal package imports (prefixed with `github.com/samestrin/atcr/`)
  Use `goimports` to auto-arrange imports.
- **Receiver Names**: 1-3 letters representing the receiver type (e.g., `s` for `Server`, `m` for `MCP`). Do not use `this` or `self`.
- **Short Variable Declarations**: Use `:=` for initialization where type is inferred. Use `var` for default zero-value declaration.

#### 2. Design Patterns & Concurrency

###### Error Handling
- Return `error` as the last parameter in functions.
- Never ignore errors; explicitly check them or return them.
- Wrap errors with contextual information using `fmt.Errorf("doing action: %w", err)` for debugging transparency.
- Do not use `panic` for normal error conditions; panic only for unrecoverable startup issues.

###### Context Propagation
- Accept `context.Context` as the first parameter in long-running operations or functions performing I/O.
- Respect context cancellation and timeout signals in goroutines.

###### Structs and Interfaces
- Keep interfaces small.
- Return concrete types from constructors (e.g., `func NewServer() *Server`), and accept interfaces as parameters to ensure decoupleability.

#### 3. Testing Standards

###### Tools
- Use the standard `testing` library.
- Assertions: Prefer standard `if got != want` checks, or use `github.com/stretchr/testify/assert` / `github.com/stretchr/testify/require` for cleaner test suites.

###### Test Structure
- **Table-Driven Tests**: Use table-driven tests for functions with multiple input/output scenarios.
- **Location**: Test files must be named `*_test.go` and reside in the same package directory as the code under test.
- **Integration Tests**: Use build tags (e.g., `//go:build integration` at the top of the file) to separate integration tests from unit tests.

#### 4. Tooling & Quality Gateways

###### Formatting & Linting
- **Formatting**: Code must be formatted with `go fmt` (or `goimports`) before checking in.
- **Linter**: Code must pass `golangci-lint run` (with standard staticcheck and errcheck configurations enabled).
- **Vet**: Always run `go vet ./...` during commits/sprints to catch common compilation-level pitfalls.

