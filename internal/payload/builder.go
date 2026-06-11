package payload

import (
	"context"
	"fmt"
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
	changedStartFmt  = ">>> CHANGED LINES %d-%d"
	changedEnd       = "<<< END CHANGED"
	binaryMarkerFmt  = "[binary file changed: %s]"
	deletedMarkerFmt = "[deleted file: %s]"
	fileHeaderFmt    = "=== FILE: %s ==="
	renamedHeaderFmt = "=== FILE: %s (renamed from %s) ==="
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

// BuildDiff returns the unified diff of base..head, verbatim (no trimming).
func BuildDiff(ctx context.Context, repo, base, head string) (string, error) {
	g := &gitRunner{ctx: ctx, dir: repo}
	if err := validateRange(g, base, head); err != nil {
		return "", err
	}
	out, err := g.output("diff", base+".."+head)
	if err != nil {
		return "", fmt.Errorf("git diff failed: %w", err)
	}
	return string(out), nil
}

// BuildBlocks returns changed hunks expanded to enclosing functions via
// git --function-context, per file. Files where function-context fails or
// yields zero hunks fall back to a plain -U10 context diff; binary files are
// excluded and represented by a one-line marker.
func BuildBlocks(ctx context.Context, repo, base, head string) (string, error) {
	g := &gitRunner{ctx: ctx, dir: repo}
	if err := validateRange(g, base, head); err != nil {
		return "", err
	}
	files, err := g.changedFiles(base, head)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, f := range files {
		if g.isBinary(base, head, f.path) {
			fmt.Fprintf(&b, binaryMarkerFmt+"\n", f.path)
			continue
		}
		out, ok := g.functionContextFile(base, head, f.path)
		if !ok {
			out, err = g.contextFile(base, head, f.path)
			if err != nil {
				return "", fmt.Errorf("git diff failed: %w", err)
			}
		}
		b.WriteString(out)
		if out != "" && !strings.HasSuffix(out, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

// BuildFiles returns the full head-version content of each changed file with
// changed regions delimited by sentinel lines. Deleted files become a
// [deleted file: <path>] marker; binary files a [binary file changed: <path>]
// marker; renamed files appear under the new path with the rename noted.
func BuildFiles(ctx context.Context, repo, base, head string) (string, error) {
	g := &gitRunner{ctx: ctx, dir: repo}
	if err := validateRange(g, base, head); err != nil {
		return "", err
	}
	files, err := g.changedFiles(base, head)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, f := range files {
		if f.kind == kindDeleted {
			fmt.Fprintf(&b, deletedMarkerFmt+"\n", f.path)
			continue
		}
		if g.isBinary(base, head, f.path) {
			fmt.Fprintf(&b, binaryMarkerFmt+"\n", f.path)
			continue
		}
		if f.kind == kindRenamed {
			fmt.Fprintf(&b, renamedHeaderFmt+"\n", f.path, f.oldPath)
		} else {
			fmt.Fprintf(&b, fileHeaderFmt+"\n", f.path)
		}
		content, err := g.headContent(head, f.path)
		if err != nil {
			return "", fmt.Errorf("reading head content of %s: %w", f.path, err)
		}
		ranges, err := g.changedHeadRanges(base, head, f.path)
		if err != nil {
			return "", err
		}
		b.WriteString(renderWithSentinels(content, ranges))
		b.WriteByte('\n')
	}
	return b.String(), nil
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
