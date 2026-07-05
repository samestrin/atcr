We need to find API contract changes or breaking changes. The changed files include .planning/epics etc, but those are docs. The code changes: internal/reconcile/emit.go added fields Justification, SourceReport. Tests added. Also stampJustifications function added.

We need to see if any public API changed. JSONFinding struct is exported? It's in internal/reconcile, not exported to external callers? It's used internally. But findings.json is a public artifact. Adding fields to JSONFinding changes JSON schema. But fields are omitempty, so backward compatibility? The contract for findings.json is considered public. Adding new fields is additive, but may break consumers expecting exact fields? The contract says additive-only is fine. But if a consumer expects exact JSON keys? Usually JSON schema tolerant? But finders expecting unknown fields may error. The change adds fields but omitempty ensures no change if empty. But if a consumer expects no unknown fields, might break. But documentation indicates additive-only. The change is in internal code, but findings.json format is public. The change to JSONFinding struct adds new fields with omitempty; tests check that omission works.

But is there any breaking change? The struct is internal, but findings.json schema is public. The new fields are optional, so backward compatibility is maintained. However, the code that emits findings.json uses JSONFindings() to produce JSONFinding slices. That now includes new fields. But if a consumer reads JSON and expects no "justification" key, it's fine. If consumer expects "justification" to be absent, it will be omitted. So no breaking.

Check stampJustifications called before Emit. That adds fields only if matched. No change to other fields.

Potential breaking: The finder's struct has new fields but tests added, but no change in behavior of existing fields. No default shift.

Check other changes: in emit.go, the new fields added. No other changes.

Check code that uses JSONFinding elsewhere. Search references.We need to search for JSONFinding usage.But we can't run search. We need to reason. JSONFinding is used in internal/reconcile/emit.go to produce JSONFindings. Also tests. No other code uses fields Justification or SourceReport. That is fine.

Potential breaking: The JSONFinding struct has new fields that might affect Unmarshal? But JSONFindings writes JSON. Consumers reading old JSON won't see these keys, so fine. But if consumer uses struct with same name, might get zero values. That's fine.

But maybe the new fields cause a change in the order of JSON keys? JSON encoding order is by struct field order. Adding new fields changes key order. But JSON spec doesn't mandate order; but some tools may rely on order. But that's minor.

However, the change might break tests that compare JSON string. But tests added. No other tests.

Potential breaking: The new fields are exported, but their names start with capital letter. That means JSON tags will be "justification" and "source_report". That is fine.

Check for any other changed code. The new stampJustifications function is called in RunReconcile. That function uses slog.Debug. No breaking.

Check for any other code modifications: In emit.go, the new fields added after EvidenceExec. No other changes.

Check for any changes in other files: No other code changes. So likely no breaking API change.

But we need to check if any public function signature changed. The only public function is RunReconcile, but signature unchanged. The new stampJustifications is unexported.

We need to check if any other public function changed. Let's search for exported functions in internal/reconcile. There's RunReconcile, JSONFindings, Emit, etc. No changes.

Thus likely no breaking change. But we must be careful: the new fields are added to JSONFinding, but the struct is used in JSONFindings. The function JSONFindings() returns []JSONFinding. That is used by external code? The findings.json is output. The tests check that JSONFinding struct marshals correctly. But no API signature change.

Thus no findings.

However, the change might alter the JSON output format. But as per additive-only policy, it's fine. But the change adds "justification" and "source_report" fields. If a consumer expects strict schema, might break. But it's documented as additive.

We should consider that the JSONFinding struct is in internal package, but findings.json is public. The change is additive, so it's okay. But the new fields are optional. So no breaking.

But we must check for any change in return type of JSONFindings. It returns []JSONFinding. No change.

Check for any default shift. No.

Check for error contract changes. No.

Thus likely no findings.

But maybe there's a breaking change: The new fields are added to JSONFinding struct, but the tests refer to JSONFinding struct from internal/reconcile. That is internal; tests are inside same package. That is okay.

However, the change might cause the tests to fail if the JSONFindings() now includes the new fields incorrectly. But tests added check that JSONFinding marshals correctly. That should pass.

Thus no findings.

But we must double-check if any function signature changed. In emit.go, the struct changed but no function signatures. In stampJustifications, no exported functions.

Check for any change in internal/reconcile/gate.go calling stampJustifications. That function is defined in internal/reconcile/justification.go. That file is new. The code compiles.

Thus no breaking change.

Thus output nothing.