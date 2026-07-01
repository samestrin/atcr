package payload

import (
	"context"
	"strings"
	"testing"
)

func TestParseFileChange_RangesAndText(t *testing.T) {
	chunk := "diff --git a/foo.go b/foo.go\n" +
		"index 111..222 100644\n" +
		"--- a/foo.go\n" +
		"+++ b/foo.go\n" +
		"@@ -4,2 +4,3 @@ func Foo() int {\n" +
		"-\treturn 1\n" +
		"+\treturn 2\n" +
		"+\t// added comment\n" +
		"+\tx := 5\n"
	fc := parseFileChange(chunk)
	if len(fc.Ranges) != 1 || fc.Ranges[0] != (LineRange{Start: 4, End: 6}) {
		t.Fatalf("ranges = %+v, want [{4 6}]", fc.Ranges)
	}
	want := map[string]bool{"return 1": true, "return 2": true, "// added comment": true, "x := 5": true}
	if len(fc.ChangedText) != len(want) {
		t.Fatalf("changed text = %v, want %d entries", fc.ChangedText, len(want))
	}
	for _, c := range fc.ChangedText {
		if !want[c] {
			t.Errorf("unexpected changed text %q", c)
		}
	}
}

func TestBuildChangedLines_Integration(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "foo.go", "package p\n\nfunc Foo() int {\n\treturn 1\n}\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "foo.go", "package p\n\nfunc Foo() int {\n\treturn 2\n}\n")
	head := commitAll(t, dir, "v2")

	cl, err := BuildChangedLines(context.Background(), dir, base, head)
	if err != nil {
		t.Fatalf("BuildChangedLines: %v", err)
	}
	fc, ok := cl["foo.go"]
	if !ok {
		t.Fatalf("foo.go not present in changed lines: %+v", cl)
	}
	found := false
	for _, r := range fc.Ranges {
		if 4 >= r.Start && 4 <= r.End {
			found = true
		}
	}
	if !found {
		t.Errorf("head line 4 (return 2) not covered by ranges %+v", fc.Ranges)
	}
	joined := strings.Join(fc.ChangedText, "|")
	if !strings.Contains(joined, "return 2") || !strings.Contains(joined, "return 1") {
		t.Errorf("changed text missing return 1/return 2: %v", fc.ChangedText)
	}
}
