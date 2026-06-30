package main

import "testing"

func findFunc(n node, name string) (node, bool) {
	if n.Kind == "func" && n.Name == name {
		return n, true
	}
	for _, c := range n.Children {
		if f, ok := findFunc(c, name); ok {
			return f, true
		}
	}
	return node{}, false
}

func TestPHPConfig_ClassMethodGrouping(t *testing.T) {
	src := []byte("<?php\nclass Foo {\n  public function bar() {\n    $x = 1;\n    $y = 2;\n    return $x + $y;\n  }\n}\n")
	root := parseSource(src, phpConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "class" || root.Children[0].Name != "Foo" {
		t.Fatalf("want class Foo, got %+v", root.Children)
	}
	if _, ok := findFunc(root, "bar"); !ok {
		t.Fatalf("expected func bar in tree: %+v", root)
	}
	a, _ := deepest(root, 4)
	b, _ := deepest(root, 5)
	if a.Kind != "func" || a.Name != "bar" || a.StartLine != b.StartLine {
		t.Fatalf("php method body lines should share func bar: %+v / %+v", a, b)
	}
}

func TestPHPConfig_HashCommentBraceIgnored(t *testing.T) {
	src := []byte("<?php\nfunction f() {\n  # } stray brace {\n  g();\n}\n")
	root := parseSource(src, phpConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "func" || root.Children[0].Name != "f" {
		t.Fatalf("php # comment braces must not break structure: %+v", root.Children)
	}
}

func TestPHPConfig_TraitIsClass(t *testing.T) {
	src := []byte("<?php\ntrait T {\n  function m() {\n    $a = 1;\n  }\n}\n")
	root := parseSource(src, phpConfig)
	if root.Children[0].Kind != "class" || root.Children[0].Name != "T" {
		t.Fatalf("php trait should map to class T: %+v", root.Children[0])
	}
}

func TestRustConfig_ImplFnGrouping(t *testing.T) {
	src := []byte("impl Foo {\n    fn bar(&self) {\n        let x = 1;\n        let y = 2;\n        x + y\n    }\n}\n")
	root := parseSource(src, rustConfig)
	if root.Children[0].Kind != "class" || root.Children[0].Name != "Foo" {
		t.Fatalf("rust impl should map to class Foo: %+v", root.Children[0])
	}
	a, _ := deepest(root, 3)
	b, _ := deepest(root, 4)
	if a.Kind != "func" || a.Name != "bar" || a.StartLine != b.StartLine {
		t.Fatalf("rust fn body lines should share func bar: %+v / %+v", a, b)
	}
}

func TestRustConfig_RawStringBraceIgnored(t *testing.T) {
	src := []byte("fn f() {\n    let s = r#\"a { b } c\"#;\n    g();\n}\n")
	root := parseSource(src, rustConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "func" {
		t.Fatalf("rust raw-string braces must not break structure: %+v", root.Children)
	}
	if len(root.Children[0].Children) != 0 {
		t.Fatalf("raw string must not create child blocks: %+v", root.Children[0].Children)
	}
}

func TestRustConfig_MatchAndLoop(t *testing.T) {
	src := []byte("fn f(v: i32) {\n    loop {\n        let x = 1;\n        break;\n    }\n}\n")
	root := parseSource(src, rustConfig)
	fn, ok := findFunc(root, "f")
	if !ok || len(fn.Children) != 1 || fn.Children[0].Kind != "while" {
		t.Fatalf("rust loop should map to a while block inside f: %+v", root.Children)
	}
}

func TestBashConfig_FunctionForms(t *testing.T) {
	src := []byte("greet() {\n  echo a\n  echo b\n}\nfunction hi {\n  echo c\n}\n")
	root := parseSource(src, bashConfig)
	if len(root.Children) != 2 {
		t.Fatalf("want 2 bash functions, got %d: %+v", len(root.Children), root.Children)
	}
	if root.Children[0].Kind != "func" || root.Children[0].Name != "greet" {
		t.Fatalf("name() form should be func greet: %+v", root.Children[0])
	}
	if root.Children[1].Kind != "func" || root.Children[1].Name != "hi" {
		t.Fatalf("function form should be func hi: %+v", root.Children[1])
	}
}

func TestBashConfig_ParamExpansionNotABlock(t *testing.T) {
	// Unquoted ${var} must NOT open a block (and its closing } must not pop the
	// enclosing function), so both body lines stay in the same func block.
	src := []byte("greet() {\n  local x=${name}\n  local y=${other:-def}\n  echo $x $y\n}\n")
	root := parseSource(src, bashConfig)
	if len(root.Children) != 1 || root.Children[0].Name != "greet" {
		t.Fatalf("param expansions corrupted the function block: %+v", root.Children)
	}
	if len(root.Children[0].Children) != 0 {
		t.Fatalf("${...} must not create child blocks: %+v", root.Children[0].Children)
	}
	a, _ := deepest(root, 2)
	b, _ := deepest(root, 3)
	if a.Name != "greet" || a.StartLine != b.StartLine {
		t.Fatalf("bash body lines should share the greet block: %+v / %+v", a, b)
	}
}

func TestBashConfig_NestedParamExpansion(t *testing.T) {
	src := []byte("f() {\n  echo ${a:-${b:-c}}\n  done_thing\n}\n")
	root := parseSource(src, bashConfig)
	if len(root.Children) != 1 || root.Children[0].Name != "f" || len(root.Children[0].Children) != 0 {
		t.Fatalf("nested ${..${..}..} must not create blocks: %+v", root.Children)
	}
}

// TestConfigs_AllNamed guards that every config carries its language name so the
// build-tag selection and any future per-language assertion can rely on it.
func TestConfigs_AllNamed(t *testing.T) {
	for _, c := range []langConfig{tsConfig, phpConfig, rustConfig, bashConfig, javaConfig, kotlinConfig, cppConfig, csharpConfig} {
		if c.name == "" {
			t.Fatalf("config missing name: %+v", c)
		}
		if len(c.keywords) == 0 {
			t.Fatalf("config %q has no keywords", c.name)
		}
	}
}

func TestJavaConfig_MethodGroupingNamed(t *testing.T) {
	// A Java method with modifiers + return type must group as a named func via
	// the trailing-identifier funcParen rule, and its body lines must share it.
	src := []byte("class Service {\n  public void execute() {\n    int x = 1;\n    int y = 2;\n    use(x, y);\n  }\n}\n")
	root := parseSource(src, javaConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "class" || root.Children[0].Name != "Service" {
		t.Fatalf("want class Service, got %+v", root.Children)
	}
	m, ok := findFunc(root, "execute")
	if !ok {
		t.Fatalf("expected func execute in tree: %+v", root)
	}
	if m.Kind != "func" {
		t.Fatalf("execute kind = %q, want func", m.Kind)
	}
	a, _ := deepest(root, 3)
	b, _ := deepest(root, 5)
	if a.Name != "execute" || a.StartLine != b.StartLine {
		t.Fatalf("java method body lines should share func execute: %+v / %+v", a, b)
	}
}

func TestJavaConfig_TextBlockBracesIgnored(t *testing.T) {
	// A Java text block """...""" is opaque: its braces must not open a block.
	src := []byte("class C {\n  void m() {\n    String s = \"\"\"\n      { not a block }\n    \"\"\";\n    next();\n  }\n}\n")
	root := parseSource(src, javaConfig)
	m, ok := findFunc(root, "m")
	if !ok {
		t.Fatalf("expected func m, got %+v", root.Children)
	}
	if len(m.Children) != 0 {
		t.Fatalf("java text-block braces must not create child blocks: %+v", m.Children)
	}
}

func TestJavaConfig_CharLiteralBraceIgnored(t *testing.T) {
	// A char literal '{' must not skew brace depth.
	src := []byte("class C {\n  void m() {\n    char open = '{';\n    char close = '}';\n    next();\n  }\n}\n")
	root := parseSource(src, javaConfig)
	m, ok := findFunc(root, "m")
	if !ok || len(m.Children) != 0 {
		t.Fatalf("java char-literal braces broke structure: %+v", root.Children)
	}
}

func TestKotlinConfig_FunAndMultilineString(t *testing.T) {
	// Kotlin `fun` introduces a named func; a """multiline""" string with an
	// interpolation brace must be opaque.
	src := []byte("fun target() {\n  val s = \"\"\"\n    value = ${ a { b } }\n  \"\"\"\n  val x = 1\n  use(s, x)\n}\n")
	root := parseSource(src, kotlinConfig)
	fn, ok := findFunc(root, "target")
	if !ok {
		t.Fatalf("expected func target, got %+v", root.Children)
	}
	if len(fn.Children) != 0 {
		t.Fatalf("kotlin multiline string braces must not create child blocks: %+v", fn.Children)
	}
}

func TestKotlinConfig_WhenIsSwitch(t *testing.T) {
	src := []byte("fun f(v: Int) {\n  when (v) {\n    1 -> a()\n    else -> b()\n  }\n}\n")
	root := parseSource(src, kotlinConfig)
	fn, ok := findFunc(root, "f")
	if !ok || len(fn.Children) != 1 || fn.Children[0].Kind != "switch" {
		t.Fatalf("kotlin when should map to a switch block inside f: %+v", root.Children)
	}
}

func TestKotlinConfig_ConstructorUsesFuncParen(t *testing.T) {
	// Keyword-less call-shaped headers such as secondary constructors must be
	// named via the funcParenName path, not silently dropped.
	src := []byte("class C {\n  constructor(name: String) {\n    init(name)\n  }\n}\n")
	root := parseSource(src, kotlinConfig)
	if _, ok := findFunc(root, "constructor"); !ok {
		t.Fatalf("expected func constructor from keyword-less header, got %+v", root.Children)
	}
}

func TestCppConfig_OutOfLineMethodNamed(t *testing.T) {
	// A C++ out-of-line definition `void Foo::bar()` must name the func bar.
	src := []byte("void Foo::bar() {\n  int x = 1;\n  step();\n}\n")
	root := parseSource(src, cppConfig)
	if _, ok := findFunc(root, "bar"); !ok {
		t.Fatalf("expected func bar from `void Foo::bar()`, got %+v", root.Children)
	}
}

func TestCppConfig_StructAndPreprocessorIgnored(t *testing.T) {
	// `#include` / `#define` lines are treated as line comments so their angle
	// brackets and stray tokens never skew structure; a struct maps to class.
	src := []byte("#include <vector>\n#define N 4\nstruct Point {\n  int x;\n  int y;\n};\n")
	root := parseSource(src, cppConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "class" || root.Children[0].Name != "Point" {
		t.Fatalf("cpp struct should map to class Point with preprocessor lines ignored: %+v", root.Children)
	}
}

func TestCppConfig_CharLiteralBraceIgnored(t *testing.T) {
	src := []byte("int f() {\n  char a = '{';\n  char b = '}';\n  return 0;\n}\n")
	root := parseSource(src, cppConfig)
	fn, ok := findFunc(root, "f")
	if !ok || len(fn.Children) != 0 {
		t.Fatalf("cpp char-literal braces broke structure: %+v", root.Children)
	}
}

func TestCSharpConfig_MethodAndRawString(t *testing.T) {
	// A C# method groups as a named func; a """raw string""" is opaque.
	src := []byte("class C {\n  public void Run() {\n    var s = \"\"\"\n      { not a block }\n    \"\"\";\n    Next();\n  }\n}\n")
	root := parseSource(src, csharpConfig)
	if len(root.Children) != 1 || root.Children[0].Kind != "class" || root.Children[0].Name != "C" {
		t.Fatalf("want class C, got %+v", root.Children)
	}
	m, ok := findFunc(root, "Run")
	if !ok {
		t.Fatalf("expected func Run, got %+v", root.Children)
	}
	if len(m.Children) != 0 {
		t.Fatalf("csharp raw-string braces must not create child blocks: %+v", m.Children)
	}
}

func TestCSharpConfig_ForeachIsFor(t *testing.T) {
	src := []byte("class C {\n  void m() {\n    foreach (var it in items) {\n      total += it;\n      log(it);\n    }\n  }\n}\n")
	root := parseSource(src, csharpConfig)
	a, _ := deepest(root, 4)
	b, _ := deepest(root, 5)
	if a.Kind != "for" || a.StartLine != b.StartLine {
		t.Fatalf("csharp foreach body lines should share a for block: %+v / %+v", a, b)
	}
}

func TestCSharpConfig_NestedInterpolationDegradesGracefully(t *testing.T) {
	// Nested string literals inside interpolation holes are not tracked; the
	// scanner closes the outer string on the first inner quote. Accept this as a
	// degrade-to-proximity limitation: the parse must not panic and the enclosing
	// method must still be recoverable.
	src := []byte("class C {\n  void m() {\n    var s = $\"{(\"x\")}\";\n    Next();\n  }\n}\n")
	root := parseSource(src, csharpConfig)
	m, ok := findFunc(root, "m")
	if !ok {
		t.Fatalf("expected func m despite nested interpolation, got %+v", root.Children)
	}
	if m.Kind != "func" || m.Name != "m" {
		t.Fatalf("want func m, got %+v", m)
	}
}

func TestPHPConfig_HeredocBracesIgnored(t *testing.T) {
	// PHP heredoc terminator EOT; (marker + semicolon) must close, so the sibling
	// func `b` is not swallowed, and the braces inside the heredoc create no block.
	src := []byte("<?php\nfunction a() {\n  $s = <<<EOT\n  not { a } block\nEOT;\n  return $s;\n}\nfunction b() {\n  return 2;\n}\n")
	root := parseSource(src, phpConfig)
	if len(root.Children) != 2 {
		t.Fatalf("php heredoc must terminate at EOT; got %d children %+v", len(root.Children), root.Children)
	}
	if root.Children[0].Name != "a" || root.Children[1].Name != "b" {
		t.Fatalf("want funcs a,b; got %+v", root.Children)
	}
	if len(root.Children[0].Children) != 0 {
		t.Fatalf("heredoc body braces must not create blocks: %+v", root.Children[0].Children)
	}
}
