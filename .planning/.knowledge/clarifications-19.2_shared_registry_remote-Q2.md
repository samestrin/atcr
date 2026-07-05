---
id: mem-2026-07-05-bd1d03
question: "Config-loader convention: absence triggers fallback, presence-but-broken always hard-fails"
created: 2026-07-05
last_retrieved: ""
sprints: []
files: [internal/registry/overlay.go, internal/registry/config.go]
tags: [clarifications, epic-19.2_shared_registry_remote, architecture, registry, config-loading]
retrievals: 0
status: active
type: clarifications
---

# Config-loader convention: absence triggers fallback, presenc

## Decision

This codebase's established convention for config/registry loaders is: absence of a config source is the only trigger for a fallback/optional-skip behavior; if a source is present but broken (unreachable, unparseable, read error), the loader always hard-fails rather than silently degrading to another source. There is no "source A fails -> silently degrade to source B" pattern anywhere in internal/registry. When designing a new config source (e.g. a remote URL gated by an env var), match this convention: fall back to the next source only when the env var/source is unset, and hard-error (no silent fallback) when it's set but broken.

Justification:
- internal/registry/overlay.go (parseRegistryFile) treats a present-but-broken source unconditionally as an error: missing file -> explicit "run atcr init" error, read error -> wrapped error, YAML parse error -> wrapped error. No silent-fallback branch exists.
- internal/registry/overlay.go (LoadProjectRegistry) is the one true fallback precedent, but it only treats absence (os.ErrNotExist) as non-error/optional; any other failure (read/parse error) still hard-fails.
- internal/registry/config.go (LoadRegistry/DefaultRegistryPath) is the sole local-registry loader and hard-fails on any read/parse problem via parseRegistryFile.
- The only "fallback" terminology elsewhere in the codebase (internal/registry/graph.go, docs/registry.md) refers to the unrelated agent model-fallback-chain feature, not config-source fallback — don't confuse the two when searching for precedent.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/overlay.go
- internal/registry/config.go
