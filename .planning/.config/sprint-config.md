## Sprint Configuration

---

### E2E / Integration Test Pre-requisites

**Before running integration tests or starting the MCP server:**
- For stdio mode: Verify that the compiler can build the binary: `go build -o bin/atcr-mcp ./cmd/atcr-mcp`
- For SSE mode: If testing SSE connection, start the server locally:
    - Run: `go run ./cmd/atcr-mcp -mode sse` (if SSE mode is supported)
    - Verify: `curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/health` responds 200

#### Build & Run Commands

Define commands for building, running, formatting, linting, and testing the project.

| Command | Description | Working Directory |
|---------|-------------|-------------------|
| `go run ./cmd/atcr-mcp` | Start MCP server in stdio mode | Root `.` |
| `go build -o bin/atcr-mcp ./cmd/atcr-mcp` | Build the MCP server binary | Root `.` |
| `go test ./...` | Run unit tests | Root `.` |
| `go test -v -tags=integration ./...` | Run integration tests | Root `.` |
| `go fmt ./...` | Format all Go source files | Root `.` |
| `go vet ./...` | Run static analysis/typecheck checks | Root `.` |
| `golangci-lint run` | Run linter | Root `.` |

---

#### Services & Ports

| Service | Port | Description |
|---------|------|-------------|
| MCP stdio | N/A | Default communication channel for LLM clients |
| MCP SSE (Optional) | 8080 | HTTP/SSE server endpoint |

---

#### Health Checks

| Service | Health Check Command | Expected Output / Behavior |
|---------|---------------------|---------------------------|
| MCP stdio | `echo '{"jsonrpc":"2.0","method":"ping","id":1}' | bin/atcr-mcp` | JSON-RPC response containing result |
| MCP SSE | `curl -s http://localhost:8080/health` | `{"status":"ok"}` or similar JSON |

---

#### Pre-Sprint Checklist

Run these checks before starting sprint execution:

- [ ] `git status` - Working directory clean
- [ ] `go mod tidy` - Dependencies synced and tidy
- [ ] `.env` configured with required variables (e.g. `GEMINI_API_KEY`, `GITHUB_TOKEN`)
- [ ] No port conflicts on port 8080 (if running SSE mode)

---

#### Post-Sprint Checklist

Run these checks after sprint completion:

- [ ] `golangci-lint run` - No linting errors
- [ ] `go vet ./...` - No vet warnings
- [ ] `go fmt ./...` - All files formatted
- [ ] `go build -o bin/atcr-mcp ./cmd/atcr-mcp` - Compilation succeeds
- [ ] `go test ./...` - All unit and integration tests pass
- [ ] Manual verification using an MCP inspector or test client
- [ ] Documentation / README updated with any new tools, features, or environment variables

---

#### Environment Variables

Required environment variables for running/testing:

| Variable | Description | Required |
|----------|-------------|----------|
| `GEMINI_API_KEY` | Gemini API Key for running Agent reviews | Yes |
| `GITHUB_TOKEN` | GitHub Token for pull request integration | Optional |
| `LOG_LEVEL` | Log verbosity (debug, info, warn, error; default info). Implemented — see `docs/logging.md`. | No (defaults to info) |

---

#### Testing Strategy

| Test Type | Command | When to Run |
|-----------|---------|-------------|
| Unit Tests | `go test ./...` | After making code changes |
| Static Checks | `go vet ./...` | Before running tests |
| Lint | `golangci-lint run` | Before commits |
| Integration Tests | `go test -tags=integration ./...` | After sprint completion |

---

#### Deployment

| Environment | Branch | Deploy Command |
|-------------|--------|----------------|
| Staging / Local | `main` | Build binary and run locally / package in container |
| Production | Release Tag | Build statically compiled binary for release |

---

#### Project-Specific Notes

###### State Management
- The MCP server is designed to be stateless, operating directly on the context provided by files, git diffs, or standard input.

###### File Structure

```
atcr/
├── cmd/
│   └── atcr-mcp/
│       └── main.go           # MCP Server Entrypoint
├── pkg/                      # Reusable Go packages for code reviews & agent engine
├── skill/                    # atcr Agent Skill configuration & system prompts
│   ├── skill.yaml
│   └── prompts/
├── .planning/                # Planning configuration and sprint artifacts
├── go.mod                    # Go module file
└── README.md                 # Root documentation
```

###### Common Issues

- **Connection hangs (stdio)**: Ensure no debugging output prints directly to stdout (use stderr for logging).
- **Go version mismatches**: Verify `go version` matches the target in `go.mod`.

