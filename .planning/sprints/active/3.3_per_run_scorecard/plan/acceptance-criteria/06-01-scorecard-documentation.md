# Acceptance Criteria: Scorecard Documentation

**Related User Story:** [06: Document Scorecard Schema and Usage](../user-stories/06-document-scorecard.md)

## Acceptance Criteria Statement
`docs/scorecard.md` documents the v1 scorecard record schema, monthly JSONL storage layout, `atcr scorecard`/`atcr leaderboard` CLI usage, the `--no-scorecard` suppression flag, and the public submission privacy model.

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Documentation | Markdown in `docs/scorecard.md` | Human-readable reference and onboarding doc |
| Schema Examples | JSON code blocks | Illustrate v1 internal record and v1 public submission record |
| Validation | Manual review + link check | Confirm all internal links resolve |

### Related Files (from codebase-discovery.json)
- `docs/scorecard.md` — create: the primary documentation artifact
- `internal/scorecard/scorecard.go` — reference: source of truth for v1 internal schema
- `internal/scorecard/export.go` — reference: source of truth for v1 public submission schema
- `cmd/atcr/scorecard.go` — reference: CLI usage for single-run view
- `cmd/atcr/leaderboard.go` — reference: CLI usage for leaderboard and export

## Happy Path Scenarios

**Scenario 1: Schema reference**
- **Given** a developer reads `docs/scorecard.md`
- **When** they review the "Record Schema" section
- **Then** they find a JSON example of a v1 per-reviewer record, a table describing every field (`schema_version`, `run_id`, `reviewer`, `model`, `role`, `findings_raised`, `findings_corroborated`, `findings_solo`, `corroboration_rate`, `findings_verified`, `findings_refuted`, `survived_skeptic_rate`, `cost_usd`, `tokens_in`, `tokens_out`, `latency_ms`), and notes that verification fields are omitted when `verification.json` is absent

**Scenario 2: Storage layout**
- **Given** a developer wants to inspect or delete local scorecard data
- **When** they read the "Storage" section
- **Then** they see that records are stored in `~/.config/atcr/scorecard/YYYY-MM.jsonl`, that the file is append-only and monthly-rotated, and that old files can be deleted manually

**Scenario 3: CLI usage examples**
- **Given** a developer wants to use the new commands
- **When** they read the "CLI Usage" section
- **Then** they find examples for:
  - `atcr scorecard <run-id>`
  - `atcr scorecard <path>`
  - `atcr leaderboard`
  - `atcr leaderboard --since 7d --model claude-sonnet-4-6 --persona bruce`
  - `atcr leaderboard --export`
  - `atcr leaderboard --export --output /tmp/submission.json`
  - `atcr reconcile --no-scorecard`

**Scenario 4: Privacy model**
- **Given** a developer wants to submit to the public Model-Eval Leaderboard
- **When** they read the "Privacy Model" section
- **Then** they see an explicit allowlist of preserved fields (model, persona/role, numeric metrics) and a list of stripped data (provider API keys, repo paths, repo content, hostnames, usernames, organization names, `run_id`)

## Edge Cases

**Edge Case 1: Schema versioning**
- **Given** a future epic bumps `schema_version`
- **When** the docs are updated
- **Then** the document explains that old records remain readable and that version negotiation is handled by the leaderboard/export commands

**Edge Case 2: Missing verification data**
- **Given** `atcr reconcile` ran without `atcr verify`
- **When** the developer reads the schema section
- **Then** they understand that `findings_verified`, `findings_refuted`, and `survived_skeptic_rate` are omitted and that the leaderboard table omits the corresponding columns

## Error Conditions

*No runtime error conditions apply to documentation.*

## Performance Requirements

*Not applicable.*

## Security Considerations

- **Accuracy:** The privacy model must match the implementation in `internal/scorecard/export.go`. Any discrepancy is treated as a documentation bug.
- **No secrets:** Documentation examples must use synthetic data; no real provider keys, repo paths, or PII.

## Test Implementation Guidance

**Test Type:** MANUAL

**Manual Review Checklist:**
- [ ] `docs/scorecard.md` exists and renders as valid Markdown (no broken links or syntax errors)
- [ ] All v1 internal record fields are documented with type and description
- [ ] Verification-conditional fields are marked as optional
- [ ] Storage location and rotation behavior are documented
- [ ] Each new CLI command/flag has a usage example
- [ ] Privacy model lists preserved and stripped fields explicitly
- [ ] All relative links to user stories and acceptance criteria resolve

## Definition of Done

**Auto-Verified:**
- [ ] `docs/scorecard.md` is present in the repository
- [ ] Markdown link checker (if available) reports no broken relative links

**Story-Specific:**
- [ ] Schema v1 is documented with a complete JSON example
- [ ] Storage location and monthly rotation are documented
- [ ] `atcr scorecard`, `atcr leaderboard`, and `--export` usage examples are present
- [ ] `--no-scorecard` suppression is documented
- [ ] Privacy model / anonymization rules are documented
- [ ] Epic 10.0 submission context is mentioned

**Manual Review:**
- [ ] Documentation reviewed for technical accuracy against implemented behavior
- [ ] Privacy section reviewed for completeness and safety
