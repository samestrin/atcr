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
