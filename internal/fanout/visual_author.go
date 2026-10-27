package fanout

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/samestrin/atcr/internal/atomicwrite"
	"github.com/samestrin/atcr/internal/payload"
)

// VisualPersonaName is the frontend QA persona that authors the browser spec
// (call 1) and reviews its screenshots (call 2) (Epic 36.0.2).
const VisualPersonaName = "felix"

// visualSpecDir is the directory, relative to the repo under review, the
// authored spec is written to. Writing it is the one deliberate write into the
// work tree a review makes; the spec is meant to stay and run in the repo's CI.
const visualSpecDir = "e2e"

// specNameRe is the shape of an authored spec's base name. The engine derives
// the name from the run id; the model never names the file.
var specNameRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

var (
	// ErrInvalidSpecName rejects a run id that does not make a safe spec name
	// (felix-<run id> must match [a-z0-9-]{1,64}): a dot, a slash, "..", an
	// upper-case letter or an over-long id never reaches the file system.
	ErrInvalidSpecName = errors.New("visual spec: run id does not make a safe spec name")
	// ErrNoSpecBlock is returned when the reply holds no non-empty, closed
	// ```ts or ```typescript block.
	ErrNoSpecBlock = errors.New("visual spec: reply holds no fenced ts or typescript block")
	// ErrSpecExists refuses to overwrite a file already at the spec path: it may
	// be the operator's own spec.
	ErrSpecExists = errors.New("visual spec: a file already exists at the spec path")
)

// SpecAuthor is the felix agent as call 1 sees it: Persona returns the persona
// template the prompt is rendered from, and Complete sends the prompt and
// returns the reply. The CLI builds the production implementation, as it does
// for ThreatMapper.
type SpecAuthor interface {
	Persona() string
	Complete(ctx context.Context, prompt string) (string, error)
}

// VisualAuthorRequest is one call-1 invocation.
type VisualAuthorRequest struct {
	// RepoDir is the root of the repo under review; the spec is written to
	// RepoDir/e2e/felix-<RunID>.spec.ts.
	RepoDir string
	// RunID names the spec. It must leave felix-<RunID> within [a-z0-9-]{1,64}.
	RunID string
	// Prompt is the data the persona template is rendered with. AgentName
	// defaults to VisualPersonaName.
	Prompt payload.PayloadContext
}

// AuthorVisualSpec runs call 1: it renders the persona, asks the model for a
// Playwright spec, extracts the first fenced ts or typescript block and writes
// it to e2e/felix-<run id>.spec.ts with internal/atomicwrite. It returns the
// spec path relative to RepoDir, with forward slashes.
//
// Nothing is written unless the reply holds a spec, and an existing file at
// the spec path is never overwritten (ErrSpecExists), including one that
// appears while the model is answering. The name and the path are checked
// before the model is called, so a doomed run spends no provider call.
func AuthorVisualSpec(ctx context.Context, author SpecAuthor, req VisualAuthorRequest) (string, error) {
	name, err := visualSpecName(req.RunID)
	if err != nil {
		return "", err
	}
	persona := author.Persona()
	if strings.TrimSpace(persona) == "" {
		return "", errors.New("visual spec: persona is empty")
	}
	dir := filepath.Join(req.RepoDir, visualSpecDir)
	target := filepath.Join(dir, name+".spec.ts")
	rel := visualSpecDir + "/" + name + ".spec.ts"
	if err := checkSpecDir(dir, true); err != nil {
		return "", err
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("%w: %s", ErrSpecExists, rel)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("visual spec: %w", err)
	}

	pc := req.Prompt
	if pc.AgentName == "" {
		pc.AgentName = VisualPersonaName
	}
	prompt, err := payload.RenderPrompt(persona, pc)
	if err != nil {
		return "", fmt.Errorf("visual spec: render persona: %w", err)
	}
	reply, err := author.Complete(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("visual spec: author call: %w", err)
	}
	spec, ok := extractSpecBlock(reply)
	if !ok {
		return "", ErrNoSpecBlock
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("visual spec: %w", err)
	}
	// MkdirAll follows a symlink that appeared during the call; refuse it.
	if err := checkSpecDir(dir, false); err != nil {
		return "", err
	}
	// Claim the path with O_EXCL so a file created after the check above is
	// refused too; atomicwrite then replaces only our own empty placeholder.
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("%w: %s", ErrSpecExists, rel)
		}
		return "", fmt.Errorf("visual spec: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("visual spec: %w", err)
	}
	if err := atomicwrite.WriteGroup([]atomicwrite.Entry{{Path: target, Data: []byte(spec)}}); err != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("visual spec: write %s: %w", rel, err)
	}
	return rel, nil
}

// visualSpecName derives the spec base name from the run id.
func visualSpecName(runID string) (string, error) {
	name := VisualPersonaName + "-" + runID
	if runID == "" || !specNameRe.MatchString(name) {
		return "", fmt.Errorf("%w: %q", ErrInvalidSpecName, runID)
	}
	return name, nil
}

// checkSpecDir refuses an e2e path that is not a real directory: a symlink
// could carry the write outside the repo under review. A missing directory is
// fine only when allowMissing.
func checkSpecDir(dir string, allowMissing bool) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) && allowMissing {
		return nil
	}
	if err != nil {
		return fmt.Errorf("visual spec: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("visual spec: %s is not a directory (a symlink or file is refused)", visualSpecDir)
	}
	return nil
}

// extractSpecBlock returns the body of the first closed, non-empty fenced
// block whose info string starts with ts or typescript, with a trailing
// newline. A fence of another language is skipped whole, so a ```ts line
// inside it does not count. A fence opened with N backticks closes on a line
// of at least N backticks.
func extractSpecBlock(reply string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(reply, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		ticks, info := fenceOpen(lines[i])
		if ticks == 0 {
			continue
		}
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if fenceCloses(lines[j], ticks) {
				end = j
				break
			}
		}
		if end < 0 {
			return "", false
		}
		lang := ""
		if f := strings.Fields(info); len(f) > 0 {
			lang = strings.ToLower(f[0])
		}
		if lang == "ts" || lang == "typescript" {
			body := strings.Join(lines[i+1:end], "\n")
			if strings.TrimSpace(body) == "" {
				return "", false
			}
			return strings.TrimRight(body, "\n") + "\n", true
		}
		i = end
	}
	return "", false
}

// fenceOpen reports the backtick count and info string of an opening fence
// line, or 0 when line is not one.
func fenceOpen(line string) (int, string) {
	s := strings.TrimSpace(line)
	n := len(s) - len(strings.TrimLeft(s, "`"))
	if n < 3 {
		return 0, ""
	}
	info := s[n:]
	if strings.Contains(info, "`") {
		return 0, ""
	}
	return n, strings.TrimSpace(info)
}

// fenceCloses reports whether line closes a fence opened with ticks backticks.
func fenceCloses(line string, ticks int) bool {
	s := strings.TrimSpace(line)
	return len(s) >= ticks && strings.Trim(s, "`") == ""
}
