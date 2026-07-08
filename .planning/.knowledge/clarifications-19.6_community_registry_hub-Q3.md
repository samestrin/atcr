---
id: mem-2026-07-08-429f89
question: "How should the e2e discover-by-model test exercise an on-disk installed persona instead of the embedded one, once on-disk community fixture support lands?"
created: 2026-07-08
last_retrieved: ""
sprints: [19.6_community_registry_hub]
files: [internal/personas/e2e_discover_test.go, internal/personas/test.go, internal/personas/unit.go]
tags: [clarifications, sprint-19.6_community_registry_hub, testing, personas, go]
retrievals: 0
status: active
type: clarifications
---

# How should the e2e discover-by-model test exercise an on-dis

## Decision

Wire TemplateFixtureRunner{PersonasDir: func() (string, error) { return destDir, nil }} (mirroring the production wiring at cmd/atcr/personas.go) and implement the currently-dead PersonasDir field inside RunFixture (internal/personas/test.go): when set, attempt os.ReadFile(destDir/<name>.md) and a disk-reading counterpart to builtins.CommunityModel for the .yaml; fall through to the embedded builtins.CommunityGet/CommunityModel path if the on-disk file is absent. The fixture patch itself stays embedded-only (builtins.CommunityFixture) since InstallUnit's contract only ever writes <name>.yaml + <name>.md to disk, never a testdata/*.patch — so there is nothing to read from disk for the patch regardless. Resolves TD-013.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/e2e_discover_test.go
- internal/personas/test.go
- internal/personas/unit.go
