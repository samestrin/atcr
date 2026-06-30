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

func TestParseSource_ControlHeaderInlineArrow(t *testing.T) {
	// An inline arrow inside a control-flow header must not flip the block kind
	// to func; the loop/switch body should keep its control kind.
	src := []byte("function outer() {\n  for (const x of items.map(i => i.id)) {\n    work(x)\n  }\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 1 {
		t.Fatalf("expected one outer block, got %+v", root.Children)
	}
	outer := root.Children[0]
	if outer.Kind != "func" || outer.Name != "outer" {
		t.Fatalf("expected outer func/outer, got %q/%q", outer.Kind, outer.Name)
	}
	if len(outer.Children) != 1 || outer.Children[0].Kind != "for" {
		t.Fatalf("inline arrow in for header must not classify as func: %+v", outer.Children)
	}
}

func TestParseSource_ArrowFunctionStillFunc(t *testing.T) {
	src := []byte("const f = () => {\n  work()\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "func" {
		t.Fatalf("arrow function should still be func: %+v", root.Children)
	}
}

// braceMethodConfig is a minimal brace-language shape (Java/C#/C++-like) used to
// exercise the trailing-identifier funcParenName rework and the tripleQuote state
// independently of the per-language tables in configs.go.
var braceMethodConfig = langConfig{
	name:         "bracemethod",
	lineComments: []string{"//"},
	blockOpen:    "/*",
	blockClose:   "*/",
	strChars:     "\"",
	charLiterals: true,
	tripleQuote:  true,
	funcParen:    true,
	keywords: []blockKeyword{
		{word: "class", kind: "class", named: true},
		{word: "if", kind: "if"},
		{word: "else", kind: "else"},
		{word: "for", kind: "for"},
		{word: "while", kind: "while"},
		{word: "switch", kind: "switch"},
	},
}

func TestFuncParenName_ModifierAndReturnTypeNamed(t *testing.T) {
	// A keyword-less method header carrying modifiers and a return type
	// (`public void execute() {`) must recover the trailing identifier as the
	// func name rather than falling through to an anonymous block.
	src := []byte("class C {\n  public void execute() {\n    work();\n  }\n}\n")
	root := parseSource(src, braceMethodConfig)
	m, ok := findFunc(root, "execute")
	if !ok {
		t.Fatalf("expected func/execute recovered from `public void execute()`, got %+v", root.Children)
	}
	if m.Kind != "func" {
		t.Fatalf("execute kind = %q, want func", m.Kind)
	}
}

func TestFuncParenName_ScopeResolutionNamed(t *testing.T) {
	// A C++ out-of-line definition `void Foo::bar() {` must name the func `bar`
	// (the `::` scope-resolution operator is not a member-access call).
	src := []byte("void Foo::bar() {\n  doIt();\n}\n")
	root := parseSource(src, braceMethodConfig)
	if _, ok := findFunc(root, "bar"); !ok {
		t.Fatalf("expected func/bar from `void Foo::bar()`, got %+v", root.Children)
	}
}

func TestFuncParenName_MemberAccessCallNotFunc(t *testing.T) {
	// A keyword-less, call-shaped header `foo.bar() {` must NOT be named as a
	// function definition: the `.` marks a member-access call. It degrades to an
	// anonymous block (grouping still works; naming does not false-attribute).
	src := []byte("foo.bar() {\n  x();\n}\n")
	root := parseSource(src, braceMethodConfig)
	if len(root.Children) != 1 {
		t.Fatalf("want one block, got %+v", root.Children)
	}
	if root.Children[0].Kind != "block" || root.Children[0].Name != "" {
		t.Fatalf("member-access call header must be anonymous block, got %q/%q",
			root.Children[0].Kind, root.Children[0].Name)
	}
}

func TestFuncParenName_BareNameStillFunc(t *testing.T) {
	// The existing bare-name() form (TS methods, Bash functions) must resolve
	// identically after the rework.
	src := []byte("class C {\n  render() {\n    draw();\n  }\n}\n")
	root := parseSource(src, braceMethodConfig)
	if _, ok := findFunc(root, "render"); !ok {
		t.Fatalf("bare name() method should still be func/render, got %+v", root.Children)
	}
}

func TestParseSource_TripleQuotedBracesIgnored(t *testing.T) {
	// A triple-quoted string (Kotlin multiline / Java text block / C# raw string)
	// is opaque: braces and quotes inside it must never open/close a block.
	src := []byte("class C {\n  void m() {\n    String s = \"\"\"\n      { not a block } \"still\" text\n    \"\"\";\n    next();\n  }\n}\n")
	root := parseSource(src, braceMethodConfig)
	m, ok := findFunc(root, "m")
	if !ok {
		t.Fatalf("expected func/m, got %+v", root.Children)
	}
	if len(m.Children) != 0 {
		t.Fatalf("triple-quoted braces must not create child blocks: %+v", m.Children)
	}
}

func TestParseSource_TripleQuoteOffKeepsSingleQuote(t *testing.T) {
	// With tripleQuote disabled, an ordinary "" empty string followed by a quote
	// must still behave as single-quote string state (no regression for configs
	// that do not opt into the triple-quote state, e.g. C/C++).
	cfg := braceMethodConfig
	cfg.tripleQuote = false
	src := []byte("void m() {\n  const char* a = \"x\";\n  next();\n}\n")
	root := parseSource(src, cfg)
	if _, ok := findFunc(root, "m"); !ok {
		t.Fatalf("expected func/m with tripleQuote off, got %+v", root.Children)
	}
}

func TestParseSource_TSCatchClauseNotFunc(t *testing.T) {
	// TypeScript catch clauses look like `catch (e) {` and must not be
	// misclassified as a function named catch by funcParenName.
	src := []byte("try {\n  work()\n} catch (e) {\n  handle()\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 2 {
		t.Fatalf("try/catch should produce two blocks, got %d children %+v", len(root.Children), root.Children)
	}
	for _, c := range root.Children {
		if c.Kind != "block" {
			t.Fatalf("catch clause should be anonymous block, got %q/%q", c.Kind, c.Name)
		}
	}
}

func TestParseSource_RustGenericImplName(t *testing.T) {
	// Generic impls must extract the type name after skipping the generic list.
	src := []byte("impl<T> Foo<T> {\n  fn bar() {}\n}\n")
	root := parseSource(src, rustConfig)
	if len(root.Children) != 1 {
		t.Fatalf("expected one impl block, got %+v", root.Children)
	}
	if root.Children[0].Kind != "class" || root.Children[0].Name != "Foo" {
		t.Fatalf("expected class/Foo, got %q/%q", root.Children[0].Kind, root.Children[0].Name)
	}
}

func TestParseSource_RustImplForName(t *testing.T) {
	// `impl Trait for Foo` must use Foo as the name so unrelated impls of
	// identical shape do not false-merge.
	src := []byte("impl Trait for Foo {\n  fn bar() {}\n}\n")
	root := parseSource(src, rustConfig)
	if len(root.Children) != 1 {
		t.Fatalf("expected one impl block, got %+v", root.Children)
	}
	if root.Children[0].Kind != "class" || root.Children[0].Name != "Foo" {
		t.Fatalf("expected class/Foo, got %q/%q", root.Children[0].Kind, root.Children[0].Name)
	}
}

func TestParseSource_TSModifierMethodNamed(t *testing.T) {
	// TS method modifiers must not defeat funcParenName; the actual method name
	// should still be recovered.
	src := []byte("class C {\n  async foo() {\n    work()\n  }\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "class" {
		t.Fatalf("expected class, got %+v", root.Children)
	}
	if len(root.Children[0].Children) != 1 {
		t.Fatalf("expected one method, got %+v", root.Children[0].Children)
	}
	m := root.Children[0].Children[0]
	if m.Kind != "func" || m.Name != "foo" {
		t.Fatalf("expected func/foo, got %q/%q", m.Kind, m.Name)
	}
}

func TestParseSource_RustUnicodeEscapeCharLiteral(t *testing.T) {
	// Rust unicode escapes like '\\u{7f}' are char literals; their braces must
	// not affect block depth.
	src := []byte("fn f() { let c = '\\u{7f}'; }")
	root := parseSource(src, rustConfig)
	if len(root.Children) != 1 || len(root.Children[0].Children) != 0 {
		t.Fatalf("unicode escape in char literal must not create child blocks: %+v", root.Children)
	}
}

func TestParseSource_PHPFlexibleHeredoc(t *testing.T) {
	php := langConfig{
		name:         "php",
		lineComments: []string{"//", "#"},
		blockOpen:    "/*",
		blockClose:   "*/",
		strChars:     "\"'",
		heredocs:     true,
		heredocOp:    "<<<",
		keywords:     []blockKeyword{{word: "function", kind: "func", named: true}},
	}
	// PHP 7.3+ allows the closing marker to be indented with spaces/tabs.
	src := []byte("function a() {\n  echo <<<EOT\n  body\n  EOT;\n}\nfunction b() {\n  echo hi;\n}\n")
	root := parseSource(src, php)
	if len(root.Children) != 2 {
		t.Fatalf("indented heredoc closer must terminate; got %d children %+v", len(root.Children), root.Children)
	}
	if root.Children[1].Name != "b" {
		t.Fatalf("second function should be b, got %q", root.Children[1].Name)
	}
}

func TestParseSource_BashBraceExpansionIgnored(t *testing.T) {
	// Bash brace expansion {a,b} / file{1,2} / {1..10} has no leading $ and must
	// NOT open a block: its braces are expansion syntax, not a group command. The
	// enclosing function must stay a single block with no spurious child.
	src := []byte("f() {\n  cp a{1,2}\n  echo {x,y,z}\n  for i in {1..3}; do :; done\n  echo done\n}\n")
	root := parseSource(src, bashConfig)
	if len(root.Children) != 1 || root.Children[0].Name != "f" {
		t.Fatalf("want single func f, got %+v", root.Children)
	}
	if got := len(root.Children[0].Children); got != 0 {
		t.Fatalf("brace expansion must not create child blocks; got %d: %+v", got, root.Children[0].Children)
	}
	if root.Children[0].EndLine != 6 {
		t.Fatalf("func f should span to its real closing brace on line 6, got EndLine=%d", root.Children[0].EndLine)
	}
}

func TestParseSource_BashGroupCommandStillBlock(t *testing.T) {
	// A real `{ ...; }` group command (space after the brace) must still open a
	// block — the expansion special-case must not swallow it.
	src := []byte("f() {\n  [ $# -gt 0 ] && {\n    echo yes\n  }\n  echo done\n}\n")
	root := parseSource(src, bashConfig)
	if len(root.Children) != 1 || root.Children[0].Name != "f" {
		t.Fatalf("want single func f, got %+v", root.Children)
	}
	if got := len(root.Children[0].Children); got != 1 {
		t.Fatalf("group command must still open one block; got %d: %+v", got, root.Children[0].Children)
	}
}

func TestParseSource_PHPAttributeNotComment(t *testing.T) {
	// A PHP 8 attribute `#[Route(...)]` before a declaration on the same line must
	// NOT be swallowed as a `#` line comment; the function block must still open.
	src := []byte("#[Route('/x')] function f() {\n  body();\n}\n")
	root := parseSource(src, phpConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "func" || root.Children[0].Name != "f" {
		t.Fatalf("PHP attribute must not swallow the function: %+v", root.Children)
	}
}

func TestParseSource_PHPHashCommentStillWorks(t *testing.T) {
	// A real `#` comment (not followed by `[`) is still stripped.
	src := []byte("function f() {\n  # a } comment\n  body();\n}\n")
	root := parseSource(src, phpConfig)
	if len(root.Children) != 1 || root.Children[0].Name != "f" || len(root.Children[0].Children) != 0 {
		t.Fatalf("PHP # comment must still be stripped: %+v", root.Children)
	}
}

func TestParseSource_TypedArrowAnnotationIsBlock(t *testing.T) {
	// `const x: () => void = { ... }` is an object literal assigned to a typed
	// const; the `=>` is a return-type annotation followed by an `=` assignment,
	// not an arrow function. It must be an anonymous block, not a func.
	src := []byte("const x: () => void = {\n  a: 1,\n}\n")
	root := parseSource(src, tsConfig)
	if len(root.Children) != 1 {
		t.Fatalf("want 1 child, got %+v", root.Children)
	}
	if root.Children[0].Kind != "block" {
		t.Fatalf("typed-arrow-annotation object literal kind = %q, want block", root.Children[0].Kind)
	}
}

func TestParseSource_BashArithmeticShiftNotHeredoc(t *testing.T) {
	// `<<` inside bash arithmetic `$((...))` / `((...))` is a left-shift, NOT a
	// heredoc. The scanner must not enter heredoc state (which would swallow the
	// rest of the file). Both the spaced and no-space forms must stay structural.
	cases := []struct {
		name string
		src  string
	}{
		{"spaced-expansion", "f() {\n  echo $(( 1 << n ))\n  echo done\n}\ng() {\n  echo hi\n}\n"},
		{"nospace-assign", "f() {\n  x=$((a<<bits))\n  echo done\n}\ng() {\n  echo hi\n}\n"},
		{"arith-command", "f() {\n  (( y = 1 << 3 ))\n  echo done\n}\ng() {\n  echo hi\n}\n"},
	}
	for _, tc := range cases {
		root := parseSource([]byte(tc.src), bashConfig)
		if len(root.Children) != 2 {
			t.Fatalf("%s: arithmetic shift must not start a heredoc; want 2 funcs, got %d: %+v", tc.name, len(root.Children), root.Children)
		}
		if root.Children[0].Name != "f" || root.Children[1].Name != "g" {
			t.Fatalf("%s: want funcs f and g, got %q and %q", tc.name, root.Children[0].Name, root.Children[1].Name)
		}
		if root.Children[0].EndLine != 4 {
			t.Fatalf("%s: func f should end at its real brace on line 4, got EndLine=%d", tc.name, root.Children[0].EndLine)
		}
	}
}
