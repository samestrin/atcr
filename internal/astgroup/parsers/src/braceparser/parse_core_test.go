package main

import "testing"

// The scanner tests drive the production tsConfig directly (see configs.go).

// deepest returns the deepest block node whose inclusive line span covers line,
// mirroring the host's CoveringBlock so unit tests can assert grouping intent.
func deepest(n node, line int) (node, bool) {
	if line < n.StartLine || line > n.EndLine {
		return node{}, false
	}
	for _, c := range n.Children {
		if d, ok := deepest(c, line); ok {
			return d, true
		}
	}
	return n, true
}

func firstChildKind(n node) string {
	if len(n.Children) == 0 {
		return ""
	}
	return n.Children[0].Kind
}

func TestParseSource_SimpleFunction(t *testing.T) {
	src := []byte("function f() {\n  let x = 1\n  let y = 2\n}\n")
	root := parseSource(src, tsConfig)
	if root.Kind != "file" {
		t.Fatalf("root kind = %q, want file", root.Kind)
	}
	if len(root.Children) != 1 {
		t.Fatalf("root children = %d, want 1", len(root.Children))
	}
	fn := root.Children[0]
	if fn.Kind != "func" || fn.Name != "f" {
		t.Fatalf("func node = %q/%q, want func/f", fn.Kind, fn.Name)
	}
	if fn.StartLine != 1 || fn.EndLine != 4 {
		t.Fatalf("func span = %d..%d, want 1..4", fn.StartLine, fn.EndLine)
	}
	// Both body lines must resolve to the same covering block (grouping intent).
	a, _ := deepest(root, 2)
	b, _ := deepest(root, 3)
	if a.Kind != "func" || a.StartLine != b.StartLine {
		t.Fatalf("body lines did not share the func block: %+v vs %+v", a, b)
	}
}

func TestParseSource_SiblingFunctionsDistinct(t *testing.T) {
	src := []byte("function a() {\n  x()\n}\nfunction b() {\n  y()\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 2 {
		t.Fatalf("want 2 sibling funcs, got %d", len(root.Children))
	}
	a, _ := deepest(root, 2)
	b, _ := deepest(root, 5)
	if a.StartLine == b.StartLine {
		t.Fatalf("distinct functions must not share a covering block")
	}
}

func TestParseSource_NestedControlFlow(t *testing.T) {
	src := []byte("function f(v) {\n  if (v) {\n    g()\n    h()\n  }\n}\n")
	root := parseSource(src, tsConfig)
	fn := root.Children[0]
	if fn.Kind != "func" {
		t.Fatalf("outer kind = %q, want func", fn.Kind)
	}
	if firstChildKind(fn) != "if" {
		t.Fatalf("inner kind = %q, want if", firstChildKind(fn))
	}
	a, _ := deepest(root, 3)
	b, _ := deepest(root, 4)
	if a.Kind != "if" || a.StartLine != b.StartLine {
		t.Fatalf("if-body lines should share the if block: %+v vs %+v", a, b)
	}
}

func TestParseSource_BraceInStringIgnored(t *testing.T) {
	src := []byte("function f() {\n  let s = \"a { b } c\"\n  let t = 'x } y {'\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 1 {
		t.Fatalf("braces inside strings must not open blocks; got %d children", len(root.Children))
	}
	if got := len(root.Children[0].Children); got != 0 {
		t.Fatalf("func should have no child blocks; got %d", got)
	}
}

func TestParseSource_BraceInLineCommentIgnored(t *testing.T) {
	src := []byte("function f() {\n  // } stray brace {\n  doWork()\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "func" {
		t.Fatalf("line-comment braces must not break structure: %+v", root.Children)
	}
}

func TestParseSource_BraceInBlockCommentIgnored(t *testing.T) {
	src := []byte("function f() {\n  /* } stray { */\n  doWork()\n}\nfunction g() {\n  more()\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 2 {
		t.Fatalf("block-comment braces must not break structure; got %d children", len(root.Children))
	}
}

func TestParseSource_TemplateLiteralIgnored(t *testing.T) {
	src := []byte("function f() {\n  let s = `a ${x} { } b`\n  done()\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 1 || len(root.Children[0].Children) != 0 {
		t.Fatalf("template-literal braces must not open blocks: %+v", root.Children)
	}
}

func TestParseSource_ArrowFunction(t *testing.T) {
	src := []byte("const handler = (e) => {\n  process(e)\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "func" {
		t.Fatalf("arrow function should be a func block: %+v", root.Children)
	}
}

func TestParseSource_ObjectLiteralIsAnonymousBlock(t *testing.T) {
	// An object literal assigned to a variable must NOT be named func/class; it
	// becomes an anonymous block so it never false-merges with a real declaration.
	src := []byte("const cfg = {\n  a: 1,\n  b: 2,\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 1 {
		t.Fatalf("want 1 block child, got %d", len(root.Children))
	}
	if root.Children[0].Kind != "block" {
		t.Fatalf("object literal kind = %q, want block", root.Children[0].Kind)
	}
}

func TestParseSource_EmptyAndDegenerate(t *testing.T) {
	for _, src := range []string{"", "\n\n\n", "   ", "// just a comment\n"} {
		root := parseSource([]byte(src), tsConfig)
		if root.Kind != "file" {
			t.Fatalf("empty/degenerate %q: root kind %q, want file", src, root.Kind)
		}
		if len(root.Children) != 0 {
			t.Fatalf("empty/degenerate %q: want 0 children, got %d", src, len(root.Children))
		}
	}
}

func TestParseSource_UnbalancedBracesDoNotPanic(t *testing.T) {
	// Extra '}' and an unclosed '{' must both degrade gracefully (no panic, file root).
	for _, src := range []string{"}}}\n", "function f() {\n  if (x) {\n", "{{{{\n"} {
		root := parseSource([]byte(src), tsConfig)
		if root.Kind != "file" {
			t.Fatalf("unbalanced %q: root kind %q", src, root.Kind)
		}
	}
}

func TestParseSource_BashHeredocBracesIgnored(t *testing.T) {
	bash := langConfig{
		name:         "bash",
		lineComments: []string{"#"},
		strChars:     "\"'",
		funcParen:    true,
		heredocs:     true,
		heredocOp:    "<<",
		keywords:     []blockKeyword{{word: "function", kind: "func", named: true}},
	}
	// The heredoc terminator is at column 0 (real bash rules), so the heredoc
	// closes and parsing resumes — the sibling func `post` must NOT be swallowed.
	src := []byte("greet() {\n  cat <<EOF\n  { braces } { in } heredoc\nEOF\n  echo done\n}\npost() {\n  echo hi\n}\n")
	root := parseSource(src, bash)
	if len(root.Children) != 2 {
		t.Fatalf("heredoc must terminate at column-0 tag so `post` is a sibling: got %d children %+v", len(root.Children), root.Children)
	}
	if root.Children[0].Kind != "func" || root.Children[0].Name != "greet" {
		t.Fatalf("first child = %q/%q, want func/greet", root.Children[0].Kind, root.Children[0].Name)
	}
	if len(root.Children[0].Children) != 0 {
		t.Fatalf("heredoc body must not create child blocks: %+v", root.Children[0].Children)
	}
	if root.Children[1].Name != "post" {
		t.Fatalf("second child = %q, want post", root.Children[1].Name)
	}
}

func TestParseSource_RustCharLiteralBraceIgnored(t *testing.T) {
	rust := langConfig{
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
		},
	}
	// The char literals '{' and '}' and the lifetime 'a must not affect brace depth.
	src := []byte("fn f<'a>(x: &'a str) {\n  let open = '{';\n  let close = '}';\n  use_it(x);\n}\n")
	root := parseSource(src, rust)
	if len(root.Children) != 1 || root.Children[0].Kind != "func" || root.Children[0].Name != "f" {
		t.Fatalf("rust char-literal/lifetime handling broke the fn block: %+v", root.Children)
	}
}

func TestParseSource_CStyleForKeepsForKind(t *testing.T) {
	// The semicolons inside the C-style for header must not reset the header, so
	// the loop body is classified "for" (not anonymous "block").
	src := []byte("function f() {\n  for (let i = 0; i < 3; i++) {\n    work(i)\n  }\n}\n")
	root := parseSource(src, tsConfig)
	fn := root.Children[0]
	if fn.Kind != "func" || len(fn.Children) != 1 {
		t.Fatalf("want one child under func f, got %+v", fn)
	}
	if fn.Children[0].Kind != "for" {
		t.Fatalf("C-style for body kind = %q, want for", fn.Children[0].Kind)
	}
}

func TestBashConfig_DollarHashNotComment(t *testing.T) {
	bash := bashConfig
	// `$#` must NOT start a comment; the multi-line group `{ ... }` opened after
	// `&&` must therefore be balanced so the function f still spans to its real
	// closing brace (no brace-stack desync swallowing `echo done`).
	src := []byte("f() {\n  [ $# -gt 0 ] && {\n    echo yes\n  }\n  echo done\n}\n")
	root := parseSource(src, bash)
	if len(root.Children) != 1 || root.Children[0].Name != "f" {
		t.Fatalf("want single func f, got %+v", root.Children)
	}
	if root.Children[0].EndLine != 6 {
		t.Fatalf("func f should span to its real closing brace on line 6, got EndLine=%d", root.Children[0].EndLine)
	}
	// `echo done` (line 5) is directly in f, after the inner group closed on line 4.
	d, _ := deepest(root, 5)
	if d.Kind != "func" || d.Name != "f" {
		t.Fatalf("line 5 should resolve to func f, got %+v", d)
	}
}

func TestBashConfig_HashCommentStillWorks(t *testing.T) {
	// A real comment (# at a word boundary) is still stripped; its brace is ignored.
	src := []byte("f() {\n  # a comment with a } brace\n  echo hi\n}\n")
	root := parseSource(src, bashConfig)
	if len(root.Children) != 1 || root.Children[0].Name != "f" || len(root.Children[0].Children) != 0 {
		t.Fatalf("boundary # comment must still be stripped: %+v", root.Children)
	}
}

func TestParseSource_BashParamExpQuotedBracesIgnored(t *testing.T) {
	// A closing brace inside a quoted string inside ${...} must not exit the
	// parameter-expansion state prematurely; the enclosing function must keep
	// its real closing brace.
	src := []byte("f() { x=${var/\"}\"/}; echo done; }\n")
	root := parseSource(src, bashConfig)
	if len(root.Children) != 1 {
		t.Fatalf("quoted brace inside ${...} must not desync parser; got %d children %+v", len(root.Children), root.Children)
	}
	if root.Children[0].Kind != "func" || root.Children[0].Name != "f" {
		t.Fatalf("expected func/f, got %q/%q", root.Children[0].Kind, root.Children[0].Name)
	}
	if root.Children[0].EndLine != 1 {
		t.Fatalf("func f should end on line 1, got EndLine=%d", root.Children[0].EndLine)
	}
}
