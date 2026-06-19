The user wants me to review a diff based on a sprint plan. Let me carefully analyze the diff and the sprint plan constraints.

Sprint Plan Summary:
- Epic 4.2: Configuration Validation
- The real deliverable is AC6: error accumulation. Refactor `validate()` and `ValidateFallbacks()` to accumulate all errors and report them together via `errors.Join`, with deterministic ordering.
- `attribute()` / `validateMerged()` must become multi-error aware.
- IN scope: accumulate-all-errors refactor of `validate()` + `ValidateFallbacks()`; multi-error attribution in `attribute()`; tests asserting all-at-once reporting per AC category against the real structs.
- OUT of scope: a new `Config` type; renaming error messages to fictional schema; provider→API-key-env presence check at load; `atcr doctor` config check.

Files in diff:
1. `cmd/atcr/review_test.go` - Integration test
2. `docs/registry.md` - Documentation update
3. `internal/registry/accumulate_test.go` - New unit tests
4. `internal/registry/attribution.go` - Modified to handle multi-error attribution
5. `internal/registry/config.go` - Modified `validate()`, extracted `validateProvider()`, `validateAgent()`, added `sortedKeys()`
6. `internal/registry/graph.go` - Modified `ValidateFallbacks()`, `walkFallbacks()`

Let me review each file for issues.

`cmd/atcr/review_test.go`:
- New test `TestReviewCmd_InvalidConfigReportsAllErrors`
- It creates a registry.yaml in home/.config/atcr with `timeout_secs: -1`, `payload_mode: bogus`, `providers: testprov` (missing `api_key_env`? No, it has `api_key_env: ATCR_TEST_REVIEW_KEY`), `agents: bruce` with `provider: testprov`, `min_severity: BOGUS`. No model specified for bruce.
- It creates `.atcr/config.yaml` with `agents: [bruce]`
- Runs `review --base HEAD^`, expects exit code 2, and checks output contains various error substrings.
- Wait, the registry.yaml has `api_key_env: ATCR_TEST_REVIEW_KEY` under `providers: testprov`. But earlier in the test it doesn't set that env var? Actually for the config validation test, it shouldn't matter because validation happens before API calls. However, looking at the test, it expects "required field 'model'" which corresponds to AC1. But `agents: bruce` doesn't have a `model` field. That seems correct.
- Is there any issue? The test writes to `~/.config/atcr/registry.yaml`. If this runs in parallel with other tests, it might interfere. But there's `isolate(t)` called. Let me check what `isolate(t)` does - not shown in diff, but presumably it isolates the test environment. However, the test is using the real home directory for the user config file. In the test, `home, err := os.UserHomeDir()` is the real home. Then it writes to `filepath.Join(home, ".config", "atcr")`. This is suspicious - if `isolate(t)` doesn't redirect the home directory or use a temp dir, this test writes to the developer's real home directory! That's a HIGH severity issue. But wait, I don't know what `isolate(t)` does. It might set up a temp home dir. But given it calls `os.UserHomeDir()` after `isolate(t)`, if `isolate(t)` changed HOME, it would return the temp home. Without seeing `isolate(t)`, I should be careful. Actually, looking at other tests in the file, they use `isolate(t)` too. But this specific test explicitly uses `os.UserHomeDir()`. If `isolate(t)` doesn't override HOME, this writes to the real filesystem. This is a test hygiene issue. However, is it in scope? The sprint plan mentions integration test in `cmd/atcr/review_test.go` with invalid config. So yes, test changes are in scope.
- Actually, looking more carefully, the test might be using the real home dir. But I need to check if `isolate(t)` manipulates environment. Since I can't see `isolate(t)`, I should look for other clues. The comment doesn't suggest it's a temp home. But many Go test helpers do set HOME. However, writing to the real home directory in a test is risky. But without certainty, maybe I shouldn't flag it if `isolate(t)` likely handles it.
- Let me look for other issues.

`internal/registry/accumulate_test.go`:
- `TestValidate_AccumulatesAllErrors`: creates a registry with multiple faults, checks all appear. Uses `intPtr(-1)`. This seems fine.
- `TestValidate_DeterministicOrder`: builds registry 20 times, checks output is identical. Checks sorted order. Seems fine.
- `TestValidate_ValidRegistryReturnsNil`: happy path. Fine.
- `TestValidateFallbacks_AccumulatesMultipleDangling`: uses `agentsWithFallbacks`. Fine.
- `TestValidateFallbacks_AccumulatesDanglingAndCycle`: fine.
- `TestValidateFallbacks_LeadInIntoReportedCycleNoPanic`: checks panic. Fine.
- `TestValidateFallbacks_LeadInLeftGrayThenRevisited`: tests specific graph cases. Fine.
- `TestValidateFallbacks_TwoIndependentCycles`: fine.
- `TestValidateFallbacks_ValidReturnsNil`: fine.
- `TestAttribute_MultiErrorPerEntryFiles`: creates registry, stamps source, checks attribution. `reg.stampSource(SourceUser)` - stamps ALL entries as user? Then overrides `AgentSource["pagent"]`. Wait, `stampSource` might set a default source, but does it set `AgentSource`? The test assumes `reg.AgentSource` exists. It might be initialized by `stampSource`. Fine.
- `TestAttribute_SingleEntryStillAttributed`: fine.
- `TestLoadRegistry_ReportsAllErrorsAtOnce`: uses `writeRegistry(t, ...)` which presumably writes a temp file and loads it. Fine.
- `TestAttribute_SettingsFaultGetsUserLabel`: fine.

Wait, in `TestLoadRegistry_ReportsAllErrorsAtOnce`, the YAML has:
```
agents:
  alpha: {provider: openai}
  bravo: {provider: openai, model: m, min_severity: BOGUS}
```
Alpha is missing model. The expected message says "agent 'alpha': required field 'model'". But in `config.go`, the `validateAgent` function checks:
```go
if a.Model == "" {
    errs = append(errs, agentErrf(name, "agent '%s': required field 'model' is missing", name))
}
```
So the message should be "required field 'model' is missing". But the test asserts:
```go
assert.Contains(t, msg, "agent 'alpha': required field 'model'", "AC1: required-field fault must be reported")
```
That matches.

`internal/registry/attribution.go`:
- `attribute` now handles `interface{ Unwrap() []error }` which is the `errors.Join` type.
- It recursively attributes each child.
- Potential issue: `errors.Join` returns an error that implements `Unwrap() []error`. But what if the joined error itself is also an `entryError`? The `errors.As` check for `*entryError` would have caught it before. But now it checks for joined errors first. That's correct.
- One subtle issue: if `err` is nil, `attribute(nil)` should return nil. The new code: if `err` is nil, the interface assertion `err.(interface{ Unwrap() []error })` on a nil error: in Go, a nil interface value won't satisfy the type assertion. But if `err` is a typed nil (`errors.Join(nil...)` returns nil, so not an issue). If someone passes a non-nil error that wraps nils? `errors.Join` with all nils returns nil. So `attribute` is likely called with non-nil errors. But the original code had:
```go
var ee *entryError
if !errors.As(err, &ee) {
    return err
}
```
If `err` is nil, `errors.As(nil, &ee)` returns false, so returns nil. Good.
In the new code, if `err` is nil: `joined, ok := err.(interface{ Unwrap() []error })` -> `ok` is false because nil interface has no dynamic type. Then falls through to `errors.As(nil, &ee)` -> false -> return nil. Good.
- Is there an issue with infinite recursion? If `r.attribute(c)` where `c` is itself a joined error? That could happen if there are nested `errors.Join`. The code handles it by recursing. But nested joins are possible. The code would handle them fine because it checks the interface first.
- One potential issue: the `interface{ Unwrap() []error }` check might match other custom error types, not just `errors.Join`. But that's fine, as long as they return errors to unwrap. The standard library uses this interface for joined errors.
- Actually, there is a subtle bug: `errors.Join` in Go 1.20+ returns an error implementing `Unwrap() []error`. But the code does:
```go
children := joined.Unwrap()
out := make([]error, len(children))
for i, c := range children {
    out[i] = r.attribute(c)
}
return errors.Join(out...)
```
What if one of the children is nil? `errors.Join(nil)` returns nil? Actually `errors.Join` with a nil in the slice handles it. But `out[i] = r.attribute(c)` could return nil if `c` is nil and passes through. Then `errors.Join` with nils might filter them? Actually in Go 1.20+, `errors.Join` returns nil if all errors are nil. If some are nil and some are not, it returns a non-nil error that unwraps to the non-nil ones. So that's fine.

`internal/registry/config.go`:
- `validate()` now accumulates errors.
- Uses `sortedKeys` for deterministic iteration. Good.
- Extracted `validateProvider` and `validateAgent`.
- In `validateProvider`:
  - If name is empty string: `providerErrf(name, ...)`. But `name` is the map key. If the key is empty string (which is possible in YAML?), this would pass empty string. The format string uses `%s` for name. Fine.
  - If `p.APIKeyEnv == ""`, it reports missing. Then else if `!envVarName.MatchString(p.APIKeyEnv)`, it reports invalid. Good, avoids two errors for empty string.
  - `url.Parse` error handling: if `BaseURL` is invalid, reports error. Else if `u.User != nil`, reports embedded credentials. Good.
- In `validateAgent`:
  - If `a.Provider == ""`, reports missing. Else if unknown provider, reports unknown. Good.
  - Checks for model empty.
  - Wait, there is a logic change from original. Original code:
```go
if a.Provider == "" {
    return agentErrf(name, "agent '%s': required field 'provider' is missing", name)
}
if a.Model == "" {
    return agentErrf(name, "agent '%s': required field 'model' is missing", name)
}
if _, ok := r.Providers[a.Provider]; !ok {
    return agentErrf(name, "agent '%s' references unknown provider '%s'", name, a.Provider)
}
```
New code:
```go
if a.Provider == "" {
    errs = append(errs, agentErrf(name, "agent '%s': required field 'provider' is missing", name))
} else if _, ok := r.Providers[a.Provider]; !ok {
    errs = append(errs, agentErrf(name, "agent '%s' references unknown provider '%s'", name, a.Provider))
}
if a.Model == "" {
    errs = append(errs, agentErrf(name, "agent '%s': required field 'model' is missing", name))
}
```
This is a good change: if provider is empty, it won't also say "references unknown provider ''". And it accumulates both missing provider and missing model.
- But wait, there is a potential issue: `if a.Provider == ""` reports missing provider. Then `else if _, ok := r.Providers[a.Provider]; !ok` checks unknown provider. Good.
- However, if `a.Provider` is empty, the original code would return immediately after missing provider. The new code continues to check other fields. This is intended (accumulation).
- But what about the `model` check? It continues regardless. Good.
- Is there any issue with `Scope` validation? Original code:
```go
for _, s := range a.Scope {
    if strings.TrimSpace(s) == "" {
        return agentErrf(...)
    }
    if strings.IndexFunc(s, func(r rune) bool { return r < 32 }) >= 0 {
        return agentErrf(...)
    }
}
```
New code:
```go
for _, s := range a.Scope {
    if strings.TrimSpace(s) == "" {
        errs = append(errs, agentErrf(name, "agent '%s': scope entries must not be empty", name))
    } else if strings.IndexFunc(s, func(r rune) bool { return r < 32 }) >= 0 {
        errs = append(errs, agentErrf(name, "agent '%s': scope entries must not contain control characters", name))
    }
}
```
Wait, there's a logic change! Original code: if scope entry is empty, it returns immediately. If it has control chars, it returns immediately.
New code: if scope entry is empty, it appends error and continues to next iteration (because of `else if`). But if a scope entry is empty, does it make sense to also check for control characters? An empty string after TrimSpace is empty, so the `else if` prevents checking control chars on empty strings. That's fine.
But what about non-empty strings with control characters? It will catch them. Good.
However, there is a subtle difference: original code would stop at first bad scope entry. New code continues checking all scope entries and reports all bad ones. That's the intent of AC6.
But wait, the `else if` means if a string is empty, it won't check control characters. But an empty string doesn't have control characters anyway, so the behavior is equivalent for that entry. However, what if `strings.TrimSpace(s) == ""` but `s` contains only whitespace? Then the error is "scope entries must not be empty". In the original code, it would also return that error. In the new code, it also returns that error. The original code didn't check control characters on whitespace-only strings because it returned first. That's fine.

Wait, there is another issue in `validateAgent`: `Scope` is a slice of strings. The original code returned on the first bad scope entry. The new code accumulates. But the `else if` means for a given entry, if it's empty/whitespace, it doesn't check control chars. That's fine.

But looking more carefully at `validateAgent`, is there an issue with `a.Scope` loop not using index? No, it just iterates values.

Wait, I see something in `config.go`:
```go
func validateProvider(name string, p Provider) []error {
	var errs []error
	if strings.TrimSpace(name) == "" {
		errs = append(errs, providerErrf(name, "providers.%s: provider name must not be empty", name))
	}
```
If `name` is empty, `providerErrf(name, ...)` uses `name` as the provider name key for `entryError`. Is that okay? The `entryError` likely stores the name. If name is empty, it might be okay. Not a real issue.

Let me check `graph.go`:
- `ValidateFallbacks` now uses `sortedKeys(r.Agents)` instead of inline sort. Good.
- It accumulates dangling refs in first loop, then cycles in second loop.
- In the first loop, it checks if fallback exists. If not, appends error.
- In the second loop, it walks white nodes.
- `walkFallbacks` changes:
  - When cycle detected, it creates a new slice `cycle := make([]string, 0, len(path)-i+1)`.
  - Then `cycle = append(cycle, path[i:]...)`.
  - Then `cycle = append(cycle, next)`.
  - Then it blackens EVERY visited node in `path`: `for _, visited := range path { color[visited] = black }`.
  - Then returns `cycle, true`.
- Before, `walkFallbacks` did: `return append(path[i:], next), true`. This mutated `path` by appending `next` to `path[i:]`? Wait, `path[i:]` is a slice of the original `path`. `append(path[i:], next)` might mutate the backing array of `path` if there's capacity. That could be a bug in the original code! But the new code explicitly creates a new slice.
- The new code blackens all visited nodes. The comment explains why: to prevent panic when a lead-in node is left gray and later revisited.
- Is there any issue with the new `walkFallbacks` logic?
  - The `path` parameter grows as it walks. When a cycle is detected, `path` contains all nodes from start to the node before `next`. `next` is the gray node that closes the cycle.
  - The nodes in `path` are blackened. But what about `next` itself? It was already gray before entering `walkFallbacks` for this iteration. Actually, `next` is the target of the edge from the last node in `path`. `color[next]` is gray. But `next` is not in `path` (except potentially if it was added earlier). Wait, in the loop:
```go
for _, n := range path {
    if n == next {
        // found
    }
}
```
So `next` is not in `path` as the last element. `path` contains nodes visited before `next`. The cycle is `path[i:]...next`. The new code blackens all nodes in `path`. But does it blacken `next`? No. `next` is gray. After returning, `ValidateFallbacks` outer loop continues. `next` is part of the cycle. The outer loop iterates over `names`. If `next` is one of the names, its color is still gray. But the outer loop only walks white roots: `if color[name] == white`. So it won't walk `next` again. However, another node could point to `next`. If that node is a root and is walked later, it will reach `next` which is gray. But `next` is not on the current path (because `color[next]` was set gray by a previous walk, not the current one). Wait, in the new code, after detecting a cycle, it blackens all nodes in `path` but NOT `next`. So `next` remains gray. Then if another root's walk reaches `next`, it will see `color[next] == gray` and panic (because the panic path says "gray node not on current path").
  - Actually, wait. Let's trace carefully.
  - In `walkFallbacks`, the `color` map is passed by reference (it's a map).
  - Before calling `walkFallbacks`, `color[start]` is set to gray.
  - Inside `walkFallbacks`, for `cur := start; ; { next := r.Agents[cur].Fallback; ...; if color[next] == gray { ... } ; path = append(path, cur); color[cur] = gray; cur = next }`
  - Wait, the loop structure:
```go
cur := start
for {
    next := r.Agents[cur].Fallback
    if next == "" { break }
    if color[next] == gray {
        // cycle found
        path = append(path, cur) // is cur already in path?
        // wait, looking at the actual code...
    }
}
```
Actually, let me look at the actual code in graph.go:
```go
func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]string, bool) {
	path := []string{start}
	cur := start
	for {
		next := r.Agents[cur].Fallback
		if next == "" {
			break
		}
		if color[next] == gray {
			// ...
			path = append(path, cur) // Wait, is this in the diff?
```
Let me re-read the diff for graph.go:
```go
@@ -82,13 +84,27 @@ func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]st
 			break
 		}
 		if color[next] == gray {
-			// Close the loop for the error message: trim the lead-in so the
-			// path starts at the repeated node. Because ValidateFallbacks only
-			// walks white roots and colors nodes gray on the current path, next
-			// is always in path — the loop below cannot complete without matching.
+			// Trim the lead-in so the reported path starts at the repeated node.
+			// Build the cycle into a fresh slice BEFORE blackening so it never
+			// aliases path's backing array, then blacken EVERY node this walk
+			// visited — lead-in nodes included, not just the trimmed cycle. Under
+			// accumulation ValidateFallbacks keeps scanning, and a later root that
+			// edges into a leftover-gray lead-in node would otherwise reach the
+			// panic below. The single-outgoing-edge invariant makes this safe: a
+			// lead-in node has one edge (into this cycle) and cannot start another
+			// cycle, so marking it fully-explored loses no future detection.
+			// Because ValidateFallbacks only walks white roots and colors nodes
+			// gray on the current path, next is always in path — the loop cannot
+			// complete without matching.
 			for i, n := range path {
 				if n == next {
-					return append(path[i:], next), true
+					cycle := make([]string, 0, len(path)-i+1)
+					cycle = append(cycle, path[i:]...)
+					cycle = append(cycle, next)
+					for _, visited := range path {
+						color[visited] = black
+					}
+					return cycle, true
 				}
 			}
 			// Unreachable: next is gray, hence already on the current path.
@@ -103,4 +117,5 @@ func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]st
 		path = append(path, cur)
 		color[cur] = gray
 		cur = next
+	}
 }
```
Wait, the diff shows the end of the function but I need to see the beginning. Let me reconstruct.

Original `walkFallbacks`:
```go
func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]string, bool) {
	path := []string{start}
	cur := start
	for {
		next := r.Agents[cur].Fallback
		if next == "" {
			break
		}
		if color[next] == gray {
			// Close the loop...
			for i, n := range path {
				if n == next {
					return append(path[i:], next), true
				}
			}
			panic("unreachable")
		}
		if color[next] == black {
			break
		}
		path = append(path, cur)
		color[cur] = gray
		cur = next
	}
	// Blacken path on exit...
	for _, n := range path {
		color[n] = black
	}
	return nil, false
}
```
Wait, I need to look more carefully. The diff hunk shows:
```go
@@ -82,13 +84,27 @@ func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]st
 			break
 		}
 		if color[next] == gray {
```
So the original function had the `break` and `gray` check. The path building is before the diff hunk. The diff shows the inside of the gray check and then at the end:
```go
@@ -103,4 +117,5 @@ func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]st
 		path = append(path, cur)
 		color[cur] = gray
 		cur = next
+	}
 }
```
Wait, that's weird. It seems the original code had `path = append(path, cur)` etc., and the diff just adds a closing brace? No, the `+}` at the end suggests something else. Let me re-read.

Actually the diff hunk header is:
```
@@ -82,13 +84,27 @@ func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]st
```
This means starting at line 82, 13 lines are removed and 27 lines added (context + changes). The removed lines are the original gray block and the lines after. The added lines include the new gray block and the path appending.

Wait, the hunk continues:
```diff
@@ -103,4 +117,5 @@ func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]st
 		path = append(path, cur)
 		color[cur] = gray
 		cur = next
+	}
 }
```
This is a separate hunk? No, it's the continuation. Actually line 103 in the old file corresponds to line 117 in the new file. The old file had:
```go
		path = append(path, cur)
		color[cur] = gray
		cur = next
}
```
And the new file has:
```go
		path = append(path, cur)
		color[cur] = gray
		cur = next
	}
}
```
Wait, that doesn't make sense. Why add a closing brace? Maybe the original file was missing one? Or the function signature changed?

Actually, looking at the diff carefully:
```
@@ -103,4 +117,5 @@ func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]st
 		path = append(path, cur)
 		color[cur] = gray
 		cur = next
+	}
 }
```
Ah, I see! The `for {` loop likely had its closing brace added because the original `for` didn't have braces around the body? No, that can't be. Let me think. Actually, the line numbers might be offset. The `+}` could be adding a closing brace for the `if color[next] == gray {` block? No, the new `if` block has its own braces.

Wait, I think I see: the original `walkFallbacks` might have been structured differently. But regardless, the new code seems syntactically correct because the test `TestValidateFallbacks_LeadInLeftGrayThenRevisited` passes? Actually I don't know if tests pass, I just see the diff.

Let me look for real issues.

Potential issue in `graph.go`: When a cycle is detected, the code blackens all nodes in `path`:
```go
for _, visited := range path {
    color[visited] = black
}
```
But does it blacken `next` (the node that is gray and closes the cycle)? No. `next` is not in `path`. `path` contains the nodes visited from `start` up to `cur` (the node whose fallback is `next`). After detecting the cycle, `next` remains gray. Then the outer loop continues. The outer loop only starts on white nodes. But `next` might not be a start node. However, another node `start2` might have a fallback chain leading to `next`. When `walkFallbacks(start2, ...)` runs, it will eventually reach `next`. If `color[next] == gray`, it will panic because `next` is not on the current path of `start2` (unless `start2` is in the same lead-in chain, but if `start2` is a root that points to `next` directly, then `next` is gray but not in `path2`). Wait, but `next` is gray from a previous walk. If `start2` is a new root and its fallback chain leads to `next`, then when `walkFallbacks` checks `color[next] == gray`, it will hit the panic path because `next` is not in `path2`.

But the comment says: "Because ValidateFallbacks only walks white roots and colors nodes gray on the current path, next is always in path — the loop cannot complete without matching." This comment is now slightly inaccurate because after blackening `path`, the gray nodes that remain are the cycle nodes (including `next`). If another root points to a cycle node, that cycle node is gray but not on the new path.

However, the test `TestValidateFallbacks_LeadInIntoReportedCycleNoPanic` explicitly tests `{a:b, b:a, c:a}` and expects no panic. Let's trace:
- `names` sorted: `a`, `b`, `c`.
- First iteration `name="a"`: white. `walkFallbacks("a")`.
  - `path=["a"]`, `cur="a"`, `next="b"`. `color["b"]` is white (default zero value). Continue.
  - `path=["a","a"]`? Wait, `path = append(path, cur)`. `cur` is "a". So `path` becomes `["a","a"]`? That seems wrong. Let me re-read the original logic.

Actually, I need to reconstruct `walkFallbacks` more carefully. I think the original code was:
```go
func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]string, bool) {
	path := []string{start}
	cur := start
	for {
		next := r.Agents[cur].Fallback
		if next == "" {
			break
		}
		if color[next] == gray {
			// ...
		}
		if color[next] == black {
			break
		}
		path = append(path, cur)
		color[cur] = gray
		cur = next
	}
	for _, n := range path {
		color[n] = black
	}
	return nil, false
}
```
Wait, `path` starts with `start`. Then `cur` starts with `start`. Then in the loop, `next` is fallback of `cur`. If `next` is not empty, not gray, not black, then `path = append(path, cur)`. But `cur` is already in `path` as the first element (and possibly appended in previous iterations). So `path` would have duplicates? Let's trace:
- Start: `path=["a"]`, `cur="a"`.
- Loop: `next="b"`. Not empty, not gray, not black.
- `path = append(path, "a")` -> `["a","a"]`. `color["a"]=gray`. `cur="b"`.
- Loop: `next="a"`. `color["a"]` is gray.
- `for i, n := range path { if n=="a" { ... } }`. `path` is `["a","a"]`. First match is index 0.
- `append(path[0:], next)` -> `append(["a","a"], "a")` -> `["a","a","a"]`. Returns that as cycle. That seems wrong because `path` has duplicates.

But the original code must have been different. Maybe `path` was initially empty:
```go
path := []string{}
cur := start
for {
    next := ...
    if next == "" { break }
    if color[next] == gray { ... }
    if color[next] == black { break }
    path = append(path, cur)
    color[cur] = gray
    cur = next
}
```
If `path` starts empty:
- Start: `path=[]`, `cur="a"`.
- `next="b"`. Not empty/gray/black. `path=["a"]`. `color["a"]=gray`. `cur="b"`.
- `next="a"`. `color["a"]` is gray.
- `for i, n := range path { if n=="a" { ... } }`. `path` is `["a"]`. Match index 0.
- `append(path[0:], next)` -> `append(["a"], "a")` -> `["a","a"]`. Cycle is `a -> a`? But it's actually `a -> b -> a`. The cycle should be `a -> b -> a`. But `path` only contains `["a"]` at this point, not `["a","b"]`? Wait, `path` was `["a"]` before the second iteration. In the second iteration, `cur` is "b". When it detects gray, `path` is still `["a"]`. So `append(path[0:], "a")` gives `["a","a"]`. That incorrectly reports `a -> a` instead of `a -> b -> a`.

This suggests the original code must have appended `cur` BEFORE checking `next`? Or the structure was:
```go
path := []string{}
cur := start
for {
    if color[cur] == gray {
        // cycle
    }
    if color[cur] == black {
        break
    }
    color[cur] = gray
    path = append(path, cur)
    next := r.Agents[cur].Fallback
    if next == "" {
        break
    }
    cur = next
}
```
But I don't have the original file. I have to infer from the diff.

Wait, the diff for `graph.go` shows:
```go
@@ -61,11 +60,14 @@ func (r *Registry) ValidateFallbacks() error {
 					break
 				}
 			}
-			return agentSentinelErr(attributed, ErrFallbackCycle,
-				fmt.Sprintf("%s detected: %s", ErrFallbackCycle, strings.Join(path, " -> "))))
+			errs = append(errs, agentSentinelErr(attributed, ErrFallbackCycle,
+				fmt.Sprintf("%s detected: %s", ErrFallbackCycle, strings.Join(path, " -> "))))
+			// walkFallbacks blackens every node it visited (cycle + lead-in) on
+			// detection, so the outer scan never re-walks a node left gray and
+			// never trips the gray-not-on-path invariant. Nothing to do here.
 		}
 	}
-	return nil
+	return errors.Join(errs...)
 }
```
This suggests `ValidateFallbacks` was returning on the first cycle. Now it appends and continues. And `walkFallbacks` returns `path` and `bool`. The `path` is used in the error message.

In `walkFallbacks`, the new code blackens `path` nodes but not `next`. But `next` is the node that is gray. In the original code, after returning from `walkFallbacks`, `ValidateFallbacks` would return immediately, so `next` remaining gray didn't matter. Now, because `ValidateFallbacks` continues scanning, `next` remains gray. If another root later walks into `next`, it will panic.

BUT the test `TestValidateFallbacks_LeadInIntoReportedCycleNoPanic` explicitly tests `{a:b, b:a, c:a}` and expects no panic. Let's see if the code would panic.

Trace `{a:b, b:a, c:a}`:
- `names` sorted: `a`, `b`, `c`.
- `name="a"`: white. `walkFallbacks("a")`.
  - I need to reconstruct `walkFallbacks` properly. Let me look at the actual code after the diff.

From the diff, the new `walkFallbacks` seems to be:
```go
func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]string, bool) {
	path := []string{start}
	cur := start
	for {
		next := r.Agents[cur].Fallback
		if next == "" {
			break
		}
		if color[next] == gray {
			for i, n := range path {
				if n == next {
					cycle := make([]string, 0, len(path)-i+1)
					cycle = append(cycle, path[i:]...)
					cycle = append(cycle, next)
					for _, visited := range path {
						color[visited] = black
					}
					return cycle, true
				}
			}
			panic("unreachable")
		}
		if color[next] == black {
			break
		}
		path = append(path, cur)
		color[cur] = gray
		cur = next
	}
	for _, n := range path {
		color[n] = black
	}
	return nil, false
}
```
Wait, if `path` starts as `[start]`, then `cur=start`. `next` is fallback of `cur`.
If `next` is not empty/gray/black: `path = append(path, cur)`. So `path` becomes `[start, start]`. Then `color[cur]=gray`, `cur=next`.
In next iteration: `next` is fallback of current `cur`.
If `color[next] == gray`: iterate over `path` which contains `[start, start, ...]`.
This seems like it would have duplicate entries. But maybe the original code was exactly like this, and the cycle detection just finds the first occurrence of `next` in `path`.

For `{a:b, b:a}`:
- `walkFallbacks("a")`: `path=["a"]`, `cur="a"`, `next="b"`.
  - `color["b"]` is white (zero value). Not gray, not black.
  - `path = append(path, "a")` -> `["a","a"]`. `color["a"]=gray`. `cur="