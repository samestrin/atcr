---
id: mem-2026-07-09-e0ea5c
question: "How does atcr's persona model resolver (Epic 19.7) decide which of alias / created-timestamp / explicit-pin strategy to use, and why is resolution never on the review hot path?"
created: 2026-07-09
last_retrieved: ""
sprints: [19.7_live_model_resolution]
files: [internal/personas/catalog.go, internal/personas/upgrade.go, internal/registry/config.go]
tags: [sprint-learning, 19.7_live_model_resolution, architecture, resolver]
retrievals: 0
status: active
type: sprint-learning
---

# How does atcr's persona model resolver (Epic 19.7) decide wh

## Decision

A persona's `binding:` string parses to `Binding{Family, Channel, Pin}` via a strict grammar (internal/personas/catalog.go): (1) empty → no resolution, the bindingless persona keeps 19.6's static pinned `model`; (2) `pin:<slug>` → explicit pin, resolves to that exact slug, never floats regardless of channel; (3) `<family>@<channel>` → alias passthrough if `family` is in `aliasTable` (7 of 10 personas — anthropic, openai, google, moonshotai — provider owns freshness via a `~vendor/…-latest` slug, channel value is ignored); else `created`-timestamp newest-in-vendor-prefix scan if `family` is in `vendorPrefixTable` (deepseek/qwen/z-ai — no `-latest` alias exists, so atcr picks the newest `created` entry itself, subject to `@stable`/`@latest` channel filtering for preview/deprecated exclusion). Resolution only runs inside `atcr personas upgrade` (`internal/personas/upgrade.go`), which fetches the catalog once per persona and writes the resolved slug into a lock; `internal/registry.ResolvePersona` (the actual review-time prompt-resolution path) reads only the lock and never touches the network — proven by an import-boundary test. This keeps code reviews reproducible-by-default: the model only changes on an explicit, user-initiated `upgrade`, never silently mid-review.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/catalog.go
- internal/personas/upgrade.go
- internal/registry/config.go
