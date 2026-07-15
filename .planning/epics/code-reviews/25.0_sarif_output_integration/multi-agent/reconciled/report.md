# atcr Reconciled Review

## Summary

- Total findings: 3
- Sources: pool
- Clusters collapsed: 0
- Severity disagreements: 0
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 1 | 0 |
| MEDIUM | 0 | 1 | 0 |
| LOW | 0 | 1 | 0 |

## Disagreements

Top 3 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `.planning/sprints/active/25.0_sarif_output_integration/plan/acceptance-criteria/04-02-gitlab-sast-widget-example.md:36` (HIGH) · score 3
- Reviewers: archer (independence 1)
- Problem: Flattened YAML key &#96;artifacts: reports: sast:&#96; is syntactically invalid and will fail when copied into &#96;.gitlab-ci.yml&#96;

### 2. solo_finding — `.planning/sprints/active/25.0_sarif_output_integration/plan/documentation/schema-validation-with-jsonschema-go.md:14` (MEDIUM) · score 2
- Reviewers: archer (independence 1)
- Problem: Package &#96;github.com/google/jsonschema-go&#96; does not exist in the official Go ecosystem; likely a hallucination for &#96;github.com/santhosh-tekuri/jsonschema/v5&#96; or similar

### 3. solo_finding — `.planning/sprints/active/25.0_sarif_output_integration/plan/acceptance-criteria/01-01-format-registration.md:46` (LOW) · score 1
- Reviewers: archer (independence 1)
- Problem: Scenario 3 title claims &#96;Formats()&#96; enumerates sarif &#34;for error messages&#34;, but body describes the general supported-formats list output, not specifically the error message path

## Findings

### HIGH

- `.planning/sprints/active/25.0_sarif_output_integration/plan/acceptance-criteria/04-02-gitlab-sast-widget-example.md:36` — confidence MEDIUM, reviewers: archer
  - Problem: Flattened YAML key &#96;artifacts: reports: sast:&#96; is syntactically invalid and will fail when copied into &#96;.gitlab-ci.yml&#96;
  - Fix: Use proper nested mapping &#96;artifacts:\n  reports:\n    sast: results.sarif&#96;
  - Evidence: Snippet presents the key path as a single-line flattened string rather than valid YAML structure

### MEDIUM

- `.planning/sprints/active/25.0_sarif_output_integration/plan/documentation/schema-validation-with-jsonschema-go.md:14` — confidence MEDIUM, reviewers: archer
  - Problem: Package &#96;github.com/google/jsonschema-go&#96; does not exist in the official Go ecosystem; likely a hallucination for &#96;github.com/santhosh-tekuri/jsonschema/v5&#96; or similar
  - Fix: Verify and correct to the actual vendored JSON Schema library name before implementation
  - Evidence: Text claims &#34;already-vendored google/jsonschema-go&#34; but standard validation uses santhosh-tekuri or invopop

### LOW

- `.planning/sprints/active/25.0_sarif_output_integration/plan/acceptance-criteria/01-01-format-registration.md:46` — confidence MEDIUM, reviewers: archer
  - Problem: Scenario 3 title claims &#96;Formats()&#96; enumerates sarif &#34;for error messages&#34;, but body describes the general supported-formats list output, not specifically the error message path
  - Fix: Rename to &#34;Formats() includes sarif in supported list&#34; for accuracy
  - Evidence: Title says &#34;for error messages&#34;; body says &#96;Formats()&#96; includes &#96;&#34;sarif&#34;&#96; alongside others
