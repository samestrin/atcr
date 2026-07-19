# Acceptance Criteria: Auto-Fix Sandboxed-by-Default Posture and `auto_fix:` Config Block Are Documented

**Related User Story:** [04: Document the Auto-Fix Sandbox Security Posture and `--no-sandbox` Risk](../user-stories/04-document-auto-fix-sandbox-security-posture.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (GitHub Flavored Markdown, `docs/` tree) | New `docs/auto-fix.md` (per Technical Considerations option (b): a single reference for the whole `auto_fix:` surface) or an added section in `docs/execution.md` (option (a)) — the file target is an open design decision per the story; this AC's content requirements apply regardless of which file holds them. |
| Test Framework | `cmd/atcr/docs_audit_test.go` (existing suite — `TestDocsIndexCoversEveryDoc`, `TestDocsReferenceOnlyRealCommands`, `TestDocsClaimedFlagsAreReal`) + manual review | This story is documentation-only and must NOT modify `cmd/atcr` Go code (including the audit test file itself, per Story Context "Constraints"); the existing suite audits every `docs/*.md` automatically because it globs `docs/*.md` at test time, so a new or edited file is covered without any test-code change. |
| Key Dependencies | `internal/registry/autofix.go` (`AutoFixConfig` struct — source of truth for field names/behavior), `docs/execution.md` (existing sandbox-guarantees prose to cross-link, not duplicate) | |

## Related Files
- `docs/auto-fix.md` - create: new page documenting the `--auto-fix` sandboxed-by-default posture and the full `auto_fix:` config block (`apply_target`, `validate_command`, `validate_timeout`), cross-linking `docs/execution.md` for the shared container-isolation model. (Alternative: append an equivalent section to `docs/execution.md` if that structural option is chosen instead — see Technical Considerations in the story.)
- `docs/README.md` - modify: add the new doc to the index (required — `TestDocsIndexCoversEveryDoc` fails CI if a new `docs/*.md` file is not linked from here); not required if the content is appended to the already-indexed `docs/execution.md` instead.
- `internal/registry/autofix.go` - reference only (read, not modified): `AutoFixConfig{ApplyTarget, ValidateCommand, ValidateTimeout}` and its `Validate()` method are the ground truth for the config-block documentation; the doc's field names and default-fallback claims must match this file exactly.
- `docs/execution.md` - reference only (read; optionally modify to add a forward cross-link from "What the sandbox guarantees" to the new auto-fix content, mirroring the story's cross-link requirement).

### Related Files (from codebase-discovery.json)

- `docs/auto-fix.md` — create (discovery `files_to_create`, structural option (b), based on `docs/execution.md`): hosts the sandboxed-by-default posture and the full `auto_fix:` config block. Alternative: `docs/execution.md:123` — update with an appended auto-fix section (option (a)); exactly one of the two is chosen at drafting time.
- `docs/README.md` — update: add the new page to the docs index (required by `TestDocsIndexCoversEveryDoc`, `cmd/atcr/docs_audit_test.go:468`) only if the new-file option is chosen.

## Happy Path Scenarios
**Scenario 1: Reader finds the sandboxed-by-default statement**
- **Given** a reader opens the documentation location this story creates/extends
- **When** they look for how `--auto-fix`'s post-apply validation step is isolated
- **Then** they find an explicit statement that validation runs inside the same `internal/sandbox` container isolation used by `--exec` (non-root user, all capabilities dropped, no-new-privileges, resource caps — memory/CPU/PID limits plus a wall-clock timeout), by default, with no flag required to opt in

**Scenario 2: Reader finds the auto-fix-specific mount-semantics distinction**
- **Given** the same documentation location
- **When** the reader reads the sandboxing description closely
- **Then** the doc distinguishes auto-fix's sandboxed validation from `--exec`'s read-only snapshot model: because validation runs against the already-patched working tree, the mount is writable rather than the read-only `/work` snapshot `--exec` uses, and this distinction is stated rather than left implied by silence (per the story's Risk Mitigation: describe what is specific to the auto-fix context, don't just restate `--exec`'s guarantees verbatim)
- **Clarification (refinement):** whether the auto-fix validation mount is writable or remains read-only is an explicit `/design-sprint` decision per plan.md ("may require a `RunSpec` extension or a distinct mount mode") and codebase-discovery (the default Go validation path already works against the read-only `/work` mount because `HOME`/`TMPDIR`/`XDG_CACHE_HOME`/`GOCACHE`/`GOTMPDIR` redirect into the writable `/scratch` tmpfs, `internal/sandbox/docker.go:127-131`). The doc must state the mount semantics *as actually shipped*: if the read-only mount is retained, it must instead explain that validation runs against the already-patched tree mounted read-only with build caches redirected to `/scratch`, and that validation commands writing into the tree itself (coverage profiles, codegen, `lint --fix`) are a documented limitation. Either way, the auto-fix-specific distinction — validation runs against the already-mutated working tree, not a pristine review snapshot — must be stated rather than implied.

**Scenario 3: Reader finds the full `auto_fix:` config block documented**
- **Given** the documentation location
- **When** the reader looks for how to configure `--auto-fix`'s validation behavior
- **Then** they find all three fields of the `auto_fix:` block — `apply_target`, `validate_command`, `validate_timeout` — each named and described, matching `internal/registry/autofix.go:AutoFixConfig` exactly (field names, YAML keys, and semantics)

## Edge Cases
**Edge Case 1: Defaults when the `auto_fix:` block is absent**
- **Given** a `.atcr/config.yaml` with no `auto_fix:` block at all
- **When** the reader checks what happens
- **Then** the doc states the block is optional and, per `AutoFixConfig`'s doc comment, a nil block falls back to defaults: apply target = repo root, validation command = the Go build default when a `go.mod` is present (`verify.ResolveValidateCommand`'s single built-in default)

**Edge Case 2: `validate_timeout` format and fallback**
- **Given** a reader configuring `validate_timeout`
- **When** they read the field's documentation
- **Then** they learn it is a Go duration string (e.g. `"2m"`), that an empty value inherits the gate's ~2 minute default, and that a zero or negative value is rejected at config-load time (not at run time) — matching `AutoFixConfig.Validate()`

**Edge Case 3: `docs/README.md` index requirement when a new file is created**
- **Given** the story creates a new `docs/auto-fix.md` file (structural option (b))
- **When** the sprint's docs-audit tests run
- **Then** `docs/README.md` must link to it, or `TestDocsIndexCoversEveryDoc` fails CI with "docs/README.md index does not link docs/auto-fix.md" — this is a hard, pre-existing gate this story must satisfy, not an optional nicety

## Error Conditions
**Error Scenario 1: Doc invents a config field that does not exist**
- Condition: the doc names a field other than `apply_target`, `validate_command`, or `validate_timeout` under `auto_fix:` (e.g. a fictional `auto_fix.no_sandbox`), or claims a YAML key name that does not match `AutoFixConfig`'s struct tags
- This is a content-accuracy failure caught only by manual review against `internal/registry/autofix.go` (no automated field-name checker exists in `cmd/atcr/docs_audit_test.go` for arbitrary config blocks); reviewers must diff the doc's claims against the struct before merge

**Error Scenario 2: Doc references a non-existent command or flag**
- Condition: the doc's code examples reference an `atcr` subcommand or `--flag` that is not real
- Error message (from CI, if triggered): `"%s references \`atcr %s\` but %q is not a real command"` or `"%s documents \`--%s\` as a flag but no such CLI flag exists"` (from `TestDocsReferenceOnlyRealCommands` / `TestDocsClaimedFlagsAreReal` in `cmd/atcr/docs_audit_test.go`)
- Exit status: `go test` non-zero, failing CI

## Performance Requirements
- **Response Time:** N/A (static documentation; no runtime execution path). The relevant "performance" surface is CI turnaround: the existing `cmd/atcr/docs_audit_test.go` suite (which now also parses this story's new/edited content) must continue to complete in the same sub-second-per-file order of magnitude as today — no new test code is added, so no new cost is introduced by this story.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — pure documentation, no runtime access control surface.
- **Input Validation:** The doc's claims are the "input" that matters here: it must not overstate the isolation guarantee (e.g., must not claim the working tree is read-only during auto-fix validation — it is not, unlike `--exec`'s snapshot) and must not omit the writable-mount distinction, since an operator who assumes `--exec`-equivalent read-only isolation could misjudge the actual blast radius of a malicious validation command mutating files outside the intended apply target.
- **Clarification (refinement):** whether the auto-fix mount ships writable or stays read-only (with `/scratch` env redirects covering build caches) is decided at `/design-sprint` (see Scenario 2's clarification); the requirement above is accuracy against the shipped semantics — the doc must not claim read-only if a writable mode ships, nor claim writable if read-only is retained.

## Test Implementation Guidance
**Test Type:** DOCS AUDIT (existing automated suite) + MANUAL (content accuracy)
**Test Data Requirements:** None beyond the live repository state — `cmd/atcr/docs_audit_test.go` reads the compiled Cobra command tree and the real `docs/*.md` files at test time; no fixtures are created by this story.
**Mock/Stub Requirements:** None. Do not add new test code to `cmd/atcr` (out of scope per the story's constraints — this story touches only `docs/`). Verification is: (1) run `go test ./cmd/atcr/...` and confirm the existing suite still passes against the new/edited doc content, and (2) a manual side-by-side read of the doc against `internal/registry/autofix.go` and `docs/execution.md`'s "What the sandbox guarantees" section to confirm no invented fields or overstated guarantees.

## Definition of Done

**Auto-Verified:**
- [ ] `go build ./...` succeeds (no Go source is touched by this story, so this simply confirms nothing else broke)
- [ ] `go test ./cmd/atcr/...` passes, including `TestDocsIndexCoversEveryDoc`, `TestDocsReferenceOnlyRealCommands`, and `TestDocsClaimedFlagsAreReal` against the new/edited doc
- [ ] No dangling markdown links introduced (verified by the same `TestDocsIndexCoversEveryDoc` link-target check)

**Story-Specific:**
- [ ] Doc states `--auto-fix` validation is sandboxed by default via `internal/sandbox` container isolation, with no flag required
- [ ] Doc distinguishes the writable-working-tree mount from `--exec`'s read-only snapshot rather than implying identical semantics
- [ ] Doc documents all three `auto_fix:` fields (`apply_target`, `validate_command`, `validate_timeout`) matching `internal/registry/autofix.go` exactly, including the nil-block and empty-field defaults
- [ ] If a new `docs/auto-fix.md` file was created, `docs/README.md` links it

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Content re-verified against Stories 1-3's final merged implementation immediately before this story's docs PR is finalized (per the story's Dependencies note and Potential Risks table)
