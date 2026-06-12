package payload

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// PayloadMode is the typed enum of reviewer-input modes. Values are lowercase;
// no case normalization is performed (AC 06-02 Edge Case 5).
type PayloadMode string

const (
	ModeDiff   PayloadMode = "diff"
	ModeBlocks PayloadMode = "blocks"
	ModeFiles  PayloadMode = "files"
)

// changed-region sentinel lines for files mode (language-agnostic, not
// comment-prefixed) and the binary/deleted file markers.
const (
	changedStartPrefix = ">>> CHANGED LINES"
	changedStartFmt    = changedStartPrefix + " %d-%d"
	changedEnd         = "<<< END CHANGED"
	binaryMarkerFmt    = "[binary file changed: %s]"
	deletedMarkerFmt   = "[deleted file: %s]"
	fileHeaderFmt      = "=== FILE: %s ==="
	renamedHeaderFmt   = "=== FILE: %s (renamed from %s) ==="
)

// Build dispatches to the builder for mode. An unknown mode is a hard error
// before any git work (AC 06-01 Error Scenario 3).
func Build(ctx context.Context, mode PayloadMode, repo, base, head string) (string, error) {
	switch mode {
	case ModeDiff:
		return BuildDiff(ctx, repo, base, head)
	case ModeBlocks:
		return BuildBlocks(ctx, repo, base, head)
	case ModeFiles:
		return BuildFiles(ctx, repo, base, head)
	default:
		return "", fmt.Errorf("unknown payload mode '%s': must be one of diff, blocks, files", mode)
	}
}

// validateRange verifies both refs before any builder runs.
func validateRange(g *gitRunner, base, head string) error {
	if err := g.verifyRef(base, "base"); err != nil {
		return err
	}
	return g.verifyRef(head, "head")
}

// BuildDiff returns the per-file unified diffs of base..head joined in
// changed-file order — byte-identical to the entries path (binary files
// appear as one-line markers, not raw binary-diff lines) so flat and
// persisted payload artifacts never diverge.
func BuildDiff(ctx context.Context, repo, base, head string) (string, error) {
	return joinEntries(BuildEntries(ctx, ModeDiff, repo, base, head))
}

// BuildBlocks returns changed hunks expanded to enclosing functions via
// git --function-context, per file. Files where function-context fails or
// yields zero hunks fall back to a plain -U10 context diff; binary files are
// excluded and represented by a one-line marker.
func BuildBlocks(ctx context.Context, repo, base, head string) (string, error) {
	return joinEntries(BuildEntries(ctx, ModeBlocks, repo, base, head))
}

// BuildFiles returns the full head-version content of each changed file with
// changed regions delimited by sentinel lines. Deleted files become a
// [deleted file: <path>] marker; binary files a [binary file changed: <path>]
// marker; renamed files appear under the new path with the rename noted.
func BuildFiles(ctx context.Context, repo, base, head string) (string, error) {
	return joinEntries(BuildEntries(ctx, ModeFiles, repo, base, head))
}

// BuildEntries returns the per-file payload contributions for mode. This is the
// bridge between the builders and the byte-budget pass: callers feed these
// entries to ApplyByteBudget, derive the changed-file count from len(entries),
// and record per-file truncation in status.json. The flat Build* entry points
// all join these entries, so both forms are byte-identical by construction.
func BuildEntries(ctx context.Context, mode PayloadMode, repo, base, head string) ([]FileEntry, error) {
	if mode != ModeDiff && mode != ModeBlocks && mode != ModeFiles {
		return nil, fmt.Errorf("unknown payload mode '%s': must be one of diff, blocks, files", mode)
	}
	g := &gitRunner{ctx: ctx, dir: repo}
	return g.buildEntries(mode, base, head)
}

// buildEntries is the runner-bound core of BuildEntries, split out so white-box
// tests can observe g.execCount and assert the per-mode git-process count is
// independent of the changed-file count.
func (g *gitRunner) buildEntries(mode PayloadMode, base, head string) ([]FileEntry, error) {
	if err := validateRange(g, base, head); err != nil {
		return nil, err
	}
	files, err := g.changedFiles(base, head)
	if err != nil {
		return nil, err
	}
	entries := make([]FileEntry, 0, len(files))
	for _, f := range files {
		body, err := g.fileBody(mode, base, head, f)
		if err != nil {
			return nil, err
		}
		entries = append(entries, FileEntry{Path: f.path, Size: int64(len(body)), Body: body})
	}
	return entries, nil
}

// ChangedFileCount returns the number of files changed in base..head from a
// single `git diff --name-status` call — the cheap pre-flight counterpart to
// BuildEntries, which materializes every per-file diff body just to be counted.
// Rename detection (-M) matches changedFiles, so the count equals
// len(BuildEntries(ModeDiff, ...)) for added, deleted, and renamed files.
func ChangedFileCount(ctx context.Context, repo, base, head string) (int, error) {
	g := &gitRunner{ctx: ctx, dir: repo}
	if err := validateRange(g, base, head); err != nil {
		return 0, err
	}
	files, err := g.changedFiles(base, head)
	if err != nil {
		return 0, err
	}
	return len(files), nil
}

// fileBody renders one changed file's contribution for mode, including the
// trailing newline so concatenating bodies reproduces the flat payload.
func (g *gitRunner) fileBody(mode PayloadMode, base, head string, f changedFile) (string, error) {
	switch mode {
	case ModeDiff:
		bin, err := g.isBinary(base, head, f.pathspec()...)
		if err != nil {
			return "", err
		}
		if bin {
			return fmt.Sprintf(binaryMarkerFmt+"\n", f.path), nil
		}
		out, err := g.output(append([]string{"diff", "-M", base + ".." + head, "--"}, f.pathspec()...)...)
		if err != nil {
			return "", fmt.Errorf("git diff failed: %w", err)
		}
		return ensureTrailingNewline(string(out)), nil

	case ModeBlocks:
		bin, err := g.isBinary(base, head, f.pathspec()...)
		if err != nil {
			return "", err
		}
		if bin {
			return fmt.Sprintf(binaryMarkerFmt+"\n", f.path), nil
		}
		out, ok, err := g.functionContextFile(base, head, f.pathspec()...)
		if err != nil {
			return "", err
		}
		if !ok {
			// Defensive-measure contract: every degradation is recorded. Without
			// this an operator cannot tell which files got function context.
			slog.Warn("blocks mode: function context unavailable, falling back to plain context diff", "file", f.path)
			if out, err = g.contextFile(base, head, f.pathspec()...); err != nil {
				return "", fmt.Errorf("git diff failed: %w", err)
			}
		}
		return ensureTrailingNewline(out), nil

	case ModeFiles:
		if f.kind == kindDeleted {
			return fmt.Sprintf(deletedMarkerFmt+"\n", f.path), nil
		}
		bin, err := g.isBinary(base, head, f.pathspec()...)
		if err != nil {
			return "", err
		}
		if bin {
			return fmt.Sprintf(binaryMarkerFmt+"\n", f.path), nil
		}
		var b strings.Builder
		if f.kind == kindRenamed {
			fmt.Fprintf(&b, renamedHeaderFmt+"\n", f.path, f.oldPath)
		} else {
			fmt.Fprintf(&b, fileHeaderFmt+"\n", f.path)
		}
		content, err := g.headContent(head, f.path)
		if err != nil {
			return "", fmt.Errorf("reading head content of %s: %w", f.path, err)
		}
		ranges, err := g.changedHeadRanges(base, head, f.pathspec()...)
		if err != nil {
			return "", err
		}
		b.WriteString(renderWithSentinels(content, ranges))
		b.WriteByte('\n')
		return b.String(), nil

	default:
		return "", fmt.Errorf("unknown payload mode '%s': must be one of diff, blocks, files", mode)
	}
}

// joinEntries concatenates entry bodies into the flat payload string, threading
// any builder error through.
func joinEntries(entries []FileEntry, err error) (string, error) {
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Body)
	}
	return b.String(), nil
}

// ensureTrailingNewline appends a newline to non-empty content that lacks one,
// so per-file bodies concatenate without running together.
func ensureTrailingNewline(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}

// renderWithSentinels emits content with each changed line range wrapped in
// the changed-region sentinels. Line numbering is 1-based against the head file.
func renderWithSentinels(content string, ranges []lineRange) string {
	// Preserve newline fidelity: a file without a trailing newline must not gain
	// one in the payload.
	hadTrailingNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(content, "\n")
	if hadTrailingNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	for i, line := range lines {
		ln := i + 1
		for _, r := range ranges {
			if r.start == ln {
				fmt.Fprintf(&b, changedStartFmt+"\n", r.start, r.end)
			}
		}
		// Neutralize content lines that would spoof a sentinel: a head file
		// containing a literal sentinel line could otherwise mislead consumers
		// about which regions changed.
		if strings.HasPrefix(line, changedStartPrefix) || strings.HasPrefix(line, changedEnd) {
			b.WriteString("> ")
		}
		b.WriteString(line)
		if i < len(lines)-1 || hadTrailingNewline {
			b.WriteByte('\n')
		}
		for _, r := range ranges {
			if r.end == ln {
				b.WriteString(changedEnd + "\n")
			}
		}
	}
	return b.String()
}
