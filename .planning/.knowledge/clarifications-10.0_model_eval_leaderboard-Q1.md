---
id: mem-2026-06-24-0a84fb
question: "How should isSafeRelPath / path validation reject symlink escapes in file loading?"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [internal/benchmark/benchmark.go:70]
tags: [clarifications, epic-10.0_model_eval_leaderboard, security, implementation, symlink, path-validation]
retrievals: 0
status: active
type: clarifications/10.0_model_eval_leaderboard
---

# How should isSafeRelPath / path validation reject symlink es

## Decision

String-only path validators (like isSafeRelPath) cannot catch symlink escapes — they have no filesystem access. The fix belongs in the Load function: replace os.Stat with os.Lstat at the point where the file is checked. os.Stat follows symlinks, so a symlink pointing outside the suite directory passes IsRegular() on its target. os.Lstat reports the symlink itself as ModeSymlink, so fi.Mode().IsRegular() returns false and the existing error path rejects it. An alternative (filepath.EvalSymlinks + containment check) is more precise but significantly more complex; for external-manifest threat models, os.Lstat + !IsRegular() is simpler and sufficient.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/benchmark/benchmark.go:70
