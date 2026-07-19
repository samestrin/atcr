---
id: mem-2026-07-18-8a4a5b
question: "macOS /private/var/folders temp-dir collision with the system-dir path validation guard"
created: 2026-07-18
last_retrieved: ""
sprints: [31.0_axi_compliance]
files: [cmd/atcr/report.go, internal/validation/validation.go, internal/validation/validation_test.go]
tags: [clarifications, sprint-31.0_axi_compliance, security, architecture, path-validation, macos, symlink]
retrievals: 0
status: active
type: clarifications
---

# macOS /private/var/folders temp-dir collision with the syste

## Decision

When hardening a --output/file-path flag by resolving symlinks (e.g. resolveOutputPath in cmd/atcr/report.go), resolving a not-yet-existing leaf's PARENT directory to close a symlinked-parent bypass will make macOS temp writes (t.TempDir(), os.TempDir()) fail validation: /var/folders/... canonicalizes to /private/var/folders/..., which collides with a blanket "/private/var" entry in a system-dir blocklist (internal/validation/validation.go:70). The fix is NOT to remove the /private/var guard — it must stay blocked for /private/var/db and similar (pinned by internal/validation/validation_test.go:82-83) — but to carve out exactly "/private/var/folders" as an exemption, symmetric with the existing precedent that "/tmp" (which resolves to "/private/tmp") is never in the blocklist at all. Both the parent-symlink-resolve fix and the validation.go carve-out must ship together as one change; shipping the resolve fix alone (as in commit 617ecda9) breaks legitimate temp writes and gets reverted (266970ab).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/report.go
- internal/validation/validation.go
- internal/validation/validation_test.go
