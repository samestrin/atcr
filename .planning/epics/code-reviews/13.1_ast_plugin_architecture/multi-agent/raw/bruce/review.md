<think>
Let me carefully review this diff for issues. The sprint plan is about AST Plugin Architecture - implementing wazero-based WebAssembly plugins for AST parsing to replace the line-proximity grouping in the reconciler.

Let me analyze each file systematically:

1. **go.mod / go.sum** - Adding wazero dependency. Looks straightforward.

2. **internal/astgroup/benchmark_test.go** - Tests AST grouping accuracy
3. **internal/astgroup/cover.go** - AST covering block logic
4. **internal/astgroup/crosscompile_test.go** - Tests cross-compilation
5. **internal/astgroup/embed.go** - Embedded WASM parsers
6. **internal/astgroup/grouper.go** - Main grouper implementation
7. **internal/astgroup/grouping_test.go** - Grouper tests
8. **internal/astgroup/host.go** - Wazero host runtime
9. **internal/astgroup/host_test.go** - Host tests
10. **internal/astgroup/merkle.go** - Merkle hashing
11. **internal/astgroup/node.go** - Node type definition
12. **internal/astgroup/override_test.go** - Override directory tests
13. **internal/astgroup/parsers/** - Parser sources and build script
14. **internal/astgroup/testdata/corpus.json** - Test corpus
15. **internal/boundaries_test.go** - Boundary test additions
16. **internal/reconcile/astgrouping.go** - AST grouping integration
17. **internal/reconcile/astgrouping_test.go** - Integration tests
18. **internal/reconcile/gate.go** - Gate that wires up AST grouping
19. **reconcile/cluster.go** - Modified to use ClusterWith
20. **reconcile/grouper.go** - New Grouper interface and ClusterWith
21. **reconcile/grouper_test.go** - Tests for Grouper
22. **reconcile/reconcile.go** - Modified to accept Grouper in Options

Let me look for issues:

**Issue 1: cover.go - block indexing bug**

In `cover.go`, the `CoveringBlock` function:
```go
func CoveringBlock(root Node, line int) (block Node, addr string, ok bool) {
	if line < root.StartLine || line > root.EndLine {
		return Node{}, "", false
	}
	chain, subAddr := coveringChain(root, line)
	addr = segment(root, 0)
	block = root
	if len(chain) > 0 {
		addr += "/" + subAddr
		block = chain[len(chain)-1]
	}
	return block, addr, true
}
```

Wait - `coveringChain` returns a chain that excludes `n` itself. Let me trace through:

For `n = root`, `coveringChain` walks `root.Children`. For each child that covers the line:
- If child is a block: returns `[child, ...subChain]` with address starting at child
- If child is not a block: returns `subChain` (which excludes child) with subAddr

Then in `CoveringBlock`:
- The returned chain is the deepest covering blocks BELOW root
- The block returned is `chain[len(chain)-1]` - the deepest

Wait, but then the comment says "the deepest block-level node whose inclusive line span contains line". So if root is a "file" node, the function should return the deepest block child. That seems right.

But there's a subtle issue: the address returned is just `segment(root, 0) + "/" + subAddr` - but it doesn't account for which sibling index the root is at. Since root is always the tree root, that's fine.

Actually wait - in `coveringChain`, when `isBlock(ch)` is true, it increments `blockIdx` BEFORE checking. But the `blockIdx` is only incremented for siblings that are blocks AND don't cover the line. That's the sibling block index for the chain. But if a child is a block AND covers the line, then `blockIdx` represents how many preceding sibling blocks were skipped. That seems correct for distinguishing siblings.

But wait - there's a bug in the index calculation. In `coveringChain`:
```go
for i := range n.Children {
    ch := n.Children[i]
    if line < ch.StartLine || line > ch.EndLine {
        if isBlock(ch) {
            blockIdx++
        }
        continue
    }
    subChain, subAddr := coveringChain(ch, line)
    if isBlock(ch) {
        seg := segment(ch, blockIdx)
        ...
    }
    ...
}
```

The `blockIdx` is only incremented for non-covering siblings. But the segment index for the covering block should represent its position among ALL block siblings, not just non-covering ones. Actually, the comment says "sibling-block-index" - so it's the index among block siblings that don't cover the line. Hmm, that's an odd choice but could be deliberate for distinguishing.

Actually, thinking more carefully: if there are two sibling blocks at the same level and the line falls in the second one, then:
- First sibling: doesn't cover → blockIdx = 1
- Second sibling: covers → uses blockIdx = 1 as its segment index

That's consistent: the covering block's index is the count of preceding block siblings that don't cover. But what if there are multiple non-covering blocks before? The blockIdx correctly counts them.

Hmm, actually this seems to be an index for sibling blocks. It works but the semantic is "index among non-covering block siblings before this one." That's fine for distinguishing.

Actually wait, there's a deeper issue. The `coveringChain` function doesn't differentiate between "line falls in this child" and "line falls in this child's subtree". Looking again:

```go
if line < ch.StartLine || line > ch.EndLine {
    if isBlock(ch) {
        blockIdx++
    }
    continue
}
```

So if the line falls within ch's span, we proceed. Then:

```go
subChain, subAddr := coveringChain(ch, line)
if isBlock(ch) {
    seg := segment(ch, blockIdx)
    if subAddr != "" {
        seg += "/" + subAddr
    }
    return append([]Node{ch}, subChain...), seg
}
// ch covers but is not a block: its block descendants (if any) are the
// real cover; if it has none, the cover is n (empty chain).
return subChain, subAddr
```

Wait, there's a subtle issue. If `ch` is not a block but covers the line, and `subChain` is empty (meaning no block descendants), then we return `nil, ""`. That means the line is not covered by any block.

But in `CoveringBlock`, when chain is empty:
```go
chain, subAddr := coveringChain(root, line)
addr = segment(root, 0)
block = root
if len(chain) > 0 {
    addr += "/" + subAddr
    block = chain[len(chain)-1]
}
```

So block = root (the "file" node) when no chain. That's correct for the case where the line falls in a non-block region (between blocks at the top level).

OK, I think the logic is actually fine. Let me look for other issues.

**Issue 2: grouper.go - cache key uses non-cleaned path**

In `grouper.go`:
```go
func (g *Grouper) GroupKey(f reconcile.Finding) string {
    ...
    file := filepath.Clean(f.File)
    ...
    pf := g.treeFor(file, lang)
    ...
    return file + "\x00" + addr + "\x00" + MerkleHash(block)
}
```

But in `treeFor`:
```go
func (g *Grouper) treeFor(file, lang string) *parsedFile {
    ...
    if pf, ok := g.cache[file]; ok {
        return pf
    }
    ...
    path, ok := g.resolvePath(file)
    ...
}
```

The cache uses `file` which is `filepath.Clean(f.File)` - that's fine. But wait, there's a subtle issue: the cache could store negative results (failure). If the file later becomes valid (e.g., created after the first call), the cache will return the stale negative result. The comment acknowledges this:
> A parse or read failure is cached as a negative result so a bad file is not re-read for every finding in it.

This is a deliberate design choice, but it could be a bug if files appear between calls. However, for a reconcile run, the files should be fixed. This is documented as deliberate, so it's likely OK.

**Issue 3: grouper.go - resolvePath doesn't check symlinks**

The `resolvePath` function uses `filepath.Rel` which does string-based path resolution. If root contains a symlink, the relative path comparison might fail. But for code review this is likely acceptable.

**Issue 4: gate.go - grouper wiring**

```go
grouper, closeGrouper := astGrouperFor(opts.Root)
defer closeGrouper()
opts.Grouper = grouper
```

Wait - `opts` is passed by value to `Reconcile`. So mutating `opts.Grouper` here works because `Reconcile` receives a copy. But the original `opts` passed to `RunReconcile` is also being mutated... that's fine since `opts` is a local variable. Let me verify by looking at `RunReconcile`.

Actually, looking at the diff more carefully, `opts` in `RunReconcile` is a parameter (passed by value). So mutating it locally is fine.

But wait - there's a potential issue. The `grouper` needs `opts.Root` which is set before this point. Let me check... the code sets `grouper, closeGrouper := astGrouperFor(opts.Root)` AFTER all the prior setup. So Root should be set. OK.

**Issue 5: grouper.go - block sibling index in CoveringBlock**

Actually wait, I need to look at this more carefully. The `coveringChain` uses a `blockIdx` counter that's reset per call. So when called from `CoveringBlock(root, line)`:
- root is the top-level
- blockIdx starts at 0
- For each child of root that doesn't cover the line, if it's a block, increment blockIdx
- For the covering child:
  - If it's a block: return chain starting with that child, with segment using blockIdx
  - If it's not a block: recurse

This is a "single-pass" design - only one child can cover the line (assuming no overlapping spans, which is correct for ASTs). Good.

**Issue 6: parsers/src/goparser/main.go - ast.Inspect traversal bug**

Looking at the build function:
```go
build = func(an ast.Node) node {
    nd := node{Kind: nodeKind(an), StartLine: line(an.Pos()), EndLine: line(an.End())}
    nd.Name = nodeName(an)
    ast.Inspect(an, func(child ast.Node) bool {
        if child == nil || child == an {
            return true
        }
        if structural(child) {
            nd.Children = append(nd.Children, build(child))
            return false // don't descend; build recurses itself
        }
        return true
    })
    return nd
}
```

Wait, there's a bug here. `ast.Inspect` visits ALL descendants of `an`, not just direct children. The code uses `return false` to prevent descent into already-processed structural nodes. But what about the order?

`ast.Inspect` traverses in source order: child before siblings of children. So for:
```
func A() {
    if cond {
        x := 1
    }
    y := 2
}
```

When we call `build` on the FuncDecl:
- Visit children: BlockStmt, IfStmt (structural), AssignStmt (structural), ExprStmt (structural)
- Wait, that's not right. `ast.Inspect` does DFS pre-order.

Actually, `ast.Inspect` traverses the AST in depth-first order, calling the function for each node. When `return false` is called, it doesn't descend into that node's children.

So the order would be:
1. FuncDecl (an itself - returns true)
2. BlockStmt (child, structural → returns false, doesn't recurse into its children)
3. ... wait, but then the children of BlockStmt (IfStmt, ExprStmt) wouldn't be visited.

Hmm, but `ast.Inspect` calls the function for a node, then descends if true is returned. So:
1. FuncDecl → returns true (it's an itself check)
2. BlockStmt → structural, build it, return false (skip its children)
3. IfStmt → never reached because BlockStmt returned false!

That's a bug. The `return false` prevents descent into BlockStmt, which means its structural children (like IfStmt) are never visited. So the tree would only have one level of structural children.

Wait, let me re-read more carefully. The `build` function is called recursively. So when we build BlockStmt, it calls `ast.Inspect(BlockStmt, ...)` which will descend into its children. But the outer `ast.Inspect(FuncDecl, ...)` won't descend into BlockStmt's children.

So actually, the structural children of BlockStmt (which would be IfStmt, ExprStmt, etc.) are captured when we call `build(BlockStmt)`. The outer traversal correctly identifies BlockStmt as a direct structural child of FuncDecl, and then build(BlockStmt) recursively captures its structural children.

So the tree is correctly built. But let me trace through a case:

```
func A() {
    if cond {
        x := 1
    }
}
```

ast.Inspect visits:
1. FuncDecl (an) → returns true (skip)
2. FuncDecl.Type (Field, not structural) → returns true (descend)
   2a. Field.List → returns true
       2a.i. Field → returns true
3. FuncDecl.Body (BlockStmt, structural) → builds BlockStmt (recursive call), returns false

Wait, but `build(child)` returns the constructed node - it doesn't add to `nd.Children` until... oh actually, it DOES add to `nd.Children`:
```go
if structural(child) {
    nd.Children = append(nd.Children, build(child))
    return false
}
```

OK so the logic is:
- If child is structural, build it and add to nd.Children, then return false (don't descend further in THIS inspect).

When `build(child)` is called, it does its own ast.Inspect starting from child, which recursively captures child's structural descendants. So the tree is correctly built.

But there's still a subtle issue: what if a node has multiple structural children at the same level? E.g.:
```
func A() {
    x := 1
    y := 2
}
```

The FuncDecl.Body is a BlockStmt. Inside the BlockStmt are two AssignStmts. 

When we build(FuncDecl):
- ast.Inspect(FuncDecl) visits:
  1. FuncDecl (an itself, skip)
  2. FuncDecl.Recv, Type, Name (not structural)
  3. FuncDecl.Body (BlockStmt, structural) → build(BlockStmt) → returns a node with Children=[AssignStmt(1), AssignStmt(2)]
  - We add BlockStmt to FuncDecl.Children, return false (don't descend into BlockStmt)

So FuncDecl.Children = [BlockStmt(node{Children=[AssignStmt, AssignStmt]})]

That's correct!

But wait - what if a function has two consecutive stmts directly (not in a block)? Actually no, Go functions always have a BlockStmt as the body. So this is fine.

What about an `if` statement that contains another `if`?
```
if a {
    if b {
        x := 1
    }
}
```

The outer IfStmt has Body (BlockStmt) and Else (nil).
- ast.Inspect(IfStmt):
  1. IfStmt (an, skip)
  2. IfStmt.Cond, Init (not structural)
  3. IfStmt.Body (BlockStmt, structural) → build(BlockStmt)
     - build(BlockStmt):
       - ast.Inspect(BlockStmt):
         1. BlockStmt (an, skip)
         2. BlockStmt.List[0] = IfStmt (structural) → build(IfStmt)
           - build(IfStmt):
             - ast.Inspect(IfStmt):
               1. IfStmt (an, skip)
               2. Body (BlockStmt, structural) → build(BlockStmt)
                 - This BlockStmt's child is AssignStmt
                 - Return BlockStmt(node{Children=[AssignStmt]})
               3. Else is nil
             - Return IfStmt(node{Children=[BlockStmt(node{Children=[AssignStmt]})]})
           - return false
       - Return BlockStmt(node{Children=[IfStmt(node{Children=[BlockStmt(node{Children=[AssignStmt]})]})]})
     - Return false

So BlockStmt.Children = [IfStmt.Children = [BlockStmt.Children = [AssignStmt]]]

That looks correct.

OK, but what about a case like `for` loop with init, cond, post?
```
for i := 0; i < 10; i++ {
    x := 1
}
```

ForStmt has Init (AssignStmt), Cond (BinaryExpr), Post (IncDecStmt), Body (BlockStmt).

- ast.Inspect(ForStmt):
  1. ForStmt (an, skip)
  2. Init (AssignStmt, structural) → build(AssignStmt) → add to ForStmt.Children
  3. Cond (BinaryExpr, not structural) → return true (descend)
     - All children of BinaryExpr are expressions (not structural)
  4. Post (IncDecStmt, structural) → build(IncDecStmt) → add to ForStmt.Children
  5. Body (BlockStmt, structural) → build(BlockStmt) → add to ForStmt.Children

So ForStmt.Children = [AssignStmt, IncDecStmt, BlockStmt]. Hmm, that includes the init/post statements as direct children of the for loop, which seems wrong. The for loop should just contain the body block.

Actually, this might cause issues with the merkle hash - the init and post statements are typically considered part of the for loop syntax but not nested structural children. Hmm.

But this is debatable - it's a design choice. The "block" kinds in cover.go include "for", and the structural children are what's inside. Including init/post as structural children means they'd be visible in the tree.

This is a minor design issue, not a clear bug. But it could affect the AST matching.

**Issue 7: parsers/src/pyparser/main.go - missing case in classify**

Looking at the Python parser:
```go
var compoundKeywords = []string{"def", "class", "if", "elif", "else", "for", "while", "with", "try", "except", "finally"}

func classify(text string) (kind, name string) {
    t := strings.TrimSpace(text)
    if strings.HasPrefix(t, "async ") {
        t = strings.TrimSpace(t[len("async "):])
    }
    for _, kw := range compoundKeywords {
        if t == kw || strings.HasPrefix(t, kw+" ") || strings.HasPrefix(t, kw+":") || strings.HasPrefix(t, kw+"(") {
            switch kw {
            case "def":
                return "func", identAfter(t, "def")
            case "class":
                return "class", identAfter(t, "class")
            case "elif":
                return "if", ""
            default:
                return kw, ""
            }
        }
    }
    return "stmt", ""
}
```

Note: `else` and `except` are matched as keywords but classified as "else"/"except" - which are in `blockKinds`. OK.

But what about `async def`? The classify strips "async " and then looks for keywords. So "async def foo():" becomes "def foo():" which matches. Good.

What about `async for`? "async for x in items:" becomes "for x in items:" which matches as "for". Good.

What about decorators?
```
@decorator
def foo():
    pass
```

The decorator line starts with `@`, which after indent stripping is `@decorator`. This won't match any compound keyword and falls through to "stmt". Then `isHeader` returns false (doesn't end with ':'). So it's a leaf stmt in the parent scope. Then `def foo():` is the next line, processed as a header. That seems OK.

**Issue 8: cover.go - block sibling index when root is a block**

In `CoveringBlock`, the first segment is `segment(root, 0)`. But `root` is always the file/module level. The `segment` function returns `"kind:name:siblingIndex"`. So for root, it's always `file::0` or `module::0`. The sibling index of 0 for root makes sense as a fixed anchor.

But wait - what about a "file" with no Name? `segment(root, 0)` = "file::0". Hmm, the address has empty name between the two colons. That's a minor cosmetic issue.

**Issue 9: merkle.go - name omission affects hash equality**

```go
func MerkleHash(n Node) string {
    h := sha256.New()
    h.Write([]byte(n.Kind))
    h.Write([]byte{0})
    h.Write([]byte(n.Name))
    ...
}
```

The encoding uses `Kind \x00 Name (children...)`. If two nodes have the same Kind but one has a Name and the other doesn't, they hash differently. Good.

But the cover.go's `segment` function produces `"kind:name:siblingIndex"`. The MerkleHash uses just `Kind` and `Name`. The address (which includes sibling index) is part of the GroupKey but not the hash. So:
- Two identical blocks (same kind, name, structure) at different sibling indices in the same file would have the same Merkle hash but different addresses → different GroupKeys. 

Wait, but if two blocks are at different sibling indices, they have different addresses. The address includes the sibling index. So they're distinguished. Good.

Actually wait - the address is constructed as `segment(root, 0) + "/" + subAddr`. The `subAddr` includes the sibling indices. But what if two different files have identical code? The GroupKey includes `file` as the first segment, so they'd be different keys. Good.

**Issue 10: host.go - reactor _initialize vs _start**

```go
cfg := wazero.NewModuleConfig().WithStartFunctions("_initialize").WithName(lang)
mod, err := h.runtime.InstantiateModule(h.ctx, compiled, cfg)
```

This uses `_initialize` as the start function, not `_start`. For a reactor (library) module, this is correct. The Go `wasip1` build with `-buildmode=c-shared` creates a reactor. Good.

But wait - the parsers use `//go:wasmexport alloc`, `//go:wasmexport free`, `//go:wasmexport parse`. These are exported functions. The build target is `GOOS=wasip1 GOARCH=wasm` with `-buildmode=c-shared`. This produces a shared library that exports the Go functions. When loaded as a reactor, `_initialize` sets up the runtime. Good.

But there's no `_start` function. WASI preview1 typically expects `_start`. With `WithStartFunctions("_initialize")`, wazero will call `_initialize` instead. That's the standard reactor pattern.

**Issue 11: parsers/src/goparser/main.go - thread safety**

The `pins` map is accessed without a mutex. But since wasm modules execute on a single thread (no goroutines unless explicitly enabled), this is safe for a single wasm instance.

But wait - the parser's `parse` function is called from the host, which serializes via the `wasmParser.mu` mutex. So only one goroutine can be in `parse` at a time. The `pins` map accesses are then effectively single-threaded. OK.

But `alloc` and `free` are also called from the host. Both are serialized by `wasmParser.mu`. So this is fine.

**Issue 12: cover.go - off-by-one in coveringChain**

Looking again:
```go
for i := range n.Children {
    ch := n.Children[i]
    if line < ch.StartLine || line > ch.EndLine {
        if isBlock(ch) {
            blockIdx++
        }
        continue
    }
    ...
}
```

Wait - what if a node's start_line is greater than its end_line? That shouldn't happen for valid ASTs, but defensively...

Also, what if a child node has the same StartLine as another (sibling)? The `if line < ch.StartLine || line > ch.EndLine` would let the line fall into BOTH children. But the for loop would process only the first one (and return). So if two siblings start on the same line, only the first is considered the cover. This could be a bug.

But for typical ASTs (sequential statements don't usually overlap), this is unlikely to matter.

**Issue 13: cover.go - coveringChain includes non-block parents?**

Wait, let me re-read:
```go
subChain, subAddr := coveringChain(ch, line)
if isBlock(ch) {
    seg := segment(ch, blockIdx)
    if subAddr != "" {
        seg += "/" + subAddr
    }
    return append([]Node{ch}, subChain...), seg
}
return subChain, subAddr
```

If `ch` is a block and covers the line, we return `[ch, subChain...]`. `subChain` was computed by calling `coveringChain(ch, line)`, which returns the blocks BELOW ch that cover the line.

So the returned chain is the deepest block at the deepest level, with all blocks from ch down to deepest as elements.

In `CoveringBlock`:
```go
chain, subAddr := coveringChain(root, line)
...
if len(chain) > 0 {
    addr += "/" + subAddr
    block = chain[len(chain)-1]  // deepest
}
```

`block` is the deepest block. `chain` is the full chain.

`subAddr` is the address of the chain. It starts at the first child (which is the direct block child of root that covers the line), and goes down.

But wait - `addr` is `segment(root, 0) + "/" + subAddr`. So the full address starts with root's segment, then "/" then subAddr. subAddr starts with the FIRST BLOCK CHILD's segment (not ch itself if ch isn't a block).

Actually, let me trace more carefully:
- `CoveringBlock(root, line)`:
  - chain, subAddr := coveringChain(root, line)
  - If root.Children's covering chain has elements:
    - subAddr is the chain address (starts with the FIRST BLOCK CHILD's segment)
    - addr = segment(root, 0) + "/" + subAddr
  - block = chain[len(chain)-1] (deepest)

That seems right.

But wait - what if the line is covered by a non-block child of root, which contains a block that covers the line?
- E.g., root.file has a child that is a GenDecl containing a FuncDecl.
- Line falls inside the FuncDecl.

coveringChain(root, line):
- For each child of root:
  - If non-block child ch covers the line:
    - subChain, subAddr := coveringChain(ch, line)
    - isBlock(ch) is false, so return subChain, subAddr

So subChain contains the blocks below ch. Good.

Then in CoveringBlock, block = chain[len(chain)-1] = the deepest block below ch. Good.

But subAddr - what does it look like? Let me trace:
- coveringChain(file_node, line):
  - file_node.Children = [GenDecl]
  - GenDecl covers the line (it's the package decl, line 1-3)
  - subChain, subAddr := coveringChain(GenDecl, line)
    - If GenDecl is non-block, and inside GenDecl there's no block child (since GenDecl doesn't contain funcs in this example):
      - Returns nil, ""
  - isBlock(GenDecl) is true (it's in blockKinds)
  - Return [GenDecl], segment(GenDecl, 0) = "gendecl::0"

OK so in this case chain = [GenDecl], subAddr = "gendecl::0". block = GenDecl. addr = "file::0/gendecl::0".

That's fine for the package declaration.

Now consider: file has [GenDecl, FuncDecl]. Line falls in FuncDecl.
- coveringChain(file, line):
  - file.Children[0] = GenDecl (line 1-3, doesn't cover line 5)
    - blockIdx++ → blockIdx = 1
  - file.Children[1] = FuncDecl (line 5-10, covers)
    - subChain, subAddr := coveringChain(FuncDecl, line)
      - If FuncDecl has a BlockStmt child:
        - blockIdx2 starts at 0
        - BlockStmt covers → subChain, subAddr := coveringChain(BlockStmt, line)
          - BlockStmt has an IfStmt that covers
          - subChain2, subAddr2 := coveringChain(IfStmt, line)
            - IfStmt has BlockStmt body that covers
            - subChain3, subAddr3 := coveringChain(BlockStmt, line)
              - No block children
              - Returns nil, ""
            - isBlock(IfStmt) is true (it's in blockKinds)
            - Return [IfStmt, BlockStmt], "if::0/block::0"
          - isBlock(BlockStmt) is true (it's in blockKinds)
          - Return [BlockStmt, IfStmt, BlockStmt], "block::0/if::0/block::0"
        - isBlock(FuncDecl) is true (it's in blockKinds)
        - Return [FuncDecl, BlockStmt, IfStmt, BlockStmt], "func::0/block::0/if::0/block::0"
    - isBlock(FuncDecl) is true
    - Return [FuncDecl, ...], "func::0/..."

So chain = [FuncDecl, BlockStmt, IfStmt, BlockStmt], block = BlockStmt (deepest), addr = "file::0/func::0/block::0/if::0/block::0".

But wait - what if FuncDecl has multiple block children at the same level? E.g., FuncDecl has [Recv, Type, Body]. Only Body is structural in the children iteration. Actually no, in `build` for FuncDecl, we'd iterate through FuncDecl's children:
- FuncDecl.Recv (FieldList, not structural) → return true
- FuncDecl.Name (Ident, not structural) → return true
- FuncDecl.Type (FuncType, not structural) → return true
- FuncDecl.Body (BlockStmt, structural) → build, return false

So FuncDecl.Children = [BlockStmt]. OK.

But what if a FuncType had a parameter or result with a function type that has a body? Wait, FuncTypes don't have bodies in Go AST. OK.

**Issue 14: cover.go - block sibling index is only for sibling block children**

In `coveringChain`:
```go
blockIdx := 0
for i := range n.Children {
    ch := n.Children[i]
    if line < ch.StartLine || line > ch.EndLine {
        if isBlock(ch) {
            blockIdx++
        }
        continue
    }
    ...
}
```

The `blockIdx` counts block siblings that don't cover the line. If there are multiple block siblings and the line falls into one of them, that one's index is the count of preceding non-covering block siblings.

But this means if siblings are interleaved (block, non-block, block, non-block), the indices skip the non-block ones. That's probably intentional - only block siblings are distinguished.

Actually, thinking more about this... what if the line falls between two block siblings (in a non-block region)? Then none of the children cover the line (assuming the non-block regions aren't children, or are children but not blocks). In that case, all children are skipped, and the function returns `nil, ""`. Then `CoveringBlock` returns `block = root`.

But what if there ARE non-block children in the gap? E.g.:
```
file {
    func A() { ... }  // line 3-5
    x := 1             // line 7 (AssignStmt, non-block)
    func B() { ... }  // line 9-12
}
```

Line 7 is in AssignStmt. In coveringChain(file, 7):
- func A: doesn't cover, blockIdx → 1
- AssignStmt (non-block): covers
  - subChain, subAddr := coveringChain(AssignStmt, 7)
    - No block children cover
    - Returns nil, ""
  - isBlock(AssignStmt) is false
  - Return nil, ""

So chain is empty. block = root (file). addr = "file::0".

But this means line 7 is grouped with the file-level. Is that correct? The AssignStmt is a "stmt" kind which is intentionally excluded from blockKinds. So findings at line 7 would map to the file node.

That's consistent with the design philosophy: "findings in genuinely different blocks apart". An assignment at the file level (package level var init) is grouped with the file. Findings in functions A or B would be grouped with those functions.

**Issue 15: host.go - race condition?**

```go
func (h *Host) Parser(lang string) (Parser, error) {
    h.mu.Lock()
    defer h.mu.Unlock()
    ...
    compiled, err := h.runtime.CompileModule(h.ctx, wasm)
    ...
    mod, err := h.runtime.InstantiateModule(h.ctx, compiled, cfg)
    ...
    h.parsers[lang] = p
    return p, nil
}
```

The mutex protects the map. `h.runtime.CompileModule` and `h.runtime.InstantiateModule` are called within the lock. Looking at wazero docs, Runtime is safe for concurrent use, but the lock here is to protect the map.

Wait - is there a deadlock risk? If two goroutines call Parser("go") at the same time, both will try to compile and instantiate the same .wasm. The mutex serializes them, so only one will do the work, and the second will see the cached parser. Good.

Actually, I see a subtle issue: the lock is held during the wasm compilation and instantiation, which can be slow (first-time cost). This blocks other goroutines from getting any parser. But since compilation is idempotent (cached), this is only a one-time cost.

Actually, this could be improved with double-checked locking (check inside the lock without holding it, then re-check inside the lock), but it's not a correctness issue.

**Issue 16: gate.go - grouper lifetime**

```go
grouper, closeGrouper := astGrouperFor(opts.Root)
defer closeGrouper()
opts.Grouper = grouper

res := Reconcile(sources, opts)
```

The grouper is set on `opts` (which is a local copy). After `Reconcile` returns, `closeGrouper` is called via defer. Good.

But wait - `opts.Grouper = grouper` where `grouper` could be nil (if env var is set). In that case, `opts.Grouper = nil`. Then `Reconcile` calls `ClusterWith(AllFindings(sources), opts.Grouper)` which checks `if g == nil { return Cluster(findings) }`. Good.

**Issue 17: crosscompile_test.go - hardcoded module path**

```go
if data, rerr := os.ReadFile(filepath.Join(dir, "go.mod")); rerr == nil &&
    len(data) > 0 && hasModuleLine(string(data), "module github.com/samestrin/atcr\n") {
```

The module path is hardcoded. If the repo is renamed or moved, this test breaks. Minor issue.

**Issue 18: parsers/src/goparser/main.go - EndLine issue**

For ast.Stmt like a single ReturnStmt, EndLine is the position of the `)` or the last token. That might be on a different line than expected. But this is standard behavior for go/ast.

**Issue 19: cover.go - empty chain when root's children have non-block covers**

Wait, I covered this above. Let me think about another case.

If the file has a single FuncDecl child and the line is in the FuncDecl:
- coveringChain(file, line):
  - file.Children[0] = FuncDecl, covers line
  - isBlock(FuncDecl) = true
  - Return [FuncDecl, subChain...], "func::0/..."

So chain includes FuncDecl. block = deepest (FuncDecl's deepest block child). 

If the line is exactly at FuncDecl.EndLine: line = ch.EndLine, so `line > ch.EndLine` is false, and we proceed. Good.

**Issue 20: goparser - EndLine for FuncDecl without body**

A FuncDecl can have a nil Body (for function declarations without body, like in interfaces). Then `line(an.End())` could be 0. Let me check:
```go
nd := node{Kind: nodeKind(an), StartLine: line(an.Pos()), EndLine: line(an.End())}
```

If `an.End()` is invalid, `line(an.End())` returns 0 (because of the `IsValid()` check). So EndLine could be 0 for a func without a body.

Then in `SmallestCovering`, if line is in [StartLine, 0] - well, StartLine > 0, so the range is invalid. The function wouldn't cover any line. So findings inside the function (well, declarations don't have bodies) would skip past the func.

But interface method declarations don't have bodies, so they wouldn't typically be findings. Still, this is a potential edge case.

**Issue 21: benchmark_test.go - corpus is hardcoded**

The corpus file is in testdata/corpus.json. The benchmark is a Go test (`TestBenchmark_ASTGroupingAccuracy`). The `t.Logf` calls output information but aren't asserted beyond the recall and precision checks.

**Issue 22: gate.go - placement of grouper setup**

```go
adjudicating = true
}

// AST-isomorphism grouping (epic 13.1): the adopted primary clustering signal.
...
grouper, closeGrouper := astGrouperFor(opts.Root)
defer closeGrouper()
opts.Grouper = grouper

res := Reconcile(sources, opts)
```

The grouper is set up after the `adjudicating = true` assignment. This is fine because `opts` is mutated locally and passed to `Reconcile`.

But wait - `opts.Grouper = grouper`. What if `grouper` is nil (env var set)? Then `opts.Grouper = nil`, and `Reconcile` falls back to legacy clustering. Good.

**Issue 23: cluster.go - removal of inline clustering**

The original `Cluster` function had the clustering logic inline. The new version calls `proximityClusters(lined)`. The new `proximityClusters` function is in `grouper.go` and is the shared core.

This is a refactor for reuse. Let me verify the behavior is preserved.

Original:
```go
var cur []Finding
prevLine := 0
for _, f := range lined {
    switch {
    case len(cur) == 0:
        cur = []Finding{f}
    case f.Line-prevLine <= lineProximity:
        cur = append(cur, f)
    default:
        clusters = append(clusters, cur)
        cur = []Finding{f}
    }
    prevLine = f.Line
}
if len(cur) > 0 {
    clusters = append(clusters, cur)
}
```

New `proximityClusters`:
```go
func proximityClusters(lined []Finding) [][]Finding {
    var clusters [][]Finding
    var cur []Finding
    prevLine := 0
    for _, f := range lined {
        switch {
        case len(cur) == 0:
            cur = []Finding{f}
        case f.Line-prevLine <= lineProximity:
            cur = append(cur, f)
        default:
            clusters = append(clusters, cur)
            cur = []Finding{f}
        }
        prevLine = f.Line
    }
    if len(cur) > 0 {
        clusters = append(clusters, cur)
    }
    return clusters
}
```

Looks identical. Good.

**Issue 24: grouper.go - line sorting before grouping**

In `ClusterWith`:
```go
sort.SliceStable(lined, func(i, j int) bool { return lined[i].Line < lined[j].Line })
```

Findings within a file are sorted by line. But the original `Cluster` also sorts. Same behavior.

But wait - in `ClusterWith`, the keyed clusters are emitted in `keyOrder` which is the order of first appearance. The unkeyed remainder is sorted and clustered by proximity. The result combines:
- Keyed clusters (in first-appearance order)
- Unkeyed clusters (in line order, then by gap)

Hmm, the original `Cluster` processed everything in line order and grouped by proximity. The new `ClusterWith` mixes the two: keyed first, then unkeyed. This changes the ordering of clusters within a file.

But the comment says:
> within a file, keyed clusters are emitted in ascending-first-line order (keys are first seen while scanning line-sorted findings)

Wait, the comment says "ascending-first-line order", but the code uses `keyOrder` which is the first-appearance order in the line-sorted scan. Since findings are sorted by line, first-appearance = ascending-first-line. OK so the comment is right.

But this means the cluster ORDER is different from `Cluster`. That's a behavior change.

Actually, looking at the test `TestClusterWith_NilGrouperMatchesLegacyCluster`, it verifies that `ClusterWith(fs, nil) == Cluster(fs)`. With nil grouper, `ClusterWith` calls `Cluster` directly, so they're the same. Good.

But with a non-nil grouper, the order is different from `Cluster`. That's intentional per the design.

**Issue 25: gate.go - opts.Root not set for Grouper**

Wait, let me check `astGrouperFor(opts.Root)`. If `opts.Root` is empty (CLI doesn't set it), then:
- In `resolvePath`: `root = filepath.Clean(".")` 
- So findings with relative paths work, absolute paths outside "." are rejected.

That's documented behavior. OK.

**Issue 26: goparser - potential issue with collect children ordering**

In `build`:
```go
ast.Inspect(an, func(child ast.Node) bool {
    if child == nil || child == an {
        return true
    }
    if structural(child) {
        nd.Children = append(nd.Children, build(child))
        return false
    }
    return true
})
```

`ast.Inspect` traverses the AST in a specific order. Let me think about what order children are visited.

`ast.Inspect` documentation says: "Inspect traverses an AST in depth-first order: It starts by calling f(node); node must not be nil. If f returns a true value, Inspect invokes f recursively for each of the non-nil children of node, followed by a call of f(nil)."

So for each node, f is called on the node itself first, then on children in order, then nil. Children are visited in order.

For a FuncDecl with body containing two AssignStmts:
- Visit FuncDecl (skip)
- Visit Recv (skip)
- Visit Type (skip)
- Visit Body (BlockStmt, structural) → build → add to FuncDecl.Children
- Visit Body.List[0]? No, because Body returned false, so we don't descend.

Wait, but Body has children. The `return false` means we don't descend into Body's children in THIS ast.Inspect. But `build(Body)` starts its OWN ast.Inspect, which will visit Body's children.

So Body.Children (via build) = [AssignStmt, AssignStmt]. Good.

But what about the ORDER of multiple block children at the same level? E.g., if a FuncDecl had two BlockStmt children (which is unusual but possible in some constructs), they'd be added in the order visited. Since ast.Inspect visits in source order, this should be consistent.

**Issue 27: pyparser - significantLines could miss lines**

```go
indent := 0
j := 0
for ; j < len(raw); j++ {
    switch raw[j] {
    case ' ':
        indent++
    case '\t':
        indent += 8
    default:
        goto done
    }
}
done:
body := strings.TrimRight(raw[j:], " \t\r")
if body == "" || strings.HasPrefix(body, "#") {
    continue
}
```

For an empty line (just `\n`), `raw` is `""`, indent stays 0, body is "", which is skipped. Good.

For a line with only whitespace, indent > 0 but body is "", skipped. Good.

For a line with leading whitespace then code, indent is counted, body is the rest. Good.

But wait - what about Windows line endings (`\r\n`)? The code splits on `\n`, so `\r` remains in the raw string. `strings.TrimRight(raw[j:], " \t\r")` trims trailing `\r`. Good.

**Issue 28: pyparser - line numbering inside multi-line strings**

```go
for i, raw := range strings.Split(src, "\n") {
    startInString := delim != ""
    delim = scanTripleQuotes(raw, delim)
    if startInString {
        continue // entire physical line is inside a multi-line string literal
    }
    ...
}
```

For a line that starts inside a triple-quoted string, we skip it entirely. But the `i` here is the 0-based line index. The `lineno` field uses `i + 1`. So lines inside a docstring are not in `lines`, but their line numbers are still "taken" by subsequent code.

Wait, `lineno` is set from `i + 1` when the line is added to `out`. For a line that's skipped (inside string), it's not added. The next non-string line still gets its `i + 1` based on the original split. So line numbers in `out` are the ACTUAL line numbers (1-based) from the source.

But if we have:
```
def foo():
    """docstring
    spanning
    multiple lines"""
    x = 1
```

The split would give:
- "" (line 1) - empty
- "def foo():" (line 2) - header
- "    \"\"\"docstring" (line 3) - skip? Wait, delim starts as "", scanTripleQuotes sees `"""`, so delim = `"""`. The line is not skipped because startInString was false.
- "    spanning" (line 4) - startInString = true (delim = `"""`), skipped
- "    multiple lines\"\"\"" (line 5) - startInString = true (delim = `"""`), skipped. Then scanTripleQuotes finds `"""`, delim = "".
- "    x = 1" (line 6) - normal line

But wait, line 3 is NOT skipped (startInString = false at start), so it's added to `out`. Its text is `    """docstring` (after indent stripping, it's `"""docstring`). This starts with `"""` but classify doesn't match any keyword, so it returns "stmt". Then `isHeader` checks if it ends with ':' - it doesn't. So it's a leaf stmt.

That's a bug! Line 3 (the start of a docstring) gets added as a stmt, but it's actually part of a multi-line string.

Let me re-trace:
- Line 1: "" → skipped (empty)
- Line 2: "def foo():" → added with lineno=2
- Line 3: "    \"\"\"docstring" → startInString is false (delim starts as ""). After scanTripleQuotes, delim = `"""`. body = `"""docstring` (after stripping 4 spaces and trailing whitespace). Doesn't start with "#", so it's added with lineno=3. classify returns "stmt".
- Line 4: "    spanning" → startInString = true (delim = `"""`). Skipped.
- Line 5: "    multiple lines\"\"\"" → startInString = true. Skipped. After scan, delim = "".
- Line 6: "    x = 1" → startInString = false. Added with lineno=6. classify returns "stmt".

So we have lines: [(indent=0, lineno=2, text="def foo():"), (indent=4, lineno=3, text='"""docstring'), (indent=4, lineno=6, text="x = 1")]

Then in `build`:
- (0, 2, "def foo():") - header (ends with ':'), kind="func", name="foo"
- After header, next line is (4, 3, '"""docstring'). Indent (4) > header indent (0), so recurse into build with indent=4.
  - (4, 3, '"""docstring') - indent == 4, kind="stmt", name=""
  - Next is (4, 6, "x = 1") - indent == 4, kind="stmt", name=""
  - Return [stmt@3, stmt@6]
- Back at top level: no more lines.

So the func has children [stmt@3 (the docstring start), stmt@6 (x = 1)]. The docstring end is line 5, but it's not in the tree (correctly skipped).

Hmm, so line 3 shows up as a stmt. But for grouping purposes, line 3 is the start of the docstring. If a finding was reported at line 3, it would map to this stmt, not the func.

That's a potential bug. A finding reported on the docstring start (which a model might do) would be grouped with the docstring stmt rather than the surrounding func.

But this is the Python parser's behavior. The corpus test `py_docstring_with_fake_def_no_overmerge` tests:
```
def real_a():
    """
    def fake():
        pass
    """
    x = 1
    return x

def real_b():
    y = 2
    return y
```

Lines 6 (x = 1 in real_a) and 10 (y = 2 in real_b). Should NOT group (different functions).

With the parser as described:
- Lines in `out`: [(0,1,"def real_a():"), (4,2,'"""'), (4,6,"x = 1"), (4,7,"return x"), (0,9,"def real_b():"), (4,10,"y = 2"), (4,11,"return y")]
  - Wait, line 2 is `    """` (4 spaces, then """). Body after indent strip is `"""`. classify returns "stmt". isHeader returns false (doesn't end with ':').
  - Line 3: `    def fake():` - but startInString = true (delim = `"""` from line 2). Skipped.
  - Line 4: `        pass` - startInString = true. Skipped.
  - Line 5: `    """` - startInString = true. Skipped. After scan, delim = "".
  - Line 6: `    x = 1` - normal. Added.
  - Line 7: `    return x` - normal. Added.
  - Line 8: empty - skipped.
  - Line 9: `def real_b():` - normal. Added.
  - Line 10: `    y = 2` - normal. Added.
  - Line 11: `    return y` - normal. Added.

Hmm wait, but in my trace, the docstring start line (2) is added as a stmt in the body of real_a(). Let me check the test expectations.

The test expects:
- Line 6 (x = 1) and line 10 (y = 2) are NOT same block.

If line 6 is grouped under real_a() (as a child stmt), and line 10 under real_b(), they're different blocks. Good.

But the issue is that line 6's covering block is the stmt (or real_a?). The stmt at line 2 is a non-block, so its parent (real_a) is the deepest block. So line 6's covering block is real_a().

Wait, actually the stmt at line 2 is at indent 4, same as the x = 1 stmt. So they're sibling stmts in real_a's body. Both are non-blocks, so their parent block is real_a().

For line 6 (x = 1), SmallestCovering would walk: file → real_a (covers line 6) → BlockStmt? No wait, the python parser doesn't emit a BlockStmt between the func and its body. Let me re-check.

Actually looking at the pyparser code, there's no BlockStmt node. The structure is:
- module
  - func (real_a)
    - stmt ("""docstring start)
    - stmt (x = 1)
    - stmt (return x)
  - func (real_b)
    - stmt (y = 2)
    - stmt (return y)

So for line 6, the deepest block is `func real_a`. The Merkle hash of `func real_a` includes its children stmts.

For line 10, deepest block is `func real_b`. Different Merkle hash. Good - they're not grouped.

OK so the test should pass. But there's still the subtle issue that line 2 (docstring start) gets a stmt node. This might affect grouping if a finding was reported at line 2.

But wait, the test `py_docstring_drift_same_def` has:
```
def handler():
    """Does a thing.

    class NotReal:
        pass
    """
    a = compute()
    b = a + 1
    return b
```
Lines 7 (a = compute()) and 9 (b = a + 1). Should group (same function).

Both are in handler()'s body. SmallestCovering for both → handler (since there are no nested blocks). Same Merkle hash. Grouped. Good.

But the docstring start (line 2) gets a stmt. So the func's children include that stmt. The Merkle hash of `handler` would include all stmts. As long as both files in the test pair have the same structure (same docstring), the hashes match.

Hmm, but what if two different `handler` functions have different docstrings but same body code? Their Merkle hashes would differ (because the docstring stmt is a child). So they'd be grouped differently. That's actually probably correct behavior - different docstrings might warrant different "blocks" in the review sense.

OK I think the pyparser is OK for the PoC. Not perfect, but good enough for a PoC.

**Issue 29: cover.go - blockIdx doesn't account for all siblings**

Going back to `coveringChain`:
```go
blockIdx := 0
for i := range n.Children {
    ch := n.Children[i]
    if line < ch.StartLine || line > ch.EndLine {
        if isBlock(ch) {
            blockIdx++
        }
        continue
    }
    ...
}
```

The blockIdx only counts block siblings that don't cover the line. If a non-block sibling doesn't cover the line, it's not counted. So if we have:
- Sibling 1: func A (block, doesn't cover)
- Sibling 2: stmt x (non-block, doesn't cover)
- Sibling 3: func B (block, covers)

blockIdx would be 1 (only func A counted). Func B's segment index is 1.

But there's only ONE func block sibling (B), and its index should be 1 (the second func, 0-indexed). That's correct.

But what if:
- Sibling 1: func A (block, covers)
- Sibling 2: stmt x (non-block, doesn't cover)
- Sibling 3: func B (block, doesn't cover)

A covers. blockIdx starts at 0. isBlock(A) is true, but A covers, so we don't increment. subChain, subAddr = coveringChain(A, ...). Returns [A, ...], segment(A, 0).

So A's segment index is 0. Correct (first block sibling).

OK that works.

**Issue 30: gate.go - ordering of grouper setup**

Let me look at where the grouper is set up in gate.go:
```go
adjudicating = true
}

// AST-isomorphism grouping (epic 13.1): the adopted primary clustering signal.
...
grouper, closeGrouper := astGrouperFor(opts.Root)
defer closeGrouper()
opts.Grouper = grouper

res := Reconcile(sources, opts)
```

This is AFTER `adjudicating = true`. Is this a problem? `adjudicating` is used elsewhere; setting up the grouper doesn't depend on it. OK.

But wait - what about errors during grouper setup? `astGrouperFor` always returns a valid grouper (per the comment: "Construction never fails"). So no error handling needed. But what if the host creation fails (e.g., wazero can't initialize)? Looking at `NewHost`:
```go
if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
    h.initErr = fmt.Errorf("astgroup: WASI init: %w", err)
}
```

The error is stored in `h.initErr`, not returned. Then when `Parser` is called, it checks `h.initErr` and returns the error. So failures are deferred to parse time. OK.

**Issue 31: grouper_test.go - test for absent corpus**

The benchmark test reads `testdata/corpus.json`. If the file is missing, the test fails. But the file is added in the diff. OK.

**Issue 32: goparser - what about import statements?**

An ImportSpec is a Spec, not a Decl or Stmt. So it's not "structural". It would be inside a GenDecl.

For a GenDecl with imports:
- ast.Inspect(GenDecl):
  - GenDecl (skip)
  - GenDecl.Doc (skip, not structural)
  - GenDecl.TokPos (skip)
  - GenDecl.Tok (skip)
  - GenDecl.Lparen (skip)
  - GenDecl.Specs[0] = ImportSpec (not structural) → return true
    - Descend into ImportSpec children (Path, NamePos, etc.) - all expressions, not structural
  - GenDecl.Rparen (skip)

So GenDecl.Children = []. The GenDecl is a block (in blockKinds via "gendecl").

For a file with only imports, the file has one GenDecl child with no structural children. Findings inside the GenDecl (lines 1-5 say) would group with the GenDecl. Findings outside (line 6 say) would group with the file.

That seems OK.

**Issue 33: gate.go - opts mutation visibility**

```go
grouper, closeGrouper := astGrouperFor(opts.Root)
defer closeGrouper()
opts.Grouper = grouper
```

`opts` is a parameter of `RunReconcile`. The mutation is to the local copy. `Reconcile` receives this copy. So `opts.Grouper` is correctly set when `Reconcile` runs.

But the original caller's `opts` is not modified. That's probably desired (no side effects on caller).

OK I think I've found enough issues. Let me prioritize them:

1. **HIGH/MEDIUM**: The pyparser adds the docstring start as a stmt, which could lead to incorrect grouping. This is documented in the test corpus, so it's known behavior, but it's still a quirk.

2. **MEDIUM**: The `coveringChain` only considers one covering child. If a line falls at a sibling boundary (e.g., line 0 or beyond all children), it returns nil and the cover is the parent. This is the documented fallback but could be a bug if lines are off-by-one.

3. **MEDIUM**: The `grouper.go` cache caches negative results, so a file created after first parse won't be parsed. This is documented but could be a bug in dynamic environments.

4. **LOW**: `resolvesPath` uses string-based `filepath.Rel` which doesn't handle symlinks.

5. **LOW**: `grouper.go` has lock held during wasm compilation (one-time cost, but blocks other goroutines).

6. **LOW**: `coveringChain` uses `blockIdx` to count non-covering block siblings, which could lead to confusing addresses if the structure changes.

Let me look for actual correctness issues more carefully...

Actually, I realize there might be a more subtle issue. Let me look at the `coveringChain` more carefully:

```go
func coveringChain(n Node, line int) (chain []Node, addr string) {
    blockIdx := 0
    for i := range n.Children {
        ch := n.Children[i]
        if line < ch.StartLine || line > ch.EndLine {
            if isBlock(ch) {
                blockIdx++
            }
            continue
        }
        subChain, subAddr := coveringChain(ch, line)
        if isBlock(ch) {
            seg := segment(ch, blockIdx)
            if subAddr != "" {
                seg += "/" + subAddr
            }
            return append([]Node{ch}, subChain...), seg
        }
        return subChain, subAddr
    }
    return nil, ""
}
```

The `blockIdx` is reset to 0 for each call to `coveringChain`. This is correct (each level starts fresh). But the blockIdx is incremented ONLY for non-covering siblings. So the covering block's index is the count of preceding non-covering block siblings.

Wait, I realize this could be wrong for an important case. Consider:
```
file {
    func A() { ... }  // line 3-5
    func B() { ... }  // line 7-9
    func C() { ... }  // line 11-13
}
```

For line 12 (in func C):
- blockIdx starts at 0
- A doesn't cover (3 > 12? No, 12 > 5). Actually 12 > 5, so doesn't cover. blockIdx → 1.
- B doesn't cover (12 > 9). blockIdx → 2.
- C covers. isBlock(C). Return [C, ...], segment(C, 2).

So C's segment index is 2 (third func, 0-indexed). Correct.

For line 8 (in func B):
- A doesn't cover. blockIdx → 1.
- B covers. Return [B, ...], segment(B, 1).

B's segment index is 1 (second func). Correct.

For line 4 (in func A):
- A covers. Return [A, ...], segment(A, 0).

A's segment index is 0 (first func). Correct.

OK so the blockIdx correctly indexes among ALL block siblings. The "non-covering" qualifier in my mental model was wrong; it's just counting block siblings encountered so far (some of which may cover, some not). Actually wait, the covering one is not counted (we return before incrementing). So the count is "block siblings before the covering one".

Hmm, but if there are non-block siblings mixed in:
```
file {
    func A() { ... }  // block
    var x = 1          // non-block (GenDecl)
    func B() { ... }  // block
}
```

For line in B:
- A: block, doesn't cover. blockIdx → 1.
- var x: non-block, doesn't cover (assuming line not in x). blockIdx unchanged.
- B: block, covers. Return [B, ...], segment(B, 1).

B's segment index is 1. But there's only one other block sibling (A), so B is the second func (index 1). Correct.

What if var x DOES cover?
- A: block, doesn't cover. blockIdx → 1.
- var x: non-block, covers. subChain, subAddr := coveringChain(var x, line). Returns (nil, ""). isBlock(var x) is false. Return (nil, "").

So chain is empty, block is parent. Hmm. So a finding on `var x = 1` would be grouped with the file.

Wait, but `var x = 1` is a declaration, not a statement. In Go AST, it's a GenDecl. blockKinds includes "gendecl". So `isBlock(var x)` would return true.

Let me re-check. In goparser's `nodeKind`:
```go
case *ast.GenDecl:
    return "gendecl"
```

So GenDecl is mapped to "gendecl", which IS in blockKinds.

So in the python parser case, it doesn't emit BlockStmt, so the structure is different. But in the Go parser case, GenDecl would be a block.

OK so for the Go case with `var x`:
- A: block, doesn't cover. blockIdx → 1.
- var x: block, covers. subChain, subAddr := coveringChain(var x, line). Returns ([var x, ...], "gendecl::0" + ...). isBlock(var x) is true. Return [var x, ...], segment(var x, 1).

Wait but the coveringChain would iterate over GenDecl's children. GenDecl's children would be the ImportSpec or ValueSpec or similar. If line is in the var declaration, the spec covers. Hmm, but specs are not Decl or Stmt, so they're not structural.

Let me think... in goparser, for GenDecl:
- ast.Inspect(GenDecl):
  - GenDecl (skip)
  - GenDecl.Specs[i] = ValueSpec (not structural - it's a Spec, not Decl or Stmt) → return true
    - Descend into ValueSpec children - all expressions/idents

So GenDecl.Children = []. Good. So findings inside the GenDecl would map to the GenDecl.

OK I think the design is reasonable.

**Let me now look for more concrete issues.**

Going back to the diff:

1. **host.go - maxSourceBytes rejection**: If a source is > 8 MiB, Parse fails with an error, and the caller (grouper) caches it as a parse failure, falling back to proximity. That's documented behavior.

2. **goparser - Go AST packages**: The goparser uses `go/ast`, `go/parser`, `go/token`. These are standard library. Good.

3. **pyparser - no Python standard library**: It's a custom parser. Limited but fits the "lightweight" design.

4. **host.go - WASI snapshot preview1**: `wasi_snapshot_preview1.Instantiate(ctx, rt)`. This is the WASI preview1 spec. The Go build for wasip1 might or might not need WASI imports. Let me think...

Actually, `wasip1` builds with `-buildmode=c-shared` produce a reactor module that depends on WASI imports (fd_write, fd_read, etc.). The wazero host needs to instantiate WASI snapshot preview1 to satisfy these imports.

But the parsers don't actually use WASI I/O (no `//go:wasmimport` calls). They only use `//go:wasmexport` to expose functions. So technically, WASI imports might not be needed.

However, the Go runtime for wasm (when built with GOOS=wasip1) might initialize some WASI things on startup. So having WASI preview1 instantiated is defensive. It might add a small overhead but no functionality.

This is fine.

5. **grouper.go - opts.Grouper = grouper**: The mutation of opts.Grouper. Wait, but `opts` is a struct value, so `opts.Grouper = grouper` modifies the local copy. Good.

But there's an issue: what if `RunReconcile` is called multiple times with the same `opts`? Each call would set up a new grouper and tear it down. That's correct.

6. **gate.go - grouper lifetime**: The grouper is set up just before Reconcile and torn down via defer. If Reconcile is fast, the grouper is held briefly. Good.

7. **host.go - runtime.Close called while parsers might be in use**: 
```go
func (h *Host) Close() error {
    h.mu.Lock()
    defer h.mu.Unlock()
    return h.runtime.Close(h.ctx)
}
```

If a parse is in progress (holds wasmParser.mu), and Close is called (holds Host.mu), there's no deadlock because they're different mutexes. But the runtime might be closed while a parse is running. Hmm.

Actually wait, wasmParser.mu is held by Parse, which calls into the wasm module. The wasm module is owned by the runtime. If Close is called concurrently, it could close the module while Parse is still using it. That's a bug.

But in the use case here, Close is only called via defer after Reconcile completes. So no concurrent parse during Close. OK.

8. **grouper.go - cache memory growth**: The cache grows unbounded as files are parsed. For a large codebase, this could use significant memory. Each parsed file is a tree of Node structs with potentially many children.

This is a performance concern but not a correctness bug. It's mentioned in the issue scope.

9. **gate.go - opts.Root used for grouper**: `astGrouperFor(opts.Root)`. opts.Root is the root directory. The grouper resolves file paths relative to this root. But what if the source tree changes between calls? The cache could become stale. Documented behavior.

10. **goparser - ast.Inspect ordering**: The order of children in `build` depends on ast.Inspect's order. ast.Inspect visits children in the order they appear in the AST. For most ASTs, this is source order. But not guaranteed.

Hmm, actually ast.Inspect does visit in a specific order: for each node, f(node), then f(children[0]), ..., f(children[n-1]), then f(nil). So children are visited in the order they appear in the Children slice. For most Go AST types, this is source order.

11. **cover.go - blockKinds**: Includes "funclit" but not "block" for Go (Go has block as BlockStmt). Let me check:
```go
"file": true, "module": true, "func": true, "funclit": true,
"class": true, "type": true, "gendecl": true, "block": true,
```

Includes "block". goparser maps BlockStmt to "block". Good.

12. **goparser - the build function uses `ast.Inspect` but iterates over children via the visitor callback**: This is a pattern issue. The cleaner way is to iterate over direct children only. Using ast.Inspect + returning false for structural children is a workaround.

Actually, this pattern works but is inefficient. ast.Inspect visits ALL descendants. For a large AST, this is O(N) per structural child, leading to O(N^2) total. For typical code review files (small to medium), this is fine. For very large files, it could be slow.

13. **pyparser - missing case for async with, async for**: classify strips "async " prefix. So "async with foo:" becomes "with foo:" which matches as "with". Good.

14. **gate.go - opts.Grouper mutation**: 
```go
grouper, closeGrouper := astGrouperFor(opts.Root)
defer closeGrouper()
opts.Grouper = grouper

res := Reconcile(sources, opts)
```

`opts` is passed by value to Reconcile. The mutation `opts.Grouper = grouper` modifies the local copy. Reconcile uses opts.Grouper. Good.

15. **host.go - WithStartFunctions("_initialize")**: For a Go-built wasm reactor, the initialization function is `_initialize`. Good.

But wait - `wazero.NewModuleConfig().WithStartFunctions("_initialize")` - this calls `_initialize` on instantiation. The Go runtime needs to be initialized. Without this, calling exported functions might not work (e.g., GC, memory allocation).

For `//go:wasmexport` with Go 1.26, the `_initialize` is called automatically by wazero when set as a start function. Good.

Actually wait, let me double-check. The Go compiler for wasip1 (when using `//go:wasmexport`) generates a "reactor" style module. Reactor modules export `_initialize` (called once on instantiation) but NOT `_start`. When wazero's `WithStartFunctions("_initialize")` is used, wazero will call `_initialize` on instantiation. That's correct for reactors.

But by default, wazero might try to call `_start` if it exists. The Go reactor doesn't export `_start`, so wazero would error if it tries. Using `WithStartFunctions("_initialize")` overrides this default.

Good, this is correct.

16. **goparser - `pins` map thread safety**: The `pins` map is accessed by `alloc`, `free`, and `parse`. Since the wasm module runs on a single OS thread (Go wasm is single-threaded unless explicitly enabled), and the host serializes calls via `wasmParser.mu`, there's no concurrent access. Good.

But wait - what about garbage collection? Go's GC might run during a wasm call. If GC runs while pins has entries, those entries might be... no wait, pins keeps references to the byte slices, so GC won't reclaim them.

17. **goparser - GC interaction with guest memory**: The Go GC manages wasm linear memory (since wasip1 builds use Go's GC). The pins map holds slices with stable pointers. The guest sees the linear memory layout. As long as the host reads from the pointer within the lifetime of the pinned slice, the data is valid.

But wait - if the host calls free, the pins entry is removed. But the host then calls free(p) before the wasm uses p again? Actually, the wasm uses p only during the parse call. After parse returns, the host can free p.

But there's a subtle issue: the host's `defer` for `free(p)` runs AFTER parse returns. So during parse, p is still pinned. Good.

What about the result buffer? The host gets (rptr, rlen) from parse, reads the data, then defers free(rptr). Between the parse return and the deferred free, the host has the data. If the host reads after free (which it shouldn't), the data could be GC'd. The host reads BEFORE the defer fires, so the data is valid.

18. **pyparser - missing test for empty source**: The host_test.go has TestHost_ParseEmptySource for python. It expects `root.Kind == "module"`. Let me check pyparser's behavior for empty source.

In pyparser's parse:
```go
lines := significantLines(src)
root := node{Kind: "module", StartLine: 1, EndLine: 1}
if len(lines) > 0 {
    kids, _ := build(lines, 0, lines[0].indent)
    root.Children = kids
    root.EndLine = lines[len(lines)-1].lineno
}
return emit(root)
```

For empty src, lines = []. root = {Kind: "module", StartLine: 1, EndLine: 1}. Good, matches the test.

19. **grouper.go - resolvePath symlink handling**: Already covered.

20. **gate.go - opts.Root might be empty**: The grouper's resolvePath handles empty root by defaulting to ".". But "opts.Root" is documented to be the root directory of the reviewed repo. If it's not set (CLI bug), the grouper would work relative to cwd, which could be anywhere. Minor issue.

OK let me think about what specific findings to report. The sprint scope is AST Plugin Architecture, so I should focus on:
- AST grouping logic
- Wazero host
- WASM plugin management
- Reconciler integration
- Performance (<10ms repeat parse)
- Cross-compilation (zero CGO)

Let me prioritize:

**HIGH severity issues:**
- None that are clearly HIGH severity bugs.

**MEDIUM severity issues:**
1. The `coveringChain` only walks the FIRST covering child. If two siblings have overlapping spans (which shouldn't happen for ASTs but could for malformed parsers), only the first is considered. Edge case.

2. The grouper caches negative parse results, so if a file is created after first parse attempt, it won't be re-parsed. Documented but could surprise users.

3. The pyparser emits docstring start as a stmt (with no body), which could lead to incorrect grouping if a finding is reported on the docstring line. This is an edge case but could cause real bugs.

4. The `goparser` uses `ast.Inspect` + `return false` pattern to collect direct structural children. This is O(N^2) for a tree with N nodes. For large files (thousands of lines), this could be slow. Performance concern.

5. The `goparser` includes init/post statements of for loops as direct children, which could cause incorrect Merkle hashing for differently-initialized loops. Subtle correctness issue.

**LOW severity issues:**
1. `crosscompile_test.go` hardcodes the module path.
2. `resolvePath` doesn't follow symlinks (could fail or succeed incorrectly with symlinked paths).
3. The grouper holds the host lock during wasm compilation, blocking other goroutines (one-time cost).
4. The merkle hash could collide for nodes with names containing special chars (no, the encoding is unambiguous).

Hmm, let me focus on the most impactful issues:

1. **MEDIUM**: The goparser's `ast.Inspect` pattern with `return false` might have a correctness issue with deeply nested structures or unusual AST shapes. Let me think more carefully...

Actually wait. Let me re-examine the goparser more carefully:

```go
build = func(an ast.Node) node {
    nd := node{Kind: nodeKind(an), StartLine: line(an.Pos()), EndLine: line(an.End())}
    nd.Name = nodeName(an)
    ast.Inspect(an, func(child ast.Node) bool {
        if child == nil || child == an {
            return true
        }
        if structural(child) {
            nd.Children = append(nd.Children, build(child))
            return false // don't descend; build recurses itself
        }
        return true
    })
    return nd
}
```

OK so when structural(child) is true, we build(child) and return false. The return false prevents ast.Inspect from descending into child. But build(child) starts its own ast.Inspect, which will collect child's structural descendants.

What if a node has TWO structural children that are deeply nested in each other? E.g., an IfStmt inside an IfStmt inside a FuncDecl.

Actually, an IfStmt has Cond, Init, Body, Else. The Cond/Init are expressions/assigns. The Body is a BlockStmt (structural). The Else can be an IfStmt or BlockStmt (structural).

For nested ifs:
```
if a {
    if b {
        ...
    }
}
```

Outer IfStmt:
- Cond: BinaryExpr (not structural)
- Body: BlockStmt (structural) → build
  - BlockStmt:
    - List[0]: IfStmt (structural) → build
      - Cond: BinaryExpr (not structural)
      - Body: BlockStmt (structural) → build
        - Body: ...
      - Else: nil

Each ast.Inspect call descends into direct children. The `return false` prevents ast.Inspect from descending into a structural child further (but build does its own descent).

So the tree is correctly built with full nesting. Good.

But the ORDER of children in nd.Children depends on ast.Inspect's order. Let me check what order ast.Inspect visits children.

ast.Inspect visits in the order specified by the Children slice of each Node. For most AST nodes, this is source order (or some natural order).

For an IfStmt, the order is: Init, Cond, Body, Else. ast.Inspect would visit in this order. Structural check on each:
- Init (often nil, or AssignStmt) → if structural, build
- Cond (BinaryExpr) → not structural, descend (returns true), then visit children (all expressions, not structural)
- Body (BlockStmt) → structural, build
- Else (nil or another stmt) → if structural, build

So an IfStmt's children could be: [Init (if structural), Body, Else (if structural)].

Hmm, the order of Init vs Body depends on ast.Inspect's order. Let me check Go's ast.Inspect source...

Actually, ast.Inspect visits children in the order they're defined in the Children slice. For IfStmt:
```go
type IfStmt struct {
    If   token.Pos // position of "if" keyword
    Init Stmt      // initialization statement; or nil
    Cond Expr      // condition
    Body *BlockStmt
    Else Stmt // else branch; or nil
    ...
}
```

So children are visited in order: Init, Cond, Body, Else.

But wait, ast.Inspect uses reflection on the Children method (or Inspectable). For most types, Children returns a fixed order slice.

Hmm, but ast.Inspect isn't based on the Children method. Let me look at the actual implementation...

Actually, ast.Inspect is implemented in terms of a type switch that calls Inspect recursively on each field. The order is determined by the switch case order.

For an IfStmt, the fields are visited in the order they appear in the struct definition: If, Init, Cond, Body, Else.

If is a token.Pos (not a Node), skipped. Init is a Stmt (could be nil). Cond is an Expr. Body is a BlockStmt. Else is a Stmt.

So order: Init, Cond, Body, Else. The structural check returns false for Body, so the children of Body aren't visited in this Inspect call. They're visited in the build(Body) call.

OK so the tree is correctly built.

But what about an `if-else` chain? E.g.:
```
if a {
    ...
} else if b {
    ...
} else {
    ...
}
```

This is a single IfStmt with:
- Cond: a
- Body: BlockStmt (the if body)
- Else: IfStmt (the else-if chain, recursively)
  - Cond: b
  - Body: BlockStmt
  - Else: BlockStmt (the final else)

In goparser:
- Outer IfStmt:
  - Children: [BlockStmt (if body), IfStmt (else-if), BlockStmt (else)]
- Wait no. Outer IfStmt has Cond (not structural), Body (BlockStmt, structural), Else (IfStmt, structural).
- ast.Inspect(Outer IfStmt):
  - Init (nil, skip)
  - Cond (BinaryExpr, not structural) → return true, descend (no structural children)
  - Body (BlockStmt, structural) → build, return false
  - Else (IfStmt, structural) → build, return false

So Outer.Children = [BlockStmt, IfStmt]. 

But the Else IfStmt itself contains its own Body and Else:
- Else IfStmt:
  - Cond (not structural)
  - Body (BlockStmt, structural) → build, return false
  - Else (BlockStmt, structural) → build, return false

Else.Children = [BlockStmt, BlockStmt].

OK that's correct.

But the ORDER of Else.Children is: first Body, then Else. The Body's block is the if body of the else-if, and Else's block is the final else. They're different blocks.

In CoveringBlock for a line in the final else:
- coveringChain(Else IfStmt, line):
  - Cond: doesn't cover (or covers if line is in cond? Unlikely)
  - Body: doesn't cover
  - Else (BlockStmt): covers
- isBlock(Else) is true (it's in blockKinds).
- Return [Else BlockStmt, ...], segment(Else BlockStmt, 1).

So the final else's segment index is 1 (second block child of the else-if IfStmt). The if body's segment index is 0. Correct.

What about a line in the else-if body?
- Body covers. isBlock(Body) is true.
- Return [Body, ...], segment(Body, 0).
- Body's segment index is 0. Correct.

OK so the indexing is correct.

Now let me think about edge cases for the corpus tests...

**Test: go_different_funcs_far_apart**

Source:
```
package p

func A() {
    x := 1
}



func B() {
    y := 2
}
```

Lines 4 (x := 1) and 10 (y := 2). Should NOT group.

In goparser:
- File has [FuncDecl A (lines 3-5), FuncDecl B (lines 9-11)]
- FuncDecl A children: [BlockStmt (lines 3-5), with child AssignStmt (line 4)]
- FuncDecl B children: [BlockStmt (lines 9-11), with child AssignStmt (line 10)]

For line 4: SmallestCovering(file, 4) → A's children → BlockStmt → AssignStmt. Wait, AssignStmt is structural (it's a Stmt). So SmallestCovering for line 4:
- coveringChain(file, 4):
  - FuncDecl A: covers. isBlock(A) = true. 
    - subChain, subAddr := coveringChain(FuncDecl A, 4):
      - blockIdx starts at 0
      - FuncDecl A's children: [BlockStmt (covers line 4)]
        - isBlock(BlockStmt) = true.
        - Return [BlockStmt, subChain...], segment(BlockStmt, 0)
        - subChain for BlockStmt: line 4 covers AssignStmt (structural). Return [AssignStmt], segment(AssignStmt, 0).
      - Return [BlockStmt, AssignStmt], "block::0/stmt::0" (or similar)
    - isBlock(FuncDecl A) = true.
    - Return [FuncDecl A, BlockStmt, AssignStmt], "func::A::0/block::0/stmt::0"
- Return [FuncDecl A, BlockStmt, AssignStmt].

SmallestCovering returns the last element: AssignStmt. MerkleHash(AssignStmt) is some hash based on AssignStmt's structure.

Wait but AssignStmt has no structural children, so MerkleHash(AssignStmt) = "assign" + "\x00" + "" + "(" + ")" hash.

For line 10: similar, SmallestCovering = AssignStmt in B's body. MerkleHash should be the same (both are AssignStmt with no children, kind="assign", name="").

So the GroupKey would be:
- Line 4: "a.go" + "\x00" + addr + "\x00" + hash
- Line 10: "a.go" + "\x00" + addr + "\x00" + hash

The addr for line 4: "file::0/func::A::0/block::0/stmt::0"
The addr for line 10: "file::0/func::B::0/block::0/stmt::0"

The addresses are different! Even though the MerkleHash is the same, the full GroupKey includes the address. So they're different. 

But wait, this is the case where the test expects them to NOT group. If the GroupKeys differ, they're correctly not grouped. Good.

**Test: go_identical_shaped_funcs_separate**

Source:
```
package p

func A() int {
    return 0
}

func B() int {
    return 0
}
```

Lines 4 (return 0 in A) and 8 (return 0 in B). Should NOT group.

SmallestCovering for line 4: ReturnStmt in A. MerkleHash(ReturnStmt) = "return" + "" + "()".
SmallestCovering for line 8: ReturnStmt in B. MerkleHash = same.

But the addresses differ: "func::A::0/..." vs "func::B::0/...". So GroupKeys differ. Not grouped. Good.

**Test: go_same_func_large_drift_blank_lines**

Source has lots of blank lines:
```
package p

func Target() {
    x := 1




    y := 2
    _ = x + y
}
```

Lines 4 (x := 1) and 11 (y := 2). Should group.

In goparser, the AST doesn't include blank lines. So:
- FuncDecl Target has body BlockStmt covering lines 3-11 (or so, depending on where braces are).
- Inside the BlockStmt: AssignStmt at line 4, AssignStmt at line 11, ExprStmt at line 12.

For line 4: SmallestCovering = AssignStmt at line 4. addr: "func::Target::0/block::0/stmt::0" (or "stmt::1" depending on order).
For line 11: SmallestCovering = AssignStmt at line 11. addr: "func::Target::0/block::0/stmt::1" (or "stmt::2").

Wait, the addresses have different stmt indices. So GroupKeys differ. They would NOT group!

Hmm, that's a bug. Or is it?

Let me look at the test more carefully:

```json
{
    "name": "go_same_func_large_drift_blank_lines",
    "file": "a.go",
    "source": "package p\n\nfunc Target() {\n\tx := 1\n\n\n\n\n\n\ty := 2\n\t_ = x + y\n}\n",
    "a_line": 4,
    "b_line": 11,
    "same_block": true
}
```

If both lines map to different AssignStmts (different sibling indices), they have different addresses and different GroupKeys. So they wouldn't group, and the test would fail!

But the test is expected to PASS per the design. So either:
1. My understanding is wrong.
2. There's a bug in the implementation.

Let me re-trace the cover.go logic.

`CoveringBlock` returns `block, addr, true` where `block` is the deepest block.

But wait - SmallestCovering returns the deepest block, which for a leaf statement (AssignStmt with no block children) is the AssignStmt itself.

Wait, AssignStmt is a Stmt, which is structural. But is AssignStmt in blockKinds?

```go
var blockKinds = map[string]bool{
    "file": true, "module": true, "func": true, "funclit": true,
    "class": true, "type": true, "gendecl": true, "block": true,
    "if": true, "for": true, "while": true, "switch": true,
    "with": true, "try": true, "except": true, "finally": true, "else": true,
}
```

NO! AssignStmt is NOT in blockKinds. blockKinds only includes compound/block-level statements. Leaf statements like "return", "assign", "expr", "stmt" are NOT in blockKinds.

But goparser DOES map AssignStmt to "assign", which is NOT in blockKinds.

So in CoveringBlock for a line inside an AssignStmt:
- coveringChain(file, line):
  - FuncDecl Target: covers. isBlock(FuncDecl) = true.
    - subChain, subAddr := coveringChain(FuncDecl, line):
      - BlockStmt: covers. isBlock(BlockStmt) = true.
        - subChain, subAddr := coveringChain(BlockStmt, line):
          - AssignStmt: covers. isBlock(AssignStmt) = false.
            - subChain, subAddr := coveringChain(AssignStmt, line):
              - No block children.
              - Return nil, ""
            - isBlock(AssignStmt) is false.
            - Return subChain, subAddr = (nil, "")
          - isBlock(BlockStmt) is true.
          - Return [BlockStmt], segment(BlockStmt, 0) = "block::0"
        - isBlock(FuncDecl) is true.
        - Return [FuncDecl, BlockStmt], "func::Target::0/block::0"

So SmallestCovering for line 4 = BlockStmt (the function body). 

And for line 11, also BlockStmt (the function body). Same address. Same Merkle hash. Same GroupKey. Grouped. 

I made an error earlier - AssignStmt is NOT in blockKinds, so it's not a block. The covering block is the BlockStmt (or FuncDecl, depending on the nesting).

OK so the implementation is correct. Let me re-verify the test cases mentally.

**Test: go_same_func_small_drift**
```
func Target() {
    x := 1
    y := 2
    _ = x + y
}
```
Lines 4 (x := 1) and 6 (y := 2).

Both lines map to BlockStmt (Target's body). Same Merkle hash. Grouped. Good.

**Test: go_same_func_large_drift_blank_lines**
Same idea. Blank lines are not in AST. Both lines map to BlockStmt. Grouped. Good.

**Test: go_different_funcs_adjacent_lines**
```
func A() {
    x := 1
    _ = x
}

func B() {
    y := 2
    _ = y
}
```
Lines 5 (in A) and 9 (in B). Should NOT group.

Line 5 maps to BlockStmt A. MerkleHash(BlockStmt A) = some hash based on its children.
Line 9 maps to BlockStmt B. MerkleHash(BlockStmt B) = different hash (different structure).

But wait, what are BlockStmt A's children? In goparser:
- ast.Inspect(BlockStmt A):
  - BlockStmt (skip)
  - BlockStmt.List[0] = AssignStmt (structural) → build
  - BlockStmt.List[1] = ExprStmt (structural) → build

So BlockStmt A.Children = [AssignStmt, ExprStmt]. MerkleHash = hash of "block" + "(" + hash(AssignStmt) + "," + hash(ExprStmt) + ")".

BlockStmt B.Children = [AssignStmt, ExprStmt] (same kinds, same structure). MerkleHash = same.

So MerkleHash(BlockStmt A) == MerkleHash(BlockStmt B)!

But the ADDRESSES differ: A's address is "func::A::0/block::0", B's address is "func::B::0/block::0". The address includes the func name (from segment).

So GroupKey for line 5 = "a.go\x00file::0/func::A::0/block::0\x00hash"
GroupKey for line 9 = "a.go\x00file::0/func::B::0/block::0\x00hash"

Different. Not grouped. 

**Test: go_different_funcs_far_apart**
```
func A() {
    x := 1
}



func B() {
    y := 2
}
```
Lines 4 and 10.

Same as above. A and B have same body structure (AssignStmt only). MerkleHash matches, but addresses differ. Not grouped. Good.

**Test: go_if_vs_else_adjacent**
```
func F(v int) int {
    if v > 0 {
        return 1
    } else {
        return 2
    }
}
```
Lines 5 (return 1 in if) and 7 (return 2 in else). Should NOT group.

Line 5 maps to BlockStmt in if body. Line 7 maps to BlockStmt in else body.

In goparser, IfStmt has Body (BlockStmt, structural) and Else (BlockStmt, structural). So IfStmt.Children = [BlockStmt (if), BlockStmt (else)].

For line 5: coveringChain(IfStmt, 5):
- Body (BlockStmt) covers line 5. isBlock(Body) = true.
- Return [Body], segment(Body, 0).

For line 7: coveringChain(IfStmt, 7):
- Body (BlockStmt) doesn't cover (line 5-6).
- blockIdx → 1.
- Else (BlockStmt) covers line 7. isBlock(Else) = true.
- Return [Else], segment(Else, 1).

So line 5's address has BlockStmt at index 0, line 7's has BlockStmt at index 1. Different addresses. Not grouped. Good.

**Test: py_docstring_with_fake_def_no_overmerge**
The docstring case. Line 6 (x = 1 in real_a) and line 10 (y = 2 in real_b). Should NOT group.

Both map to their respective funcs. Different MerkleHash. Different addresses. Not grouped. Good.

OK so the implementation should pass the benchmark tests. Let me look for actual bugs now.

Going back to potential issues:

1. **goparser: ForStmt children include init/post**
   - A for loop's ForStmt has Children = [Init (if structural), Body (BlockStmt)] or [Init, Post, Body].
   - Wait, ForStmt has Init (Stmt), Cond (Expr), Post (Stmt), Body (BlockStmt).
   - In ast.Inspect(ForStmt):
     - Init (Stmt) - if AssignStmt, structural → build, return false
     - Cond (Expr) - not structural → return true, descend (no structural children)
     - Post (Stmt) - if IncDecStmt, structural → build, return false
     - Body (BlockStmt) - structural → build, return false
   
   So ForStmt.Children = [Init, Post, Body] (if Init and Post are structural).
   
   The CoveringBlock for a line in the body would map to Body. Different for loops with different inits would have different MerkleHash(Body), which is correct.
   
   But the CoveringBlock for a line in the INIT of a for loop would map to Init (a non-block stmt). So Init's parent (ForStmt) is the block. SmallestCovering = ForStmt.
   
   MerkleHash(ForStmt) includes Init, Post, Body. So different for loops with different inits/posts would have different ForStmt hashes. Two findings on the inits of different for loops would be grouped as "same block" (both map to ForStmt, different MerkleHash).
   
   Hmm, but wait - two findings in different for loops would have different addresses (different sibling indices). So they're distinguished by address. Good.

2. **goparser: RangeStmt**
   - RangeStmt has For, Key, Value, TokPos, Tok, Range, X, Body.
   - Key, Value, X are expressions (not structural).
   - Body is BlockStmt (structural).
   - So RangeStmt.Children = [Body].
   
   That's clean.

3. **pyparser: class with methods**
   - class C: has methods m and n.
   - classify returns "class", name = "C" for `class C:`.
   - Methods are nested defs at deeper indent.

   For class with two methods at lines 3 and 5:
   - line 3 (def m): coveringChain(module, 3):
     - class C: covers. isBlock = true.
       - subChain, subAddr := coveringChain(class C, 3):
         - def m: covers (indent > 0). isBlock = true. Return [def m], segment(def m, 0).
       - Return [class C, def m], "class::C::0/func::m::0"
   - line 5 (def n): similar.
     - def m: doesn't cover. blockIdx → 1.
     - def n: covers. Return [def n], segment(def n, 1).
     - Return [class C, def n], "class::C::0/func::n::1"

   Different addresses. Not grouped. Good.

4. **pyparser: nested if**
   - def check(v): if v: ...
   - For a line inside the if body:
     - coveringChain(module, line):
       - def check: covers. isBlock = true.
         - subChain, subAddr := coveringChain(def check, line):
           - if v:: covers (indent > 4). isBlock = true.
             - subChain, subAddr := coveringChain(if, line):
               - stmt a: covers. isBlock = false. Return (nil, "").
             - isBlock(if) = true. Return [if], segment(if, 0).
           - Return [def check, if], "func::check::0/if::0"

   So SmallestCovering = if. MerkleHash(if) includes its children stmts.

   Two findings in the same if block would both map to if, same address, same hash (assuming same structure). Grouped.

5. **pyparser: for loop body**
   - def loop(items): for it in items: ...
   - For lines in the for body:
     - coveringChain(module, line):
       - def loop: covers. isBlock = true.
         - subChain, subAddr := coveringChain(def loop, line):
           - total = 0: indent == 4, covers but not block. Return (nil, "") (its subChain is empty since it has no block children).
           - Wait, isBlock(total = 0 stmt) is false. So return (subChain, subAddr) = (nil, "").
           - isBlock(def loop) is true. Return [def loop], "func::loop::0".
   
   Hmm wait, total = 0 is at indent 4. def loop's children should include all stmts at indent 4. Let me re-check build:
   
   ```go
   func build(lines []pline, i, indent int) ([]node, int) {
       var out []node
       for i < len(lines) {
           cur := lines[i]
           if cur.indent < indent {
               break
           }
           if cur.indent > indent {
               // Defensive: a deeper line without a header (continuation or
               // inconsistent indent). Attach as a leaf at this level.
               out = append(out, node{Kind: "stmt", StartLine: cur.lineno, EndLine: cur.lineno})
               i++
               continue
           }
           kind, name := classify(cur.text)
           n := node{Kind: kind, Name: name, StartLine: cur.lineno, EndLine: cur.lineno}
           i++
           if isHeader(cur.text) && i < len(lines) && lines[i].indent > indent {
               kids, ni := build(lines, i, lines[i].indent)
               n.Children = kids
               i = ni
               n.EndLine = maxEnd(n)
           }
           out = append(out, n)
       }
       return out, i
   }
   ```
   
   For def loop at indent 0:
   - i = 0, lines[0] = (0, 1, "def loop(items):"). indent matches (0).
   - kind = "func", name = "loop". i → 1.
   - isHeader (ends with ':') = true. lines[1] = (4, 2, "total = 0"). indent (4) > 0. Recurse.
     - build(lines, 1, 4):
       - i = 1, lines[1] = (4, 2, "total = 0"). indent matches (4).
       - kind = "stmt", name = "". i → 2.
       - isHeader (ends with ':') = false. Don't recurse.
       - out = [stmt@2]
       - i = 2, lines[2] = (8, 3, "for it in items:"). indent (8) > 4. Defensive case: append as leaf stmt.
       - i = 2: out = [stmt@2, stmt@3]. i → 3.
       - i = 3, lines[3] = (8, 4, "total += it"). indent (8) > 4. Append.
       - i = 3: out = [stmt@2, stmt@3, stmt@4]. i → 4.
       - i = 4, lines[4] = (8, 5, "log(it)"). indent > 4. Append.
       - i = 4: out = [..., stmt@5]. i → 5.
       - i = 5, lines[5] = (4, 6, "return total"). indent (4) matches 4. kind = "stmt". i → 6.
       - isHeader false. out = [..., stmt@6]. i = 6.
       - End of lines.
       - Return out, 6.

   Wait, there's a problem here! The for loop's body (lines 3-5 in the for body, which is at indent 8) should be a CHILD of the for statement, not siblings of total = 0.

   Let me re-check the source:
   ```
   def loop(items):
       total = 0
       for it in items:
           total += it
           log(it)
       return total
   ```

   Indents:
   - def loop: 0
   - total = 0: 4
   - for it in items: 4
   - total += it: 8
   - log(it): 8
   - return total: 4

   So:
   - lines[0] = (0, 1, "def loop(items):")
   - lines[1] = (4, 2, "total = 0")
   - lines[2] = (4, 3, "for it in items:")
   - lines[3] = (8, 4, "total += it")
   - lines[4] = (8, 5, "log(it)")
   - lines[5] = (4, 6, "return total")

   build(lines, 0, 0):
   - i = 0, lines[0]: indent 0 matches. kind = "func", name = "loop". i = 1.
   - isHeader (ends with ':') = true. lines[1] = (4, 2, ...). indent (4) > 0. Recurse with build(lines, 1, 4).
     - build(lines, 1, 4):
       - i = 1, lines[1] = (4, 2, "total = 0"). indent matches. kind = "stmt", name = "". i = 2.
       - isHeader false. out = [stmt@2]. i = 2.
       - i = 2, lines[2] = (4, 3, "for it in items:"). indent matches. kind = "for", name = "". i = 3.
       - isHeader (ends with ':') = true. lines[3] = (8, 4, ...). indent (8) > 4. Recurse with build(lines, 3, 8).
         - build(lines, 3, 8):
           - i = 3, lines[3] = (8, 4, "total += it"). indent matches. kind = "stmt". i = 4.
           - isHeader false. out = [stmt@4]. i = 4.
           - i = 4, lines[4] = (8, 5, "log(it)"). indent matches. kind = "stmt". i = 5.
           - isHeader false. out = [stmt@4, stmt@5]. i = 5.
           - i = 5, lines[5] = (4, 6, "return total"). indent (4) < 8. Break.
           - Return [stmt@4, stmt@5], 5.
       - kids = [stmt@4, stmt@5], ni = 5. for.Children = [stmt@4, stmt@5]. i = 5. EndLine = max(3, 5) = 5.
       - out = [stmt@2, for@3]. i = 5.
       - i = 5, lines[5] = (4, 6, "return total"). indent matches. kind = "stmt". i = 6.
       - isHeader false. out = [stmt@2, for@3, stmt@6]. i = 6.
       - End of lines. Return [stmt@2, for@3, stmt@6], 6.
   - def loop.Children = [stmt@2, for@3, stmt@6]. EndLine = max(1, 6) = 6.
   
   Now for a line in the for body (line 4 or 5):
   - coveringChain(module, 4):
     - def loop: covers. isBlock = true.
       - subChain, subAddr := coveringChain(def loop, 4):
         - blockIdx = 0
         - stmt@2 (lines 2-2): doesn't cover (4 > 2). Not block. Skip.
         - for@3 (lines 3-5): covers. isBlock = true.
           - subChain, subAddr := coveringChain(for, 4):
             - stmt@4 (lines 4-4): covers. Not block. Return (nil, "").
           - isBlock(for) = true. Return [for], segment(for, 0) = "for::0".
         - isBlock(def loop) = true. Return [def loop, for], "func::loop::0/for::0".
   
   So SmallestCovering for line 4 = for. MerkleHash(for) includes its children stmts.

   Two findings in the same for body would both map to for. Grouped. Good.

6. **pyparser: docstring edge case**

Let me check the corpus test more carefully:

```json
{
    "name": "py_docstring_with_fake_def_no_overmerge",
    "source": "def real_a():\n    \"\"\"\n    def fake():\n        pass\n    \"\"\"\n    x = 1\n    return x\n\ndef real_b():\n    y = 2\n    return y\n",
    "a_line": 6,
    "b_line": 10,
    "same_block": false
}
```

Lines:
- 1: def real_a():
- 2:     """
- 3:     def fake():
- 4:         pass
- 5:     """
- 6:     x = 1
- 7:     return x
- 8: (empty)
- 9: def real_b():
- 10:     y = 2
- 11:     return y

In pyparser:
- Split by "\n":
  - "def real_a():" (line 1)
  - "    \"\"\"" (line 2)
  - "    def fake():" (line 3)
  - "        pass" (line 4)
  - "    \"\"\"" (line 5)
  - "    x = 1" (line 6)
  - "    return x" (line 7)
  - "" (line 8)
  - "def real_b():" (line 9)
  - "    y = 2" (line 10)
  - "    return y" (line 11)

- Iterate:
  - Line 1: delim = "". After scanTripleQuotes, delim = "". startInString = false. indent = 0. body = "def real_a():". Added. → (0, 1, "def real_a():")
  - Line 2: delim = "". After scanTripleQuotes, delim = `"""`. startInString = false. indent = 4. body = `"""`. Doesn't start with "#". Added. → (4, 2, `"""`)
  - Line 3: delim = `"""`. startInString = true. Skipped.
  - Line 4: delim = `"""`. startInString = true. Skipped.
  - Line 5: delim = `"""`. startInString = true. Skipped. After scanTripleQuotes, delim = "".
  - Line 6: delim = "". startInString = false. indent = 4. body = "x = 1". Added. → (4, 6, "x = 1")
  - Line 7: similar. → (4, 7, "return x")
  - Line 8: empty. Skipped.
  - Line 9: → (0, 9, "def real_b():")
  - Line 10: → (4, 10, "y = 2")
  - Line 11: → (4, 11, "return y")

So `lines` = [(0, 1, "def real_a():"), (4, 2, `"""`), (4, 6, "x = 1"), (4, 7, "return x"), (0, 9, "def real_b():"), (4, 10, "y = 2"), (4, 11, "return y")]

build(lines, 0, 0):
- i=0, lines[0]: indent 0 matches. kind="func", name="real_a". i=1.
- isHeader true. lines[1] = (4, 2, ...). indent 4 > 0. Recurse build(lines, 1, 4).
  - build(lines, 1, 4):
    - i=1, lines[1] = (4, 2, `"""`). indent matches. classify returns "stmt" (no compound keyword match). i=2.
    - isHeader false. out = [stmt@2]. i=2.
    - i=2, lines[2] = (4, 6, "x = 1"). indent matches. classify returns "stmt". i=3.
    - isHeader false. out = [stmt@2, stmt@6]. i=3.
    - i=3, lines[3] = (4, 7, "return x"). indent matches. classify returns "stmt". i=4.
    - isHeader false. out = [stmt@2, stmt@6, stmt@7]. i=4.
    - i=4, lines[4] = (0, 9, "def real_b():"). indent (0) < 4. Break.
    - Return [stmt@2, stmt@6, stmt@7], 4.
- real_a.Children = [stmt@2, stmt@6, stmt@7]. EndLine = max(1, 7) = 7.
- i=4, lines[4] = (0, 9, "def real_b():"). indent matches. kind="func", name="real_b". i=5.
- isHeader true. lines[5] = (4, 10, ...). indent 4 > 0. Recurse build(lines, 5, 4).
  - build(lines, 5, 4):
    - i=5, lines[5] = (4, 10, "y = 2"). indent matches. kind="stmt". i=6.
    - isHeader false. out = [stmt@10]. i=6.
    - i=6, lines[6] = (4, 11, "return y"). indent matches. kind="stmt". i=7.
    - isHeader false. out = [stmt@10, stmt@11]. i=7.
    - End. Return [stmt@10, stmt@11], 7.
- real_b.Children = [stmt@10, stmt@11]. EndLine = max(9, 11) = 11.
- End.

So tree:
- module (lines 1-11)
  - func real_a (lines 1-7)
    - stmt (lines 2-2) — the docstring start
    - stmt (lines 6-6) — x = 1
    - stmt (lines 7-7) — return x
  - func real_b (lines 9-11)
    - stmt (lines 10-10) — y = 2
    - stmt (lines 11-11) — return y

For line 6 (x = 1):
- coveringChain(module, 6):
  - func real_a: covers. isBlock = true.
    - subChain, subAddr := coveringChain(real_a, 6):
      - stmt@2: doesn't cover (line 6 > 2). Not block. Skip.
      - stmt@6: covers. Not block.
        - subChain, subAddr := coveringChain(stmt@6, 6):
          - No children. Return (nil, "").
        - Return (nil, "") since stmt@6 is not block.
      - Wait, that returns (nil, ""), but isBlock(stmt@6) is false. So return subChain, subAddr = (nil, "").
    - isBlock(real_a) = true. Return [real_a], "func::real_a::0".

So SmallestCovering for line 6 = real_a. MerkleHash(real_a) includes its children.

For line 10 (y = 2): similar, SmallestCovering = real_b. Different MerkleHash.

Different addresses. Different MerkleHashes. Different GroupKeys. Not grouped. 

But the MerkleHash of real_a includes the docstring stmt. If two different real_a functions had different docstrings but same bodies, their MerkleHashes would differ. So findings on x = 1 in different real_a functions with different docstrings would NOT group.

Is this correct? Depends on the use case. If the docstring is part of the "logical block" for review purposes, yes. If not, then no. The design chose to include it.

OK let me focus on real bugs now.

**Potential bug 1: goparser includes init/post as direct children of ForStmt**

Wait, I covered this. The MerkleHash of the ForStmt node includes Init, Post, Body. So different for loops with different inits have different ForStmt hashes. But findings inside the for loop body would map to Body, not ForStmt. So they'd be grouped by Body hash, which doesn't include Init/Post. That's correct - two findings in different for loops with same body structure but different inits would be grouped together. Hmm, is that right?

Well, two findings inside for loops with the SAME BODY structure but DIFFERENT INITS would be grouped by body. Is that correct?

The "logical block" is the for loop body. Different for loops with different inits have different "logical contexts", but if the bodies are identical, they could be considered the same "logical block". So yes, this is correct behavior.

**Potential bug 2: goparser's ast.Inspect efficiency**

For a large Go file (say 10,000 lines), ast.Inspect visits every node. For each structural child, we recursively call ast.Inspect on it. This is O(N^2) in the worst case (each visit scans the whole subtree, but we skip already-visited subtrees).

For the PoC, this is acceptable. Not a bug.

**Potential bug 3: goparser's children order**

The order of children depends on ast.Inspect's traversal order. For most nodes, this is the order of fields in the struct, which is the source order for fields like Init, Cond, Body.

But for some nodes, the order might be unexpected. E.g., a SpecDecl's Specs are in source order (typically).

For the CoveringBlock, the order of children determines the address. As long as the order is deterministic, the address is stable.

**Potential bug 4: pyparser classifies `class C(Base):` correctly?**

```python
class C(Base):
    def m(self):
        ...
```

classify gets text = "class C(Base):". 
- t = "class C(Base):"
- For kw = "class": t starts with "class ". 
- Return ("class", identAfter(t, "class"))
- identAfter: rest = "C(Base):". For r in rest: r='C' is not ( or : or ' '. So end = len(rest). Return rest = "C(Base):".

Hmm, the name is "C(Base):" instead of "C". The parentheses aren't stripped.

This could cause Merkle hashes to differ for `class C(Base):` vs `class C:` (both have name "C" in their parent context, but the actual name strings differ).

For the test `py_different_methods_in_class`:
```
class C:
    def m(self): ...
    def n(self): ...
```

Lines 3 (def m) and 5 (def n). Should NOT group.

Both defs are in class C. classify for "def m(self):" returns name = "m(self):" (not stripped).

For two different classes `class C:` and `class C(Base):`, the name strings differ. Their MerkleHashes differ. Different files, different addresses, different GroupKeys. Not grouped. Good.

But within the same file, two classes with same name and different bases would have different MerkleHashes. That's correct - they're different.

For the case where the name includes parens, the MerkleHash of the class includes this weird name. Two same-named classes with same bases would have same hashes. Two same-named classes with different bases would have different hashes. Correct.

So no bug here, just a quirk.

**Potential bug 5: pyparser `def m(self):` name extraction**

classify for "def m(self):" returns name = "m(self):". So two methods `def m(self):` and `def m(arg):` would have different names. Their MerkleHashes differ.

But two methods `def m(self):` and `def m(other):` should arguably be the same logical block. The parameter names are implementation details. Hmm.

For the test corpus, all defs use consistent parameter names (or no params), so this isn't tested. But it's a subtle correctness issue.

**Potential bug 6: goparser doesn't handle Go generics**

A generic function `func F[T any](x T) T { ... }` has a TypeParams field. This is a FieldList (not structural). So goparser would skip it.

The function body would be parsed normally. No bug.

**Potential bug 7: gate.go opts.Grouper mutation thread safety**

`opts` is a local variable. `opts.Grouper = grouper` mutates the local. Reconcile receives a copy. No race condition.

**Potential bug 8: Host.Parser concurrency**

```go
func (h *Host) Parser(lang string) (Parser, error) {
    h.mu.Lock()
    defer h.mu.Unlock()
    ...
}
```

Multiple goroutines calling Parser concurrently serialize on the mutex. The second goroutine sees the cached parser. Good.

But what if the first goroutine's compile/instantiate fails? The error is returned, but the partial state (compiled module?) might leak. Let me check...

```go
compiled, err := h.runtime.CompileModule(h.ctx, wasm)
if err != nil {
    return nil, fmt.Errorf("astgroup: compile %s: %w", lang, err)
}
cfg := wazero.NewModuleConfig().WithStartFunctions("_initialize").WithName(lang)
mod, err := h.runtime.InstantiateModule(h.ctx, compiled, cfg)
if err != nil {
    return nil, fmt.Errorf("astgroup: instantiate %s: %w", lang, err)
}
```

If InstantiateModule fails, the compiled module is still cached in the runtime. The next call to Parser would re-CompileModule (since `h.parsers[lang]` is empty due to the early return). This wastes the compilation work.

A minor inefficiency, not a correctness bug.

**Potential bug 9: Host.Parser race during initialization**

While one goroutine is compiling/instantiating (holding h.mu), another goroutine calling Parser would block. After the first returns, the second sees the cached parser. Good.

But what if the first goroutine's compilation is slow? Other goroutines block. Could be a performance issue.

For PoC with two languages, this is acceptable.

**Potential bug 10: grouper cache negative entries**

```go
g.cache[file] = pf // cache the (initially negative) result; updated below on success
```

If `resolvePath` fails, we cache a negative result and return. Future calls return the same negative result. Documented behavior.

If `os.ReadFile` fails, we cache negative and return. Documented.

If `g.host.Parser` fails, we cache negative. Documented.

If `parser.Parse` fails, we cache negative. Documented.

What if the file is created/updated between calls? The cache wouldn't reflect the change. This is a bug for dynamic environments, but for a single reconcile run (where files are fixed), it's fine.

**Potential bug 11: pyparser doesn't handle blank lines inside indented blocks**

```python
def foo():
    x = 1

    y = 2
```

In pyparser, lines = [(0, 1, "def foo():"), (4, 2, "x = 1"), (4, 4, "y = 2")]. The blank line 3 is not in lines.

build(lines, 0, 0):
- def foo: i=1. isHeader true. lines[1] = (4, 2, ...). Recurse build(lines, 1, 4).
  - lines[1] = (4, 2, "x = 1"). i=2.
  - lines[2] = (4, 4, "y = 2"). i=3.
  - End. Return [stmt@2, stmt@4].

foo.Children = [stmt@2, stmt@4]. The blank line is just not represented. That's fine.

**Potential bug 12: goparser for `if-else if` with init**

```go
if x := compute(); x > 0 {
    return x
}
```

IfStmt.Init = AssignStmt. IfStmt.Cond = BinaryExpr. IfStmt.Body = BlockStmt.

In ast.Inspect:
- Init (AssignStmt, structural) → build, return false
- Cond (BinaryExpr, not structural) → return true
- Body (BlockStmt, structural) → build, return false

So IfStmt.Children = [AssignStmt, BlockStmt]. 

For a line in `compute()` (the init's RHS), the AST has:
- IfStmt.Init = AssignStmt
  - AssignStmt.Lhs[0] = Ident "x"
  - AssignStmt.Rhs[0] = CallExpr (compute())
  - These are expressions (not structural).

So a line in `compute()` would map to the AssignStmt (init). SmallestCovering would walk up: IfStmt is the block. MerkleHash(IfStmt) includes its children.

OK no bug.

**Let me look for one more category: error handling issues**

1. **host.go: p.memory.Write failure**

```go
if !p.memory.Write(ptr, src) {
    return Node{}, fmt.Errorf("astgroup: write src out of range (ptr=%d len=%d)", ptr, len(src))
}
```

If the guest's memory is too small, this fails. The error is returned. The caller (grouper) caches it as a parse failure and falls back to proximity. Good.

But `ptr` came from `alloc(uint32(len(src)))`. If alloc returned a valid pointer but the memory is somehow not contiguous, this could fail. Wazero's Write should handle this correctly.

2. **host.go: p.memory.Read failure**

Similar handling. Good.

3. **host.go: parse call failure**

```go
pr, err := p.parse.Call(p.ctx, uint64(ptr), uint64(len(src)))
if err != nil {
    return Node{}, fmt.Errorf("astgroup: parse call: %w", err)
}
```

If the guest panics or returns an error, this fails. Good.

4. **goparser: parser.ParseFile failure**

```go
f, err := parser.ParseFile(fset, "src.go", src, parser.SkipObjectResolution)
if err != nil {
    return emit(node{Kind: "error", Name: err.Error()})
}
```

Even on syntax error, emit a minimal file node. The host checks for Kind == "error" and returns an error. The grouper caches the negative result and falls back to proximity. Good.

5. **pyparser: bad pointer**

```go
buf, ok := pins[ptr]
if !ok || int(n) > len(buf) {
    return emit(node{Kind: "error", Name: "bad pointer"})
}
```

Defensive check. Good.

OK I think I've done a thorough review. Let me compile the findings.

Actually, wait. Let me look at one more thing: the pyparser doesn't handle indented multi-line strings well in some cases. Let me think...

Actually no, the scanTripleQuotes handles multi-line strings correctly.

Let me also look at the test corpus to see if there are tests I'm missing.

Looking at the corpus tests:
- go_same_func_small_drift: lines 4, 6 in same func. Should group.
- go_same_func_large_drift_blank_lines: lines 4, 11 in same func with blank lines. Should group.
- go_same_if_block: lines 5, 7 in same if. Should group.
- go_signature_and_body_same_func: lines 3, 5 in same func (one in signature area, one in body). Should group.
- go_same_body_far_apart: lines 4, 9 in same func body. Should group.
- go_different_funcs_adjacent_lines: lines 5, 9 in different funcs. Should NOT group.
- go_different_funcs_far_apart: lines 4, 10 in different funcs. Should NOT group.
- go_if_vs_else_adjacent: lines 5, 7 in if vs else. Should NOT group.
- go_identical_shaped_funcs_separate: lines 4, 8 in different identical funcs. Should NOT group.
- go_off_by_one_same_func: lines 4, 5. Should group.
- py_same_def_small_drift: lines 2, 4. Should group.
- py_same_def_large_drift_blank_lines: lines 2, 8. Should group.
- py_same_method_in_class: lines 3, 5. Should group.
- py_for_loop_body: lines 4, 5. Should group.
- py_different_defs_adjacent: lines 3, 5. Should NOT group.
- py_different_methods_in_class: lines 3, 5. Should NOT group.
- py_off_by_one_same_def: lines 2, 3. Should group.
- go_same_for_body_drift: lines 6, 9 in same for body. Should group.
- py_nested_if_same_block: lines 3, 5 in same if. Should group.
- py_docstring_with_fake_def_no_overmerge: lines 6, 10 in different funcs. Should NOT group.
- py_docstring_drift_same_def: lines 7, 9 in same func with docstring. Should group.
- go_func_vs_func_off_by_one_boundary: lines 5, 8 in different funcs. Should NOT group.

22 test cases. The benchmark requires at least 20.

OK let me finalize my findings.

Actually, let me also check the `p.alloc` etc. nil check:

```go
p := &wasmParser{
    ctx:    h.ctx,
    mod:    mod,
    alloc:  mod.ExportedFunction("alloc"),
    free:   mod.ExportedFunction("free"),
    parse:  mod.ExportedFunction("parse"),
    memory: mod.Memory(),
}
if p.alloc == nil || p.free == nil || p.parse == nil || p.memory == nil {
    return nil, fmt.Errorf("astgroup: plugin %s missing required exports", lang)
}
h.parsers[lang] = p
return p, nil
```

But wait - if any export is missing, we return an error WITHOUT closing the module. The instantiated module `mod` is leaked.

Let me check wazero's API... `InstantiateModule` returns a module that needs to be `Close`d to release resources. If we don't close it on this error path, it's a resource leak.

Actually, looking at wazero docs: when the runtime is closed, all instantiated modules are also closed. So when `Host.Close` is called, the modules are cleaned up.

But if the host is reused (multiple parses for different languages), the leaked module stays in the runtime. Not a correctness bug, but a resource concern.

This could be a MEDIUM issue if the runtime is expected to be long-lived.

But for the PoC, the host is created and closed per reconcile run. Not a big deal.

OK let me write up findings now.

Let me also look at the `grouper_test.go` for missing tests:

The test `TestGrouper_RefusesPathOutsideRoot` tests that `../secret.go` and absolute paths outside root are rejected. But what about `subdir/../subdir/file.go`? That's a path that stays within root after normalization but uses `..`. Let me check resolvePath:

```go
p := file
if !filepath.IsAbs(p) {
    p = filepath.Join(root, file)
}
p = filepath.Clean(p)

rel, err := filepath.Rel(root, p)
if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
    return "", false
}
```

filepath.Join cleans the path, so `subdir/../subdir/file.go` becomes `subdir/file.go`. Then filepath.Rel("root", "root/subdir/file.go") = "subdir/file.go". Not starting with "..", so it's accepted. Good.

What about `./file.go`? filepath.Join cleans it to `file.go`. Good.

What about an empty file? `file = ""`. filepath.Clean("") = ".". So p = root/. = root. filepath.Rel(root, root) = ".". That's not "..". So `rel == ".."` is false. `strings.HasPrefix(".", ".."+"/")` is false. So accepted.

Hmm, but the file passed to os.ReadFile would be root, which is a directory. os.ReadFile on a directory fails with an error. The grouper caches the negative result. OK.

But the cache key is the original file string (after Clean). If multiple findings pass empty file, they'd all share the same cache entry. The cache entry is negative. So all findings get "" GroupKey. They fall back to proximity. Consistent behavior.

Actually wait, let me re-check. In grouper.go:
```go
file := filepath.Clean(f.File)
```

If f.File = "", file = ".". Then treeFor(".", lang). The cache uses "." as key. resolvePath(".") returns root/filepath.Clean(".") = root. os.ReadFile(root) fails (directory). pf.ok = false. GroupKey returns "".

So a finding with empty file gets "" GroupKey. That's correct (fallback to proximity).

But what if f.File = "x.go" vs "./x.go"? Both clean to "x.go". Same cache entry. Same behavior. Good.

OK let me also check the Find File path normalization:
```go
file := filepath.Clean(f.File)
lang := LanguageForExt(strings.ToLower(filepath.Ext(file)))
```

filepath.Ext("x.go") = ".go". filepath.Ext("X.GO") = ".GO". strings.ToLower(".GO") = ".go". So uppercase is handled.

What about ".tar.go"? filepath.Ext = ".go". Treated as Go file. OK.

What about a file with no extension? filepath.Ext = "". LanguageForExt("") = "". GroupKey returns "". Good.

OK now let me look at the `grouper_test.go` more carefully:

```go
func TestGrouper_GroupsAcrossLineDrift(t *testing.T) {
    ...
    src := `package p

func Target() {
    x := 1
    y := 2
    _ = x + y
}

func Other() {
    z := 3
    _ = z
}
`
    path := filepath.Join(dir, "code.go")
    require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
    ...
}
```

The src has a trailing newline after the last `}`. So file has lines:
- 1: package p
- 2: (empty)
- 3: func Target() {
- 4:     x := 1
- 5:     y := 2
- 6:     _ = x + y
- 7: }
- 8: (empty)
- 9: func Other() {
- 10:     z := 3
- 11:     _ = z
- 12: }

Wait, the test uses Line 4, 6 for Target and Line 10 for Other. Let me check what happens.

goparser parses this Go code:
- File has children: [FuncDecl Target, FuncDecl Other]
- FuncDecl Target has body BlockStmt
- BlockStmt has children: [AssignStmt x:=1, AssignStmt y:=2, ExprStmt _ = x+y]

SmallestCovering for line 4 (x := 1):
- coveringChain(file, 4):
  - FuncDecl Target: covers (3-7). isBlock = true.
    - subChain, subAddr := coveringChain(Target, 4):
      - blockIdx = 0
      - BlockStmt (3-7): covers. isBlock = true.
        - subChain, subAddr := coveringChain(BlockStmt, 4):
          - AssignStmt (4-4): covers. Not block.
            - Return (nil, "").
          - Return (nil, "").
        - Return [BlockStmt], "block::0".
      - Return [Target, BlockStmt], "func::Target::0/block::0".

block = BlockStmt. addr = "file::0/func::Target::0/block::0". hash = MerkleHash(BlockStmt).

For line 6 (_ = x + y):
- Similar. BlockStmt covers. Same address. Same hash. Grouped. 

For line 10 (z := 3):
- coveringChain(file, 10):
  - FuncDecl Target (3-7): doesn't cover (10 > 7). blockIdx → 1.
  - FuncDecl Other (9-12): covers. isBlock = true.
    - subChain, subAddr := coveringChain(Other, 10):
      - blockIdx = 0
      - BlockStmt (9-12): covers. isBlock = true.
        - subChain, subAddr := coveringChain(BlockStmt, 10):
          - AssignStmt (10-10): covers. Not block.
            - Return (nil, "").
          - Return (nil, "").
        - Return [BlockStmt], "block::0".
      - Return [Other, BlockStmt], "func::Other::0/block::0".

block = BlockStmt. addr = "file::0/func::Other::0/block::0".

Different address (Target vs Other). Different GroupKey. Not grouped. 

OK so the tests should pass.

Let me look at one more potential issue: the `result` from wazero.

```go
pr, err := p.parse.Call(p.ctx, uint64(ptr), uint64(len(src)))
if err != nil {
    return Node{}, fmt.Errorf("astgroup: parse call: %w", err)
}
packed := pr[0]
rptr := uint32(packed >> 32)
rlen := uint32(packed)
```

The wasm function returns i64 (packed = resPtr << 32 | resLen). On 64-bit wasm, uint64 holds both. On 32-bit wasm... well, wasm32 doesn't exist anymore. wasip1 is wasm32 or wasm64. Go's wasip1 target is wasm32? Let me check.

Actually, Go's wasip1 target is 32-bit. wazero supports both wasm32 and wasm64. For wasm32, pointers are 32-bit. So the guest's "ptr" is 32 bits, but the return value is 64 bits.

If rptr > 2^32, the shift `packed >> 32` would lose information. But in wasm32, rptr < 2^32, so it's fine.

OK no issue.

**Now let me actually look at potential bugs more carefully:**

1. **Potential: pyparser emits docstring start as a leaf stmt**

This is a real behavior quirk. The docstring start line gets a stmt node. This affects Merkle hashing. For two funcs with same body but different docstrings, their MerkleHashes differ.

Is this a bug? It depends on the design. If the docstring is considered part of the "logical block", then yes, different docstrings = different blocks. If not, then this is a bug.

For review purposes, a docstring doesn't usually affect code logic. So a finding on `x = 1` should group with `x = 1` in another similar function, even if their docstrings differ.

But this is a PoC Python parser. It's intentionally lightweight. The current behavior is documented in the test name (`py_docstring_drift_same_def`).

2. **Potential: cover.go - blockIdx counting**

Wait, I want to verify one more thing. The blockIdx in coveringChain is incremented for non-covering block siblings. The covering block's segment index is the count of preceding non-covering block siblings.

But what if there are MULTIPLE covering siblings? ASTs shouldn't have overlapping siblings, but...

For safety, the function returns at the first covering sibling. So if there's an unexpected overlap, only the first is considered. The address would use the first covering sibling's index.

This could be a bug if the parser produces overlapping siblings (e.g., a malformed AST). But for valid ASTs, this shouldn't happen.

3. **Potential: goparser children order for IfStmt**

For an if-else chain like `if a {} else if b {} else {}`, the AST is:
- IfStmt
  - Cond (a)
  - Body (BlockStmt, if)
  - Else (IfStmt)
    - Cond (b)
    - Body (BlockStmt, elif)
    - Else (BlockStmt, else)

In goparser:
- ast.Inspect(outer IfStmt):
  - Cond: not structural
  - Body: structural → build, return false
  - Else (inner IfStmt): structural → build, return false

Outer.Children = [BlockStmt (if), inner IfStmt].

For inner IfStmt:
- ast.Inspect(inner IfStmt):
  - Cond: not structural
  - Body (BlockStmt, elif): structural → build
  - Else (BlockStmt, else): structural → build

Inner.Children = [BlockStmt (elif), BlockStmt (else)].

For a line in the elif body: coveringChain finds Body. Address: "if::0/block::1" (if we count blockIdx).
For a line in the else body: coveringChain finds Else. Address: "if::0/block::1" (same? Let me re-check).

Hmm wait, in inner IfStmt:
- blockIdx starts at 0
- Body (elif): covers if line in elif. Return [Body], segment(Body, 0).
- Else (else): doesn't cover line in elif. blockIdx → 1.
- Return [Body], segment(Body, 0) for line in elif.

For line in else:
- Body (elif): doesn't cover. blockIdx → 1.
- Else (else): covers. Return [Else], segment(Else, 1).

So Body has index 0, Else has index 1. Different addresses. Good.

OK no bug.

4. **Potential: cover.go - line at exact start/end of node**

If line == ch.StartLine, the condition `line < ch.StartLine || line > ch.EndLine` is false (since line < StartLine is false when equal). So the child is considered covering. Good.

If line == ch.EndLine, similarly the condition is false. Good.

5. **Potential: pyparser doesn't handle decorators properly**

A decorated function:
```python
@decorator
def foo():
    pass
```

Lines:
- 1: @decorator
- 2: def foo():
- 3:     pass

pyparser lines = [(0, 1, "@decorator"), (0, 2, "def foo():"), (4, 3, "pass")].

build(lines, 0, 0):
- @decorator: indent 0. classify: not a compound keyword. Returns "stmt". isHeader: doesn't end with ':'. i=1.
- def foo: indent 0. kind="func", name="foo". i=2. isHeader true. Recurse.
  - pass: indent 4. kind="stmt". i=3. End.
- def foo.Children = [stmt@3].

So @decorator becomes a sibling stmt of def foo in the module. That's incorrect - the decorator is conceptually part of the function.

But this is a PoC. The Python parser doesn't fully implement decorators. Documented limitation.

6. **Potential: goparser doesn't handle all Go syntax**

E.g., type assertions, channel sends, etc. are expressions, not statements/declarations. They're inside other nodes. So they're folded into the parent's Kind/Name.

This is by design - the goal is structural similarity, not exact AST reproduction.

7. **Potential: host.go - large memory allocations**

`alloc(uint32(len(src)))` allocates `len(src)` bytes. For a 8MB source, that's 8MB. The guest memory needs to hold this. Wazero's default memory size is 64KB? No, wazero allows setting memory limits.

Actually, the default wasm memory is 64KB pages. With 8MB source, we'd need at least 8MB + overhead. The wazero runtime allows growing memory.

If the runtime has a memory limit, the alloc could fail. The error would propagate up.

For PoC, no memory limit set. OK.

8. **Potential: gate.go - astGrouperFor called even when no AST grouping needed**

If the env var is set, astGrouperFor returns nil. opts.Grouper = nil. Reconcile uses legacy clustering.

If the env var is NOT set, astGrouperFor creates a wazero host. The host is created even if no findings need it (e.g., empty findings list).

For PoC, the overhead is one-time (host creation). Acceptable.

9. **Potential: crosscompile_test.go uses `go build`**

```go
cmd := exec.Command("go", "build", "-o", os.DevNull, "./cmd/atcr")
cmd.Dir = root
cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm64")
out, err := cmd.CombinedOutput()
require.NoErrorf(t, err, "zero-CGO cross-compile to linux/arm64 failed:\n%s", out)
```

This compiles the atcr binary for linux/arm64 with CGO disabled. If CGO is enabled somewhere, the cross-compile
</think>
