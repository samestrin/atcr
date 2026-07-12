# Findings Format Reference

The findings stream is a versioned, pipe-delimited contract. The contract below is self-contained and sufficient for standalone skill installs; `docs/findings-format.md` provides supplementary background, parsing rules, and the JSON form.

- Per-source files (`sources/<name>/findings.txt`, including `sources/host/findings.txt`) carry the `# atcr-findings/v1` header and 8 columns: `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER`.
- Reconciled output (`reconciled/findings.txt`) has 9 columns: the `REVIEWER` column becomes `REVIEWERS` (comma-joined) and a `CONFIDENCE` column is added (`HIGH` when 2+ distinct reviewers agree, else `MEDIUM`).
- Severity extraction is by strict prefix (`^(CRITICAL|HIGH|MEDIUM|LOW)\|`), so prose mentions of a severity word are ignored.
