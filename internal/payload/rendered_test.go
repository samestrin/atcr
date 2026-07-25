package payload

import (
	"strings"
	"testing"
)

// gitDiffTwoFiles is a two-file `git diff` payload of the shape ModeDiff and
// ModeBlocks produce.
const gitDiffTwoFiles = `diff --git a/alpha.go b/alpha.go
index 1111111..2222222 100644
--- a/alpha.go
+++ b/alpha.go
@@ -1,3 +1,4 @@
 package alpha

+// added
 func A() {}
diff --git a/beta/beta.go b/beta/beta.go
index 3333333..4444444 100644
--- a/beta/beta.go
+++ b/beta/beta.go
@@ -1,2 +1,2 @@
 package beta
-func B() {}
+func B() int { return 1 }
`

// looseDiffTwoFiles has no `diff --git` boundary — the shape an externally
// ingested diff file takes after BuildEntriesFromDiff round-trips it.
const looseDiffTwoFiles = `--- a/one.txt
+++ b/one.txt
@@ -1 +1 @@
-one
+ONE
--- a/two.txt
+++ b/two.txt
@@ -1 +1 @@
-two
+TWO
`

func TestEntriesFromRenderedPayload_GitDiffSplitsPerFile(t *testing.T) {
	got := EntriesFromRenderedPayload(ModeDiff, gitDiffTwoFiles)

	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(got), got)
	}
	if got[0].Path != "alpha.go" {
		t.Errorf("entry 0 path = %q, want alpha.go", got[0].Path)
	}
	if got[1].Path != "beta/beta.go" {
		t.Errorf("entry 1 path = %q, want beta/beta.go", got[1].Path)
	}
	// The bodies must reconstruct the input exactly: an audit consumer hashes
	// them, so a dropped or duplicated byte is a wrong hash.
	var joined strings.Builder
	for _, e := range got {
		joined.WriteString(e.Body)
	}
	if joined.String() != gitDiffTwoFiles {
		t.Errorf("bodies do not round-trip the input:\ngot:\n%s", joined.String())
	}
	for i, e := range got {
		if e.Size != int64(len(e.Body)) {
			t.Errorf("entry %d size = %d, want %d", i, e.Size, len(e.Body))
		}
	}
}

func TestEntriesFromRenderedPayload_LooseDiffSplitsPerFile(t *testing.T) {
	got := EntriesFromRenderedPayload(ModeDiff, looseDiffTwoFiles)

	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(got), got)
	}
	if got[0].Path != "one.txt" || got[1].Path != "two.txt" {
		t.Errorf("paths = %q, %q; want one.txt, two.txt", got[0].Path, got[1].Path)
	}
}

func TestEntriesFromRenderedPayload_BinaryMarkerIsItsOwnEntry(t *testing.T) {
	// A binary file contributes a marker line, not a diff section. It must not
	// be swallowed into a neighbouring file's body, which would corrupt that
	// file's hash and lose the binary file from the record entirely.
	text := "[binary file changed: assets/logo.png]\n" + gitDiffTwoFiles

	got := EntriesFromRenderedPayload(ModeDiff, text)

	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(got), got)
	}
	if got[0].Path != "assets/logo.png" {
		t.Errorf("entry 0 path = %q, want assets/logo.png", got[0].Path)
	}
	if got[1].Path != "alpha.go" {
		t.Errorf("entry 1 path = %q, want alpha.go", got[1].Path)
	}
}

func TestEntriesFromRenderedPayload_FilesMode(t *testing.T) {
	text := `=== FILE: cmd/main.go ===
package main

func main() {}
=== FILE: new/name.go (renamed from old/name.go) ===
package renamed
[deleted file: gone.txt]
`

	got := EntriesFromRenderedPayload(ModeFiles, text)

	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(got), got)
	}
	want := []string{"cmd/main.go", "new/name.go", "gone.txt"}
	for i, w := range want {
		if got[i].Path != w {
			t.Errorf("entry %d path = %q, want %q", i, got[i].Path, w)
		}
	}
	if !strings.Contains(got[0].Body, "func main() {}") {
		t.Errorf("entry 0 body lost its content: %q", got[0].Body)
	}
}

// TestEntriesFromRenderedPayload_NoTrailingNewline: a payload whose last line
// is a marker with no trailing newline (a budget pass that shed everything
// after it, say) must still yield that entry rather than dropping it.
func TestEntriesFromRenderedPayload_NoTrailingNewline(t *testing.T) {
	got := EntriesFromRenderedPayload(ModeFiles, "[binary file changed: logo.png]")

	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(got), got)
	}
	if got[0].Path != "logo.png" {
		t.Errorf("path = %q, want logo.png", got[0].Path)
	}
	if got[0].Body != "[binary file changed: logo.png]" {
		t.Errorf("body = %q, want the marker verbatim", got[0].Body)
	}
}

func TestEntriesFromRenderedPayload_NeverErrorsOnUnparseableInput(t *testing.T) {
	// The audit seam must degrade to "no code context" rather than failing the
	// review it is observing. Every one of these is a payload shape this helper
	// cannot attribute to files.
	cases := map[string]struct {
		mode PayloadMode
		text string
	}{
		"empty":            {ModeDiff, ""},
		"whitespace only":  {ModeDiff, "   \n\t\n"},
		"prose":            {ModeDiff, "here is some text that is not a diff at all\n"},
		"unknown mode":     {PayloadMode("nonsense"), gitDiffTwoFiles},
		"combined diff":    {ModeDiff, "diff --cc merged.go\n@@@ -1,1 -1,1 +1,1 @@@\n a\n"},
		"truncated header": {ModeDiff, "--- a/only-an-old-header.txt\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// The contract is "returns something usable and does not panic".
			got := EntriesFromRenderedPayload(tc.mode, tc.text)
			for _, e := range got {
				if e.Body == "" {
					t.Errorf("emitted an entry with an empty body: %+v", e)
				}
			}
		})
	}
}

func TestEntriesFromRenderedPayload_UnattributableSectionKeepsContent(t *testing.T) {
	// A section whose path cannot be determined still carries its bytes: an
	// unattributed record beats a missing one for an auditor.
	text := "diff --cc merged.go\n@@@ -1,1 -1,1 +1,1 @@@\n context\n"

	got := EntriesFromRenderedPayload(ModeDiff, text)

	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(got), got)
	}
	if got[0].Body != text {
		t.Errorf("body = %q, want the section verbatim", got[0].Body)
	}
}

func TestEntriesFromRenderedPayload_BodiesAliasInput(t *testing.T) {
	// Bodies must be substrings of the input, not copies: the fan-out engine
	// threads these onto every observed invocation, and copying would duplicate
	// the whole payload per agent.
	got := EntriesFromRenderedPayload(ModeDiff, gitDiffTwoFiles)
	if len(got) == 0 {
		t.Fatal("no entries")
	}
	for i, e := range got {
		if !strings.Contains(gitDiffTwoFiles, e.Body) {
			t.Errorf("entry %d body is not a substring of the input", i)
		}
	}
}
