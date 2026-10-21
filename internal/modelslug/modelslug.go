// Package modelslug validates the unhashed model slug carried by the outbound
// telemetry payloads (epic 35.17 AC6). It is a stdlib-only leaf so both
// internal/scorecard and internal/telemetry can share one validator without either
// depending on the other.
package modelslug

// MaxLen is the longest model slug sent on the wire, in bytes.
const MaxLen = 128

// Sanitize returns model unchanged when it is a plain catalog slug, and the empty
// string otherwise. A plain slug is at most MaxLen bytes, each one of
// [A-Za-z0-9._:/~@+-]; so a slug containing a control character, whitespace, or any
// other byte (including any non-ASCII byte) is sent as an empty model. The model is
// the one payload field that is not hashed, so it must never carry free text.
func Sanitize(model string) string {
	if len(model) > MaxLen {
		return ""
	}
	for i := 0; i < len(model); i++ {
		if !allowed(model[i]) {
			return ""
		}
	}
	return model
}

func allowed(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	}
	switch c {
	case '.', '_', ':', '/', '~', '@', '+', '-':
		return true
	}
	return false
}
