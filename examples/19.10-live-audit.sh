#!/usr/bin/env bash
# atcr 19.10 live audit — replay the confirmed 19.6 failure range against the real
# roster and prove the per-model payload-sizing fixes (F1–F9) actually resolved it.
#
# The 19.6 multi-agent run (base f9d5161… → head b6bcb67…, 101 files / 6,429 insertions)
# returned 1 finding from 11 reviewers: dax failed with litellm.ContextWindowExceededError,
# otto with a BadRequestError naming --allow-auto-truncate, and greta/vera/brad timed out.
# This harness re-runs that exact range and hard-gates the fresh summary.json on the three
# AC-Live assertions, then prints a before/after evidence table vs the committed baseline.
#
# It is env-coupled: without a reachable roster (orchestrator.lan litellm proxy + the real
# 11-agent panel in .atcr/config.yaml) it SKIPs with exit 0, so it never blocks `go test ./...`
# or a CI job that happens to invoke it. It is deliberately NOT a `go test` (plan Constraints).
#
# Usage:   bash examples/19.10-live-audit.sh
# Env:     ATCR_BIN                 atcr binary/command (default: atcr)
#          ATCR_LIVE_AUDIT_BASE     base SHA   (default: f9d5161…, the confirmed 19.6 base)
#          ATCR_LIVE_AUDIT_HEAD     head SHA   (default: b6bcb67…, the confirmed 19.6 head)
#          ATCR_LIVE_AUDIT_BASELINE committed "before" review dir
#          ATCR_LIVE_AUDIT_OUT      fresh timestamped output dir (never overwrites baseline)
#          ATCR_LIVE_AUDIT_MIN_AGENTS  findings floor (default: 2 — "not 1")
#          ATCR_DOCTOR_TIMEOUT      per-call doctor probe timeout secs (default: 15)
# Exit:    0 pass (or SKIP), 1 gate failure, 2 usage/config/artifact error
set -euo pipefail

ATCR_BIN="${ATCR_BIN:-atcr}"
BASE_SHA="${ATCR_LIVE_AUDIT_BASE:-f9d5161be5b07214edc3fb435497d169883a3020}"
HEAD_SHA="${ATCR_LIVE_AUDIT_HEAD:-b6bcb676d2cbb461ed25f723e7daaae805589450}"
BASELINE_DIR="${ATCR_LIVE_AUDIT_BASELINE:-.planning/sprints/completed/19.6_community_registry_hub/code-review/multi-agent}"
OUT_DIR="${ATCR_LIVE_AUDIT_OUT:-.atcr/live-audit-$(date +%Y%m%d-%H%M%S)}"
MIN_AGENTS_WITH_FINDINGS="${ATCR_LIVE_AUDIT_MIN_AGENTS:-2}"
DOCTOR_TIMEOUT="${ATCR_DOCTOR_TIMEOUT:-15}"

# The five agents whose 19.6 failure this sprint set out to fix.
PREV_FAILED_AGENTS=(dax otto greta vera brad)

skip() { echo "SKIP: $*" >&2; echo "SKIP" ; exit 0; }
die()  { echo "ERROR: $*" >&2; echo "FAIL" ; exit 2; }

command -v jq >/dev/null 2>&1 || die "jq is required but not found on PATH"
# A missing/mis-named atcr binary is a usage error (exit 2), NOT a clean "roster
# unreachable" SKIP — otherwise a broken invocation would pass a CI gate green.
command -v "$ATCR_BIN" >/dev/null 2>&1 || die "atcr binary '$ATCR_BIN' not found on PATH; set ATCR_BIN (e.g. ATCR_BIN=./bin/atcr)"

# ---------------------------------------------------------------------------
# Step 1: early skip guard — is the live roster reachable at all?
#
# `atcr doctor` invokes every configured endpoint once. Exit 2 = config/usage error
# (no roster resolved); the JSON reports per-agent status where ok|ok_warning == reachable.
# ExitCode is not serialized, so we count healthy agents from the JSON. We skip ONLY on
# total unreachability (config error, or zero reachable agents) — a partial failure is
# itself the signal the gate below is designed to catch, so we let it proceed.
# ---------------------------------------------------------------------------
set +e
doctor_json="$("$ATCR_BIN" doctor --json --timeout "$DOCTOR_TIMEOUT" 2>/dev/null)"
doctor_rc=$?
set -e

if [ "$doctor_rc" -eq 2 ]; then
  skip "live roster unreachable — 'atcr doctor' reported a config/usage error (exit 2); check .atcr/config.yaml providers and orchestrator.lan. See examples/19.10-live-audit.sh"
fi

# The audit only needs the five previously-failed agents to be reachable. Skip
# (do not gate-fail) when none of those five are reachable, but allow the audit
# to proceed if any one of them is live — a partial failure is the signal this
# gate is designed to inspect.
reachable_names="$(printf '%s' "$doctor_json" | jq -r '.agents[] | select(.status == "ok" or .status == "ok_warning") | .name' 2>/dev/null || true)"
reachable_failed=0
for agent in "${PREV_FAILED_AGENTS[@]}"; do
  if printf '%s\n' "$reachable_names" | grep -qx "$agent"; then
    reachable_failed=$((reachable_failed + 1))
  fi
done
if [ "$reachable_failed" -eq 0 ]; then
  skip "live roster unreachable — none of the previously-failed agents (${PREV_FAILED_AGENTS[*]}) are reachable via 'atcr doctor' (orchestrator.lan or .atcr/config.yaml providers not configured). See examples/19.10-live-audit.sh"
fi
echo "doctor: $reachable_failed of ${#PREV_FAILED_AGENTS[@]} previously-failed agent(s) reachable — proceeding with the live audit" >&2

# ---------------------------------------------------------------------------
# Step 2: verify the committed "before" baseline exists (Step 6 reads it).
# Failing loudly here (not silently skipping the comparison) is the mitigation for the
# baseline being archived/moved further; it is an artifact error, not a gate failure.
# ---------------------------------------------------------------------------
BASELINE_SUMMARY="$BASELINE_DIR/sources/pool/summary.json"
[ -f "$BASELINE_SUMMARY" ] || die "baseline summary.json not found at $BASELINE_SUMMARY — the 19.6 'before' record may have moved; set ATCR_LIVE_AUDIT_BASELINE"
jq empty "$BASELINE_SUMMARY" >/dev/null 2>&1 || die "baseline summary.json is not valid JSON: $BASELINE_SUMMARY"

# ---------------------------------------------------------------------------
# Step 3: run the live review over the exact 19.6 range into a fresh output dir.
# Capture its exit code but do NOT relay it — a nonzero from partial agent failure is
# exactly the scenario the gate must inspect, not just forward. No --fail-on here (this
# script implements its own AC-Live gate, not atcr's generic severity gate).
# ---------------------------------------------------------------------------
echo "running: $ATCR_BIN review --base $BASE_SHA --head $HEAD_SHA --output-dir $OUT_DIR" >&2
set +e
"$ATCR_BIN" review --base "$BASE_SHA" --head "$HEAD_SHA" --output-dir "$OUT_DIR"
review_rc=$?
set -e
echo "atcr review exited $review_rc (nonzero is expected on partial failure; inspecting artifacts)" >&2

# ---------------------------------------------------------------------------
# Step 4: locate the fresh artifacts. Missing summary/manifest => the run crashed before
# producing artifacts (usage/artifact error, exit 2), distinct from a gate failure.
# ---------------------------------------------------------------------------
SUMMARY="$OUT_DIR/sources/pool/summary.json"
MANIFEST="$OUT_DIR/manifest.json"
[ -f "$SUMMARY" ]  || die "fresh summary.json not found at $SUMMARY — the review run did not complete (crashed before producing artifacts)"
[ -f "$MANIFEST" ] || die "fresh manifest.json not found at $MANIFEST — the review run did not complete (crashed before producing artifacts)"
jq empty "$SUMMARY" >/dev/null 2>&1 || die "fresh summary.json is not valid JSON: $SUMMARY"

# ---------------------------------------------------------------------------
# Step 5: the three AC-Live hard gates. Accumulate all violations before exiting so one
# run reports every failure, not just the first.
# ---------------------------------------------------------------------------
GATE_FAILED=0

# Gate A: zero ContextWindowExceededError across all agents.
if jq -e '[.agents[] | select(.error != null and (.error | contains("ContextWindowExceededError")))] | length == 0' "$SUMMARY" >/dev/null 2>&1; then
  echo "GATE A PASS: no ContextWindowExceededError across the roster" >&2
else
  GATE_FAILED=1
  echo "GATE A FAIL: ContextWindowExceededError present:" >&2
  jq -r '.agents[] | select(.error != null and (.error | contains("ContextWindowExceededError"))) | "  \(.agent): \(.error)"' "$SUMMARY" >&2 || true
fi

# Gate B: the five previously-failing agents all status=ok.
for agent in "${PREV_FAILED_AGENTS[@]}"; do
  if jq -e --arg a "$agent" '.agents[] | select(.agent == $a) | .status == "ok"' "$SUMMARY" >/dev/null 2>&1; then
    echo "GATE B PASS: $agent status=ok" >&2
  else
    GATE_FAILED=1
    actual="$(jq -r --arg a "$agent" '(.agents[] | select(.agent == $a) | "\(.status)\t\(.error // "")") // "ABSENT"' "$SUMMARY" 2>/dev/null || echo "ABSENT")"
    echo "GATE B FAIL: $agent expected status=ok, got: $actual" >&2
  fi
done

# Gate C: findings from >= MIN_AGENTS_WITH_FINDINGS distinct agents (count only; quality is
# reviewed by hand per AC-Live).
agents_with_findings="$(jq '[.agents[] | select(.findings_count > 0)] | length' "$SUMMARY" 2>/dev/null || echo 0)"
[ -n "$agents_with_findings" ] || agents_with_findings=0
if [ "$agents_with_findings" -ge "$MIN_AGENTS_WITH_FINDINGS" ]; then
  echo "GATE C PASS: $agents_with_findings agent(s) returned findings (floor $MIN_AGENTS_WITH_FINDINGS)" >&2
else
  GATE_FAILED=1
  echo "GATE C FAIL: only $agents_with_findings agent(s) returned findings (floor $MIN_AGENTS_WITH_FINDINGS):" >&2
  jq -r '.agents[] | select(.findings_count > 0) | "  \(.agent): \(.findings_count)"' "$SUMMARY" >&2 || true
fi

# ---------------------------------------------------------------------------
# Step 6: before/after evidence table (baseline vs this run), to stdout and a run artifact.
# ---------------------------------------------------------------------------
EVIDENCE="$OUT_DIR/live-audit-evidence.txt"
{
  echo "19.10 live-audit evidence — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "range: base=$BASE_SHA head=$HEAD_SHA"
  echo "before: $BASELINE_SUMMARY"
  echo "after:  $SUMMARY"
  echo
  printf '%-10s  %-10s  %-10s  %-9s  %-9s\n' "AGENT" "BEFORE" "AFTER" "B.FINDS" "A.FINDS"
  # Union of agent names from both summaries, joined on the agent key.
  jq -rn \
    --slurpfile b "$BASELINE_SUMMARY" \
    --slurpfile a "$SUMMARY" '
      ($b[0].agents // []) as $ba | ($a[0].agents // []) as $aa
      | ([($ba[].agent), ($aa[].agent)] | unique) as $names
      | ($ba | map({(.agent): .}) | add // {}) as $bm
      | ($aa | map({(.agent): .}) | add // {}) as $am
      | $names[]
      | . as $n
      | [ $n,
          ($bm[$n].status // "-"),
          ($am[$n].status // "-"),
          (($bm[$n].findings_count // 0) | tostring),
          (($am[$n].findings_count // 0) | tostring) ]
      | @tsv' \
    | while IFS=$'\t' read -r n bs as bf af; do
        printf '%-10s  %-10s  %-10s  %-9s  %-9s\n' "$n" "$bs" "$as" "$bf" "$af"
      done
  echo
  b_line="$(jq -r '"before: \([.agents[]|select(.status=="ok")]|length) ok / \([.agents[]|select(.status=="timeout")]|length) timeout / \([.agents[]|select(.status!="ok" and .status!="timeout")]|length) failed, \([.agents[]|select(.findings_count>0)]|length) agent(s) with findings"' "$BASELINE_SUMMARY")"
  a_line="$(jq -r '"after:  \([.agents[]|select(.status=="ok")]|length) ok / \([.agents[]|select(.status=="timeout")]|length) timeout / \([.agents[]|select(.status!="ok" and .status!="timeout")]|length) failed, \([.agents[]|select(.findings_count>0)]|length) agent(s) with findings"' "$SUMMARY")"
  echo "$b_line"
  echo "$a_line"
} | tee "$EVIDENCE"

echo "evidence written to $EVIDENCE" >&2

# ---------------------------------------------------------------------------
# Step 7: final one-line PASS/FAIL summary + exit code.
# ---------------------------------------------------------------------------
if [ "$GATE_FAILED" -eq 0 ]; then
  echo "PASS"
  exit 0
fi
echo "FAIL"
exit 1
