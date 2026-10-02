package reconcile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// findings_format_think_strip_doc_test.go guards docs/findings-format.md, whose
// paragraphs are one long line each — the docs this package pins are never
// hard-wrapped. The file once mixed two policies: most assertions used plain
// whole-line want strings, but a few embedded a mid-sentence "\n" in the want and
// papered over the mismatch with a flattened-fallback `||` clause. That made the
// file internally contradictory (its own header said the docs are never
// hard-wrapped) and applied wrap tolerance to 3 assertions while leaving the
// other 17 wrap-intolerant, so a future doc reflow would have broken the
// majority. This guard pins the one policy: want literals carry no embedded
// newlines, and there is no wrap-tolerance fallback anywhere in the file.
func TestFindingsFormatThinkStripDocTest_OneWrapPolicy(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("findings_format_think_strip_doc_test.go"))
	if err != nil {
		t.Fatalf("read sibling test source: %v", err)
	}
	body := string(src)

	if strings.Contains(body, `strings.ReplaceAll(want, "\n", " ")`) {
		t.Errorf("findings_format_think_strip_doc_test.go must use ONE wrap policy: found the flattened-fallback `||` clause (want strings must not embed mid-sentence newlines)")
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		// A want literal with an embedded newline shows up as a Go string literal
		// containing an escape sequence: `"...the\n..."`. Comment text may mention
		// newlines freely, so only string literals are examined.
		if !strings.Contains(trimmed, `"`) {
			continue
		}
		if strings.Contains(trimmed, `\n`) && !strings.HasPrefix(trimmed, "//") {
			t.Errorf("findings_format_think_strip_doc_test.go must use ONE wrap policy: want literal embeds a mid-sentence newline (docs are one long line per paragraph, never hard-wrapped): %s", trimmed)
		}
	}
}
