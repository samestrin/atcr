The user wants me to review the provided diff as "archer", a snippet-level bug reviewer.
I need to look for bugs at the line/expression level in the changed code.
Focus areas: off-by-one, null/nil/undefined, logic inversions, copy-paste errors, shadowing/dead paths.
Scope: only changed regions. The payload shows changes in `.planning/technical-debt/README.md`, `CHANGELOG.md`, `cmd/atcr/debt_resolve.go`, `cmd/atcr/debt_resolve_test.go`, `cmd/atcr/reconcile_test.go`.
I will ignore the plan document itself as it's just metadata, but I'll check the code changes.

Let's look at `cmd/atcr/debt_resolve.go`:
Line ~160: `if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {`
Wait, `mustFlag(cmd, "reason")` might panic or return empty if not set? Cobra's `Flags().String()` returns the default value if not changed. The default for `--reason` is `""`. So `strings.TrimSpace("")` is `""`, `!= ""` is false. That's fine.
But wait, `cmd.Flags().Changed("status")` is true if explicitly passed. What about `--reason`? It checks `strings.TrimSpace(mustFlag(cmd, "reason")) != ""`. If user passes `--reason ""`, it's empty string. Is that intended? Probably fine.

Line ~175: `status := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status")))`
Default is `"resolved"`. `strings.ToLower("resolved")` -> `"resolved"`. Checks against `resolveStatuses`. Fine.

Line ~230: `rec.RunID = now + "-" + status`
Wait, `now` is formatted as `time.RFC3339`, e.g., `2026-07-13T10:00:00Z`. So `RunID` becomes `2026-07-13T10:00:00Z-resolved`. Previously it was `now + "-resolve"`. This is fine.

Line ~245: `if r := strings.TrimSpace(reason); r != "" { rec.Justification = r }`
Fine.

Line ~250: `_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Marked %s %s.\n", id, status)`
Previously: `"Marked %s resolved.\n"`. Fine.

Let's check `cmd/atcr/debt_resolve_test.go`:
`TestDebtResolve_WontfixStatusFoldsItemOutOfOpenList` creates `wontfix := rec`, then modifies fields. `rec` is from `openRec`. `writeDebtStore` appends them. Seems fine.
Wait, `openRec` in test helper probably sets ID. `wontfix` shares the same `ID` as `rec`. That's intentional for testing folding.

`TestDebtResolve_MarkWontfixSetsStatusAndFoldsOut`: checks output contains "wontfix". `assert.Contains(t, strings.ToLower(out), "wontfix")`. Fine.

`TestDebtResolve_InvalidStatusIsUsageError`: passes `"bogus"`. Checks error. Fine.

`TestDebtResolve_StatusOrReasonWithoutResolveIsUsageError`:
Checks `--status without --resolve` and `--reason without --resolve`.
Wait, in `runDebtResolve`, the check is:
`if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {`
What if user passes `--reason ""` explicitly? `mustFlag` returns `""`. `strings.TrimSpace("") != ""` is false. So it falls through to list view. Is that a bug? Probably acceptable since `--reason ""` is usually not used. But `cmd.Flags().Changed("reason")` might be better. However, checking if it's non-empty is standard for optional flags. Not a critical bug.

Let's look closely at `cmd/atcr/debt_resolve.go` around line 155-160:
```go
	id := mustFlag(cmd, "resolve")
	// --status/--reason only mean something for a mark action; without --resolve they
	// would be silently ignored (dropping the user's dismissal intent and skipping
	// --status validation). Reject that combination rather than fall through to list.
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
		return usageError(fmt.Errorf("--status/--reason require --resolve <id>"))
	}
```
Wait, `mustFlag` is a helper that probably calls `cmd.Flags().GetString()` or panics if not found. It's fine.

Let's check `cmd/atcr/debt_resolve.go` line ~215: `func markDebtResolved(cmd *cobra.Command, dir, id, status, reason string) error {`
Inside the loop over `recs`:
```go
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
```
Wait, `recs` is a slice of `localdebt.Record`. `recs[i]` is a copy. Taking address `&r` where `r := recs[i]` is fine because `r` is a local variable on each iteration? Wait, `r := recs[i]` creates a new variable `r` inside the loop body. So `&r` is valid and points to a distinct variable each time `orig == nil`. That's correct Go.

Wait, look at this:
```go
		if orig == nil && recs[i].File != "" {
			r := recs[i]
			orig = &r
		}
```
This is inside the loop. If there are multiple open records with the same ID (which happens in append-only stores), it takes the first one where `File != ""`. That's intended.

But wait! `alreadyClosed` is set to true if *any* record for this ID has a closed status, and then `continue`s. The loop continues checking other records. If it finds an open record later with `File != ""`, it will still assign it to `orig`. Then after the loop:
```go
	if alreadyClosed {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already resolved; nothing to do.\n", id)
		return nil
	}
```
This correctly returns early if `alreadyClosed` was set. So it's fine.

Let's check `cmd/atcr/reconcile_test.go`:
`TestPersistLocalDebt_WontfixSuppressesReappend`
```go
	seed := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion,
		RunID:         "2026-07-13T00:00:00Z-wontfix",
		Timestamp:     "2026-07-13T00:00:00Z",
		Severity:      "HIGH",
		File:          "a.go",
		Line:          1,
		Problem:       "flagged false positive",
		Status:        "wontfix",
		Justification: "accepted pattern",
	}
	seed.StampID()
	require.NoError(t, localdebt.Append(dir, seed))
```
Then it calls `persistLocalDebt("review", res, false, &diag)`.
`res.Findings` has one finding with `File: "a.go", Line: 1, Problem: "flagged false positive"`.
`StampID()` presumably hashes these to generate an ID. So the IDs match. The test verifies suppression. Looks solid.

Wait, look at `cmd/atcr/debt_resolve.go` line ~230:
```go
	rec.RunID = now + "-" + status
```
Is `status` lowercase? Yes, validated via `strings.ToLower`. But what if the user passes `--status WONTFIX`? It gets lowercased. That's fine.
But wait, `RunID` format changes from `...-resolve` to `...-resolved` or `...-wontfix`. This might break existing scripts that parse `RunID`, but it's epic 24.0, so acceptable.

Let's check `cmd/atcr/debt_resolve.go` line ~155 again:
```go
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
		return usageError(fmt.Errorf("--status/--reason require --resolve <id>"))
	}
```
What if `--status` is explicitly passed as empty string? `cmd.Flags().Changed("status")` would be true. But default for status is `"resolved"`. You can't pass `--status ""` easily with cobra unless you define it, but cobra's String flag requires a value. So it's fine.

Wait! Look at `cmd/atcr/debt_resolve.go`:
```go
	cmd.Flags().String("status", "resolved", "terminal status to record for --resolve (resolved|wontfix)")
	cmd.Flags().String("reason", "", "justification recorded on the --resolve record (e.g. why a finding is wontfix)")
```
In `runDebtResolve`:
```go
	id := mustFlag(cmd, "resolve")
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
		return usageError(fmt.Errorf("--status/--reason require --resolve <id>"))
	}
```
If the user passes `--status wontfix` but forgets `--resolve`, it errors. Good.
But what if they pass `--reason "foo"` without `--resolve`? It errors. Good.

Is there any bug in `cmd/atcr/debt_resolve_test.go`?
`TestDebtResolve_InvalidStatusIsUsageError`:
```go
	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "bogus")
	require.Error(t, err, ...)
```
The code checks `if !resolveStatuses[status]`. `resolveStatuses` is a map. Safe.

Let's look at `cmd/atcr/debt_resolve.go` line ~240:
```go
	// A supplied --reason records why the finding was dismissed/resolved; an empty
	// reason preserves any justification the item already carried (e.g. reconcile
	// enrichment), never blanking it.
	if r := strings.TrimSpace(reason); r != "" {
		rec.Justification = r
	}
```
Wait, `rec` is a copy of `*orig`. `orig` was taken from `recs[i]`. So modifying `rec.Justification` modifies the copy. Then `localdebt.Append(dir, rec)` appends it. That's correct.

What about `cmd/atcr/debt_resolve.go` line ~215:
```go
func markDebtResolved(cmd *cobra.Command, dir, id, status, reason string) error {
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
```
If `ReadAll` fails, it returns an error. Fine.

Wait, look at `cmd/atcr/debt_resolve.go` line ~160-170:
```go
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
		return usageError(fmt.Errorf("--status/--reason require --resolve <id>"))
	}
	if id != "" {
		status := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status")))
		if !resolveStatuses[status] {
			return usageError(fmt.Errorf("invalid --status %q: expected resolved|wontfix", mustFlag(cmd, "status")))
		}
		return markDebtResolved(cmd, dir, id, status, mustFlag(cmd, "reason"))
	}
```
What if `id != ""` but `--status` is invalid? It returns an error. Good.
But wait! `mustFlag(cmd, "status")` is called twice: once for validation, once for passing to `markDebtResolved`. That's fine.

Let's check `cmd/atcr/debt_resolve.go` line ~250:
`Marked %s %s.\n` prints status. If status is lowercase `wontfix`, it prints `Marked <id> wontfix.`. That's consistent with the epic goal.

Is there any off-by-one or logic inversion?
In `isClosedStatus`:
```go
func isClosedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "resolved", "deferred", "wontfix":
		return true
	default:
		return false
	}
}
```
Correct.

In `selectOpenDebt` (not shown in diff but referenced): folds by ID. The diff shows `isClosedStatus` is used there presumably. Not in this chunk, but fine.

Wait, look at `cmd/atcr/debt_resolve.go` line ~228:
```go
	rec.RunID = now + "-" + status
```
Previously it was `now + "-resolve"`. Now it's `now + "-" + status`. If `status` is `"resolved"`, `RunID` becomes `...-resolved`. That's fine.

Let's check `cmd/atcr/debt_resolve_test.go` carefully.
`TestDebtResolve_WontfixStatusFoldsItemOutOfOpenList`:
```go
	wontfix := rec
	wontfix.RunID = "2026-07-01T11:00:00Z-a-wontfix"
	wontfix.Timestamp = wontfix.RunID
	wontfix.Status = "wontfix"
	dir := writeDebtStore(t, rec, wontfix, ...)
```
Here `wontfix` is a copy of `rec`. They share the same ID (since `wontfix` was copied from `rec` before any ID change? Wait, `openRec` probably sets ID. Does copying `rec` to `wontfix` copy the ID? Yes, structs are copied by value in Go. So `wontfix.ID == rec.ID`. That's intentional for testing folding.)
But wait! `writeDebtStore` appends them. If they have the same ID, does it matter? The test is about checking that a pre-existing wontfix record folds out an open one. It's fine.

Wait, look at `cmd/atcr/debt_resolve.go` line ~155:
```go
	id := mustFlag(cmd, "resolve")
	// --status/--reason only mean something for a mark action; without --resolve they
	// would be silently ignored (dropping the user's dismissal intent and skipping
	// --status validation). Reject that combination rather than fall through to list.
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
		return usageError(fmt.Errorf("--status/--reason require --resolve <id>"))
	}
```
What if the user passes `--status wontfix` but also passes `--resolve ""`? Cobra's String flag default is `""`. If they explicitly pass `--resolve ""`, `mustFlag` returns `""`. The check `id == ""` catches it. It errors. That's probably fine, or maybe it should allow empty resolve to list? But `--resolve` is meant to be an ID. So it's fine.

Let's look at `cmd/atcr/debt_resolve.go` line ~235:
```go
	rec := *orig
	rec.RunID = now + "-" + status
	rec.Timestamp = now
	rec.Status = status
	rec.ResolvedAt = now
	if r := strings.TrimSpace(reason); r != "" {
		rec.Justification = r
	}
```
`rec` is a copy of `*orig`. `orig` is a pointer to a record in `recs` slice. Modifying `rec` (the value) doesn't affect the slice. That's correct.

Wait, is there a bug in `cmd/atcr/debt_resolve.go` line ~160?
`strings.TrimSpace(mustFlag(cmd, "reason")) != ""`
If the user passes `--reason "   "`, it trims to empty, so condition is false. It falls through to list view. Is that a bug? Probably acceptable, but checking `cmd.Flags().Changed("reason")` would be stricter. However, `mustFlag` might panic if flag not found? No, `--reason` is defined with default `""`. So it's safe.

Let's check `cmd/atcr/reconcile_test.go` line ~520:
```go
	seed.StampID()
	require.NoError(t, localdebt.Append(dir, seed))
```
`StampID()` computes ID based on file/line/problem. Then `res.Findings[0].Finding` has same file/line/problem. So IDs match. The test verifies suppression. Looks correct.

Wait, look at `cmd/atcr/debt_resolve.go` line ~215:
```go
func markDebtResolved(cmd *cobra.Command, dir, id, status, reason string) error {
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
```
If `ReadAll` returns an error, it returns early. Fine.

Is there any issue with `cmd/atcr/debt_resolve.go` line ~165?
```go
	status := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status")))
	if !resolveStatuses[status] {
		return usageError(fmt.Errorf("invalid --status %q: expected resolved|wontfix", mustFlag(cmd, "status")))
	}
```
What if `--status` is not provided? Default is `"resolved"`. `mustFlag` returns `"resolved"`. `strings.ToLower("resolved")` -> `"resolved"`. `resolveStatuses["resolved"]` is true. So it passes validation. Good.

Wait! In `cmd/atcr/debt_resolve.go`, line ~160:
```go
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
		return usageError(fmt.Errorf("--status/--reason require --resolve <id>"))
	}
```
If `--reason` is provided as `""` explicitly? Cobra's `String` flag doesn't allow omitting the value easily unless you use `=`, but `go test -flag ""` might be parsed as empty string. If user passes `--reason=""`, `Changed` is true, but `TrimSpace("") != ""` is false. So it falls through to list view. This is a minor UX issue, not a bug. I'll skip it unless it's critical.

Let's check `cmd/atcr/debt_resolve.go` line ~248:
```go
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Marked %s %s.\n", id, status)
```
If `status` is `"wontfix"`, it prints `Marked <id> wontfix.`. Previously it printed `Marked <id> resolved.`. This matches the epic's goal.

Wait, look at `cmd/atcr/debt_resolve.go` line ~230:
```go
	rec.RunID = now + "-" + status
```
If `status` is `"wontfix"`, `RunID` becomes `...-wontfix`. If it's `"resolved"`, `...-resolved`. This changes the suffix from `-resolve` to the status. That's fine.

Let's look at `cmd/atcr/debt_resolve_test.go` line ~260:
```go
func TestDebtResolve_MarkWontfixSetsStatusAndFoldsOut(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec, ...)
	out, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "wontfix")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "wontfix")
```
`runDebt` probably runs the cobra command. `out` is stdout. `strings.ToLower(out)` contains `"wontfix"`. Since the command prints `Marked <id> wontfix.\n`, `strings.ToLower` will contain it. That's fine.

Wait, look at `cmd/atcr/debt_resolve.go` line ~158:
```go
	id := mustFlag(cmd, "resolve")
	// --status/--reason only mean something for a mark action; without --resolve they
	// would be silently ignored (dropping the user's dismissal intent and skipping
	// --status validation). Reject that combination rather than fall through to list.
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
```
What if `--resolve` is provided but empty string? `id == ""` catches it. It errors. That's probably correct because an empty resolve ID doesn't make sense.

Is there any bug in `cmd/atcr/debt_resolve.go` line ~235?
```go
	rec := *orig
	rec.RunID = now + "-" + status
	rec.Timestamp = now
	rec.Status = status
	rec.ResolvedAt = now
	// A supplied --reason records why the finding was dismissed/resolved; an empty
	// reason preserves any justification the item already carried (e.g. reconcile
	// enrichment), never blanking it.
	if r := strings.TrimSpace(reason); r != "" {
		rec.Justification = r
	}
```
`rec.Justification` is set only if `reason` is non-empty. If empty, it keeps the original justification from `orig`. That's correct per the comment.

Wait! Look at `cmd/atcr/debt_resolve.go` line ~215:
```go
func markDebtResolved(cmd *cobra.Command, dir, id, status, reason string) error {
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
```
Wait! `recs` is a slice of `localdebt.Record`. In Go, iterating over a slice yields copies. `r := recs[i]` creates a copy. `&r` takes address of the local variable `r`. This is safe and correct in Go 1.22+ (and even before, as long as `r` isn't used after loop). But wait! `recs` is from `ReadAll`, which returns a slice. The addresses in `orig` point to variables on the stack of the loop iteration or heap-allocated? Actually, `r` is a local variable inside the loop body. Each iteration creates a new `r`. So `&r` points to a distinct variable each time it's assigned. That's fine.

But wait! `recs[i].File != ""` check: what if an open record has no file? It skips it. That's intentional to avoid nil pointer or empty status later? Actually, `orig` is used later to copy fields. If `recs[i].File == ""`, we skip assigning it. That means if the only open record for this ID has no File, `orig` remains nil, and later it errors "no open technical-debt item with id...". That's fine.

Is there any bug in `cmd/atcr/debt_resolve.go` line ~160?
`strings.TrimSpace(mustFlag(cmd, "reason")) != ""`
If the user passes `--reason ""`, it evaluates to false. The check falls through to list view. This is a minor UX quirk, not a bug. I'll note it as LOW if needed, but probably skip.

Let's look at `cmd/atcr/debt_resolve.go` line ~175:
```go
	status := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status")))
	if !resolveStatuses[status] {
		return usageError(fmt.Errorf("invalid --status %q: expected resolved|wontfix", mustFlag(cmd, "status")))
	}
```
What if `--status` is passed as `"WONTFIX"`? `strings.ToLower` makes it `"wontfix"`. `resolveStatuses["wontfix"]` is true. Validation passes. Then in `markDebtResolved`, `status` is already lowercased. That's fine.

Wait! In `cmd/atcr/debt_resolve.go` line ~230:
```go
	rec.RunID = now + "-" + status
```
If `status` is `"wontfix"`, `RunID` ends with `-wontfix`. Previously it was `-resolve`. This changes the format. It's fine for this epic.

Let's check `cmd/atcr/debt_resolve_test.go` line ~280:
```go
func TestDebtResolve_InvalidStatusIsUsageError(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)
	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "bogus")
	require.Error(t, err, "an unrecognized --status must be a usage error, not a silently non-folding record")
}
```
The code checks `!resolveStatuses[status]`. `resolveStatuses` is a map. Safe.

Is there any issue with `cmd/atcr/debt_resolve.go` line ~155?
```go
	id := mustFlag(cmd, "resolve")
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
		return usageError(fmt.Errorf("--status/--reason require --resolve <id>"))
	}
```
If `--resolve` is not provided, `mustFlag` returns default `""`. `id == ""` is true. If `--status` or `--reason` is changed/non-empty, it errors. Correct.

Wait! Look at `cmd/atcr/debt_resolve.go` line ~160:
`strings.TrimSpace(mustFlag(cmd, "reason")) != ""`
If the user passes `--reason "   "`, it trims to empty, condition false. Falls through to list view. This might be considered a bug because the user explicitly passed `--reason` but it's ignored. However, trimming whitespace is standard. I'll skip unless it's critical.

Let's check `cmd/atcr/debt_resolve.go` line ~245:
```go
	if r := strings.TrimSpace(reason); r != "" {
		rec.Justification = r
	}
```
If `reason` is `"   "`, it trims to empty, condition false. `rec.Justification` keeps original. This matches the comment "never blanking it". Correct.

Wait! In `cmd/atcr/debt_resolve_test.go` line ~310:
```go
func TestDebtResolve_StatusOrReasonWithoutResolveIsUsageError(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	// --status without --resolve must not silently fall through to the list view:
	// it would drop the user's dismissal intent (and skip status validation).
	_, err := runDebt(t, "resolve", "--dir", dir, "--status", "wontfix")
	require.Error(t, err, "--status without --resolve must be a usage error, not a silent list")

	// --reason without --resolve is the same footgun.
	_, err = runDebt(t, "resolve", "--dir", dir, "--reason", "some note")
	require.Error(t, err, "--reason without --resolve must be a usage error")
```
The test passes `--reason "some note"`. `strings.TrimSpace("some note") != ""` is true. So it errors. Correct.

I don't see any critical bugs. Let's look for subtle ones.
In `cmd/atcr/debt_resolve.go` line ~215:
```go
func markDebtResolved(cmd *cobra.Command, dir, id, status, reason string) error {
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
```
If `ReadAll` fails, it returns error. Fine.

Wait! Look at `cmd/atcr/debt_resolve.go` line ~230:
```go
	rec.RunID = now + "-" + status
```
What if `status` contains spaces? It's validated to be `"resolved"` or `"wontfix"`. So safe.

Look at `cmd/atcr/debt_resolve.go` line ~165:
```go
	status := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status")))
	if !resolveStatuses[status] {
		return usageError(fmt.Errorf("invalid --status %q: expected resolved|wontfix", mustFlag(cmd, "status")))
	}
```
`mustFlag` is called twice. It's fine.

Is there any issue with `cmd/atcr/debt_resolve.go` line ~158?
`cmd.Flags().Changed("status")` checks if the flag was explicitly set on the command line. If user sets `--status resolved` (the default), `Changed` is true, but `id == ""` might be false or true depending on `--resolve`. If `--resolve` is missing, it errors. That's correct.

Wait! In `cmd/atcr/debt_resolve.go` line ~158:
```go
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
		return usageError(fmt.Errorf("--status/--reason require --resolve <id>"))
	}
```
What if the user passes `--resolve` but it's empty string? Cobra's String flag default is `""`. If they pass `--resolve ""`, `mustFlag` returns `""`. `id == ""` is true. It errors. That's probably intended because an empty resolve ID doesn't make sense. But maybe it should allow `--resolve ""` to just list? The comment says "without --resolve they would be silently ignored". So it's fine.

Let's check `cmd/atcr/debt_resolve_test.go` line ~250:
```go
func TestDebtResolve_WontfixStatusFoldsItemOutOfOpenList(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	// A terminal wontfix record for the same id must fold the finding out of the
	// open backlog exactly like a resolved record (Epic 24.0 AC #2).
	wontfix := rec
	wontfix.RunID = "2026-07-01T11:00:00Z-a-wontfix"
	wontfix.Timestamp = wontfix.RunID
	wontfix.Status = "wontfix"
	dir := writeDebtStore(t, rec, wontfix, ...)
```
`wontfix := rec` copies the struct. They share `ID`. That's intentional.

Wait! In `cmd/atcr/debt_resolve.go` line ~235:
```go
	rec := *orig
	rec.RunID = now + "-" + status
	rec.Timestamp = now
	rec.Status = status
	rec.ResolvedAt = now
	if r := strings.TrimSpace(reason); r != "" {
		rec.Justification = r
	}
```
`rec` is a value copy of `*orig`. Then `localdebt.Append(dir, rec)` appends it. That's correct.

I will look for off-by-one or string slicing bugs. None apparent.
Null/nil: `orig` could be nil if no open record with File found. Handled later.
Logic inversions: `isClosedStatus` correctly returns true for closed.
Copy-paste: `markDebtResolved` signature changed to accept status/reason. Test updated accordingly.
Shadowing: `r := recs[i]` inside loop is fine. `rec := *orig` is fine.

Wait! Look at `cmd/atcr/debt_resolve.go` line ~160 again:
```go
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace(mustFlag(cmd, "reason")) != "") {
		return usageError(fmt.Errorf("--status/--reason require --resolve <id>"))
	}
```
If `--reason` is passed as `""` explicitly, `mustFlag` returns `""`. `TrimSpace("") != ""` is false. It falls through to list view. This might be a minor issue, but not a bug per se. I'll skip.

Let's check `cmd/atcr/debt_resolve.go` line ~230:
`rec.RunID = now + "-" + status`
If `status` is `"wontfix"`, `RunID` becomes `...-wontfix`. Previously it was `...-resolve`. This changes the format. It's fine.

Wait! In `cmd/atcr/debt_resolve.go` line ~215:
```go
func markDebtResolved(cmd *cobra.Command, dir, id, status, reason string) error {
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: cmd.ErrOrStderr()})
	if err != nil {
		return fmt.Errorf("atcr debt resolve: failed to read local debt store: %w", err)
	}
```
If `ReadAll` fails, it returns error. Fine.

Is there any bug in `cmd/atcr/debt_resolve_test.go` line ~310?
`TestDebtResolve_StatusOrReasonWithoutResolveIsUsageError` tests both `--status` and `--reason` without `--resolve`. It expects errors. The code handles it. Correct.

One thing: `cmd/atcr/debt_resolve.go` line ~165:
```go
	status := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status")))
	if !resolveStatuses[status] {
		return usageError(fmt.Errorf("invalid --status %q: expected resolved|wontfix", mustFlag(cmd, "status")))
	}
```
`mustFlag` is called twice. It's fine.

What about `cmd/atcr/debt_resolve.go` line ~245?
```go
	if r := strings.TrimSpace(reason); r != "" {
		rec.Justification = r
	}
```
If `reason` is `"   "`, it trims to empty, condition false. `rec.Justification` keeps original. Matches comment. Correct.

I'll scan for any subtle issues.
In `cmd/atcr/debt_resolve.go` line ~230: `rec.RunID = now + "-" + status`
If `status` is `"wontfix"`, `RunID` ends with `-wontfix`. This is fine.

Wait! Look at `cmd/atcr/debt_resolve.go` line ~158:
```go
	id := mustFlag(cmd, "resolve")
	if id == "" && (cmd.Flags().Changed("status") || strings.TrimSpace