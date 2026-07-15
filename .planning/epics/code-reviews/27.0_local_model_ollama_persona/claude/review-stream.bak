# Code Review Stream - 27.0_local_model_ollama_persona (Epic)

**Started:** July 15, 2026 12:26:26PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: `personas/community/` includes a ready-to-use local persona for each of the 3 hardware tiers, each passing its `community_fixture_test.go` fixture
- **Verdict:** VERIFIED ✅
- **Evidence:** `personas/community/gerald.yaml:4-5` (tier-32gb-dense, `local/gemma3-27b`), `personas/community/orson.yaml:4-5` (tier-32gb-long-context, `local/qwen3-30b-a3b`), `personas/community/liam.yaml:4-5` (tier-64gb-heavyweight, `local/llama3.3-70b`); `personas/community/index.json:102-131` registers all three with `provider: local` + distinct tier tags; fixture triples present at `personas/community/testdata/{gerald,orson,liam}_fixture.patch`; roster entries at `personas/community_test.go:129-131`.
- **Notes:** 3-file triple (yaml+md+fixture) present per tier. Fixture-gate + count assertions confirmed at test-run time (Phase 4).

### Criterion: A local model can execute a review pipeline using one of these personas without external network calls (once the user's `registry.yaml` defines a matching `local` provider)
- **Verdict:** VERIFIED ✅
- **Evidence:** Each persona binds `provider: local` / `model: local/<tag>` / `role: reviewer` (`gerald.yaml:4-7`). Registration contract relaxed to the `{openrouter, local}` allowlist at `personas/community_test.go:374-376` while discovery still keys on model. Drift exemption added so `atcr models check` never false-flags `local/` slugs as missing (`internal/personas/drift.go:81-91,129-134`), covered by `internal/personas/drift_test.go:177-189`. Provider-level `local:` block (base_url `localhost:11434/v1`) documented in `docs/providers.md`.
- **Notes:** Machinery is in place end-to-end; actual offline execution depends on the user's own Ollama endpoint, per the AC's own parenthetical. No external-call path introduced by the persona itself.

### Criterion: Documentation clearly outlines the zero-egress setup, including the `api_key_env` placeholder-value requirement
- **Verdict:** VERIFIED ✅
- **Evidence:** `docs/personas-install.md:290-342` adds a "Local / Privacy-First (zero data egress)" section (brew install, ollama pull, offline wiring, per-tier table) with an explicit `api_key_env` placeholder callout; `docs/providers.md:27` adds a note that the keyless local endpoint still requires a non-empty `api_key_env` export.
- **Notes:** Both docs cross-link. Placeholder requirement (`export OLLAMA_API_KEY=local-no-key-needed`) documented in both locations.

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (no sprint-design.md — epic)
**Files Reviewed:** 3 source (.go) + persona/doc deliverable content
**Issues Found:** 6 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic mode)

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 6

Findings (all LOW): stale "10 personas" comment (community_test.go:296); stale `Tier` enum comment + dead field (community_test.go:110); no assertion coupling `provider:local` ↔ `local/` model prefix (community_test.go:377); open-namespace drift exemption comment overclaims parity with closed alias set (drift.go:90); gerald.md asserts unconditional zero-egress the binding can't enforce (gerald.md:9); `search --model qwen` doc hint is ambiguous vs cloud `quinn` (personas-install.md:306). No critical/high/medium; no off-host leak path found; fixture secret confirmed unmistakably fake.
