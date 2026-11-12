# Docs audit close-out (epic 111)

`docs-audit/inventory.md` is the audit record: what every documented command, flag,
default, config key, `ATCR_*` environment variable, output format and example was
graded against. This note records where that audit ended: the sha the docs now match,
the latest tag, the resolution of every mismatch, and the app-side defects that went
to the technical-debt list instead.

## Shas and tag

| What | Value |
|---|---|
| Audited baseline | `986327bcc17ad2c6dfe62b6cf3c77bfcb9adc1ae` — atcr `main` when epic 111 started (2026-11-10). The inventory's source, and the tree T1 graded. |
| Sha the docs now match | `b125b6f878ff66cbe7ccc6a1a4001a2daa893602` — `main` after epic 111 T6, the last commit that touched `README.md`, `docs/` or `CHANGELOG.md` (2026-11-12). The audit-close-out PR changes documentation prose nowhere, so the matched sha does not move. |
| Latest tag | `v1.0.0` — tag object `1d4402dbdab394d8bdf6fb24d308b464b3c5adf0`, dated 2026-11-03. It is 36 commits behind the matched sha. The docs are graded against the `main` build, not this tag, per the epic header; the next release is cut from `main` after this epic. |

The binary used for grading was built from source at each task's head
(`go build ./cmd/atcr`, `GOWORK=off`), and every claim was checked with `--help` for
the command paths involved, the config schema, real output, or the code that produces
it. The inventory's method section lists the probes.

## Resolution of every mismatch

| Row | Task | Commit | Result |
|---|---|---|---|
| M1 — README `atcr debt` row lists 6 of 18 subcommands | T2 | `6166fdd4` | Fixed: all 18 named, checked name-by-name against `atcr debt --help` |
| M2 — README uses `skill export --dir` as canonical | T2 | `6166fdd4` | Fixed: the row names `--dest`; `--dir` stays a hidden deprecated alias |
| M3 — README baseline heading and anchor use `--dir` | T2 | `6166fdd4` | Fixed: heading says `--scope`; link points at `#baseline-review---all----scope` |
| M4 — `docs/registry.md:166` names a `--diff-file` review | T4 | `27bea0db` | Fixed: the SAST bullet names the two range-less paths (`--all`/`--scope`, `atcr benchmark run`) |
| M5 — `docs/registry.md` uses `--dir` as canonical (4 sites) | T4 | `27bea0db` | Fixed: all four say `--scope` |
| M6 — `docs/verification.md:110` names a `--diff-file` review | T5 | `76d24dd6` | Fixed: names the two reachable headless causes |
| M7 — `docs/personas-install.md:15` macOS path | T4 | `27bea0db` | Fixed: `~/.config/atcr/personas/` on every OS, macOS included |
| M8 — `docs/code-review-backend.md:59` says `--all / --dir` | T4 | `27bea0db` | Fixed: `--all / --scope` |
| M9 — `## [1.0.0]` dated 2026-10-30 | T3 | `d87332cb` | Fixed: `## [1.0.0] - 2026-11-03`, matching the tag date |
| M10 — released v1.0.0 fragments under Unreleased | T3 | `d87332cb` | Fixed: the 42 fragments are under `## [1.0.0]` in category order and deleted; only `changelog.d/README.md` is in both trees |
| M11 — CHANGELOG says `atcr skill install` | T3 | `d87332cb` | Fixed: `atcr skill export` |
| M12 — CHANGELOG names bare `atcr resume` | T3 | `d87332cb` | Fixed: `atcr review --resume` |
| M13 — release note records `--dir` guidance the app still prints | T2, T3 | `6166fdd4`, `d87332cb` | Doc kept (it records real output); the stale app strings are filed as TECH DEBT on EST-6066 and EST-6067 |
| T5-1 — `docs/payload-modes.md:203` broken `#grounding` anchor | T5 | `76d24dd6` | Fixed: `#grounding-gate` |
| T5-2 — `docs/personas-authoring.md:38` broken registry anchor | T5 | `76d24dd6` | Fixed: the full heading slug |
| T5-3 — `docs/technical-debt.md:1388` names `--dir` as the replacement flag | T5 | `76d24dd6` | Fixed: names `--store`, notes the deprecated alias |

Two pages T6 repaired had already been graded PASS for their claims as a whole
(`docs/benchmark.md`, `docs/technical-debt.md`); the fix is part of T6's `b125b6f8`
and does not open a new inventory row.

Every `MISMATCH` cell in the inventory now carries the fixing task, so the file has no
open mismatch. The inventory's per-file extraction tables keep the baseline extraction
for `README.md`, `CHANGELOG.md` and the pages T6 edited; their `Result` cells and the
rows above carry the resolution.

## App-side defects (filed, not fixed here)

Epic tasks post `TECH DEBT` comments instead of editing the shared table, because the
parallel epic PRs would conflict; skill `close-epic` collects these rows into the
technical-debt list.

| Issue | File | Problem |
|---|---|---|
| EST-6066 (T2), EST-6067 (T3) | `cli/skill.go:77,85,118` | User-facing errors recommend the hidden deprecated `--dir` alias instead of canonical `--dest` (the app side of M13). |
| EST-6066 (T2) | `cli/debt.go:22,32,34` | Long help says "Use `--dir`" where `--store` is canonical, and its inline list names 13 of the 18 debt subcommands. |
| EST-6069 (T5) | `internal/verify/pipeline.go:624`, `internal/fanout/threatmap.go:26`, `internal/fanout/visual_stage.go:20` | Three user-facing strings name a `--diff-file` review mode that no command has. |

T4 found no app-side error: its four rows were documentation mistakes about
deliberate app behavior. None of those rows is already in the technical-debt list.

## Guards now in CI

`cli/docs_audit_test.go` (epic 111 T6, `b125b6f8`) fails CI when:

- a page in `README.md`, `docs/` or the CHANGELOG names an `atcr` command or flag the
  compiled command tree does not have (`TestDocsReferenceOnlyRealCommands`,
  `TestDocsClaimedFlagsAreReal`);
- a page names an `ATCR_*` variable the code does not read (`TestDocsNameOnlyRealEnvVars`),
  or a config block names a key the schema does not have
  (`TestDocsConfigBlocksNameOnlyRealKeys`);
- one of the 57 documented hermetic examples fails when run
  (`TestDocsHermeticExamplesRun`).

Each guard has a companion test that feeds it a deliberate fake and asserts it fails
(`TestDocsReferenceOnlyRealCommandsCatchesAFakeCommandAndFlag`,
`TestUndocumentedEnvVarsCatchesAFakeName`, `TestConfigBlockErrorsCatchesAFakeKey`,
`TestRunDocExampleFailsABrokenExample`, `TestDocExampleTelemetryEscapeFails`), so a
guard that stops detecting a break is itself a red test. Technical-debt table rows are
excluded from the docs-claim scan (`TestDebtTableRowsAreNotDocsClaims`), so the shared
list can carry stale prose without failing the build.

The remaining 177 fenced examples are skipped for a concrete reason recorded in the
inventory (provider key, network, live roster, external tool, or a non-executable
illustration). A skip is not a pass; those examples are still covered by the static
command, flag, config-key and env-var guards.

To re-run the audit's guard locally:

```bash
GOWORK=off go test ./cli/ -run 'TestDocs|TestUndocumentedEnvVars|TestConfigBlockErrors|TestDebtTableRows' -count=1
```

`go test ./cli/...` runs the same guards in the full CI `cli` shard.