package payload

// validModes is the frozen v1 enum. registry.payloadModeValid duplicates this
// set by hand because the package boundary forbids importing it here.
var validModes = map[PayloadMode]bool{ModeDiff: true, ModeBlocks: true, ModeFiles: true}

// ValidMode reports whether s names a known payload mode (lowercase, exact).
// It is the payload-side anchor for the registry enum-parity test; mode
// defaulting lives in registry (DefaultPayloadMode), which owns load-time
// validation — ParseMode was deleted as dead code so a second, disagreeing
// copy of empty-string semantics cannot creep back in.
func ValidMode(s string) bool {
	return validModes[PayloadMode(s)]
}
