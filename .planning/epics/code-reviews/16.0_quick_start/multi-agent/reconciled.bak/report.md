# atcr Reconciled Review

## Summary

- Total findings: 6
- Sources: pool
- Clusters collapsed: 1
- Severity disagreements: 1
- Consensus filtered: 11 (uncorroborated singletons routed to the ambiguous sidecar)
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 2 | 0 |
| MEDIUM | 1 | 3 | 0 |
| LOW | 0 | 0 | 0 |

## Disagreements

Top 17 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `cmd/atcr/quickstart.go:97` (HIGH) · score 3
- Reviewers: dax (independence 1)
- Problem: runQuickstart silently discards writeSyntheticRegistry error when runInit fails

### 2. solo_finding — `internal/quickstart/manifest.go:83` (HIGH) · score 3
- Reviewers: brad (independence 1)
- Problem: Unquoted fmt.Fprintf for provider name and base_url allows YAML structure injection if the refreshed API returns values containing &#39;:&#39;, &#39;#&#39;, or other special characters

### 3. severity_split — `cmd/atcr/quickstart.go:152` (MEDIUM) · score 2
- Severity disagreement: LOW vs MEDIUM
- Reviewers: dax, otto (independence 2)
- Problem: keyEnvFlow scanner.Scan() failure (e.g. EOF on stdin) returns empty string with no error surfaced

### 4. solo_finding — `cmd/atcr/quickstart.go:173` (MEDIUM) · score 2
- Reviewers: brad (independence 1)
- Problem: resolveProfilePath uses filepath.Abs which does not resolve symlinks, allowing a user to bypass the atcr-owned file guard by passing a symlink pointing into .atcr/ or the registry

### 5. gray_zone — `cmd/atcr/quickstart.go:175` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: appendExport failure is logged to errOut but the error is swallowed — caller gets nil
- Detail: similarity 0.00
- Positions:
  - dax — MEDIUM: appendExport failure is logged to errOut but the error is swallowed — caller gets nil

### 6. solo_finding — `cmd/atcr/quickstart.go:258` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: shellSingleQuote does not escape control characters other than single-quote

### 7. solo_finding — `internal/quickstart/manifest.go:72` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: validate rejects control characters in model ids but not in provider Name, BaseURL, or APIKeyEnv

### 8. gray_zone — `internal/quickstart/manifest.go:105` (MEDIUM) · score 2
- Reviewers: otto (independence 1)
- Problem: &#96;RegistryYAML&#96; uses &#96;fmt.Fprintf&#96; for a simple string builder
- Detail: similarity 0.00
- Positions:
  - otto — MEDIUM: &#96;RegistryYAML&#96; uses &#96;fmt.Fprintf&#96; for a simple string builder

### 9. gray_zone — `internal/quickstart/refresh.go:39` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: BuildManifestFromModels filters empty-trimmed ids but does not reject control characters in ids
- Detail: similarity 0.00
- Positions:
  - dax — MEDIUM: BuildManifestFromModels filters empty-trimmed ids but does not reject control characters in ids

### 10. gray_zone — `internal/quickstart/refresh.go:58` (MEDIUM) · score 2
- Reviewers: brad (independence 1)
- Problem: io.ReadAll reads the entire stdin into memory without an upper bound, allowing a malicious or misconfigured API response to OOM the refresh process
- Detail: similarity 0.00
- Positions:
  - brad — MEDIUM: io.ReadAll reads the entire stdin into memory without an upper bound, allowing a malicious or misconfigured API response to OOM the refresh process

### 11. gray_zone — `.github/workflows/refresh-synthetic-manifest.yml:52` (LOW) · score 1
- Reviewers: brad (independence 1)
- Problem: git checkout -b fails if the branch already exists from a concurrent or retried run, causing the workflow to abort without cleanup
- Detail: similarity 0.00
- Positions:
  - brad — LOW: git checkout -b fails if the branch already exists from a concurrent or retried run, causing the workflow to abort without cleanup

### 12. gray_zone — `cmd/atcr/quickstart.go:114` (LOW) · score 1
- Reviewers: brad (independence 1)
- Problem: bufio.NewScanner uses a default 64KB token limit, silently truncating or failing on API keys or profile paths longer than 64KB
- Detail: similarity 0.00
- Positions:
  - brad — LOW: bufio.NewScanner uses a default 64KB token limit, silently truncating or failing on API keys or profile paths longer than 64KB

### 13. gray_zone — `cmd/atcr/quickstart.go:238` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: &#96;appendExport&#96; uses a hardcoded 0o600 for shell profiles
- Detail: similarity 0.00
- Positions:
  - otto — LOW: &#96;appendExport&#96; uses a hardcoded 0o600 for shell profiles

### 14. gray_zone — `cmd/atcr/quickstart_test.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: No test exercises the error path when runInit fails (e.g. permission-denied on .atcr/)
- Detail: similarity 0.00
- Positions:
  - dax — LOW: No test exercises the error path when runInit fails (e.g. permission-denied on .atcr/)

### 15. gray_zone — `cmd/atcr/quickstart_test.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: No test exercises the scanner error path (closed stdin, /dev/null input)
- Detail: similarity 0.00
- Positions:
  - dax — LOW: No test exercises the scanner error path (closed stdin, /dev/null input)

### 16. gray_zone — `internal/quickstart/workflow.go:36` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: &#96;WorkflowYAML&#96; uses &#96;go install ...@latest&#96; in a generated CI scaffold
- Detail: similarity 0.00
- Positions:
  - otto — LOW: &#96;WorkflowYAML&#96; uses &#96;go install ...@latest&#96; in a generated CI scaffold

### 17. gray_zone — `internal/quickstart/workflow_test.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: TestWorkflowYAML_ReviewOnlySynthetic does not assert the generated YAML is valid YAML
- Detail: similarity 0.00
- Positions:
  - dax — LOW: TestWorkflowYAML_ReviewOnlySynthetic does not assert the generated YAML is valid YAML

## Findings

### HIGH

- `cmd/atcr/quickstart.go:97` — confidence MEDIUM, reviewers: dax
  - Problem: runQuickstart silently discards writeSyntheticRegistry error when runInit fails
  - Fix: Return the error from writeSyntheticRegistry or wrap it; currently the error is discarded and the function falls through to keyEnvFlow
  - Evidence: &#96;if err := writeSyntheticRegistry(o, manifest, builtins.Names()); err != nil { return err }&#96; — the error is returned, but the earlier &#96;runInit&#96; error path at line 97 returns without calling writeSyntheticRegistry, so this is a false read. The real gap: no test exercises runInit failure followed by the rest of the flow.
- `internal/quickstart/manifest.go:83` — confidence MEDIUM, reviewers: brad
  - Problem: Unquoted fmt.Fprintf for provider name and base_url allows YAML structure injection if the refreshed API returns values containing &#39;:&#39;, &#39;#&#39;, or other special characters
  - Fix: Use a proper YAML encoder (e.g. yaml.Marshal) or quote all interpolated values with single quotes and escape embedded quotes
  - Evidence: fmt.Fprintf(&amp;b, &#34;  %s:\n&#34;, m.Provider.Name) and fmt.Fprintf(&amp;b, &#34;    base_url: %s\n&#34;, m.Provider.BaseURL) construct YAML via string concatenation without escaping or quoting

### MEDIUM

- `cmd/atcr/quickstart.go:152` — confidence HIGH, reviewers: dax, otto
  - Severity disagreement: LOW vs MEDIUM
  - Problem: keyEnvFlow scanner.Scan() failure (e.g. EOF on stdin) returns empty string with no error surfaced
  - Fix: Return an error when scanner.Err() is non-nil after the loop, or check it after each prompt; a truncated stdin silently yields empty key/profile
  - Evidence: [otto] &#96;os.MkdirAll(filepath.Dir(wfPath), 0o755)&#96; and &#96;os.WriteFile(wfPath, ..., 0o644)&#96; / [dax] &#96;readLine&#96; returns &#96;&#34;&#34;&#96; on both empty input and scanner failure; the caller treats both as &#34;skip&#34; with no distinction
- `cmd/atcr/quickstart.go:173` — confidence MEDIUM, reviewers: brad
  - Problem: resolveProfilePath uses filepath.Abs which does not resolve symlinks, allowing a user to bypass the atcr-owned file guard by passing a symlink pointing into .atcr/ or the registry
  - Fix: Use filepath.EvalSymlinks on both the profile path and the atcr/registry paths before comparison
  - Evidence: abs, err := filepath.Abs(profile) in resolveProfilePath does not dereference symlinks, so a symlink to an atcr-owned file bypasses the prefix/equals checks in profileIsAtcrOwned
- `cmd/atcr/quickstart.go:258` — confidence MEDIUM, reviewers: dax
  - Problem: shellSingleQuote does not escape control characters other than single-quote
  - Fix: A key containing a newline or escape sequence injected via paste can break the export line or inject shell commands when the user sources the profile
  - Evidence: &#96;strings.ReplaceAll(s, &#34;&#39;&#34;, &#96;&#39;\&#39;&#39;&#96;)&#96; only handles single quotes; a newline in the key would produce a multi-line export that a shell would execute
- `internal/quickstart/manifest.go:72` — confidence MEDIUM, reviewers: dax
  - Problem: validate rejects control characters in model ids but not in provider Name, BaseURL, or APIKeyEnv
  - Fix: Add control-character checks for Provider.Name, Provider.BaseURL, and Provider.APIKeyEnv; a newline in BaseURL would forge YAML structure in the generated registry just like a model id
  - Evidence: Only &#96;m.Models&#96; loop checks &#96;unicode.IsControl&#96;; Provider fields are validated only for whitespace-trimmed emptiness
