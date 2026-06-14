package payload

import (
	"strings"
	"testing"
)

func TestScopeFocus(t *testing.T) {
	if got := ScopeFocus(nil); got != "" {
		t.Fatalf("ScopeFocus(nil) = %q, want empty", got)
	}
	if got := ScopeFocus([]string{}); got != "" {
		t.Fatalf("ScopeFocus([]) = %q, want empty", got)
	}

	single := ScopeFocus([]string{"performance"})
	if !strings.Contains(single, "performance") {
		t.Fatalf("ScopeFocus single = %q, want it to mention performance", single)
	}

	multi := ScopeFocus([]string{"performance", "efficiency"})
	for _, want := range []string{"performance", "efficiency"} {
		if !strings.Contains(multi, want) {
			t.Fatalf("ScopeFocus multi = %q, want it to mention %q", multi, want)
		}
	}
	// Blank entries are skipped, never rendered as an empty category.
	clean := ScopeFocus([]string{"performance", "", "  "})
	if strings.Contains(clean, ", ,") || strings.Contains(clean, "  ,") {
		t.Fatalf("ScopeFocus left a blank category in %q", clean)
	}
}
