package astgroup

import "embed"

//go:embed parsers/go.wasm parsers/python.wasm
var parserFS embed.FS

// builtinParsers maps a language id to its embedded .wasm plugin path. Adding a
// language is "drop in a new .wasm file" plus one entry here (and an extension
// mapping in LanguageForExt). The embedded set is the hermetic default; a runtime
// override directory (future extension) would be consulted before this map.
var builtinParsers = map[string]string{
	"go":     "parsers/go.wasm",
	"python": "parsers/python.wasm",
}

// LanguageForExt maps a file extension (including the dot, lowercased) to a
// parser language id, or "" if no parser is available for it. Callers that get
// "" should fall back to line-proximity grouping for that finding.
func LanguageForExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	default:
		return ""
	}
}
