package reconcile

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// VisualDir is the review-dir subdirectory a visual run writes its screenshots
// and console log into (Epic 36.0.3). Findings name those files in EVIDENCE as
// "visual/<file>".
const VisualDir = "visual"

// Console excerpt budget: the head of the log, cut at whichever bound is hit
// first, so a chatty page cannot bloat findings.json.
const (
	visualExcerptMaxLines = 20
	visualExcerptMaxBytes = 2048
)

// visualRefPattern matches a "visual/..." token that starts the EVIDENCE text or
// follows a separator. Requiring the separator keeps a repo path such as
// "src/visual/logo.png" from reading as a sidecar reference.
var visualRefPattern = regexp.MustCompile("(?:^|[\\s(\\[{<,;:='\"`])(" + VisualDir + "/[^\\s()\\[\\]{}<>,;'\"`]+)")

var (
	screenshotExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true}
	consoleLogExts = map[string]bool{".log": true, ".txt": true}
)

// visualRefs returns the distinct "visual/<file>" references in evidence, in
// order of first appearance. Trailing sentence punctuation is trimmed, and only
// a plain file name directly under visual/ is kept: a nested path, "." or ".."
// is dropped, so a reference can never leave the visual directory.
func visualRefs(evidence string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range visualRefPattern.FindAllStringSubmatch(evidence, -1) {
		ref := strings.TrimRight(m[1], ".:!?")
		name := strings.TrimPrefix(ref, VisualDir+"/")
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

// stampVisualEvidence fills VisualEvidence on each finding whose EVIDENCE names
// sidecar files under reviewDir/visual (Epic 36.0.3): image references become
// Screenshots, and the first log reference becomes ConsoleLog with the head of
// the file as ConsoleExcerpt. A reference counts only when the file exists as a
// regular file inside a real (not symlinked) visual directory, so a finding that
// merely mentions such a path is left untouched. It mutates jf in place and is a
// no-op when reviewDir is empty or has no visual directory.
func stampVisualEvidence(jf []JSONFinding, reviewDir string) {
	if reviewDir == "" || len(jf) == 0 {
		return
	}
	dir := filepath.Join(reviewDir, VisualDir)
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return
	}
	for i := range jf {
		var ve VisualEvidence
		for _, ref := range visualRefs(jf[i].Evidence) {
			ext := strings.ToLower(filepath.Ext(ref))
			isShot := screenshotExts[ext]
			if !isShot && (!consoleLogExts[ext] || ve.ConsoleLog != "") {
				continue
			}
			full := filepath.Join(dir, strings.TrimPrefix(ref, VisualDir+"/"))
			if fi, err := os.Lstat(full); err != nil || !fi.Mode().IsRegular() {
				continue
			}
			if isShot {
				ve.Screenshots = append(ve.Screenshots, ref)
				continue
			}
			ve.ConsoleLog = ref
			ve.ConsoleExcerpt = consoleExcerpt(full)
		}
		if len(ve.Screenshots) > 0 || ve.ConsoleLog != "" {
			jf[i].VisualEvidence = &ve
		}
	}
}

// consoleExcerpt reads the head of the log at path within the excerpt budget,
// cutting on a line and rune boundary. A read error yields an empty excerpt;
// the log path is still recorded by the caller.
func consoleExcerpt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, visualExcerptMaxBytes))
	if err != nil {
		return ""
	}
	s := string(b)
	if lines := strings.SplitN(s, "\n", visualExcerptMaxLines+1); len(lines) > visualExcerptMaxLines {
		s = strings.Join(lines[:visualExcerptMaxLines], "\n")
	}
	// The byte cut can split a rune; dropping invalid bytes only shortens s.
	return strings.TrimRight(strings.ToValidUTF8(s, ""), "\r\n")
}
