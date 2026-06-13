package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type readFileArgs struct {
	Path      string `json:"path"`
	StartLine *int   `json:"start_line"`
	EndLine   *int   `json:"end_line"`
}

// readFileHandler returns line-numbered file content. absPath has already been
// validated by the jail. Output above MaxReadFileBytes is truncated with a marker.
func readFileHandler(_ context.Context, d *Dispatcher, argsJSON json.RawMessage, absPath string) (ToolResult, error) {
	var a readFileArgs
	if err := json.Unmarshal(argsJSON, &a); err != nil {
		return ToolResult{}, toolErrf("read_file: invalid arguments: %v", err)
	}
	if a.StartLine != nil && *a.StartLine < 1 {
		return ToolResult{}, toolErrf("read_file: start_line must be at least 1")
	}
	if a.EndLine != nil && *a.EndLine < 1 {
		return ToolResult{}, toolErrf("read_file: end_line must be at least 1")
	}
	if a.StartLine != nil && a.EndLine != nil && *a.StartLine > *a.EndLine {
		return ToolResult{}, toolErrf("read_file: start_line cannot be greater than end_line")
	}

	f, err := openReadOnly(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ToolResult{}, toolErrf("read_file: file not found: %s", a.Path)
		}
		return ToolResult{}, toolErrf("read_file: cannot open %s: %v", a.Path, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return ToolResult{}, toolErrf("read_file: cannot stat %s: %v", a.Path, err)
	}
	if info.IsDir() {
		return ToolResult{}, toolErrf("read_file: %s is a directory", a.Path)
	}

	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if a.StartLine != nil && lineNo < *a.StartLine {
			continue
		}
		if a.EndLine != nil && lineNo > *a.EndLine {
			break
		}
		fmt.Fprintf(&b, "%d: %s\n", lineNo, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return ToolResult{}, toolErrf("read_file: read error on %s: %v", a.Path, err)
	}

	rendered := b.String()
	full := len(rendered)
	if cap := d.limits.MaxReadFileBytes; cap > 0 && full > cap {
		return ToolResult{Content: truncate(rendered, cap), Truncated: true, OriginalBytes: full}, nil
	}
	return ToolResult{Content: rendered, OriginalBytes: full}, nil
}
