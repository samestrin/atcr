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

## TD-009 — AC 04-07 Jaccard distinctness threshold (0.85) is loose vs observed (LOW)
**Origin:** Phase 5, task 5.20.A adversarial review, 2026-07-07
**File:** personas/community_test.go (TestCommunityPersonas_Differentiation)
**Issue:** The distinctness gate fails a persona pair only at token-set Jaccard > 0.85 over combined `## Role`+`## Focus`, but the observed max across all 45 pairs is ~0.168 — roughly 5x headroom. The gate therefore only catches near-verbatim duplication; a future "same lens, renamed target" pair sharing ~80% of tokens would still pass under 0.85. The 0.85 value is LOCKED by AC 04-07 for deterministic reproducibility, so it was NOT lowered here.
**Why accepted:** 0.85 is an explicit AC-locked threshold; lowering it unilaterally would contradict the requirement. The current library is comfortably distinct (max 0.168), and a complementary categorical guard (distinct category words + distinct primary task tags) was added this sprint to catch the same-lens-renamed duplication the loose Jaccard alone would miss.
**Fix in:** A future AC 04-07 revision that re-locks the threshold nearer the observed distribution (e.g. 0.35–0.40) or replaces the single Jaccard gate with a lens-keyword-disjointness assertion.

## TD-010 — C3 `{{ }}` guardrail rejects the required template variables, so NO community persona can install or resolve (HIGH)
**Origin:** Phase 6, task 6.7 CLI verification (AC 07-02 Edge Case 1), 2026-07-07
**File:** internal/personas/unit.go:25 / internal/registry/persona.go:142
**Issue:** Both C3 untrusted-prompt guardrails — `validateFetchedPrompt` (install-time, unit.go:25) and `validateCommunityPrompt` (resolve-time, persona.go:142) — reject a fetched community `.md` if it contains ANY `{{` or `}}`, with no allowlist. But every one of the 10 shipped community personas (personas/community/*.md) is a full Go text/template carrying the 7 required variables (`{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`) plus `{{if .ToolsEnabled}}...{{end}}` — required by the authoring contract (docs/personas-authoring.md §2) and rendered by the fixture runner. Verified: `atcr personas install delia` fails with "persona prompt contains template metacharacters ({{ or }})"; the resolve-time guard rejects the same prompt at review time. Net: the model-tuned custom prompt (the epic's core value) can neither install nor resolve — breaking LOCKED Clarification C1 ("custom community prompts MUST resolve") and AC6 (end-to-end install flow). Masked in CI because the community fixture test uses the embedded `builtins.CommunityGet` path (bypassing both guardrails) and the Phase 3 guardrail tests assert the reject-all behavior.
**Why accepted:** Not accepted — surfaced as a HARD STOP at the Phase 6 gated boundary. Deviates from Phase 3 Clarification 3, which specified rejecting `{{ }}` "beyond the known required template variables" (an allowlist the implementation dropped). Fix rewrites a security guardrail across two packages + updates the Phase 3 guardrail tests — Phase 3/7 scope, not Phase 6 docs; the maintainer must adjudicate the guardrail semantics.
**Fix in:** Phase 7 (task 7.1 "custom prompts resolve via ResolvePersona" / 7.2 "AC6 end-to-end") — replace reject-all with an allowlist that permits exactly the known required template actions (`{{.AgentName|.ScopeRule|.FileCount|.BaseRef|.HeadRef|.PayloadMode|.Payload}}`, `{{if .ToolsEnabled}}`, `{{end}}`) and rejects any other `{{ }}`; update unit_test.go / persona_test.go guardrail assertions; add the missing install→resolve integration coverage for a real template-bearing community persona.
**Resolved:** 2026-07-07 — Phase 6 (option 1, maintainer-approved). Shared `registry.ValidateFetchedPersonaPrompt` allowlist added (exact-match on the known persona template actions; any other `{{ }}` or unbalanced brace rejected), used by both the install-time (`internal/personas/unit.go`) and resolve-time (`internal/registry/persona.go`) guards so they cannot drift; Phase 3 reject-all tests updated to reject unknown actions + accept known vars; a guard asserts all 10 embedded community personas pass; CLI verified `search→install→list→test delia` end-to-end. Phase 7 tasks 7.1/7.2 now add the remaining cross-story integration assertions on top of this fix.

## TD-011 — init/quickstart fetch-and-pin roster is disjoint from the shipped community index (HIGH)
**Origin:** Phase 7, task 7.LAST sprint-exit gate review, 2026-07-08
**File:** cmd/atcr/init.go:47 / cmd/atcr/quickstart.go:102
**Issue:** `init`/`quickstart` fetch-and-pin the roster `builtins.Names()` (the 9 model-agnostic built-ins: bruce/greta/kai/mira/dax/otto/sasha/penny/ingrid), but the shipped `personas/community/index.json` publishes only the 10 model-indexed library personas (anthony…glenna) — a fully disjoint set. Against the real default URL once `samestrin/atcr` is public, online `init`/`quickstart` therefore pin ZERO community personas and emit one `persona "X" not found in community index — skipping` warning per built-in (9 stderr lines). AC 01-02's design intent (Related Files L15/L23/L32: "replace the embedded built-in copy loop with fetch-and-pin", "roster personas fetched by init/quickstart") is unmet in the shipped content, so AC1's fetch-and-pin delivers no value there. Masked in CI because init_test/quickstart_test seed MOCK indexes whose entries match the roster names, so the disjoint-set reality is never exercised.
**Why accepted:** Not reachable pre-public-launch (repo private → default URL 404s → `init` online hard-fails cleanly with the `--offline` hint; the built-in review panel still works via the embedded copy `runInit` writes to `.atcr/personas/`). The fetch-and-pin MECHANISM is proven; the gap is content/wiring. The correct resolution is a product decision that reconciles AC1 (fetch-and-pin) with AC5 (frontier personas opt-in, out of the default funnel) and C2 (built-ins stay embedded, model-agnostic) — out of Phase 7's validation-only scope. Maintainer-adjudicated 2026-07-08: defer to Epic 19.7, which rebuilds init/quickstart model enablement.
**Fix in:** Epic 19.7 (Live Model Resolution) AC7 — rebuild init/quickstart model enablement so online `init`/`quickstart` deliver a working, non-noisy persona set: either publish the built-in lenses into the community channel behind a model-agnostic gate carve-out (keeps C2), or align the roster with what the index publishes and drop the misleading skip-warning.

## TD-012 — Install strict-decode narrowing now applies to bundle members, untested (MEDIUM)
**Origin:** Phase 7, task 7.LAST sprint-exit gate review, 2026-07-08
**File:** internal/personas/install.go:32
**Issue:** `Install` validates fetched YAML via `ValidateCommunityPersonaYAML` (strict `KnownFields(true)` over the community-persona schema), and `InstallBundle` (bundles.go:152) calls `Install` for every bundle member. So a bundle-member YAML carrying any key outside `AgentConfig ∪ {name,version,description,tasks,tags,fixture,path}` — which would have installed under the prior non-strict `ValidateAgentYAML` — now fails to install. This is a backward-compat narrowing on the bundle path with no bundle-specific strict-decode test.
**Why accepted:** Rated MEDIUM (below the sprint's CRITICAL/HIGH inline-fix bar). The two shipped bundles (bundle/django, bundle/go-production) carry only recognized keys and still install; the live install against the real repo is not exercised until `samestrin/atcr` is public. Confirming/relaxing the bundle decode policy is a scoping decision, not a correctness break today.
**Fix in:** A follow-up that either (a) confirms strict decode is intended for bundles and adds a bundle strict-decode reject test, or (b) keeps bundles on `ValidateAgentYAML` and reserves strict decode for the single-unit community path. Pairs naturally with TD-006 (route InstallBundle members through InstallUnit).

## TD-013 — AC6 e2e fixture step runs the embedded persona, not the installed unit (LOW)
**Origin:** Phase 7, task 7.LAST sprint-exit gate review, 2026-07-08
**File:** internal/personas/e2e_discover_test.go:80
**Issue:** The 7.2 discover-by-model flow's final "test (fixture)" step calls `TemplateFixtureRunner{}.RunFixture("delia")`, which resolves the EMBEDDED community persona via `builtins.CommunityGet` — not the unit just installed to `destDir` in the install step. So the on-disk installed artifact's fixture is never exercised; the "end-to-end" holds only because embedded == served content. Documented in the test comment.
**Why accepted:** On-disk (installed) community fixture support is the reserved, not-yet-implemented `TemplateFixtureRunner.PersonasDir` seam (internal/personas/test.go:33); pointing the fixture step at `destDir` is not possible until that lands. The embedded path is exactly what `atcr personas test` uses today, so the step is faithful to the shipped CLI behavior.
**Fix in:** When on-disk community fixture support lands (the reserved `PersonasDir` seam), point the e2e fixture step at the installed unit so the full install→test path is exercised on disk.
