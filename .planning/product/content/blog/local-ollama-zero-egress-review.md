# Zero Data Egress: AI Code Review That Never Leaves Your Hardware

**Status:** Draft
**Target:** CISOs, Security Engineers, Regulated Industries (finance, health, defense)
**Publication Phase:** 3 — Enterprise Trust & Security
**Grounded in:** Epic 27.0 (local Ollama/llama.cpp/vLLM persona pack: `gerald`, `orson`, `liam`; `local` provider routing)

## The Hook

For a large class of teams, the entire category of "AI code review" is a non-starter for one reason: it means shipping proprietary source code to a third-party API. If you're in defense, health, finance, or anywhere with a data-residency clause in the contract, "just send your diffs to a frontier model" isn't a trade-off discussion — it's an immediate no.

ATCR's answer isn't a privacy policy or a checkbox. It's a review that makes no external network calls at all.

## The Technical Challenge

Running a reviewer locally sounds simple — point it at Ollama and go — until you try it. A prompt tuned for a 400-billion-parameter frontier model produces mush on a model that fits in 32 GB of RAM. Local hardware comes in tiers, and a persona that assumes unlimited context will thrash a machine that doesn't have it. And most tools' "local mode" still phones home for catalog lookups or telemetry, quietly breaking the one guarantee that mattered.

## The ATCR Solution

ATCR ships a **"zero data egress" persona pack** built specifically for local endpoints (27.0) — three reviewer personas, each tuned to a hardware tier and a review lens:

- **`gerald`** (32 GB dense) — secrets and data-egress review.
- **`orson`** (32 GB long-context) — duplication and repo-wide redundancy.
- **`liam`** (64 GB+ heavyweight) — invariants and state-consistency.

They run against a local Ollama / llama.cpp / vLLM endpoint. **`local`** is a sanctioned provider routing key alongside `openrouter`, so a local persona is a first-class citizen, not a hack — and once your registry points at `localhost`, the review runs entirely on your own hardware with **no external network calls**. ATCR even knows the difference: `atcr models check` no longer flags a `local/<model>` slug as drift `missing`, because that slug is resolved by *your* endpoint, not an external catalog snapshot. The install docs include a Local / Privacy-First zero-egress walkthrough (including the `api_key_env` placeholder requirement — a non-empty value is needed even for a keyless local endpoint).

Your code stays on your machines. The reviewer is tuned to actually be useful on the hardware you have. The guarantee is architectural, not contractual.

## Call to Action

If a data-egress clause has kept AI review off the table, this is the version you can actually deploy. Point ATCR's registry at your local Ollama endpoint, install `gerald`, `orson`, or `liam` for the hardware you've got, and get multi-persona review with your source code never crossing your network boundary. Prove it with a packet capture — there's nothing to see.

## Next Steps (drafting notes)

- [ ] Add the exact registry config pointing at `localhost`.
- [ ] Include a hardware-tier → persona selection table.
- [ ] Show a packet-capture / airgap demonstration of zero egress.
