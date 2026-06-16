---
id: mem-2026-06-15-372177
question: "Should leaderboard writeExportFile (cmd/atcr/leaderboard.go:147) harden against the --output symlink/TOCTOU, document the behavior, or accept the risk?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [cmd/atcr/leaderboard.go, cmd/atcr/anchor.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, security, leaderboard, symlink, toctou, output-flag, cli-convention, accept-and-document]
retrievals: 0
status: active
type: clarifications skill, sprint 3.3_per_run_scorecard, 2026-06-15
---

# Should leaderboard writeExportFile (cmd/atcr/leaderboard.go:

## Decision

Document that --output follows symlinks and close as accepted risk — consistent with the atcr CLI's accept-and-document posture for user-supplied paths (see anchorDir anchor.go:12-17 and the scorecard.go:104 path-traversal disposition). Blast radius is the user's own permissions on a path the user explicitly chose; writeExportFile already writes via temp-file + atomic os.Rename (0600 file / 0700 dir), and os.Rename takes no O_NOFOLLOW. The TOCTOU is the non-atomic os.Stat dir check vs the rename replacing a symlink at the target. True hardening would require an lstat-refuse-if-symlink guard before the rename, which only narrows (does not close) the window and adds friction for a non-threat on a local CLI. Recommended fix: a one-line note on the --output flag help and the writeExportFile comment (leaderboard.go:156-159). The lstat-refuse guard is the concrete hardening form if belt-and-suspenders is wanted.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/leaderboard.go
- cmd/atcr/anchor.go
