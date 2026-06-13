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

// gitRunner executes git argv against a fixed directory and context. The
// payload package wraps os/exec directly (there is no internal/git package).
//
// The whole-range caches batch the per-file fan-out: each diff variant for a
// base..head range is computed once (one git process), split per file on
// column-0 `diff --git` boundaries, and served to every file from the cache.
// This keeps the per-file helpers' signatures intact (so their direct unit
// tests are unaffected) while collapsing O(N) git processes to O(1) per mode.
type gitRunner struct {
	ctx context.Context
	dir string

	// execCount counts git subprocess invocations (every output call). It backs
	// the constant-process-count regression test; it is otherwise inert.
	execCount int

	// cacheKey is the "base..head" the caches below were computed for; a
	// mismatched range resets them. A gitRunner's range is constant in practice
	// (one per Build* call), so this only guards reuse in white-box tests.
	cacheKey   string
	filesCache []changedFile          // changed files (one --name-status -M)
	binCache   map[string]bool        // head path -> binary (one --numstat -M)
	fcCache    map[string]string      // head path -> --function-context chunk
	plainCache map[string]string      // head path -> --unified=10 chunk
	rawCache   map[string]string      // head path -> plain -M diff chunk
	rangeCache map[string][]lineRange // head path -> head-side changed ranges
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
	g.ensureRange(base, head)
	if g.filesCache != nil {
		return g.filesCache, nil
	}
	files, err := g.changedFiles(base, head)
	if err != nil {
		return nil, err
	}
	if files == nil {
		files = []changedFile{} // non-nil so an empty range still memoizes
	}
	g.filesCache = files
	return files, nil
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

// ensureRange resets the whole-range caches if base..head changed since they
// were computed. In production a gitRunner serves one range for its whole life,
// so this is a no-op after the first call; it only matters for white-box tests
// that reuse a runner across ranges.
func (g *gitRunner) ensureRange(base, head string) {
	if key := base + ".." + head; g.cacheKey != key {
		g.cacheKey = key
		g.filesCache = nil
		g.binCache, g.fcCache, g.plainCache, g.rawCache, g.rangeCache = nil, nil, nil, nil, nil
	}
}

// numstatNewPath reconstructs the head-side (new) path from a --numstat path
// field, which renders renames as "old => new" or "pre{old => new}post".
func numstatNewPath(field string) string {
	if i := strings.IndexByte(field, '{'); i >= 0 {
		if j := strings.IndexByte(field, '}'); j > i {
			inner := field[i+1 : j]
			if k := strings.Index(inner, " => "); k >= 0 {
				return field[:i] + inner[k+len(" => "):] + field[j+1:]
			}
		}
	}
	if k := strings.Index(field, " => "); k >= 0 {
		return field[k+len(" => "):]
	}
	return field
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
	best := ""
	for h := range heads {
		if len(h) > len(best) && strings.HasSuffix(first, " b/"+h) {
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
func splitDiffByFile(diff string, heads map[string]bool) map[string]string {
	out := make(map[string]string)
	if diff == "" {
		return out
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
			// A chunk that matches no known head path means the splitter could
			// not attribute it — an unforeseen git output form. Record it rather
			// than drop it silently, so a file rendering with an empty body is
			// traceable instead of invisible (same contract as the blocks
			// function-context fallback record).
			header := chunk
			if nl := strings.IndexByte(chunk, '\n'); nl >= 0 {
				header = chunk[:nl]
			}
			slog.Warn("diff splitter: chunk not attributed to any changed file", "header", header)
		}
	}
	return out
}

// diffChunks runs one whole-range `git diff <opts> -M base..head` and splits it
// per file, verbatim (raw bytes — diff payloads ship to reviewers as-is), keyed
// against the changed-file list.
func (g *gitRunner) diffChunks(base, head string, opts ...string) (map[string]string, error) {
	heads, err := g.headPathSet(base, head)
	if err != nil {
		return nil, err
	}
	args := append([]string{"diff"}, opts...)
	args = append(args, "-M", base+".."+head)
	out, err := g.output(args...)
	if err != nil {
		return nil, err
	}
	return splitDiffByFile(string(out), heads), nil
}

// binarySet returns the set of head paths that are binary in base..head, from
// one whole-range `--numstat -M`. git numstat prints "-\t-\t<path>" for binary
// files. git diff exits zero whether or not differences exist, so any error is
// fatal (bad repo, killed process, cancelled context) and propagated; an empty
// diff yields an empty (non-nil) set. The result is memoized so the N per-file
// binary checks collapse to a single git process.
func (g *gitRunner) binarySet(base, head string) (map[string]bool, error) {
	g.ensureRange(base, head)
	if g.binCache != nil {
		return g.binCache, nil
	}
	out, err := g.run("diff", "--numstat", "-M", base+".."+head)
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
	g.binCache = set
	return set, nil
}

// fcChunks / plainChunks / rawChunks memoize the whole-range function-context,
// -U10, and plain -M diffs respectively, each split per head path.
func (g *gitRunner) fcChunks(base, head string) (map[string]string, error) {
	g.ensureRange(base, head)
	if g.fcCache != nil {
		return g.fcCache, nil
	}
	m, err := g.diffChunks(base, head, "--function-context")
	if err != nil {
		return nil, err
	}
	g.fcCache = m
	return m, nil
}

func (g *gitRunner) plainChunks(base, head string) (map[string]string, error) {
	g.ensureRange(base, head)
	if g.plainCache != nil {
		return g.plainCache, nil
	}
	m, err := g.diffChunks(base, head, "--unified=10")
	if err != nil {
		return nil, err
	}
	g.plainCache = m
	return m, nil
}

func (g *gitRunner) rawChunks(base, head string) (map[string]string, error) {
	g.ensureRange(base, head)
	if g.rawCache != nil {
		return g.rawCache, nil
	}
	m, err := g.diffChunks(base, head)
	if err != nil {
		return nil, err
	}
	g.rawCache = m
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

// rangeChunks memoizes the whole-range zero-context diff, split per head path
// and parsed into changed line ranges, so the N per-file range queries collapse
// to a single git process.
func (g *gitRunner) rangeChunks(base, head string) (map[string][]lineRange, error) {
	g.ensureRange(base, head)
	if g.rangeCache != nil {
		return g.rangeCache, nil
	}
	chunks, err := g.diffChunks(base, head, "--unified=0")
	if err != nil {
		return nil, err
	}
	m := make(map[string][]lineRange, len(chunks))
	for path, chunk := range chunks {
		m[path] = parseHeadRanges(chunk)
	}
	g.rangeCache = m
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
