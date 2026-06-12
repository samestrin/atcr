package registry

import "strings"

// validPayloadModes is the frozen v1 payload-mode enum. It is kept in sync with
// internal/payload.PayloadMode by hand: the package boundary (registry imports
// nothing internal) forbids importing the constant, so the three values are
// duplicated deliberately. Adding a mode is a coordinated change in both places.
var validPayloadModes = map[string]bool{"diff": true, "blocks": true, "files": true}

// payloadModeValid reports whether a configured payload value is acceptable at
// load time. Empty or whitespace-only is treated as unset (valid — it falls
// through to the next precedence tier). Values are lowercase-only; no case
// normalization is performed, so "DIFF" is rejected.
func payloadModeValid(value string) bool {
	v := strings.TrimSpace(value)
	return v == "" || validPayloadModes[v]
}
