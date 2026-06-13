# User Story 7: Tool Definitions & Dispatcher

**Plan:** [2.0: Tool-Using Reviewers](../plan.md)

## User Story

**As a** tool-using reviewer agent
**I want** the engine to expose `read_file`, `grep`, and `list_files` as OpenAI-compatible function definitions with a dispatcher that enforces per-call byte caps
**So that** the agent has a minimal, predictable, read-only toolset whose outputs are bounded and safe

## Story Context

- **Background:** Epic 2.0 gives reviewers access to the repository through a deliberately minimal v1 toolset. The agent loop ([User Story 1: Agent Loop Execution](01-agent-loop-execution.md)) consumes these tools, and the path jail ([User Story 3: Path Jail & Snapshot Sandbox](03-path-jail-sandbox.md)) confines their filesystem access. This story owns the tool definitions themselves — their JSON Schema, their handler implementations, and the dispatcher that routes `tool_calls` to the right handler while enforcing per-call byte caps.
- **Assumptions:**
  - The toolset is intentionally limited to `read_file`, `grep`, and `list_files` for v1; additions wait for field evidence.
  - The path jail validates and resolves every path before filesystem I/O; this story does not re-implement jail logic but relies on it.
  - The OpenAI function-calling JSON Schema format (`name`, `description`, `parameters`) is the only wire format supported in v1.
- **Constraints:**
  - No new third-party dependencies — tool handlers use Go stdlib only (`os`, `regexp`, `path/filepath`, `bufio`, `strings`).
  - No write tools, no shell execution, no network access.
  - All file opens use `os.O_RDONLY` (plus `O_NOFOLLOW` where supported); there is no path for a write tool to be registered.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 3 (Path Jail & Snapshot Sandbox) — the dispatcher calls `jail.Resolve` before filesystem I/O |

## Success Criteria (SMART Format)

- **Specific:** `internal/tools` exposes three tool definitions (`read_file`, `grep`, `list_files`) as OpenAI-compatible JSON Schema, a dispatcher that routes incoming `tool_calls` to the correct handler, and per-call byte-cap enforcement with a truncation marker. Each handler uses the path jail and opens files read-only.
- **Measurable:** 100% of unit tests for the three tool handlers and dispatcher pass without any LLM, network, or real provider; tests cover line-numbered `read_file` output, optional `start_line`/`end_line`, regex `grep` with optional `glob`, depth-capped `list_files`, and per-call byte-cap truncation for `read_file` and `grep`.
- **Achievable:** The required primitives (`os.OpenFile`, `bufio.Scanner`, `regexp.Regexp`, `filepath.WalkDir`) are all stdlib; the path jail and snapshot manager provide the sandbox boundary.
- **Relevant:** Without this story the agent loop has tool definitions but no implemented handlers; the epic cannot satisfy the original requirement that agents "read a file outside the payload, grep for callers, and produce findings citing that evidence."
- **Time-bound:** Delivered before [User Story 1: Agent Loop Execution](01-agent-loop-execution.md) reaches integration with real tool dispatch; required for end-to-end multi-turn review tests.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [07-01](../acceptance-criteria/07-01-read-file-tool.md) | `read_file` Tool: Line-Numbered Output and Byte Cap | Unit |
| [07-02](../acceptance-criteria/07-02-grep-tool.md) | `grep` Tool: Regex, Glob Filter, and Match Cap | Unit |
| [07-03](../acceptance-criteria/07-03-list-files-tool.md) | `list_files` Tool: Depth-Capped Directory Listing | Unit |
| [07-04](../acceptance-criteria/07-04-tool-dispatcher-byte-caps.md) | Tool Dispatcher: Routing and Per-Call Byte Caps | Unit |

## Original Criteria Overview

1. Tool definitions for `read_file`, `grep`, and `list_files` are exposed as OpenAI function-calling JSON Schema (`name`, `description`, `parameters`).
2. `read_file(path, start_line?, end_line?)` returns line-numbered content; optional `start_line`/`end_line` returns a slice; content above the per-call byte cap is truncated with a marker.
3. `grep(pattern, glob?)` compiles `pattern` with Go stdlib `regexp`, optionally filters files by `glob`, and returns matches up to a match cap; truncation is marked when the cap is exceeded.
4. `list_files(dir?)` returns a depth-capped listing relative to the snapshot root; `dir` defaults to the root; depth cap prevents unbounded recursion.
5. The dispatcher routes each `tool_call` to its handler by name, enforces the per-call byte cap, and returns structured errors for unknown tools, jail violations, and handler errors.
6. All tool handlers are unit-tested without any LLM, network, or real provider; tests use fixture files and the path jail.

## Technical Considerations

- **Implementation Notes:**
  - New package `internal/tools` owns definitions, dispatcher, and handlers:
    - `defs.go`: `ToolDef` struct and exported `Tools() []ToolDef` helper returning the three v1 schemas.
    - `dispatch.go`: `Dispatcher` struct with `Execute(ctx, name, argsJSON) (Result, error)`; parses JSON arguments, validates required fields, calls `jail.Resolve` for path args, invokes the handler, applies `capResultBytes`.
    - `read_file.go`: `readFileHandler` using `bufio.Scanner` for line numbering; supports `start_line`/`end_line` (1-based, inclusive); reads up to `MaxReadFileBytes`.
    - `grep.go`: `grepHandler` walks the snapshot root with `filepath.WalkDir`, optionally filters by `glob` with `filepath.Match`, compiles `pattern` with `regexp.Compile`, collects matches up to `MaxGrepMatches`; truncates file content per match to a small cap and marks truncation.
    - `list_files.go`: `listFilesHandler` uses `os.ReadDir` recursively up to `MaxListDepth`; returns relative paths with a directory/file indicator.
    - `result.go`: `ToolResult` struct with `Content string`, `Truncated bool`, `OriginalBytes int`.
  - Per-call byte cap constants live in `defs.go` or a dedicated `limits.go`: `MaxReadFileBytes`, `MaxGrepFileBytes`, `MaxGrepMatches`, `MaxListDepth`, `MaxListFiles`.
  - The dispatcher's byte cap applies to the final string returned to the agent loop; handlers should pre-truncate where possible to avoid materializing huge strings.
  - Line numbering format: `"1: first line\n2: second line\n"`. Slicing by `start_line`/`end_line` preserves original line numbers.
- **Integration Points:**
  - `internal/tools/jail.go` — dispatcher calls `jail.Resolve(relPath)` before passing an absolute path to any handler.
  - `internal/tools/snapshot.go` — handlers receive the snapshot root; `list_files` defaults to the root when `dir` is omitted or empty.
  - `internal/fanout/engine.go` — agent loop passes `tool_calls` to the dispatcher and appends the returned `ToolResult.Content` as `role:"tool"` messages.
  - `internal/llmclient/client.go` — `ToolDef` schema matches the OpenAI function-calling shape expected by the wire format.
- **Data Requirements:**
  - `ToolDef`: `{Name string, Description string, Parameters map[string]any}` (OpenAI JSON Schema).
  - `ToolCall`: ID, function name, arguments JSON string.
  - `ToolResult`: `{Content string, Truncated bool, OriginalBytes int}`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `grep` regex is user-supplied (from model) and could be expensive | Medium | Use `regexp.Compile` with a timeout context; limit total files walked and matches collected; reject patterns that compile to extremely complex NFAs |
| `list_files` on a large repository returns huge listings | Medium | Hard depth cap and max entry cap; truncate and mark |
| `read_file` on a generated/vendor file returns megabytes | Medium | Per-call byte cap truncates before returning to the model; original bytes recorded |
| Handlers bypass the jail by accident | High | Dispatcher resolves every path through `jail.Resolve` before calling handlers; handlers accept absolute paths and never call `filepath.Join` with raw input |

---

**Created:** June 13, 2026
**Status:** AC Generated - Ready for Implementation
