package registry

import "strings"

// DefaultSecretGuard is the embedded payload-egress secret-guard policy (Epic
// 35.20). It was "warn" until v1.3.0, which still sent a detected credential to
// the provider; the owner chose "redact" (EST-8260): reviews keep dispatching,
// with each detected credential replaced by a placeholder.
const DefaultSecretGuard = "redact"

// secretGuardAllowed is the allowed set, in the order load errors list it.
const secretGuardAllowed = "off, warn, redact, block"

// validSecretGuardPolicies is the secret_guard policy set (Epic 35.20). Before a
// review prompt leaves for a provider, secret_guard selects what happens when
// the egress scan finds a credential in it:
//
//   - "off"    no scan — the prompt is sent unchanged;
//   - "warn"   log the hit count and kinds, send unchanged;
//   - "redact" (default) replace each hit span with a placeholder before sending;
//   - "block"  fail the run naming agent, kind and file (never the value).
//
// Enforcement lives in internal/fanout, not here — this package only parses,
// validates, and resolves the value.
var validSecretGuardPolicies = map[string]bool{
	"off":    true,
	"warn":   true,
	"redact": true,
	"block":  true,
}

// secretGuardValid reports whether a configured secret_guard policy is
// acceptable at load. Empty or whitespace-only is treated as unset (valid — it
// falls through to the next precedence tier), mirroring onOverflowValid.
func secretGuardValid(value string) bool {
	v := strings.TrimSpace(value)
	return v == "" || validSecretGuardPolicies[v]
}
