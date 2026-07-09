# Catalog Snapshot Fixture Discipline

**Priority:** [IMPORTANT]

## Overview

Epic 19.7's resolver and `atcr models check` command read live OpenRouter catalog data, but CI must remain zero-live-network (AC8). The project already enforces this discipline for the community registry client: `internal/personas/client.go`'s `fetch()` is exercised through an injected `HTTPClient` backed by `httptest.NewServer`, never against the real community repo in CI. The same pattern applies to the new OpenRouter catalog client.

The fixture that makes this possible is a checked-in JSON snapshot of OpenRouter's `/api/v1/models` response. Tests point the catalog client at a local `httptest` server that returns this snapshot, so every resolution path — alias matching, `created`-timestamp newest-in-vendor-prefix selection, `@stable` preview exclusion, deprecation detection, and missing-slug handling — runs deterministically against known data.

## Key Concepts

- **Fixture location.** The canonical snapshot lives at `internal/personas/testdata/catalog_snapshot.json`. This path follows the existing Go convention already used by `internal/personas/client_test.go` and other package testdata.
  > Source: [codebase-discovery.json > integration_gaps > gap-006]

- **Snapshot content.** The fixture must contain enough of the `/api/v1/models` array to exercise all resolver branches:
  - At least one `~`-prefixed `-latest` alias per alias-covered vendor (Anthropic, OpenAI, Google, Moonshot).
  - Multiple concrete models under the DeepSeek, Qwen, and `z-ai/` prefixes so the `created`-timestamp "newest-in-vendor-prefix" resolver has candidates to choose from.
  - At least one model with a non-null `expiration_date` so deprecation detection is testable.
  - At least one model whose slug contains preview/beta/exp tokens so `@stable` exclusion is testable.
  - All 10 of Epic 19.6's currently pinned slugs so the "seed lock with zero migration" path is covered.

- **Catalog client design reuses the existing HTTPClient seam.** `internal/personas/client.go:35` defines a minimal `HTTPClient` interface (`Do(req) (*http.Response, error)`). Production passes `*http.Client`; tests pass an `httptest.NewServer`-backed client. The new catalog client must accept the same interface so it can be pointed at the fixture server.
  > Source: [codebase-discovery.json > existing_patterns > Injectable HTTPClient + httptest zero-live-network testing]

- **Tests never call the real OpenRouter endpoint.** All resolver/catalog unit tests use the snapshot fixture. A single opt-in integration or spike command (AC1) is the only place an authenticated live completion call is made, and that is a manual/user-initiated step, not CI.

- **Refresh command.** Because the live catalog schema and model list drift over time, the epic ships a refresh command that regenerates `internal/personas/testdata/catalog_snapshot.json` from a live `/api/v1/models` call. This command is run on demand — typically when the resolver tests need to cover a new model shape or when a schema change is suspected — not on every build. The refresh command itself is allowed to touch the network, but its output is committed so CI stays offline.
  > Source: [original-requirements.md > Proposed Solution #9]
  > Source: [codebase-discovery.json > integration_gaps > gap-006]

- **Snapshot drift is a risk, not a feature.** A stale snapshot can hide a real API change. Mitigate by documenting the refresh command, keeping a comment in the fixture noting the fetch date, and running the refresh when resolver behavior changes or when OpenRouter announces schema changes.
  > Source: [plan.md > Risk Mitigation]

## Code Examples

No literal code snippets exist in the discovery data for the not-yet-implemented catalog client. The structural template is:

```go
// ILLUSTRATIVE ONLY — not existing code.
// A CatalogClient accepts the same HTTPClient interface as the rest of
// internal/personas, so tests can inject an httptest server.
type CatalogClient struct {
    HTTPClient HTTPClient
    BaseURL    string
}
```

The fixture is loaded by tests with `os.ReadFile("testdata/catalog_snapshot.json")` and served through `httptest.NewServer`:

```go
// ILLUSTRATIVE ONLY — not existing code.
ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    _ = r // assert method/path if desired
    data, _ := os.ReadFile("testdata/catalog_snapshot.json")
    w.Header().Set("Content-Type", "application/json")
    _, _ = w.Write(data)
}))
defer ts.Close()
```

## Quick Reference

| Item | Value / Rule |
|---|---|
| Fixture path | `internal/personas/testdata/catalog_snapshot.json` |
| Injection seam | `internal/personas.HTTPClient` (`Do(req) (*http.Response, error)`) |
| Test server | `httptest.NewServer` |
| Live network in CI | None |
| Refresh command | Regenerates the fixture on demand from `/api/v1/models` |
| Fixture must cover | `-latest` aliases, `z-ai/`/`deepseek/`/`qwen/` prefixes, `expiration_date`, preview tokens, all 10 existing pinned slugs |

## Related Documentation

- [OpenRouter Catalog & Completions API](openrouter-catalog-api.md)
- [Existing Codebase Patterns to Reuse](existing-resolver-patterns.md)
- codebase-discovery.json (gap-006, existing_patterns)
- original-requirements.md (Proposed Solution #9, AC8)
