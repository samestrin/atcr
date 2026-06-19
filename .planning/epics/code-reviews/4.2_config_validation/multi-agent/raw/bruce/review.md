<think>
Let me analyze this diff carefully. The diff is for Epic 4.2: Configuration Validation, specifically focused on AC6 (error accumulation). The scope is to refactor `validate()` and `ValidateFallbacks()` to accumulate all errors and report them together via `errors.Join`, with deterministic ordering.

Let me go through the changes:

1. **`internal/registry/config.go`** - Refactored `validate()` to accumulate errors using `errors.Join` instead of returning on the first error. Split into helper functions `validateProvider()` and `validateAgent()`. Added `sortedKeys` helper for deterministic ordering.

2. **`internal/registry/graph.go`** - Refactored `ValidateFallbacks()` to accumulate errors. Modified `walkFallbacks()` to blacken all visited nodes (including lead-in nodes) when a cycle is detected.

3. **`internal/registry/attribution.go`** - Updated `attribute()` to handle joined errors by attributing each child error individually.

4. **`internal/registry/accumulate_test.go`** - New test file with comprehensive tests.

5. **`cmd/atcr/review_test.go`** - New integration test.

6. **`docs/registry.md`** - Doc update.

Let me look for issues:

### Issue 1: `validateProvider` - variable name conflict
In `validateProvider`, the function takes a `Provider` parameter named `p`, but the outer loop in `validate()` has `name` as the key. Let me check if there's a shadowing issue. Looking at the code:

```go
for _, name := range sortedKeys(r.Providers) {
    errs = append(errs, validateProvider(name, r.Providers[name])...)
}
```

That's fine, no shadowing.

### Issue 2: `walkFallbacks` - cycle detection with lead-in blackening

The comment says "a lead-in node has one edge (into this cycle) and cannot start another cycle, so marking it fully-explored loses no future detection." This is only true if a node has at most one outgoing edge. Let me check: "each agent has at most one outgoing edge" - yes, that's mentioned in the comment. So this is safe.

But wait - in the `TestValidateFallbacks_LeadInLeftGrayThenRevisited` test, case 1 is `{a:b, b:c, c:b, d:a}`. The walk from `a` would be: a -> b -> c -> b (cycle detected at b). The cycle path would be [b, c, b]. The full path visited is [a, b, c]. So nodes a, b, c are all blackened. Then `d` is walked: d -> a (but a is black now, so the walk just stops at d). No cycle detected from d.

Wait, looking at the walkFallbacks code more carefully:

```go
func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]string, bool) {
    path := []string{start}
    color[start] = gray
    cur := start
    for {
        next := r.Agents[cur].Fallback
        if next == "" {
            color[cur] = black
            break
        }
        if color[next] == gray {
            // cycle detection
            ...
        }
        if color[next] == black {
            color[cur] = black
            break
        }
        // color[next] must be white
        path = append(path, next)
        color[next] = gray
        cur = next
    }
    return nil, false
}
```

Hmm wait, the walk doesn't blacken all path nodes when it reaches a black node. It only blackens the current node. So if we have a chain that reaches a black node, only the last node is blackened, not the full chain.

Let me trace case 1: `{a:b, b:c, c:b, d:a}`

- d is white, walk from d: path=[d], cur=d, next=a, color[a]=gray, path=[d,a], cur=a
- next=b, color[b]=gray, path=[d,a,b], cur=b
- next=c, color[c]=gray, path=[d,a,b,c], cur=c
- next=b, color[b]=gray! Cycle detected. next=b is at index 1 in path. cycle = path[1:] + b = [a,b,c,b]? 

Wait, let me re-read. `path` at the time of cycle detection is [d, a, b, c]. `next` is `b`. The index of `b` in path is 2. So `cycle = path[2:] + b = [b, c, b]`.

Then it blackens all nodes in path: d, a, b, c all become black.

Now the outer loop continues. But wait - in `TestValidateFallbacks_LeadInLeftGrayThenRevisited` case 1 is `{a:b, b:c, c:b, d:a}`. With deterministic ordering, the first name scanned is `a`.

- a is white, walk from a: path=[a], cur=a, next=b, color[b]=gray, path=[a,b], cur=b
- next=c, color[c]=gray, path=[a,b,c], cur=c
- next=b, color[b]=gray! Cycle detected. next=b is at index 1 in path. cycle = path[1:] + b = [b, c, b].
- Blacken all path nodes: a, b, c all become black.

Then scan `b`: b is black, skip.
Then scan `c`: c is black, skip.
Then scan `d`: d is white, walk from d: path=[d], cur=d, next=a, color[a]=black! So cur becomes black, break. Returns nil, false.

So one cycle reported. 

Now case 2: `{a:x, x:y, y:z, z:y, w:x}`

Scan `a` (or `w` - depends on alphabetical order). Sorted: a, w, x, y, z.

- a is white, walk from a: path=[a], cur=a, next=x, color[x]=gray, path=[a,x], cur=x
- next=y, color[y]=gray, path=[a,x,y], cur=y
- next=z, color[z]=gray, path=[a,x,y,z], cur=z
- next=y, color[y]=gray! Cycle at y. cycle = path[2:] + y = [y, z, y].
- Blacken all path: a, x, y, z all black.

Scan `w`: w is white, walk from w: path=[w], cur=w, next=x, color[x]=black! cur becomes black, break.

So one cycle reported. 

### Issue 3: `walkFallbacks` - path aliasing in return value

Looking at the old code:
```go
return append(path[i:], next), true
```

This was a bug - `path[i:]` creates a slice that aliases the same underlying array. If the caller modifies it, it would corrupt `path`. But since the caller doesn't modify, it's fine.

The new code explicitly creates a new slice:
```go
cycle := make([]string, 0, len(path)-i+1)
cycle = append(cycle, path[i:]...)
cycle = append(cycle, next)
```

Good fix.

### Issue 4: `walkFallbacks` - "Unreachable" comment

The comment says "Unreachable: next is gray, hence already on the current path. The loop cannot complete without matching." But what if the comparison fails to find `next` in `path`? That would be a logic error. But since the code just returns from the function, an unreached return with `nil, false` would be wrong. Let me check if there's a return after the `for i, n := range path` loop... No, there isn't! After the loop, execution falls through to the `return nil, false` at the end of the function. That's wrong because the function already detected a cycle but returns `(nil, false)`.

Wait, let me re-read:

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
// Unreachable: next is gray, hence already on the current path.
```

Yes, if `next` is gray but not found in `path` (which is impossible by the invariant), we'd return `(nil, false)`. But the comment says "Unreachable", so this is acceptable.

### Issue 5: The `attribute()` function

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
        ...
```

This looks correct. `errors.Join` returns a value that implements `Unwrap() []error`, so this is the right way to check.

But wait - if `len(children) == 0`, `errors.Join(out...)` returns `nil`. Is that the right behavior? If the joined error had children that all became non-errors after attribution (e.g., nil entries), then we'd want to return nil. That seems fine.

What if `len(children) == 1` and that one child is attributed to a file? The result would be `errors.Join(single_error)` which equals the single error. But the attribution would be preserved. That's fine.

### Issue 6: `attribute()` - the `r` parameter is not used for non-joined errors

Let me look at the rest of `attribute()`:

Actually I can't see the full function in the diff. Let me look more carefully... The diff shows:
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
```

The rest is cut off. I need to assume the rest is similar to before.

### Issue 7: `r.Providers[name]` in the loop

```go
for _, name := range sortedKeys(r.Providers) {
    errs = append(errs, validateProvider(name, r.Providers[name])...)
}
```

This is fine.

### Issue 8: `validateAgent` - "references unknown provider" check

```go
if a.Provider == "" {
    errs = append(errs, agentErrf(name, "agent '%s': required field 'provider' is missing", name))
} else if _, ok := r.Providers[a.Provider]; !ok {
    errs = append(errs, agentErrf(name, "agent '%s' references unknown provider '%s'", name, a.Provider))
}
```

Good, this prevents double-reporting when provider is empty.

### Issue 9: `validateProvider` - redundant check

In the old code:
```go
if p.APIKeyEnv == "" {
    return providerErrf(name, "providers.%s: required field 'api_key_env' is missing", name)
}
if !envVarName.MatchString(p.APIKeyEnv) {
    return providerErrf(name, "providers.%s: api_key_env %q is not a valid environment variable name", name, p.APIKeyEnv)
}
```

In the new code:
```go
if p.APIKeyEnv == "" {
    errs = append(errs, providerErrf(name, "providers.%s: required field 'api_key_env' is missing", name))
} else if !envVarName.MatchString(p.APIKeyEnv) {
    errs = append(errs, providerErrf(name, "providers.%s: api_key_env %q is not a valid environment variable name", name, p.APIKeyEnv))
}
```

Good, this prevents double-reporting.

### Issue 10: URL parsing in `validateProvider`

In the old code:
```go
if p.BaseURL != "" {
    u, err := url.Parse(p.BaseURL)
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
        return providerErrf(name, "providers.%s: base_url must be a valid http or https URL", name)
    }
    if u.User != nil {
        return providerErrf(name, "providers.%s: base_url must not embed credentials (userinfo)", name)
    }
}
```

In the new code:
```go
if p.BaseURL != "" {
    u, err := url.Parse(p.BaseURL)
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
        errs = append(errs, providerErrf(name, "providers.%s: base_url must be a valid http or https URL", name))
    } else if u.User != nil {
        errs = append(errs, providerErrf(name, "providers.%s: base_url must not embed credentials (userinfo)", name))
    }
}
```

Good, prevents double-reporting.

### Issue 11: `walkFallbacks` - path doesn't include the leaf node

Looking at the walkFallbacks function again:

```go
for {
    next := r.Agents[cur].Fallback
    if next == "" {
        color[cur] = black
        break
    }
    if color[next] == gray {
        // cycle detection - blacken path
        ...
    }
    if color[next] == black {
        color[cur] = black
        break
    }
    // color[next] must be white
    path = append(path, next)
    color[next] = gray
    cur = next
}
```

This only blackens the current node (`cur`) when reaching a black or empty target. It doesn't blacken the entire path. So if a lead-in chain reaches a black node, only the last node becomes black. This means if we have a long chain, the intermediate nodes stay gray, but they'll never be re-walked because their outgoing edge is still pointing somewhere.

Actually wait, in the original code (before the cycle fix), this is fine because we only walk from white roots. If a node is gray, it's either still on a path (which won't happen since we're single-threaded) or it's been partially walked. But with the new accumulation behavior, we might encounter leftover-gray nodes...

Hmm, let me think. After cycle detection, we blacken all path nodes. So all nodes on the cycle path become black. Now if another walk encounters a black node, it just stops (cur becomes black, break). The walk returns `(nil, false)`.

But what if a walk has a chain like: w -> x -> y -> z -> y (cycle at y)?

- w is white, path=[w], cur=w, next=x, color[x]=gray, path=[w,x], cur=x
- next=y, color[y]=gray, path=[w,x,y], cur=y
- next=z, color[z]=gray, path=[w,x,y,z], cur=z
- next=y, color[y]=gray! Cycle. cycle = path[2:] + y = [y, z, y]. Blacken all: w, x, y, z.

OK that works.

What about: w -> x -> y (y has no fallback)?
- w is white, path=[w], cur=w, next=x, color[x]=gray, path=[w,x], cur=x
- next=y, color[y]=gray, path=[w,x,y], cur=y
- next="" (y has no fallback), color[y]=black, break.
- Returns (nil, false).

But w and x are still gray! If we later try to walk from a different root and reach x, the walk would see color[x]=gray and think it's a cycle!

Wait, but we only walk from white roots. So if w is white, we walk from w. If w is gray, we skip it (not white). 

But what if the walk leaves w and x gray (e.g., w -> x where x has no fallback)? Then w is gray, x is black. So w is still gray.

If we try to walk from w again - we won't, because we only walk from white roots. w is not white.

But what if w's fallback is empty, and we just leave w gray? Let's check: 
- w is white, path=[w], cur=w, next="" (w has no fallback), color[w]=black, break.

OK so if the start has no fallback, the start becomes black. Good.

But if the start has a fallback that leads to a dead end:
- w is white, path=[w], cur=w, next=x, color[x]=gray, path=[w,x], cur=x
- next="" (x has no fallback), color[x]=black, break.

Now w is gray, x is black. w is left gray!

This is a problem. If we then try to walk from a different node that edges into w, the walk would see color[w]=gray and think it's a cycle (or rather, would hit the gray check and think it's a cycle).

Let me check: in `ValidateFallbacks`, do we ever walk from a different node that edges into w? Let's see:
- d -> w. Walk from d: path=[d], cur=d, next=w, color[w]=gray! Cycle detected at w. cycle = path[1:] + w = [w, w]? No wait, next=w, path[1:]=[w]. So cycle = [w, w]? 

Wait, path at time of detection is [d]. The index of w in path is... not found! So the for loop completes without finding next in path, and we return (nil, false). That's the "unreachable" path.

This is a bug! If w is left gray (as a leaf), and another walk encounters w as a gray node, we'd erroneously detect a cycle or fail to find the cycle in path.

But wait, let me check the order of operations. In the outer loop, we walk from white roots in sorted order. So if w was walked first and left gray, then later we encounter a path like d -> w, we'd see w is gray.

Actually, let me re-check. When does the walk leave a node gray?

Looking at the function:
```go
for {
    next := r.Agents[cur].Fallback
    if next == "" {
        color[cur] = black
        break
    }
    if color[next] == gray {
        // ...cycle detection, returns
    }
    if color[next] == black {
        color[cur] = black
        break
    }
    // color[next] is white
    path = append(path, next)
    color[next] = gray
    cur = next
}
```

So a node is left gray if we hit a `break` after setting it gray (e.g., after appending to path). Wait no, the `break` only happens when next is "" (cur becomes black) or when next is black (cur becomes black). So in both break cases, cur is set to black.

But the intermediate nodes in path (other than cur) are left gray! 

Let me trace: w -> x -> y (y has no fallback).
- Start: w white, path=[w], cur=w, color[w]=gray
- next=x (w's fallback is x), color[x] is white. path=[w,x], color[x]=gray, cur=x
- next="" (x has no fallback), color[x]=black, break.

After this walk: w is gray, x is black.

Now walk from some node z that edges into w: z -> w.
- Start: z white, path=[z], cur=z, color[z]=gray
- next=w, color[w]=gray! Cycle detection triggered.
- next=w is at index... not in path (path is [z]). So the for loop completes, falls through to "unreachable" code, returns (nil, false).

This is a problem!

But wait, the test `TestValidateFallbacks_LeadInLeftGrayThenRevisited` should catch this. Let me look at the test cases:

Case 1: `{a:b, b:c, c:b, d:a}`
- a, b, c, d sorted.

Walk from a:
- path=[a], cur=a, color[a]=gray
- next=b, color[b] white. path=[a,b], color[b]=gray, cur=b
- next=c, color[c] white. path=[a,b,c], color[c]=gray, cur=c
- next=b, color[b] gray! Cycle. next=b at index 1. cycle = [b, c, b]. Blacken all path: a, b, c all black.

Now walk from b: b is black, skip.
Walk from c: c is black, skip.
Walk from d:
- path=[d], cur=d, color[d]=gray
- next=a, color[a] black! color[d]=black, break.

OK, all good. No leftover gray.

Case 2: `{a:x, x:y, y:z, z:y, w:x}`

Walk from a:
- path=[a], cur=a, color[a]=gray
- next=x, white. path=[a,x], color[x]=gray, cur=x
- next=y, white. path=[a,x,y], color[y]=gray, cur=y
- next=z, white. path=[a,x,y,z], color[z]=gray, cur=z
- next=y, gray! Cycle. next=y at index 2. cycle = [y, z, y]. Blacken all: a, x, y, z.

Walk from w:
- path=[w], cur=w, color[w]=gray
- next=x, color[x] black! color[w]=black, break.

All good. No leftover gray.

But the test cases don't include the "chain to dead end" scenario. Let me think if this is actually a problem in practice.

Consider: `{a:b, b:c, c:}` (c has no fallback, but b's chain is a -> b -> c, and a cycles through b somehow).

Actually, with the "each agent has at most one outgoing edge" invariant, here's the issue:
- If we walk a -> b -> c (c is dead end), then a and b are left gray, c is black.
- If we later walk d -> a (d is white), we hit color[a]=gray.

This is a real concern. The test `TestValidateFallbacks_LeadInLeftGrayThenRevisited` claims to be a regression test for this, but the test cases only cover the cycle scenarios, not the dead-end scenarios.

Hmm, but looking at the test, it does say "Both reviewer repros are covered: {a:b,b:c,c:b,d:a} and {a:x,x:y,y:z,z:y,w:x}." So maybe the original issue was specifically about cycles, not dead ends. Let me re-read the panic description:

> walkFallbacks colors a lead-in node ("a") gray on the way INTO a cycle ("b"<->"c"), but "a" is not part of the trimmed cycle path. If only the cycle path were blackened, a separate later root ("d") whose edge points at the leftover-gray "a" would trip walkFallbacks's "gray node not on current path" invariant and panic.

So the issue was that the cycle was detected and reported, but only the cycle path was blackened, leaving the lead-in gray. Then a later walk from a different root would hit the gray lead-in and panic.

The fix in the new code is to blacken the ENTIRE path, not just the cycle. Let me check:

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

Yes, it blackens all nodes in path, not just the cycle. Good.

But what about the dead-end case? If we walk a -> b -> c (c has no fallback, breaks), then a and b are left gray. This is a separate bug that exists in the original code and is NOT addressed by this fix.

Let me think about whether this matters. In the dead-end case:
- Walk a -> b -> c (c dead end). a and b left gray, c black.
- If we later try to walk a chain that hits b (e.g., d -> b), we'd see color[b]=gray, which triggers the cycle detection. Since b is not in path, the for loop completes, and we return (nil, false) silently. No cycle reported, no panic.

But the issue is: we'd have already reported the cycle involving a -> b -> c (none, since c is dead end), but then if d -> b exists, we'd... actually no, in the dead-end case, no cycle is reported. The walk just returns (nil, false).

Wait, let me re-trace. If we walk a -> b -> c (c dead end), no cycle is detected (c has no fallback), and the walk returns (nil, false). The walk also doesn't append any error to errs. So no error is reported for this chain.

But if a chain like d -> b is later walked, and b is gray, the gray check fires. next=b, color[b]=gray, so we go into cycle detection. path is [d]. We look for b in path - not found. For loop completes. We return (nil, false). No error reported.

But wait, the original test for the panic case had a cycle, and the issue was that the lead-in was left gray, and a later walk tripped the invariant. In that case, the cycle WAS reported (the first walk reported it), and the second walk panicked. Now with the fix, the lead-in is also blackened, so the second walk sees a black node and stops cleanly.

OK so the fix is correct for the cycle case. But the dead-end case still has the leftover-gray issue, just that it doesn't cause a panic in the current code (it silently returns nil, false). Is that OK?

Actually, let me re-check the dead-end case more carefully:

```go
for {
    next := r.Agents[cur].Fallback
    if next == "" {
        color[cur] = black
        break
    }
    ...
    path = append(path, next)
    color[next] = gray
    cur = next
}
```

If we walk a -> b -> c (c has no fallback), after the walk:
- a: gray (set on start)
- b: gray (set when appended to path)
- c: black (set in the `next == ""` branch)

If we later try to walk d -> b:
- d: gray (set on start)
- next=b, color[b]=gray!
- Cycle detection: next=b, look for b in path=[d]. Not found. For loop completes.
- We fall through to the "unreachable" comment and return (nil, false).

This is a silent failure - we don't report any error, but we also don't panic. The walk just returns as if nothing happened.

But is this a problem in practice? Well, the walk is called for every white root. If a root's chain is entirely through dead-end nodes, no cycle is reported. If a root's chain reaches a previously-walked gray node (which is actually a dead-end leaf), we'd silently skip it.

But actually, I realize: if b is gray because it's a lead-in to a dead-end c, then b's only edge is to c. So any walk that reaches b is equivalent to walking from b itself. The walk from d would be: d -> b -> c (dead end). The walk from b would be: b -> c (dead end). Both have the same result: no cycle.

So the silent skip is actually correct! The dead-end node b is "fully explored" in the sense that its chain is dead-end, and any walk that reaches b would also be a dead-end walk.

OK, so the dead-end case is not a real bug. Good.

But wait, there's another case. What if a gray lead-in node has been partially walked, and the walk from that node continues to be interesting? For example, a -> b, but b is just a dead end. If we walk from a: a becomes gray, then b becomes gray, then c is dead end, so b becomes black. Now a is left gray. If we walk from d -> a, we'd see a is gray, which would trigger the (incorrect) cycle detection.

Hmm, let me trace: walk from a:
- path=[a], cur=a, color[a]=gray
- next=b, white. path=[a,b], color[b]=gray, cur=b
- next="" (b has no fallback), color[b]=black, break.

After: a is gray, b is black.

Walk from d:
- path=[d], cur=d, color[d]=gray
- next=a, color[a]=gray!
- Cycle detection. Look for a in path=[d]. Not found. Return (nil, false).

So a silent skip. The walk from d doesn't report any error. But the walk from d would have been: d -> a -> b (dead end). No cycle.

OK so the silent skip is still correct in this case.

But what if the walk from a was interrupted and left a gray, and there's a real cycle elsewhere that involves a? For example: a -> b -> a (a is part of a cycle). Walk from a:
- path=[a], cur=a, color[a]=gray
- next=b, white. path=[a,b], color[b]=gray, cur=b
- next=a, color[a]=gray! Cycle. next=a at index 0. cycle = [a, b, a]. Blacken all: a, b.

OK that's fine, the cycle is detected and all involved nodes are blackened.

What about: a -> b, and d -> a, and a -> b is a dead end (b has no fallback). Walk from a leaves a gray, b black. Walk from d sees a gray, silent skip. No cycle. But that's correct because a -> b is dead end.

OK I'm convinced the dead-end case is fine.

Let me check one more case: what if the walk from a reaches a black node and the intermediate nodes are left gray?

Example: a -> b -> c, and c is already black. Walk from a:
- path=[a], cur=a, color[a]=gray
- next=b, white. path=[a,b], color[b]=gray, cur=b
- next=c, color[c]=black! color[b]=black, break.

After: a is gray, b is black, c is black.

If d -> a is later walked: d -> a, color[a]=gray, cycle detection (silent skip). No error reported. But the walk from d would have been: d -> a -> b -> c (dead end at c). No cycle. So silent skip is correct.

OK, the algorithm is correct. The fix to blacken all path nodes in cycle detection is also correct.

### Issue 12: `attribute()` - what if the joined error has a non-entry error child?

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

If a child is a non-entry error (e.g., a settings-level fault), the recursive `r.attribute(c)` would handle it (returning it unchanged or with the user-tier prefix). That's fine.

### Issue 13: `TestValidate_AccumulatesAllErrors` - test for settings and agent errors

The test:
```go
reg := &Registry{
    Providers: map[string]Provider{"openai": {APIKeyEnv: "OPENAI_API_KEY"}},
    Agents: map[string]AgentConfig{
        "alpha": {Provider: "openai", Model: ""},
        "bravo": {Provider: "openai", Model: "m", MinSeverity: "BOGUS"},
    },
    PayloadMode: "bogus",
    TimeoutSecs: intPtr(-1),
}
```

This registry has multiple errors. The test checks that all of them are reported. Good.

But wait, the test doesn't have a `stampSource` call, and `validate()` is called directly (not `attribute()`). So no file prefix is added. The test only checks that the error message contains certain substrings. That's fine.

### Issue 14: `TestAttribute_SettingsFaultGetsUserLabel` - what if the only error is a settings fault?

```go
reg := &Registry{
    Providers:   map[string]Provider{"p": {APIKeyEnv: "KEY"}},
    Agents:      map[string]AgentConfig{"a": {Provider: "p", Model: "m"}},
    PayloadMode: "bogus",
}
reg.stampSource(SourceUser)
```

This has one error (PayloadMode: "bogus"). After `validate()`, the joined error has one child. After `attribute()`, the child is attributed to "registry.yaml" and the joined error has one attributed child. The test checks that the message contains "registry.yaml: invalid payload_mode". Good.

### Issue 15: `walkFallbacks` - return value path aliasing fix

The fix from `return append(path[i:], next), true` to using a new slice is correct. The old code had a potential aliasing issue.

### Issue 16: Documentation update

The docs/registry.md change is a single line addition. Seems fine.

### Issue 17: `intPtr` helper

The test file uses `intPtr(-1)`. This is presumably defined elsewhere in the package. Let me check if it exists... The diff doesn't show it being added, so it must be in the existing test files. That's fine.

### Issue 18: `agentsWithFallbacks` helper

The test file uses `agentsWithFallbacks(map[string]string{...})`. This must be defined in the test file. The diff doesn't show the helper definition, but presumably it's in the test file (lines not shown). That's fine.

### Issue 19: Missing import for `sort` in graph.go

The diff shows `sort` being removed from graph.go's imports. But `sortedKeys` is now used in graph.go (via `ValidateFallbacks`). Is `sort` still needed in graph.go? No, because `sortedKeys` is defined in config.go and uses `sort` from its own imports. So removing `sort` from graph.go is correct.

### Issue 20: Missing `errors` import in config.go

The diff shows `errors` being added to config.go's imports. Yes, this is needed for `errors.Join`. Correct.

### Issue 21: Missing `sort` import in config.go

The diff shows `sort` being added to config.go's imports. Yes, this is needed for `sortedKeys`. Correct.

### Issue 22: The `attribute()` function - what if `joined.Unwrap()` returns an empty slice?

`errors.Join()` with no arguments returns nil. With one argument, it returns that argument. So `Unwrap()` would never return an empty slice for a non-nil joined error. The check is fine.

### Issue 23: The `TestReviewCmd_InvalidConfigReportsAllErrors` test

This test creates a config with multiple errors and checks that they're all reported. Let me verify the test:

```go
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
```

The config has:
- timeout_secs: -1 (settings range error)
- payload_mode: bogus (settings enum error)
- agent bruce missing model (required field error)
- agent bruce min_severity: BOGUS (enum error)

The test checks:
```go
assert.Contains(t, out, "timeout_secs", "AC3 fault must surface")
assert.Contains(t, out, "payload_mode", "AC2 fault must surface")
assert.Contains(t, out, "required field 'model'", "AC1 fault must surface")
assert.Contains(t, out, "min_severity", "AC2/AC4 fault must surface")
```

These all look reasonable. The test should pass if the implementation is correct.

### Issue 24: `os.MkdirAll(".atcr", 0o755)` in the test

The test creates a `.atcr` directory. This is needed for the review command to work. The test also creates a `.atcr/config.yaml`. This is fine.

### Issue 25: `initGitRepoWithChange(t)` in the test

This presumably initializes a git repo with a change. The test then runs `atcr review --base HEAD^`. This should work.

### Issue 26: `home, err := os.UserHomeDir()` - potential issue

If the test runs in an environment where `os.UserHomeDir()` fails
</think>
