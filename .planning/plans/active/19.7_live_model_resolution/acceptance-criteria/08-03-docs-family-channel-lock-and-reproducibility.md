# Acceptance Criteria: Docs Document Family/Channel/Lock Model and Reproducible-vs-Upgrade Behavior

**Related User Story:** [08: Catalog Snapshot Fixture, Refresh Command & Documentation](../user-stories/08-catalog-snapshot-refresh-command-and-docs.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation updates (`docs/personas-authoring.md`, `docs/personas-install.md`) | Additive sections, no new files required |
| Test Framework | Manual review / markdown lint | No code tests |
| Key Dependencies | Plan documentation in `documentation/` | Docs should link to implementation details rather than duplicate them |

### Related Files (from codebase-discovery.json)
- `docs/personas-authoring.md` — modify: add a section describing the family/channel/lock schema for persona authors and maintainers.
- `docs/personas-install.md` — modify: add sections for `atcr personas upgrade` lock reporting, `atcr models check`, and the reproducibility guarantee.
- `docs/` (other as needed) — modify: cross-links from relevant pages.
- `documentation/openrouter-catalog-api.md` — reference: link to implementation details on the catalog schema, alias behavior, and deprecation signal.
- `documentation/models-check-command.md` — reference: link to the `atcr models check` command design, exit codes, and `--json` output shape.
- `documentation/existing-resolver-patterns.md` — reference: link to the `fetch()`/`Upgrade()`/`isNewer()` reuse seams, command registration, and reproducibility posture.
- `documentation/catalog-snapshot-fixture.md` — reference: link to the checked-in fixture and refresh command rationale.

## Happy Path Scenarios

**Scenario 1: Persona authoring docs explain the binding/lock schema**
- **Given** a maintainer reads `docs/personas-authoring.md`
- **When** they reach the new "Model family/channel bindings and resolved locks" section
- **Then** they understand that a persona may declare a logical binding such as `anthropic/claude-opus@stable`, that atcr stores a resolved concrete slug as a lock, and that the lock is what reviews actually run

**Scenario 2: Install/upgrade docs explain the user-facing commands**
- **Given** an end user reads `docs/personas-install.md`
- **When** they reach the new sections
- **Then** they understand that `atcr personas upgrade` is the only command that advances the lock, that it prints a `name: old-slug → new-slug` report, that major jumps may be gated by a fixture re-pass, and that `atcr models check` reports drift/deprecation/missing-slug conditions without changing anything

**Scenario 3: Docs link to implementation details instead of duplicating them**
- **Given** a reader wants the exact schema or command exit-code contract
- **When** they follow the links in the new doc sections
- **Then** they are directed to the plan's `documentation/` files (e.g., `models-check-command.md`, `openrouter-catalog-api.md`, `existing-resolver-patterns.md`)

## Edge Cases

**Edge Case 1: Existing on-disk personas from before Epic 19.7**
- **Given** a user has personas installed before the family/channel schema existed
- **When** they read the docs
- **Then** they see a note that 19.6's pinned `model` value seeds the initial lock with zero migration required

**Edge Case 2: Reproducibility guarantee vs. explicit upgrade**
- **Given** a user is concerned about silent model changes
- **When** they read the docs
- **Then** they see an explicit statement that reviews run the locked slug deterministically and that the resolver/catalog endpoint is never touched on the review hot path

## Error Conditions

**Error Scenario 1: Documentation links to a plan file that no longer exists**
- Behavior: this AC is not satisfied; links must be verified to point to existing `documentation/*.md` files

## Performance Requirements
- N/A

## Security Considerations
- N/A

## Test Implementation Guidance
**Test Type:** Documentation review
**Test Data Requirements:** N/A
**Mock/Stub Requirements:** N/A

## Definition of Done
**Auto-Verified:**
- [ ] Markdown files render without syntax errors

**Story-Specific:**
- [ ] `docs/personas-authoring.md` contains an additive section explaining family/channel bindings, the resolved lock, and the zero-migration seeding from 19.6's `model` field
- [ ] `docs/personas-install.md` contains additive sections for `atcr personas upgrade` lock reporting, the major-bump verify flag, `atcr models check`, and the reproducibility guarantee
- [ ] New doc sections link to the relevant plan `documentation/*.md` files instead of duplicating implementation details
- [ ] All links in the updated docs resolve to existing files

**Manual Review:**
- [ ] Docs reviewed and approved
