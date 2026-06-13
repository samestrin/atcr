package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type grepArgs struct {
	Pattern string `json:"pattern"`
	Glob    string `json:"glob"`
	Dir     string `json:"dir"`
}

// grepHandler searches regular files under absPath for a Go-regexp pattern,
// optionally filtered by a filename glob. Match lines are reported relative to
// the snapshot root. The match list is capped; per-line length is capped.
func grepHandler(ctx context.Context, d *Dispatcher, argsJSON json.RawMessage, absPath string) (ToolResult, error) {
	var a grepArgs
	if err := json.Unmarshal(argsJSON, &a); err != nil {
		return ToolResult{}, toolErrf("grep: invalid arguments: %v", err)
	}
	if a.Pattern == "" {
		return ToolResult{}, toolErrf("grep: pattern cannot be empty")
	}
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return ToolResult{}, toolErrf("grep: invalid regex: %v", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ToolResult{}, toolErrf("grep: directory not found: %s", a.Dir)
		}
		return ToolResult{}, toolErrf("grep: cannot stat %s: %v", a.Dir, err)
	}
	if !info.IsDir() {
		return ToolResult{}, toolErrf("grep: dir is not a directory: %s", a.Dir)
	}

	maxMatches := d.limits.MaxGrepMatches
	maxLineBytes := d.limits.MaxGrepLineBytes
	var matches []string
	total := 0
	truncated := false

	walkErr := filepath.WalkDir(absPath, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if de.IsDir() {
			if de.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !de.Type().IsRegular() {
			return nil // skip symlinks, devices, sockets
		}
		if a.Glob != "" {
			if ok, _ := filepath.Match(a.Glob, de.Name()); !ok {
				return nil
			}
		}
		scanFileForMatches(path, relDisplay(d.root, path), re, maxMatches, maxLineBytes, &matches, &total, &truncated)
		return nil
	})
	if walkErr != nil {
		if ctx.Err() != nil {
			return ToolResult{}, ctx.Err()
		}
		return ToolResult{}, toolErrf("grep: walk failed: %v", walkErr)
	}

	if len(matches) == 0 {
		if a.Glob != "" {
			return ToolResult{Content: fmt.Sprintf("no matches for '%s' in glob '%s'", a.Pattern, a.Glob)}, nil
		}
		return ToolResult{Content: fmt.Sprintf("no matches for '%s'", a.Pattern)}, nil
	}

	content := strings.Join(matches, "\n")
	if truncated {
		content += fmt.Sprintf("\n[...%d more matches truncated...]", total-len(matches))
		return ToolResult{Content: content, Truncated: true, OriginalBytes: len(content)}, nil
	}
	return ToolResult{Content: content, OriginalBytes: len(content)}, nil
}

// scanFileForMatches scans one file line by line, appending formatted matches
// until the match cap is hit (after which it keeps counting total for the
// truncation marker). It closes the file before returning.
func scanFileForMatches(path, rel string, re *regexp.Regexp, maxMatches, maxLineBytes int, matches *[]string, total *int, truncated *bool) {
	f, err := openReadOnly(path)
	if err != nil {
		return // skip files we cannot open read-only (e.g. symlink with O_NOFOLLOW)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	ln := 0
	for sc.Scan() {
		ln++
		line := sc.Text()
		if !re.MatchString(line) {
			continue
		}
		*total++
		if maxMatches > 0 && len(*matches) >= maxMatches {
			*truncated = true
			continue
		}
		if maxLineBytes > 0 && len(line) > maxLineBytes {
			line = safeRuneCut(line, maxLineBytes) + "…"
		}
		*matches = append(*matches, fmt.Sprintf("%s:%d: %s", rel, ln, line))
	}
	// Surface a scanner error (e.g. bufio.ErrTooLong on an over-long line, or a
	// mid-file read error) so the model is not misled into thinking an
	// incompletely scanned file produced no further matches.
	if err := sc.Err(); err != nil {
		*matches = append(*matches, fmt.Sprintf("%s: [skipped: %v]", rel, err))
	}
}
