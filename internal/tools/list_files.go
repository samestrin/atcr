package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type listArgs struct {
	Dir string `json:"dir"`
}

// listFilesHandler returns a depth-capped, entry-capped recursive listing of
// absPath. Entries are prefixed "d " (directory) or "f " (file) and reported
// relative to the listed directory.
func listFilesHandler(ctx context.Context, d *Dispatcher, argsJSON json.RawMessage, absPath string) (ToolResult, error) {
	var a listArgs
	if err := json.Unmarshal(argsJSON, &a); err != nil {
		return ToolResult{}, toolErrf("list_files: invalid arguments: %v", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ToolResult{}, toolErrf("list_files: directory not found: %s", a.Dir)
		}
		return ToolResult{}, toolErrf("list_files: cannot stat %s: %v", a.Dir, err)
	}
	if !info.IsDir() {
		return ToolResult{}, toolErrf("list_files: dir is not a directory: %s", a.Dir)
	}

	w := &listWalker{
		ctx:      ctx,
		base:     absPath,
		maxDepth: d.limits.MaxListDepth,
		maxFiles: d.limits.MaxListFiles,
	}
	w.walk(absPath, 1)

	content := strings.Join(w.lines, "\n")
	switch {
	case w.entryTruncated:
		content += "\n[...more entries truncated...]"
	case w.depthTruncated:
		content += "\n[...depth cap reached...]"
	}
	return ToolResult{Content: content, Truncated: w.entryTruncated || w.depthTruncated, OriginalBytes: len(content)}, nil
}

type listWalker struct {
	ctx            context.Context
	base           string
	maxDepth       int
	maxFiles       int
	lines          []string
	entryTruncated bool
	depthTruncated bool
}

// walk lists dir at the given 1-based depth. It returns false to signal the
// caller to stop entirely (entry cap reached or context cancelled).
func (w *listWalker) walk(dir string, depth int) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true // skip unreadable directory, keep going elsewhere
	}
	for _, e := range entries {
		if w.ctx.Err() != nil {
			return false
		}
		if e.IsDir() && strings.EqualFold(e.Name(), ".git") {
			continue
		}
		if w.maxFiles > 0 && len(w.lines) >= w.maxFiles {
			w.entryTruncated = true
			return false
		}
		rel := relDisplay(w.base, filepath.Join(dir, e.Name()))
		if e.IsDir() {
			w.lines = append(w.lines, "d "+rel)
			if w.maxDepth > 0 && depth+1 > w.maxDepth {
				w.depthTruncated = true
				continue
			}
			if !w.walk(filepath.Join(dir, e.Name()), depth+1) {
				return false
			}
		} else {
			w.lines = append(w.lines, "f "+rel)
		}
	}
	return true
}
