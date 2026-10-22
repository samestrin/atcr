# Epic Plan 99.1: Login hardening (fixture)

## Objective
Harden the login path. Fixture for the Epic 36.2 intent-verification tests:
the review diff wires `b()` into `a()` and nothing else.

## Tasks
- **T1** — Wire `b()` into `a()` so login runs the new check
- **T2** — Add `rateLimit()` to the login handler
- **T3** — Record failed logins with `auditLog()`
