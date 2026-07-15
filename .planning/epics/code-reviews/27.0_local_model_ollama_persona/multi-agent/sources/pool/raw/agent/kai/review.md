 Let me analyze the diff carefully against the sprint plan and look for architectural/design issues.

Files changed in the diff (relevant to scope):
1. `.planning/epics/active/27.0_local_model_ollama_persona.md` -> deleted
2. `.planning/epics/completed/27.0_local_model_ollama_persona.md` -> created (the completed plan file with refinements)
3. `CHANGELOG.md` -> added 27.0.0 entry
4. `docs/personas-install.md` -> added Local/Privacy-First section
5. `docs/providers.md` -> added note about local api_key_env placeholder
6. `internal/personas/drift.go` -> added local provider slug exemption logic
7. `internal/personas/drift_test.go` -> added test for local provider slug exemption
8. `personas/community/gerald.md` -> new persona prompt
9. `personas/community/gerald.yaml` -> new persona metadata
10. `personas/community/index.json` -> added three new personas
11. `personas/community/liam.md` -> new persona prompt
12. `personas/community/liam.yaml` -> new persona metadata
13. `personas/community/orson.md` -> new persona prompt
14. `personas/community/orson.yaml` -> new persona metadata
15. `personas/community/testdata/gerald_fixture.patch` -> fixture test data
16. `personas/community/testdata/liam_fixture.patch` -> fixture test data
17. `personas/community/testdata/orson_fixture.patch` -> fixture test data
18. `personas/community_test.go` -> updated tests and roster

Let me look for architectural/design issues:

1. **Boundary violations**: 
- The `internal/personas/drift.go` changes introduce `localProviderSlugPrefix` as a constant and `isLocalProviderSlug` function. This is within the personas package, seems appropriate.
- The `personas/community_test.go` changes modify tests to allow `local` provider. This is test code, boundary seems fine.

2. **Coupling**:
- In `personas/community_test.go`, the `communityPersonas` slice is a hardcoded roster that now includes 3 new entries. This was explicitly acknowledged in the plan as something that needed editing. The test `TestCommunityIndex_Registration` cross-checks the embedded-file count against the static slice length. This is a known coupling issue (static roster vs dynamic embeds) but it seems to be pre-existing and acknowledged in the plan clarifications. The plan says "both TestCommunityAccessors and TestCommunityIndex_Registration cross-check counts against the static slice." This is a maintained invariant, not new coupling introduced by this change.
- The `localProviderSlugPrefix` constant in drift.go couples the drift checking logic to the specific `local/` prefix convention. But since community personas use `model: local/...`, and discovery keys on model, this seems like a necessary coupling. However, I should check if there's a better way - could this be more general? The alias handling already exists as a precedent.

3. **Contract design**:
- In `personas/community_test.go:371` (now around line ~374 after changes), the assertion was changed from `require.Equalf(t, "openrouter", e.Provider, ...)` to `require.Containsf(t, []string{"openrouter", "local"}, e.Provider, ...)`. This is an allowlist of exactly 2 strings. The plan mentions "broadening the provider allowlist beyond {openrouter, local}" is out of scope. This seems like a narrowly scoped change.
- Wait, looking more carefully at `CHANGELOG.md` line 7: "`local` as a sanctioned provider routing key for community personas, alongside `openrouter`." And the code in `community_test.go` uses `[]string{"openrouter", "local"}`. Is there any issue with this hardcoded allowlist approach? It's a test assertion, not production code. In production, the `drift.go` code handles `local/` prefix specially.
- The `personas/community/index.json` entries now include `"provider": "local"`. Is there any production code that might break because it expected `provider` to always be `openrouter`? The plan says no other test needs editing. But I should look for production code that might check provider. The drift.go is one place (production code). Are there other places?

4. **Duplication of responsibility**:
- The `localProviderSlugPrefix = "local/"` constant in `drift.go` and the allowlist check in `community_test.go` represent two different ways of validating the same concept: that local providers are allowed. In `drift.go`, it checks the `model` field prefix (`local/`). In the test, it checks the `provider` field (`local`). This is actually correct because they check different things (model namespace vs provider routing key). The model is `local/gemma3-27b` while provider is `local`.
- However, is there duplication between the logic in `drift.go` and what might exist elsewhere? The drift.go adds `isLocalProviderSlug` function. Could this have been handled by extending the `aliasSlugSet` concept? Not really, because local slugs aren't a fixed set.

5. **Extensibility traps / Hardcoded assumptions**:
- In `personas/community_test.go`, the `communityPersonas` slice requires manual edits. This was pre-existing.
- The test assertion `require.Containsf(t, []string{"openrouter", "local"}, e.Provider, ...)` hardcodes exactly two allowed providers. The plan explicitly states broadening beyond `{openrouter, local}` is out of scope, so this seems intentional. But architecturally, if a future epic wants to add another provider (e.g., "azure", "aws"), they'd need to touch this test again. Is this a design flaw? The comment says "Pin the routing key to the sanctioned allowlist" and mentions "never an arbitrary vendor-named provider." This seems like a deliberate gate.
- In `drift.go`, the `isLocalProviderSlug` function checks for `strings.HasPrefix(slug, "local/")`. This is hardcoded. Is there a more principled way to handle this? Perhaps not without a larger refactor.
- The new personas all have `version: 1.0.0` and the task names in `index.json` are hardcoded: `secrets-review`, `duplication-review`, `invariant-review`. The test `TestCommunityPersonas_DistinctTaskScoping` checks for task duplication. Are these tasks distinct from existing ones? Existing tasks appear to be: `architecture-review`, `correctness-review`, `api-review`, `validation-review`, `concurrency-review`, `resource-review`, `performance-review`, `type-safety-review`, `dependency-review`, `observability-review`. The new ones (`secrets-review`, `duplication-review`, `invariant-review`) are new and don't conflict.

Let me look for more subtle issues:

** Issue 1: `personas/community/index.json` inconsistency**
Looking at the `index.json` diff, the entries are added at the end. But there's something subtle about the task tags. The existing entries use kebab-case with `-review` suffix for tasks. The new entries follow this pattern: `secrets-review`, `duplication-review`, `invariant-review`. Seems consistent.

** Issue 2: docs/personas-install.md**
The docs mention the personas `gerald`, `orson`, `liam` with their models. But it says "The Bound model column is the discovery id... it is not the string your local server answers to. When you wire the persona, set your agent's model to the exact tag you pulled — e.g. `gemma3:27b`, not `local/gemma3-27b`"

This creates a tension: the persona YAML says `model: local/gemma3-27b`, but the user's registry should say `model: gemma3:27b`. The `model` field in the persona YAML is used for discovery (as the test verifies: "discoverable by its bound model"), but the actual runtime model string is what the user configures in their agent's `registry.yaml`.

Wait, looking at `personas/community_test.go` test `TestCommunityIndex_Registration`, it checks that the index provider/model equals the persona YAML. So `model: local/gemma3-27b` is in the YAML. The docs say the user should set their agent config to `gemma3:27b`. Is there any architectural issue here? The persona YAML model is used for discovery/catalog, while the actual provider config uses the real Ollama tag. This seems like it could be confusing but it's by design (the plan mentions "The persona ships the tuned prompt; you supply the model binding").

But wait - is there a risk that somewhere in production code, the persona's `model` field is used to actually call the LLM, rather than just for discovery? If the persona metadata says `model: local/gemma3-27b`, and the system tries to use that as the model parameter in the API request to Ollama, it would fail because Ollama expects `gemma3:27b`. However, looking at the existing architecture, the user configures their agent in `registry.yaml` with `model: gemma3:27b`, and that agent config is what drives the actual API call. The persona YAML model is just metadata/indexing. So this is probably fine.

** Issue 3: `gerald.md` template**
The `gerald.md` file has `{{if .ToolsEnabled}}## Tool-Assisted Review...` but the `.ToolsEnabled` check. This is consistent with other templates.

** Issue 4: Test changes**
In `personas/community_test.go`, line 175 before change (now around there) had `TestCommunityAccessors` with count assertions. The diff doesn't show changes to `TestCommunityAccessors`. Wait, looking at the diff, I don't see `TestCommunityAccessors` changed. The plan says count assertions need editing. But the diff only shows changes to `TestCommunityIndex_Registration` (changing the provider assertion) and updating the comment on `TestCommunityPersonas_Differentiation` from C(10,2)=45 to C(13,2)=78, and changes to the `communityPersonas` slice.

Wait, the plan says "Both `TestCommunityAccessors` and `TestCommunityIndex_Registration` cross-check counts against the static slice. One-line addition per new persona." But I don't see `TestCommunityAccessors` in the diff! Let me re-check the diff carefully.

Looking at the diff for `personas/community_test.go`:
- Lines 118-129: `communityPersonas` slice updated with 3 new entries.
- Lines 249-274: `TestCommunityPersonas_SlugConsistency` unchanged except for context lines? Actually the diff shows no changes to the test body, just context.
- Line 273-274: Comment updated from "C(10,2)=45 pairs" to "C(13,2)=78 pairs".
- Lines 329-374: `TestCommunityIndex_Registration` updated with provider allowlist assertion.

I don't see `TestCommunityAccessors` in the diff. Is it in unchanged lines? If so, and it has a hardcoded count assertion, then that test would fail with 13 personas instead of 10. Let me check if `TestCommunityAccessors` exists in the unchanged context.

The diff shows:
```
@@ -323,78 +326,82 @@ func TestCommunityPersonas_DistinctTaskScoping(t *testing.T) {
```

This means lines before 323 are unchanged context. If `TestCommunityAccessors` was around line 175 as mentioned in the plan, it might be in the unchanged part. And if that test has a hardcoded count of 10 personas, then adding 3 new personas without updating that count would break the test.

This is a potential finding! The plan explicitly states: "Both `TestCommunityAccessors` and `TestCommunityIndex_Registration` cross-check counts against the static slice. One-line addition per new persona." The diff shows changes to `TestCommunityIndex_Registration` (which cross-checks `len(communityPersonas)`), but `TestCommunityAccessors` is not visible in the diff at all. If it's in unchanged code and was missed, this is a correctness issue.

However, I need to be careful about the SCOPE CONSTRAINT. The rule says: "a finding whose FILE:LINE falls outside the changed lines will be discarded before it reaches the report when grounding is active (git-range reviews) — it is not enough for the code to merely be visible in the surrounding context. If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope so the reconciler annotates rather than discards it. Stay on the diff."

So if `TestCommunityAccessors` is unchanged and has a hardcoded count, I should flag it with CATEGORY `out-of-scope` if it's pre-existing, or if it's broken by this change but the lines are unchanged, I still need to flag it on the diff lines that relate to it. Actually, wait. If this change adds 3 personas but doesn't update `TestCommunityAccessors`, the bug is that the diff is incomplete. But the finding would point at unchanged lines. I should see if I can tie it to a changed line.

Actually, looking more carefully at the diff: the `communityPersonas` slice is changed (3 new lines added). The `TestCommunityAccessors` test likely iterates over `communityPersonas` and might not need changing if it just iterates. But the plan explicitly said `TestCommunityAccessors:175` has count assertions. If the count assertion is `require.Len(t, personas, 10)`, that would be on an unchanged line. But is that line visible in the surrounding context? Let me look at what lines are visible.

The diff hunk for community_test.go starts at line 114 with `type communityPersona struct {` and shows the `communityPersonas` var block through line ~130. Then it skips to line 243 for `TestCommunityPersonas_SlugConsistency`, then line 273 for the differentiation test, then line 323 for `TestCommunityPersonas_DistinctTaskScoping`, then `TestCommunityIndex_Registration`.

Lines between ~131 and ~242 are not shown at all. If `TestCommunityAccessors` is in that gap, it's not visible. So I cannot flag it with line numbers because I don't see it.

But wait, the plan says "Key Decisions: ... **Roster edit required** (corrects the plan's "no Go code changes needed"): the hard-coded `communityPersonas` slice in `personas/community_test.go:118-129` plus count assertions (`TestCommunityAccessors:175`, `TestCommunityIndex_Registration:339`) cross-check the embedded-file count against the slice length." This is the plan's own clarification that these need editing. The diff shows the roster edited and `TestCommunityIndex_Registration` edited. It doesn't show `TestCommunityAccessors:175` edited.

Is `TestCommunityAccessors` in the diff? Let me check for the string in the diff text... I see `TestCommunityPersonas_SlugConsistency`, `TestCommunityPersonas_Differentiation`, `TestCommunityPersonas_DistinctTaskScoping`, `TestCommunityIndex_Registration`, `TestCommunityPersonas_PromptStructure`. I do not see `TestCommunityAccessors` anywhere in the diff output.

This means `TestCommunityAccessors` is unchanged. If it has a hardcoded count `10`, the change breaks it. This is a bug in the change itself (the change is incomplete because it doesn't update this test). However, the file path is `personas/community_test.go` and the line is outside the changed lines. According to the guidelines, if I must flag a genuine pre-existing issue in unchanged code, give it CATEGORY `out-of-scope`. But this isn't a pre-existing issue—it's a bug introduced by this change because the change adds entries without updating the count assertion. The issue is that the diff is incomplete.

Hmm, the instructions say: "Review only the changed regions. The payload shows you the change in context, but a finding whose FILE:LINE falls outside the changed lines will be discarded before it reaches the report when grounding is active (git-range reviews) — it is not enough for the code to merely be visible in the surrounding context. If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope so the reconciler annotates rather than discards it. Stay on the diff."

So any finding I emit must have a FILE:LINE that falls within the changed lines. If I point at `personas/community_test.go:175` but the diff doesn't include line 175, it gets discarded. I could point at a changed line that causes the issue, like the added roster entries at lines 125-127, and say the fix is to also update `TestCommunityAccessors`. But the CATEGORY would need to be something like `correctness` and I'd describe the problem as "Added personas to roster without updating count assertions in tests." But wait, I don't see `TestCommunityAccessors` in the diff, so I don't know for sure it exists or what line it's on. I only know from the sprint plan text that it exists at line 175.

Actually, looking at the diff again, I see the `communityPersonas` slice is at lines 118-130 (after change). The test `TestCommunityIndex_Registration` is shown and contains:
```go
require.Lenf(t, byStem, len(communityPersonas),
    "expected exactly one index entry per authored persona (%d)", len(communityPersonas))
```
This is dynamically sized (uses `len(communityPersonas)`), so it doesn't need changing when entries are added. The plan mentioned `TestCommunityIndex_Registration:339` but the current code uses `len(communityPersonas)`. So maybe this was already dynamic.

What about `TestCommunityAccessors:175`? I can't see it. The plan says it cross-checks embedded-file count against slice length. If it does something like `require.Len(t, files, len(communityPersonas))`, then adding to `communityPersonas` would still be fine because it uses `len`. But if it hardcodes a number, it's broken.

Given I cannot see the line, and the instructions say findings on unchanged lines are discarded, I should focus on what I can see.

Let me look for actual architectural issues in the visible code.

**Visible issue: `localProviderSlugPrefix` logic in drift.go**
The function `isLocalProviderSlug` checks `strings.HasPrefix(slug, localProviderSlugPrefix)`. This is in `internal/personas/drift.go`. Is this a clean design?

Looking at the code:
```go
const localProviderSlugPrefix = "local/"

func isLocalProviderSlug(slug string) bool {
	return strings.HasPrefix(slug, localProviderSlugPrefix)
}
```
This mirrors the alias slug handling above it. However, there's a subtle coupling issue: the `local/` prefix is being treated specially in the drift checker. But what if other parts of the system need to know about this prefix? For instance, are there other places that validate whether a model slug is resolvable? The drift checker is just one consumer. If the local provider is a first-class concept, should this prefix be defined centrally rather than in `drift.go`?

Wait, looking more carefully: the slug is the `Model` field from `InstalledLock`. The `local/` prefix is part of the model identifier in the persona YAML. The drift checker is exempting it from catalog lookup. Is this the right place for this knowledge? The drift package is about comparing installed locks against a catalog. It makes sense that it knows about exemptions. But the constant "local/" might be better placed in a shared location if it's used elsewhere. I don't see it used elsewhere in the diff.

Actually, looking at the test `TestCommunityIndex_Registration`, it checks the provider field (`e.Provider`) against `["openrouter", "local"]`. The drift check looks at the model slug prefix `local/`. These two checks (`provider == "local"` vs `strings.HasPrefix(model, "local/")`) are related but separate. Is there a risk they get out of sync? For instance, could someone create a persona with `provider: local` but `model: ollama/gemma3-27b` (without `local/` prefix)? The test would pass (provider is in allowlist), but the drift checker wouldn't exempt it (model doesn't start with `local/`). Conversely, a persona could have `provider: openrouter` but `model: local/gemma3-27b` — the drift checker would exempt it but the test would fail.

This is a contract design issue: there are two separate validation mechanisms checking different fields for the "local" concept, and nothing ensures they stay consistent. The persona YAML has both `provider` and `model`, and the system checks one for routing and another for drift. If the convention is that local personas MUST have both `provider: local` AND `model: local/...`, then there should be a single source of truth or at least a validation that enforces this coupling. But the allowlist in the test only checks `provider`, and the drift exemption only checks `model` prefix. They could diverge.

Is this a real problem? The test `TestCommunityIndex_Registration` does check that index provider/model equal persona YAML. It doesn't enforce that `provider: local` implies `model: local/...` or vice versa. The drift.go exemption relies on the model prefix, not the provider. If a future developer adds a local persona with `provider: local` but forgets the `local/` model prefix (or uses a different model naming scheme), the drift checker would report it missing. This is a potential extensibility trap.

But looking at the sprint plan: "New personas use `provider: local`, `model: local/<ollama-model>`". So this coupling is an intended convention. Is it enforced anywhere? Not in the visible code.

Wait, looking at `personas/community_test.go`, the provider allowlist check was added:
```go
require.Containsf(t, []string{"openrouter", "local"}, e.Provider, ...)
```
And the model check still does:
```go
require.Containsf(t, strings.ToLower(e.Model), p.VendorToken, ...)
```

For local personas, the vendor tokens are "gemma", "qwen", "llama". The models are `local/gemma3-27b`, `local/qwen3-30b-a3b`, `local/llama3.3-70b`. The lowercase model contains the vendor token. That passes.

But there's no test enforcing that `provider: local` implies `model: local/...`. This seems like a gap that could cause drift false positives if not followed. Is it worth flagging?

Actually, looking at the existing code, the test `TestCommunityModel` at line 484 checks `/`-namespacing. The plan says `:484 passes unmodified with a local/<model> string.` So it likely enforces that models contain a `/`. But it doesn't enforce the `local/` prefix specifically.

**Another issue: `gerald.md` vendor-guidance comment format**
The `gerald.md` file starts with:
```html
<!-- vendor-guidance: Gemma — model card and prompting guidance (...), https://ai.google.dev/gemma/docs -->
```
The test `TestCommunityPersonas_PromptStructure` enforces exactly one non-empty vendor-guidance citation. This seems fine.

**Another issue: test comment mismatch**
In `personas/community_test.go`, the comment above `TestCommunityPersonas_Differentiation` was updated:
```go
// generic list restated ten times. Runs over all C(13,2)=78 pairs.
```
But wait, the comment above still says "ten times" but then says C(13,2)=78. That's inconsistent. Let me check:
Original comment (visible in unchanged context in the diff? No, the diff shows the changed line). The diff shows:
```go
-// generic list restated ten times. Runs over all C(10,2)=45 pairs.
+// generic list restated ten times. Runs over all C(13,2)=78 pairs.
```
The `ten times` should have been updated to `thirteen times` or removed. This is a LOW issue (comment drift).

**Another issue: `CHANGELOG.md` task naming**
In `CHANGELOG.md`, it says:
```
- Three local reviewer personas tuned for hardware tiers: `gerald` (32 GB dense, secrets & data-egress), `orson` (32 GB long-context, duplication & repo-wide redundancy), and `liam` (64 GB+ heavyweight, invariants & state-consistency).
```
But in `index.json`, the task for `orson` is `duplication-review`, and for `liam` is `invariant-review`. The changelog says "duplication & repo-wide redundancy" and "invariants & state-consistency". That's descriptive prose, not the task tag. Not an issue.

**Another issue: `docs/personas-install.md` agent configuration example**
The docs show:
```yaml
agents:
  gerald:
    provider: local
    model: gemma3:27b                     # the tag you pulled with `ollama pull`
    role: reviewer
```
Wait, the persona YAML says `role: reviewer`, and the user agent config also says `role: reviewer`. Is `role` required in both? Or does the persona YAML's role get used? In the existing architecture, the agent config typically specifies `role`, but the persona YAML also has it. Looking at the existing community personas in index.json, they don't have `role` in the index (the index doesn't include role). The YAML files do have `role: reviewer`.

But looking at the new YAML files:
```yaml
name: gerald
version: 1.0.0
description: ...
provider: local
model: local/gemma3-27b
persona: gerald
role: reviewer
```
This matches the existing YAML pattern. But wait, looking at the `index.json` diff, the new entries don't have a `role` field. Neither do the existing entries. The `index.json` structure for existing entries is:
```json
{
  "name": "anthony",
  "version": "1.0.0",
  "description": "...",
  "path": "anthony.yaml",
  "provider": "openrouter",
  "model": "anthropic/claude-opus-4.8",
  "tasks": ["architecture-review"],
  "tags": [...]
}
```
No `role` field. So `role` is in the YAML but not in the index. That's pre-existing.

**Potential MEDIUM issue: allowlist as slice literal in test**
In `TestCommunityIndex_Registration`:
```go
require.Containsf(t, []string{"openrouter", "local"}, e.Provider, ...)
```
This creates a new slice on every iteration of the loop (over 13 personas). That's 13 heap allocations in a test. Not a big deal for a test. More importantly, it's an inline allowlist. If this were extracted as a package-level var `sanctionedCommunityProviders`, it would be clearer. But this is test code, so it's LOW at best.

**Potential MEDIUM issue: `personas/community_test.go` doesn't enforce local prefix consistency**
As I thought above: there's no validation that `provider: local` implies `model: local/...`. The drift checker assumes this convention. If someone adds a persona with `provider: local` and `model: gemma3:27b` (no prefix), the test passes but drift reports missing. This is an extensibility trap. But is it in changed lines? The changed lines are the provider allowlist assertion (where this could be caught) and the roster. I can flag this on the provider assertion line.

Actually, looking at the test, after checking provider is in allowlist, it checks:
```go
require.Containsf(t, strings.ToLower(e.Model), p.VendorToken, ...)
```
This checks vendor token is in model. For `local` personas, vendor tokens are `gemma`, `qwen`, `llama`. So `local/gemma3-27b` contains `gemma`. But there's no check that local provider models start with `local/`. Could we add one? The fix would be: if provider is `local`, require model has `local/` prefix.

But then again, the drift checker only checks model prefix. Should the drift checker instead check provider type? No, because `InstalledLock` is what goes into `CheckDrift`, and `InstalledLock` comes from installation, which records the model slug. The drift checker doesn't see the provider field of the persona YAML; it only sees the lock. And the lock contains the resolved model string. So the drift checker MUST key off the model slug. The question is: should the test enforce that local provider personas use `local/` model prefix so that the drift checker exemption works?

This seems like a genuine gap. Let me suggest it.

**Potential MEDIUM issue: `isLocalProviderSlug` vs alias handling redundancy**
In `drift.go`:
```go
if _, isAlias := aliasSlugSet[slug]; isAlias {
    continue
}
if isLocalProviderSlug(slug) {
    continue
}
```
Two separate `if` statements with `continue`. This is fine. But really, this is building a pattern of "exempt slug sets". A more extensible design would be to have a map or list of exemption predicates, rather than hardcoding N separate if statements. Given there are only two so far (alias and local), this is probably fine. But if the roadmap already contradicts this (the sprint plan says "broadening the provider allowlist beyond {openrouter, local}" is out of scope), then maybe not. However, the plan does say "The openrouter lock was designed as an extendable allowlist." For drift.go, it's a different story.

Actually, looking at the plan's scope: "IN scope: ... the one-assertion contract relaxation; the drift-check exemption". So the drift-check exemption IS in scope. Is the implementation extensible? It's a hardcoded prefix check. If another provider like "synthetic" needs the same treatment, it would need another hardcoded prefix. This is a minor extensibility trap but probably LOW.

**Potential CRITICAL/HIGH issue: `personas/community_test.go:338` count assertion**
Wait, looking at `TestCommunityIndex_Registration`, the assertion is:
```go
require.Lenf(t, byStem, len(communityPersonas),
    "expected exactly one index entry per authored persona (%d)", len(communityPersonas))
```
This is dynamic. But what about `TestCommunityAccessors`? I found it! Looking at the unchanged context in the diff around line 114, I see:
```go
// communityPersonas is the authoritative roster of the model-indexed library.
```
And the diff skips to line 243. I don't see `TestCommunityAccessors`. But I do see the plan explicitly mentions it at line 175. If the diff doesn't show changes to it, and the plan says it needs editing, this suggests the diff might be incomplete. But the instructions say "Review only the changed regions." I cannot flag line 175 because it's not in the changed lines. However, I can note that the `communityPersonas` slice was changed without updating the accessor count, but since I can't see the accessor test, I should be careful.

Wait, let me re-read the diff header for `personas/community_test.go`:
```
@@ -114,18 +114,21 @@ type communityPersona struct {
```
This shows lines 114-132 (before) and 114-135 (after) for the first hunk.
Then:
```
@@ -243,30 +246,30 @@ func TestCommunityPersonas_SlugConsistency(t *testing.T) {
```
This shows lines 243-274 (before) and 246-276 (after).
Then:
```
@@ -323,78 +326,82 @@ func TestCommunityPersonas_DistinctTaskScoping(t *testing.T) {
```
Wait, the diff header shows `@@ -323,78 +326,82`. This means starting at line 323 in the original, 78 lines are shown. That would cover lines 323-400. It includes `TestCommunityPersonas_DistinctTaskScoping` and `TestCommunityIndex_Registration`. It does not include anything around line 175.

If `TestCommunityAccessors` is at line 175, it's not in the diff. And if it wasn't changed, then either:
1. The test doesn't need changing (maybe it's already dynamic), OR
2. The diff is incomplete and the test will break.

Given the plan explicitly says "Both `TestCommunityAccessors` and `TestCommunityIndex_Registration` cross-check counts against the static slice. One-line addition per new persona. ... Confirmed — required, not optional." This strongly suggests `TestCommunityAccessors` DOES need changing and wasn't shown in the diff. But wait, the diff might have been truncated in the payload, or maybe the test was updated in a part of the file not shown? Let me check the file path again - no, the diff is complete for that file.

Actually, looking at the payload, the diff for `personas/community_test.go` has exactly three hunks. If `TestCommunityAccessors` existed and was modified, it would appear. Since it doesn't, and the plan says it needs editing, this is a concern. But I cannot file a finding on an unchanged line. I could file a finding on the changed `communityPersonas` lines (118-127) saying "Roster added but static count assertions in TestCommunityAccessors (line ~175) not updated; tests will fail." The line would be in the changed region. The category would be `correctness`.

But is it true? I don't see `TestCommunityAccessors` in the diff. The plan says it's there. I think it's a safe bet based on the sprint plan's own explicit clarification. However, as a reviewer, I should only flag what I can verify. The sprint plan is given as context. It says "Roster edit required ... Both `TestCommunityAccessors` and `TestCommunityIndex_Registration` cross-check counts against the static slice." This means the authors KNEW they needed to edit it. If the diff doesn't show it, they may have missed it. I'll flag it on the roster change lines.

Let me think about whether there's any other issue.

**Issue: `index.json` is hand-edited and may drift**
The `index.json` is a manually maintained JSON file. New personas were added to it. This is pre-existing practice. Not an issue introduced by this change.

**Issue: fixture patches**
The fixture patches are new files. They look like valid patches. The test `community_fixture_test.go` will pick them up automatically via embed. No issues there.

**Issue: `docs/providers.md` note placement**
The added note is inside the `## Direct connection` section, after the example YAML block. It says:
```markdown
> **Local endpoints still need an `api_key_env` placeholder.** ...
```
This is fine.

**Issue: `personas/community/gerald.md` line 10**
"You are the zero-egress gate: privacy-conscious teams run you precisely because the review itself sends nothing to a remote API."

This is persona prompt text. Not an architecture issue.

**Issue: `CHANGELOG.md` format**
The CHANGELOG adds a new section. Not an architecture issue.

**Issue: `personas/community/index.json` trailing comma/newlines**
The JSON diff shows new entries added at the end. JSON doesn't allow trailing commas. The diff shows no trailing comma after the last entry. The previous last entry `glenna` now has a comma, and the new last entry `liam` does not. This is correct JSON.

**Issue: drift.go `CheckDrift` signature**
The function takes `[]InstalledLock` and `[]CatalogModel`. The local slug exemption happens on the lock's Model field. There's no way for `CheckDrift` to know the provider; it only knows the model slug. This is correct because locks record resolved models.

Wait, but if a user installs a local persona, the `InstalledLock` records the model as `local/gemma3-27b`. Then when `CheckDrift` runs, it sees this model and exempts it. That's the intended behavior.

Is there any coupling issue with the `local/` prefix being hardcoded in `drift.go`? The alias handling uses a set built from `aliasTable`. The local handling uses a string prefix. They are different because local models are a namespace, not a fixed set. This seems fine.

**Actual finding: Comment not updated in `community_test.go`**
In `personas/community_test.go`, the diff shows:
```go
-// generic list restated ten times. Runs over all C(10,2)=45 pairs.
+// generic list restated ten times. Runs over all C(13,2)=78 pairs.
```
The text "ten times" was not updated to reflect 13 personas. This is a LOW finding on that changed line.

**Actual finding: Missing `local/` prefix validation**
In `TestCommunityIndex_Registration`, after the provider allowlist check, there should be a check that if `e.Provider == "local"`, then `e.Model` starts with `local/`. Without this, the drift exemption may not apply. But wait, the drift exemption applies to installed locks, not the index directly. Still, the convention is that local personas have `provider: local` and `model: local/...`. If they diverge, drift behavior is unpredictable.

Actually, looking at the test:
```go
require.Containsf(t, strings.ToLower(e.Model), p.VendorToken,
    "model %q must carry vendor token %q", e.Model, p.VendorToken)
```
For local personas, the vendor token is in the part after `local/`. This still passes. But there's no explicit `local/` prefix validation. I'll flag this on the provider allowlist line as a MEDIUM extensibility trap.

Wait, but is the `local/` prefix required by the architecture, or just a convention? The drift.go code treats ANY slug starting with `local/` as a local provider slug, regardless of the actual provider field. And the test treats ANY index entry with `provider: local` as valid. If these two don't stay in sync (