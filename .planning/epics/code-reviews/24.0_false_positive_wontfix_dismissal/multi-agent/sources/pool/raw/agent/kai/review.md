 The user wants me to act as "kai", an architecture and design reviewer, and review the changed files/lines in the provided diff against the sprint plan for Epic 24.0.

Epic 24.0 is about adding a "wontfix" dismissal mechanism to the ATCR CLI's debt resolution system.

Changed files:
1. `.planning/epics/active/24.0_false_positive_wontfix_dismissal.md` → `.planning/epics/completed/...` (rename only, no content change)
2. `.planning/technical-debt/README.md` - Stats update and new TD items added from epic 24.0
3. `CHANGELOG.md` - Added changelog entry for 24.0.0
4. `cmd/atcr/debt_resolve.go` - Core logic changes: new flags, validation, `isClosedStatus` updated, `markDebtResolved` signature change
5. `cmd/atcr/debt_resolve_test.go` - New tests for wontfix, reason, validation
6. `cmd/atcr/reconcile_test.go` - New test for dedup suppression

I need to find architecture/design issues specifically in the changed regions. Focus on:
- Boundary violations
- Coupling
- Contract design
- Duplication of responsibility
- Extensibility traps

Scope constraint: Stay on the diff. Only flag issues whose FILE:LINE falls within changed lines. If I must flag pre-existing issues, use CATEGORY out-of-scope.

Let me analyze the actual code changes in detail.

**cmd/atcr/debt_resolve.go changes:**

Lines 28-32: New `resolveStatuses` map. Only "resolved" and "wontfix". Comment says "deferred" is intentionally excluded because written by other paths.

Lines 55-56: Added flags `--status` (default "resolved") and `--reason`.

Lines 63-76: Run function changes.
- Gets `id` from `--resolve`
- Checks if `id == ""` and (`Changed("status")` or `TrimSpace(reason) != ""`), returns usage error.
- Then validates status against `resolveStatuses`.
- Note: `mustFlag` likely returns string. `cmd.Flags().Changed("status")` is used.

Wait, looking closely at lines 65-66:
```go
if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
    return usageError(fmt.Errorf("--status/--reason require --resolve <id>"))
}
```

This checks `Changed("status")` OR non-empty reason. But what about the case where `--status` is provided but equals the default "resolved"? Actually `Changed("status")` returns true if the flag was set by the user, regardless of value. So if someone runs `--resolve abc --status resolved`, that's fine. If someone runs `--status resolved` without `--resolve`, it errors. Good.

But wait: `mustFlag(cmd, "status")` at line 71: it gets the flag value. It does `strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status")))`. But the default is "resolved" from flag definition. If the user doesn't specify `--status`, `Changed("status")` is false, but `mustFlag` returns "resolved". So if `id != ""`, it proceeds to validate. That's fine since default is valid.

However, there's a subtle contract issue: the `markDebtResolved` function is now called `markDebtResolved(cmd, dir, id, status, reason)`. It records `rec.Status = status` and `rec.ResolvedAt = now`. Even for `wontfix`, it sets `ResolvedAt`. Is that correct? The record has a `ResolvedAt` field. For `wontfix`, is resolution time semantically correct? Well, it's a terminal status, and the field already exists in the schema. But is the field name `ResolvedAt` implying resolved only, or is it generic closed time? The existing schema probably uses `ResolvedAt` for both. But the epic doesn't mention renaming it. The field is set for both statuses. That's probably fine as a closed-at timestamp, though the name is slightly misleading for wontfix. Maybe not a real issue since existing schema.

Line 225: `alreadyClosed` message says `fmt.Fprintf(cmd.OutOrStdout(), "%s is already resolved; nothing to do.\n", id)`. But the `.planning/technical-debt/README.md` new item at line 225 mentions this: "already-closed message hardcodes 'already resolved' so re-marking a wontfix item prints an inaccurate 'already resolved' line". This was actually caught and added to the TD file! The code change at line 225 (wait, is line 225 in the diff? Let me check the diff).

Looking at the diff for `debt_resolve.go`:

Line 225 in the old file is around... let me map lines.

Old `markDebtResolved`:
```go
func markDebtResolved(cmd *cobra.Command, dir, id string) error {
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return fmt.Errorf("atcr debt resolve: failed to read local debt store: %w", err)
	}
// ...
	if alreadyClosed {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already resolved; nothing to do.\n", id)
		return nil
	}
```

Line 225 in the new file? The diff shows:
```go
 	if alreadyClosed {
-		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already resolved; nothing to do.\n", id)
+		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already resolved; nothing to do.\n", id)
 		return nil
 	}
```

Wait, looking carefully at the diff... Actually line 225 in the diff context. Let me read the diff lines:

```
@@ -193,51 +215,57 @@ func renderResolveList(w io.Writer, recs []localdebt.Record) error {
 // markDebtResolved records an append-only resolution for id: it copies the item's
 // open record, stamps a terminal status/timestamp, and appends it so the fold in
 // selectOpenDebt drops the item from the open list. The stable id is preserved
 // (never re-stamped) so the resolution lines up with the original finding.
-func markDebtResolved(cmd *cobra.Command, dir, id string) error {
+func markDebtResolved(cmd *cobra.Command, dir, id, status, reason string) error {
 	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
 	if err != nil {
 		return fmt.Errorf("atcr debt resolve: failed to read local debt store: %w", err)
 	}
 
 	var orig *localdebt.Record
 	var alreadyClosed bool
 	for i := range recs {
 		if recs[i].ID != id {
 			continue
 		}
 		if isClosedStatus(recs[i].Status) {
 			alreadyClosed = true
 			continue
 		}
 		if orig == nil && recs[i].File != "" {
 			r := recs[i]
 			orig = &r
 		}
 	}
 	// Concurrency-tolerant, not lock-protected: a terminal record for this id already
 	// exists, so this invocation reports and no-ops instead of appending a duplicate
 	// resolution record. Two concurrent invocations can each pass this check before
 	// either appends (the accepted TD-004 no-lock stance); selectOpenDebt's append-only
 	// fold treats any extra resolution record for an already-closed id as redundant, so
 	// the result is harmless duplicate bloat, not corruption.
 	if alreadyClosed {
 		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already resolved; nothing to do.\n", id)
 		return nil
 	}
```

Wait, the line `_, _ = fmt.Fprintf(...)` did NOT change in the diff! It still says "already resolved". So the TD item mentions line 225 (which is this line in the new file). Since the line is visible in the diff context (unchanged line surrounded by changed lines), but the line itself is NOT changed. The instruction says: "Review only the changed regions. ... If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope so the reconciler annotates rather than discards it."

However, the TD item was added to README.md as part of the diff! That's meta. But the code at line 225 is unchanged. Since I need to flag it as out-of-scope if I flag it. But is it genuinely pre-existing? Yes, the message "already resolved" was there before and remains. The new feature adds `wontfix` but doesn't update the message. This is indeed a pre-existing issue that becomes more apparent with the new feature. But the line itself is not in the changed hunk (it's in the context). So if I flag it, I must use CATEGORY out-of-scope. But wait, I could argue that leaving the message unchanged while adding wontfix is a design flaw in the change itself — the change introduces wontfix but fails to adapt the already-closed feedback to be status-agnostic. However the actual line of code is unchanged; the flaw is in the lack of modification to an existing line. Does that count as "changed region"? The diff shows the function signature changed, but that specific line is context. Strictly speaking, the code at that line is not changed. So per scope constraints, if I flag it, it must be out-of-scope. But is it critical/high? No. Low or Medium.

Actually, looking at the TD README, the team already identified this as a finding and added it to their TD. So maybe I don't need to flag it. But the prompt says I am kai finding problems. The fact that they already tracked it suggests it's valid. Since it's unchanged code, I'll skip it or mark out-of-scope. But probably better to skip since they already know.

Let's look for other issues.

**Contract Design Issues:**

1. **The `--status` flag has a default of "resolved" but the validation map only contains lower-case keys.** The flag default is `"resolved"` (lowercase). The user could pass `--status Resolved` or `--status WONTFIX`. The code lowercases and trims before checking the map. Good. But what about `deferred`? `deferred` is a valid closed status per `isClosedStatus`, but `resolveStatuses` explicitly excludes it:
```go
// "deferred" is intentionally excluded — it is written by other paths, not by an explicit resolve.
var resolveStatuses = map[string]bool{"resolved": true, "wontfix": true}
```
This is a contract choice: `--status deferred` is rejected. Is this a design flaw? The comment explains why. It's intentional. But is it extensible? If later they want to let users defer via CLI, they'd need to add it. Not a problem.

2. **`ResolvedAt` field semantics for `wontfix`:** In `markDebtResolved`, the code sets `rec.ResolvedAt = now` regardless of status. For a wontfix dismissal, calling it "ResolvedAt" is semantically misleading — it wasn't resolved, it was dismissed. This is an API/contract issue: the schema conflates "closed time" with "resolution time". If downstream consumers interpret `ResolvedAt` as "when the fix was applied", wontfix records will mislead them. However, this is a pre-existing field in the schema (`internal/localdebt/record.go`). The diff doesn't change the schema, only populates it. Since the schema is pre-existing and not in the changed lines, this might be out-of-scope. But the change leverages the field for wontfix without acknowledging the semantic drift. Actually, the line `rec.ResolvedAt = now` is in the changed region! So I can flag the line itself because it was carried over from the old code but is now being applied to a new status; wait, the line `rec.ResolvedAt = now` existed before and is still there. Is it a changed line? In the diff:
```go
 	rec.Status = status
 	rec.ResolvedAt = now
```
The line `rec.ResolvedAt = now` is unchanged but appears in the hunk. Since it's in the diff hunk (lines added/removed context), the FILE:LINE must be in the changed lines? No, the instruction says "a finding whose FILE:LINE falls outside the changed lines will be discarded before it reaches the report when grounding is active". `rec.ResolvedAt = now` is not marked with `+` or `-` in the diff; it's context line. But sometimes diff context lines are considered "visible in surrounding context but not changed". The rule says: "Review only the changed regions. The payload shows you the change in context, but a finding whose FILE:LINE falls outside the changed lines will be discarded before it reaches the report when grounding is active (git-range reviews)". So I should only flag lines that are added or removed.

Changed lines in `cmd/atcr/debt_resolve.go`:
- Lines 28-32: `resolveStatuses` var added
- Lines 55-56: Flag definitions added
- Lines 61-76: RunE logic changed heavily
- Lines 93-96: `isClosedStatus` updated
- Lines 215-257: `markDebtResolved` signature and body changed (lines 215, 220, 244, 247-257 are `+` lines)

Let's map to new file lines. The diff hunk headers show:
`@@ -23,72 +23,94 @@` for the first part.
`@@ -193,51 +215,57 @@` for markDebtResolved.

So new lines 215+ are the changed markDebtResolved.

On line 253 (new): `rec.ResolvedAt = now`. This is a `+` line? Let's see:
```
+	rec.Status = status
+	rec.ResolvedAt = now
```
Yes, these are added lines (the old code had these but maybe without the `+` in front in the diff? Wait, the diff shows these lines with `+` because the function was entirely re-indented or re-worked? Let's verify.

Old function body had:
```go
	rec.Status = "resolved"
	rec.ResolvedAt = now
```
New function body has:
```go
	rec.Status = status
	rec.ResolvedAt = now
```
The line `rec.ResolvedAt = now` is unchanged in content but appears in a hunk of added lines because the hunks are offset and the diff algorithm shows it as part of the added block. Actually in unified diff, a line with no prefix is context. Wait, in the diff block for the second hunk, are there context lines?

Looking closely:
```diff
@@ -193,51 +215,57 @@ func renderResolveList(w io.Writer, recs []localdebt.Record) error {
 // markDebtResolved records an append-only resolution for id: it copies the item's
 ...
-func markDebtResolved(cmd *cobra.Command, dir, id string) error {
+func markDebtResolved(cmd *cobra.Command, dir, id, status, reason string) error {
 	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
 	if err != nil {
 		return fmt.Errorf("atcr debt resolve: failed to read local debt store: %w", err)
 	}
 
 	var orig *localdebt.Record
 	var alreadyClosed bool
 	for i := range recs {
 		if recs[i].ID != id {
 			continue
 		}
 		if isClosedStatus(recs[i].Status) {
 			alreadyClosed = true
 			continue
 		}
 		if orig == nil && recs[i].File != "" {
 			r := recs[i]
 			orig = &r
 		}
 	}
 	// Concurrency-tolerant, not lock-protected...
 	if alreadyClosed {
 		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already resolved; nothing to do.\n", id)
 		return nil
 	}
 	if orig == nil {
 		return fmt.Errorf("no open technical-debt item with id %q in the local store", id)
 	}
 
 	now := time.Now().UTC().Format(time.RFC3339)
 	rec := *orig
-	rec.RunID = now + "-resolve"
+	rec.RunID = now + "-" + status
 	rec.Timestamp = now
-	rec.Status = "resolved"
+	rec.Status = status
 	rec.ResolvedAt = now
+	// A supplied --reason records why...
+	if r := strings.TrimSpace(reason); r != "" {
+		rec.Justification = r
+	}
 	if err := localdebt.Append(dir, rec); err != nil {
 		return fmt.Errorf("atcr debt resolve: failed to record resolution: %w", err)
 	}
-	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Marked %s resolved.\n", id)
+	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Marked %s %s.\n", id, status)
 	return nil
 }
```

I see `rec.ResolvedAt = now` has no prefix in the diff! Wait, it's shown without `+` or `-`. Let me recount:
- `-rec.Status = "resolved"` (old)
- `+rec.Status = status` (new)
- ` rec.ResolvedAt = now` (context line? no prefix)

Actually in unified diff, lines without `+` or `-` are context lines that exist in both. However, because this is a completely rewritten hunk with many lines changed, context lines can appear. The line `rec.ResolvedAt = now` is a context line (no `+`/`-`). So its FILE:LINE is technically in the old file and new file but not "changed". So if I flag it, it might be discarded unless I treat it as out-of-scope.

But the line `rec.RunID = now + "-resolve"` changed to `rec.RunID = now + "-" + status`. That's a changed line (old removed, new added). And `rec.Status = status` is added replacing `rec.Status = "resolved"`.

**Key Architecture/Design Findings:**

1. **Boundary/Coupling: CLI directly constructs `localdebt.Record` fields instead of using a domain method.**
`markDebtResolved` manually copies fields from `orig`, sets `Status`, `ResolvedAt`, `RunID`, `Timestamp`, `Justification`. This is a contract duplication: the knowledge of how to create a terminal status record lives in the CLI layer rather than in `internal/localdebt`. If the record schema evolves (e.g., adding a new required field for terminal records), every CLI/cmd caller must be updated. This is coupling the CLI to the internal representation of a record mutation.
- File: `cmd/atcr/debt_resolve.go`
- Line: around 247-253 (the block setting fields on `rec`)
- Problem: CLI layer manually fabricates terminal record mutation instead of delegating to `internal/localdebt`
- Fix: Add a `localdebt.Record.WithTerminal(status, reason string) Record` method or similar constructor so the persistence layer owns the shape of a status record
- Category: coupling
- Severity: MEDIUM

2. **Contract Design: `RunID` format change introduces implicit dependency between CLI and downstream consumers.**
`rec.RunID = now + "-" + status`. The `RunID` format has changed from a static suffix `-resolve` to a dynamic suffix `-<status>`. If any downstream parser, log aggregator, or skill expects `RunID` to end with `-resolve` to identify resolution records, this change breaks them. The `RunID` is used as a unique identifier for a run; making its format depend on user input (`status`) leaks the status enum into the ID space and couples RunID semantics to the CLI flag values.
- File: `cmd/atcr/debt_resolve.go`
- Line: 248 (`rec.RunID = now + "-" + status`)
- Problem: `RunID` format now encodes status as suffix, coupling ID generation to CLI flag values and potentially breaking downstream parsers that key off `-resolve`
- Fix: Keep `RunID` a stable run identifier (e.g., `now + "-resolve"` regardless of status) and store the status in its own field; don't encode semantic metadata in opaque IDs
- Category: contracts
- Severity: MEDIUM (or HIGH if there's a downstream consumer). The prompt mentions "APIs that lie" and "error types that lose information". This is an ID contract change. Probably MEDIUM because it's not guaranteed to break something, but it's a trap.

3. **Contract Design: `ResolvedAt` is set for `wontfix` records, conflating semantics.**
Even though the line is context, the *act* of applying it to wontfix is driven by the changed code. However, the line itself is unchanged. But the design decision in the changed code is to keep setting `ResolvedAt` for non-resolved statuses. Since the line is context, maybe I can't flag it strictly. But I can flag the design indirectly: the `markDebtResolved` function's name and its setting of `ResolvedAt` on a `wontfix` record. Wait, the function name `markDebtResolved` didn't change (the diff shows it was `markDebtResolved` before and after? Actually the diff shows `-func markDebtResolved(...)` and `+func markDebtResolved(...)` with new params. So the function name is in the changed region (line 215). I can flag the function name as a contract issue: it is now used to mark `wontfix` too, but its name says "Resolved". That's a naming/contract mismatch.
- File: `cmd/atcr/debt_resolve.go`
- Line: 215
- Problem: `markDebtResolved` function name implies resolution-only but now handles any terminal status including wontfix; callers pass `status` into a resolver-named function
- Fix: Rename to `markDebtTerminal` or `recordTerminalStatus` to reflect the generalized contract
- Category: contracts
- Severity: LOW

4. **Duplication of Responsibility: Validation of status enum exists in CLI and implicitly in `isClosedStatus`.**
`resolveStatuses` map defines valid input statuses. `isClosedStatus` defines what folds out. They are separate but related. If `isClosedStatus` is updated to include a new terminal status (e.g., "accepted"), but `resolveStatuses` isn't, the CLI rejects it. If `resolveStatuses` gets a new one but `isClosedStatus` doesn't, the record is written but won't fold out. This is two sources of truth for the status enum.
- File: `cmd/atcr/debt_resolve.go`
- Lines: 28-32 (`resolveStatuses`) and 93-96 (`isClosedStatus`)
- Problem: Status enum is split between `resolveStatuses` (write-side validation) and `isClosedStatus` (read-side folding); they can drift
- Fix: Derive `resolveStatuses` from the same canonical set as `isClosedStatus`, or have `isClosedStatus` reference a shared `closedStatuses` slice/map
- Category: duplication
- Severity: MEDIUM

5. **Coupling: `alreadyClosed` message hardcodes "resolved" but `isClosedStatus` now includes `wontfix`.**
The line `fmt.Fprintf(..., "%s is already resolved; nothing to do.\n", id)` is in context (unchanged). But the *changed* code around it calls `isClosedStatus` which now includes `wontfix`. So the message becomes inaccurate for `wontfix`. However, since the line itself is unchanged, I must flag it as out-of-scope if I flag it. Wait, the instruction says: "If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope so the reconciler annotates rather than discards it."
So I can flag it:
- File: `cmd/atcr/debt_resolve.go`
- Line: let's estimate. The diff hunk starts at old line 193, new line 215. The `alreadyClosed` block is about 30 lines into the new hunk, so maybe around line 244? Actually in the new file, `markDebtResolved` starts at line 215. The `alreadyClosed` check is around line 242. Let's calculate:
Line 215: func signature
Line 216-217: recs, err
Line 218-220: err check
Line 221: var orig
Line 222: var alreadyClosed
Line 223-235: for loop (about 12 lines)
Line 236-240: comment block (5 lines)
Line 241: if alreadyClosed {
Line 242: _, _ = fmt.Fprintf(...)
So line 242.
Problem: Message hardcodes "resolved" but now applies to any closed status including wontfix
Fix: Parameterize message to say "already closed" or status-specific
Category: out-of-scope (since line is unchanged)
Severity: LOW
Evidence: `fmt.Fprintf(... "%s is already resolved; nothing to do.\n", id)` alongside `isClosedStatus` that now returns true for wontfix.

But they already tracked this in README.md. So maybe redundant. I'll skip to avoid noise, or include as out-of-scope since it's a genuine issue exposed by the change.

6. **Extensibility Trap: `cmd.Flags().Changed("status")` guard only checks explicit status, not default collisions.**
The guard at lines 65-66 checks if `--status` was Changed or `--reason` non-empty. But what if `--resolve` is provided with `--status` default (i.e., the user explicitly writes `--status resolved`, which equals the default)? `Changed("status")` is true, but `id` is non-empty, so the guard doesn't trigger. That's fine. The guard is only for missing `--resolve`.

But what about the future: if they add a new terminal status that other paths can write (like "deferred"), `isClosedStatus` includes it, but `resolveStatuses` does not. The `alreadyClosed` logic will already handle a record that is closed by "deferred" (from other paths). If the user tries `--resolve id --status deferred`, it's rejected. That's correct per spec. Not an issue.

7. **Test coupling: Tests reach into `localdebt` directly instead of using the CLI contract.**
In `reconcile_test.go`, the new test `TestPersistLocalDebt_WontfixSuppressesReappend` constructs a `localdebt.Record{}` manually, calls `seed.StampID()`, `localdebt.Append()`, then calls `persistLocalDebt(...)` directly. This is white-box testing of an unexported function. But this is consistent with the existing test style in the file. Not a new design issue introduced by this change; it's pre-existing test approach.

8. **Boundary: `reason` is stored as plain string, no length limit or sanitization.**
The reason string is trimmed and stored directly in `rec.Justification`. This could lead to arbitrary length justification stored in the append-only ledger. But existing schema probably allows any string. Not necessarily a new issue.

9. **`rec.ResolvedAt` set for wontfix in changed lines?** As discussed, the line is context. But the changed line `rec.RunID = now + "-" + status` could be flagged for using `status` directly in a field that may be used as an ID or filename. Actually, `RunID` is just a string field, not a filename. But it appears in JSONL records. If `status` contains spaces (it shouldn't because validated), but it's validated against a map. Good.

10. **Reconcile test dedup test: `persistLocalDebt` called directly.**
The new test calls `persistLocalDebt` directly. This is an existing pattern. Not new.

11. **TD README additions: The new TD items themselves identify issues.**
Since I am reviewing the change, and the change includes adding these TD items, I should not flag the TD items as issues in the code (they are acknowledged). However, the fact that the team is adding TD items for things like "already-closed message hardcodes resolved" suggests they are deferring fixes. I don't need to re-report them unless they are critical.

12. **Contract: `isClosedStatus` lowercases and trims `status`. But `resolveStatuses` keys are lowercase. This is consistent. However, the `Status` field on the `Record` stores whatever was passed. If `markDebtResolved` stores "wontfix" (lowercase), but an external writer writes "WONTFIX", `isClosedStatus` handles it, but `selectOpenDebt` uses exact string match? No, `selectOpenDebt` calls `isClosedStatus` which lowercases. So it's case-insensitive on read. But the storage has mixed case potentially. That's okay.

Wait, looking at `selectOpenDebt`, it looks at `recs[i].Status` and calls `isClosedStatus`. So it's case-insensitive on read. But the CLI validation enforces lowercase at write. Good.

But there's a subtle issue: the `localdebt` package may have other writers (reconcile). Reconcile writes status as empty string. So no conflict.

13. **Duplication: The new test `TestDebtResolve_WontfixStatusFoldsItemOutOfOpenList` duplicates the folding logic test that already exists implicitly.**
Not really a design flaw.

14. **Command name and flag design: `--resolve <id> --status wontfix` is slightly awkward.**
The epic originally considered `--dismiss <id>`. The refinement suggested adding `--dismiss` alias. The final implementation uses `--resolve <id> --status wontfix`. This is a CLI contract: users must know to combine `--resolve` with `--status wontfix`. Is this a design flaw? The epic plan accepted this approach per refinement item 1: "Reuse atcr debt resolve/isClosedStatus instead of a new atcr dismiss subsystem." So it's intentional. However, from a contract perspective, `--resolve` as the verb with `wontfix` as the status is slightly misleading: you are not resolving it, you are dismissing it. But the plan explicitly chose this. I should not second-guess the epic's chosen direction unless it's a genuine architectural violation.

15. **Critical? The `markDebtResolved` function returns "already resolved" for wontfix items that are already closed.** This is inaccurate but not contract-breaking for other components.

16. **The `AlreadyClosed` logic and `orig == nil` logic: If a record is closed but also open?**
The loop sets `alreadyClosed = true` if any record with that ID has `isClosedStatus`. Then it continues. Then if `orig == nil && recs[i].File != ""`, it captures the first open record. So if there's a closed record and an open record for the same ID, `alreadyClosed` wins and it no-ops, reporting "already resolved". Is that correct? Actually, in an append-only log, if you have: open rec, then resolved rec, then open rec (same ID re-detected by reconcile), then `alreadyClosed` would be true because it scans the whole history and finds the resolved record. Then it reports already resolved and does nothing. That means a wontfix/res finding stays closed even if a new open record appears later. That seems correct per the fold logic: any terminal record closes the ID. But what if the new open record is actually a new finding with the same ID? The ID is derived from file+line+problem hash, so it's the same finding. So suppression is intended. Good.

But wait: what if a user runs `--resolve <id> --status wontfix` on an ID that is already closed with status `resolved`? It says "already resolved; nothing to do." Accurate enough. What if it's already `wontfix` and they run `--resolve <id> --status resolved`? It says "already resolved" and no-ops. That prevents changing a wontfix to resolved via the CLI. Is that intentional? Probably yes: you can't change terminal status once closed without manual store editing. That could be an extensibility trap: if a user mistakenly marks wontfix and wants to mark resolved later, they can't. But that's inherent to the append-only design.

17. **The guard `id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "")` does not guard against `--resolve=""` (empty string ID).**
If someone passes `--resolve "" --status wontfix`, `id` is empty string. The guard checks `id == ""`. But does `--resolve` allow empty string? `mustFlag` returns empty string if not set? Actually `mustFlag` likely returns the value. If the user passes `--resolve=""`, `id` would be `""`. Then the guard fires and returns usage error. But wait: what if they pass `--resolve` with empty string? The first guard checks `id == ""`. But is `id` empty from `mustFlag` different from "not provided"? In Cobra, if a flag is not provided, `mustFlag` returns empty string (or default). So the guard treats "not provided" and "provided as empty" the same. But `--resolve` without value might be an error from Cobra itself depending on flag type. Not a big issue.

18. **The `reason` flag is called `--reason` but maps to `Justification` field.**
This is a naming inconsistency between CLI and domain model. The epic plan specifically says: "Add a --reason flag ... that populates the record's Justification field". So it's intentional, but still a mapping between two names. Not necessarily a flaw.

19. **The test `TestDebtResolve_MarkWontfixSetsStatusAndFoldsOut` scans `recs` with `recs[i].Status == "wontfix"`. This couples the test to the exact string value "wontfix", but that's the contract under test.**

20. **The `CHANGELOG.md` change is just documentation.**

21. **The `.planning/technical-debt/README.md` change includes adding TD items. Is there an issue there?**
The stats were updated: Open items went from 0 to 3. This is because they added 3 new TD items. That's fine.

One of the new TD items mentions: `cmd/atcr/reconcile.go:203` pre-existing fail-open branch means corrupt store re-appends wontfixed finding. But `cmd/atcr/reconcile.go` is not in the diff. The finding was added to the TD file as text, not as a code change. Since it's not in the changed code regions, I ignore it.

**Most impactful design findings to report:**

A. **Duplication of status enum:** `resolveStatuses` and `isClosedStatus` are two sources of truth. This is a clear MEDIUM finding on changed lines.

B. **Coupling/Boundary violation:** `cmd/atcr` manually constructs the terminal record. Specifically, `markDebtResolved` knows how to copy `orig`, set `RunID`, `Timestamp`, `Status`, `ResolvedAt`, and optionally `Justification`. This logic belongs in `internal/localdebt`. If other CLI commands or services need to mark records, they'd duplicate this. It's a medium-level coupling issue.

C. **Contract in RunID:** `rec.RunID = now + "-" + status` in the new code. This encodes the status into a field meant to identify the run. If any downstream system parses `RunID` to detect resolutions, it will break. Or if status values change, old RunIDs become inconsistent. It's a contract fragility. This is a changed line, so valid.

D. **Function name `markDebtResolved` no longer matches its generalized behavior.** Since the function signature is changed, I can flag it at line 215.

E. **The already-closed message mismatch.** The line is unchanged but exposed by the change. I can flag as out-of-scope LOW.

F. **Pre-existing `ResolvedAt` semantics.** Since the line is context, and the epic ignores schema changes, I should probably not flag it, or flag as out-of-scope. But the instruction says to suppress out-of-scope. I'll skip this to focus on the diff.

Let me verify line numbers for the changed additions.

For `cmd/atcr/debt_resolve.go`:
- Hunk 1 starts at new line 23. The file is 94 lines after. Let's map:
  - Lines 23-30: comment + `resolveSeverities` var unchanged context.
  - Lines 28-32: `resolveStatuses` var added. Wait, the diff shows:
    ```
    +// resolveStatuses is the validated --status enum for a mark action. Both values are
    +// terminal (isClosedStatus folds them out): "resolved" means the code was actually
    +// fixed; "wontfix" (Epic 24.0) dismisses a false-positive/accepted pattern so agents
    +// stop re-surfacing it. "deferred" is intentionally excluded — it is written by other
    +// paths, not by an explicit resolve.
    +var resolveStatuses = map[string]bool{"resolved": true, "wontfix": true}
    ```
    So new lines 28-32.
  - Lines 55-56: flags added.
  - Lines 61-76: RunE logic changed. New line 61 is `id := mustFlag(cmd, "resolve")`. New line 65 is the guard. New line 71 is status validation. These are all changed.
  - Lines 93-96: `isClosedStatus` changed. New lines 90-99 approximately.
  - Hunk 2: new lines 215-271 (function signature through end).
    - Line 215: new function signature.
    - Line 248: `rec.RunID = now + "-" + status` (added).
    - Line 250: `rec.Status = status` (added).
    - Line 253-256: reason block (added).
    - Line 260: `Marked %s %s` (added).

Wait, the diff doesn't show line 248 explicitly with a `+` for