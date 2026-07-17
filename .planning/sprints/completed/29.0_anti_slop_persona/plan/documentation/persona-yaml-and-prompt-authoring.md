# Persona YAML & Prompt Authoring
**Priority:** [CRITICAL]

## Overview

A community persona ships as a matched three-file unit: `personas/community/<slug>.yaml` (the agent binding — provider, model, persona, role, and optional language), `personas/community/<slug>.md` (the prompt template), and `personas/community/testdata/<slug>_fixture.patch` (a synthetic unified diff proving the persona fires). `simon.yaml` and `simon.md` must be modeled directly on the existing `personas/community/sonny.yaml` / `sonny.md` pair, replacing Role/Focus content with slop-detection targets — tautological comments, unnecessary abstractions (factories/interfaces wrapping a single struct), defensive-programming overkill, and dead or hallucinated code paths — while keeping the mandatory `## Role`, `## Focus`, `## Scope`, `## Severity Rubric`, and `## Output Format` headings and the exact 7-column `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE` output contract intact. (> Source: codebase-discovery.json#Community persona 3-file unit; codebase-discovery.json#Reusable component "sonny.yaml / sonny.md 3-file pattern")

`simon.yaml` is parsed with `gopkg.in/yaml.v3`, which is the de-facto YAML library the project standardizes on for config and registry parsing. The critical constraint from that library is `Decoder.KnownFields(true)` strict-mode decoding: unknown YAML keys become load-time errors rather than being silently ignored, which is exactly the mechanism `internal/personas/community_schema_test.go`'s `TestCommunityPersonas_StrictSchema` exercises against every community persona YAML file. Practically, this means `simon.yaml` must use only recognized keys — the strict-decode target (`communityPersonaFile`, internal/registry/validate.go:68) is the union of the agent-binding fields (`provider`, `model`, `persona`, `role`, optional `language`, plus the other `AgentConfig` keys) and the catalog-only keys (`name`, `version`, `description`, `tasks`, `tags`, `fixture`, `path`) — any typo'd or invented key outside that union fails the strict schema decode. Mirroring `sonny.yaml`'s exact key set (`name`, `version`, `description`, `provider`, `model`, `persona`, `role`) keeps simon inside it. The YAML must also bind a concrete, non-placeholder provider/model pair, since `TestCommunityPersonas_NoPlaceholderModel` rejects placeholder values, and the persona slug (`simon`) must satisfy `^[a-z]+$` and avoid the retired-role denylist per `TestCommunityPersonas_HumanNames`. (> Source: yaml-v3.md#Struct-Tags; > Source: yaml-v3.md#Key-APIs; > Source: codebase-discovery.json#Embedded-set gates auto-cover every community persona (no roster needed))

`simon.md` is a `text/template` document, not `html/template` — no HTML escaping applies, and the template is rendered with payload variables such as `{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, and `{{.Payload}}`. The fetched-prompt guardrail's allowlist (`allowedPersonaFields`, internal/registry/persona.go:140) adds one more permitted field, `{{.ToolsEnabled}}`, and permits exactly two construct kinds: bare references to those eight fields, and `{{if .ToolsEnabled}}…{{end}}` blocks (the pattern `sonny.md` uses for its Tool-Assisted Review section). Everything else — `{{range}}`/`{{with}}`/`{{template}}`/`{{define}}`, pipelines, function calls, field chains — is rejected at install/resolve time, so keep `simon.md` to the sanctioned set. The prompt must also stay under the `MaxPersonaPromptLen` length cap so the resolution chain's fallback behavior (a broken/oversized template demotes to the next layer rather than aborting the run) is preserved. (> Source: standard-library.md#text/template — persona prompt rendering; > Source: codebase-discovery.json#Embedded-set gates auto-cover every community persona (no roster needed))

## Key Concepts

- Community personas are a fixed three-file unit (`<slug>.yaml`, `<slug>.md`, `testdata/<slug>_fixture.patch`) and are auto-covered by the embedded-set gates without needing a roster entry. > Source: codebase-discovery.json#Community persona 3-file unit
- `simon.yaml` must decode cleanly under `yaml.v3` strict `KnownFields(true)` mode — unknown keys are errors, not warnings. > Source: yaml-v3.md#Key-APIs
- Only exported (uppercase) struct fields are marshaled by `yaml.v3`; struct tags use the `` `yaml:"key,flag1,flag2"` `` format with flags like `omitempty`, `flow`, `inline`, and `-`. > Source: yaml-v3.md#Struct-Tags
- A community persona requires a concrete provider+model binding per the registry schema even though simon's detection target (AI-generated code bloat) is not inherently model-specific. > Source: codebase-discovery.json#Architecture note
- `simon.md` must keep the mandatory `## Role`, `## Focus`, `## Scope`, `## Severity Rubric`, and `## Output Format` sections and the exact 7-column `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE` output contract, mirroring `sonny.md`. > Source: codebase-discovery.json#Reusable component "sonny.yaml / sonny.md 3-file pattern"
- Persona prompts render with `text/template` (never `html/template`, since no HTML escaping is wanted in prompts) using payload vars including `{{.Payload}}`, `{{.PayloadMode}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.AgentName}}`, `{{.ScopeRule}}`, plus the optional `{{.ToolsEnabled}}` field — any allowlisted field may condition an `{{if}}` block in a fetched community prompt; in practice that is always `{{if .ToolsEnabled}}`. > Source: standard-library.md#text/template — persona prompt rendering
- A broken or oversized persona template must fail parsing/rendering in a way that demotes to the next fallback layer, not abort the run — this behavior must be preserved when authoring `simon.md`. > Source: standard-library.md#text/template — persona prompt rendering
- `internal/personas/community_schema_test.go` enforces the gate: `TestCommunityPersonas_StrictSchema` (strict YAML decode), `TestCommunityPersonas_NoPlaceholderModel` (no placeholder provider/model), `TestCommunityPersonas_HumanNames` (slug matches `^[a-z]+$`, not on the retired-role denylist). > Source: codebase-discovery.json#Embedded-set gates auto-cover every community persona (no roster needed)

## Code Examples

`yaml.v3` whole-document Unmarshal/Marshal with struct tags, as documented for atcr config/registry parsing (this is the only verbatim yaml.v3 usage example in the source material — `simon.yaml` itself is hand-authored data, not Go struct code, but the decode contract it must satisfy is this one):

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

> Source: yaml-v3.md#Quick-Start

Strict-mode decoding relevant to `simon.yaml`'s schema gate:

> `Decoder.KnownFields(true)` — strict mode: unknown YAML keys become errors. **Use this for atcr config and registry parsing** to catch typos (`serial_agnets:`) at load time instead of silently ignoring them.

> Source: yaml-v3.md#Key-APIs

Conditional-section pattern for `simon.md`: the standard-library spec documents `{{if .LargeDiff}}...{{end}}` as the base-system's per-payload-mode guidance pattern, but `LargeDiff` is NOT in the fetched-prompt allowlist — a community prompt using it fails `ValidateFetchedPersonaPrompt`. The conditional construct that IS sanctioned for community prompts is `{{if .ToolsEnabled}}...{{end}}` (used by `sonny.md` for its Tool-Assisted Review section, and exercised in both states by `TestCommunityPersonas_RendersInBothToolStates`):

> Conditional sections via `{{if .LargeDiff}}...{{end}}` carry over for per-payload-mode guidance.

> Source: standard-library.md#text/template — persona prompt rendering (base-system pattern; for `simon.md` substitute the allowlisted `{{if .ToolsEnabled}}` form)

No verbatim source code for `sonny.yaml` or `sonny.md` was included in the two source documents provided for this file, so their exact contents are not reproduced here — see the Related Documentation links and `docs/personas-authoring.md` for the authoritative structure to copy.

## Quick Reference

| Concept | API/Tag | Notes |
|---|---|---|
| Strict unknown-key rejection | `yaml.NewDecoder(r).KnownFields(true)` | Backing mechanism for `TestCommunityPersonas_StrictSchema`; `simon.yaml` must use only recognized keys |
| Struct field export | Exported (uppercase) fields only | Unexported fields are never marshaled by `yaml.v3` |
| Struct tag flags | `` `yaml:"key,omitempty\|flow\|inline\|-"` `` | `omitempty` honors `IsZero()`; `-` ignores a field entirely |
| Whole-document parse | `yaml.Unmarshal(data, &cfg)` / `yaml.Marshal(cfg)` | Type mismatches return `*yaml.TypeError` with partial unmarshaling |
| Persona template engine | `text/template` (not `html/template`) | No HTML escaping in rendered prompts |
| Required template tokens | `{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}` | Required by `TestCommunityPersonas_PromptStructure`; `{{.ToolsEnabled}}` is the one additional allowlisted field — bare field refs and `{{if .ToolsEnabled}}…{{end}}` blocks only; `{{range}}`/`{{with}}`/`{{template}}`, pipelines, and field chains are rejected |
| Template failure handling | Parse/render errors demote to fallback layer | Preserve this — do not let a broken `simon.md` abort the run |
| Persona unit | `simon.yaml` + `simon.md` + `testdata/simon_fixture.patch` | Fixed 3-file pattern, mirrored from `sonny.yaml`/`sonny.md`/`sonny_fixture.patch` |
| Schema gate | `internal/personas/community_schema_test.go` | `TestCommunityPersonas_StrictSchema`, `TestCommunityPersonas_NoPlaceholderModel`, `TestCommunityPersonas_HumanNames` |
| Output contract | 7-column pipe table | `SEVERITY\|FILE:LINE\|PROBLEM\|FIX\|CATEGORY\|EST_MINUTES\|EVIDENCE` |

## Related Documentation
- [../plan.md](../plan.md)
- `docs/personas-authoring.md` — authoritative persona unit structure to copy (referenced above)
- `.planning/specifications/packages/yaml-v3.md`
- `.planning/specifications/packages/standard-library.md`
