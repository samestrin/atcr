# atcr Reconciled Review

## Summary

- Total findings: 8
- Sources: pool
- Clusters collapsed: 2
- Severity disagreements: 0
- Authority promoted: 6
- Consensus filtered: 4 (uncorroborated singletons routed to the ambiguous sidecar)
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 1 | 0 | 0 |
| HIGH | 4 | 0 | 0 |
| MEDIUM | 3 | 0 | 0 |
| LOW | 0 | 0 | 0 |

## Disagreements

Top 10 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/personas/drift.go:118` (CRITICAL) · score 4
- Reviewers: mira (independence 1)
- Problem: (CheckDrift) Drift exemption unconditionally skips &#96;local/&lt;model&gt;&#96; slugs so &#96;atcr models check&#96; returns green even when Ollama/llama.cpp is down, the model was never pulled, or &#96;base_url&#96; is unreachable — a broken local setup looks healthy until the runtime request fails

### 2. solo_finding — `docs/personas-install.md:309` (HIGH) · score 3
- Reviewers: mira (independence 1)
- Problem: liam persona binds &#96;local/llama3.3-70b&#96; for a &#34;64 GB+ heavyweight&#34; tier but docs give no pre-flight VRAM/RAM check — attempting on insufficient hardware will OOM-kill the Ollama server mid-review and abort the whole panel with no per-persona isolation

### 3. solo_finding — `docs/personas-install.md:325` (HIGH) · score 3
- Reviewers: mira (independence 1)
- Problem: Docs give no timeout guidance for local personas — a 70B Llama on consumer hardware routinely takes 30–120s per completion; if the LLM client has a cloud-tuned default timeout (common in OpenAI/Anthropic SDKs) every local review aborts mid-stream

### 4. solo_finding — `docs/personas-install.md:329` (HIGH) · score 3
- Reviewers: mira (independence 1)
- Problem: Model-id footgun — persona binds &#96;local/gemma3-27b&#96; but Ollama rejects that string (it answers to &#96;gemma3:27b&#96; or &#96;gemma3-27b&#96;); the docs explain the rewrite but a direct copy-paste of the persona &#96;model&#96; field into &#96;registry.yaml&#96; will 404 at runtime with no clear remediation

### 5. solo_finding — `personas/community/testdata/gerald_fixture.patch:7` (HIGH) · score 3
- Reviewers: mira (independence 1)
- Problem: Fixture embeds the literal &#96;sk-live-EXAMPLE-not-a-real-key&#96; — the &#96;sk-live-&#96; prefix matches Stripe live-key patterns and the string ships in the binary via &#96;//go:embed&#96;; CI secret scanners (TruffleHog/Gitleaks rule sets that prefix-match) can intermittently fail the build or flag a committed test artifact as a real leak

### 6. gray_zone — `personas/community/gerald.md:45` (MEDIUM) · score 2
- Reviewers: brad (independence 1)
- Problem: Example evidence uses / as string delimiters, colliding with the / escape rule for literal 
- Detail: similarity 0.00
- Positions:
  - brad — MEDIUM: Example evidence uses / as string delimiters, colliding with the / escape rule for literal 

### 7. solo_finding — `personas/community_test.go:126` (MEDIUM) · score 2
- Reviewers: mira (independence 1)
- Problem: New roster entries use &#96;Tier: &#34;local&#34;&#96; — if any downstream code switches on tier (selection logic, UI grouping, install defaults) it may have an unhandled case and silently demote or skip these personas

### 8. gray_zone — `docs/personas-install.md:312` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: &#96;gerald&#96; is listed as &#96;local/gemma3-27b&#96; in the table but the prompt refers to &#34;Gemma dense model&#34;
- Detail: similarity 0.00
- Positions:
  - otto — LOW: &#96;gerald&#96; is listed as &#96;local/gemma3-27b&#96; in the table but the prompt refers to &#34;Gemma dense model&#34;

### 9. gray_zone — `docs/personas-install.md:315` (LOW) · score 1
- Reviewers: brad (independence 1)
- Problem: Placeholder api_key_env value assumes local server ignores auth headers, but vLLM/llama.cpp with auth enabled will 401 on dummy keys
- Detail: similarity 0.00
- Positions:
  - brad — LOW: Placeholder api_key_env value assumes local server ignores auth headers, but vLLM/llama.cpp with auth enabled will 401 on dummy keys

### 10. gray_zone — `personas/community_test.go:274` (LOW) · score 1
- Reviewers: bruce (independence 1)
- Problem: Comment says &#34;C(10,2)=45 pairs&#34; but communityPersonas now has 13 entries; C(13,2)=78
- Detail: similarity 0.00
- Positions:
  - bruce — LOW: Comment says &#34;C(10,2)=45 pairs&#34; but communityPersonas now has 13 entries; C(13,2)=78

## Findings

### CRITICAL

- `internal/personas/drift.go:118` — confidence HIGH, reviewers: mira
  - Problem: (CheckDrift) Drift exemption unconditionally skips &#96;local/&lt;model&gt;&#96; slugs so &#96;atcr models check&#96; returns green even when Ollama/llama.cpp is down, the model was never pulled, or &#96;base_url&#96; is unreachable — a broken local setup looks healthy until the runtime request fails
  - Fix: Probe the configured local &#96;base_url&#96; (and optionally &#96;GET /api/tags&#96;) before treating a &#96;local/&lt;model&gt;&#96; lock as drift-clean; emit a distinct &#96;local-unreachable&#96; or &#96;local-model-missing&#96; finding so the user gets a signal
  - Evidence: if isLocalProviderSlug(slug) { continue }

### HIGH

- `docs/personas-install.md:309` — confidence HIGH, reviewers: mira
  - Problem: liam persona binds &#96;local/llama3.3-70b&#96; for a &#34;64 GB+ heavyweight&#34; tier but docs give no pre-flight VRAM/RAM check — attempting on insufficient hardware will OOM-kill the Ollama server mid-review and abort the whole panel with no per-persona isolation
  - Fix: Add a clear minimum-RAM callout AND wrap the local provider call in a per-request timeout/cancel that surfaces an actionable error (not a generic 500) when the upstream dies
  - Evidence: llama3.3-70b 64GB+ heavyweight (dual-GPU / M4 Pro)
- `docs/personas-install.md:325` — confidence HIGH, reviewers: mira
  - Problem: Docs give no timeout guidance for local personas — a 70B Llama on consumer hardware routinely takes 30–120s per completion; if the LLM client has a cloud-tuned default timeout (common in OpenAI/Anthropic SDKs) every local review aborts mid-stream
  - Fix: Document the expected per-tier latency and surface a configurable per-provider timeout for &#96;local&#96;, or warn users to raise it before first invoke
  - Evidence: ollama pull llama3.3:70b # liam (64 GB+ heavyweight)
- `docs/personas-install.md:329` — confidence HIGH, reviewers: mira
  - Problem: Model-id footgun — persona binds &#96;local/gemma3-27b&#96; but Ollama rejects that string (it answers to &#96;gemma3:27b&#96; or &#96;gemma3-27b&#96;); the docs explain the rewrite but a direct copy-paste of the persona &#96;model&#96; field into &#96;registry.yaml&#96; will 404 at runtime with no clear remediation
  - Fix: Validate the local-prefixed discovery id at persona load (strip &#96;local/&#96; and verify the tail against the server&#39;s model list) OR publish a side-by-side discovery-id↔server-tag table so the wire step is unambiguous
  - Evidence: model: gemma3:27b # the tag you pulled with &#96;ollama pull&#96;
- `personas/community/testdata/gerald_fixture.patch:7` — confidence HIGH, reviewers: mira
  - Problem: Fixture embeds the literal &#96;sk-live-EXAMPLE-not-a-real-key&#96; — the &#96;sk-live-&#96; prefix matches Stripe live-key patterns and the string ships in the binary via &#96;//go:embed&#96;; CI secret scanners (TruffleHog/Gitleaks rule sets that prefix-match) can intermittently fail the build or flag a committed test artifact as a real leak
  - Fix: Use an unambiguous synthetic marker that no scanner would prefix-match (e.g., &#96;EXAMPLE-stripe-shape-only-not-a-key-&#96;) and pin the prefix in a comment so reviewers know it is intentional
  - Evidence: const apiSecret = &#34;sk-live-EXAMPLE-not-a-real-key&#34;

### MEDIUM

- `internal/personas/drift.go:71` — confidence HIGH, reviewers: brad, mira
  - Problem: Prefix-match exemption &#96;strings.HasPrefix(slug, &#34;local/&#34;)&#96; is unanchored beyond the slash — a future catalog slug like &#96;local/foo&#96; from any vendor would be silently swallowed; reasonable today but the exemption will not warn when the catalog ever grows such an id
  - Fix: Switch to a set of known local model identifiers (or require the prefix to be the entire first path segment with no further &#96;/&#96;) rather than a bare prefix check
  - Evidence: [mira] return strings.HasPrefix(slug, localProviderSlugPrefix) / [brad] isLocalProviderSlug uses strings.HasPrefix(slug, &#34;local/&#34;) without case normalization
- `personas/community_test.go:126` — confidence HIGH, reviewers: mira
  - Problem: New roster entries use &#96;Tier: &#34;local&#34;&#96; — if any downstream code switches on tier (selection logic, UI grouping, install defaults) it may have an unhandled case and silently demote or skip these personas
  - Fix: Audit tier switch sites and add an explicit &#96;&#34;local&#34;&#96; arm or a default branch that maps unknown tiers to a safe fallback
  - Evidence: {Slug: &#34;gerald&#34;, VendorToken: &#34;gemma&#34;, Tier: &#34;local&#34;, Category: &#34;secret&#34;}
- `personas/community_test.go:371` — confidence HIGH, reviewers: dax, mira
  - Problem: (TestCommunityIndex_Registration) Locked &#96;provider == &#34;openrouter&#34;&#96; assertion is relaxed to an allowlist literal &#96;{openrouter, local}&#96; — any future sanctioned provider addition must touch this test plus the same literal; compile-time guarantee over routing keys is now weaker than for the rest of the persona contract
  - Fix: Move the allowed-providers set into a package-level &#96;var&#96; referenced by both production routing code and the test so the allowlist has a single source of truth and adding a provider is a one-line change
  - Evidence: [dax] require.Containsf(t, []string{&#34;openrouter&#34;, &#34;local&#34;}, e.Provider, ...) without a corresponding model prefix check / [mira] require.Containsf(t, []string{&#34;openrouter&#34;, &#34;local&#34;}, e.Provider
