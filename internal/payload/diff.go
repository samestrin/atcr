package payload

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/log"
)

// changeKind classifies a file's change between base and head.
type changeKind int

const (
	kindModified changeKind = iota
	kindAdded
	kindDeleted
	kindRenamed
)

// changedFile describes one file in the base..head range. Path is the
// head-side path (the new path for renames).
type changedFile struct {
	path    string
	oldPath string // populated for renames
	kind    changeKind
}

// pathspec returns the pathspec arguments for per-file git commands. Renames
// must include both sides: pathspec filtering happens before rename pairing,
// so limiting to the head path alone makes git render the file as a bare
// addition (full file as added lines).
func (f changedFile) pathspec() []string {
	if f.kind == kindRenamed {
		return []string{f.oldPath, f.path}
	}
	return []string{f.path}
}

// rangeState holds the whole-range cache fields for one base..head pair.
// All cache reads must go through gitRunner.forRange, which reconciles the
// key before returning a pointer — no accessor can read a cache field without
// first passing through that gate.
type rangeState struct {
	key        string
	files      []changedFile          // changed files (one --name-status -M)
	binary     map[string]bool        // head path -> binary (one --numstat -M)
	fc         map[string]string      // head path -> --function-context chunk
	plain      map[string]string      // head path -> --unified=10 chunk
	raw        map[string]string      // head path -> plain -M diff chunk
	zeroCtx    map[string]string      // head path -> --unified=0 chunk (raw)
	lineRanges map[string][]lineRange // head path -> head-side changed ranges

	// excludeSpec holds git pathspecs for the files removed by the ignore filter,
	// applied to every whole-range diff so git never emits their chunks. This
	// keeps the diff splitter's "every chunk maps to a changed file" invariant
	// intact: the filtered file list (files) and the diff output stay in lockstep.
	// Populated by changedFilesMemo; empty when nothing was ignored (zero
	// behavior change for the common case).
	excludeSpec []string
}

// pathspecArgs returns the trailing `-- . :(exclude)<path>...` args for whole-
// range diff commands, or nil when nothing was ignored. A positive `.` pathspec
// is required alongside the exclusions so git scopes them to the whole tree.
func (s *rangeState) pathspecArgs() []string {
	if len(s.excludeSpec) == 0 {
		return nil
	}
	return append([]string{"--", "."}, s.excludeSpec...)
}

// gitRunner executes git argv against a fixed directory and context. The
// payload package wraps os/exec directly (there is no internal/git package).
//
// The whole-range caches batch the per-file fan-out: each diff variant for a
// base..head range is computed once (one git process), split per file on
// column-0 `diff --git` boundaries, and served to every file from the cache.
// This keeps the per-file helpers' signatures intact (so their direct unit
// tests are unaffected) while collapsing O(N) git processes to O(1) per mode.
type gitRunner struct {
	ctx    context.Context
	dir    string
	logger *slog.Logger // nil → no-op discard logger; set to capture output without swapping global

	// execCount counts git subprocess invocations (every output call). It backs
	// the constant-process-count regression test; it is otherwise inert.
	execCount int

	// noIgnore disables the .gitignore/.atcrignore payload filter for this runner
	// (the --no-ignore opt-out). Default false → filtering active.
	noIgnore bool

	// ignore is the lazily-loaded repo-root ignore matcher (nil until first use).
	// ignoreReady guards the one-time load so a repo with no ignore files is not
	// re-stat'd on every range.
	ignore      *ignoreMatcher
	ignoreReady bool

	// state holds the whole-range caches for the current base..head pair.
	// Access only via forRange, which resets state when the range changes.
	state rangeState
}

// matcher returns the runner's repo-root ignore matcher, loading it once from
// g.dir. Returns nil when filtering is disabled (--no-ignore) so callers skip
// the partition entirely.
func (g *gitRunner) matcher() *ignoreMatcher {
	if g.noIgnore {
		return nil
	}
	if !g.ignoreReady {
		g.ignore = newIgnoreMatcher(g.dir, g.log())
		g.ignoreReady = true
	}
	return g.ignore
}

// run executes `git -C <dir> args...` and returns trimmed stdout. LC_ALL=C
// pins stderr to English for stable error matching; a cancelled context is
// surfaced as ctx.Err().
func (g *gitRunner) run(args ...string) (string, error) {
	out, err := g.output(args...)
	return strings.TrimSpace(string(out)), err
}

// output is like run but returns raw, untrimmed stdout — diff payloads must be
// shown to reviewers verbatim (no trimming or mutation).
func (g *gitRunner) output(args ...string) ([]byte, error) {
	// core.quotePath=false keeps non-ASCII paths unquoted so the path strings
	// parsed out of --name-status round-trip back into `git show`/`git diff`.
	g.execCount++
	full := append([]string{"-C", g.dir, "-c", "core.quotePath=false"}, args...)
	cmd := exec.CommandContext(g.ctx, "git", full...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANG=C")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		if ctxErr := g.ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("git %s cancelled: %w", strings.Join(args, " "), ctxErr)
		}
		return nil, fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return out.Bytes(), nil
}

// verifyRef validates that ref resolves to a commit. label is "base" or "head"
// so the error matches AC 06-01 (Error Scenarios 1 and 2). --end-of-options
// blocks option injection via refs beginning with '-'.
func (g *gitRunner) verifyRef(ref, label string) error {
	if _, err := g.run("rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}"); err != nil {
		return fmt.Errorf("failed to resolve %s ref '%s': unknown revision or path not in the working tree", label, ref)
	}
	return nil
}

// changedFiles lists the files changed in base..head with rename detection.
func (g *gitRunner) changedFiles(base, head string) ([]changedFile, error) {
	out, err := g.run("diff", "--name-status", "-M", base+".."+head)
	if err != nil {
		return nil, fmt.Errorf("git diff --name-status failed: %w", err)
	}
	if out == "" {
		return nil, nil
	}
	var files []changedFile
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		if status == "" {
			continue
		}
		switch status[0] {
		case 'R', 'C': // rename/copy: status, old, new
			if len(fields) < 3 {
				continue
			}
			files = append(files, changedFile{path: fields[2], oldPath: fields[1], kind: kindRenamed})
		case 'D':
			files = append(files, changedFile{path: fields[1], kind: kindDeleted})
		case 'A':
			files = append(files, changedFile{path: fields[1], kind: kindAdded})
		default: // M, T, ...
			files = append(files, changedFile{path: fields[1], kind: kindModified})
		}
	}
	return files, nil
}

// changedFilesMemo memoizes changedFiles so the splitter (which keys chunks
// against this list) and buildEntries share one --name-status -M process.
func (g *gitRunner) changedFilesMemo(base, head string) ([]changedFile, error) {
	s := g.forRange(base, head)
	if s.files != nil {
		return s.files, nil
	}
	files, err := g.changedFiles(base, head)
	if err != nil {
		return nil, err
	}
	files, s.excludeSpec = g.applyIgnore(files)
	if files == nil {
		files = []changedFile{} // non-nil so an empty range still memoizes
	}
	s.files = files
	return files, nil
}

// applyIgnore partitions files into the kept set (returned) and the excluded
// set, returning git pathspecs for the excluded paths. Each excluded file is
// logged at slog debug (AC #3). When the matcher is inactive (no ignore files,
// or --no-ignore) the input is returned unchanged with a nil exclude spec, so
// the common case pays only one map/nil check.
func (g *gitRunner) applyIgnore(files []changedFile) (kept []changedFile, exclude []string) {
	m := g.matcher()
	if !m.active() {
		return files, nil
	}
	kept = make([]changedFile, 0, len(files))
	for _, f := range files {
		if !m.match(f.path) {
			kept = append(kept, f)
			continue
		}
		g.log().Debug("payload: skipping ignored file", "file", f.path, "kind", f.kind)
		// `literal` magic is mandatory: without it git treats the path as a glob,
		// so an ignored filename containing pathspec metacharacters ([ * ?) would
		// also exclude unrelated changed files (e.g. :(exclude)a[b].go matches
		// ab.go), silently dropping a real file or leaving an unattributed chunk
		// that hard-errors the splitter.
		exclude = append(exclude, ":(exclude,literal)"+f.path)
		// A rename whose head path is ignored: exclude the old path too so git
		// drops the rename pair entirely rather than re-rendering it as an add.
		if f.kind == kindRenamed {
			exclude = append(exclude, ":(exclude,literal)"+f.oldPath)
		}
	}
	return kept, exclude
}

// headPathSet returns the set of head-side paths in base..head, the authoritative
// key list the diff splitter matches chunks against.
func (g *gitRunner) headPathSet(base, head string) (map[string]bool, error) {
	files, err := g.changedFilesMemo(base, head)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(files))
	for _, f := range files {
		set[f.path] = true
	}
	return set, nil
}

// headPathOf returns the head-side path from a pathspec. pathspec() yields
// [oldPath, path] for renames and [path] otherwise, so the head path is always
// the last element. The whole-range caches are keyed by head path.
func headPathOf(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[len(paths)-1]
}

// log returns the runner's logger, falling back to a no-op discard logger when
// none is injected (mirrors internal/mcp/handlers.go's nil-safe guard). This
// keeps the single-sink contract: production callers inject the context logger
// via BuildEntries/ChangedFileCount, and direct gitRunner{} construction in
// tests never reaches the global slog default logger. Callers use g.log().Warn(...).
func (g *gitRunner) log() *slog.Logger {
	if g.logger != nil {
		return g.logger
	}
	return log.Discard()
}

// forRange reconciles the cached state against base..head and returns a
// pointer to it. If the range changed, state is replaced with a zero
// rangeState keyed to the new range, clearing all cache fields atomically.
// Every cache accessor must call forRange rather than reading state directly;
// this makes reconciliation structurally unavoidable rather than a per-method
// convention.
func (g *gitRunner) forRange(base, head string) *rangeState {
	if key := base + ".." + head; g.state.key != key {
		g.state = rangeState{key: key}
	}
	return &g.state
}

// numstatNewPath reconstructs the head-side (new) path from a --numstat path
// field, which renders renames as "old => new" or "pre{old => new}post".
func numstatNewPath(field string) string {
	k := strings.Index(field, " => ")
	if k < 0 {
		return field
	}
	// Search for the enclosing braces by anchoring on the arrow, not the
	// start of the field. A parent-directory name may contain '{', which
	// would cause a naive first-brace scan to mis-key the rename segment.
	if i := strings.LastIndexByte(field[:k], '{'); i >= 0 {
		if j := strings.IndexByte(field[k:], '}'); j >= 0 {
			j += k
			return field[:i] + field[k+len(" => "):j] + field[j+1:]
		}
	}
	return field[k+len(" => "):]
}

// chunkKey returns the head path a `diff --git` chunk belongs to by matching the
// chunk's FIRST line — `diff --git a/<old> b/<new>` — against the authoritative
// set of head paths from changedFiles. Only the header line is inspected, so
// diff-body content that mimics a file header (an added line `++ b/x` renders as
// `+++ b/x`) can never mis-key the chunk; the longest matching head path wins so
// a nested path is not shadowed by a shorter suffix, and matching against the
// known set sidesteps git's space-path quirks (trailing tabs on ---/+++,
// ambiguous spaces). Binary/mode-only chunks key the same way.
func chunkKey(chunk string, heads map[string]bool) string {
	first := chunk
	if nl := strings.IndexByte(chunk, '\n'); nl >= 0 {
		first = chunk[:nl]
	}
	const sep = " b/"
	// Fast path: extract everything after the last ' b/' token and do a
	// single map lookup. Correct for the common case where paths do not
	// contain ' b/' as a substring; a miss falls through to the O(N)
	// longest-suffix scan which handles embedded ' b/' and space-heavy names.
	if i := strings.LastIndex(first, sep); i >= 0 {
		if candidate := first[i+len(sep):]; heads[candidate] {
			return candidate
		}
	}
	best := ""
	for h := range heads {
		if len(h) > len(best) && strings.HasSuffix(first, sep+h) {
			best = h
		}
	}
	return best
}

// splitDiffByFile splits a whole-range git diff into per-file chunks on
// column-0 `diff --git ` boundaries and keys each against the known head-path
// set. A per-file chunk is byte-identical to the output of the same diff run for
// that path alone, because git computes per-file patches and concatenates them —
// this is what lets the verbatim-body contract survive the batching.
func splitDiffByFile(diff string, heads map[string]bool) (map[string]string, error) {
	out := make(map[string]string)
	if diff == "" {
		return out, nil
	}
	const marker = "diff --git "
	var starts []int
	if strings.HasPrefix(diff, marker) {
		starts = append(starts, 0)
	}
	for i := 0; i+1 < len(diff); i++ {
		if diff[i] == '\n' && strings.HasPrefix(diff[i+1:], marker) {
			starts = append(starts, i+1)
		}
	}
	for k, s := range starts {
		end := len(diff)
		if k+1 < len(starts) {
			end = starts[k+1]
		}
		chunk := diff[s:end]
		if key := chunkKey(chunk, heads); key != "" {
			out[key] = chunk
		} else {
			header := chunk
			if nl := strings.IndexByte(chunk, '\n'); nl >= 0 {
				header = chunk[:nl]
			}
			return nil, fmt.Errorf("diff splitter: chunk not attributed to any changed file: %s", header)
		}
	}
	return out, nil
}

// diffChunks runs one whole-range `git diff <opts> -M base..head` and splits it
// per file, verbatim (raw bytes — diff payloads ship to reviewers as-is), keyed
// against the changed-file list.
func (g *gitRunner) diffChunks(base, head string, opts ...string) (map[string]string, error) {
	heads, err := g.headPathSet(base, head)
	if err != nil {
		return nil, err
	}
	// headPathSet ran changedFilesMemo, so s.excludeSpec is populated: excluding
	// the ignored paths from the diff keeps git's chunk output in lockstep with
	// the filtered head-path set the splitter matches against.
	s := g.forRange(base, head)
	args := append([]string{"diff"}, opts...)
	args = append(args, "-M", base+".."+head)
	args = append(args, s.pathspecArgs()...)
	out, err := g.output(args...)
	if err != nil {
		return nil, err
	}
	return splitDiffByFile(string(out), heads)
}

// binarySet returns the set of head paths that are binary in base..head, from
// one whole-range `--numstat -M`. git numstat prints "-\t-\t<path>" for binary
// files. git diff exits zero whether or not differences exist, so any error is
// fatal (bad repo, killed process, cancelled context) and propagated; an empty
// diff yields an empty (non-nil) set. The result is memoized so the N per-file
// binary checks collapse to a single git process.
func (g *gitRunner) binarySet(base, head string) (map[string]bool, error) {
	// Ensure the ignore filter has run so s.excludeSpec is populated before the
	// numstat diff — otherwise ignored binaries would leak into the set.
	if _, err := g.changedFilesMemo(base, head); err != nil {
		return nil, err
	}
	s := g.forRange(base, head)
	if s.binary != nil {
		return s.binary, nil
	}
	args := append([]string{"diff", "--numstat", "-M", base + ".." + head}, s.pathspecArgs()...)
	out, err := g.run(args...)
	if err != nil {
		return nil, fmt.Errorf("git diff --numstat failed: %w", err)
	}
	set := make(map[string]bool)
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			fields := strings.SplitN(line, "\t", 3)
			if len(fields) >= 3 && fields[0] == "-" && fields[1] == "-" {
				set[numstatNewPath(fields[2])] = true
			}
		}
	}
	s.binary = set
	return set, nil
}

// fcChunks / plainChunks / rawChunks memoize the whole-range function-context,
// -U10, and plain -M diffs respectively, each split per head path.
func (g *gitRunner) fcChunks(base, head string) (map[string]string, error) {
	s := g.forRange(base, head)
	if s.fc != nil {
		return s.fc, nil
	}
	m, err := g.diffChunks(base, head, "--function-context")
	if err != nil {
		return nil, err
	}
	s.fc = m
	return m, nil
}

func (g *gitRunner) plainChunks(base, head string) (map[string]string, error) {
	s := g.forRange(base, head)
	if s.plain != nil {
		return s.plain, nil
	}
	m, err := g.diffChunks(base, head, "--unified=10")
	if err != nil {
		return nil, err
	}
	s.plain = m
	return m, nil
}

func (g *gitRunner) rawChunks(base, head string) (map[string]string, error) {
	s := g.forRange(base, head)
	if s.raw != nil {
		return s.raw, nil
	}
	m, err := g.diffChunks(base, head)
	if err != nil {
		return nil, err
	}
	s.raw = m
	return m, nil
}

// isBinary reports whether path is binary in base..head, served from the
// memoized whole-range numstat set keyed by head path. pathspec() puts the head
// path last for renames, so a binary rename is detected by its new path without
// risking a collision with the (now absent) old path.
func (g *gitRunner) isBinary(base, head string, paths ...string) (bool, error) {
	set, err := g.binarySet(base, head)
	if err != nil {
		return false, err
	}
	return set[headPathOf(paths)], nil
}

// functionContextFile returns the function-context diff for a single file,
// verbatim (raw bytes, no trimming — diff payloads ship to reviewers as-is),
// served from the memoized whole-range split. ok is false with a nil error when
// the file has no function-context chunk (zero hunks), signalling the caller to
// fall back to a plain context diff; a git failure is fatal and propagated
// rather than masked as a fallback (TD-010).
func (g *gitRunner) functionContextFile(base, head string, paths ...string) (out string, ok bool, err error) {
	chunks, gerr := g.fcChunks(base, head)
	if gerr != nil {
		return "", false, fmt.Errorf("git diff --function-context failed: %w", gerr)
	}
	chunk, present := chunks[headPathOf(paths)]
	if !present || strings.TrimSpace(chunk) == "" {
		return "", false, nil
	}
	return chunk, true, nil
}

// contextFile returns a plain -U10 context diff for a single file, verbatim
// (the blocks fallback for no-brace languages and files where function-context
// fails), served from the memoized whole-range split.
func (g *gitRunner) contextFile(base, head string, paths ...string) (string, error) {
	chunks, err := g.plainChunks(base, head)
	if err != nil {
		return "", err
	}
	return chunks[headPathOf(paths)], nil
}

// headContent returns the full head-version content of path.
func (g *gitRunner) headContent(head, path string) (string, error) {
	out, err := g.output("show", head+":"+path)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// hunkHeaderRe captures the head-side start and length from a unified-diff
// hunk header: `@@ -a,b +c,d @@`.
var hunkHeaderRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// lineRange is an inclusive 1-based line span in the head file.
type lineRange struct{ start, end int }

// parseHeadRanges parses the head-side changed line ranges from a zero-context
// diff chunk so each range maps to real head line numbers.
func parseHeadRanges(chunk string) []lineRange {
	var ranges []lineRange
	for _, line := range strings.Split(chunk, "\n") {
		m := hunkHeaderRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		length := 1
		if m[2] != "" {
			length, _ = strconv.Atoi(m[2])
		}
		if length == 0 {
			continue // pure deletion: no head lines to mark
		}
		ranges = append(ranges, lineRange{start: start, end: start + length - 1})
	}
	return ranges
}

// zeroCtxChunks memoizes the whole-range zero-context (--unified=0) diff split
// per head path (raw chunks). Both rangeChunks (files-mode line-range parse) and
// changedLines (grounding changed-text parse) consume this one process, so the
// zero-context diff runs once per range across payload building and grounding.
func (g *gitRunner) zeroCtxChunks(base, head string) (map[string]string, error) {
	s := g.forRange(base, head)
	if s.zeroCtx != nil {
		return s.zeroCtx, nil
	}
	m, err := g.diffChunks(base, head, "--unified=0")
	if err != nil {
		return nil, err
	}
	s.zeroCtx = m
	return m, nil
}

// rangeChunks memoizes the whole-range zero-context diff, split per head path
// and parsed into changed line ranges, so the N per-file range queries collapse
// to a single git process. It reuses the memoized raw zero-context chunks so the
// grounding builder shares the same --unified=0 process.
func (g *gitRunner) rangeChunks(base, head string) (map[string][]lineRange, error) {
	s := g.forRange(base, head)
	if s.lineRanges != nil {
		return s.lineRanges, nil
	}
	chunks, err := g.zeroCtxChunks(base, head)
	if err != nil {
		return nil, err
	}
	m := make(map[string][]lineRange, len(chunks))
	for path, chunk := range chunks {
		m[path] = parseHeadRanges(chunk)
	}
	s.lineRanges = m
	return m, nil
}

// changedHeadRanges returns the head-side changed line ranges for path, served
// from the memoized whole-range zero-context split.
func (g *gitRunner) changedHeadRanges(base, head string, paths ...string) ([]lineRange, error) {
	m, err := g.rangeChunks(base, head)
	if err != nil {
		return nil, err
	}
	return m[headPathOf(paths)], nil
}
