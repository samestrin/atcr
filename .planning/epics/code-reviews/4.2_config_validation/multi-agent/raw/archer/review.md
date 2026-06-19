Let me analyze this diff carefully against the sprint plan scope.

The sprint plan's clarified scope:
- IN scope: accumulate-all-errors refactor of `validate()` + `ValidateFallbacks()`; multi-error attribution in `attribute()`; tests asserting all-at-once reporting per AC category against the real structs.
- OUT of scope: a new `Config` type; renaming error messages; provider→API-key-env presence check at load; an `atcr doctor` config check.

Files in the diff:
1. `cmd/atcr/review_test.go` - new test for invalid config (in scope - integration test AC9)
2. `docs/registry.md` - doc update (in scope-ish, documents the new behavior)
3. `internal/registry/accumulate_test.go` - new tests (in scope)
4. `internal/registry/attribution.go` - multi-error attribution (in scope)
5. `internal/registry/config.go` - validate() accumulation refactor (in scope)
6. `internal/registry/graph.go` - ValidateFallbacks() accumulation refactor (in scope)

Let me look for issues:

### `internal/registry/config.go`

The `validate()` function now accumulates errors. Let me check for issues:

1. In `validateProvider`, the `else if` for `envVarName.MatchString` - this is correct, only check the regex if APIKeyEnv is non-empty.

2. In `validateAgent`, the unknown-provider check uses `else if` - this is correct, suppresses the unknown-provider error when provider is empty.

3. The `sortedKeys` generic function - looks fine.

4. Looking at `validateAgent`, there's a potential issue: when `a.Provider == ""`, the unknown-provider check is suppressed (good). But what about when `a.Provider` references a provider that itself is invalid? That's probably fine - we just report unknown provider.

Wait, let me look more carefully at the scope validation:
```go
for _, s := range a.Scope {
    if strings.TrimSpace(s) == "" {
        errs = append(errs, agentErrf(name, "agent '%s': scope entries must not be empty", name))
    } else if strings.IndexFunc(s, func(r rune) bool { return r < 32 }) >= 0 {
        errs = append(errs, agentErrf(name, "agent '%s': scope entries must not contain control characters", name))
    }
}
```

This is fine - changed from separate if to if/else if. Actually wait, in the original:
```go
if strings.TrimSpace(s) == "" {
    return agentErrf(name, "agent '%s': scope entries must not be empty", name)
}
if strings.IndexFunc(s, func(r rune) bool { return r < 32 }) >= 0 {
    return agentErrf(name, "agent '%s': scope entries must not contain control characters", name)
}
```

The original had two separate `if` statements (not if/else). The new code uses `else if`. This is actually a behavior change: if a string is empty AND has control characters... well, an empty string can't have control characters, so this is fine. Actually `strings.TrimSpace(s) == ""` means s is empty or whitespace, and a whitespace string wouldn't have control chars < 32 (space is 32). So the else if is fine.

### `internal/registry/graph.go`

Let me look at the `walkFallbacks` changes carefully:

```go
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
    // Unreachable: next is gray, hence already on the current path.
```

Wait, there's a subtle issue here. The comment says "next is always in path" but after the accumulation change, is that still true?

Let me trace through. `walkFallbacks` is called from `ValidateFallbacks`. Let me look at the full context of `ValidateFallbacks`:

```go
func (r *Registry) ValidateFallbacks() error {
    names := sortedKeys(r.Agents)
    var errs []error
    for _, name := range names {
        fb := r.Agents[name].Fallback
        if fb == "" {
            continue
        }
        if _, ok := r.Agents[fb]; !ok {
            errs = append(errs, ...)
        }
    }
    // ... cycle detection
```

Wait, I need to see the cycle detection part. Let me look at the diff again. The diff shows:

```go
for _, name := range names {
    fb := r.Agents[name].Fallback
    if fb == "" {
        continue
    }
    if _, ok := r.Agents[fb]; !ok {
        errs = append(errs, agentSentinelErr(name, ErrDanglingFallback, ...))
    }
}

// cycle detection (not shown in diff but exists)
```

The cycle detection part isn't fully shown. Let me look at what's there:

```go
		} else if color[attributed] == gray {
			// ... cycle found
			errs = append(errs, agentSentinelErr(attributed, ErrFallbackCycle, ...))
		}
	}
	return errors.Join(errs...)
```

Hmm, I don't have the full context of the cycle detection loop. But based on the tests, the behavior seems correct.

Now, the key concern: the `walkFallbacks` function blackens ALL nodes in `path` (including lead-in nodes) when a cycle is detected. The comment explains why: "a later root that edges into a leftover-gray lead-in node would otherwise reach the panic below."

But wait - there's a subtle issue. If a lead-in node is blackened, and that lead-in node itself has a fallback that points to something else (but wait, each agent has at most one outgoing edge, so a lead-in node's only edge is into the cycle). So blackening the lead-in is safe.

Actually, let me think about this more carefully. The comment says "The single-outgoing-edge invariant makes this safe: a lead-in node has one edge (into this cycle) and cannot start another cycle, so marking it fully-explored loses no future detection."

This is correct. Each agent has at most one fallback edge. So if node A's fallback points to B (which is in a cycle), A cannot be part of any other cycle. Blackening A is safe.

But wait - what if A is a lead-in to cycle X, and A is ALSO reachable as a lead-in from another node C? Like:
- C -> A -> B -> C (cycle B<->C, A is lead-in, C is also lead-in to A)

Hmm, but each node has one outgoing edge. So:
- A's fallback is B
- B's fallback is C  
- C's fallback is B (cycle B<->C)

Wait, that doesn't work. If C's fallback is B, and B's fallback is C, that's the cycle. A's fallback is B (lead-in). C's fallback is B... no, C's fallback would need to be something.

Actually, let me think about the test case `{a:b, b:c, c:b, d:a}`:
- a -> b (lead-in)
- b -> c
- c -> b (cycle: b -> c -> b)
- d -> a (lead-in to lead-in)

When we walk from `a`:
- path = [a], color[a] = gray
- next = b, color[b] = white, so path = [a, b], color[b] = gray
- next = c, color[c] = white, so path = [a, b, c], color[c] = gray
- next = b, color[b] = gray -> cycle detected!
- Trim: path[i:] where path[i] == b, so cycle = [b, c, b]
- Blacken all in path: color[a] = black, color[b] = black, color[c] = black
- Return [b, c, b], true

Then when we walk from `d`:
- path = [d], color[d] = gray
- next = a, color[a] = black -> break (not gray, not white... wait)

Hmm, what happens when `color[next]` is black? Let me look at the walkFallbacks code more carefully. The diff doesn't show the full function, but based on the structure:

```go
func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]string, bool) {
    path := []string{start}
    color[start] = gray
    for {
        next := r.Agents[start].Fallback  // or however it gets next
        if next == "" {
            break
        }
        if color[next] == gray {
            // cycle detection
        }
        if color[next] == black {
            break  // already fully explored
        }
        // color[next] == white
        color[next] = gray
        path = append(path, next)
        start = next
    }
    // blacken path
    for _, n := range path {
        color[n] = black
    }
    return nil, false
}
```

Wait, I'm guessing at the structure. Let me look at what the diff actually shows:

```go
func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]string, bool) {
```

The diff shows the gray-node handling but not the full loop structure. But based on the comment "walkFallbacks blackens every node it visited (cycle + lead-in) on detection", and the existing behavior where nodes are blackened after the walk completes, this seems correct.

Actually, I notice the diff only shows part of walkFallbacks. The part shown is:

```go
		if color[next] == gray {
			// ... cycle detection with blackening
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
			// Unreachable: next is gray, hence already on the current path.
```

OK so the cycle detection blackens all nodes in path. This is the key change.

Now, let me think about whether there's a correctness issue with the "Unreachable" comment. The comment says "next is gray, hence already on the current path." But is this still true under accumulation?

Before the change: `ValidateFallbacks` only walks white roots. When a cycle is found, it returns immediately. So gray nodes are only on the current path.

After the change: `ValidateFallbacks` continues scanning after finding a cycle. But the cycle detection now blackens all visited nodes (including lead-ins). So when a later root is walked, any previously-visited node is black (not gray). Therefore, when `walkFallbacks` encounters a gray node, it must be on the current path. The invariant holds.

But wait - is there a case where `walkFallbacks` is called on a root that's already gray? Let me check. The outer loop in `ValidateFallbacks` (the cycle detection part) presumably checks `if color[name] == white` before calling `walkFallbacks`. If a node was left gray (not blackened), and the outer loop encounters it... but the new code blackens everything, so this shouldn't happen.

Actually, I realize I need to see the full cycle detection loop. The diff shows:

```go
		} else if color[attributed] == gray {
```

Wait, that's not right. Let me re-read the diff more carefully.

The diff shows:
```go
		if _, ok := r.Agents[fb]; !ok {
			errs = append(errs, agentSentinelErr(name, ErrDanglingFallback,
				fmt.Sprintf("%s: agent '%s' fallback references unknown agent '%s'", ErrDanglingFallback, name, fb)))
		}
	}

	// cycle detection (implied, not fully shown)
```

Then:
```go
				}
			}
-			return agentSentinelErr(attributed, ErrFallbackCycle,
-				fmt.Sprintf("%s detected: %s", ErrFallbackCycle, strings.Join(path, " -> ")))
+			errs = append(errs, agentSentinelErr(attributed, ErrFallbackCycle,
+				fmt.Sprintf("%s detected: %s", ErrFallbackCycle, strings.Join(path, " -> "))))
+			// walkFallbacks blackens every node it visited (cycle + lead-in) on
+			// detection, so the outer scan never re-walks a node left gray and
+			// never trips the gray-not-on-path invariant. Nothing to do here.
			}
		}
-	return nil
+	return errors.Join(errs...)
```

So the cycle detection loop calls `walkFallbacks` and if it returns a cycle, appends the error. The key question is: does the outer loop check `color[name] == white` before calling `walkFallbacks`?

I believe it does, based on the original code structure. The original code had:
```go
for _, name := range names {
    // ... dangling check
}
// cycle detection
for _, name := range names {
    if color[name] != white {
        continue
    }
    path, isCycle := r.walkFallbacks(name, color)
    if isCycle {
        return ... // now: append to errs
    }
}
```

With the blackening of all visited nodes, this should be correct.

Let me now look for actual issues:

### Issue 1: `docs/registry.md` typo fix - "in a second" vs "in a atcr"

The original had: "surfaces in a second at load time" - wait, let me re-read:

Original: "every validation failure surfaces in a second at load time"
New: "every validation failure surfaces in a second at load time"

Wait, the diff shows:
```
-surfaces in a second at load time — not after a 10-minute timeout.
+surfaces in a second at load time — not after a 10-minute timeout. All faults in a config are reported together...
```

Hmm, actually looking more carefully:
```
-surfaces in a atcr second at load time
+surfaces in a second at load time
```

Wait no, let me re-read the diff:
```
-atcr reads configuration from a user file, optional project files, and embedded defaults, resolved by a strict precedence chain. Every file is strictly parsed: **unknown keys are load errors**, so configs stay typo-safe, and every validation failure surfaces in a second at load time — not after a 10-minute timeout.
+atcr reads configuration from a user file, optional project files, and embedded defaults, resolved by a strict precedence chain. Every file is strictly parsed: **unknown keys are load errors**, so configs stay typo-safe, and every validation failure surfaces in a second at load time — not after a 10-minute timeout. All faults in a config are reported together (each naming the file that defined the offending entry), so you fix them in a single pass rather than one error per run.
```

OK, so the only change is adding the last sentence. The "in a second" was already there. No issue.

### Issue 2: Test in `cmd/atcr/review_test.go`

```go
func TestReviewCmd_InvalidConfigReportsAllErrors(t *testing.T) {
	isolate(t)
	initGitRepoWithChange(t)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(`timeout_secs: -1
payload_mode: bogus
providers:
  testprov:
    api_key_env: ATCR_TEST_REVIEW_KEY
agents:
  bruce:
    provider: testprov
    min_severity: BOGUS
`), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents: [bruce]\n"), 0o644))

	code, out := execCmdCapture(t, "review", "--base", "HEAD^")
	require.Equal(t, 2, code, "AC5: invalid config is a usage error (exit 2)")
	// AC6: all faults reported together, not one-at-a-time.
	assert.Contains(t, out, "timeout_secs", "AC3 fault must surface")
	assert.Contains(t, out, "payload_mode", "AC2 fault must surface")
	assert.Contains(t, out, "required field 'model'", "AC1 fault must surface")
	assert.Contains(t, out, "min_severity", "AC2/AC4 fault must surface")
}
```

Potential issue: The test writes to the real user home directory (`os.UserHomeDir()`). Even though `isolate(t)` is called, if `isolate` doesn't isolate the home directory, this test could modify the user's actual config. This is a potential issue.

Wait, but `isolate(t)` is called at the start. Let me think about what `isolate` does. It likely sets up a temp directory as the working directory and possibly sets HOME to a temp dir. But the test uses `os.UserHomeDir()` which reads from the environment. If `isolate` sets HOME to a temp dir, then `os.UserHomeDir()` would return that temp dir, and the test would be safe.

But if `isolate` does NOT set HOME, then this test writes to the real `~/.config/atcr/registry.yaml`, which is a problem.

Actually, looking at the test more carefully, it calls `os.UserHomeDir()` AFTER `isolate(t)`. If `isolate` sets HOME, then `os.UserHomeDir()` returns the isolated path. This is probably fine - `isolate` likely handles this.

But wait - there's another issue. The test creates a registry file at `~/.config/atcr/registry.yaml` but the test also creates `.atcr/config.yaml` with `agents: [bruce]`. The `.atcr/config.yaml` is a project-level config. But the registry file is at the user level. The test seems to be testing that the user-level registry is loaded and validated.

Actually, I think this is fine. The test is testing the integration path where an invalid registry causes a usage error.

Hmm, but there's a potential issue: the test doesn't clean up the registry file it creates in the home directory. If `isolate` uses a temp HOME, then cleanup happens automatically. If not, the file persists. But this is likely handled by `isolate`.

Let me think about whether there are real issues to report...

### Issue 3: `attribution.go` - type assertion for `interface{ Unwrap() []error }`

```go
if joined, ok := err.(interface{ Unwrap() []error }); ok {
    children := joined.Unwrap()
    out := make([]error, len(children))
    for i, c := range children {
        out[i] = r.attribute(c)
    }
    return errors.Join(out...)
}
```

This uses a type assertion to detect joined errors. `errors.Join` returns a type that implements `Unwrap() []error`. This is correct.

But there's a subtle issue: what if `err` is nil? If `err` is nil, the type assertion would fail (ok = false), and we'd fall through to `errors.As(err, &ee)` which would also fail, and we'd return nil. Actually wait, if `err` is nil, the type assertion on a nil error would... let me think. A nil error has type `error` (interface), and the type assertion `err.(interface{ Unwrap() []error })` on a nil interface would return `false` for `ok`. So it falls through. Then `errors.As(nil, &ee)` returns false. Then we check `if r.SettingsSource.File != ""` etc. This should be fine because `attribute` is only called with a non-nil error.

Actually, looking at the code flow: `attribute` is called with the result of `validate()` or `ValidateFallbacks()`. If validation passes, `errors.Join(nil...)` returns nil, and `attribute` is not called (or if called with nil, it returns nil). So this is fine.

### Issue 4: Potential issue with `validateAgent` - duplicate errors for scope

```go
for _, s := range a.Scope {
    if strings.TrimSpace(s) == "" {
        errs = append(errs, agentErrf(name, "agent '%s': scope entries must not be empty", name))
    } else if strings.IndexFunc(s, func(r rune) bool { return r < 32 }) >= 0 {
        errs = append(errs, agentErrf(name, "agent '%s': scope entries must not contain control characters", name))
    }
}
```

If there are multiple empty scope entries, this would append multiple identical error messages. This is a minor issue - it would report the same error multiple times. But this is probably acceptable since each scope entry is a separate fault.

Actually, wait - the error message doesn't identify WHICH scope entry is problematic. So if there are two empty entries, you'd get two identical messages. This is a maintainability/usability issue but not a correctness issue.

### Issue 5: `sortedKeys` generic function

```go
func sortedKeys[V any](m map[string]V) []string {
    keys := make([]string, 0, len(m))
    for k := range m {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    return keys
}
```

This is fine. It's a clean generic helper.

### Issue 6: Test `TestValidate_DeterministicOrder` 

```go
first := build().validate().Error()
for i := 0; i < 20; i++ {
    require.Equal(t, first, build().validate().Error(),
        "accumulated validation output must be deterministic across runs")
}
```

This tests determinism by running 20 times. This is reasonable but not exhaustive (Go's map iteration randomization could theoretically produce the same order 20 times by chance). But 20 iterations is typically sufficient for CI.

### Issue 7: `walkFallbacks` cycle detection

Let me look at this more carefully. The cycle detection now blackens ALL nodes in path:

```go
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
```

Wait, there's a potential issue here. The `for i, n := range path` loop finds the FIRST occurrence of `next` in path. But what if `next` appears multiple times in path? That shouldn't happen because each node appears at most once in the path (we only add white nodes to the path, and we gray them when adding). So `next` appears exactly once in path. This is fine.

But there's another subtle issue: the `for _, visited := range path` loop blackens ALL nodes in path, including the lead-in nodes. But what about `next` itself? `next` is already in path (that's why we're in this branch), so it gets blackened too. Good.

But what about nodes AFTER `next` in the path? For example, if path = [a, b, c, d] and next = b, then we blacken a, b, c, d. But the cycle is only [b, c, d, b]. Nodes c and d are part of the cycle, so blackening them is correct. Node a is a lead-in, and blackening it is correct (as explained in the comment).

Wait, actually I need to re-examine. If path = [a, b, c, d] and next = b, that means:
- We started at a
- a's fallback is b
- b's fallback is c
- c's fallback is d
- d's fallback is b (cycle: b -> c -> d -> b)

So the cycle is [b, c, d, b], and a is a lead-in. Blackening a, b, c, d is correct.

But what if there's another node e whose fallback is c? After blackening, when we walk from e:
- path = [e], color[e] = gray
- next = c, color[c] = black -> break

So e's walk ends without finding a cycle. This is correct because c is already in a detected cycle, and e's only edge points to c, so e can't be part of a different cycle.

OK, I think the logic is correct.

### Issue 8: Let me look at the `attribute` function more carefully

```go
func (r *Registry) attribute(err error) error {
    if joined, ok := err.(interface{ Unwrap() []error }); ok {
        children := joined.Unwrap()
        out := make([]error, len(children))
        for i, c := range children {
            out[i] = r.attribute(c)
        }
        return errors.Join(out...)
    }
    var ee *entryError
    if !errors.As(err, &ee) {
        // Non-entry failures are top-level settings faults (payload_mode,
        // timeout_secs, etc.) carried only by the user registry.
        if r.SettingsSource.File != "" {
            return fmt.Errorf("%s: %w", r.SettingsSource.File, err)
        }
        return err
    }
    // ... entry-specific attribution
```

Wait, there's a potential issue here. When `attribute` is called on a joined error, it recursively calls `attribute` on each child. But what if a child is ALSO a joined error? The recursion would handle it. But what if a child is an `entryError` that wraps another error? `errors.As` would find the `entryError` in the chain. This seems fine.

But there's a subtle issue: the `errors.As(err, &ee)` call. If `err` is a joined error, the type assertion would catch it first (since `errors.Join` returns a type implementing `Unwrap() []error`). But what if `err` is a single `entryError` wrapped in another error? `errors.As` would traverse the chain and find it. This is fine.

Actually, wait. Let me think about whether `entryError` implements `Unwrap() []error`. If it does, then the type assertion would catch it as a "joined" error, which would be wrong. Let me check... `entryError` likely implements `Unwrap() error` (single unwrap), not `Unwrap() []error`. So the type assertion wouldn't catch it. This is fine.

### Issue 9: Let me look at the test file `accumulate_test.go` more carefully

```go
func TestValidateFallbacks_LeadInLeftGrayThenRevisited(t *testing.T) {
    cases := []map[string]string{
        {"a": "b", "b": "c", "c": "b", "d": "a"},
        {"a": "x", "x": "y", "y": "z", "z": "y", "w": "x"},
    }
    for i, edges := range cases {
        reg := agentsWithFallbacks(edges)
        var err error
        require.NotPanics(t, func() { err = reg.ValidateFallbacks() },
            "case %d: a lead-in node left gray must not panic when revisited", i)
        require.Error(t, err)
        assert.ErrorIs(t, err, ErrFallbackCycle)
        assert.Equal(t, 1, strings.Count(err.Error(), "fallback cycle detected"),
            "case %d: exactly one cycle expected, got: %s", i, err.Error())
    }
}
```

This test uses `agentsWithFallbacks` which is presumably a test helper. I don't see it defined in the diff. It's probably defined elsewhere in the test package. This is fine.

### Issue 10: Let me check if there are any issues with the `validateProvider` function

```go
func validateProvider(name string, p Provider) []error {
    var errs []error
    if strings.TrimSpace(name) == "" {
        errs = append(errs, providerErrf(name, "providers.%s: provider name must not be empty", name))
    }
    if p.APIKeyEnv == "" {
        errs = append(errs, providerErrf(name, "providers.%s: required field 'api_key_env' is missing", name))
    } else if !envVarName.MatchString(p.APIKeyEnv) {
        errs = append(errs, providerErrf(name, "providers.%s: api_key_env %q is not a valid environment variable name", name, p.APIKeyEnv))
    }
    if p.BaseURL != "" {
        u, err := url.Parse(p.BaseURL)
        if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
            errs = append(errs, providerErrf(name, "providers.%s: base_url must be a valid http or https URL", name))
        } else if u.User != nil {
            errs = append(errs, providerErrf(name, "providers.%s: base_url must not embed credentials (userinfo)", name))
        }
    }
    return errs
}
```

Wait, there's a potential issue here. If `url.Parse` returns an error, `u` might be nil. Then `u.Scheme` would panic. Let me check: `url.Parse` returns `(*url.URL, error)`. If there's an error, the URL might still be partially parsed, but it shouldn't be nil. Actually, looking at Go's `url.Parse`:

> Parse returns nil and an error if the URL is invalid.

Wait, actually, let me check. From Go docs: "Parse parses rawurl into a URL structure. ... Parse returns nil, err if the URL is invalid."

Hmm, actually I'm not sure. Let me think... In Go's `net/url` package, `url.Parse` returns a non-nil URL even on some errors. But if the URL is completely invalid, it might return nil.

Actually, looking at the Go source code, `url.Parse` can return a non-nil URL with an error. But the code checks `err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == ""`. If `err != nil`, the short-circuit evaluation means `u.Scheme` and `u.Host` are not evaluated. So if `url.Parse` returns nil for u when there's an error, the `err != nil` check would catch it first, and we wouldn't dereference u. This is fine because of short-circuit evaluation with `||`.

Wait, but what if `url.Parse` returns `(nil, nil)`? That shouldn't happen - if parsing succeeds, u is non-nil. If parsing fails, err is non-nil. So this is fine.

Actually, I just realized: the original code had the same pattern:
```go
u, err := url.Parse(p.BaseURL)
if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
```

So this isn't a new issue introduced by the diff. It's pre-existing. And it's correct due to short-circuit evaluation.

### Issue 11: Let me look at the `validateAgent` function for the unknown-provider check

```go
if a.Provider == "" {
    errs = append(errs, agentErrf(name, "agent '%s': required field 'provider' is missing", name))
} else if _, ok := r.Providers[a.Provider]; !ok {
    errs = append(errs, agentErrf(name, "agent '%s' references unknown provider '%s'", name, a.Provider))
}
```

This is good - it uses `else if` to suppress the unknown-provider error when provider is empty. The comment explains this.

But wait - what about the model check? If provider is empty AND model is empty, both errors are reported:
```go
if a.Model == "" {
    errs = append(errs, agentErrf(name, "agent '%s': required field 'model' is missing", name))
}
```

This is correct - both are independent required-field errors.

### Issue 12: Let me check the test `TestLoadRegistry_ReportsAllErrorsAtOnce`

```go
func TestLoadRegistry_ReportsAllErrorsAtOnce(t *testing.T) {
    _, err := LoadRegistry(writeRegistry(t, `
timeout_secs: -1
payload_mode: bogus
providers:
  openai:
    api_key_env: OPENAI_API_KEY
agents:
  alpha: {provider: openai}
  bravo: {provider: openai, model: m, min_severity: BOGUS}
`))
    require.Error(t, err)
    msg := err.Error()
    assert.Contains(t, msg, "timeout_secs", "AC3: type/range fault must be reported")
    assert.Contains(t, msg, "payload_mode", "AC2: enum fault must be reported")
    assert.Contains(t, msg, "agent 'alpha': required field 'model'", "AC1: required-field fault must be reported")
    assert.Contains(t, msg, "agent 'bravo': min_severity", "AC2/AC4: agent enum fault must be reported")
}
```

This test uses `writeRegistry` which is presumably a test helper that writes the YAML to a temp file and returns the path. This is fine.

### Issue 13: Let me look at the `attribute` function's handling of settings faults

In the original code, `attribute` would handle a single error. Now it handles joined errors by recursing. But there's a question: when `validate()` returns a joined error, the settings-level faults (timeout_secs, payload_mode, etc.) are NOT `entryError`s. They're plain `fmt.Errorf` errors. So when `attribute` recurses on each child, the settings faults would hit the `!errors.As(err, &ee)` branch and be prefixed with `r.SettingsSource.File`.

But wait - in the original code, `attribute` was called on the entire error from `validate()`. If `validate()` returned a single settings fault, `attribute` would prefix it with the settings file. Now, `validate()` returns a joined error, and `attribute` recurses on each child. Each settings fault child would be prefixed with the settings file. This is correct.

But what about entry faults? Each entry fault is an `entryError`, and `attribute` would find it via `errors.As` and prefix it with the entry's source file. This is also correct.

### Issue 14: Let me look at the test `TestAttribute_MultiErrorPerEntryFiles`

```go
func TestAttribute_MultiErrorPerEntryFiles(t *testing.T) {
    reg := &Registry{
        Providers: map[string]Provider{"p": {APIKeyEnv: "KEY"}},
        Agents: map[string]AgentConfig{
            "uagent": {Provider: "p", Model: "m", MinSeverity: "BOGUS"},
            "pagent": {Provider: "p", Model: "m", MinSeverity: "BOGUS"},
        },
    }
    reg.stampSource(SourceUser)
    reg.AgentSource["pagent"] = EntrySource{Tier: SourceProject, File: projectRegistryLabel}

    attributed := reg.attribute(reg.validate())
    require.Error(t, attributed)
    msg := attributed.Error()

    assert.Contains(t, msg, "registry.yaml: agent 'uagent'",
        "user-tier fault must be prefixed with the user registry file")
    assert.Contains(t, msg, ".atcr/registry.yaml: agent 'pagent'",
        "project-tier fault must be prefixed with the project registry file")
}
```

This test verifies that each entry fault is attributed to its own file. This is a good test.

### Issue 15: Let me look at the `TestReviewCmd_InvalidConfigReportsAllErrors` test more carefully

The test writes to `os.UserHomeDir()/.config/atcr/registry.yaml`. If `isolate(t)` doesn't override HOME, this would write to the real home directory. Let me check if there's an issue here.

Actually, looking at the test again:
```go
home, err := os.UserHomeDir()
require.NoError(t, err)
regDir := filepath.Join(home, ".config", "atcr")
```

If `isolate(t)` sets HOME to a temp directory, then `os.UserHomeDir()` returns that temp directory, and the test is safe. If `isolate(t)` doesn't set HOME, this test would modify the real `~/.config/atcr/registry.yaml`.

But looking at the test file, there are other tests that use `isolate(t)` and then create `.atcr` directories in the current working directory. The `isolate` function likely sets up a temp working directory. But does it set HOME?

This is a potential issue, but I can't be sure without seeing the `isolate` function. Let me check if there's evidence in the diff... No, the `isolate` function isn't shown.

Actually, looking at the test more carefully, it uses `os.UserHomeDir()` which on Unix returns `$HOME`. If `isolate` sets HOME, this is safe. If not, it's a problem. But since this is a test file and the test is