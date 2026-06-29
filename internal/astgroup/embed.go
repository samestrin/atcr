package astgroup

import "embed"

//go:embed parsers/go.wasm parsers/python.wasm
//go:embed parsers/ts.wasm parsers/php.wasm parsers/rust.wasm parsers/bash.wasm
var parserFS embed.FS

// builtinParsers maps a language id to its embedded .wasm plugin path. Adding a
// language is "drop in a new .wasm file" plus one entry here (and an extension
// mapping in LanguageForExt). The embedded set is the hermetic default; a runtime
// override directory (future extension) would be consulted before this map.
//
// The ts/php/rust/bash plugins are four builds of one shared brace-block parser
// source (parsers/src/braceparser), each with its language's keyword/naming table
// baked in at compile time; the language id is the discriminator the host uses to
// pick the binary, exactly as for go/python.
var builtinParsers = map[string]string{
	"go":     "parsers/go.wasm",
	"python": "parsers/python.wasm",
	"ts":     "parsers/ts.wasm",
	"php":    "parsers/php.wasm",
	"rust":   "parsers/rust.wasm",
	"bash":   "parsers/bash.wasm",
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
	// TypeScript/JavaScript family — one shared brace parser, one language id.
	case ".ts", ".tsx", ".cts", ".mts", ".js", ".jsx", ".mjs":
		return "ts"
	case ".php":
		return "php"
	case ".rs":
		return "rust"
	case ".sh", ".bash":
		return "bash"
	default:
		return ""
	}
}
