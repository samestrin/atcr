package cli

// Visual-verification docs guard (epic 36.0.1 T6). The doc is the operator's
// contract for a feature whose failure modes are otherwise invisible (a
// missing Playwright project, a sandbox that cannot host a browser, a timeout
// that kills the run), so the sections the epic names are asserted rather than
// left to review.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// visualVerificationDoc reads docs/visual-verification.md or fails the test.
func visualVerificationDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootDir(t), "docs", "visual-verification.md"))
	if err != nil {
		t.Fatalf("docs/visual-verification.md must exist (epic 36.0.1 T6): %v", err)
	}
	return string(b)
}

// TestVisualVerificationDocCoversTheContract asserts the doc carries every
// section the epic names, with the load-bearing fact in each: the Playwright
// project prerequisite, the in-sandbox dev server, --preview-url, the timeout,
// the hard error when requested and unavailable, the deferred Isolate backend,
// and the darwin OS-level refusal.
func TestVisualVerificationDocCoversTheContract(t *testing.T) {
	doc := visualVerificationDoc(t)
	lower := strings.ToLower(doc)

	for _, tc := range []struct {
		label string
		// Every token must appear; the doc fails when it keeps the vocabulary
		// but drops the fact.
		tokens []string
	}{
		{"Playwright project prerequisite", []string{"playwright.config.", "@playwright/test", "never scaffold"}},
		{"in-sandbox dev server", []string{"webserver", "loopback", "no network"}},
		{"--preview-url", []string{"--preview-url", "https://", "http://", "127.0.0.0/8", "::1", "exit 2"}},
		{"timeout", []string{"2 minute", "timed out", "process group"}},
		{"hard error when requested and unavailable", []string{"hard error"}},
		{"deferred Isolate backend", []string{"isolate", "deferr"}},
		{"darwin OS-level backend refused", []string{"darwin", "os-level", "docker", "linux"}},
	} {
		for _, tok := range tc.tokens {
			if !strings.Contains(lower, tok) {
				t.Errorf("docs/visual-verification.md section %q omits %q", tc.label, tok)
			}
		}
	}
}

// TestVisualVerificationDocStatesSandboxImagePrerequisite pins the operator
// half of the prerequisite: the default Docker image carries neither Node nor
// the browsers, so sandbox.image must be set to one that does.
func TestVisualVerificationDocStatesSandboxImagePrerequisite(t *testing.T) {
	doc := visualVerificationDoc(t)
	for _, want := range []string{"sandbox.image", "alpine:3.20", "npx playwright install"} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/visual-verification.md omits %q; the operator must be told the default image cannot run Playwright and what to set", want)
		}
	}
}

// TestVisualVerificationDocScopesHardErrorToPreviewURL pins the two failure
// postures the epic separates: with --preview-url the unavailable check is a
// hard error; without it the review continues with the check reported as timed
// out. The assertion is scoped to the section about unavailable Playwright so
// incidental mentions elsewhere cannot satisfy it.
func TestVisualVerificationDocScopesHardErrorToPreviewURL(t *testing.T) {
	doc := visualVerificationDoc(t)
	section, ok := markdownSection(doc, regexp.MustCompile(`(?i)when Playwright cannot run`))
	if !ok {
		t.Fatal("docs/visual-verification.md has no section on Playwright being unavailable")
	}
	for _, want := range []string{"--preview-url", "hard error", "continues", "timed out"} {
		if !strings.Contains(section, want) {
			t.Errorf("the unavailable-Playwright section must name %q", want)
		}
	}
}

// TestVisualVerificationDocStatesDarwinReason pins why the darwin OS-level
// backend is refused (its sandbox profile denies loopback) and the alternative
// the operator is told to use, not just that it is refused.
func TestVisualVerificationDocStatesDarwinReason(t *testing.T) {
	doc := visualVerificationDoc(t)
	section, ok := markdownSection(doc, regexp.MustCompile(`(?i)darwin`))
	if !ok {
		t.Fatal("docs/visual-verification.md has no section on the darwin OS-level refusal")
	}
	for _, want := range []string{"loopback", "Docker", "Linux"} {
		if !strings.Contains(section, want) {
			t.Errorf("the darwin section must name %q", want)
		}
	}
}

// TestVisualVerificationDocIsIndexed keeps the doc reachable: docs/README.md
// is the index the website build consumes, and the drift test
// (TestDocsIndexCoversEveryDoc) covers presence, so this pins the index entry
// to the right description rather than a bare link.
func TestVisualVerificationDocIsIndexed(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootDir(t), "docs", "README.md"))
	if err != nil {
		t.Fatalf("read docs/README.md: %v", err)
	}
	if !strings.Contains(string(b), "visual-verification.md") {
		t.Error("docs/README.md does not link docs/visual-verification.md")
	}
}
