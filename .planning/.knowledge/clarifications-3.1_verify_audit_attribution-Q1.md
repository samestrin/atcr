---
id: mem-2026-06-14-a5ac45
question: "On the tool_harness_unavailable early-return path in verifyFinding, should VerificationResult.Model record the joined candidate skeptic models or empty string \"\"?"
created: 2026-06-14
last_retrieved: ""
sprints: []
files: [internal/verify/pipeline.go, internal/verify/emit_verification.go, internal/verify/invoke.go]
tags: [clarifications, epic-3.1_verify_audit_attribution, architecture, audit-trail]
retrievals: 0
status: active
type: epic-3.1_verify_audit_attribution
---

# On the tool_harness_unavailable early-return path in verifyF

## Decision

Use empty string "" on the tool_harness_unavailable path. The candidate models were never invoked and produced no output; recording their names in Model would misattribute an audit record to models that executed nothing. The Verdict ("unverifiable") and Notes ("tool_harness_unavailable") fields already tell an auditor exactly why the record is empty — no secondary Model signal is needed to disambiguate. AC3 explicitly permits "" on this path. Consistency with the no_eligible_skeptic path (which also leaves Model empty) makes both infrastructure-failure cases behave identically, reducing special-case surface area in the implementation. The codebase's audit intent (visible in invokeSkeptic at invoke.go:63-69) is to attribute only to what actually ran — extending that principle to Model means "" on harness failure is correct. Key references: pipeline.go:350-358 (both early-return paths leave base.Model at zero value); emit_verification.go:24-35 (Model is a plain string with no sentinel convention); invoke.go:63-69 (failure-isolation contract always sets Skeptic to the name of what ran, never to candidates).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/pipeline.go
- internal/verify/emit_verification.go
- internal/verify/invoke.go
