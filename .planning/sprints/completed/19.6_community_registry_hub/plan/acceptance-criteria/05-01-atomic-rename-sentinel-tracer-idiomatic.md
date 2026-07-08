# Acceptance Criteria: Atomic Rename of `sentinel`/`tracer`/`idiomatic` to `sasha`/`penny`/`ingrid`

**Related User Story:** [05: Human-Names Migration for Built-in Stragglers](../user-stories/05-human-names-migration-for-built-in-stragglers.md)
**Design References:** [human-names-migration.md](../documentation/human-names-migration.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go embedded-file package (`personas/`) + build-time `go:embed` guard | Rename is a filesystem + registration change, no new runtime code |
| Test Framework | Go `testing` package (`go test ./personas/...`, `go test ./internal/personas/...`) | Init-time panic guard in `personas/personas.go` fails fast on mismatch |
| Key Dependencies | stdlib `embed`; existing `builtins.Get`/`builtins.Fixture` in `personas/personas.go` | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `personas/personas.go` (names slice ~line 20, embedded file guard) — modify: replace `"sentinel"`, `"tracer"`, `"idiomatic"` with `"sasha"`, `"penny"`, `"ingrid"` in the `names` slice; update package/fixture doc comments.

> **Registration path is BUILT-IN, not community-only (LOCKED per C2).** `sasha`/`penny`/`ingrid` are migrated stragglers that STAY embedded built-ins (model-agnostic lens prompts), registered ONLY in `personas/personas.go`'s `names` slice. They do NOT get a community YAML nor a `personas/community/index.json` entry — those are for the model-bound library personas (Stories 2–4), not these built-ins. There is no "if built-in / if community-only" fork here: the path is unconditionally the `names`-slice built-in path.
- `personas/sentinel.md` → `personas/sasha.md` — rename: preserve the security/OWASP review lens.
- `personas/tracer.md` → `personas/penny.md` — rename: preserve the performance/N+1/latency review lens.
- `personas/testdata/sentinel_fixture.patch` → `personas/testdata/sasha_fixture.patch` — rename.
- `personas/testdata/tracer_fixture.patch` → `personas/testdata/penny_fixture.patch` — rename.
- `personas/personas_test.go` — modify: update `names` slice assertions, `HasFixture` loop personas, and fixture-path test calls from `sentinel`/`tracer` to `sasha`/`penny`.
- `docs/personas-authoring.md` / `docs/personas-install.md` — modify: update worked examples and built-in persona lists (see AC 05-04).


## Happy Path Scenarios
**Scenario 1: `sasha` resolves after rename**
- **Given** `personas/sentinel.md` has been renamed to `personas/sasha.md` and `personas/personas.go`'s `names` slice contains `"sasha"` in place of `"sentinel"`
- **When** `builtins.Get("sasha")` is called
- **Then** it returns the security/OWASP persona template content (the same lens `sentinel.md` carried), with no error

**Scenario 2: `penny` resolves after rename**
- **Given** `personas/tracer.md` has been renamed to `personas/penny.md` and registered as `"penny"`
- **When** `builtins.Get("penny")` is called
- **Then** it returns the performance/N+1/latency persona template content, with no error

**Scenario 3: Fixture lookup follows the renamed slug**
- **Given** `personas/testdata/sentinel_fixture.patch` has been renamed to `personas/testdata/sasha_fixture.patch`
- **When** `builtins.Fixture("sasha")` is called
- **Then** it returns the fixture patch content that previously backed `sentinel`, with no error

## Edge Cases
**Edge Case 1: Old slug is fully unregistered, not aliased**
- **Given** the rename has landed (names slice updated, files renamed)
- **When** `builtins.Get("sentinel")` is called
- **Then** it returns the "unknown persona" error (`isRegistered` returns false) — `sentinel` is not kept as a deprecated alias resolving to the same content as `sasha`

**Edge Case 2: Partial rename trips the init-time panic guard**
- **Given** `personas/personas.go`'s `names` slice has been updated to include `"sasha"` but `personas/sasha.md` has not yet been created (or the old `sentinel.md` was deleted without adding the new file)
- **When** the `personas` package's `init()` runs (e.g., any `atcr` binary invocation)
- **Then** the embedded-file-count/name mismatch check panics with a message listing the mismatched file set, preventing a partially-migrated binary from starting silently

**Edge Case 3: All three renames land as one atomic change (three-part, not four)**
- **Given** `sentinel`→`sasha`, `tracer`→`penny`, and `idiomatic`→`ingrid` are migrated together
- **When** the change is committed
- **Then** the atomic unit is exactly THREE parts per persona for the built-in path — the embedded template `.md`, the `<slug>_fixture.patch`, and the `names`-slice registration — all updated in the same commit, with no intermediate state where some personas are renamed and others still carry role-based slugs. There is deliberately NO fourth part: these stragglers stay embedded model-agnostic built-ins, so there is no `<slug>.yaml` metadata file and no `personas/community/index.json` entry to keep in sync. The init-time panic-guard atomicity guarantee therefore covers only the embedded-file set (`.md` templates + fixtures) against the `names` slice — it does not consult any YAML/index metadata, and none is expected.

## Error Conditions
**Error Scenario 1: Stale `names` slice after file rename**
- **Given** `personas/sentinel.md` is renamed to `personas/sasha.md` on disk but `personas/personas.go`'s `names` slice still lists `"sentinel"`
- **When** the package initializes
- **Then** `expectedEmbeddedFiles()` expects `sentinel.md` (absent) and does not expect `sasha.md` (present) — the set-mismatch panic fires with both discrepancies visible in the panic message
- HTTP status / error code: N/A (Go `panic`, process exits non-zero)

**Error Scenario 2: Mismatched fixture filename**
- **Given** `personas/testdata/sentinel_fixture.patch` was not renamed but `builtins.Fixture("sasha")` is called
- **When** the fixture lookup executes
- **Then** it returns `no embedded fixture for persona "sasha"` — the fixture path is name-derived (`testdata/<name>_fixture.patch`), so an un-renamed fixture file is invisible to the new slug rather than silently matching

## Performance Requirements
- **Response Time:** No runtime performance impact — renames only change embedded filenames and a compile-time string slice; `Get`/`Fixture` remain O(1) map/file lookups.
- **Throughput:** N/A (build-time embed, not a request path).

## Security Considerations
- **Authentication/Authorization:** N/A — no new trust boundary; personas remain compiled into the binary.
- **Input Validation:** N/A — this AC covers static registration data, not user-supplied input.

## Test Implementation Guidance
**Test Type:** UNIT (`personas/personas_test.go`, `internal/personas/personas_test.go`)
**Test Data Requirements:** Renamed `.md` templates and `_fixture.patch` files must exist under `personas/` and `personas/testdata/` before tests run; no synthetic fixtures needed since the migration reuses existing fixture content under new filenames
**Mock/Stub Requirements:** None — `go:embed` reads real files at build time; tests exercise the actual embedded FS, matching the existing test pattern in `personas_test.go`

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `personas/personas.go`'s `names` slice contains `sasha`, `penny` in place of `sentinel`, `tracer` (idiomatic/ingrid covered by AC 05-02)
- [ ] `personas/sentinel.md`/`personas/tracer.md` and their `testdata/*_fixture.patch` counterparts are renamed, not duplicated (old files do not remain on disk)
- [ ] `builtins.Get("sentinel")` and `builtins.Get("tracer")` return "unknown persona" errors post-migration
- [ ] `go build ./...` succeeds and the package `init()` does not panic

**Manual Review:**
- [ ] Code reviewed and approved
