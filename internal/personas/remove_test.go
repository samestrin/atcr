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
}
