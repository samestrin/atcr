# Execution reproduction (`--exec`)

Execution is the last and most dangerous rung of the review ladder. Stages 2–4
(tool-using reviewers, adversarial verification, cross-examination) ground a
finding in *read* code and adversarial challenge. Execution lets a skeptic go one
step further and **demonstrate** a finding by running code: a reproduced failure
cannot be a hallucination, and it hands the resolver a failing command to start
from.

Because this runs model-authored code on your machine, it is **strictly opt-in**
and **refuses to run** without a configured, container-isolated sandbox backend
that passes a preflight check. It is off by default and every prior stage works
without it.

## Enabling it

Execution requires two things, both explicit:

1. A `sandbox:` block in `.atcr/config.yaml` (the backend, image, and the
   project's test command).
2. The `--exec` flag on the command.

```yaml
# .atcr/config.yaml
agents:
  - greta
sandbox:
  backend: docker            # only "docker" is supported today
  image: golang:1.25         # MUST be present locally (runs are network-isolated)
  test_command: [go, test, ./...]
  # optional hardening overrides (sane defaults apply):
  # docker_path: /opt/homebrew/bin/docker
  # memory: 512m
  # cpus: "1.0"
  # pids_limit: 256
  # timeout_secs: 60
```

```bash
atcr verify <review-id> --exec          # standalone verify with execution
atcr review --verify --exec             # one-shot review -> reconcile -> verify
```

If `--exec` is passed without a `sandbox:` block, or the backend fails its
preflight check, the command **hard-errors (exit 2) without executing anything**.
On `review --verify --exec` the gate is resolved *first*, so a misconfigured
sandbox fails fast before any review API calls are spent. `review --exec` without
`--verify` is rejected — execution runs in the verify stage, where the skeptics
that consume it live.

## What the sandbox guarantees

Every run is executed in an ephemeral container with:

- **No network** (`--network none`) — the run cannot exfiltrate or call out.
- **Read-only snapshot** — the review's code snapshot is mounted read-only at
  `/work`; the run cannot mutate your working tree. A writable `tmpfs` scratch
  overlay is provided at `/scratch`.
- **Read-only root filesystem**, **all capabilities dropped** (`--cap-drop ALL`),
  **no new privileges** (`--security-opt no-new-privileges`), and a **non-root
  user**.
- **Resource caps** — memory, CPU, and PID limits, plus a wall-clock timeout.

Bare-metal execution is intentionally unsupported. The backend is pluggable;
Docker is the only implementation today (Podman is a future addition).

### Preflight

Before any execution the backend verifies, in order: the Docker daemon is
reachable, the configured base **image is present locally** (runs are
network-isolated, so it cannot be pulled on demand — run `docker pull <image>`
first), and a trivial hardened container runs to completion. Any failure refuses
the run.

## The tools

In an `--exec` run, the skeptics are offered two additional tools (every other
agent keeps the read-only tool set unchanged):

- `run_tests(target?)` — runs the configured `test_command` in the sandbox; an
  optional `target` scopes it (e.g. a single package).
- `run_script(content, timeout?)` — runs a short shell script in the sandbox
  scratch overlay.

Output is captured, truncated to a budget, and attached to the agent transcript.
`/work` (the snapshot) is **read-only**; the only writable location is the
`/scratch` tmpfs. `HOME`, `TMPDIR`, and the common build-cache vars
(`GOCACHE`/`GOTMPDIR`/`XDG_CACHE_HOME`) are pointed at `/scratch` so test runners
that need to write a cache or temp files work under the read-only rootfs; a
script that needs scratch space should write under `/scratch`.

> **Status (Epic 11.0):** the sandbox, the execution tools, the `--exec` gate,
> the `evidence_exec` data model, the two-run determinism rule, and the
> "Reproduced" badge renderer are all shipped and tested. The final wiring that
> **automatically** runs a skeptic's reproduction through the determinism rule
> and stamps `evidence_exec` onto the finding in a live run is gated behind the
> Epic 11.0 security review (it executes untrusted code, the highest-bar change),
> and is the activation step tracked for that review. Until then, `--exec` gives
> skeptics the `run_tests`/`run_script` tools (their output informs the verdict),
> and the `evidence_exec` block / badge render whenever the block is present.

## Determinism — flaky tests cannot poison evidence

A repro is only accepted as proof when the failure reproduces **deterministically**.
The command is run **twice**:

- both runs fail with the **same non-zero exit code** → `confirmed` (the finding
  is marked `VERIFIED`, with an `evidence_exec` block and a "Reproduced" badge);
- the exit codes **disagree**, either run **times out**, or both runs **pass** →
  `unverifiable` — the prior confidence is preserved, never promoted and never
  used to bury the finding.

## How the evidence flows

A reproduced finding carries an `evidence_exec` block in
`reconciled/findings.json` (`{ command, exit_code, output_excerpt }`), is stamped
`verification.verdict = "confirmed"` with `skeptic = "repro"`, and renders a
**Reproduced** badge in `report.md`. Because the verdict is `confirmed`, it is
`VERIFIED` by the existing confidence and gate rules — execution earns the
existing verdict by demonstration rather than adding a new tier. See
[findings-format.md](findings-format.md) and [verification.md](verification.md).

## Security posture

Execution runs untrusted, model-authored code. The container isolation above is
the containment boundary, and the feature is opt-in for exactly that reason.
Treat enabling `--exec` as a security decision: review the backend configuration
(image provenance, resource caps) before turning it on in CI, and prefer a
dedicated, minimal base image that carries only the toolchain your
`test_command` needs.
