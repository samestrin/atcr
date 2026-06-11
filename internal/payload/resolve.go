package payload

import (
	"fmt"
	"strings"
)

// DefaultMode is the built-in payload mode when no tier configures one. Small
// and MoE models read real code better than unified diffs, so blocks wins by
// default (original-requirements.md clarification, 2026-06-10).
const DefaultMode = ModeBlocks

// validModes is the frozen v1 enum. registry.payloadModeValid duplicates this
// set by hand because the package boundary forbids importing it here.
var validModes = map[PayloadMode]bool{ModeDiff: true, ModeBlocks: true, ModeFiles: true}

// ValidMode reports whether s names a known payload mode (lowercase, exact).
func ValidMode(s string) bool {
	return validModes[PayloadMode(s)]
}

// ParseMode validates s and returns the typed mode. Empty/whitespace is unset
// and resolves to DefaultMode; any other unknown value is a hard error.
func ParseMode(s string) (PayloadMode, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return DefaultMode, nil
	}
	if !ValidMode(v) {
		return "", fmt.Errorf("invalid payload mode '%s': must be one of diff, blocks, files", v)
	}
	return PayloadMode(v), nil
}
