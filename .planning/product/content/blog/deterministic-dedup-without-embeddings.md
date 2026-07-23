# How ATCR Merges Five Models' Findings Without a Vector Database

**Status:** Draft
**Target:** Architects, Backend Engineers, anyone who has built a RAG/dedup pipeline
**Publication Phase:** 2 — Education & Architecture
**Grounded in:** Epic 13.1 (AST-isomorphism grouping via WebAssembly parsers), 13.2 (Hungarian bipartite matching + DBSCAN noise isolation)

## The Hook

Run a diff past five different models and you get five different opinions — and a lot of the same opinion phrased five different ways. "Possible nil dereference on line 42." "This pointer may be null." "Missing null check near the mutex." Same bug, three findings, two line numbers. If you just concatenate the outputs, your reviewer's first act is to spam the developer.

The obvious fix is an embedding model and a vector database to cluster "similar" findings. ATCR does not do that — on purpose.

## The Technical Challenge

Embedding-based dedup has three problems for a code reviewer. It's **non-deterministic** (the same inputs can cluster differently run to run), it's **another API bill and another dependency**, and it clusters on *surface similarity* when what you actually care about is *the same code location*. Two findings about genuinely different bugs that happen to be phrased alike will get merged. Two findings about the same bug reported on line 42 and line 45 (because two models counted whitespace differently) will get split.

Worse, a naive greedy "merge anything close" pass transitively over-merges: A is close to B, B is close to C, so A, B, and C become one cluster even though A and C describe unrelated issues. Now your "consensus" finding is a lie.

## The ATCR Solution

ATCR clusters findings by *code identity*, computed deterministically, in-tree, with no new dependencies.

- **AST-isomorphism grouping (13.1):** each finding's line is mapped to the smallest AST block that covers it, and a structural Merkle hash of that block becomes the grouping key. Whitespace, blank lines, and model line-skew stop mattering — two findings inside the same logical block group together even when their reported line numbers drift. The parsers are compiled to WebAssembly and run on a pure-Go wazero host, so there's zero CGO and the binary still cross-compiles everywhere. Drop in a new `.wasm` file and you've added a language. Benchmarked: AST recall 1.00 / precision 1.00, versus ~0.79 recall for line-proximity with false merges of adjacent-but-different blocks.
- **Optimal matching, not greedy clustering (13.2):** across models, findings are aligned with Kuhn-Munkres (Hungarian) bipartite matching, so each consensus issue gathers *at most one finding per model* and the transitive over-merge problem simply cannot happen. Edge weights combine the AST-isomorphism signal (isomorphic findings match at distance 0) with token-set Jaccard distance — integer math, no floating-point boundary drift.
- **Deterministic noise isolation (13.2):** a single model's uncorroborated finding, standing alone where other models corroborate each other, is moved to an `ambiguous.json` sidecar via DBSCAN and removed from the consensus output — with no arbitrary cluster-count threshold to tune. Consensus stays trustworthy; the sidecar is *strictly* uncorroborated noise you can read or ignore.

Every step is reproducible. Same findings in, same clusters out, every time — no embedding endpoint, no vector store, no per-run drift.

## Call to Action

If you're building anything that reconciles multiple AI outputs, the lesson generalizes: cluster on the structural identity of the thing being described, not the fuzzy similarity of the words describing it. Read how ATCR does it, and stop paying an embedding bill to make your reviewer *less* deterministic.

## Next Steps (drafting notes)

- [ ] Add a worked example: three phrasings of one bug collapsing to a single consensus finding.
- [ ] Diagram the AST-block Merkle-hash grouping key.
- [ ] Link to the reconciler internals docs and the `ATCR_DISABLE_AST_GROUPING` opt-out.
