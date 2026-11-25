package fanout

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrUnknownBrowserBackend is returned by ResolveBrowserBackend for a name no
// backend registered under. The wrapped message lists the registered names.
var ErrUnknownBrowserBackend = errors.New("fanout: unknown browser backend")

// BrowserBackend runs a browser spec against a preview page and reports what
// the page did. The spec is model-written code, so an implementation MUST run it
// only through a sandbox.Backend (no network, read-only snapshot). The
// interface is the extension point for runners other than Playwright; a new one
// is added with RegisterBrowserBackend and needs no change to the engine.
type BrowserBackend interface {
	// Name identifies the backend for ResolveBrowserBackend, diagnostics and
	// the evidence trail.
	Name() string
	// Preflight verifies the backend can run: its tooling and the sandbox it
	// runs through are usable. It MUST pass before Run is used.
	Preflight(ctx context.Context) error
	// Run executes run and returns what the browser saw. err is reserved for
	// backend faults (no project to run, sandbox failure, unreadable report); a
	// failing spec or a timeout is reported in the result, not as an error.
	Run(ctx context.Context, run BrowserRun) (BrowserResult, error)
}

// BrowserRun describes one browser check.
type BrowserRun struct {
	// RepoDir is the absolute path of the snapshot of the repo under review.
	RepoDir string
	// Spec is the spec file to run, relative to RepoDir.
	Spec string
	// PreviewURL is the page the spec opens.
	PreviewURL string
	// Timeout bounds the run; zero means the backend's default.
	Timeout time.Duration
}

// BrowserResult is what a browser run brought back.
type BrowserResult struct {
	// Backend is the Name of the backend that produced the result.
	Backend string
	// Screenshots are the image files the run wrote.
	Screenshots []BrowserScreenshot
	// Console holds the page's console messages.
	Console []ConsoleEntry
	// NetworkFailures holds the page's failed requests.
	NetworkFailures []NetworkFailure
	// FailedTests holds the tests that did not pass.
	FailedTests []BrowserTestFailure
	// ExitCode is the runner's exit status.
	ExitCode int
	// TimedOut is true when the run exceeded its timeout and was killed.
	TimedOut bool
}

// BrowserTestFailure is one test the runner reported as failed.
type BrowserTestFailure struct {
	// Title is the test's full title, suite titles included.
	Title string
	// File is the spec file the test lives in, when the runner reported it.
	File string
	// Status is the runner's final status, for example "failed" or "timedOut".
	Status string
	// Error is the first error message, when there is one.
	Error string
}

// BrowserScreenshot is one screenshot file returned from the run.
type BrowserScreenshot struct {
	// Name is the attachment name the runner gave the screenshot.
	Name string
	// Path is the file's path as the runner reported it.
	Path string
	// Data is the file's bytes, empty when the file was not returned.
	Data []byte
}

// ConsoleEntry is one console message from the page.
type ConsoleEntry struct {
	// Level is the console method, for example "error" or "warning".
	Level string
	Text  string
	// Location is where the message came from (URL and line), when known.
	Location string
}

// NetworkFailure is one request the page made that failed.
type NetworkFailure struct {
	URL    string
	Method string
	// Status is the HTTP status, 0 when the request got no response.
	Status int
	// Error is the failure text when the request got no response.
	Error string
}

// The registry below is the extension point for named alternative backends; it
// is deliberately not where production visual review gets its browser.
//
// Production visual review builds the backend itself: newVisualStage in
// cli/review_visual.go (around lines 63-74) resolves the run's sandbox with
// verify.ResolveExecBackend, constructs NewPlaywrightBackend with that sandbox,
// runs its Preflight and injects the result straight into fanout.VisualStage as
// the Browser field. Playwright is therefore not registered here. It needs a
// sandbox resolved per run, which a package-level registry filled at import
// time cannot supply, and the stage already holds the one backend it will use;
// looking the same backend up by name would add a resolution step that can only
// fail or find a differently sandboxed instance.
//
// RegisterBrowserBackend stays the extension point for named alternative
// backends (backends that do not need a per-run sandbox of their own), so a new
// runner is added without changing the engine or the visual stage.
var (
	browserBackendsMu sync.RWMutex
	browserBackends   = map[string]BrowserBackend{}
)

// RegisterBrowserBackend adds b to the registry under b.Name(). A nil backend,
// an empty name and a name already registered are errors.
func RegisterBrowserBackend(b BrowserBackend) error {
	if b == nil {
		return errors.New("fanout: cannot register a nil browser backend")
	}
	name := b.Name()
	if name == "" {
		return errors.New("fanout: browser backend name must not be empty")
	}
	browserBackendsMu.Lock()
	defer browserBackendsMu.Unlock()
	if _, ok := browserBackends[name]; ok {
		return fmt.Errorf("fanout: browser backend %q is already registered", name)
	}
	browserBackends[name] = b
	return nil
}

// ResolveBrowserBackend returns the backend registered under name. An unknown
// name returns ErrUnknownBrowserBackend with the registered names listed.
func ResolveBrowserBackend(name string) (BrowserBackend, error) {
	browserBackendsMu.RLock()
	b, ok := browserBackends[name]
	browserBackendsMu.RUnlock()
	if ok {
		return b, nil
	}
	known := "none"
	if names := BrowserBackendNames(); len(names) > 0 {
		known = strings.Join(names, ", ")
	}
	return nil, fmt.Errorf("%w %q (registered: %s)", ErrUnknownBrowserBackend, name, known)
}

// BrowserBackendNames returns the registered backend names, sorted.
func BrowserBackendNames() []string {
	browserBackendsMu.RLock()
	defer browserBackendsMu.RUnlock()
	names := make([]string, 0, len(browserBackends))
	for name := range browserBackends {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
