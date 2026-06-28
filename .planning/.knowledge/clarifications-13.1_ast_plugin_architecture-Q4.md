---
id: mem-2026-06-27-3ca77e
question: "In astgroup/grouper.go treeFor(), is the cache update a race condition or a transient-error caching defect?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/astgroup/grouper.go, internal/astgroup/host.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, concurrency, caching, transient-error, grouper, mutex]
retrievals: 0
status: active
type: clarifications
---

# In astgroup/grouper.go treeFor(), is the cache update a race

## Decision

It is a transient-error caching defect, not a race. g.mu.Lock() is acquired at the top of treeFor and held via defer for the entire function body — including the cache read, the pre-store write, the readFile I/O, the host.Parser() call, and parser.Parse(). No TOCTOU window exists. The real bug: on a transient readFile error (EAGAIN, EMFILE, etc.), a negative parsedFile (pf.ok == false) stored at the pre-store write remains in the cache permanently, silently suppressing all future retries for that file. The fix — delete(g.cache, file) on transient read errors — corrects this caching-policy defect. The TD label "race condition" is incorrect; it should be "transient-error caching defect." The fix is still valid and should be applied.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/grouper.go
- internal/astgroup/host.go
