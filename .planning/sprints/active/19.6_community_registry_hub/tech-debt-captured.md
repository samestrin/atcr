# Tech Debt Captured — Sprint 19.6 (Community Registry Hub)

Deferred findings surfaced during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded into the adversarial TD stream, tagged `SOURCE=execute-sprint`).

## TD-001 — Darwin PersonasDir migration / AC1 back-compat (MEDIUM)
**Origin:** Phase 1, task 1.LAST gate review, 2026-07-07
**File:** internal/personas/paths.go:19
**Issue:** Phase 3 redefines `PersonasDir()` from `os.UserConfigDir()/atcr/personas` to `filepath.Dir(DefaultRegistryPath())/personas` so installs land on the resolver chain. On darwin this moves the effective dir from `~/Library/Application Support/atcr/personas` to `~/.config/atcr/personas`, silently orphaning any persona already installed at the old path (dropped from `personas list`/`remove`/`upgrade`), which conflicts with AC1's "backward compatibility for existing on-disk personas."
**Why accepted:** Live install against the real repo is not exercised until `samestrin/atcr` is public, so no real darwin user has personas at the old path yet; the reconciliation itself (dir on the chain) is the AC 01-06 priority and cannot be deferred.
**Fix in:** Phase 3 (task 3.14) or bounded fast-follow — either add a one-time migration (move/symlink old darwin dir to the new one on first run) or explicitly record in-code that no pre-public-launch back-compat is owed.

## TD-002 — Persona prompt length cap couples to executor system_prompt cap (LOW)
**Origin:** Phase 1, task 1.LAST gate review, 2026-07-07
**File:** internal/registry/config.go:83
**Issue:** The untrusted reviewer-persona length cap is specified to "mirror" `MaxExecutorSystemPromptLen` (=4096), an unrelated Epic 7.0.1 executor/autofix limit. If Phase 3 hardcodes the literal 4096 (as the design note's §6 restates it), the two caps drift silently when the executor cap is later tuned.
**Why accepted:** The value 4096 is correct today and shared by intent; this is a maintainability/coupling concern, not a correctness bug.
**Fix in:** Phase 3 — reference the constant directly, or define a dedicated `MaxPersonaPromptLen` aliased to `MaxExecutorSystemPromptLen`, and document that the shared value is intentional rather than coincidental.

## TD-003 — AC7 index gate does not couple entry `path` to `name` (MEDIUM)
**Origin:** Phase 2, task 2.LAST gate review, 2026-07-07
**File:** internal/personas/search_test.go (verifyCommunityIndex)
**Issue:** The AC7 gate cross-checks each index entry's `provider`/`model` against the YAML at the entry's `path`, but never asserts `path == name + ".yaml"`. Runtime install/fetch resolves a persona by its `name` (`FetchPersonaYAML` → `<name>.yaml`), so a Phase 5 authoring divergence where `name` and `path` disagree could pass the gate while `atcr personas install <name>` fetches a different or nonexistent YAML — the gate's consistency guarantee is weaker than it reads. Phase 4 search (filters on the index's own Provider/Model) is unaffected.
**Why accepted:** The community index is empty `[]` through Phase 2 (no entries to diverge yet), and the exact community layout / name↔path convention is a Phase 5 sizing decision (flat vs. nested) not yet locked — baking a strict `path == name + ".yaml"` assertion now risks being too rigid for the chosen layout.
**Fix in:** Phase 5 (tasks 5.13–5.15, community index registration) — once the layout is fixed, add a gate assertion deriving `path` from `name` (or vice-versa), or explicitly document that name/path coupling is out of the gate's scope and is verified per-persona during authoring.

## TD-004 — `atcr init` exists-gate message names config.yaml regardless of which target exists (LOW)
**Origin:** Phase 3, task 3.11.A adversarial review, 2026-07-07
**File:** cmd/atcr/init.go:171-176
**Issue:** The "config already exists at .atcr/config.yaml — use --force" gate trips on ANY existing init target (including a surviving `.atcr/personas/*.md` when config.yaml was deleted), so the message can be factually wrong about which file blocks the run. The gate's protective intent is correct (an existing file forces an explicit --force), and under --force the persona is now always preserved, so this is message-accuracy only.
**Why accepted:** Phase 3 Clarification Q1 explicitly LOCKED the exists-gate as "untouched"; `TestInit_AlreadyExists` pins the current message text. Rewording the message (or narrowing the trip to config.yaml) contradicts the lock and breaks a pinned test — out of scope for the preserve change.
**Fix in:** A follow-up that revisits the gate wording with the lock lifted — name the actual existing target(s), or narrow the trip to config.yaml since personas are always preserved.

## TD-005 — `personas list --scores` shows two tiers while plain `list` shows three (LOW)
**Origin:** Phase 3, task 3.11.A adversarial review, 2026-07-07
**File:** cmd/atcr/personas.go (listPersonasWithScores → ListWithScores)
**Issue:** Plain `personas list` now routes through `ListTiers` (project > community > built-in), but the `--scores` variant still calls `List` (built-in + community only). After `init` scaffolds the 9 editable `.md` files into `.atcr/personas`, plain list labels them Source `project` while `--scores` labels the same rows `built-in` — the two variants disagree on the Source column.
**Why accepted:** AC 01-05's source-labeling scenarios target the plain `list`; `--scores` is a separate corroboration feature. Threading `projectDir` through `ListWithScores` (a signature change + new caller wiring) is beyond the AC 01-05 preserve/label scope and was deferred to keep task 3.10–3.12 focused.
**Fix in:** A follow-up that adds a tier-aware `ListWithScores` (or an internal `ListTiersWithScores`) so both list variants agree on the Source column.

## TD-006 — Bundle install does not deliver co-located custom prompts (LOW)
**Origin:** Phase 3, task 3.14.A adversarial review, 2026-07-07
**File:** internal/personas/bundles.go (InstallBundle → Install per member)
**Issue:** Task 3.15 routed the single-persona `personas install` and init/quickstart fetch-and-pin through `InstallUnit`, which delivers the complete unit (`<name>.yaml` + co-located `<name>.md`) and applies the C3 fetched-prompt guardrails. `InstallBundle` still calls the older `Install` (YAML only), so a persona installed via a bundle gets no custom prompt and skips the install-time guardrail (C2 unit-completeness gap). Security is still upheld by the resolve-time guard, but bundle-installed personas silently lose their model-tuned prompt.
**Why accepted:** AC 01-06 / C2 scope this sprint's unit-delivery work to the single-install and init/quickstart paths; reworking `InstallBundle` (per-member outcomes, AlreadyPresent semantics) to route through `InstallUnit` is a larger change deferred to keep task 3.13–3.15 focused on the resolver + guardrails.
**Fix in:** A follow-up that routes `InstallBundle` members through `InstallUnit` (or shares its fetch+validate+paired-write logic) so bundle installs deliver complete units uniformly.

## TD-007 — `personas upgrade` refreshes only the YAML, not the co-located custom prompt (MEDIUM)
**Origin:** Phase 3, task 3.LAST gate review, 2026-07-07
**File:** internal/personas/upgrade.go:27-92
**Issue:** `Upgrade` re-fetches and overwrites only `<name>.yaml`; it never re-fetches, refreshes, or removes the co-located `<name>.md`. Upgrading a persona whose custom prompt changed keeps serving the OLD `.md` into the LLM review; upgrading a binding-only→custom-prompt version never installs the new `.md` — reintroducing the stale-prompt hazard `InstallUnit` was built to prevent, and diverging upgraded units from the C2 unit model.
**Why accepted:** AC 01-02 explicitly scoped `upgrade` to "advance the recorded pin with no logic change required" this sprint; extending it to the paired-unit refresh (fetch `.md`, write-or-remove the sibling, apply the C3 guardrails) is the same unit-consistency work deferred for bundles (TD-006) and belongs with it.
**Fix in:** A follow-up that routes `Upgrade` through the same unit logic as `InstallUnit` (re-fetch the `.md`, write-or-remove the sibling, enforce the fetched-prompt guardrails) alongside TD-006.

## TD-008 — Persona table renderers emit untrusted index fields raw to the terminal (MEDIUM)
**Origin:** Phase 4, task 4.11.A adversarial review, 2026-07-07
**File:** cmd/atcr/personas.go (renderPersonaSearch / writeTable)
**Issue:** `renderPersonaSearch` writes community-index string fields (Name/Version/Provider/Model/Description) raw into tab-delimited `text/tabwriter` rows with no control-character sanitization. Because the index is community-submitted (C3 untrusted-input surface), a field value containing a literal newline forges a spurious data row, a literal tab shifts subsequent cells into the wrong column, and ANSI/control bytes pass straight through to the terminal (output spoofing). Phase 4's new Provider/Model columns widen a surface that is partly pre-existing (Name/Description were already rendered raw here and in `renderPersonaList`). Format-string abuse is NOT possible — all values are `%s` args, never part of the format string.
**Why accepted:** Rated MEDIUM (below this sprint's CRITICAL/HIGH inline-fix bar); largely pre-existing rather than net-new; the live install/fetch against the real community index is not exercised until `samestrin/atcr` is public, so no untrusted index is served in CI today. AC 03-04 scopes Phase 4 to column presence + the "-" placeholder, not terminal-injection hardening.
**Fix in:** A follow-up that strips or escapes control characters (tab, newline, CR, ESC/ANSI) from each cell value in the shared `writeTable` path (covering `renderPersonaSearch` and `renderPersonaList` uniformly), or rejects index entries whose fields contain them at decode time.
