The user wants me to review the code changes as "kai — architecture and design reviewer". I need to focus on architecture/design issues in the changed regions. Let me analyze the diff carefully.

Files changed:
1. .planning/epics/active/18.0.1_debt-bulk-add.md → .planning/epics/deferred/18.0.1_debt-bulk-add.md (just moved, no content)
2. .planning/technical-debt/README.md - added a new debt item, updated stats
3. CHANGELOG.md - added changelog for 18.2.0
4. docs/findings-format.md - documentation for new fields
5. internal/reconcile/emit.go - added Justification and SourceReport fields to JSONFinding
6. internal/reconcile/emit_test.go - updated test to include new downstream-only fields
7. internal/reconcile/gate.go - added stampJustifications call in RunReconcile
8. internal/reconcile/justification.go - new file with narrative matching logic
9. internal/reconcile/justification_test.go - tests

Let me review each changed region for architectural/design issues:

1. .planning/technical-debt/README.md - just adding a debt item. This is meta-planning. The new debt item is about logging match rate. Not really an architecture issue in code.

2. CHANGELOG.md - documentation only.

3. docs/findings-format.md - documentation only.

4. internal/reconcile/emit.go:
   - Added Justification string field and SourceReport *SourceReport field
   - SourceReport struct with Path, Line, Section
   - These are additive and omitempty
   - Architecture concern: JSONFinding is becoming a large "bag of fields" that accumulates fields from many stages. This is an extensibility trap / contract design issue. The comments say "downstream-only fields" but the structure itself doesn't enforce this.
   - The SourceReport struct has Path as non-omitempty, Line and Section omitempty. This is inconsistent with documentation? Let me check docs: "path (review-dir-relative review.md path), line (1-based anchor line), section (nearest Markdown heading — omitted when absent)". Actually Path should always be present if SourceReport exists, so non-omitempty Path is fine.

5. internal/reconcile/emit_test.go:
   - Just added Justification and SourceReport to downstreamOnly map. Fine.

6. internal/reconcile/gate.go:
   - Added stampJustifications(jf, reviewDir) call after stampSymbolAnchors.
   - Architecture concern: The sequence of stamping passes is growing: JSONFindings → validateFindingPaths → stampSymbolAnchors → stampJustifications. Each pass mutates the JSONFinding slice in place. This is a pipeline of side effects on a flat data structure.
   - Another concern: stampJustifications reads from reviewDir/sources, but the sources were already discovered earlier. There's duplication: Discover already walks sourcesDir and knows the leaf directories. Now stampJustifications does another walk to collect review.md files. This is a duplication of responsibility / filesystem walk duplication.
   - The review.md path is discovered separately from the findings.txt discovery. There's no shared source representation.
   - The review.md reading happens after Reconcile, so it doesn't participate in the actual reconciliation. That's by design (narrative is downstream-only), but it means the "source" concept is split.

7. internal/reconcile/justification.go - This is the main new file. Let me look for issues:
   - Boundary/coupling: The package reconcile now knows about file path parsing, markdown structure, line range parsing, etc. This mixes concerns. Reconcile package deals with merging findings; now it also does narrative extraction.
   - The comment says "kept local to avoid an import cycle (fanout is a consumer, not a dependency, of this package)". This is a boundary issue - they duplicated a constant to avoid a cycle. But more importantly, the logic of parsing review.md could belong elsewhere (e.g., a report/reader package).
   - Duplication: reviewFileName constant is duplicated from internal/fanout. This is a hidden dependency / duplication.
   - Contract design: The anchorTier logic parses `file:N` and `file:A-B` with custom logic. This duplicates how findings.txt references are parsed elsewhere? The `isPathChar` and line range parsing may not match how the system elsewhere parses file:line references.
   - Extensibility trap: The parsing is hardcoded for specific markdown formats (list items, headings). The roadmap already contradicts this if different reviewers produce different review.md formats. The docs say "each reviewer (pool agent and host) writes" - but the format parsing is brittle.
   - Hidden dependency: Uses `sourcesSubdir` constant from the package (not visible in diff but implied).
   - Error handling: collectReviewNarratives silently skips unreadable files - "same resilience stance as Discover". This loses information. If a review.md exists but can't be read, it's silently ignored. This could mask permission issues.
   - The matchNarrative function has O(narratives × lines) complexity and extracts section for the best match only (good). But it scores every line in every review.md.
   - The `extractSection` function has brittle markdown parsing that may not match CommonMark or the actual review.md format.
   - The `anchorTier` has a subtle bug? Let me check: if `strings.Contains(s, file)` is true, best = 1. Then it scans for `file:` occurrences. But if `file` contains `:`? Probably not. If file is a suffix of a longer path, `strings.Contains` would set best=1, but the `file:` loop checks `isPathChar(s[abs-1])` to skip suffix matches. Wait, but if the file appears without `:`, best=1 anyway. So if review says "internal/x/y.go looked fine" and finding is for "y.go", then best=1 (because y.go is suffix of internal/x/y.go). Then in the `file:` loop, it will find `y.go:`? No, the line is "internal/x/y.go looked fine" - no colon after y.go. So best stays 1, which is below minAnchorTier=2, so no match. Good. But what if line is "internal/x/y.go:42"? The `file:` loop will find `y.go:` at abs. abs>0 and s[abs-1] is '/', which is isPathChar, so continue. best remains 1. Then minAnchorTier=2 rejects. Good.
   - But wait, what about case sensitivity? File paths on some systems are case-insensitive. The match is case-sensitive. Probably intended.
   - Another issue: `anchorTier` uses `strings.Contains(s, file)` to set best=1, but this is case-sensitive and could match a file name inside a code block, etc. But tier 1 is rejected, so OK.
   - The `reviewNarrative.leaf` is the directory name containing review.md. This is used as a reviewer tiebreak. But the actual reviewer name might not match the directory name (e.g., the host's review.md might be in "host" dir, but a finding's Reviewers could be ["host"] - OK. But what about pool agents? They might be in "pool/raw/agent/claude" - leaf would be "agent" or "claude"? Let me think: filepath.Base(filepath.Dir(path)) where path is .../sources/pool/raw/agent/claude/review.md → dirname is claude, leaf is claude. OK. But the finding's Reviewers after merge are individual reviewer names. So if review.md is in sources/pool/raw/agent/claude, leaf=claude. That matches. Good.
   - But what about the merged pool review? There might be a sources/pool/review.md. Leaf would be "pool". A finding might have Reviewers=["claude", "bruce"] but the merged pool review.md might discuss it. The leaf "pool" isn't a reviewer, so revPref=false. Then tiebreak goes to earliest narrative. This might pick the wrong review.md. This is a design issue: there's no mapping between merged review.md and the reviewers it represents.
   - Actually, wait: per docs, "A source's review.md — the human-readable narrative each reviewer (every pool agent and the host) writes alongside its findings.txt in the same leaf directory". So each agent has its own review.md. The merged pool/findings.txt is not re-discovered, but the docs say review.md is read at reconcile time. Does it also skip merged pool/review.md? The collectReviewNarratives walks all review.md files under sourcesDir. It doesn't exclude sources/pool/review.md. This could be a contract issue.
   - In Discover, "Per-agent raw files (`sources/pool/raw/agent/<name>/findings.txt`) are the pool inputs; the merged `sources/pool/findings.txt` is written for downstream convenience but is **not** re-discovered". But collectReviewNarratives doesn't have this leaf-preference exclusion for review.md. So if there's a sources/pool/review.md, it would be collected. This is a duplication of responsibility / inconsistent boundary.
   - More importantly, the review.md reading doesn't respect the `allow` parameter from RunReconcile. `Discover` is called with `allow` to restrict which immediate source children are read. But `stampJustifications` ignores `allow` and reads all review.md files under sourcesDir. This is a boundary violation / inconsistent access control. If a caller passes `allow` to restrict sources, review.md narratives from disallowed sources could still be attached.
   - Actually, let me check RunReconcile signature: `RunReconcile(ctx context.Context, reviewDir string, allow []string, opts Options)`. The `allow` restricts Discover. But stampJustifications gets only reviewDir. So yes, allow is ignored for narratives.
   - This is a real architecture issue: inconsistent application of source restrictions.
   - Also, `stampJustifications` doesn't take `ctx`, so it can't be canceled. But it's reading files, so it could be slow on large sources trees. Not a huge issue but a pipeline design concern.
   - Another issue: `stampJustifications` mutates `jf` in place, but there's no return of error. This is consistent with other stamp functions.

8. internal/reconcile/justification_test.go:
   - Tests are fine.

Let me also consider:
- The new debt item in README.md mentions "justification match rate is only logged at slog.Debug, so review.md format drift that silently breaks all matching goes unnoticed at default log verbosity". This is a real observability issue that the author themselves recorded as debt. As kai, I should flag this as an architecture/observability issue. But wait - the debt item is added BY this change, and it describes a problem with the change itself. So it's a valid finding. The file:line is in the changed region. Category could be observability. But the problem is already acknowledged in the debt file. Should I still flag it? The instructions say "findings only" and "No flattery, no summaries — findings only". The fact that they recorded it as debt doesn't mean I shouldn't flag it as a reviewer. Actually, it's in scope because the file is changed and the line references justification.go:60 which is in the new code. The debt item says the issue exists. I should flag it.

Wait, but the debt README is not code. The file:line reference `internal/reconcile/justification.go:60` points to the slog.Debug line. So the actual finding is in code. Let me make sure I reference the code line, not the debt file line. The changed code is in justification.go around line 330-340 where slog.Debug is called. Let me check: in justification.go, the slog.Debug call is at line 339 or so based on the diff. Let me count more carefully.

Actually, let me focus on code issues and give exact file:line.

Let me identify specific findings:

1. **CRITICAL/HIGH**: `stampJustifications` ignores the `allow` source restriction passed to `RunReconcile`. In gate.go, Discover is called with `allow`, but stampJustifications reads all review.md files under sourcesDir. This is a boundary/access-control inconsistency. If a tool or test passes `allow` to limit which sources participate, narratives from excluded sources can still attach to findings.

File:LINE: internal/reconcile/gate.go:228 (the stampJustifications call) or internal/reconcile/justification.go:86 (stampJustifications signature). Actually the call is the manifestation. Let me pick gate.go:228 approximately. The diff shows the call added after stampSymbolAnchors. In the diff context, it's around line 228. Let me look: in gate.go, the call is added around the line that says `stampJustifications(jf, reviewDir)`. Based on the diff, it's after "stampSymbolAnchors(jf, grouper)" which is around line 219. The new call is probably around line 225-228. I'll estimate internal/reconcile/gate.go:227.

Actually, I need to be more precise. The diff hunk for gate.go starts at line 155. The context around the change:
```
 	stampSymbolAnchors(jf, grouper)
+	// Correlate each finding to the narrative...
+	stampJustifications(jf, reviewDir)
```
The actual `stampJustifications(jf, reviewDir)` line is new. The hunk shows it after line 219 context. So the new line is around 227. Let me say internal/reconcile/gate.go:227.

2. **MEDIUM/HIGH**: Duplicated filesystem walk and inconsistent source model. Discover already walks sourcesDir and identifies source leaves. stampJustifications does a second independent walk to collect review.md files. This is duplication of responsibility. Worse, the review.md collection doesn't use the source metadata (like leaf directory) already discovered, and doesn't respect leaf-preference exclusion (sources/pool/findings.txt is excluded from discovery, but sources/pool/review.md is not excluded).

File:LINE: internal/reconcile/justification.go:118 (collectReviewNarratives) or internal/reconcile/gate.go:227 (the call site).

3. **MEDIUM**: The `reviewFileName` constant is duplicated from `internal/fanout` to avoid an import cycle, which is a hidden dependency / duplication. The comment admits this. This is a contract duplication.

File:LINE: internal/reconcile/justification.go:17

4. **MEDIUM**: `collectReviewNarratives` silently skips unreadable review.md files and subtrees, losing error information. The comment says "same resilience stance as Discover" but this means permission/read errors are swallowed. This is error-types-that-lose-information.

File:LINE: internal/reconcile/justification.go:120-145 (the walk function with error swallowing).

5. **MEDIUM/LOW**: `stampJustifications` has no context parameter, so long file reads cannot be canceled. The pipeline checks ctx between stages but not during the narrative read. This is a contract/coupling issue.

File:LINE: internal/reconcile/justification.go:86 (signature) or gate.go:227.

6. **MEDIUM**: Hardcoded markdown parsing assumptions (list item detection, heading detection, block extraction) in the reconcile package. The reconcile package now owns markdown narrative parsing, which is a responsibility leak. If review.md format evolves, this code must change. This couples finding reconciliation to narrative formatting.

File:LINE: internal/reconcile/justification.go:280-360 (extractSection, isItemStart, isHeadingLine, etc.)

7. **MEDIUM**: The match rate is only logged at Debug level, so a total miss (review.md format drift breaking all matching) is invisible at default log levels. This is the debt item the authors recorded. It's an observability issue.

File:LINE: internal/reconcile/justification.go:109 (slog.Debug). Let me count. In the diff, stampJustifications function ends with `slog.Debug("justifications stamped", "matched", matched, "total", len(jf))`. Looking at the diff, it's around line 109 of justification.go. Let me say justification.go:109.

8. **LOW/MEDIUM**: `JSONFinding` continues to accumulate stage-specific fields, becoming a large union struct. This is an extensibility trap. Fields like Justification, SourceReport, EvidenceExec, FixWarning, ClusterMerged, ClusterID, etc. are all in one struct. The comments document which stage sets which, but the type system doesn't enforce it. This is "APIs that lie" / contract design.

File:LINE: internal/reconcile/emit.go:96-130 (the struct fields).

9. **MEDIUM**: The `SourceReport.Path` field is documented as review-dir-relative, but there's no validation that it actually is relative or that it doesn't escape. `filepath.Rel` is used and skips if it fails, but if reviewDir is a prefix of path, it could still produce "../something" paths? Actually filepath.Rel(reviewDir, path) where path is under reviewDir will return a clean relative path without "..". If path is outside, it returns an error and is skipped. So OK. But the Path is then stored as-is. Not a big issue.

10. **MEDIUM**: `anchorTier` uses custom file:line reference parsing that may diverge from how other parts of the system parse such references. For example, the system may already have a parser for findings.txt rows or file:line references. Duplicating this logic risks drift.

File:LINE: internal/reconcile/justification.go:200-250 (anchorTier, parseLineRange, leadingInt).

11. **LOW**: `extractSection` walks up to find start until it hits a blank/heading/list item, but it doesn't handle nested list items or blockquotes consistently. This is a format assumption.

12. **MEDIUM**: The tiebreak logic `beatsMatch` is subtle and uses narrative index ordering. The narratives are sorted by relPath. A finding might get its narrative from a non-reviewer source if that source sorts earlier, even when a reviewer source exists at the same tier but sorts later. The revPref tiebreak only applies when tiers are equal. This is documented but brittle.

Actually the revPref tiebreak handles that: if tiers are equal, revPref wins. So if a reviewer source has the same tier, it wins regardless of sort order. Good.

13. **MEDIUM**: The minAnchorTier=2 and anchorLineProximity=3 are magic constants with no configuration or external validation. The proximity of 3 is tied to cluster-merge line divergence. This is a hidden dependency on clustering logic.

File:LINE: internal/reconcile/justification.go:33-42.

14. **HIGH?**: The `allow` issue is the most serious architectural problem. Let me think about severity. If `allow` is used in tests or by MCP to restrict sources, then narratives from excluded sources could attach. Is this breaking a load-bearing contract? The `allow` parameter's contract is to restrict which immediate source children are read. Narratives are part of the source input. So yes, this violates the contract. Severity HIGH.

Let me reconsider. In RunReconcile, `allow []string` restricts "which immediate source children are read". The review.md files are also immediate source children. So stampJustifications reading all of them violates the stated contract. HIGH seems right.

Actually, let me check the exact comment: "allow restricts which immediate source children are read (empty = open discovery)". Yes.

15. Another issue: The `stampJustifications` is called on `jf` which is the result of `res.JSONFindings()`. But after this, `res.jsonFindings = jf` is set. This is the same pattern as validateFindingPaths and stampSymbolAnchors. No new issue there.

16. Wait, I should also think about whether `stampJustifications` should run before or after path validation. If a finding has a path_warning (hallucinated path), but a review.md mentions that path, it could still get a justification. Is that OK? Probably yes - the justification is about the narrative, not path validity. But it's worth noting.

17. Also, `stampJustifications` doesn't use the grouper/symbol anchoring info. It does its own file:line parsing. This is another source of duplication.

18. Should I flag the "large union struct" issue? JSONFinding has accumulated many fields. The change adds two more. This is a real architecture issue but maybe MEDIUM. The comments are explicit about which stage owns which, so it's documented. But the type system doesn't enforce. I'll flag it as MEDIUM.

19. The debt README.md change adds a new LOW debt item. Should I flag it? The debt item itself is just documentation. But it describes a real code issue. I should flag the code issue, not the documentation. The code issue is the slog.Debug at justification.go:109. I'll flag it as LOW or MEDIUM. Since it means review.md format drift goes unnoticed at default log verbosity, this is an observability gap. MEDIUM seems right because it affects operational visibility. But the change itself records it as LOW. Hmm. The authors rated it LOW. But as an independent reviewer, I might rate it MEDIUM. However, I should be consistent with the severity rubric. The rubric says MEDIUM is "coupling or duplication that will rot if not addressed". LOW is "naming/structure choice that obscures intent". This is observability. I'd say LOW or MEDIUM. Since total miss of matching is a real operational issue, I'll say MEDIUM. But maybe the authors already correctly identified it as LOW. I don't want to be too harsh. Let me think: if review.md format changes and no justifications match, but the system silently continues, downstream consumers get no value from the feature. That's not critical or high, but it's more than naming. I'll go with MEDIUM.

Actually, the debt file itself is changed and references the code. But I need to reference the code line. So: MEDIUM|internal/reconcile/justification.go:109|...

20. Another thing: The `SourceReport` struct's `Path` field has `json:"path"` without omitempty. But if SourceReport is non-nil, Path should always be set. However, if for some reason Path were empty, it would serialize as `"path":""`. That's fine. Not a big issue.

21. The `JSONFinding` struct now has both `File`/`Line` and `SourceReport` with its own `Line`. There's potential confusion about which line is which. Good naming distinguishes them. Not a finding.

22. Let me re-read the gate.go change. The new comment says "Best-effort file:line match against every review.md under reviewDir/sources". But Discover respects `allow`. So this is the inconsistency.

23. Is there a circular knowledge issue? `internal/reconcile` now reads `review.md` which is produced by `internal/fanout`. Fanout consumes reconcile output. So there's a cycle in the data flow: fanout writes sources/ (findings.txt + review.md), reconcile reads both and writes reconciled/. That's fine, not circular knowledge. But the `reviewFileName` constant duplication is because of a potential import cycle. Let me verify: internal/fanout imports internal/reconcile? Or internal/reconcile imports internal/fanout? The comment says "fanout is a consumer, not a dependency, of this package" - so internal/fanout imports internal/reconcile. If internal/reconcile imported internal/fanout, that would be a cycle. So they duplicated the constant. This is a boundary issue. They could define the constant in a shared lower-level package, but they chose duplication. Flag as MEDIUM (duplication of responsibility / hidden dependency).

24. Is there an issue with `stampJustifications` not returning errors? It silently ignores I/O errors. This is consistent with "best-effort" but means a corrupted review.md (e.g., binary file named review.md) doesn't surface. Not a big architecture issue.

25. One more: The `anchorTier` function and `parseLineRange` reimplement file:line parsing. If the system has a shared package for parsing findings.txt references or file:line anchors, this duplicates it. I don't see evidence of such a package in the diff, but it's likely there (e.g., for parsing findings.txt rows). I'll flag this as duplication of responsibility / hidden dependency. Actually, without evidence, I might be overreaching. The diff doesn't show a shared parser. But the existence of `Finding` with File/Line fields and findings.txt parsing elsewhere suggests there might be shared logic. I should be careful not to speculate. But the custom parsing of `file:A-B` ranges is clearly a new parser. I'll flag it as MEDIUM: custom parsing couples reconcile to review.md reference format.

OK, let me select the strongest findings:

1. HIGH - allow parameter ignored for review.md narratives (gate.go:227 / justification.go:86)
2. MEDIUM - duplicated sources walk; inconsistent source model and no leaf-preference for review.md (justification.go:118)
3. MEDIUM - reviewFileName constant duplicated from fanout to avoid import cycle (justification.go:17)
4. MEDIUM - I/O errors swallowed in collectReviewNarratives (justification.go:120-145)
5. MEDIUM - hardcoded markdown parser in reconcile package (justification.go:280-360)
6. MEDIUM - match rate only logged at Debug, masking format drift (justification.go:109)
7. MEDIUM - JSONFinding union struct accumulates stage-specific fields (emit.go:96-130)

Wait, I need to limit findings to the most important ones. The instructions don't say a maximum, but I should be selective. Let me prioritize:

- HIGH: allow ignored (most serious contract violation)
- MEDIUM: duplicated source walk / inconsistent source model
- MEDIUM: reviewFileName duplication / import cycle workaround
- MEDIUM: swallowed I/O errors
- MEDIUM: Debug-only match rate logging
- MEDIUM: hardcoded markdown parser in reconcile
- MEDIUM: JSONFinding extensibility trap

That's 7 findings. Seems reasonable.

Now, exact file:line references. The instructions say FILE:LINE must fall within changed lines when grounding is active. So I need to reference lines within the diff hunks.

For gate.go, the new `stampJustifications(jf, reviewDir)` line is in the hunk. I need to estimate the actual line number in the new file. The diff shows the hunk starting at line 155 with context. The new line appears after `stampSymbolAnchors(jf, grouper)` and before `res.jsonFindings = jf`. In the context, `stampSymbolAnchors(jf, grouper)` is around line 219. The new block adds several comment lines and then `stampJustifications(jf, reviewDir)`. Let me estimate line 227. Actually, in the diff context, counting from line 155:
- Line 155: `// RunReconcile discovers...`
- ...
- Around line 219: `stampSymbolAnchors(jf, grouper)`
- Then new comment lines (6 lines) + blank line + `stampJustifications(jf, reviewDir)` = line 227.

For justification.go, it's a new file. The line numbers are as in the new file. I can use the line numbers from the diff hunk (which starts at line 1). So:
- reviewFileName constant: around line 17
- stampJustifications function signature: around line 86
- slog.Debug: around line 109
- collectReviewNarratives: around line 118
- anchorTier: around line 200
- extractSection etc.: around line 280

For emit.go, the new fields are in the struct. The hunk starts at line 53. The Justification/SourceReport fields are added near the end. Let me estimate: the struct starts around line 64. The new fields are after EvidenceExec. EvidenceExec appears around line 118. Justification around 120-130. SourceReport struct around 132-140.

Let me be more precise. The diff hunk for emit.go starts at line 53. The context lines show the struct. Counting:
- Line 64 (approx): `type JSONFinding struct {`
- ...
- EvidenceExec field around line 118
- Justification comment starts after, field around line 125
- SourceReport field around line 132
- SourceReport struct starts around line 135

For the finding about JSONFinding accumulating fields, I can reference the whole struct region emit.go:64-140. But I need one line. I'll reference emit.go:64 (the struct definition) or emit.go:96 (a representative field area). The most precise is emit.go:64.

Let me reconsider line numbers. Since the file is changed, the line numbers I provide will be checked against the changed region. If I'm slightly off, it might still be within the hunk. But I should be as accurate as possible.

Actually, for a new file (justification.go), the entire file is changed, so any line is fine. For emit.go and gate.go, I need to be within the hunk.

Let me count gate.go more carefully. The diff hunk header is `@@ -155,128 +155,136 @@`. So the hunk starts at line 155 in the old file and line 155 in the new file. The new file has 136 lines in this hunk vs old 128. I need to find the line number of `stampJustifications(jf, reviewDir)`.

Looking at the diff context, I'll count from line 155:
155: `// RunReconcile discovers sources under reviewDir/sources, runs the deterministic`
156: `// pipeline, and writes the artifacts to reviewDir/reconciled, returning the`
157: `// Result. allow restricts which immediate source children are read (empty = open`
158: `// discovery). It is the single engine entry the CLI and MCP both call. ctx is`
159: `// checked between the Discover, Reconcile, and Emit stages so a client cancel or`
160: `// server shutdown aborts the pipeline without emitting partial artifacts.`
161: `//`
162: `// Adjudication re-invocation: if reviewDir/reconciled/adjudication.json exists`
... (I'll skip ahead)
The code starts around line 170:
170: `func RunReconcile(ctx context.Context, reviewDir string, allow []string, opts Options) (Result, error) {`
171: `	if err := ctx.Err(); err != nil {`
... 
I'll count the lines in the hunk up to the stampJustifications call. This is tedious. Let me estimate it as line 227. The hunk has 136 lines. The stampJustifications call is roughly in the middle-to-later part. 227 is probably close.

Actually, the line number doesn't need to be exact to the character; it needs to be within the changed region. If I say gate.go:227 and the actual line is 226 or 228, it's probably still in the hunk. But if it's outside the hunk range (155-290), it would be discarded. 227 is well within 155-290. OK.

For emit.go, hunk starts at 53. The new fields are at the end of the struct. The hunk extends to 53 + number of context lines. The struct is large. Let me estimate Justification field at emit.go:121. The hunk probably goes to around line 140. 121 is within.

Actually, let me look at the diff again for emit.go. The hunk header is `@@ -53,84 +53,113 @@`. The old hunk has 84 lines, new has 113 lines. So new hunk covers lines 53-165. The Justification field is near the end. If the struct starts at line 64 and has ~80 lines, the new fields are around line 140-145. Let me say emit.go:121 for the JSONFinding struct finding is fine, but for the specific field addition, maybe emit.go:121 (Justification comment) or emit.go:132 (SourceReport field).

For the "accumulating fields" finding, referencing emit.go:64 (the struct start) is good. But is line 64 within the changed hunk? Yes, hunk starts at 53 and includes the struct definition.

Wait, I need to be careful. The changed region in emit.go is the addition of the new fields. The existing struct definition lines are context (unchanged). But the hunk includes them as context. The instructions say "Review only the changed regions" and "a finding whose FILE:LINE falls outside the changed lines will be discarded". For git-range reviews, "changed lines" typically means added/modified/deleted lines, not context lines. Hmm. But the struct start line is unchanged context. If I reference emit.go:64, it might be considered outside the changed lines.

To be safe, I should reference the added lines. For the JSONFinding extensibility trap, I can reference the new Justification/SourceReport fields at their added lines. That's definitely changed. Let me use emit.go:121 (Justification field comment start) or emit.go:132 (SourceReport field). Actually, the added lines include:
- Justification comment and field
- SourceReport comment and field  
- SourceReport type definition

So I can reference emit.go:121 or emit.go:132. The exact line numbers are estimates. Let me say emit.go:121 for the Justification field.

But wait, the structural issue is about the whole struct accumulating fields, not just the new ones. Referencing the new field is fine because it exemplifies the problem.

Let me list my final findings with file:line:

1. HIGH|internal/reconcile/gate.go:227|stampJustifications ignores allow source restriction|Thread allow into stampJustifications and skip review.md files outside allowed source children|boundary|45|RunReconcile passes allow to Discover but stampJustifications(jf, reviewDir) walks all review.md under sources/

2. MEDIUM|internal/reconcile/justification.go:118|Second independent sourcesDir walk duplicates Discover's source model|Accept already-discovered sources and their review.md paths instead of re-walking|coupling|60|collectReviewNarratives repeats filepath.WalkDir over sourcesSubdir

3. MEDIUM|internal/reconcile/justification.go:17|reviewFileName constant duplicated from fanout package|Move shared artifact filename constants to a lower-level package both can import|duplication|30|comment admits duplication to avoid import cycle with internal/fanout

4. MEDIUM|internal/reconcile/justification.go:120|I/O and parse errors in collectReviewNarratives are silently swallowed|Return or aggregate errors so unreadable review.md files are visible|error-handling|30|WalkDir err and ReadFile rerr return nil without logging

5. MEDIUM|internal/reconcile/justification.go:280|Markdown parsing logic (headings/list items/block extraction) hardcoded in reconcile package|Extract a review.md reader package so reconcile consumes parsed narratives|coupling|90|extractSection/isItemStart/isHeadingLine implement ad-hoc markdown parser in reconcile

6. MEDIUM|internal/reconcile/justification.go:109|Justification match rate logged only at Debug|Log at Info when narratives exist but matched==0 so format drift is visible|observability|20|slog.Debug("justifications stamped", ...) with no warning path

7. MEDIUM|internal/reconcile/emit.go:121|JSONFinding accumulates stage-specific fields without structural separation|Introduce per-stage payload types or interface/embedding so downstream-only fields cannot be set by reconcile|extensibility|120|Justification/SourceReport join ClusterMerged/ClusterID/FixWarning/EvidenceExec in one union struct

Wait, I want to double-check the line numbers. For justification.go, since it's a new file, I can be more confident:
- Line 17: reviewFileName constant (yes, in the const block)
- Line 86: stampJustifications signature (yes, `func stampJustifications(jf []JSONFinding, reviewDir string) {`)
- Line 109: slog.Debug line (yes, near end of stampJustifications)
- Line 118: collectReviewNarratives signature (yes, `func collectReviewNarratives(sourcesDir, reviewDir string) []reviewNarrative {`)
- Line 280: extractSection signature (yes, around there)

For emit.go, the exact line numbers are harder. Let me look at the diff again to estimate.

In emit.go hunk, the added lines start after the EvidenceExec field. The context shows:
```
 	EvidenceExec *EvidenceExec `json:"evidence_exec,omitempty"`
+	// Justification is the narrative context extracted at reconcile time from the
+	// originating source's review.md (Epic 18.2): ...
+	Justification string `json:"justification,omitempty"`
+	// SourceReport is the back-reference ...
+	SourceReport *SourceReport `json:"source_report,omitempty"`
+}
+
+// SourceReport is the back-reference ...
```

The EvidenceExec line is in context. The added lines are the comments and fields. To find the line number, I need to know where in the hunk this occurs. The hunk starts at line 53 and has 113 lines. The struct starts around line 64. Let me count roughly:
- Lines 53-63: const block (context)
- Line 64: type JSONFinding struct
- Lines 65-118: existing fields (~54 lines)
- Line 119: EvidenceExec field
- Line 120+: blank, then new comments
- Line 121: Justification comment start
- Line 126: Justification field
- Line 127-131: SourceReport comment
- Line 132: SourceReport field
- Line 133: }
- Line 135: SourceReport type

So emit.go:121 is a good reference for the Justification field addition. But I'm not 100% sure. If the actual line is different, as long as it's within the hunk (lines 53-165), it's fine. 121 is within.

Actually, I realize I could reference emit.go:119 (EvidenceExec field) but that's context, not changed. Better to reference the added lines.

For gate.go:227, I'm less confident. Let me try to count from the hunk header. Hunk starts at line 155. The code lines in the hunk:

155: // RunReconcile discovers...
156: // pipeline...
157: // Result. allow restricts...
