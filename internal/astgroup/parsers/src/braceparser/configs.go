package main

// This file holds the four per-language tables that parameterize the shared
// scanner in parse_core.go. All language differences live here as DATA — comment
// and string syntax, heredoc/raw-string/char-literal rules, and the block-
// introducing keyword/naming table — so the scanner control flow stays language-
// agnostic over braces + this table (Risk mitigation: "keep language differences
// in data, not control flow"). build.sh compiles one .wasm per language by
// selecting `active` (active_*.go) at compile time via build tag.
//
// Kinds emitted MUST be members of the host's blockKinds set (internal/astgroup
// cover.go): file/func/class/if/for/while/switch/block (+ else). Names are the
// identifier following the keyword, so sibling blocks of identical shape still
// hash distinctly.

// tsConfig covers TypeScript and JavaScript (.ts/.tsx/.cts/.mts/.js/.jsx/.mjs).
// Arrow functions and `name(...) {` method shorthand both become func blocks;
// object/array literals fall through to anonymous "block".
var tsConfig = langConfig{
	name:         "ts",
	lineComments: []string{"//"},
	blockOpen:    "/*",
	blockClose:   "*/",
	strChars:     "\"'`", // double, single, and template (backtick) literals
	arrowFunc:    true,
	funcParen:    true,
	keywords: []blockKeyword{
		{word: "function", kind: "func", named: true},
		{word: "class", kind: "class", named: true},
		{word: "interface", kind: "class", named: true},
		{word: "if", kind: "if"},
		{word: "else", kind: "else"},
		{word: "for", kind: "for"},
		{word: "while", kind: "while"},
		{word: "switch", kind: "switch"},
	},
}

// phpConfig covers PHP (.php). Both // and # are line comments; functions and
// methods use the `function` keyword (so funcParen stays off to avoid mislabeling
// bare calls); heredoc/nowdoc use <<<. PHP string interpolation lives inside
// quoted strings, already handled by string state, so paramExpand stays off.
var phpConfig = langConfig{
	name:         "php",
	lineComments: []string{"//", "#"},
	blockOpen:    "/*",
	blockClose:   "*/",
	strChars:     "\"'",
	heredocs:     true,
	heredocOp:    "<<<",
	keywords: []blockKeyword{
		{word: "function", kind: "func", named: true},
		{word: "class", kind: "class", named: true},
		{word: "interface", kind: "class", named: true},
		{word: "trait", kind: "class", named: true},
		{word: "if", kind: "if"},
		{word: "elseif", kind: "if"},
		{word: "else", kind: "else"},
		{word: "for", kind: "for"},
		{word: "foreach", kind: "for"},
		{word: "while", kind: "while"},
		{word: "switch", kind: "switch"},
	},
}

// rustConfig covers Rust (.rs). Only " opens a string; ' is a char literal or
// lifetime (charLiterals), not a string delimiter, so '{' / '}' / 'a never skew
// brace depth. Raw strings r"..." / r#"..."# are opaque. impl/trait/mod/struct/
// enum map to "class"; loop maps to "while"; match maps to "switch".
var rustConfig = langConfig{
	name:         "rust",
	lineComments: []string{"//"},
	blockOpen:    "/*",
	blockClose:   "*/",
	strChars:     "\"",
	rawStrings:   true,
	charLiterals: true,
	keywords: []blockKeyword{
		{word: "fn", kind: "func", named: true},
		{word: "impl", kind: "class", named: true},
		{word: "trait", kind: "class", named: true},
		{word: "mod", kind: "class", named: true},
		{word: "struct", kind: "class", named: true},
		{word: "enum", kind: "class", named: true},
		{word: "if", kind: "if"},
		{word: "else", kind: "else"},
		{word: "for", kind: "for"},
		{word: "while", kind: "while"},
		{word: "loop", kind: "while"},
		{word: "match", kind: "switch"},
	},
}

// bashConfig covers Bash (.sh/.bash). Function-level granularity only: both
// `name() {` (funcParen) and `function name {` (keyword) forms are recovered.
// `#` is a line comment, << starts a heredoc, and ${...} is opaque so its braces
// never open/close a block. Bash control flow (if/fi, for/done, case/esac) is
// end-keyword delimited and intentionally NOT grouped (see epic clarifications).
var bashConfig = langConfig{
	name:         "bash",
	lineComments: []string{"#"},
	strChars:     "\"'",
	funcParen:    true,
	heredocs:     true,
	heredocOp:    "<<",
	paramExpand:  true,
	keywords: []blockKeyword{
		{word: "function", kind: "func", named: true},
	},
}
