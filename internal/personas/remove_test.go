package personas

import (
	"testing"

	builtins "github.com/samestrin/atcr/personas"
)

func TestIsBuiltin_KnownAndUnknown(t *testing.T) {
	for _, name := range builtins.Names() {
		if !isBuiltin(name) {
			t.Errorf("isBuiltin(%q) = false, want true", name)
		}
	}
	if isBuiltin("definitely-not-a-built-in") {
		t.Errorf("isBuiltin(\"definitely-not-a-built-in\") = true, want false")
	}
	if isBuiltin("") {
		t.Errorf("isBuiltin(\"\") = true, want false")
	}
	// Community persona names are not builtins — they resolve from the embedded
	// community/*.md library, not the builtin names slice.
	for _, name := range builtins.CommunityNames() {
		if isBuiltin(name) {
			t.Errorf("isBuiltin(%q) = true, want false (community persona)", name)
		}
	}
}
