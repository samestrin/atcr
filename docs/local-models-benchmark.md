# Local Model Benchmark

This page is the measurement sheet for choosing local reviewer models for two
memory tiers. The epic 113.1 runs are recorded below ([Results](#results)),
and the owner's pick from them is recorded under [The pick](#the-pick-ac6).
The method stays here so a later model, quantization, Ollama, atcr, or hardware
change can be compared like for like: keep the results and the run-result JSON
files together.

## Method

Run every candidate on every tier listed for it, one candidate per benchmark
roster, using the same persona and settings on both tiers. Use
[`benchmarks/local-candidates.yaml`](../benchmarks/local-candidates.yaml) to
create a project registry overlay for each run; it pins the model tag,
quantization, thinking settings, and output-token cap. Do not add a `fallback`:
a fallback would attribute a failed candidate's work to another model.

For each candidate and tier:

1. Pull the exact pinned Ollama tag and confirm the installed Ollama version
   meets the candidate's `min_ollama`. Record the software versions and atcr
   commit beside the result. The listed download size covers weights only; the
   operator must confirm that the review context and KV cache also fit.
2. Create a scratch project containing only that candidate's agent in
   `.atcr/config.yaml`, using the same persona on both tiers. Point its provider
   at that tier's Ollama `/v1` endpoint and copy the agent settings from the
   candidate profile: `max_tokens`, the thinking fields, and
   `response_format: json_object`. Declare `response_format` even if the model
   is not known to honor it: `atcr doctor` runs its review-shaped probe, and
   the short-reply floor in it, only for an agent that declares
   `response_format`. Without it doctor passes a near-empty model. Use
   `review_strategy: chunked` for the fit probe, as described in
   [Benchmark Suite](benchmark.md#checking-a-persona--model-pair-before-repointing-an-agent).
3. Run `atcr doctor` before scoring. A failed review-shaped response probe,
   including its short-reply floor, stops the candidate; record the failure
   rather than treating it as a benchmark result. A candidate that answers the
   probe in under 16 tokens (not counting reported reasoning tokens) shows
   `short_reply` in its row, and doctor exits 1 (see the expected output under
   [Operator commands](#operator-commands)). If
   `atcr doctor --agents <candidate-agent> --json` shows no
   `response_format_status` for the agent, the probe and its floor did not run:
   check that the overlay declares `response_format` before going on.

   The floor needs a reply to measure and the provider's token count for it.
   Doctor gives no floor verdict, and says nothing about it, when:
   - the provider rejected the request (`not_honored` with an HTTP 4xx in the
     HINT),
   - the verdict is `unverified`, or the reply was cut off at the cap, or
   - the endpoint reports no `usage.completion_tokens`. Ollama's `/v1`
     endpoint reports it; confirm that once per tier with a plain
     `/v1/chat/completions` request.

   In those cases fix the endpoint and re-run doctor, or record the candidate
   as failed, before scoring. A `not_honored` because the model ignored the
   field (the HINT says the reply was not a bare JSON object, or the parser
   read no findings) is held to the floor like any other reply: under 16
   tokens the row is `short_reply` and doctor exits 1; at or over it,
   `not_honored` is a warning, not a failure, so record it in Notes. Also
   record the thinking verdict for a thinking model.
4. Run the `fit-v1` health probe with three replicates and inspect
   `atcr benchmark fit`. Do not select a pair marked `unfit`.
5. Verify and run the `standard-v1` suite with a checkpoint. Keep the generated
   run-result JSON. Read category recall from `corroboration_rate`, volume from
   `findings_raised_avg`, and median latency from `latency_p50_ms`.
6. Read `unmatched_per_case` from that reviewer's `reviewer_false_alarms` row.
   This is an **upper bound**, not a false-alarm count: an unmatched finding can
   be a genuine defect the suite did not plant. Inspect a sample before using it
   to reject a candidate. See [False alarms](benchmark.md#false-alarms-an-upper-bound).
7. Fill one row below only from the completed run-result. Leave interrupted,
   failed, or incomplete runs unfilled and describe them in Notes instead.
   Compare candidates on the same tier, suite version, persona, and settings;
   latency is not comparable across tiers.

The `fit-v1` suite checks call health only and is not a quality score. The
`standard-v1` recall metric is category recall against planted-defect categories,
not precision. Do not export either local selection run to the public
leaderboard.

## Hardware tiers

| Tier id | Machine | Memory under test | Serving stack |
|---|---|---|---|
| `gpu-32gb` | `ai.lan`, RTX 5090 | 32 GB GPU memory | Ollama, OpenAI-compatible `/v1` endpoint |
| `unified-64gb` | `macmini-m4` | 64 GB unified memory | Ollama, OpenAI-compatible `/v1` endpoint |

Record the exact GPU or SoC, operating system, Ollama version, atcr commit, and
any other significant load on the machine. Run only one candidate at a time on
a tier.

## Candidates

All candidates use the tags and settings in
[`benchmarks/local-candidates.yaml`](../benchmarks/local-candidates.yaml), not a
moving `latest` tag. Thinking candidates use `thinking_style:
reasoning_effort`, `thinking: on`, and `thinking_level: high`. Every candidate,
the baseline included, declares `response_format: json_object` so that doctor's
short-reply floor runs. The review lane then runs in JSON mode for every
candidate: the persona's `## Output Format` section is replaced by the shared
JSON block, or the block is appended when the persona has none
([registry](registry.md), `response_format`), so all candidates are compared on
the same output format.

| Candidate | Ollama tag | Quantization | Weights (GB) | Tiers | `max_tokens` |
|---|---|---:|---:|---|---:|
| `qwen3.8-27b` | `qwen3.8:27b-q4_K_M` | Q4_K_M | 17.74 | both | 32768 |
| `gemma4-31b` | `gemma4:31b-it-q4_K_M` | Q4_K_M | 20.39 | both | 32768 |
| `gemma4-26b-a4b` | `gemma4:26b-a4b-it-q4_K_M` | Q4_K_M | 17.99 | both | 32768 |
| `qwen3.6-35b-a3b` | `qwen3.6:35b-a3b-q4_K_M` | Q4_K_M | 23.94 | both | 32768 |
| `gpt-oss-20b` | `gpt-oss:20b` | MXFP4 | 13.79 | both | 32768 |
| `nemotron-3-nano-30b-a3b` | `nemotron-3-nano:30b-a3b-q4_K_M` | Q4_K_M | 24.27 | both | 32768 |
| `gemma3-27b` (baseline) | `gemma3:27b-it-q4_K_M` | Q4_K_M | 17.41 | both | 8192 |

The baseline is the existing `gerald` binding and has no thinking mode. Apart
from the shared `response_format: json_object`, keep its settings unchanged so
it is a reference point, not a separately tuned entry.

## Operator commands

Run these from the scratch project containing the one-candidate overlay. Replace
angle-bracket placeholders, including the agent name and output prefix. The
repository path must point at the same atcr checkout used to build the binary.

```bash
ollama pull <pinned-ollama-tag>
export OLLAMA_API_KEY=local-no-key-needed

atcr doctor --agents <candidate-agent>
# Go on only if doctor exits 0 and `--json` shows a response_format_status
# for the agent. A candidate under the floor exits 1: its row's STATUS is
# short_reply, with this HINT,
#   model <pinned-ollama-tag> answered the review-shaped probe in 4 tokens,
#   under the 16-token floor; a near-empty review usually means a
#   chat-template or stop-token fault, so fix the model's template and stop
#   tokens before scoring it
# stderr counts it as "doctor: 0 ok / 1 failed", and the error reads
#   one or more agents have no working endpoint (a review-shaped reply was
#   under the 16-token floor for: <candidate-agent> (<pinned-ollama-tag>))
atcr doctor --agents <candidate-agent> --json

atcr benchmark run \
  --suite-path <atcr-repo>/benchmarks/fit-v1 \
  --replicates 3 \
  --checkpoint <tier>-<candidate>-fit.checkpoint.json \
  --output <tier>-<candidate>-fit.json
atcr benchmark fit --in <tier>-<candidate>-fit.json

atcr benchmark verify --suite-path <atcr-repo>/benchmarks/standard-v1
atcr benchmark run \
  --suite-path <atcr-repo>/benchmarks/standard-v1 \
  --checkpoint <tier>-<candidate>-standard.checkpoint.json \
  --output <tier>-<candidate>-standard.json
```

If a standard-suite run stops, fix the provider or machine problem and rerun the
same command with the same checkpoint and unchanged roster. Start a fresh
checkpoint whenever the candidate, persona, settings, or suite changes. Keep
both JSON files and the suite's reported reproducibility hash with the filled
row.

## Results

Recorded 2026-11-20 from the operator's AC5 table (posted on EST-6869 with the
owner's pick, card `cd872952`). Every row is `benchmarks/standard-v1`, all 17
cases, one persona prompt for all candidates, atcr `5f8b6037` and the same
suite reproducibility hash. The `gpu-32gb` tier ran 2026-11-14 on `ai.lan`
(RTX 5090): all 7 candidates passed the smoke check and scored — none was
dropped by it. The `unified-64gb` tier ran on `macmini-m4` over the nights
ending 2026-11-20; `nemotron-3-nano-30b-a3b` failed there mid-suite (see its
rows). The runs used the commands under
[Operator commands](#operator-commands). The run-result JSON files are kept on
the orchestrator under
`~/bench113/{gpu-32gb,unified-64gb}/results/*/*-standard.json`.

Cells carry category recall (`corroboration_rate`), unmatched findings per
case (`unmatched_per_case`, an **upper bound** — see
[False alarms](benchmark.md#false-alarms-an-upper-bound)), and p50 seconds per
case, as posted. Latency is not comparable across tiers. The posted summary
did not re-state the doctor/thinking verdicts, the `fit-v1` verdicts, or
`findings_raised_avg`; those fields live in the retained run-result JSONs, not
in this table. The hand-checked random sample of unmatched findings for the top candidates
(part of AC5) was attempted on 2026-11-22 against the retained run artifacts
and could not be adjudicated. `<cand>-standard.json` keeps per-persona totals
only and `<cand>-standard.checkpoint.json` keeps per case only the raised
category words, so no finding's text, file or line survives a run. Three
independent read-only reviews (orson `gemma3-27b` gpu-32gb; liam
`gemma4-26b-a4b` gpu-32gb; liam `qwen3.8-27b` unified-64gb; seed 0, sample
min(15, total) = 8 / 11 / 15) rebuilt the unmatched list from the checkpoints
with the equivalence rules and reproduced the published counts exactly (8 of
102 findings; 11 of 17; 20 of 28), but every sampled item was unclear: a
category word cannot be checked against code, so 0 true positives, 0 false
alarms, all unclear. Every unmatched `liam` finding uses liam's own vocabulary
(contract, api-contract, state, invariant, race, resource-leak, type) that
`equivalence.go` keeps out of `correctness`, and one is the routing label
`out-of-scope`, so for `liam` the unmatched column mostly measures vocabulary
mismatch, not false alarms. The numbers below remain a category-level upper
bound. Keeping each finding's text/file/line in the run result is follow-up
debt (see the debt issue linked from EST-7815).

### `gerald` (secrets & data-egress)

| Tier | Candidate | Category recall | Unmatched / case (upper bound) | p50 latency (s) | Notes |
|---|---|---:|---:|---:|---|
| `gpu-32gb` | `qwen3.8-27b` | .020 | 0.00 | 13.1 |  |
| `gpu-32gb` | `gemma4-31b` | .020 | 0.00 | 10.7 |  |
| `gpu-32gb` | `gemma4-26b-a4b` | .020 | 0.06 | 5.9 |  |
| `gpu-32gb` | `qwen3.6-35b-a3b` | .020 | 0.00 | 7.5 |  |
| `gpu-32gb` | `gpt-oss-20b` | .039 | 0.88 | 23.1 |  |
| `gpu-32gb` | `nemotron-3-nano-30b-a3b` | .000 | 0.00 | 1.8 |  |
| `gpu-32gb` | `gemma3-27b` (baseline) | .127 | 2.12 | 6.9 | best gerald recall on the tier |
| `unified-64gb` | `qwen3.8-27b` | .020 | 0.00 | 132.1 |  |
| `unified-64gb` | `gemma4-31b` | .020 | 0.12 | 128.6 |  |
| `unified-64gb` | `gemma4-26b-a4b` | .020 | 0.00 | 40.1 |  |
| `unified-64gb` | `qwen3.6-35b-a3b` | .000 | 0.00 | 46.0 |  |
| `unified-64gb` | `gpt-oss-20b` | .020 | 0.12 | 51.6 |  |
| `unified-64gb` | `nemotron-3-nano-30b-a3b` | — | — | — | FAIL: Ollama HTTP 500 "prediction aborted, token repeat limit reached" on case 9/17 (`freecad-freecad-pr-18688`), reproduced every night; not a context limit (28.5K of 65,536 tokens used) |
| `unified-64gb` | `gemma3-27b` (baseline) | .167 | 1.24 | 110.9 | best gerald recall on the tier |

**Follow-up run (hosted, not a local tier):** `gerald` on Synthetic
`hf:openai/gpt-oss-120b`, `standard-v1`, 17 of 17 cases, same atcr `5f8b6037`
and suite hash, `max_tokens` 32768, reasoning high: recall .020, unmatched
0.29 per case, p50 13.7 s; the reply was clean (not truncated) on 15 of 17
cases. Run files: orchestrator `~/bench113/synthetic-gptoss120b/`. .020 is
below the `gemma3` baseline's .127, so this run did not bring `gerald` back
into the presets. The owner also asked a follow-up question: check whether
the thinking models' "clean" answers on `gerald` are a setup effect (JSON
mode plus thinking).

### `orson` (duplication & repo-wide redundancy)

| Tier | Candidate | Category recall | Unmatched / case (upper bound) | p50 latency (s) | Notes |
|---|---|---:|---:|---:|---|
| `gpu-32gb` | `qwen3.8-27b` | .118 | 0.00 | 32.3 |  |
| `gpu-32gb` | `gemma4-31b` | .137 | 0.00 | 17.6 |  |
| `gpu-32gb` | `gemma4-26b-a4b` | .098 | 0.06 | 16.3 |  |
| `gpu-32gb` | `qwen3.6-35b-a3b` | .098 | 0.00 | 13.3 |  |
| `gpu-32gb` | `gpt-oss-20b` | .147 | 0.00 | 54.1 |  |
| `gpu-32gb` | `nemotron-3-nano-30b-a3b` | .049 | 0.00 | 3.8 |  |
| `gpu-32gb` | `gemma3-27b` (baseline) | .461 | 0.47 | 12.8 | **picked for `local-32gb`** |
| `unified-64gb` | `qwen3.8-27b` | .118 | 0.00 | 231.2 |  |
| `unified-64gb` | `gemma4-31b` | .137 | 0.00 | 252.8 |  |
| `unified-64gb` | `gemma4-26b-a4b` | .118 | 0.00 | 87.9 |  |
| `unified-64gb` | `qwen3.6-35b-a3b` | .098 | 0.18 | 61.5 |  |
| `unified-64gb` | `gpt-oss-20b` | .069 | 0.06 | 247.7 |  |
| `unified-64gb` | `nemotron-3-nano-30b-a3b` | — | — | — | FAIL: same HTTP 500 token-repeat abort as the gerald row, case 9/17, every night |
| `unified-64gb` | `gemma3-27b` (baseline) | .431 | 0.71 | 157.3 | **picked for `local-64gb`** |

### `liam` (invariants & state-consistency)

| Tier | Candidate | Category recall | Unmatched / case (upper bound) | p50 latency (s) | Notes |
|---|---|---:|---:|---:|---|
| `gpu-32gb` | `qwen3.8-27b` | .118 | 1.12 | 85.2 |  |
| `gpu-32gb` | `gemma4-31b` | .108 | 0.82 | 50.4 |  |
| `gpu-32gb` | `gemma4-26b-a4b` | .118 | 0.65 | 34.1 | **picked for `local-32gb`** |
| `gpu-32gb` | `qwen3.6-35b-a3b` | .108 | 0.59 | 24.8 |  |
| `gpu-32gb` | `gpt-oss-20b` | .059 | 0.65 | 100.0 |  |
| `gpu-32gb` | `nemotron-3-nano-30b-a3b` | .049 | 1.76 | 10.3 |  |
| `gpu-32gb` | `gemma3-27b` (baseline) | .196 | 2.82 | 11.2 | higher recall, 2.82 unmatched/case vs the pick's 0.65 |
| `unified-64gb` | `qwen3.8-27b` | .176 | 1.18 | 532.0 | **picked for `local-64gb`**; best liam recall on the tier |
| `unified-64gb` | `gemma4-31b` | .108 | 0.71 | 458.0 |  |
| `unified-64gb` | `gemma4-26b-a4b` | .157 | 0.53 | 222.5 |  |
| `unified-64gb` | `qwen3.6-35b-a3b` | .098 | 0.88 | 126.7 |  |
| `unified-64gb` | `gpt-oss-20b` | .127 | 0.71 | 430.9 |  |
| `unified-64gb` | `nemotron-3-nano-30b-a3b` | — | — | — | FAIL: same HTTP 500 token-repeat abort as the gerald row, case 9/17, every night |
| `unified-64gb` | `gemma3-27b` (baseline) | .157 | 1.82 | 92.8 |  |

## The pick (AC6)

Owner-approved 2026-11-20 (EST-6869, card `cd872952`). atcr fans out — each
persona runs on its own server at the same time — so each persona takes its
best model and the three need not share one. There is no local-only rule; the
local runs were to get numbers.

| Persona | `local-32gb` (`gpu-32gb`) | `local-64gb` (`unified-64gb`) |
|---|---|---|
| `orson` | `gemma3:27b-it-q4_K_M` | `gemma3:27b-it-q4_K_M` |
| `liam` | `gemma4:26b-a4b-it-q4_K_M` | `qwen3.8:27b-q4_K_M` |
| `gerald` | not in the preset | not in the preset |

The scores that decided it (recall / unmatched per case / p50 s, from the
tables above):

- `orson`: `gemma3-27b` .461 / 0.47 / 12.8 on 32 GB and .431 / 0.71 / 157.3 on
  64 GB — the best orson recall on both tiers by a wide margin.
- `liam`, 32 GB: `gemma4-26b-a4b` .118 / 0.65 / 34.1. The `gemma3` baseline
  scored higher recall (.196) but 2.82 unmatched findings per case against
  0.65; the owner picked `gemma4-26b-a4b`.
- `liam`, 64 GB: `qwen3.8-27b` .176 / 1.18 / 532.0 — the best liam recall on
  the tier. `liam` no longer binds `llama3.3:70b` for tier A (AC6); that
  binding is retired.
- `gerald`: dropped from both local presets for now. Every newer local
  candidate scored .000–.039 recall for gerald against the `gemma3` baseline
  (.127 on 32 GB, .167 on 64 GB), and the hosted `gpt-oss-120b` follow-up run
  scored .020 — also below the baseline. `gerald` can still be added by hand.

Memory fit at the stated quantization (AC6): the picked tags carry 17.41 GB
(`gemma3:27b-it-q4_K_M`), 17.99 GB (`gemma4:26b-a4b-it-q4_K_M`) and 17.74 GB
(`qwen3.8:27b-q4_K_M`) of weights — all Q4_K_M, all under 32 GB — and each
completed the full 17-case suite on its tier, so the review context and KV
cache fit the tier's memory in the operator's runs. No picked model needs
more than 32 GB for tier A or 64 GB for tier B.

The `local` preset aliases **`local-32gb`**. The presets, the community
persona files and `index.json` bind these tags in code (epic 113.1 T1,
`cli/quickstart.go`); a test fails if two of those surfaces disagree.
