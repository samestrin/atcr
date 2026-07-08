# Authoring a Persona

A persona is a named reviewer lens: a prompt that tells one model what to hunt for, plus the metadata that binds it to a provider, scopes it to a language, and proves it works against a fixture. This guide is the complete, source-free reference for writing one — a persona YAML, its prompt template, and its fixture — and the contribution checklist to submit it.

Two things make a persona trustworthy:

1. **It validates against the registry schema** — so it can never carry a malformed or unknown field into a review.
2. **It passes its fixture** — a small diff with a known problem the persona must flag, run in CI with no network.

## 1. The persona YAML

An installable community persona is a YAML document that is a **superset of a registry agent**: the agent fields (`provider`, `model`, …) are validated by the same rules the registry applies at load, and a few extra persona-file keys (`version`, `description`) carry catalog metadata the registry ignores. Validation is **non-strict** for the extra keys but **strict** for the agent fields — an unknown *agent* field or an out-of-range value is rejected before the persona is ever written to disk.

Copy this fill-in-the-blank template and fill in every **REQUIRED** field:

```yaml
# ── Persona-file metadata (catalog only; ignored by the registry schema) ──
name: security/owasp            # OPTIONAL — informational slug/title
version: 1.0.0                  # OPTIONAL — shown by `atcr personas list`; drives `upgrade` version comparison
description: OWASP Top 10 review # OPTIONAL — shown by `atcr personas search`

# ── Agent binding (validated against the registry schema) ──
provider: openrouter            # REQUIRED — an OpenAI-compatible provider key
model: anthropic/claude-3.7-sonnet  # REQUIRED — the model id at that provider
persona: owasp                  # OPTIONAL — prompt template name (defaults to the agent name)
role: skeptic                   # OPTIONAL — reviewer | skeptic | judge (default reviewer)

# ── Language scope (OPTIONAL — drives language-aware skeptic routing) ──
language: ["go"]                # canonical: dotless, lowercased; omit for a generalist
```

**Required vs optional.** Only `provider` and `model` are required by the schema. Everything else is optional and defaults sanely: `persona` defaults to the agent name, `role` defaults to `reviewer`, and an omitted `language` means **no language constraint** (the persona is eligible for every review). Out-of-range or unknown *agent* fields are load errors, so a typo surfaces at install time, not mid-review.

> **Security:** A persona prompt is executed as part of the review pipeline. Do **not** embed credentials, secrets, tokens, or instructions to make external network calls in a persona — the prompt is fed verbatim to the model. The registry schema rejects unrecognized agent fields specifically so an unsupported behavior cannot be smuggled in.

### The `language` scope field

`language` declares the file extensions a persona specializes in. When a finding's file extension matches a persona's `language` scope, that persona is **preferred** over an unscoped one during skeptic selection (see [registry.md](registry.md#language-scope-and-skeptic-routing) for the routing algorithm).

**Canonical format — no leading dot, lowercased:**

| You write | Stored as |
|-----------|-----------|
| `["go"]` | `["go"]` |
| `[".go"]` | `["go"]` |
| `[" .GO "]` | `["go"]` |
| `["go", "ts"]` | `["go", "ts"]` (multi-language) |

The loader canonicalizes every entry (trim whitespace → strip **all** leading dots → lowercase), so `go`, `.go`, `..go`, and ` .GO ` all store and match identically. Prefer writing the canonical form (`["go"]`) directly.

**Validation:** an entry that is empty, whitespace-only, or just dots (`"."`, `".."`) is rejected — those would canonicalize to a blank token that matches every extensionless finding. Control characters are rejected. There is **no** allow-list of known languages: you may declare any extension your persona targets.

**Single trailing extensions only:** `language` entries must be single trailing extensions such as `go` or `ts`. Compound forms like `tar.gz` or `d.ts` are stored verbatim, but the router canonicalizes a finding's extension to its last segment (e.g. `gz` or `ts`), so compound entries silently never match. Use the last segment (`gz`, `ts`) instead.

**Only *surrounding* whitespace is trimmed:** canonicalization removes leading/trailing whitespace, but *interior* whitespace is left untouched. An entry like `"g o"` is stored verbatim as `g o` and — like a compound extension — silently never matches any finding extension; it is **not** rejected at load. Write a single contiguous token (`go`), never one with embedded spaces.

**Nil semantics:** omit `language` entirely (or leave it empty) and the persona carries no constraint — it participates in every review regardless of the repository's detected language, with no routing preference. Use a `language` scope only when the persona is genuinely language-specific.

### Model-in-structured-metadata convention

A persona's bound `model` must live in its **structured metadata** — the `model:` YAML key above — and never only in the free-text `description`. Discovery *by model* (`atcr personas search --model …`) matches the structured `model` field of the community `index.json` entry — kept in lockstep with this YAML key by the [section 5](#5-the-community-index-entry) gate — so a model named only in prose is invisible to search and does not satisfy the authoring contract.

This is a **forward-looking rule for every community/library persona**: the persona fixture test in `internal/personas/test.go` (`TemplateFixtureRunner.RunFixture`) asserts the resolved persona carries a non-empty `model` in structured metadata — a blank or missing value fails `go test` with a clear, attributable error — alongside the fixture render check described in [section 3](#3-the-fixture). Embedded built-in personas are model-agnostic and **exempt** (they carry no `provider`/`model`); this exemption is built-in-only, and community contributions are held to the convention.

### Persona naming: human first names

Every new persona — built-in **or** community — is named with a **human first name** (e.g. `bruce`, `sasha`, `penny`, `ingrid`, `anthony`, `delia`), never a role-based slug (`sentinel`, `tracer`, `idiomatic`). This is a **forward-looking rule for new contributions**: it does not require renaming any already-shipped persona, only that additions follow the convention instead of reintroducing role-based names. The rule applies uniformly to built-in and community personas — there is no built-in-only exemption.

> **Single source of truth (shared with Epic 23.0 AC5).** Epic 23.0 is **absorbed by and superseded by Epic 19.6**: the role-based built-in stragglers were renamed to human names under 19.6's Phase 5 (see `human-names-migration.md`), so 23.0 is not run as a standalone renamer. 19.6's broader wording (built-in **and** community) subsumes 23.0 AC5's built-in-only scope — a superset, not a contradiction.

## 2. The prompt template

The prompt is what the persona actually *says* to the model. Built-in personas live as Markdown templates in `personas/` (for example `personas/bruce.md`, `personas/sasha.md`); a community persona's `persona:` field names its prompt. Mirror the canonical structure exactly — the same section headings and the same template variables:

```markdown
# {{.AgentName}} — <one-line lens description>

## Role
You are {{.AgentName}}, the panel's <lens> reviewer. <What you hunt for, in the
imperative. No flattery, no summaries — findings only.>

## Focus
1. <Target class #1 — be specific>
2. <Target class #2>
3. <…>

## Scope
{{.ScopeRule}}

{{if .ToolsEnabled}}## Tool-Assisted Review
You may use read_file, grep, and list_files to explore beyond the payload. Cite
the exact file and line numbers you actually read; never invent context.

{{end}}## Severity Rubric
- CRITICAL: <directly exploitable / certain, reachable failure>
- HIGH: <likely failure given realistic input>
- MEDIUM: <defense-in-depth / needs deliberate attention>
- LOW: <hardening or clarity, limited blast radius>

## Output Format
Emit ONLY findings, one per line, exactly 7 pipe-delimited columns:

SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE

Rules: replace literal | in any field with /; CATEGORY is one lowercase word;
EST_MINUTES is an integer; EVIDENCE cites the offending code; no prose. If
nothing is wrong, emit nothing.

## Payload
Reviewing {{.FileCount}} changed file(s), {{.BaseRef}}..{{.HeadRef}}, payload mode: {{.PayloadMode}}.

{{.Payload}}
```

**Required template variables** (the renderer fails if a referenced variable is missing, and the fixture test fails if any `{{ }}` action is left unrendered): `{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`. The `{{if .ToolsEnabled}}…{{end}}` block is optional but recommended — it is included only for tool-using agents.

**Mandatory sections:** a `## Role` declaration and a `## Output Format` block with the exact 7-column pipe-delimited contract above. Keep the column format byte-for-byte — the reconciler parses it.

**Name the category in the prompt.** The fixture test asserts the persona's expected finding **category word** appears in the *prompt template itself* (case-insensitive), not merely in the rendered diff. So if your fixture expects an `injection` finding, the word `injection` must appear in your `## Focus` or `## Output Format` example. This guarantees the persona is genuinely authored to find the category, rather than the word leaking in from the injected diff.

## 3. The fixture

A fixture proves the persona works without an LLM or a network call. It is a small diff containing a **known, synthetic** instance of the persona's target class.

**Requirements** (the location, naming, and rendering rules are what the fixture test exercises; mode and content are project conventions):

| Requirement | Rule |
|-------------|------|
| **Location** | Built-in personas: `personas/testdata/`. Community-library personas: `personas/community/testdata/` (co-located with the community layout). Both are test-enforced — the fixture runner reads from the matching location (`//go:embed testdata/*.patch` for built-ins, `//go:embed community/testdata/*.patch` for the community library). |
| **Format** | a unified-diff `.patch` (or `.diff`) file |
| **Naming** | `<slug>_fixture.patch` — e.g. `sasha_fixture.patch` (built-in), `anthony_fixture.patch` (community) |
| **File mode** | `0644` (convention; not asserted by the test) |
| **Content** | **synthetic values only** — never a real credential. Use placeholders like `FAKE_API_KEY_00000000` (convention; not asserted by the test) |
| **Network** | none — the fixture is read from disk and rendered locally; no live call is permitted in the test path |

**What the test does** (for each persona, with no LLM and no network):

1. Loads the committed `.patch` fixture from `personas/testdata/` — a missing or uncommitted fixture fails here.
2. Asserts the expected category word is present in the persona **template** (see "Name the category in the prompt" above).
3. Renders the template with the fixture as the diff payload and confirms **no unrendered `{{ }}` actions remain**.

Worked example — a `penny` (performance) fixture that plants an N+1 query inside a loop:

```diff
--- a/store/orders.go
+++ b/store/orders.go
@@ -10,6 +10,11 @@ func (s *Store) OrderTotals(ids []int) []int {
 	totals := make([]int, 0, len(ids))
+	for _, id := range ids {
+		// N+1: one query per id inside the loop
+		row := s.db.QueryRow("SELECT amount FROM orders WHERE id = ?", id)
+		var amt int
+		_ = row.Scan(&amt)
+		totals = append(totals, amt)
+	}
 	return totals
 }
```

Its `penny.md` prompt names the `n+1` category, so the test confirms the persona is authored to catch exactly this.

## 4. Contribution checklist

Before submitting your persona, confirm every item:

- [ ] **Persona YAML** has both required fields (`provider`, `model`) and validates — `go test ./...` is green, or `atcr personas install <slug>` succeeds against your registry.
- [ ] **`language` scope** (if present) is in canonical form (no leading dot, lowercased, e.g. `["go", "ts"]`); omit it entirely for a generalist persona.
- [ ] **Prompt template** mirrors the canonical structure: `## Role`, `## Focus`, `## Scope` (`{{.ScopeRule}}`), `## Severity Rubric`, the exact 7-column `## Output Format` contract, and `## Payload` (`{{.Payload}}`).
- [ ] **Required template variables** are all present and the template renders with no leftover `{{ }}` actions.
- [ ] **Category word** for the persona's target class appears in the prompt template itself.
- [ ] **Fixture** is a `.patch`/`.diff` in `personas/testdata/`, named `<slug>_fixture.patch`, mode `0644`, containing a **synthetic** instance of the target class (no real secrets).
- [ ] **Fixture test passes** locally with no network access.
- [ ] **No secrets, credentials, or network instructions** in the prompt.
- [ ] **Index entry** (if the persona ships in the community `index.json`) carries non-empty `provider` and `model` that **exactly match** the persona YAML's `provider`/`model` — enforced by a `go test` gate, not editorial review.

## 5. The community index entry

Personas distributed through the community channel are enumerated in `personas/community/index.json` — an array of entries that `atcr personas search` reads to answer *"which persona is tuned for the model I have?"*. The index is **authored in-repo** (not generated), and a `go test` gate asserts each entry stays consistent with its source persona YAML.

Each entry has this shape (the JSON keys map 1:1 to `PersonaIndexEntry` in `internal/personas/search.go`):

```json
{
  "name": "security/owasp",
  "version": "1.0.0",
  "description": "OWASP Top-10 security reviewer",
  "path": "security/owasp.yaml",
  "provider": "openrouter",
  "model": "anthropic/claude-3.7-sonnet",
  "tasks": ["security-review"],
  "tags": ["owasp", "security"]
}
```

| Key | Required | Meaning |
|-----|----------|---------|
| `name` | yes | Persona slug/title, shown in listings. |
| `version` | yes | Semver; drives `atcr personas upgrade` comparison. |
| `description` | yes | Shown by `atcr personas search` (keyword match). |
| `path` | yes | Path to the persona YAML relative to the index root (e.g. `security/owasp.yaml`). |
| `provider` | yes | Routing-endpoint key — **must be non-empty and equal the persona YAML's `provider`**. |
| `model` | yes | The model id — **must be non-empty and equal the persona YAML's `model`**. Discovery by model matches this structured field, never free-text. |
| `tasks` | no | Forward-looking task tags. **Omit the key entirely** when absent — do not emit `"tasks": []`. |
| `tags` | no | Forward-looking free-form tags. **Omit the key entirely** when absent — do not emit `"tags": []`. |

Here "Required" means **gate-enforced at `go test` time, not enforced by the Go type**: `PersonaIndexEntry` tags `provider`/`model` as `omitempty`, so an entry that omits them still decodes — the gate below is what rejects it.

**Enforcement (hard gate, not editorial):** a Go test iterates every entry in `personas/community/index.json`, loads each entry's source persona YAML via its `path`, and fails `go test` if any entry's `provider`/`model` is empty or drifts from the YAML. A library persona with missing or mismatched metadata cannot merge. Embedded built-in personas are **exempt** — they are never enumerated in the community index. `provider`/`model`/`tasks`/`tags` are display/search metadata only: never embed executable content, secrets, or network instructions in them.

See [personas-install.md](personas-install.md) for installing and using personas, and [registry.md](registry.md) for the full agent schema and routing semantics.
