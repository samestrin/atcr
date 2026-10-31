package fanout

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/atomicwrite"
)

// visualEvidenceDir is the review-dir subdirectory the browser run's evidence
// is written to (Epic 36.0.3); findings and reports cite files under it by
// their visual/<file> path.
const visualEvidenceDir = "visual"

// The captured-text files, fixed names that no screenshot can take: every
// screenshot name ends in an image extension.
const (
	visualConsoleFile = "console.txt"
	visualNetworkFile = "network.txt"
)

// visualEvidenceNameMax caps a screenshot's file name, extension included.
const visualEvidenceNameMax = 64

// visualImageExts are the extensions a screenshot file keeps; any other is
// replaced by .png, Playwright's default.
var visualImageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true}

// VisualArtifacts are the browser run's evidence as written under the review
// dir, each a slash-separated visual/<file> path relative to it. A field is
// empty when nothing of that kind was written.
type VisualArtifacts struct {
	Screenshots []string
	Console     string
	Network     string
}

// persistVisualEvidence writes res's screenshots, and its console and
// failed-request text, to <reviewDir>/visual/ in one atomicwrite group and
// returns their relative paths. The text passes guardSecrets under policy
// first, as it does on its way to the vision agent, so block writes nothing
// and returns ErrSecretGuardBlocked, and redact writes the redacted text.
// Screenshot pixels are not scanned. File names come from the runner's
// attachment names through visualEvidenceName, so none can leave visual/.
// Console and failed-request text is persisted even when res holds no
// screenshot bytes. Nothing is written, and no visual/ is created, when
// reviewDir is empty or res holds no screenshot bytes and no captured text.
func persistVisualEvidence(ctx context.Context, policy, reviewDir string, res BrowserResult) (VisualArtifacts, error) {
	var shots []BrowserScreenshot
	for _, sc := range res.Screenshots {
		if len(sc.Data) > 0 {
			shots = append(shots, sc)
		}
	}
	if reviewDir == "" || (len(shots) == 0 && len(res.Console) == 0 && len(res.NetworkFailures) == 0) {
		return VisualArtifacts{}, nil
	}
	guarded, err := guardSecrets(ctx, policy, VisualPersonaName,
		guardSegment{text: consoleText(res.Console), file: visualConsoleLabel},
		guardSegment{text: networkText(res.NetworkFailures), file: visualNetworkLabel})
	if err != nil {
		return VisualArtifacts{}, err
	}

	dir := filepath.Join(reviewDir, visualEvidenceDir)
	var (
		out     VisualArtifacts
		entries []atomicwrite.Entry
		used    = map[string]bool{visualConsoleFile: true, visualNetworkFile: true}
	)
	for _, sc := range shots {
		name := visualEvidenceName(sc, used)
		entries = append(entries, atomicwrite.Entry{Path: filepath.Join(dir, name), Data: sc.Data})
		out.Screenshots = append(out.Screenshots, path.Join(visualEvidenceDir, name))
	}
	if guarded[0] != "" {
		entries = append(entries, atomicwrite.Entry{Path: filepath.Join(dir, visualConsoleFile), Data: []byte(guarded[0])})
		out.Console = path.Join(visualEvidenceDir, visualConsoleFile)
	}
	if guarded[1] != "" {
		entries = append(entries, atomicwrite.Entry{Path: filepath.Join(dir, visualNetworkFile), Data: []byte(guarded[1])})
		out.Network = path.Join(visualEvidenceDir, visualNetworkFile)
	}

	// A visual/ already in the review dir must be a real directory: a planted
	// symlink would carry the writes outside it.
	if fi, err := os.Lstat(dir); err == nil {
		if !fi.IsDir() {
			return VisualArtifacts{}, fmt.Errorf("visual evidence: %s exists and is not a directory", dir)
		}
	} else if err := os.Mkdir(dir, 0o755); err != nil {
		return VisualArtifacts{}, fmt.Errorf("visual evidence: %w", err)
	}
	if err := atomicwrite.WriteGroup(entries); err != nil {
		return VisualArtifacts{}, fmt.Errorf("visual evidence: %w", err)
	}
	return out, nil
}

// visualEvidenceName maps a screenshot onto a file name of [a-z0-9._-] with
// no "..", no leading or trailing "." or "-", at most visualEvidenceNameMax
// bytes, and not already in used (which it is added to); a clash takes the
// first free -2, -3, ... suffix. The stem is the attachment name (else the
// runner's file name, else "screenshot"); the extension is the name's or the
// path's image extension, else .png.
func visualEvidenceName(sc BrowserScreenshot, used map[string]bool) string {
	raw := sc.Name
	if raw == "" {
		raw = filepath.Base(sc.Path)
	}
	ext := strings.ToLower(filepath.Ext(raw))
	if visualImageExts[ext] {
		raw = strings.TrimSuffix(raw, filepath.Ext(raw))
	} else if ext = strings.ToLower(filepath.Ext(sc.Path)); !visualImageExts[ext] {
		ext = ".png"
	}
	stem := visualSafeStem(raw)
	for n := 1; ; n++ {
		suffix := ""
		if n > 1 {
			suffix = "-" + strconv.Itoa(n)
		}
		s := stem
		if max := visualEvidenceNameMax - len(suffix) - len(ext); len(s) > max {
			s = strings.TrimRight(s[:max], ".-")
		}
		if s == "" {
			s = "screenshot"
		}
		name := s + suffix + ext
		if !used[name] {
			used[name] = true
			return name
		}
	}
}

// visualSafeStem lower-cases s, turns every rune outside [a-z0-9._-] into
// "-", collapses runs of "-" and of ".", and trims "." and "-" from both
// ends, so the result holds no path separator and no "..".
func visualSafeStem(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	for strings.Contains(out, "--") || strings.Contains(out, "..") {
		out = strings.ReplaceAll(strings.ReplaceAll(out, "--", "-"), "..", ".")
	}
	return strings.Trim(out, ".-")
}
