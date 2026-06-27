---
id: TD-0024
order: 24
section: '[2026-06-18] From Sprint: epic-4.3'
date: "2026-06-18"
group: "1"
status: deferred
severity: LOW
file: internal/validation/validation.go:62
category: SECURITY
est_minutes: "30"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

System-dir denylist covers only /etc, /proc, /sys, leaving /boot, /dev, /root, /var and ~/.ssh writable via --output-dir / --output (deliberate Option-B permissive choice) (Won't-fix: intended Option-B policy per epic 4.3 clarifications 2026-06-18; revisit only on a concrete isolation requirement)

## Fix

If stronger isolation is later required, switch to an allowlist anchored at the repo/.atcr root instead of a denylist
