# Local Model Benchmark

This page is the measurement sheet for choosing local reviewer models for two
memory tiers. It is a **shell for an operator-run benchmark**: no measurements
have been recorded here yet. Keep the results and the run-result JSON files
together so a later model, quantization, Ollama, atcr, or hardware change can be
compared like for like.

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
   check that the overlay declares `response_format` before going on. A
   `not_honored` `response_format` verdict is a warning, not a failure, and the
   floor still ran; record it in Notes. Also record the thinking verdict for a
   thinking model.
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
JSON block ([registry](registry.md), `response_format`), so all candidates are
compared on the same output format.

| Candidate | Ollama tag | Quantization | Weights (GB) | Tiers | `max_tokens` |
|---|---|---:|---:|---|---:|
| `qwen3.8-27b` | `qwen3.8:27b-q4_K_M` | Q4_K_M | 17.74 | both | 32768 |
| `gemma4-31b` | `gemma4:31b-it-q4_K_M` | Q4_K_M | 20.39 | both | 32768 |
| `gemma4-26b-a4b` | `gemma4:26b-a4b-it-q4_K_M` | Q4_K_M | 17.99 | both | 32768 |
| `qwen3.6-35b-a3b` | `qwen3.6:35b-a3b-q4_K_M` | Q4_K_M | 23.94 | both | 32768 |
| `gpt-oss-20b` | `gpt-oss:20b` | MXFP4 | 13.79 | both | 32768 |
| `nemotron-3-nano-30b-a3b` | `nemotron-3-nano:30b-a3b-q4_K_M` | Q4_K_M | 24.27 | both | 32768 |
| `gemma3-27b` (baseline) | `gemma3:27b-it-q4_K_M` | Q4_K_M | 17.41 | both | 8192 |

The baseline is the existing `gerald` binding and has no thinking mode. Keep its
settings unchanged so it is a reference point, not a separately tuned entry.

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

**No results recorded.** Every measurement cell below is intentionally empty.
Add the run date and run-result filename in Notes when filling a row.

| Tier | Candidate | Doctor / thinking | Fit verdict | Category recall | Findings / case | Unmatched / case (upper bound) | p50 latency (ms) | Notes |
|---|---|---|---|---|---|---|---|---|
| `gpu-32gb` | `qwen3.8-27b` |  |  |  |  |  |  |  |
| `gpu-32gb` | `gemma4-31b` |  |  |  |  |  |  |  |
| `gpu-32gb` | `gemma4-26b-a4b` |  |  |  |  |  |  |  |
| `gpu-32gb` | `qwen3.6-35b-a3b` |  |  |  |  |  |  |  |
| `gpu-32gb` | `gpt-oss-20b` |  |  |  |  |  |  |  |
| `gpu-32gb` | `nemotron-3-nano-30b-a3b` |  |  |  |  |  |  |  |
| `gpu-32gb` | `gemma3-27b` (baseline) |  |  |  |  |  |  |  |
| `unified-64gb` | `qwen3.8-27b` |  |  |  |  |  |  |  |
| `unified-64gb` | `gemma4-31b` |  |  |  |  |  |  |  |
| `unified-64gb` | `gemma4-26b-a4b` |  |  |  |  |  |  |  |
| `unified-64gb` | `qwen3.6-35b-a3b` |  |  |  |  |  |  |  |
| `unified-64gb` | `gpt-oss-20b` |  |  |  |  |  |  |  |
| `unified-64gb` | `nemotron-3-nano-30b-a3b` |  |  |  |  |  |  |  |
| `unified-64gb` | `gemma3-27b` (baseline) |  |  |  |  |  |  |  |
