# Acceptance Criteria: `simon.md` Canonical Section Order, Template-Token Contract, and Anti-Slop Focus

**Related User Story:** [1: Author the `simon` Persona Unit](../user-stories/01-author-the-simon-persona-unit.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go `text/template` Markdown prompt authoring | `personas/community/simon.md`, rendered by the registry's persona renderer |
| Test Framework | `go test` + `testify/assert`/`require` | `internal/registry/persona_test.go` guardrail + `internal/personas` fixture runner |
| Key Dependencies | `registry.ValidateFetchedPersonaPrompt` (`internal/registry/persona.go`), `registry.MaxPersonaPromptLen = 8192` (`internal/registry/config.go:93`) | prompt-length cap and template-action allow-list enforcement |

## Related Files
- `personas/community/simon.md` - create: prompt template with `## Role`, `## Focus`, `## Scope`, (optional `{{if .ToolsEnabled}}...{{end}}`), `## Severity Rubric`, `## Output Format`, `## Payload` sections
- `personas/community/sonny.md` - reference only: structural skeleton this file is a direct copy-and-edit of (section order, token placement, single `{{if .ToolsEnabled}}...{{end}}` block)
- `internal/registry/persona_test.go:305` - test (unmodified): `TestValidateFetchedPersonaPrompt_AllEmbeddedCommunityPersonasPass` iterates `personas.CommunityNames()` and runs `ValidateFetchedPersonaPrompt` against each embedded `.md`
- `docs/personas-authoring.md` - reference only: §2 "The prompt template" is the authoritative structural contract (lines 71-119)

### Related Files (from codebase-discovery.json)
- `personas/community/simon.md` - create (`files_to_create`): prompt template hyper-focused on tautological AI comments, unnecessary design-pattern abstractions, defensive-programming overkill, and dead/hallucinated code paths, based on `personas/community/sonny.md`
- `personas/community/sonny.md` - reference only (`related_files`, high relevance): structural skeleton for the prompt (Role/Focus/Scope/Severity Rubric/Output Format sections, vendor-guidance comment, template tokens)
- `internal/registry/persona_test.go:305` - test, reference only (`related_files`, medium relevance): `TestValidateFetchedPersonaPrompt_AllEmbeddedCommunityPersonasPass` — simon.md must pass the fetched-prompt guardrail (length cap + template-action gate)
- `docs/personas-authoring.md` - reference only (`build_from.primary_file`): prompt template contract (mandatory sections, required tokens, 7-column output contract)

## Happy Path Scenarios
**Scenario 1: Canonical section order and required headings**
- **Given** `simon.md` contains, in order, `## Role`, `## Focus`, `## Scope`, `## Severity Rubric`, `## Output Format`, `## Payload` (mirroring `sonny.md`'s structure)
- **When** the persona is loaded and rendered
- **Then** all six mandatory sections are present and the exact 7-column `## Output Format` contract (`SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE`) is byte-for-byte identical to the canonical contract used by `sonny.md`

**Scenario 2: Only allow-listed bare template tokens are used, plus one `{{if .ToolsEnabled}}...{{end}}` block**
- **Given** `simon.md` references only `{{.AgentName}}`, `{{.ScopeRule}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.PayloadMode}}`, `{{.Payload}}`, `{{.ToolsEnabled}}` as bare references, with a single `{{if .ToolsEnabled}}...{{end}}` block copied verbatim from `sonny.md`'s position
- **When** `TestValidateFetchedPersonaPrompt_AllEmbeddedCommunityPersonasPass` calls `ValidateFetchedPersonaPrompt(text)` on the loaded `simon` template
- **Then** validation passes with no error (no `{{range}}`, `{{with}}`, `{{template}}`, `{{define}}`, pipelines, field chains like `{{.Payload.Field}}`, or variable assignments)

**Scenario 3: `## Focus` is hyper-focused on the four named anti-slop targets and names a new category word**
- **Given** `simon.md`'s `## Focus` section is a numbered list covering (1) tautological/apologetic AI comments, (2) unnecessary design patterns (factories, interfaces) wrapping simple logic, (3) defensive-programming overkill (redundant nil/null checks where type safety already guarantees non-nil), (4) dead or hallucinated code paths, and uses the word `bloat` verbatim (case-insensitive) in the prose
- **When** a human or the fixture-authoring step (Story 2) inspects the Focus section for the persona's target category
- **Then** the word `bloat` appears at least once in `simon.md`'s text, and each bullet names a concrete, recognizable anti-slop pattern rather than generic "code quality" advice

## Edge Cases
**Edge Case 1: Category word does not collide with an already-claimed word**
- **Given** the 13 already-claimed category words are `coupling`, `logic`, `contract`, `validation`, `race`, `leak`, `complexity`, `type`, `dependency`, `observability`, `secret`, `duplication`, `invariant`
- **When** `simon.md`'s Focus/Output-Format prose is grepped for `bloat` against every other file in `personas/community/*.md`
- **Then** no other community persona `.md` file contains `bloat` as its category word (verified: `grep -il "bloat" personas/community/*.md` returns no matches as of authoring)

**Edge Case 2: Single vendor-guidance citation line, distinct from `sonny.md`'s**
- **Given** `sonny.md` opens with `<!-- vendor-guidance: Anthropic — "Be clear and direct" ... -->`
- **When** `simon.md` is authored with exactly one `<!-- vendor-guidance: ... -->` line relevant to anti-bloat/conciseness prompting guidance (not a verbatim copy of `sonny.md`'s citation)
- **Then** the file contains exactly one such comment line, positioned as the first line of the file (matching the canonical position)

**Edge Case 3: Prompt stays comfortably under `MaxPersonaPromptLen` (8192 bytes)**
- **Given** `registry.MaxPersonaPromptLen = 8192` caps a fetched/pinned community persona prompt (`internal/registry/config.go:93`, enforced at `internal/registry/persona.go:166`)
- **When** `simon.md`'s full rendered byte length is measured
- **Then** it is well under 8192 bytes (the `sonny.md` template this AC copies is ~2KB; a comparable Focus-section expansion keeps `simon.md` in the same order of magnitude)

**Edge Case 4: Role+Focus language must stay under the 0.85 Jaccard differentiation threshold**
- **Given** `TestCommunityPersonas_Differentiation` (`personas/community_test.go:272`, `const threshold = 0.85` at line 282) compares the combined `## Role` + `## Focus` token-set of every community roster pair and fails on `Jaccard(Role+Focus) > 0.85`
- **When** `simon.md`'s Role/Focus prose is authored with generic code-quality wording instead of AI-authorship-specific artifacts, it risks drifting above the threshold against an existing community lens (e.g. `sonny`'s correctness/logic focus)
- **Then** this must be avoided by construction — ground every Role/Focus sentence in the four named anti-slop targets (tautological comments, unnecessary abstractions, defensive-programming overkill, dead/hallucinated code paths) so the pairwise similarity stays `<= 0.85` against all 13 existing personas; the gate itself is roster-driven and fires in Story 2, but the language constraint is fixed here at authoring time

## Error Conditions
**Error Scenario 1: A disallowed template action would fail the fetched-prompt guardrail**
- Error message: `ValidateFetchedPersonaPrompt` returns an error for any of `{{range}}`, `{{with}}`, `{{template}}`, `{{define}}`, a pipeline (`{{printf "%s" .Payload}}`), a field chain (`{{.Payload.Field}}`), a variable assignment (`{{$x := .Payload}}`), or an unbalanced/dangling action
- HTTP status / error code: N/A (Go `error` from `ValidateFetchedPersonaPrompt`, surfaced as a `go test` failure — see `internal/registry/persona_test.go:283-299` for the exact bad-input table)
- This AC's scope is to avoid triggering this error — author `simon.md` by editing a direct copy of `sonny.md`'s skeleton rather than writing template syntax from scratch

**Error Scenario 2: An over-length prompt would fail the length cap**
- Error message: `"persona prompt exceeds maximum length of %d bytes"` (formatted with `MaxPersonaPromptLen = 8192`, `internal/registry/persona.go:167`)
- This AC's scope is to avoid triggering this error — keep the Focus section as a tight numbered list, not prose paragraphs

## Performance Requirements
- **Response Time:** N/A — static template file, resolved once via `go:embed` at compile time; no per-request cost distinct from any other community persona
- **Throughput:** N/A — single Markdown file

## Security Considerations
- **Authentication/Authorization:** N/A — no credentials, secrets, tokens, or network-call instructions may be embedded in the prompt per `docs/personas-authoring.md` §2's security guidance; the prompt is fed verbatim to the model
- **Input Validation:** The template-action allow-list (`ValidateFetchedPersonaPrompt`) is the enforcement boundary preventing an untrusted community-tier prompt from smuggling in Go-template control flow that could alter rendering behavior beyond the intended token substitutions

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** The authored `personas/community/simon.md` file itself; no separate test-data file needed for this AC (fixture `.patch` authoring is Story 2's scope, not this AC's)
**Mock/Stub Requirements:** None — `TestValidateFetchedPersonaPrompt_AllEmbeddedCommunityPersonasPass` reads the real embedded `.md` via `personas.CommunityGet("simon")`; no LLM or network call

## Definition of Done
**Auto-Verified:**
- [x] `go test ./internal/registry/...` passes `TestValidateFetchedPersonaPrompt_AllEmbeddedCommunityPersonasPass/simon` (subtest name derives from `t.Run` over `personas.CommunityNames()`, which now includes `simon`)
- [x] No linting errors (`gofmt`/`go vet` clean on unmodified Go test files; Markdown has no Go lint surface)
- [x] Build succeeds (`go build ./...`) — `go:embed community/*.md` picks up the new file automatically

**Story-Specific:**
- [x] All six mandatory section headings present in canonical order, with the exact 7-column `## Output Format` contract
- [x] Only allow-listed bare template tokens plus one `{{if .ToolsEnabled}}...{{end}}` block are used; no range/with/template/define/pipeline/field-chain
- [x] `## Focus` numbered list covers all four named anti-slop targets and contains the verbatim word `bloat`, not colliding with any of the 13 already-claimed category words
- [x] Exactly one `<!-- vendor-guidance: ... -->` citation line, and rendered length stays well under 8192 bytes

**Manual Review:**
- [ ] Code reviewed and approved
