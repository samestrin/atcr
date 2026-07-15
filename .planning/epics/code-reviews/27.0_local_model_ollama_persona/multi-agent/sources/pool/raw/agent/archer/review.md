Let me analyze the diff carefully, focusing on the changed regions and looking for snippet-level bugs.

Let me go through each changed file:

1. **`.planning/epics/active/27.0_local_model_ollama_persona.md`** - deleted, moved to completed. No code.

2. **`.planning/epics/completed/27.0_local_model_ollama_persona.md`** - new file, planning doc. No code bugs.

3. **`CHANGELOG.md`** - changelog text. No code.

4. **`docs/personas-install.md`** - documentation. Let me check the table and code examples.

Looking at the table:
```
| `gerald` | 32 GB dense (file-by-file) | `local/gemma3-27b` | secrets & data-egress |
| `orson` | 32 GB long-context (256k, full-repo) | `local/qwen3-30b-a3b` | duplication & repo-wide redundancy |
| `liam` | 64 GB+ heavyweight (dual-GPU / M4 Pro) | `local/llama3.3-70b` | invariants & state-consistency |
```

Wait, the index.json says `liam` description is "Llama heavyweight invariant and state-consistency reviewer" - singular "invariant". The docs table says "invariants & state-consistency" - plural. Minor inconsistency but not a bug per se.

The yaml says:
```
ollama pull gemma3:27b              # gerald   (32 GB dense)
# ollama pull qwen3:30b-a3b         # orson    (32 GB long-context)
# ollama pull llama3.3:70b          # liam     (64 GB+ heavyweight)
```

And the agent config:
```
agents:
  gerald:
    provider: local
    model: gemma3:27b                     # the tag you pulled with `ollama pull`
    role: reviewer
```

This looks consistent.

5. **`docs/providers.md`** - added a note. No code.

6. **`internal/personas/drift.go`** - Added `localProviderSlugPrefix` and `isLocalProviderSlug` function, and added a check in `CheckDrift`.

Let me look at the drift.go changes more carefully:

```go
const localProviderSlugPrefix = "local/"

func isLocalProviderSlug(slug string) bool {
	return strings.HasPrefix(slug, localProviderSlugPrefix)
}
```

And in CheckDrift:
```go
if _, isAlias := aliasSlugSet[slug]; isAlias {
    continue
}
// A local-provider persona (Epic 27.0) binds a local/<model> slug that a
// user's own endpoint (ollama/llama.cpp/vllm) resolves, never the OpenRouter
// catalog. It is absent from the snapshot by design, so — like an alias slug
// — it must not be reported `missing`.
if isLocalProviderSlug(slug) {
    continue
}
```

This looks correct. The `strings` package is already imported (used elsewhere in the file with `strings.TrimSpace`).

7. **`internal/personas/drift_test.go`** - Added a test. Looks fine.

8. **`personas/community/gerald.md`** - new persona template. Let me check the template tokens.

The template uses: `{{.AgentName}}`, `{{.ScopeRule}}`, `{{if .ToolsEnabled}}`, `{{end}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`.

Let me count the template tokens. The clarifications say "Each `.md` carries all 7 template tokens". Let me check what the 7 tokens are. Looking at other personas would help, but I can see:
1. `{{.AgentName}}`
2. `{{.ScopeRule}}`
3. `{{.ToolsEnabled}}` (in `{{if .ToolsEnabled}}`)
4. `{{.FileCount}}`
5. `{{.BaseRef}}`
6. `{{.HeadRef}}`
7. `{{.PayloadMode}}`
8. `{{.Payload}}`

That's 8 tokens? Or maybe `{{.BaseRef}}` and `{{.HeadRef}}` count as one? Let me not worry about this - the fixture test would catch it.

9. **`personas/community/gerald.yaml`** - new file.

```yaml
name: gerald
version: 1.0.0
description: Gemma secrets and data-egress reviewer (local zero-egress lens)
provider: local
model: local/gemma3-27b
persona: gerald
role: reviewer
```

10. **`personas/community/index.json`** - added three entries. Let me check the index entries match the YAML files.

gerald index:
```json
{
    "name": "gerald",
    "version": "1.0.0",
    "description": "Gemma secrets and data-egress reviewer (local zero-egress lens)",
    "path": "gerald.yaml",
    "provider": "local",
    "model": "local/gemma3-27b",
    "tasks": ["secrets-review"],
    "tags": ["secret", "privacy", "gemma", "local", "tier-32gb-dense"]
}
```

gerald.yaml:
```yaml
name: gerald
version: 1.0.0
description: Gemma secrets and data-egress reviewer (local zero-egress lens)
provider: local
model: local/gemma3-27b
persona: gerald
role: reviewer
```

The index has `tasks` and `tags` which the YAML doesn't have. The test `TestCommunityIndex_Registration` checks:
- `e.Name == p.Slug` ✓
- `e.Path == p.Slug+".yaml"` ✓
- `ym.Provider == e.Provider` ✓
- `ym.Model == e.Model` ✓
- `ym.Description == e.Description` ✓
- `e.Provider` in `["openrouter", "local"]` ✓
- `strings.ToLower(e.Model)` contains `p.VendorToken` - for gerald, vendor token is "gemma", model is "local/gemma3-27b" → "local/gemma3-27b" contains "gemma" ✓
- `e.Tasks` not empty ✓
- `e.Tags` not empty ✓

orson index:
```json
{
    "name": "orson",
    "version": "1.0.0",
    "description": "Qwen long-context duplication and repo-wide redundancy reviewer (local 256k lens)",
    "path": "orson.yaml",
    "provider": "local",
    "model": "local/qwen3-30b-a3b",
    "tasks": ["duplication-review"],
    "tags": ["duplication", "redundancy", "qwen", "local", "tier-32gb-long-context"]
}
```

orson.yaml:
```yaml
name: orson
version: 1.0.0
description: Qwen long-context duplication and repo-wide redundancy reviewer (local 256k lens)
provider: local
model: local/qwen3-30b-a3b
persona: orson
role: reviewer
```

Vendor token for orson is "qwen", model is "local/qwen3-30b-a3b" → contains "qwen" ✓

But wait - there's already a persona `quinn` with vendor token "qwen"! Let me check the test for distinct vendor tokens...

Looking at the roster:
```go
{Slug: "quinn", VendorToken: "qwen", Tier: "open", Category: "type"},
...
{Slug: "orson", VendorToken: "qwen", Tier: "local", Category: "duplication"},
```

Both quinn and orson have `VendorToken: "qwen"`. Is this a problem?

Looking at the test `TestCommunityIndex_Registration`, the "Scenario 3" check:
```go
var found bool
for _, cand := range entries {
    if strings.Contains(strings.ToLower(cand.Model), p.VendorToken) && cand.Name == p.Slug {
        found = true
        break
    }
}
require.Truef(t, found, "persona %q must be discoverable via its model vendor token %q", p.Slug, p.VendorToken)
```

This checks that `cand.Name == p.Slug`, so it's looking for an entry where both the model contains the vendor token AND the name matches. So for orson, it looks for an entry where model contains "qwen" AND name == "orson". The orson entry has model "local/qwen3-30b-a3b" (contains "qwen") and name "orson" → found = true ✓. This passes.

But is there a test that requires unique vendor tokens? Let me look at the test more carefully...

The `TestCommunityPersonas_DistinctTaskScoping` test checks for distinct task scoping. Let me see if there's a uniqueness check on vendor tokens or categories.

Looking at the roster:
```go
{Slug: "gerald", VendorToken: "gemma", Tier: "local", Category: "secret"},
{Slug: "orson", VendorToken: "qwen", Tier: "local", Category: "duplication"},
{Slug: "liam", VendorToken: "llama", Tier: "local", Category: "invariant"},
```

Categories: "secret", "duplication", "invariant" - all distinct ✓

But wait, the clarifications say: "the tests forbid lens/category/task duplication". Let me check if there's a test that checks for distinct categories.

Looking at `TestCommunityPersonas_DistinctTaskScoping`:
```go
func TestCommunityPersonas_DistinctTaskScoping(t *testing.T) {
```

I can't see the full body of this test in the diff, but the name suggests it checks distinct task scoping. The comment on the roster says "The open-model rows are appended in task 5.4."

Let me look at what the test might check. The diff shows:
```
 // TestCommunityPersonas_DistinctTaskScoping covers AC 04-07 Scenario 1 / Error 1: no
 // pair of personas has combined ## Role+## Focus token-set Jaccard above the
 // locked 0.85 threshold, evidencing genuine per-model task scoping rather than one
-// generic list restated ten times. Runs over all C(10,2)=45 pairs.
+// generic list restated ten times. Runs over all C(13,2)=78 pairs.
```

So the test runs Jaccard similarity over all pairs. With 13 personas, that's C(13,2)=78 pairs. The comment was updated from 45 to 78. ✓

Now, the vendor token for orson is "qwen" which duplicates quinn's vendor token. Is there a test that checks for unique vendor tokens? Let me look at the test code visible in the diff...

I don't see a test that explicitly checks for unique vendor tokens. The `TestCommunityIndex_Registration` test checks that each persona is discoverable by its vendor token, but it doesn't check uniqueness.

However, there might be a test outside the diff that checks this. But since I can only review changed regions, let me focus on what's in the diff.

Actually, wait. Let me re-read the scope constraint. I should focus on the changed regions. Let me look at what's changed in `community_test.go`:

The roster is changed (3 new entries added). The comment about C(10,2) is changed to C(13,2). The provider assertion is changed from `require.Equalf(t, "openrouter", e.Provider, ...)` to `require.Containsf(t, []string{"openrouter", "local"}, e.Provider, ...)`.

Now let me think about potential bugs:

1. **Vendor token duplication**: `orson` has `VendorToken: "qwen"`, same as `quinn`. If there's a test that checks for unique vendor tokens, this would fail. But I can't see such a test in the diff. However, the `TestCommunityIndex_Registration` test's "Scenario 3" check for discoverability by vendor token would still pass because it checks `cand.Name == p.Slug`.

Actually, let me think about this more carefully. The test for "Scenario 3" says:
```go
for _, cand := range entries {
    if strings.Contains(strings.ToLower(cand.Model), p.VendorToken) && cand.Name == p.Slug {
        found = true
        break
    }
}
```

For `quinn` (vendor token "qwen"), this looks for an entry where model contains "qwen" AND name == "quinn". The quinn entry has model "qwen/qwen3-coder-plus" (contains "qwen") and name "quinn" → found = true ✓.

For `orson` (vendor token "qwen"), this looks for an entry where model contains "qwen" AND name == "orson". The orson entry has model "local/qwen3-30b-a3b" (contains "qwen") and name "orson" → found = true ✓.

Both pass. No bug here.

2. **Category uniqueness**: Let me check if there's a test for unique categories. The diff shows `TestCommunityPersonas_DistinctTaskScoping` but I can't see its full body. Let me look at what's visible...

Actually, I can see the comment says "no pair of personas has combined ## Role+## Focus token-set Jaccard above the locked 0.85 threshold". This is about text similarity of the prompt templates, not about category uniqueness. So even if two personas have similar categories, as long as their prompt templates are sufficiently different, this test passes.

3. **Let me check the fixture patches more carefully.**

gerald_fixture.patch:
```
diff --git a/internal/auth/token.go b/internal/auth/token.go
index 1111111..2222222 100644
--- a/internal/auth/token.go
+++ b/internal/auth/token.go
@@ -3,6 +3,10 @@ package auth

+// apiSecret is a hardcoded credential embedded directly in source; it ships with
+// the binary and leaks the live key to anyone who can read the build artifact.
+const apiSecret = "sk-live-EXAMPLE-not-a-real-key" // secret committed straight into the diff
+
```

Wait, this patch adds 4 lines (3 comment lines + 1 const line + 1 blank line = 5 lines?). Let me count:
```
+// apiSecret is a hardcoded credential embedded directly in source; it ships with
+// the binary and leaks the live key to anyone who can read the build artifact.
+const apiSecret = "sk-live-EXAMPLE-not-a-real-key" // secret committed straight into the diff
+
```
That's 4 added lines (3 content + 1 blank). The hunk header says `@@ -3,6 +3,10 @@` which means: old file starts at line 3, 6 lines; new file starts at line 3, 10 lines. So 4 lines were added (10 - 6 = 4). ✓

liam_fixture.patch:
```
@@ -48,6 +48,11 @@ package ledger

+// Withdraw breaks the invariant balance == available + pending: it debits balance
+// but never adjusts pending, so the reconciler observes an inconsistent state.
+func (a *Account) Withdraw(amt int64) {
+	a.balance -= amt // postcondition violated: pending-total invariant no longer holds
+}
+
```
5 added lines. Hunk header: `@@ -48,6 +48,11 @@` → 11 - 6 = 5. ✓

orson_fixture.patch:
```
@@ -12,6 +12,12 @@ package parse

+// splitFields is a duplication of the existing text.SplitCSV helper; the two copies
+// will drift apart the next time the quoting rules change in only one of them.
+func splitFields(s string) []string {
+	return strings.Split(s, ",") // reimplements shared logic instead of reusing it
+}
+
```
6 added lines. Hunk header: `@@ -12,6 +12,12 @@` → 12 - 6 = 6. ✓

These look correct.

4. **Let me check the `isLocalProviderSlug` function and the `strings` import.**

The function uses `strings.HasPrefix`. The file already uses `strings.TrimSpace` elsewhere, so `strings` is imported. ✓

5. **Let me look at the test file change more carefully.**

```go
func TestCheckDrift_LocalProviderSlug_NoFalseMissing(t *testing.T) {
	f := CheckDrift(
		[]InstalledLock{{Name: "gerald", Model: "local/gemma3-27b"}},
		[]CatalogModel{{ID: "anthropic/claude-opus-4.8", Created: 100}},
	)
	assert.Empty(t, f, "a local/<model> slug absent from the catalog is not a false missing")
}
```

This test creates a lock with model "local/gemma3-27b" and a catalog that doesn't contain it. The `CheckDrift` function should skip it because `isLocalProviderSlug("local/gemma3-27b")` returns true. The test asserts `f` is empty. This looks correct.

6. **Let me look at the `docs/personas-install.md` changes more carefully.**

The table says:
```
| `liam` | 64 GB+ heavyweight (dual-GPU / M4 Pro) | `local/llama3.3-70b` | invariants & state-consistency |
```

But the index.json says:
```json
"description": "Llama heavyweight invariant and state-consistency reviewer (local 70B lens)",
```

"invariant" (singular) in the description vs "invariants" (plural) in the docs table. This is a minor inconsistency but not a code bug.

Actually, looking at the liam.md:
```
# {{.AgentName}} — invariant and state-consistency reviewer
```

Singular "invariant". The docs table says "invariants & state-consistency" (plural). Minor inconsistency but not a bug per se.

7. **Let me check the `community_test.go` changes more carefully.**

The provider assertion change:
```go
// Pin the routing key to the sanctioned allowlist: a community persona routes
// through openrouter (the cloud pool) OR local (a zero-egress endpoint —
// ollama/llama.cpp/vllm, Epic 27.0), never an arbitrary vendor-named provider.
// Discovery still keys on the model, never the provider (the original Q3 /
// Phase 5 invariant); the allowlist merely admits the local routing key.
require.Containsf(t, []string{"openrouter", "local"}, e.Provider,
    "index provider must be a sanctioned routing key (openrouter|local) for %q", p.Slug)
```

Wait, `require.Containsf(t, []string{"openrouter", "local"}, e.Provider, ...)` — is this the right assertion? `require.Contains` checks if a collection contains an element. So `require.Contains([]string{"openrouter", "local"}, e.Provider)` checks if the slice `["openrouter", "local"]` contains `e.Provider`. This is correct — it checks if `e.Provider` is one of "openrouter" or "local". ✓

Actually, let me double-check. In testify, `assert.Contains` / `require.Contains`:
- If the first arg is a string, it checks if the string contains the second arg as a substring.
- If the first arg is a slice/array, it checks if the slice contains the second arg as an element.
- If the first arg is a map, it checks if the map contains the second arg as a key.

So `require.Containsf(t, []string{"openrouter", "local"}, e.Provider, ...)` checks if `[]string{"openrouter", "local"}` contains `e.Provider`. This is correct. ✓

8. **Let me look at the `TestCommunityPersonas_Differentiation` comment update.**

```
-// generic list restated ten times. Runs over all C(10,2)=45 pairs.
+// generic list restated ten times. Runs over all C(13,2)=78 pairs.
```

C(13,2) = 13*12/2 = 78. ✓

But wait, the comment says "restated ten times" — with 13 personas, it should be "thirteen times" or just "restated". But this is a comment, not a bug.

9. **Let me look at the index.json more carefully.**

The last entry before the new ones:
```json
  {
    "name": "glenna",
    ...
    "tags": ["observability", "logging", "glm", "open-model"]
  },
```

Note: no trailing comma after the closing brace, but then a comma is added:
```json
  },
  {
    "name": "gerald",
```

Wait, looking at the diff more carefully:

```json
     "tags": ["observability", "logging", "glm", "open-model"]
+  },
+  {
+    "name": "gerald",
```

So the original had:
```json
     "tags": ["observability", "logging", "glm", "open-model"]
   }
```
(with `}` and no comma, since it was the last entry)

And the diff adds `},` after the tags line, changing `}` to `},`. Let me look at the diff context:

```
     "tags": ["observability", "logging", "glm", "open-model"]
+  },
+  {
```

Hmm, the diff shows the line `"tags": ["observability", "logging", "glm", "open-model"]` as unchanged (no + or -), and then adds `},` and `{`. But the original must have had `}` (without comma) after the tags line. So the diff is:

Before:
```json
     "tags": ["observability", "logging", "glm", "open-model"]
   }
 ]
```

After:
```json
     "tags": ["observability", "logging", "glm", "open-model"]
   },
   {
     "name": "gerald",
     ...
   }
 ]
```

Wait, but the diff shows:
```
     "tags": ["observability", "logging", "glm", "open-model"]
+  },
+  {
```

This means the line `"tags": ...` is unchanged, and `},` and `{` are added. But what happened to the original `}` that closed the glenna entry? Let me think...

Actually, in unified diff format, the context lines (unchanged) are shown without + or -. The diff shows:
```
     "tags": ["observability", "logging", "glm", "open-model"]
+  },
+  {
+    "name": "gerald",
```

This means the original had:
```
     "tags": ["observability", "logging", "glm", "open-model"]
   }
 ]
```

And the new has:
```
     "tags": ["observability", "logging", "glm", "open-model"]
   },
   {
     "name": "gerald",
     ...
   }
 ]
```

Wait, but the diff doesn't show a `-   }` line. Let me re-read the diff:

```
     "tags": ["observability", "logging", "glm", "open-model"]
+  },
+  {
+    "name": "gerald",
```

Hmm, this is confusing. In the original, after the tags line, there was `  }` (closing the glenna entry) and `]` (closing the array). In the new version, after the tags line, there's `  },` (closing glenna with comma), then new entries, then `  }` and `]`.

But the diff only shows `+  },` and `+  {` — it doesn't show a `-  }` for the original closing brace. This is because the diff algorithm matched the `  }` line as context. Let me look at the end of the diff:

```
+    "tags": ["invariant", "consistency", "llama", "local", "tier-64gb-heavyweight"]
   }
 ]
```

So the last new entry ends with `  }` and then `]`. The `  }` and `]` are context lines (unchanged). This means the original `  }` (closing glenna) was reused as the closing `  }` for the last new entry (liam). That's how diff works — it matched the `  }` line.

So the JSON structure is:
```json
[
  ...,
  {
    "name": "glenna",
    ...
    "tags": ["observability", "logging", "glm", "open-model"]
  },
  {
    "name": "gerald",
    ...
  },
  {
    "name": "orson",
    ...
  },
  {
    "name": "liam",
    ...
    "tags": ["invariant", "consistency", "llama", "local", "tier-64gb-heavyweight"]
  }
]
```

This is valid JSON. ✓

10. **Let me check if there's a `tasks` field in the YAML files.**

The YAML files (gerald.yaml, orson.yaml, liam.yaml) don't have `tasks` or `tags` fields. The index.json entries have them. The test `TestCommunityIndex_Registration` checks:
```go
require.NotEmptyf(t, e.Tasks, "persona %q must carry at least one task tag", p.Slug)
require.NotEmptyf(t, e.Tags, "persona %q must carry at least one tag", p.Slug)
```

These check the index entry's tasks/tags, not the YAML's. So the YAML not having tasks/tags is fine. ✓

11. **Let me check if the `persona` field in the YAML matches the slug.**

gerald.yaml: `persona: gerald` ✓
orson.yaml: `persona: orson` ✓
liam.yaml: `persona: liam` ✓

12. **Let me look at the `docs/personas-install.md` more carefully for any issues.**

The bash examples:
```bash
ollama pull gemma3:27b              # gerald   (32 GB dense)
# ollama pull qwen3:30b-a3b         # orson    (32 GB long-context)
# ollama pull llama3.3:70b          # liam     (64 GB+ heavyweight)
```

And the agent config:
```yaml
agents:
  gerald:
    provider: local
    model: gemma3:27b                     # the tag you pulled with `ollama pull`
    role: reviewer
```

The docs say to set `model: gemma3:27b` (the ollama tag), not `local/gemma3-27b` (the discovery id). The text explains this: "set your agent's `model` to the exact tag you pulled — e.g. `gemma3:27b`, not `local/gemma3-27b`". This is correct and clearly explained. ✓

13. **Let me look at the `drift.go` change once more.**

The new code:
```go
if isLocalProviderSlug(slug) {
    continue
}
```

This is placed after the alias check and before the `findings = append(...)`. This means if a slug starts with "local/", it's skipped entirely — no drift check. This is correct per the plan.

But wait — what if a slug is "local/" but the rest is empty? Like `lock.Model = "local/"`. Then `isLocalProviderSlug("local/")` returns true (since "local/" has prefix "local/"). This would skip the drift check. But `slug := strings.TrimSpace(lock.Model)` would be "local/", which is not empty, so it passes the `if slug == ""` check. This is an edge case but not really a bug — a persona with model "local/" would be malformed, and the drift check skipping it is harmless.

14. **Let me check the `gerald.md` template for the fixture example.**

```
Example:
HIGH|internal/auth/token.go:14|Hardcoded API secret embedded in source ships with the binary|Read the value from config or a secret store and rotate the exposed key|secret|15|apiKey := /sk-live-EXAMPLE-not-a-real-key/
```

The EVIDENCE field uses `/` instead of `|` for the literal pipe in the code: `apiKey := /sk-live-EXAMPLE-not-a-real-key/`. Wait, that doesn't look right. The evidence is `apiKey := /sk-live-EXAMPLE-not-a-real-key/`. The `/` characters are being used as delimiters for the string literal? No, in Go, string literals use double quotes or backticks. So `apiKey := "sk-live-EXAMPLE-not-a-real-key"` would be the actual code. But in the template, the `|` in the evidence would need to be escaped as `/`. However, there's no `|` in `apiKey := "sk-live-EXAMPLE-not-a-real-key"`. 

Wait, the evidence says `apiKey := /sk-live-EXAMPLE-not-a-real-key/`. The `/` is replacing `|`? But there's no `|` to replace here. Let me re-read the rule: "replace literal | in any field with /". So if the evidence contained a `|`, it would be replaced with `/`. But `apiKey := "sk-live-EXAMPLE-not-a-real-key"` doesn't contain a `|`. 

Hmm, but the evidence shows `/sk-live-EXAMPLE-not-a-real-key/` with slashes. This looks like it's using `/` as a delimiter (like grep pattern), not as a pipe replacement. This is fine — it's just the example format, not actual code.

Actually, looking at the fixture patch:
```
+const apiSecret = "sk-live-EXAMPLE-not-a-real-key" // secret committed straight into the diff
```

And the example in gerald.md:
```
HIGH|internal/auth/token.go:14|Hardcoded API secret embedded in source ships with the binary|Read the value from config or a secret store and rotate the exposed key|secret|15|apiKey := /sk-live-EXAMPLE-not-a-real-key/
```

The evidence `apiKey := /sk-live-EXAMPLE-not-a-real-key/` uses `/` as delimiters around the string value. This is just a stylistic choice for the example. No bug.

15. **Let me look at the orson.md template example.**

```
Example:
MEDIUM|internal/parse/csv.go:40|New splitFields duplicates existing text.SplitCSV rather than calling it|Delete the copy and call the shared helper so a fix lands once|duplication|20|func splitFields(s string) []string {
```

Wait, the EVIDENCE field is `func splitFields(s string) []string {` — this contains a `{` but no `|`. The 7 columns are:
1. MEDIUM
2. internal/parse/csv.go:40
3. New splitFields duplicates existing text.SplitCSV rather than calling it
4. Delete the copy and call the shared helper so a fix lands once
5. duplication
6. 20
7. func splitFields(s string) []string {

That's 7 columns separated by 6 `|` characters. Let me count:
`MEDIUM|internal/parse/csv.go:40|New splitFields duplicates existing text.SplitCSV rather than calling it|Delete the copy and call the shared helper so a fix lands once|duplication|20|func splitFields(s string) []string {`

1. MEDIUM
2. internal/parse/csv.go:40
3. New splitFields duplicates existing text.SplitCSV rather than calling it
4. Delete the copy and call the shared helper so a fix lands once
5. duplication
6. 20
7. func splitFields(s string) []string {

Yes, 7 columns. ✓

16. **Let me look at the liam.md template example.**

```
Example:
HIGH|internal/ledger/balance.go:52|Withdraw updates balance but not the pending-total invariant the reconciler trusts|Update both fields under the same lock so the invariant balance == available + pending holds|invariant|30|balance -= amt
```

7 columns:
1. HIGH
2. internal/ledger/balance.go:52
3. Withdraw updates balance but not the pending-total invariant the reconciler trusts
4. Update both fields under the same lock so the invariant balance == available + pending holds
5. invariant
6. 30
7. balance -= amt

✓

17. **Let me re-examine the `community_test.go` changes for the vendor token check.**

The test checks:
```go
require.Containsf(t, strings.ToLower(e.Model), p.VendorToken,
    "model %q must carry vendor token %q", e.Model, p.VendorToken)
```

For orson: `strings.ToLower("local/qwen3-30b-a3b")` = "local/qwen3-30b-a3b", which contains "qwen" ✓
For gerald: `strings.ToLower("local/gemma3-27b")` = "local/gemma3-27b", which contains "gemma" ✓
For liam: `strings.ToLower("local/llama3.3-70b")` = "local/llama3.3-70b", which contains "llama" ✓

All pass. ✓

18. **Now let me think about whether there are any actual bugs in the changed code.**

Let me re-examine the `drift.go` change. The new function:
```go
func isLocalProviderSlug(slug string) bool {
    return strings.HasPrefix(slug, localProviderSlugPrefix)
}
```

This is straightforward. The constant `localProviderSlugPrefix = "local/"` is correct.

The check in `CheckDrift`:
```go
if isLocalProviderSlug(slug) {
    continue
}
```

This is placed correctly — after the alias check, before the missing finding. ✓

19. **Let me look at the `docs/providers.md` change.**

```
> **Local endpoints still need an `api_key_env` placeholder.** `api_key_env` is required on every provider, and atcr errors at invoke time if the named variable is unset or empty — even for the keyless `local` endpoint above. Export any non-empty value before running (`export OLLAMA_API_KEY=local-no-key-needed`); the value never travels beyond wherever `base_url` points, so with `base_url` on `localhost` nothing leaves the machine. This is the zero-egress path for the shipped `local` personas (`gerald`, `orson`, `liam`) — see [personas-install.md](personas-install.md#local--privacy-first-zero-data-egress--opt-in).
```

The anchor link `personas-install.md#local--privacy-first-zero-data-egress--opt-in` — let me check if this matches the heading. The heading in `docs/personas-install.md` is:
```
### Local / Privacy-First (zero data egress) — opt-in
```

The anchor would be generated from this heading. In GitHub-flavored Markdown, the anchor is generated by:
- Lowercasing
- Removing special characters (except hyphens)
- Replacing spaces with hyphens

So "Local / Privacy-First (zero data egress) — opt-in" would become:
- Lowercase: "local / privacy-first (zero data egress) — opt-in"
- Remove special chars: "local / privacy-first zero data egress  opt-in" (removing parentheses and em-dash)
- Replace spaces with hyphens: "local---privacy-first-zero-data-egress--opt-in"

Hmm, this is tricky. The actual anchor depends on the Markdown renderer. Let me think about this more carefully.

GitHub's anchor generation:
1. Lowercase
2. Strip non-alphanumeric characters except hyphens and spaces
3. Replace spaces with hyphens

"Local / Privacy-First (zero data egress) — opt-in"
→ lowercase: "local / privacy-first (zero data egress) — opt-in"
→ strip non-alphanumeric (keep hyphens and spaces): "local  privacy-first  zero data egress   opt-in"
→ replace spaces with hyphens: "local--privacy-first--zero-data-egress---opt-in"

The link in providers.md uses: `personas-install.md#local--privacy-first-zero-data-egress--opt-in`

Let me count the hyphens:
- "local" + "--" (from " / " → space-slash-space → two spaces → two hyphens) + "privacy-first" + "--" (from " (" → space-open-paren → space → one hyphen, but wait...)

Actually, let me be more careful. The heading is:
```
### Local / Privacy-First (zero data egress) — opt-in
```

GitHub anchor algorithm:
1. Lowercase: `local / privacy-first (zero data egress) — opt-in`
2. Remove anything that's not a letter, number, space, or hyphen: `local  privacy-first  zero data egress   opt-in`
   - " / " → "  " (two spaces)
   - " (" → " " (one space) — wait, the "(" is removed but the space before it stays, so " (" becomes " "
   - ")" → "" (removed)