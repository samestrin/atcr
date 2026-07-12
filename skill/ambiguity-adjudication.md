# Ambiguity Adjudication (optional)

`atcr reconcile` writes `.atcr/reviews/<id>/reconciled/ambiguous.json` — always present, an empty array when there are no gray-zone clusters. Each entry has an `id`, the member findings, and a similarity score: these are same-location findings whose problem texts are similar enough to *maybe* be duplicates (Jaccard in the 0.4–0.7 gray zone) but not similar enough to merge automatically. By default they remain **unmerged** — the conservative choice, because a false merge hides a finding and a false split in a CI gate is safer than a false pass.

**When you adjudicate, act as a strict gatekeeper against false positives.** Before you `merge` two findings, confirm that *both* are grounded in the actual payload — each finding's cited `file:line` and evidence must correspond to code that really exists in the diff/blocks/files. Never merge a hallucinated or unsupported finding into a real one: a `merge` promotes the pair's confidence, so folding an ungrounded claim into a genuine issue launders a false positive into a trusted result. If either member of a cluster is not demonstrably supported by the code, mark the cluster `distinct` and note why in the rationale. Reconciliation already isolates uncorroborated lone findings (single-reviewer, non-security, below HIGH severity are routed to the ambiguous sidecar rather than promoted), so an ungrounded singleton needs no rescue from you — your adjudication should only ever *confirm* real duplicates, never resurrect noise.

If you choose to adjudicate:

1. Read `ambiguous.json`. For each cluster, decide whether the two findings describe the *same underlying issue* (consider file/line proximity, problem-text overlap, and category alignment).
2. Write `.atcr/reviews/<id>/reconciled/adjudication.json`. Copy `baseline_hash` **verbatim** from the `ambiguous_hash` field of `reconciled/summary.json` — do not compute it yourself:

```json
{
  "baseline_hash": "<copy ambiguous_hash from reconciled/summary.json verbatim>",
  "decisions": [
    { "cluster_id": "amb-1a2b3c4d5e6f", "decision": "merge",    "rationale": "same null-deref, different wording", "host_model": "<your model id>", "timestamp": "<RFC3339>" },
    { "cluster_id": "amb-9f8e7d6c5b4a", "decision": "distinct", "rationale": "different functions",              "host_model": "<your model id>", "timestamp": "<RFC3339>" }
  ]
}
```

   `decision` is `merge`, `distinct`, or `skipped`. Only `merge` collapses a cluster; `distinct` and `skipped` (and any cluster you omit) stay unmerged.
3. Re-run `atcr reconcile <id>`. It validates the decisions file against the preserved original gray set (`ambiguous.original.json` once adjudication has run, else the current `ambiguous.json`): a missing or mismatched `baseline_hash` is rejected (decisions authored against a different generation must not re-merge silently), an unknown `cluster_id` is rejected, and a decisions file with no clusters to adjudicate is an error. It then applies the merges, preserves the original sidecar as `ambiguous.original.json`, and re-emits the reconciled artifacts. Re-running with the same decisions is idempotent.

Process every cluster in one pass — do not truncate by volume. When in doubt, leave a cluster unmerged.
