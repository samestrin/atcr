// Package jsonrepair fixes the common syntax slips a model makes when it writes
// JSON, as a last try after a strict decode has failed.
//
// It repairs syntax only and never invents data: trailing commas, single-quoted
// strings, // and /* */ comments, unquoted object keys, and mismatched, extra or
// missing closers after complete content. The scan is string-aware, so the text
// inside a string literal is never edited (a single-quoted string is re-quoted,
// and its decoded value is unchanged). It never closes an open string literal
// or an element cut off by end of input: a cut-off reply still ends at its last
// complete element, and a half-written element is never admitted.
//
// The package is a stdlib-only leaf: it imports no internal package.
package jsonrepair

import (
	"encoding/json"
	"strings"
)

// Repair returns s with its JSON syntax slips fixed and true, or s unchanged
// and false when s is already valid JSON or cannot be repaired without
// guessing. ok=true always means json.Valid(out). Repair is idempotent (its
// output is valid, so a second call returns it unchanged with false) and runs
// in time linear in len(s).
//
// A closer that matches no open container is first dropped as an extra closer;
// only if that leaves invalid JSON is it read as a wrong closer for the
// innermost open container. Missing closers are inserted only where a later
// closer proves the container ended, or at end of input for a top-level array
// whose last element is complete. Any other container still open at end of
// input is a cut-off element, and Repair declines.
func Repair(s string) (string, bool) {
	if json.Valid([]byte(s)) {
		return s, false
	}
	out, ok, sawUnmatched := repairPass(s, false)
	if !ok && sawUnmatched {
		out, ok, _ = repairPass(s, true)
	}
	if !ok || !json.Valid([]byte(out)) {
		return s, false
	}
	return out, true
}

// Parser states of the innermost open container. Outside every container,
// repairPass tracks only whether the top-level value is done.
const (
	stObjKey       = iota // after '{': a key or '}'
	stObjKeyComma         // after ',' in an object: a key, or '}' (trailing comma)
	stObjColon            // after a key: ':'
	stObjValue            // after ':': a value
	stArrElem             // after '[': a value or ']'
	stArrElemComma        // after ',' in an array: a value, or ']' (trailing comma)
	stAfterValue          // after a member or element: ',' or the closer
)

type frame struct {
	open     byte // '{' or '['
	state    int
	commaPos int // index in out of the last ',' emitted in this frame
}

// repairPass makes one linear scan over s. replaceUnmatched picks how a closer
// that matches no open container is read: false drops it as extra, true reads
// it as the innermost container's closer. sawUnmatched reports whether such a
// closer was met inside a container, so the caller knows a second pass could
// differ.
func repairPass(s string, replaceUnmatched bool) (out string, ok, sawUnmatched bool) {
	b := make([]byte, 0, len(s)+8)
	var stack []frame
	var openCount [2]int // open '{' and '[' frames on the stack, so a closer finds its match in O(1)
	topDone := false
	lastNumber := false // the last value token was a number, which may be cut off

	// valueDone records that a complete value was emitted.
	valueDone := func() {
		if len(stack) == 0 {
			topDone = true
			return
		}
		stack[len(stack)-1].state = stAfterValue
	}
	// expectsValue reports whether a value may start here.
	expectsValue := func() bool {
		if len(stack) == 0 {
			return !topDone
		}
		switch stack[len(stack)-1].state {
		case stObjValue, stArrElem, stArrElemComma:
			return true
		}
		return false
	}
	expectsKey := func() bool {
		if len(stack) == 0 {
			return false
		}
		st := stack[len(stack)-1].state
		return st == stObjKey || st == stObjKeyComma
	}
	// closeTop pops the innermost container, dropping a trailing comma and
	// writing its closer. It refuses a container that ends after a key or a
	// colon, since closing it would drop or invent a member.
	closeTop := func() bool {
		f := stack[len(stack)-1]
		switch f.state {
		case stObjKeyComma, stArrElemComma:
			b = append(b[:f.commaPos], b[f.commaPos+1:]...)
		case stObjKey, stArrElem, stAfterValue:
		default:
			return false
		}
		if f.open == '{' {
			b = append(b, '}')
		} else {
			b = append(b, ']')
		}
		stack = stack[:len(stack)-1]
		openCount[kindIndex(f.open)]--
		lastNumber = false
		valueDone()
		return true
	}

	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			b = append(b, c)
			i++

		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			i += 2
			for i < len(s) && s[i] != '\n' {
				i++
			}

		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return "", false, sawUnmatched // unterminated comment: input was cut off
			}
			b = append(b, ' ') // keeps the tokens on either side apart
			i += 2 + end + 2

		case c == '"' || c == '\'':
			isKey := expectsKey()
			if !isKey && !expectsValue() {
				return "", false, sawUnmatched
			}
			var n int
			if c == '"' {
				b, n = copyDoubleQuoted(b, s, i)
			} else {
				b, n = convertSingleQuoted(b, s, i)
			}
			if n < 0 {
				return "", false, sawUnmatched // open string at end of input
			}
			i = n
			if isKey {
				stack[len(stack)-1].state = stObjColon
			} else {
				lastNumber = false
				valueDone()
			}

		case c == ':':
			if len(stack) == 0 || stack[len(stack)-1].state != stObjColon {
				return "", false, sawUnmatched
			}
			stack[len(stack)-1].state = stObjValue
			b = append(b, c)
			i++

		case c == ',':
			if len(stack) == 0 || stack[len(stack)-1].state != stAfterValue {
				return "", false, sawUnmatched
			}
			f := &stack[len(stack)-1]
			if f.open == '{' {
				f.state = stObjKeyComma
			} else {
				f.state = stArrElemComma
			}
			f.commaPos = len(b)
			b = append(b, c)
			i++

		case c == '{' || c == '[':
			if !expectsValue() {
				return "", false, sawUnmatched
			}
			st := stObjKey
			if c == '[' {
				st = stArrElem
			}
			stack = append(stack, frame{open: c, state: st})
			openCount[kindIndex(c)]++
			b = append(b, c)
			i++

		case c == '}' || c == ']':
			i++
			want := byte('{')
			if c == ']' {
				want = '['
			}
			switch {
			case openCount[kindIndex(want)] > 0:
				// Close every container above the match, then the match.
				for {
					top := stack[len(stack)-1].open
					if !closeTop() {
						return "", false, sawUnmatched
					}
					if top == want {
						break
					}
				}
			case len(stack) == 0:
				if !topDone {
					return "", false, sawUnmatched
				}
				// An extra closer after the complete value: dropped.
			default:
				sawUnmatched = true
				if replaceUnmatched && !closeTop() {
					return "", false, sawUnmatched
				}
			}

		case c == '-' || (c >= '0' && c <= '9'):
			if !expectsValue() {
				return "", false, sawUnmatched
			}
			start := i
			for i < len(s) && isNumberByte(s[i]) {
				i++
			}
			b = append(b, s[start:i]...)
			lastNumber = true
			valueDone()

		case isIdentStart(c):
			start := i
			for i < len(s) && isIdentByte(s[i]) {
				i++
			}
			word := s[start:i]
			switch {
			case expectsKey():
				b = append(b, '"')
				b = append(b, word...)
				b = append(b, '"')
				stack[len(stack)-1].state = stObjColon
			case expectsValue() && (word == "true" || word == "false" || word == "null"):
				b = append(b, word...)
				lastNumber = false
				valueDone()
			default:
				return "", false, sawUnmatched // a bare word as a value would be a guess
			}

		default:
			return "", false, sawUnmatched
		}
	}

	if len(stack) == 0 {
		return string(b), topDone, sawUnmatched
	}
	// End of input inside a container. Only a top-level array whose last
	// element is complete is closed; anything deeper is a cut-off element, and
	// a number at the very end may itself be cut off.
	if len(stack) != 1 || stack[0].open != '[' {
		return "", false, sawUnmatched
	}
	if stack[0].state == stAfterValue && lastNumber {
		return "", false, sawUnmatched
	}
	if !closeTop() {
		return "", false, sawUnmatched
	}
	return string(b), true, sawUnmatched
}

// copyDoubleQuoted appends the double-quoted string starting at s[i] verbatim
// and returns the index just past its closing quote, or -1 if s ends first.
func copyDoubleQuoted(b []byte, s string, i int) ([]byte, int) {
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '"':
			return append(b, s[i:j+1]...), j + 1
		}
	}
	return b, -1
}

// convertSingleQuoted appends the single-quoted string starting at s[i] as a
// double-quoted string with the same decoded value, and returns the index just
// past its closing quote, or -1 if s ends first. \' becomes ', a bare " is
// escaped, and every other escape is copied as written.
func convertSingleQuoted(b []byte, s string, i int) ([]byte, int) {
	mark := len(b)
	b = append(b, '"')
	for j := i + 1; j < len(s); j++ {
		switch c := s[j]; c {
		case '\\':
			if j+1 >= len(s) {
				return b[:mark], -1
			}
			if s[j+1] == '\'' {
				b = append(b, '\'')
			} else {
				b = append(b, c, s[j+1])
			}
			j++
		case '"':
			b = append(b, '\\', '"')
		case '\'':
			return append(b, '"'), j + 1
		default:
			b = append(b, c)
		}
	}
	return b[:mark], -1
}

// kindIndex maps an opener to its openCount slot.
func kindIndex(open byte) int {
	if open == '{' {
		return 0
	}
	return 1
}

func isNumberByte(c byte) bool {
	return (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E'
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentByte(c byte) bool {
	return isIdentStart(c) || c == '-' || (c >= '0' && c <= '9')
}
