# Tech Debt Captured — Sprint 1.0_atcr_core

## TD-001 — CI workflow guard inconsistency and redundant vet step (LOW)
**Origin:** Phase 1, task 1.3 adversarial review, 2026-06-10
**File:** .github/workflows/ci.yml:34
**Issue:** go.mod existence guards are dead code now that go.mod is committed, and two guard styles coexist (`[ -f go.mod ]` vs `hashFiles`). The standalone `go vet` step duplicates golangci-lint's built-in govet.
**Why accepted:** Cosmetic CI cleanup; behavior is correct, just redundant.
**Fix in:** Phase 5 docs/CI pass — drop the guards and the standalone vet step.

## TD-002 — coverage.out generated in CI but never consumed (LOW)
**Origin:** Phase 1, task 1.3 adversarial review, 2026-06-10
**File:** .github/workflows/ci.yml:49
**Issue:** CI generates a coverage profile but never uploads, thresholds, or reports it, implying coverage enforcement that does not exist.
**Why accepted:** Coverage gate (≥70%) is enforced locally in DoD validation; CI threshold wiring is a nice-to-have.
**Fix in:** Phase 5 — add a coverage threshold check or artifact upload to ci.yml.

## TD-004 — payload_mode / fail_on never enum-validated or case-normalized at any tier (MEDIUM)
**Origin:** Phase 1, task 1.30 gate review, 2026-06-10
**File:** internal/registry/precedence.go:53
**Issue:** `payload_mode: bogus` and `fail_on: bogus` resolve into Settings silently; docs use lowercase `--fail-on high` while the embedded default is `HIGH`. Downstream phases could each invent divergent validation.
**Mitigation this sprint:** Tasks 2.25 (payload-mode enum validation, lowercase-only) and 3.33 (fail-on threshold validated against enum before any I/O) are already planned to land exactly this validation centrally.
**Fix in:** Phase 2 task 2.25 and Phase 3 task 3.33.

## TD-005 — personas package exports raw strings without a template-data contract (MEDIUM)
**Origin:** Phase 1, task 1.30 gate review, 2026-06-10
**File:** personas/personas.go:5
**Issue:** Persona templates use 7 variables ({{.AgentName}}, {{.ScopeRule}}, {{.FileCount}}, {{.BaseRef}}, {{.HeadRef}}, {{.PayloadMode}}, {{.Payload}}) but no exported struct anchors them; renderer and templates can drift.
**Mitigation this sprint:** Task 2.33 (payload template vars) defines the typed template-data struct with missingkey=error; task 2.45 adds tests that all six embedded personas render against it.
**Fix in:** Phase 2 tasks 2.33 / 2.45.
**Resolved:** 2026-06-10 — `payload.PayloadContext` anchors all 7 vars (rendered with missingkey=error); `TestEmbeddedPersonasRenderAgainstContext` renders all six personas + _base against it, so renderer and templates can no longer drift.

## TD-006 — atcr init writes explicit defaults that mask the registry settings tier (LOW, kept by design)
**Origin:** Phase 1, task 1.30 gate review, 2026-06-10
**File:** internal/registry/project.go (DefaultProjectConfigYAML)
**Issue:** The generated config bakes payload_mode/timeout_secs/fail_on explicitly, so registry-tier user defaults never apply to initialized projects unless the user removes those lines.
**Why accepted:** AC 02-01 mandates the generated config contain all five top-level keys with these exact defaults. Users who want registry-tier inheritance can delete the lines; docs/registry.md will note this in the Phase 5 rewrite.
**Fix in:** Phase 5 docs — document the inheritance behavior in docs/registry.md.

## TD-003 — --format flag accepts any string at flag layer (LOW)
**Origin:** Phase 1, task 1.3 adversarial review, 2026-06-10
**File:** cmd/atcr/report.go:15
**Issue:** `--format` help promises md/json/checklist but no enum validation exists yet, so invalid values would only fail inside the future handler.
**Mitigation this sprint:** Task 3.37 (report renderers) implements invalid-format errors as part of its AC; this marker tracks that the flag-layer validation must land there.
**Fix in:** Phase 3, task 3.37 — typed enum value or PreRunE validation mapping to exit 2.

## TD-007 — merge-commit base uses first parent only, undocumented for non-2-parent merges (LOW)
**Origin:** Phase 2, task 2.3 adversarial review, 2026-06-10
**File:** internal/gitrange/resolver.go:99
**Issue:** `--merge-commit SHA` resolves base as `SHA^` (first parent). For an octopus merge, or when the user wants the merged-in branch's actual fork point, this can produce a surprisingly large range, and the behavior is not documented in the flag help.
**Why accepted:** `base = SHA^` is the AC-mandated decision-tree behavior (carried over verbatim); the common 2-parent merge case is correct. Refining for octopus merges is a v2 concern.
**Fix in:** Phase 5 docs — note the first-parent assumption in docs and `--merge-commit` flag help.

## TD-008 — resolveRef conflates a hard git failure with an invalid ref (LOW)
**Origin:** Phase 2, task 2.3 adversarial review, 2026-06-10
**File:** internal/gitrange/resolver.go:177
**Issue:** `resolveRef` treats any non-empty error OR empty stdout as `ErrInvalidRef`. A genuine git failure (corrupt object, I/O error) on `rev-parse --verify` would be mislabeled "does not resolve to a commit" rather than surfaced as an infrastructure error.
**Why accepted:** With `--verify --quiet` the dominant failure mode is a non-existent ref, which the AC requires be reported as an invalid-ref error; the mislabel only occurs on rare repo corruption.
**Fix in:** Phase 3+ — distinguish `err != nil` (wrap raw git error) from `out == "" && err == nil` (true invalid ref).

## TD-009 — files-mode sentinels are spoofable by file content (MEDIUM)
**Origin:** Phase 2, task 2.23 adversarial review, 2026-06-10
**File:** internal/payload/builder.go:144
**Issue:** `renderWithSentinels` emits head content verbatim. A changed file whose source legitimately or maliciously contains a line equal to `>>> CHANGED LINES n-m` or `<<< END CHANGED` injects fake changed-region markers into the reviewer payload, letting content spoof or hide marked regions.
**Why accepted:** Very low likelihood in real source; files mode is an opt-in audit mode, and personas are instructed to treat marked regions as guidance not ground truth. Out of scope for the v1 payload builders AC.
**Fix in:** Phase 3+ — neutralize content lines matching a sentinel (prefix-quote) or use a per-run nonce in the sentinel.

## TD-010 — blocks fallback and binary detection swallow genuine git errors (LOW)
**Origin:** Phase 2, task 2.19 adversarial review, 2026-06-10
**File:** internal/payload/diff.go:122
**Issue:** `functionContextFile` maps every git error to ok=false (fallback) and `isBinary` maps every git error to false. A genuine I/O failure is therefore indistinguishable from the legitimate zero-hunks / non-binary case. In practice the blocks fallback (`contextFile`) would re-surface a real error, and `isBinary` only runs on already-validated changed paths.
**Why accepted:** The AC mandates fallback when function-context "exits nonzero OR produces zero hunks"; the swallow only masks rare infrastructure failures on already-valid paths.
**Fix in:** Phase 3+ — inspect exit status to separate non-fatal (no diff) from fatal git errors.

## TD-011 — per-file git fan-out spawns N×4-5 processes (LOW)
**Origin:** Phase 2, task 2.19 adversarial review, 2026-06-10
**File:** internal/payload/builder.go:114
**Issue:** blocks/files modes invoke up to 4-5 git processes per changed file (numstat, function-context, context, show, unified=0). On large changesets this is a meaningful process-spawn cost.
**Why accepted:** Meets the <2s/<100-file perf target; correctness-first for v1.
**Fix in:** v2 — batch classification (`--numstat`/`--name-status` once) and split a single diff per file.

## TD-012 — payload-mode enum duplicated across registry and payload with no drift guard (LOW)
**Origin:** Phase 2, task 2.27 adversarial review, 2026-06-10
**File:** internal/registry/payload.go:9
**Issue:** The frozen {diff,blocks,files} enum is hand-duplicated in `registry.validPayloadModes` and `payload.validModes` because the package boundary forbids `registry` importing `payload`. No automated test asserts the two sets stay in sync; adding a v2 mode to one and not the other would mis-validate at one tier yet pass all current tests.
**Why accepted:** The enum is frozen for v1; drift can only occur with a deliberate future edit. A cross-package guard test needs a package that may import both (fanout/mcp), which do not exist until Phase 3/4.
**Fix in:** Phase 3/4 — add an enum-parity test from a package permitted to import both registry and payload (e.g. fanout or an e2e test package).

## TD-013 — byte-budget size summation has no int64 overflow guard (LOW)
**Origin:** Phase 2, task 2.31 adversarial review, 2026-06-10
**File:** internal/payload/budget.go:46
**Issue:** `total += e.Size` could wrap negative for pathological/huge sizes, making `total <= budget` true and skipping truncation (a silent over-budget payload). Negative `FileEntry.Size` values are also not rejected.
**Why accepted:** Sizes are real file byte counts (<2GB each) summed over <100 files; overflow is unreachable with realistic inputs. Correctness for normal inputs is fully tested (including duplicate paths and zero-size files).
**Fix in:** v2 — reject negative sizes and detect `total > math.MaxInt64 - e.Size` before adding.

## TD-014 — findings header match is loose and control chars beyond CR/LF pass through (LOW)
**Origin:** Phase 2, task 2.39 adversarial review, 2026-06-10
**File:** internal/stream/parser.go:97
**Issue:** (1) `# atcr-findings/v1x`, `v10`, `v1.2` all match the `# atcr-findings/` prefix and are reported as `ErrUnknownVersion` rather than a clean version-token comparison, so a consumer cannot distinguish a well-formed-but-unsupported version from a garbage header. (2) `escapeField` now neutralizes pipes and CR/LF, but other control bytes (NUL, etc.) still pass through into the wire contract.
**Why accepted:** Reporting any non-v1 `atcr-findings/*` header as unknown-version is a reasonable v1 classification; control bytes other than newlines do not occur in real findings text (severity-prefixed lines from LLMs). The structural defects (newline split, comma-forged reviewers, trailing-pipe drop) were all fixed inline in 2.40.
**Fix in:** v2 — parse the version token exactly; optionally strip remaining control characters in escapeField.

## TD-015 — base_url with embedded credentials could leak via wrapped transport errors (LOW)
**Origin:** Phase 2, task 2.43 adversarial review, 2026-06-10
**File:** internal/llmclient/client.go:121
**Issue:** If a provider base_url embedded userinfo (`https://user:pass@host`), a request-creation or transport error wraps the full URL via `%w`, which could surface the credential in logs — the same risk class as the (already-prevented) API-key leak.
**Mitigation this sprint:** The registry loader already rejects any `base_url` containing userinfo at load time (internal/registry/config.go validate), so an embedded-credential URL never reaches the client in practice.
**Fix in:** v2 — defensively strip userinfo (or scrub the URL) before building the request, so the client is safe independent of registry validation.

## TD-016 — engine-appends-REVIEWER rule is convention-only, unenforced by the parser (MEDIUM)
**Origin:** Phase 2, task 2.50 gate review, 2026-06-10
**File:** internal/stream/parser.go:106
**Issue:** Personas are documented to emit 7 columns; the engine appends REVIEWER. But `ParseSource` is lenient (pads short rows), so a misbehaving model that self-emits an 8th column has that value land in `Finding.Reviewer` — forging its own attribution. The contract is convention-only, not enforced at the parser/type layer.
**Mitigation this sprint:** The fan-out engine (Phase 3, task 3.9) is the writer of per-source `findings.txt` and MUST set `Finding.Reviewer` to the agent name itself, ignoring any model-supplied 8th column — this captures that requirement so Phase 3 does not rely on model honesty.
**Fix in:** Phase 3 task 3.9 — engine constructs Reviewer from the agent name; optionally parse model output as a 7-column shape that rejects a populated 8th field.

## TD-017 — fan-out parallel lane has no concurrency cap (MEDIUM)
**Origin:** Phase 3, task 3.3 adversarial review, 2026-06-10
**File:** internal/fanout/engine.go:88
**Issue:** Every non-serial slot spawns its own goroutine with no semaphore or worker-pool bound. A very large roster would fire that many concurrent provider HTTP calls at once, risking 429 storms and socket/FD exhaustion.
**Why accepted:** v1 ships six embedded personas; real rosters are <=~10 agents, comfortably within the AC's "10 concurrent agent calls" target. A concurrency cap adds config surface (max_parallel) not requested in v1.
**Fix in:** v2 — bound the parallel lane with a buffered semaphore channel sized from a new max_parallel setting.

## TD-018 — minor fan-out engine hardening gaps (LOW)
**Origin:** Phase 3, task 3.3 adversarial review, 2026-06-10
**File:** internal/fanout/engine.go
**Issue:** (1) NewEngine(nil) would nil-panic inside invokeAgent rather than failing cleanly; (2) DurationMS is 0 on the ctx-short-circuit path even when real wall-clock elapsed before cancellation; (3) parallel lane passes slots[i] by arg while the serial goroutine closes over slots and indexes directly — divergent styles that invite a future closure-capture bug.
**Why accepted:** All three are latent/defensive — current call sites never pass a nil completer, the short-circuit path only fires on already-expired contexts, and the shared-results writes are race-clean today (verified under -race with mixed lanes). No behavior is wrong for real inputs.
**Fix in:** v2 — add a nil-completer guard, stamp elapsed time on early-return paths, and standardize slot capture to explicit parameters.

## TD-019 — atomic artifact writes are not fsync-durable; pool write is not transactional (LOW)
**Origin:** Phase 3, task 3.11 adversarial review, 2026-06-10
**File:** internal/fanout/status.go:55 (atomicWriteFile), internal/fanout/artifacts.go (WritePool)
**Issue:** atomicWriteFile renames a temp over the target (atomic w.r.t. readers) but never fsyncs the temp or the parent dir, so a power-loss crash between rename and metadata flush could lose the file on some filesystems. Separately, WritePool writes per-agent files then the merged findings.txt/summary.json without a pool-level transaction: an I/O failure mid-run leaves a partially-populated pool (documented behavior, error surfaced).
**Why accepted:** Rename-over-temp gives reader-atomicity and crash-consistency on the common case; full fsync durability is a power-loss concern out of scope for a local review tool. Pool-level transactionality is unnecessary — a mid-run disk failure is a hard error (exit 1) and the preserved partial artifacts aid debugging.
**Fix in:** v2 — fsync temp + parent dir in atomicWriteFile if durability is required; optionally stage the pool in a temp dir and rename it into place for set-atomicity.
