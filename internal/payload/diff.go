package payload

import (
	"bytes"
	"context"
	"fmt"
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

// gitRunner executes git argv against a fixed directory and context. The
// payload package wraps os/exec directly (there is no internal/git package).
type gitRunner struct {
	ctx context.Context
	dir string
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

// isBinary reports whether path is a binary file in the base..head diff.
// git numstat prints "-\t-\t<path>" for binary files.
func (g *gitRunner) isBinary(base, head, path string) bool {
	out, err := g.run("diff", "--numstat", base+".."+head, "--", path)
	if err != nil || out == "" {
		return false
	}
	first := strings.Split(out, "\n")[0]
	fields := strings.SplitN(first, "\t", 3)
	return len(fields) >= 2 && fields[0] == "-" && fields[1] == "-"
}

// functionContextFile returns the function-context diff for a single file.
// ok is false (no error) when git fails or yields zero hunks, signalling the
// caller to fall back to a plain context diff.
func (g *gitRunner) functionContextFile(base, head, path string) (out string, ok bool) {
	got, err := g.run("diff", "--function-context", base+".."+head, "--", path)
	if err != nil || strings.TrimSpace(got) == "" {
		return "", false
	}
	return got, true
}

// contextFile returns a plain -U10 context diff for a single file (the blocks
// fallback for no-brace languages and files where function-context fails).
func (g *gitRunner) contextFile(base, head, path string) (string, error) {
	return g.run("diff", "--unified=10", base+".."+head, "--", path)
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

// changedHeadRanges returns the head-side changed line ranges for path, parsed
// from a zero-context diff so each range maps to real head line numbers.
func (g *gitRunner) changedHeadRanges(base, head, path string) ([]lineRange, error) {
	out, err := g.run("diff", "--unified=0", base+".."+head, "--", path)
	if err != nil {
		return nil, err
	}
	var ranges []lineRange
	for _, line := range strings.Split(out, "\n") {
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
	return ranges, nil
}
