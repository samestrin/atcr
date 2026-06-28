---
id: mem-2026-06-27-3e02e1
question: "How should the Close-vs-Parse race in astgroup/host.go be fixed, and what is its production priority?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/astgroup/host.go, internal/astgroup/grouper.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, concurrency, synchronization, wazero, host, rwmutex, waitgroup, close-race]
retrievals: 0
status: active
type: clarifications
---

# How should the Close-vs-Parse race in astgroup/host.go be fi

## Decision

The race window: Host.Parser() releases h.mu before returning the *wasmParser pointer; the caller then calls parser.Parse(src) outside any Host lock; if Close() fires between these two lines, h.runtime.Close() tears down the wasm runtime while Parse() is executing, causing use-after-close. The existing h.mu protects only the parsers map and h.closed flag, not the lifetime of *wasmParser instances after they are handed out.

Two correct synchronization mechanisms:
1. Promote h.mu to sync.RWMutex — Parse callers hold RLock for the full duration of the parse; Close() takes the exclusive write lock, which drains all in-flight parses before teardown. Idiomatic Go for the "many readers, one closer" pattern.
2. sync.WaitGroup — h.wg.Add(1) before returning from Parser(), h.wg.Done() deferred after Parse() in the caller, Close() calls h.wg.Wait() before h.runtime.Close(). Keeps the existing Parser() interface intact.

A log message alone is insufficient — it cannot prevent the runtime teardown from racing with an active wasm function call.

Production priority is LOW: SharedHost() is explicitly documented as intentionally never closed (host.go:88-91; lifetime = process lifetime). The race is unreachable on the singleton production path. Fix is required for tests or any transient Host instances that exercise Close().

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/host.go
- internal/astgroup/grouper.go
