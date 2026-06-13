# Epic 1.7 — Real Review Run Verification: Run Evidence

**Recorded:** 2026-06-12 | **Branch:** `feature/epic-1.7-real-review-run-verification`
**Authorization:** live-provider token spend approved (clarification 2026-06-12); primary provider litellm `http://192.168.68.109:4000/v1`.
**Provider config:** personal `~/.config/atcr/registry.yaml` (uncommitted; reduced roster bruce+dax) + local `.atcr/config.yaml` (uncommitted, git-excluded).

This file is the committed, durable evidence for the three Story-05 manual gates. The full review artifacts live locally under `.atcr/reviews/` (uncommitted by design — provider/run artifacts are a personal-registry concern per plan/plan.md:171).

## Pre-flight (Epic 1.2 `atcr doctor`)

`atcr doctor` validated every roster endpoint before any review token spend (exit 0). Final roster after model selection (below): `bruce → llm-large` (728ms), `dax → gpt-oss-20b` (1485ms), both `ok`.

## AC 05-03 — Orchestration loop verified end-to-end ✅

Authoritative run: **`2026-06-12_epic-1.7-realrun-local`** (`status: completed`, `partial: true`).
Target: `--merge-commit 19c92246ab8914681ca5e59d4c447d092c50b392` (PR #1 squash-merge — atcr 1.0 core). Range resolved base `e5c4c5b` → head `19c9224`, 1 commit.

Full loop exercised, every step real:
1. `atcr range --merge-commit 19c9224…` → resolution JSON, non-empty.
2. `atcr review --merge-commit 19c9224… --byte-budget 100000` (background) → review id captured.
3. `atcr status <id>` polled (10s interval) → `completed`.
4. Host (+1) review written: `sources/host/findings.txt` (8-column v1, 3 findings) + `sources/host/review.md`.
5. `atcr reconcile <id>` → "reconciled 3 finding(s) from 2 source(s)".
6. `atcr report <id> --format md` → report rendered (3 LOW findings, all confidence MEDIUM, reviewers: host).
7. Review directory: `.atcr/reviews/2026-06-12_epic-1.7-realrun-local/`.

Pool outcome: `dax` (gpt-oss-20b) succeeded (66s, 0 findings); `bruce` (llm-large) failed on context-window — so the run also exercised the **partial-failure branch** (one agent fails, one succeeds, reconcile still proceeds, `partial: true` surfaced), which is part of the AC 05-03 orchestration contract.

### Model-selection note (real-world finding, captured to TD)

Two earlier runs failed on provider capacity, not on atcr:
- Run 1 (default 512 KB budget, roster qwen-3.7-plus + deepseek-4): **all agents failed** — deepseek-4 HTTP 400 (payload 155,296 tokens vs plan cap 49,152) and qwen-3.7-plus 360s timeout.
- Run 2 (`--byte-budget 100000`, same roster): context error cleared for deepseek but it then hit HTTP 429 (plan concurrency limit: 4 units, each request costs 4); qwen-3.7-plus timed out again at 360s.
- Run 3 (`--byte-budget 100000`, roster swapped to local `llm-large` + `gpt-oss-20b`): **completed**.

Captured as TD: the default 512 KiB byte budget yields ~155k-token payloads that exceed common model context/plan limits, and `atcr doctor`'s trivial-nonce probe does not catch it (see TD MEDIUM `internal/payload/budget.go`).

## AC 05-04 Scenario 1 — Adversarial tone verified ✅

`sources/host/review.md` inspected by an **independent reviewer subagent** (it did not write the review — no host self-certification), applying AC 05-04 Scenario 1's checks against the AC file's exact wording.

**Verdict: PASS.** No praise/compliments/positive observations; all five "no issues found in <area>" statements phrased neutrally (no smuggled quality adjectives); every section ties to a concrete finding or a neutral absence statement. (AC Scenario 1: findings "do not include praise, compliments, or positive observations"; review.md has "no praise-only content".)

The host review's three findings (all LOW, defensive/edge-case) are captured to TD:
- `internal/reconcile/ambiguous.go:166` — `AmbiguousHash` swallows a JSON render error and returns "".
- `internal/fanout/outcome.go:52` — `summarize` switch has no default arm (non-exhaustive tally).
- `internal/payload/budget.go:81` — an all-dropped payload is reviewed as empty with no distinct signal.

## AC 05-04 — Adjudication sensibility verified ✅ (seeded-corpus fallback)

The authoritative run produced an empty `ambiguous.json` (`[]`) — no gray-zone clusters (the host's findings are at distinct locations; the pool emitted none). Per clarification 5 and Success Criteria, the **seeded-corpus fallback** was exercised through real `atcr reconcile`.

Seed: **`2026-06-12_epic-1.7-adjudication-seed`** — two sources (`agent-a`, `agent-b`) with same-location finding pairs hand-crafted into the Jaccard 0.4–0.7 band:
- `internal/example/config.go:40` — similarity **0.556** (same null-deref, different wording) → gray.
- `internal/example/validate.go:100` — similarity **0.455** (input-length vs auth-token-format validation) → gray.

Initial reconcile recorded both as ambiguous, `clusters_collapsed: 0` (conservative unmerged default). Authored `reconciled/adjudication.json` (`baseline_hash` copied verbatim from `summary.json` `ambiguous_hash`):
- `config.go:40` → **merge** (same underlying null-deref) → collapsed to 1 finding, `reviewers: agent-a,agent-b`, `CONFIDENCE: HIGH`.
- `validate.go:100` → **distinct** (genuinely different validations) → kept as 2 separate findings.

Re-reconcile applied the decisions (4 → 3 findings), preserved the pre-adjudication sidecar as `ambiguous.original.json` (2 clusters), and left the distinct pair recorded in `ambiguous.json` (1 cluster). A second re-run was **idempotent** (identical 3 findings, `clusters_collapsed: 1`). Merge/distinct decisions are sensible and the audit chain is intact.

## Gate closure

- AC 05-03 Manual Review item ticked (`05-03-orchestration-loop.md`).
- AC 05-04 Manual Review items ticked — tone + adjudication (`05-04-adversarial-review-and-adjudication.md`).
- TD rows #9 (`skill/SKILL.md:33` / `:61` / `:96`) closed with this review's directory path.
