package tools

import "encoding/json"

// ToolDef is a read-only tool exposed to reviewer agents. It marshals to the
// OpenAI function-calling envelope ({"type":"function","function":{...}}), the
// lowest-common-denominator wire format across OpenAI-compatible providers.
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema object
}

// MarshalJSON emits the OpenAI tool envelope so a slice of ToolDef can be
// serialized directly into a chat-completions "tools" array.
func (d ToolDef) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        d.Name,
			"description": d.Description,
			"parameters":  d.Parameters,
		},
	})
}

// Tools returns the three v1 read-only tool definitions. The set is static:
// tools are hardcoded here, never loaded from external configuration, so a
// model cannot register new tools.
func Tools() []ToolDef {
	return []ToolDef{readFileDef(), grepDef(), listFilesDef()}
}

func readFileDef() ToolDef {
	return ToolDef{
		Name:        "read_file",
		Description: "Read a file from the repository snapshot, returning line-numbered content. Optional 1-based inclusive start_line/end_line select a slice.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string", "description": "Path relative to the snapshot root."},
				"start_line": map[string]any{"type": "integer", "description": "1-based first line to include."},
				"end_line":   map[string]any{"type": "integer", "description": "1-based last line to include."},
			},
			"required": []string{"path"},
		},
	}
}

func grepDef() ToolDef {
	return ToolDef{
		Name:        "grep",
		Description: "Search file contents under the snapshot root with a Go regular expression. Optional glob filters files by name; optional dir restricts the search subtree.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Go regexp pattern."},
				"glob":    map[string]any{"type": "string", "description": "Optional filename glob, e.g. *.go."},
				"dir":     map[string]any{"type": "string", "description": "Optional subdirectory to restrict the search."},
			},
			"required": []string{"pattern"},
		},
	}
}

func listFilesDef() ToolDef {
	return ToolDef{
		Name:        "list_files",
		Description: "List files and directories under the snapshot root (or dir if given), recursively up to a depth cap.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"dir": map[string]any{"type": "string", "description": "Optional subdirectory to list; defaults to the snapshot root."},
			},
			"required": []string{},
		},
	}
}
