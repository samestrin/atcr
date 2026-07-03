# Code Review Stream - 16.0_quick_start (Epic)

**Started:** July 02, 2026 05:20:43PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — `atcr init` provides an interactive terminal wizard
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/quickstart.go:39-66` (command), `cmd/atcr/quickstart.go:138-186` (interactive key flow), `cmd/atcr/main.go:189` (registration)
- **Notes:** Per epic clarification, delivered as new command `atcr quickstart` (init left unchanged). Interactive prompts via bufio scanner in keyEnvFlow; streams injectable for tests.

### Criterion: AC2 — Wizard guides synthetic sign-up (browser link with referral tracking)
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/quickstart.go:138-151` (OSC-8 link + `--open`), `internal/quickstart/manifest.go:89-98` (SignupLink referral append)
- **Notes:** OSC-8 hyperlink printed; `--open` opt-in browser launch. Referral mechanism present; `synthetic.json` ships `referral: ""` by design (atcr claims no referral credit until configured). Matches clarification Q1(a).

### Criterion: AC3 — Tool securely receives/accepts the generated API token
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/quickstart.go:162-207` (env-var guidance, optional profile append), `cmd/atcr/quickstart.go:213-229` (profileIsAtcrOwned guard), `cmd/atcr/quickstart.go:249-251` (shellSingleQuote)
- **Notes:** Posture-preserving: key never written to any atcr-owned file. Env var guidance + optional append to user-named shell profile only. Matches clarification Q2(a).

### Criterion: AC4 — Auto-generate `.atcr/config.yaml` with defaults + synthetic model refs
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/quickstart.go:79-95` (init reuse + registry write), `cmd/atcr/quickstart.go:278-308` (writeSyntheticRegistry), `internal/quickstart/manifest.go:105-131` (RegistryYAML)
- **Notes:** Config split per clarification Q5 — roster → project `.atcr/config.yaml` (via runInit), provider+agents → user `registry.yaml`. Agents bound round-robin to synthetic models.

### Criterion: AC5 — Auto-generate `.github/workflows/atcr.yml` without overwriting existing configs
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/quickstart.go:111-131` (scaffoldWorkflow per-file guard), `internal/quickstart/workflow.go:15-47` (WorkflowYAML)
- **Notes:** Per-file non-overwrite guard (refuses without `--force`, skips just that file). Minimal review-only template installs atcr + runs `atcr review`, wired to `LLM_SYNTHETIC_API_KEY`. Matches clarification Q6/Q7(i).

### Bonus (in-scope): scheduled auto-refresh Action
- **Verdict:** VERIFIED ✅
- **Evidence:** `.github/workflows/refresh-synthetic-manifest.yml`, `cmd/refresh-manifest/main.go`, `internal/quickstart/refresh.go:24-76` (BuildManifestFromModels + RunRefresh)
- **Notes:** Secret-guarded weekly cron regenerates bundled manifest from live `/models`, opens PR on drift. Empty-model-list refusal prevents shipping a broken manifest.


## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (no sprint-design.md — epic)
**Files Reviewed:** 6 (quickstart.go, refresh-manifest/main.go, manifest.go, refresh.go, workflow.go, refresh-synthetic-manifest.yml)
**Issues Found:** 13 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 13

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 5
- Low: 7

### Notable themes
- **Key-never-in-atcr-file invariant has two guard bypasses** (HIGH symlink, MED case-insensitive FS) — the security posture the wizard advertises is defeatable via a crafted profile path.
- **Manifest validation asymmetry** — control chars blocked in model ids but not YAML-significant chars (`:`, `#`) nor provider fields, allowing YAML corruption/forgery into the generated registry.yaml from a hostile /models refresh.
- **CI supply-chain defaults** — scaffolded user workflow uses `@latest`; refresh workflow uses unpinned action tags with write perms.
