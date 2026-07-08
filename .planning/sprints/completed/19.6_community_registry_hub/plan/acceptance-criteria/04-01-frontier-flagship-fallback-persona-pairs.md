# Acceptance Criteria: Frontier Provider Flagship+Fallback Persona Pairs

**Related User Story:** [04: Model-Indexed Persona Library Authoring](../user-stories/04-model-indexed-persona-library-authoring.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Persona content (YAML + Markdown), community layout | `personas/community/<slug>.yaml`, `personas/community/<slug>.md` |
| Test Framework | Go `testing` package (community fixture-test loop from AC 04-04) | Schema validation exercised via the registry loader, not a new framework |
| Key Dependencies | `docs/personas-authoring.md` authoring contract; registry agent schema (`provider`/`model` required fields) | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `personas/community/<anthropic-flagship-slug>.yaml` / `<anthropic-fallback-slug>.yaml` — create: Anthropic flagship+fallback persona YAMLs with non-empty `provider`/`model`.
- `personas/community/<openai-flagship-slug>.yaml` / `<openai-fallback-slug>.yaml` — create: OpenAI flagship+fallback persona YAMLs.
- `personas/community/<google-flagship-slug>.yaml` / `<google-fallback-slug>.yaml` — create: Google flagship+fallback persona YAMLs.
- `personas/community/<slug>.md` — create: prompt template per frontier persona, phrased per vendor prompting guidance.
- `personas/community/index.json` — modify: one entry per frontier persona with `provider`/`model` matching each YAML.
- `docs/personas-authoring.md` — reference: authoring contract (YAML schema, prompt template, fixture, contribution checklist).
- `personas/_base.md` — reference: shared prompt-template scaffold.

### Provider-vs-vendor semantics (LOCKED — Q3)
In the registry agent schema, `provider` is a **routing-endpoint key that must exist in the registry `Providers` map** (`internal/registry/config.go:672-676`: `validateAgent` rejects a `provider` that is not a defined `Providers` key). It is NOT the vendor name. Vendor identity (Anthropic/OpenAI/Google) lives in the `model` string (e.g. `provider: openrouter` + `model: anthropic/claude-3.7-sonnet`, per `docs/personas-authoring.md`). Therefore personas are grouped, differentiated, and asserted by the **vendor token in `model`** (`claude` / `gpt` / `gemini`) and/or `tasks`/`tags` — never by `provider ∈ {anthropic,openai,google}`.

### Intended flagship+fallback model bindings (LOCKED — Q2, 2026-07-07)
Each frontier vendor ships a flagship (primary) persona and a same-family fallback persona, mirroring the `registry.yaml` `bruce`/`bruce-backup` convention. The pinned intent below is traceable via the vendor-guidance citation each `.md` must carry (`<!-- vendor-guidance: <url-or-section> -->`, see AC 04-03):

| Vendor token in `model` | Flagship (primary) model id | Fallback (same-family) model id |
|-------------------------|-----------------------------|---------------------------------|
| `claude` (Anthropic) | Opus-tier id (e.g. `anthropic/claude-opus-4-1`) | Sonnet-tier id (e.g. `anthropic/claude-sonnet-4`) |
| `gpt` (OpenAI) | flagship-tier GPT id | lighter same-family GPT id |
| `gemini` (Google) | Gemini Pro-tier id | Gemini Flash-tier id |

The exact model-id strings are the author's responsibility at commit time; what this AC pins is the flagship+fallback *pairing* per vendor and its grounding citation. `provider` for every one of these personas is a valid `Providers`-map routing key (e.g. `openrouter`/`synthetic`), not the vendor name.


## Happy Path Scenarios
**Scenario 1: Each of the 3 frontier vendors has exactly a flagship+fallback pair**
- **Given** the completed `personas/community/` directory
- **When** the persona YAML files are enumerated and grouped by the **vendor token in `model`** (`claude` / `gpt` / `gemini`) — NOT by `provider`
- **Then** each of `claude`, `gpt`, and `gemini` resolves to exactly 2 persona YAML files (flagship + fallback), for a total of 6 frontier personas, and every one of those YAMLs has a `provider` that is a valid registry `Providers`-map routing key (e.g. `openrouter`/`synthetic`)

**Scenario 2: Flagship and fallback within a provider bind to distinct models**
- **Given** the Anthropic flagship and fallback persona YAMLs
- **When** their `model` fields are compared
- **Then** the two values are different (e.g. an Opus-tier model id for flagship, a Sonnet-tier model id for fallback), proving the pair is not a duplicate binding

**Scenario 3: Every frontier persona YAML passes strict registry-schema validation**
- **Given** a frontier persona YAML (e.g. the OpenAI flagship persona)
- **When** the persona loader parses and validates it against the registry agent schema
- **Then** validation succeeds with `provider` and `model` both non-empty and no unknown agent field present

**Scenario 4: Empty or missing `model` field is rejected before the persona is considered complete**
- **Given** a frontier persona YAML with `provider: "openai"` and `model` omitted or set to `""`
- **When** the registry schema validates it
- **Then** validation fails with a required-field error identifying the persona file and the missing `model` key, and the AC's Definition of Done cannot be marked complete until the field is populated

## Edge Cases
**Edge Case 1: Fallback persona is not a copy-pasted flagship prompt**
- **Given** the flagship and fallback persona Markdown prompts for the same provider
- **When** their `## Focus` sections are compared
- **Then** the fallback's review lens/task scope differs from the flagship's (per the story's constraint that personas are task-scoped, not generic restatements), even though both bind to the same vendor family (same `model` vendor token)

**Edge Case 2: `provider` is one of the agreed routing-key list (authoring-convention content-lint)**
- **Given** the `provider` value authored in each frontier YAML
- **When** a content-lint test asserts it is a member of an agreed routing-key allowlist (e.g. `{openrouter, synthetic, ...}`)
- **Then** the value matches an allowed routing key exactly (lowercase, no aliasing typos). NOTE: this is an authoring-convention lint, NOT loader enforcement — `ValidateAgentYAML` (`internal/registry/validate.go:29-38`) synthesizes a throwaway single-key registry (`Providers: {cfg.Provider: ...}`) so its provider-reference check passes for *whatever* value is present; casing/typo errors are therefore caught only by the content-lint here, not "rejected at load."

## Error Conditions
**Error Scenario 1: Missing `model` field on a frontier persona**
- **Given** a frontier persona YAML with `provider` set but `model` omitted or empty
- **When** the persona loader validates it
- **Then** validation fails with a required-field error and the persona is not written to the index — this is a pre-merge authoring defect, not a runtime path exercised by end users

**Error Scenario 2: Flagship/fallback pair collapses to the same model id**
- **Given** a provider's flagship and fallback YAMLs
- **When** their `model` fields are found to be identical
- **Then** the persona-review checklist step in Definition of Done fails the AC — a duplicate binding does not satisfy "flagship+fallback pair"

## Performance Requirements
- **Response Time:** N/A — static content authored at commit time; no runtime performance surface introduced by this AC.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — persona YAML/Markdown are static repository content with no auth surface.
- **Input Validation:** Every frontier persona YAML validates against the strict registry agent schema (unknown agent fields rejected); no persona embeds credentials, secrets, or network-call instructions in its prompt template, per `docs/personas-authoring.md`'s security note.

## Test Implementation Guidance
**Test Type:** UNIT (schema validation via the existing registry loader test pattern) + manual content review (flagship/fallback distinctness, vendor-guidance grounding)
**Test Data Requirements:** The 6 frontier persona YAML files themselves, loaded through the same validation path `internal/registry` already exercises for agent configs
**Mock/Stub Requirements:** None — pure static-file validation, no network or LLM call required

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Grouped by the `model` vendor token, `claude`, `gpt`, and `gemini` each have exactly one flagship and one fallback persona YAML (6 total)
- [ ] Every frontier persona YAML has non-empty `provider`+`model`; `provider` is a member of the agreed routing-key allowlist (content-lint), and `model` carries the intended vendor token
- [ ] Flagship and fallback `model` values differ within each vendor family (same vendor token, different tier id per the pinned bindings table)
- [ ] Each persona `.md` carries a `<!-- vendor-guidance: ... -->` citation (per AC 04-03) grounding its flagship+fallback phrasing
- [ ] Flagship and fallback prompts are task-scoped differently, not duplicate generic text

**Manual Review:**
- [ ] Code reviewed and approved
