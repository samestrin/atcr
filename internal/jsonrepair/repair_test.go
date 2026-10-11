package jsonrepair

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepair_Defects covers each defect class Repair fixes. want is the exact
// repaired text, so a test also pins that nothing else moved.
func TestRepair_Defects(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		// trailing commas
		{"trailing comma in object", `{"a":1,}`, `{"a":1}`},
		{"trailing comma in array", `[1,2,]`, `[1,2]`},
		{"trailing comma before whitespace", "{\"a\":1, \n}", "{\"a\":1 \n}"},
		{"trailing comma inside one element", `[{"a":1},{"b":2,},{"c":3}]`, `[{"a":1},{"b":2},{"c":3}]`},
		{"trailing commas nested", `{"a":[1,],"b":{"c":2,},}`, `{"a":[1],"b":{"c":2}}`},

		// single quotes
		{"single-quoted key and value", `{'a':'b'}`, `{"a":"b"}`},
		{"single-quoted value holding a double quote", `{"a":'say "hi"'}`, `{"a":"say \"hi\""}`},
		{"single-quoted value with escaped apostrophe", `{"a":'it\'s'}`, `{"a":"it's"}`},
		{"single-quoted value keeps other escapes", `{"a":'x\ny\\z'}`, `{"a":"x\ny\\z"}`},
		{"single-quoted top-level string", `'plain'`, `"plain"`},

		// comments
		{"line comment", "{\"a\":1 // why\n}", "{\"a\":1 \n}"},
		{"line comment at end of input", "[1] // done", "[1] "},
		{"block comment", `{"a":/* note */1}`, `{"a": 1}`},
		{"block comment between elements", `[1,/* x */2]`, `[1, 2]`},
		{"comment before a trailing comma's closer", "[1, // last\n]", "[1 \n]"},

		// unquoted keys
		{"unquoted key", `{a:1}`, `{"a":1}`},
		{"unquoted keys with underscore, dollar, dash, digit", `{file_line:"x",$id:1,est-min:2,k9:3}`, `{"file_line":"x","$id":1,"est-min":2,"k9":3}`},
		{"literal as key", `{true:1}`, `{"true":1}`},
		{"unquoted key inside one element", `[{"a":1},{b:2},{"c":3}]`, `[{"a":1},{"b":2},{"c":3}]`},

		// missing closers after complete content
		{"missing object closer before array closer", `[{"a":1]`, `[{"a":1}]`},
		{"missing array closer before object closer", `{"a":[1,2}`, `{"a":[1,2]}`},
		{"top-level array missing its closer", `[{"a":1},{"b":2}`, `[{"a":1},{"b":2}]`},
		{"top-level array missing closer after trailing comma", `[{"a":1},`, `[{"a":1}]`},
		{"empty top-level array missing its closer", `[`, `[]`},
		{"top-level array missing closer after a comment", "[{\"a\":1} // end", "[{\"a\":1} ]"},

		// extra closers
		{"extra closer after value", `{"a":1}}`, `{"a":1}`},
		{"several extra closers", `[1]]}]`, `[1]`},
		{"extra closer inside an array", `[{"a":1}},{"b":2}]`, `[{"a":1},{"b":2}]`},

		// mismatched closers
		{"wrong closer for an object", `{"a":1]`, `{"a":1}`},
		{"wrong closer for an inner object", `{"x":{"a":1]}`, `{"x":{"a":1}}`},

		// several at once
		{
			"mixed slips in one finding",
			"{severity:'high', // model note\n file_line:'a.go:3', problem:'x',}",
			"{\"severity\":\"high\", \n \"file_line\":\"a.go:3\", \"problem\":\"x\"}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Repair(tc.in)
			require.True(t, ok, "Repair(%q) declined", tc.in)
			assert.Equal(t, tc.want, got)
			assert.True(t, json.Valid([]byte(got)))
		})
	}
}

// TestRepair_Declines covers what Repair must not fix: a cut-off string or
// element, a guess at a missing value, and grammar outside its defect classes.
// Each returns the input unchanged with ok=false.
func TestRepair_Declines(t *testing.T) {
	for _, in := range []string{
		``,
		`   `,
		`// only a comment`,
		`[{"a":1},{"b":"cut off`,      // open string at end of input
		`[{"a":1},{"b":'cut off`,      // open single-quoted string
		`[{"a":1},{"b":2`,             // cut-off final element
		`[{"a":1},{"b":2,`,            // cut-off final element after a comma
		`[{"a":1},{"b":`,              // cut-off after a colon
		`[{"a":1},{"b"`,               // cut-off after a key
		`{"a":1`,                      // top-level object at end of input could be a cut-off element
		`{"a":true`,                   // top-level object never closed at end of input, even after a complete value
		`{"a":"s"`,                    // same, after a complete string
		`{"a":[1]`,                    // same, after a complete array
		`{"a":{}`,                     // same, after a complete object
		`{`,                           // same, empty
		`{"findings":[{"a":1}`,        // wrapper cut off: inner array is not top-level
		`[[1,2],[3`,                   // inner array cut off
		`[1,2`,                        // a final number may itself be cut off
		`[1,/* cut off`,               // unterminated block comment
		`{"a":}`,                      // missing value: never guessed
		`{"a"}`,                       // key with no value
		`{"a":1,"b"}`,                 // key with no value before the closer
		`[1,,2]`,                      // empty element
		`[,1]`,                        // leading comma
		`{"a":1 "b":2}`,               // missing comma
		`{"a":maybe}`,                 // bare word value
		`{"a":True}`,                  // not a JSON literal
		`{"a":NaN}`,                   // not a JSON number
		`{"a":1}{"b":2}`,              // two values
		`{"a":1} trailing prose`,      // prose after the value
		`]`,                           // closer with nothing open
		`{"a":'it's'}`,                // apostrophe ends the single-quoted string
		"{\"a\":\"raw\tcontrol\"}",    // invalid string content is not edited
		`{"a":01}`,                    // invalid number is not edited
		`{"a":1 / 2}`,                 // a lone slash
		`[{"a":1},{"b":2,"c":{"d":3}`, // nested cut-off element
		`[{"a":1],{"b":2}]`,           // the ']' ends the array, so the rest is trailing text
	} {
		got, ok := Repair(in)
		assert.False(t, ok, "Repair(%q) should decline, got %q", in, got)
		assert.Equal(t, in, got)
	}
}

// TestRepair_ValidInputUnchanged pins that valid JSON is never altered.
func TestRepair_ValidInputUnchanged(t *testing.T) {
	for _, in := range []string{
		`{}`, `[]`, `null`, `1`, `"s"`, ` {"a": [1, 2.5e3, -0, true, false, null]} `,
		`{"url":"http://x.y/z","c":"/* not a comment */","q":"it's","t":"a,}"}`,
		"[\n  {\"file_line\": \"a.go:1\", \"problem\": \"x\"}\n]\n",
	} {
		got, ok := Repair(in)
		assert.False(t, ok, in)
		assert.Equal(t, in, got)
	}
}

// TestRepair_StringLiteralsUntouched pins that text inside a string literal is
// never edited: comment markers, quotes, commas, braces and bare words inside a
// string decode to the same value after repair.
func TestRepair_StringLiteralsUntouched(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]any
	}{
		{
			`{"url":"http://a.b/c//d", x:1,}`,
			map[string]any{"url": "http://a.b/c//d", "x": 1.0},
		},
		{
			`{"c":"/* keep */ and // keep", x:1,}`,
			map[string]any{"c": "/* keep */ and // keep", "x": 1.0},
		},
		{
			`{"s":"a,} ] , }", 'k':'v',}`,
			map[string]any{"s": "a,} ] , }", "k": "v"},
		},
		{
			`{"q":"it's 'quoted'", "e":"say \"x\"", y:2,}`,
			map[string]any{"q": "it's 'quoted'", "e": `say "x"`, "y": 2.0},
		},
		{
			`{'s':'{"a":1,} // not a comment /* nor this */', z:true,}`,
			map[string]any{"s": `{"a":1,} // not a comment /* nor this */`, "z": true},
		},
		{
			`{"w":"unquoted: key, true", 'u':'é ✓ \u00e9',}`,
			map[string]any{"w": "unquoted: key, true", "u": "é ✓ é"},
		},
	}
	for _, tc := range cases {
		got, ok := Repair(tc.in)
		require.True(t, ok, tc.in)
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &m), got)
		assert.Equal(t, tc.want, m, tc.in)
	}
}

// TestRepair_Idempotent pins that a repaired value repairs to itself.
func TestRepair_Idempotent(t *testing.T) {
	for _, in := range []string{
		`{'a':1,}`, `[{"a":1]`, `{"a":1}}`, "[1, // c\n 2,]", `[{"a":1},`, `{a:[1,2}`,
	} {
		once, ok := Repair(in)
		require.True(t, ok, in)
		twice, ok2 := Repair(once)
		assert.False(t, ok2, in)
		assert.Equal(t, once, twice, in)
	}
}

// TestRepair_LinearTime runs inputs built to be quadratic for a naive repair —
// deep nesting closed by unmatched closers, many trailing commas, a long
// comment and many re-quoted strings — at 1 MiB each. A quadratic pass over
// any of them would take minutes; the bound is loose enough for -race on CI.
func TestRepair_LinearTime(t *testing.T) {
	const n = 1 << 20
	inputs := map[string]string{
		"deep nesting then unmatched closers": strings.Repeat("[", n/4) + strings.Repeat("}", n/4),
		"deep objects then one array closer":  "[" + strings.Repeat(`{"a":`, n/10) + "1" + "]",
		"many trailing commas":                "[" + strings.Repeat("[1,],", n/6) + "]",
		"long comment":                        "[1, /*" + strings.Repeat("x", n) + "*/]",
		"many single-quoted strings":          "[" + strings.Repeat("'ab',", n/5) + "'c']",
		"comma then long whitespace":          "[1," + strings.Repeat(" ", n) + "]",
		"many extra closers":                  "[1]" + strings.Repeat("]", n),
	}
	for name, in := range inputs {
		start := time.Now()
		_, _ = Repair(in)
		assert.Less(t, time.Since(start), 10*time.Second, name)
	}
}

// FuzzRepair checks the invariants on arbitrary input: no panic, ok=true
// implies valid JSON, valid input is returned unchanged with ok=false, and a
// repaired value repairs to itself.
func FuzzRepair(f *testing.F) {
	for _, seed := range []string{
		`{"a":1,}`, `{'a':'b'}`, "{\"a\":1 // c\n}", `{a:1}`, `[{"a":1]`, `{"a":1}}`,
		`[{"a":1},{"b":`, `[1,2`, `{"a":"x`, `{'a':'it\'s'}`, `[/* x */]`, `{"a":[1,2}`,
		`[{"a":1}},{"b":2}]`, `{"a":1]`, `"\`, `'\`, `/*`, `[`, `{`, `}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, ok := Repair(in)
		if json.Valid([]byte(in)) {
			if ok || out != in {
				t.Fatalf("valid input altered: %q -> %q (ok=%v)", in, out, ok)
			}
			return
		}
		if !ok {
			if out != in {
				t.Fatalf("declined repair altered input: %q -> %q", in, out)
			}
			return
		}
		if !json.Valid([]byte(out)) {
			t.Fatalf("ok=true but output invalid: %q -> %q", in, out)
		}
		if again, ok2 := Repair(out); ok2 || again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", in, out, again)
		}
	})
}
