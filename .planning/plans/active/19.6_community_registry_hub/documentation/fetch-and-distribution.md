# Community Persona Fetch & Distribution (net/http + YAML) [CRITICAL]

## Overview

The community-persona channel makes `samestrin/atcr` the canonical source of reviewer personas by fetching them at runtime rather than compiling them into the binary. The provider client for this fetch path is a plain `http.Client` POSTing/GETting over `net/http` — no provider SDKs — following the same discipline used for the OpenAI-compatible chat client: build requests with `http.NewRequestWithContext(ctx, ...)` so timeouts and cancellation propagate, and keep the transport timeout (`http.Client{Timeout: ...}`) distinct from any higher-level operation deadline (> Source: [standard-library.md]). Persona index and YAML payloads returned by the fetch are decoded with `encoding/json`/`gopkg.in/yaml.v3`, tolerating unknown fields by default so the registry can evolve without breaking older clients (> Source: [standard-library.md], [yaml-v3.md]).

In this repo, the fetch path is already structured around two choke points that this plan's AC1 (fetch-and-pin) and AC6 (mock-registry end-to-end tests) extend rather than replace: `internal/personas/client.go` defines the minimal `HTTPClient` interface (`Do(req) (*http.Response, error)`) satisfied by `*http.Client`, and owns `FetchIndex`/`FetchPersonaYAML`; `cmd/atcr/personas.go` holds a package-level `personasClient` variable that tests swap for an `httptest.NewServer`-backed client, keeping fetch tests free of live network calls in CI. Repointing the default fetch source is a one-constant change: `BaseURL()` in `client.go` returns `strings.TrimSpace(os.Getenv(envPersonasURL))` when set, else falls back to the `RegistryBaseURL` constant — every caller (install/search/list/upgrade/init/quickstart) picks up the new default automatically without per-caller edits.

For distribution, persona index entries and registry metadata are read with `gopkg.in/yaml.v3`, the de-facto standard YAML library in the Go ecosystem and already integrated into atcr's config-parsing conventions (> Source: [yaml-v3.md]). Because `PersonaIndexEntry` already ignores unknown JSON fields on decode, adding structured `Provider`/`Model`/`Tasks`/`Tags` metadata to the struct — the model-indexed discovery feature this plan requires — is additive and backward-compatible with personas fetched from older or newer registry snapshots.

## Key Concepts

- **Context-scoped HTTP requests.** Build requests with `http.NewRequestWithContext(ctx, ...)` so per-agent and global timeouts can cancel an in-flight persona fetch.
  > Source: [standard-library.md]

- **Separate transport timeout from operation timeout.** `http.Client{Timeout: 120 * time.Second}` guards a single HTTP exchange; longer-lived deadlines belong in the context, not the client.
  > Source: [standard-library.md]

- **Bearer auth pattern.** `req.Header.Set("Authorization", "Bearer "+key)`, with the key resolved from the provider's `api_key_env` at invoke time — relevant if a future registry endpoint requires authenticated fetch.
  > Source: [standard-library.md]

- **Retry policy for transient failures.** Retry on 429/500/502/503/504 with ~500ms initial delay and 1.5x backoff so retries don't exhaust the caller's timeout; other 4xx responses fail immediately. This must be preserved for any registry fetch retries.
  > Source: [standard-library.md]

- **Unknown-field tolerance by default.** `encoding/json` decodes into a minimal envelope and ignores unknown fields by default, which is exactly why `PersonaIndexEntry` can gain `Provider`/`Model`/`Tasks`/`Tags` fields without breaking existing decode paths — old and new registry index entries both decode cleanly.
  > Source: [standard-library.md]; codebase pattern in `internal/personas/client.go`

- **Always close and drain response bodies.** `defer resp.Body.Close()`, and drain bodies on retry paths, so connections can be reused across repeated registry fetches (index, then per-persona YAML).
  > Source: [standard-library.md]

- **Injectable HTTPClient + httptest for zero-network CI.** `internal/personas/client.go`'s `HTTPClient` interface and `cmd/atcr/personas.go`'s package-level `personasClient` variable let AC6's mock-registry end-to-end tests point `ATCR_PERSONAS_URL` at an `httptest.NewServer`, satisfying the same interface `*http.Client` does.
  > Source: codebase pattern (`internal/personas/client.go`, `cmd/atcr/personas.go`)

- **Single-constant registry repoint.** `BaseURL()`'s env-var-wins-else-`RegistryBaseURL` pattern means changing the canonical community registry source is a one-line constant edit in `client.go`, picked up by every caller (install/search/list/upgrade/init/quickstart).
  > Source: codebase pattern (`internal/personas/client.go`)

- **YAML decoding for registry/persona payloads.** `yaml.Unmarshal(data, &cfg)` converts whole-document YAML into typed Go structs, with `*yaml.TypeError` partially populating other fields on a type mismatch — relevant when a persona YAML file has a malformed field.
  > Source: [yaml-v3.md]

- **Strict decoding recommended for hand-edited registry files.** `Decoder.KnownFields(true)` turns unknown YAML keys into errors at load time (e.g. catching `serial_agnets:` typos), which the yaml-v3 doc explicitly calls out as the right mode for atcr config and registry parsing.
  > Source: [yaml-v3.md]

- **Struct tags govern marshal/unmarshal shape.** `` `yaml:"key,flag1,flag2"` `` with flags like `omitempty`, `flow`, `inline`, and `-`; only exported fields are marshaled — directly applicable when extending `PersonaIndexEntry` with new `Provider`/`Model`/`Tasks`/`Tags` fields.
  > Source: [yaml-v3.md]

- **YAML 1.1 boolean caveat.** `yes/no`/`on/off` decode as bools into typed bool fields — worth knowing if any persona/registry metadata field is boolean and registry YAML is hand-edited by community contributors.
  > Source: [yaml-v3.md]

## Code Examples

```go
import "gopkg.in/yaml.v3"

type Config struct {
    Agents       []string `yaml:"agents"`
    SerialAgents []string `yaml:"serial_agents,omitempty"`
    TimeoutSecs  int      `yaml:"timeout_seconds,omitempty"`
}

var cfg Config
if err := yaml.Unmarshal(data, &cfg); err != nil { ... }
out, err := yaml.Marshal(cfg)
```
> Source: [yaml-v3.md]

```bash
go get gopkg.in/yaml.v3
```
> Source: [yaml-v3.md]

## Fetch-and-Pin, Offline Behavior, and Backward Compatibility (AC1)

AC1 changes `atcr init` and `atcr quickstart` from copying embedded built-in `.md` templates into `.atcr/personas/` to fetching model-indexed community personas from the repointed `RegistryBaseURL` and pinning the version found in each fetched persona YAML's `version` field.

- **Current state:** `cmd/atcr/init.go:runInit` and `cmd/atcr/quickstart.go:runQuickstart` write built-in persona `.md` files via `builtins.Names()` and `builtins.Get()`; the effective canonical source of personas is the compiled binary.
- **Target state:** `init`/`quickstart` call `internal/personas.Install` (or a bundle install) against `commpersonas.BaseURL()`, using the fetched persona YAML's `version` as the pinned version recorded in `~/.config/atcr/personas/`. `atcr personas upgrade` later compares that installed version to the remote version and advances the pin.
- **Version pin source:** the installed YAML's `version` field (catalog metadata, ignored by the registry schema). `internal/personas/list.go` already reads this value for display and `internal/personas/upgrade.go` already compares it; no new storage mechanism is needed.

### `--offline` stub behavior

Because `atcr review` itself requires network access to model endpoints, the onboarding path can treat offline fetch failure as a clear error rather than silently degrading. The recommended AC1 behavior is:

1. Add an `--offline` flag to `atcr init` and `atcr quickstart` that skips the community fetch entirely and falls back to the existing embedded built-in personas (preserving today's behavior).
2. Without `--offline`, if the community fetch fails (repo not yet public, no network, etc.), print a descriptive error and exit non-zero.
3. Document the fallback explicitly so users know `atcr init --offline` gives them the built-in panel when they cannot reach the registry.

> Source: original-requirements.md (AC1), plan.md (Theme 1)

### Backward compatibility for existing `.atcr/personas/` workspaces

Existing workspaces contain editable `.md` copies of built-in personas under `.atcr/personas/`. AC1 must not clobber these during an in-place `init`/`quickstart` rerun:

- `runInit` already refuses to overwrite any existing target unless `--force` is set (`cmd/atcr/init.go:76-78`). Preserve that contract.
- For `--force`, do not blindly replace `.md` files with fetched YAML; warn that existing `.md` files are being kept and install any missing community personas alongside them.
- If the workspace was created before AC1, `atcr personas list` still shows the built-in personas because the source is `built-in`; the newly installed community personas appear with `Source: community` and their pinned `version`.

> Source: codebase-discovery.json integration gap ("init/quickstart fetch-and-pin vs. existing .atcr/personas/ .md files")

## Index.json Generation Workflow

`personas/community/index.json` is the canonical discovery index for the model-aware search path. No tooling currently generates it from the per-persona YAML files. Because AC2 and AC7 depend on the index's `provider`/`model`/`tasks`/`tags` matching the YAML's agent/catalog metadata, drift between the two would break model-aware discovery and fixture enforcement.

Decision required and recommended approaches:

| Approach | How it works | Trade-offs |
|---|---|---|
| Hand-maintained | Authors edit `index.json` when they add or change a persona | Simplest to start; prone to drift |
| Generation script | A Makefile target or small Go tool reads `personas/community/*.yaml` and writes `index.json` | Eliminates manual drift; must be run before commit |
| CI validation | A CI job fails if `index.json` does not match a regenerated version | Catches drift without blocking local edits; pairs well with either approach above |

Recommended: combine a generation script with CI validation so `index.json` is always reproducible from the YAML sources.

> Source: codebase-discovery.json integration gap ("Index.json generation workflow")

## Quick Reference

| Concern | Approach | Source |
|---|---|---|
| Request construction | `http.NewRequestWithContext(ctx, ...)` | standard-library.md |
| Transport vs operation timeout | `http.Client{Timeout: 120 * time.Second}` (transport) vs context deadline (operation) | standard-library.md |
| Auth header | `req.Header.Set("Authorization", "Bearer "+key)` | standard-library.md |
| Retry policy | Retry 429/500/502/503/504, ~500ms initial delay, 1.5x backoff; other 4xx fail immediately | standard-library.md |
| JSON decode tolerance | Unknown fields ignored by default — additive struct changes are safe | standard-library.md |
| Body handling | `defer resp.Body.Close()`; drain on retry paths | standard-library.md |
| YAML whole-document decode | `yaml.Unmarshal(data, &out)` / `yaml.Marshal(in)` | yaml-v3.md |
| YAML strict mode | `NewDecoder(r).KnownFields(true)` — recommended for registry.yaml | yaml-v3.md |
| YAML struct tags | `` `yaml:"key,omitempty"` ``, `flow`, `inline`, `-` | yaml-v3.md |
| Default fetch source | `RegistryBaseURL` constant, overridable via `ATCR_PERSONAS_URL` env var, resolved in `BaseURL()` | codebase pattern (`internal/personas/client.go`) |
| Zero-network fetch tests | `HTTPClient` interface + `personasClient` var swapped for `httptest.NewServer` | codebase pattern (`internal/personas/client.go`, `cmd/atcr/personas.go`) |
| Backward-compatible index metadata | `PersonaIndexEntry` ignores unknown JSON fields — safe to add `Provider`/`Model`/`Tasks`/`Tags` | codebase pattern (`internal/personas/client.go`) |
| Fetch-and-pin in init/quickstart | Install via `internal/personas.Install` using the fetched YAML's `version` field as the pin | plan.md (AC1) |
| `--offline` fallback | Skip community fetch; fall back to embedded built-in personas | original-requirements.md (AC1) |
| Existing `.md` workspace compat | Keep user-edited `.md` files; install fetched YAML only when no conflict exists or with `--force` | codebase-discovery.json |
| Index generation | Generate/validate `personas/community/index.json` from YAML sources to avoid provider/model drift | codebase-discovery.json |

## Related Documentation

- [../../../../specifications/packages/standard-library.md](../../../../specifications/packages/standard-library.md)
- [../../../../specifications/packages/yaml-v3.md](../../../../specifications/packages/yaml-v3.md)
