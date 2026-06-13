package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// truncMarker is appended to any result content shortened by a byte cap.
const truncMarker = "\n[...truncated...]"

// Resolver validates and resolves a relative path against a sandbox root,
// returning an absolute path or an error if the path escapes the jail. It is
// satisfied by *Jail; the dispatcher depends only on this small interface so
// handlers never see raw, unvalidated input.
type Resolver interface {
	Resolve(relPath string) (string, error)
	// Root returns the canonical sandbox root. The dispatcher uses it as the
	// default base for grep/list_files and for rendering relative paths, so it
	// must use the SAME canonicalization as the paths Resolve returns. Sourcing
	// the root from the resolver (rather than a separate constructor argument)
	// makes a root/jail mismatch structurally impossible.
	Root() string
}

// ToolError is a non-fatal tool failure. The agent loop converts it into the
// content of a role:"tool" message rather than aborting the review.
type ToolError struct{ Message string }

func (e *ToolError) Error() string { return e.Message }

func toolErrf(format string, a ...any) *ToolError {
	return &ToolError{Message: fmt.Sprintf(format, a...)}
}

// handlerFunc executes one tool. absPath is the jail-resolved path for the
// tool's path/dir argument (or the snapshot root when that argument is omitted);
// handlers therefore never call the jail themselves.
type handlerFunc func(ctx context.Context, d *Dispatcher, args json.RawMessage, absPath string) (ToolResult, error)

// pathSpec names the argument a tool resolves through the jail.
type pathSpec struct {
	name     string // argument name, e.g. "path" or "dir"
	required bool   // if true, an empty/absent value is an error
}

// Dispatcher routes tool_calls to read-only handlers, resolving path arguments
// through the jail and enforcing a per-call byte cap. It is the sole path by
// which handlers are invoked.
type Dispatcher struct {
	jail     Resolver
	root     string
	limits   Limits
	handlers map[string]handlerFunc
	pathArgs map[string]pathSpec
}

// NewDispatcher builds a dispatcher over the three built-in read-only tools.
// The snapshot root (default search/listing base) is taken from jail.Root() so
// it always shares the jail's canonicalization.
func NewDispatcher(jail Resolver, limits Limits) *Dispatcher {
	d := &Dispatcher{
		jail:     jail,
		root:     jail.Root(),
		limits:   limits,
		handlers: map[string]handlerFunc{},
		pathArgs: map[string]pathSpec{},
	}
	d.mustRegister("read_file", readFileHandler, pathSpec{name: "path", required: true})
	d.mustRegister("grep", grepHandler, pathSpec{name: "dir"})
	d.mustRegister("list_files", listFilesHandler, pathSpec{name: "dir"})
	return d
}

// SetLimits replaces the result caps. It is not safe for concurrent use: call
// it during construction/tuning before any goroutine invokes Execute. Execute
// itself is safe to call concurrently.
func (d *Dispatcher) SetLimits(l Limits) { d.limits = l }

// RegisteredTools returns the names of all registered tools.
func (d *Dispatcher) RegisteredTools() []string {
	names := make([]string, 0, len(d.handlers))
	for name := range d.handlers {
		names = append(names, name)
	}
	return names
}

// writeToolPatterns are name fragments that flag a mutating tool. The harness is
// read-only by construction, so any such registration is rejected.
var writeToolPatterns = []string{"write", "create", "delete", "remove", "modif", "update", "append", "patch"}

// RegisterTool adds a handler after checking its name against the write-tool
// blocklist. It returns an error (never silently accepts) for write-shaped names.
func (d *Dispatcher) RegisterTool(name string, h handlerFunc) error {
	if err := guardToolName(name); err != nil {
		return err
	}
	d.handlers[name] = h
	return nil
}

func guardToolName(name string) error {
	lower := strings.ToLower(name)
	for _, p := range writeToolPatterns {
		if strings.Contains(lower, p) {
			return fmt.Errorf("tool registry: write tools are not allowed: %s", name)
		}
	}
	return nil
}

func (d *Dispatcher) mustRegister(name string, h handlerFunc, ps pathSpec) {
	if err := d.RegisterTool(name, h); err != nil {
		panic(err) // built-in names are read-only by construction
	}
	d.pathArgs[name] = ps
}

// Execute routes a single tool call. It returns a *ToolError (never panics) for
// unknown tools, malformed arguments, jail violations, and handler failures, so
// the agent loop can feed the message back to the model as a tool result.
func (d *Dispatcher) Execute(ctx context.Context, name string, argsJSON json.RawMessage) (res ToolResult, err error) {
	h, ok := d.handlers[name]
	if !ok {
		return ToolResult{}, toolErrf("unknown tool: %s", name)
	}

	defer func() {
		if r := recover(); r != nil {
			res = ToolResult{}
			err = toolErrf("tool execution failed: %v", r)
		}
	}()

	raw := map[string]json.RawMessage{}
	if trimmed := bytes.TrimSpace(argsJSON); len(trimmed) > 0 {
		if e := json.Unmarshal(trimmed, &raw); e != nil {
			return ToolResult{}, toolErrf("invalid arguments: %v", e)
		}
	}

	absPath := d.root
	if spec, ok := d.pathArgs[name]; ok && spec.name != "" {
		rel, present, perr := stringArg(raw, spec.name)
		if perr != nil {
			return ToolResult{}, toolErrf("%s: invalid arguments: %v", name, perr)
		}
		switch {
		case !present || rel == "":
			if spec.required {
				return ToolResult{}, toolErrf("%s: %s is required", name, spec.name)
			}
		default:
			resolved, jerr := d.jail.Resolve(rel)
			if jerr != nil {
				return ToolResult{}, jerr
			}
			absPath = resolved
		}
	}

	res, err = h(ctx, d, argsJSON, absPath)
	if err != nil {
		return ToolResult{}, err
	}
	return d.capResult(res), nil
}

// capResult applies the dispatcher-level byte cap as a final safety net,
// preserving any truncation a handler already recorded.
func (d *Dispatcher) capResult(res ToolResult) ToolResult {
	limit := d.limits.MaxResultBytes
	if limit > 0 && len(res.Content) > limit {
		full := len(res.Content)
		res.Content = truncate(res.Content, limit)
		res.Truncated = true
		if res.OriginalBytes < full {
			res.OriginalBytes = full
		}
		return res
	}
	if res.OriginalBytes == 0 {
		res.OriginalBytes = len(res.Content)
	}
	return res
}

// truncate shortens s so the returned string (including the marker) is at most
// limit bytes.
func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	keep := limit - len(truncMarker)
	if keep < 0 {
		return truncMarker[:limit]
	}
	return safeRuneCut(s, keep) + truncMarker
}

// safeRuneCut returns s truncated to at most n bytes without splitting a
// multi-byte UTF-8 rune (so the result is always valid UTF-8).
func safeRuneCut(s string, n int) string {
	if n >= len(s) {
		return s
	}
	if n < 0 {
		n = 0
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// stringArg extracts a string argument. It distinguishes absent (present=false)
// from present-but-wrong-type (error).
func stringArg(raw map[string]json.RawMessage, key string) (value string, present bool, err error) {
	v, ok := raw[key]
	if !ok {
		return "", false, nil
	}
	if e := json.Unmarshal(v, &value); e != nil {
		return "", true, fmt.Errorf("%s must be a string", key)
	}
	return value, true, nil
}

// relDisplay renders path relative to base using forward slashes, falling back
// to the absolute path if it cannot be made relative.
func relDisplay(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
