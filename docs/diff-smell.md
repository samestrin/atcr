# Diff-Smell

Skeptic verification asks *"is this finding real?"*. Diff-smell asks a different,
purely mechanical question: **did this patch cheat?**

It scans a unified diff for the fingerprints of an over-simplified
("reward-hacked") change — one that makes a failing test pass by deleting the
test rather than fixing the code, or clears a lint by suppressing it.

It is **deterministic and model-free**: same diff in, same verdict out. No agent,
no provider, no API key, no network. That makes it cheap enough to run on every
commit and safe to run in a sandbox with no credentials.

The same analyzer already gates atcr's own auto-fix pipeline — a model-produced
fix that trips a HARD smell is rejected and retried before it is ever written.
`atcr verify diff` is that analyzer exposed as a command, so a patch judged
here is judged by identical logic.

## Usage

```bash
atcr verify diff                             # scan HEAD (the default)
atcr verify diff --rev HEAD~1                # scan a single commit
atcr verify diff --staged                    # scan the git index
atcr verify diff --diff patch.diff           # scan a file
git diff | atcr verify diff --diff -         # scan stdin
```

### Diff sources

Exactly one source may be named. Naming none falls through to `--rev`'s `HEAD`
default, so a bare `atcr verify diff` scans the last commit.

> **A review id or path named `diff` is shadowed.** `verify` is both a group and a
> leaf — it takes an optional `[id-or-path]` — and cobra resolves a matching
> subcommand before positional args. So `atcr verify diff` always reaches this
> scanner. To verify a review whose id happens to be `diff`, pass it after `--`:
> `atcr verify -- diff`.

| Flag | Source |
|------|--------|
| `--rev <rev>` | a single commit in `--repo-root` (default: `HEAD`) |
| `--staged` | the staged changes in `--repo-root` |
| `--diff <path>` | a unified diff read from a file |
| `--diff -` | a unified diff read from stdin |

`--repo-root <path>` sets the repository root for the two git-backed sources
(default: the current directory). `--repo` remains a deprecated hidden alias.

**Merge commits** are diffed against their **first parent** (`--first-parent -m`),
so `--rev <merge>` reports what the merge introduces to the branch it lands on. A
plain `git show` prints nothing for a merge, which would make the gate a silent
no-op precisely where CI runs it — `--rev` defaults to `HEAD`, and both
`refs/pull/N/merge` checkouts and post-merge branches have a merge there.
Single-parent commits are unaffected.

Naming two sources is a usage error (exit `2`) that names both offenders, rather
than a silent precedence win.

Git is always invoked with `--no-ext-diff`, `--no-textconv`, and `--no-color`.
The first two matter for safety, not formatting: `--repo-root <path>` invites pointing
the scanner at a tree you do not control, and a repo-local `diff.external` **or**
a `[diff "x"] textconv = <program>` driver (armed by a worktree `.gitattributes`)
would otherwise execute an arbitrary program with your privileges. They are
separate vectors — disabling one does not disable the other. `--no-color` keeps a
`color.ui = always` config from feeding ANSI escapes to a parser expecting a plain
unified diff.

**Size cap.** Every source refuses input over **2 MiB** with a "too large to
scan" usage error (exit `2`). The relocation check compares each added line
against each removed line in the same file, so scan cost grows quadratically with
input size — an unbounded diff costs minutes of CPU, not just memory. The cap
clears any realistic commit diff; split a larger one, or scan its commits
individually with `--rev`.

## Smells

| Smell | Severity | What it catches |
|-------|----------|-----------------|
| `test_only` | hard | the change touched only test files — no implementation change |
| `test_deleted` | hard | a test file was deleted outright |
| `test_skipped` | hard | a skip/ignore marker was added |
| `test_renamed_away` | hard | a test file was renamed to a non-test path, disabling it |
| `weakened_assertion` | hard / soft | assertions removed without replacement (hard), or replaced one-for-one (soft — verify they were not weakened) |
| `suppression` | soft | a linter/type-checker suppression was added |
| `empty_catch` | soft | an empty or swallowing exception handler was added |
| `stub_body` | soft | a stub / not-implemented / standalone TODO-or-FIXME body was added |

`test_skipped` recognizes `t.Skip(`, `t.Skipf(`, `@pytest.mark.skip`, `it.skip(`,
`test.skip(`, `describe.skip(`, `context.skip(`, `xit(`, `xdescribe(`, `@Ignore`,
`@Disabled`, and `#[ignore]`. `suppression` recognizes `//nolint`, `@ts-ignore`,
`@ts-expect-error`, `eslint-disable`, `# type: ignore`, `# noqa`,
`# pylint: disable`, `# pragma: no cover`, `# nosec`, `@SuppressWarnings`,
`# rubocop:disable`, and `@phpstan-ignore`.

A line that also appears verbatim among the same file's removed lines is treated
as **relocated, not introduced**, so the per-added-line smells (`test_skipped`,
`suppression`, `empty_catch`, `stub_body`) do not fire on moved or reindented
code. The guard does not extend to `weakened_assertion`, which counts assertions
per file: a relocated assertion still reads as a one-for-one replacement and
raises the SOFT verdict.

### The three atcr-only HARD smells

atcr's analyzer is strictly stronger than the upstream `llm-tools` `diff-smell`
it was ported from. Three HARD smells exist only here, and each closes a case
where upstream scored a blatant reward hack as `clean`:

- **`test_deleted`** — upstream has no deletion detector. It relies on
  `test_only`, which cannot fire when the same diff also edits an implementation
  file. So "delete the failing test, tweak the code" scored clean.
- **`test_skipped`** — upstream has no skip detector, so `t.Skip("flaky")` added
  alongside a real change scored clean. This is the cheapest way there is to make
  a failing test pass.
- **`test_renamed_away`** — upstream records only the `b/` side of the
  `diff --git` header, so `foo_test.go` → `foo_disabled.go` scored clean *and*
  counted the result as an implementation file, which additionally suppressed
  `test_only` for everything else in the diff.

A consumer calling atcr gets all three. A consumer copying upstream gets none of
them, and starts its own drift.

## Verdicts and exit codes

The verdict is `hard` if any hard smell fired, `soft_only` if only soft smells
fired, else `clean`.

Gating is **opt-in**. Without `--fail-on`, the command reports the verdict and
exits `0` for every verdict — preserving drop-in parity with upstream
`diff-smell`, which never exits nonzero on content.

- **`--fail-on hard`** — exit `1` on a `hard` verdict.
- **`--fail-on soft`** — exit `1` on `hard` or `soft_only`.
- **`--fail-on none`** — explicit no-op, so a script can always pass the flag and
  vary only its value.
- **`--fail-on ""`** — an empty or whitespace-only value is also unset, so
  `atcr verify diff --fail-on "$LEVEL"` is a no-op when `LEVEL` is unset rather
  than a usage error. This matches `atcr verify --min-severity` and
  `atcr reconcile --fail-on`.

| Exit | Meaning |
|------|---------|
| `0` | scan completed (any verdict, unless `--fail-on` tripped) |
| `1` | the `--fail-on` gate tripped |
| `2` | usage error — see the full list below |

Exit `2` covers every one of:

- a bad `--fail-on` value (an empty one is *unset*, not an error)
- an unreadable `--diff` file, or a git failure
- two named diff sources
- input over the size cap ("too large to scan")
- an explicitly empty `--rev` or `--diff` value
- a `--rev` value that starts with `-`

That last rule is a deliberate argument-injection guard: without it, `--rev
--output=/tmp/pwn` alone would be enough to make the scan write a file of the
caller's choosing. `--rev` is additionally passed after git's `--end-of-options`
and before a `--` separator, so a path-shaped value such as `--rev foo.go` is
refused rather than silently reinterpreted as a pathspec.

An invalid `--fail-on` value is rejected **before any git process is spawned**. A
tripped gate still prints the full report, so one run tells you both that it
failed and why.

> `--fail-on` here takes **verdicts** (`hard`, `soft`), while `atcr review`,
> `atcr reconcile`, and `atcr github` take **severities**
> (`CRITICAL`/`HIGH`/`MEDIUM`/`LOW`). Same concept — "exit 1 at or above this" —
> with disjoint value sets, so a severity passed here is a usage error rather
> than a silent misread.

## Clean, not an error

Two inputs report `clean` with an explanatory note on **stderr** rather than
failing:

- **An empty diff.** An empty staged set is a normal git state in a pre-commit
  hook. (This differs from `atcr review`, where an empty range means
  misconfiguration and is a hard error — see [ci-integration.md](ci-integration.md).)
- **Content that is not a unified diff.** Reported clean without scanning,
  matching upstream's never-fail-on-content behavior.

Both keep stdout payload-only, so `--json` output stays parseable in every case.

## Text output

Without `--json`, stdout carries a human summary: one greppable verdict line,
then one indented row per smell, citing `file:line` where the smell has one.

A scan with findings (the same input as the JSON example below):

```text
verdict: hard — 1 hard, 2 soft smell(s); 1 test file(s), 1 impl file(s)
  HARD test_skipped       pkg/auth_test.go:12: t.Skip("flaky")
  SOFT weakened_assertion pkg/auth_test.go: test replaced assertion(s) one-for-one; verify they were not weakened
  SOFT suppression        pkg/auth.go:21: //nolint:errcheck
```

A clean scan:

```text
verdict: clean — no over-simplification smells; 0 test file(s), 1 impl file(s)
```

The separator on the verdict line is a U+2014 em dash — that is the shipped
shape, recorded here so scripts match on the `verdict: ` prefix rather than the
full line. The text format is best-effort human output and is **not** part of
the stability contract below; pin to `--json` for anything a script parses.

## JSON output

`--json` emits the result as the sole content of stdout. Field names are
upstream's, so a consumer already parsing `llm-support diff-smell --json` can
repoint at atcr without reshaping.

Given this diff:

```diff
diff --git a/pkg/auth_test.go b/pkg/auth_test.go
--- a/pkg/auth_test.go
+++ b/pkg/auth_test.go
@@ -10,2 +10,3 @@ func TestAuth(t *testing.T) {
 	setup()
-	require.Equal(t, want, got)
+	require.NotNil(t, got)
+	t.Skip("flaky")
diff --git a/pkg/auth.go b/pkg/auth.go
--- a/pkg/auth.go
+++ b/pkg/auth.go
@@ -20,1 +20,2 @@
 func Auth() error {
+	//nolint:errcheck
```

`atcr verify diff --json` emits:

```json
{
  "files": {
    "test": [
      "pkg/auth_test.go"
    ],
    "impl": [
      "pkg/auth.go"
    ]
  },
  "smells": [
    {
      "type": "test_skipped",
      "severity": "hard",
      "file": "pkg/auth_test.go",
      "line": 12,
      "evidence": "t.Skip(\"flaky\")"
    },
    {
      "type": "weakened_assertion",
      "severity": "soft",
      "file": "pkg/auth_test.go",
      "evidence": "test replaced assertion(s) one-for-one; verify they were not weakened"
    },
    {
      "type": "suppression",
      "severity": "soft",
      "file": "pkg/auth.go",
      "line": 21,
      "evidence": "//nolint:errcheck"
    }
  ],
  "summary": {
    "test_files": 1,
    "impl_files": 1,
    "hard": 1,
    "soft": 2,
    "by_type": {
      "suppression": 1,
      "test_skipped": 1,
      "weakened_assertion": 1
    },
    "verdict": "hard"
  }
}
```

Notes on the shape:

- `line` is the **new-file** line number of the offending added line. Only the
  added-line detectors (`suppression`, `empty_catch`, `stub_body`,
  `test_skipped`) can name one; the file-level and count-derived smells
  (`test_only`, `test_deleted`, `test_renamed_away`, `weakened_assertion`) omit
  the key entirely rather than emit a misleading `0`.
- Empty collections render as `[]` / `{}`, never `null`, so a consumer indexing
  a field never has to nil-check one that is merely empty.

## Stability contract

The moment a consumer pins to `atcr verify diff --json`, that shape *is* its API.
So it is stated deliberately rather than becoming a contract by accident.

**Committed:**

- the three top-level keys — `files`, `smells`, `summary`
- the smell object's field names — `type`, `severity`, `file`, `line`, `evidence`
- the closed verdict set — `clean`, `soft_only`, `hard`
- the closed severity set — `hard`, `soft`
- the exit mapping in the table above

**Additive and non-breaking:** adding a new smell **type**. A consumer keys on
`severity` and `summary.verdict`, both closed sets, so a new type name flows
through existing logic without a change.

**Breaking** — renaming a key, removing a field, or changing the verdict or
severity vocabulary. Each earns a `CHANGELOG.md` **Breaking** entry.

`TestSmellResult_GoldenJSON` in `internal/verify/diffsmell_contract_test.go` is
the mechanical guard: it pins the exact marshaled document above, so the shape
cannot drift silently through a refactor.

## Version discoverability

`v0.1.0` does **not** contain this command; it ships in the first tagged release
after that. Rather than pinning a version, probe for the subcommand and degrade
gracefully — a consumer should skip the gate with a warning, not hard-fail, when
running against an older atcr:

```bash
if atcr verify diff --help 2>&1 | grep -- '--fail-on' >/dev/null; then
  atcr verify diff --staged --fail-on hard
else
  echo "warning: atcr is too old for 'verify diff'; skipping the diff-smell gate" >&2
fi
```

**Probe the help TEXT, not the exit status.** `verify` is both a group and a leaf
— it takes an optional `[id-or-path]` — so on an older atcr cobra reads `diff` as
a positional and `--help` short-circuits to `verify`'s own help, exiting `0`. So
does `atcr help verify diff`. An exit-status probe therefore reports "new enough"
on *every* binary ever shipped, and the guarded call then dies with
`unknown flag: --staged`. `--fail-on` is registered only on `verify diff`, never
on `verify`, so grepping the help output for it is what actually separates the
two.

**Use `grep … >/dev/null`, not `grep -q`.** `-q` exits at the first match and
closes the pipe, so `atcr` is killed by `SIGPIPE` and reports `141`. Under the
`set -o pipefail` that the hook below (and most CI shells) enable, that failure
becomes the pipeline's status — and the probe reports "too old" on a *new* atcr,
silently skipping the gate. Letting `grep` drain its input avoids the signal
entirely.

`atcr version` reports the installed version if you would rather compare
directly.

## Pre-commit hook example

```bash
#!/usr/bin/env bash
# .git/hooks/pre-commit — block a staged reward hack before it becomes a commit.
set -euo pipefail

if ! atcr verify diff --help 2>&1 | grep -- '--fail-on' >/dev/null; then
  exit 0   # older atcr: no gate, no failure
fi

atcr verify diff --staged --fail-on hard
```

An empty index exits `0` with a note, so this is a no-op on a
nothing-staged commit rather than a spurious block.

## Relationship to upstream `llm-support diff-smell`

atcr's analyzer is a **port** of `llm-tools`' `diff-smell`
(`internal/support/commands/diff_smell.go`), copied rather than imported because
the upstream function lives under that module's own `internal/`. Both depend only
on the Go standard library.

| | upstream `llm-support diff-smell` | `atcr verify diff` |
|---|---|---|
| Verdict vocabulary | `clean` / `soft_only` / `hard` | identical |
| JSON field names | `type`, `severity`, `file`, `line`, `evidence`, … | identical |
| `--json` default | on | **off** — atcr's convention is a human default with an opt-in `--json` |
| HARD smells | 2 | 5 (`test_deleted`, `test_skipped`, `test_renamed_away` added) |
| Exit on findings | always 0 | 0 by default, opt-in via `--fail-on` |

Drift is tracked **one-way, by deliberate choice**: `internal/verify/testdata/diffsmell/`
holds a corpus of diffs with the verdicts atcr's own analyzer must produce. It is
not automatically verified against upstream — a two-way check would have to vendor
the upstream analyzer or shell out to an installed `llm-support` binary,
reintroducing exactly the cross-module coupling the port exists to avoid. The
`internal/verify/diffsmell.go` header records the upstream commit the port came
from and every deliberate divergence.

## Related documents

- [verification.md](verification.md) — adversarial skeptic verification, the
  model-driven counterpart that shares the `verify` namespace.
- [ci-integration.md](ci-integration.md) — the authoritative exit-code table.
- [agentic-consumption.md](agentic-consumption.md) — the payload-only stdout rule
  this command obeys.
