package fanout

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/sandbox"
)

// DefaultPlaywrightTimeout bounds a Playwright run when BrowserRun.Timeout is
// zero. The dev server the repo's own webServer config starts runs inside the
// same sandbox run, so its start-up counts against it.
const DefaultPlaywrightTimeout = 2 * time.Minute

// ErrNoPlaywrightProject is returned by the Playwright backend when the repo
// under review has no playwright.config.* or no installed @playwright/test. The
// backend never scaffolds one: it writes no package.json and no config.
var ErrNoPlaywrightProject = errors.New("fanout: no Playwright project in the repo under review")

// ErrBrowserBackendUnsupported is returned when the sandbox a browser backend
// would run through cannot host it. The darwin OS-level backend is refused: its
// sandbox-exec profile denies all network, loopback included, so the dev server
// and the browser inside one run cannot reach each other. Use Docker or Linux.
var ErrBrowserBackendUnsupported = errors.New("fanout: browser backend unsupported on this sandbox")

// ErrPlaywrightImageRequired is returned by Preflight when the Docker sandbox
// runs the default image, which carries neither Node nor the browsers. The
// operator sets sandbox.image to one that does (for example the official
// mcr.microsoft.com/playwright image).
var ErrPlaywrightImageRequired = errors.New("fanout: the Playwright backend needs a sandbox image with Node and the Playwright browsers")

// Paths inside the writable tree (relative to its root, /work) the run writes
// and the sandbox returns. The output directory is overridden on the command
// line so screenshots land in a known place whatever the repo's config says.
const (
	playwrightWorkDir         = ".atcr-browser"
	playwrightOutputDir       = playwrightWorkDir + "/test-results"
	playwrightReportPath      = playwrightWorkDir + "/report.json"
	playwrightScreenshotsPath = playwrightWorkDir + "/screenshots.tar"
	// playwrightMaxScreenshots bounds the files packed into the screenshots tar.
	playwrightMaxScreenshots = 64
)

// playwrightConfigNames are the config file names Playwright looks for.
var playwrightConfigNames = []string{
	"playwright.config.ts", "playwright.config.js",
	"playwright.config.mts", "playwright.config.mjs",
	"playwright.config.cts", "playwright.config.cjs",
}

// playwrightRunShell is the fixed `sh -c` text the run executes. It carries no
// data from the run: the npx argv arrives positionally as "$@". It points Node
// and npx at the snapshot's node_modules (the writable seed copy excludes
// node_modules): NODE_PATH for CommonJS, plus a node_modules link in the
// writable tree for ES modules and npx's bin lookup. It forbids npx from installing anything (there is no network),
// sends the JSON report to a file so it is neither truncated with the output nor
// mixed with the dev server's logs, then packs the screenshots under the output
// directory into one tar, since RunSpec.Collect names files, not directories.
// The command's exit status is preserved.
var playwrightRunShell = `export NODE_PATH=/src/node_modules PATH="/src/node_modules/.bin:$PATH" ` +
	`PLAYWRIGHT_JSON_OUTPUT_NAME=` + playwrightReportPath + ` npm_config_yes=false; ` +
	`[ -e node_modules ] || ln -s /src/node_modules node_modules 2>/dev/null; ` +
	`mkdir -p ` + playwrightWorkDir + `; "$@"; _atcr_rc=$?; ` +
	`find ` + playwrightOutputDir + ` -type f \( -name '*.png' -o -name '*.jpg' -o -name '*.jpeg' \) 2>/dev/null ` +
	`| head -n ` + fmt.Sprint(playwrightMaxScreenshots) + ` > ` + playwrightWorkDir + `/screenshots.list; ` +
	`[ -s ` + playwrightWorkDir + `/screenshots.list ] && tar -cf ` + playwrightScreenshotsPath +
	` -T ` + playwrightWorkDir + `/screenshots.list 2>/dev/null; exit $_atcr_rc`

// PlaywrightBackend is the BrowserBackend that runs a spec with
// `npx playwright test --reporter=json` inside a sandbox.Backend. The sandbox
// gives the run no network, a read-only snapshot at /src and a writable copy at
// /work, and kills the run's whole process group at the timeout.
type PlaywrightBackend struct {
	sb sandbox.Backend
	// goos is the host platform, runtime.GOOS outside tests.
	goos string
}

var _ BrowserBackend = (*PlaywrightBackend)(nil)

// NewPlaywrightBackend returns a Playwright backend that runs through sb.
func NewPlaywrightBackend(sb sandbox.Backend) *PlaywrightBackend {
	return newPlaywrightBackendForOS(sb, runtime.GOOS)
}

func newPlaywrightBackendForOS(sb sandbox.Backend, goos string) *PlaywrightBackend {
	return &PlaywrightBackend{sb: sb, goos: goos}
}

// Name implements BrowserBackend.
func (b *PlaywrightBackend) Name() string { return "playwright" }

// checkSandbox refuses a sandbox that cannot host the browser run.
func (b *PlaywrightBackend) checkSandbox() error {
	if b.sb == nil {
		return errors.New("fanout: the Playwright backend has no sandbox")
	}
	if b.goos == "darwin" {
		if _, ok := b.sb.(*sandbox.OSLevelBackend); ok || b.sb.Name() == "os-level" {
			return fmt.Errorf("%w: the darwin os-level sandbox denies loopback, so the dev server cannot start; use Docker or Linux", ErrBrowserBackendUnsupported)
		}
	}
	return nil
}

// Preflight implements BrowserBackend. It refuses the darwin OS-level sandbox
// and a Docker sandbox left on its default image, then runs the sandbox's own
// preflight.
func (b *PlaywrightBackend) Preflight(ctx context.Context) error {
	if err := b.checkSandbox(); err != nil {
		return err
	}
	if d, ok := b.sb.(*sandbox.DockerBackend); ok {
		img := strings.TrimSpace(d.Image())
		if img == "" || img == sandbox.DefaultDockerConfig().Image {
			return fmt.Errorf("%w: set sandbox.image (got %q)", ErrPlaywrightImageRequired, img)
		}
	}
	return b.sb.Preflight(ctx)
}

// Run implements BrowserBackend. A repo with no Playwright project returns
// ErrNoPlaywrightProject before anything runs. A run that exceeds its timeout
// returns a result with TimedOut set and a nil error.
func (b *PlaywrightBackend) Run(ctx context.Context, run BrowserRun) (BrowserResult, error) {
	if err := b.checkSandbox(); err != nil {
		return BrowserResult{}, err
	}
	if !filepath.IsAbs(run.RepoDir) {
		return BrowserResult{}, fmt.Errorf("fanout: playwright: repo dir must be absolute, got %q", run.RepoDir)
	}
	if err := probePlaywrightProject(run.RepoDir); err != nil {
		return BrowserResult{}, err
	}
	spec, err := playwrightSpecPath(run.RepoDir, run.Spec)
	if err != nil {
		return BrowserResult{}, err
	}
	timeout := run.Timeout
	if timeout <= 0 {
		timeout = DefaultPlaywrightTimeout
	}
	runSpec := sandbox.RunSpec{
		Command: []string{"/bin/sh", "-c", playwrightRunShell, "atcr-playwright",
			"env", "ATCR_PREVIEW_URL=" + run.PreviewURL,
			"npx", "playwright", "test", spec, "--reporter=json", "--output=" + playwrightOutputDir},
		Timeout:     timeout,
		SnapshotDir: run.RepoDir,
		Writable:    true,
		Collect:     []string{playwrightReportPath, playwrightScreenshotsPath},
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type outcome struct {
		res sandbox.RunResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := b.sb.Run(runCtx, runSpec)
		done <- outcome{res, err}
	}()

	timedOut := BrowserResult{Backend: b.Name(), ExitCode: 124, TimedOut: true}
	var out outcome
	select {
	case out = <-done:
	case <-runCtx.Done():
		// The sandbox kills the process group at its own deadline; this bounds
		// the call even when a backend does not return promptly.
		if ctx.Err() != nil {
			return BrowserResult{}, ctx.Err()
		}
		return timedOut, nil
	}
	if ctx.Err() != nil {
		return BrowserResult{}, ctx.Err()
	}
	if out.res.TimedOut || (out.err != nil && runCtx.Err() != nil) {
		timedOut.ExitCode = max(out.res.ExitCode, timedOut.ExitCode)
		return timedOut, nil
	}
	if out.err != nil {
		return BrowserResult{}, fmt.Errorf("fanout: playwright: sandbox run: %w", out.err)
	}
	report, ok := out.res.Files[playwrightReportPath]
	if !ok || len(report) == 0 {
		return BrowserResult{}, fmt.Errorf("fanout: playwright: no JSON report (exit %d): %s", out.res.ExitCode, strings.TrimSpace(out.res.Output))
	}
	res, err := ParsePlaywrightReport(report)
	if err != nil {
		return BrowserResult{}, err
	}
	res.Backend = b.Name()
	res.ExitCode = out.res.ExitCode
	attachScreenshots(&res, out.res.Files[playwrightScreenshotsPath])
	return res, nil
}

// probePlaywrightProject reports ErrNoPlaywrightProject unless repo has a
// playwright.config.* at its root and an installed @playwright/test. It only
// reads.
func probePlaywrightProject(repo string) error {
	hasConfig := false
	for _, name := range playwrightConfigNames {
		if fi, err := os.Stat(filepath.Join(repo, name)); err == nil && fi.Mode().IsRegular() {
			hasConfig = true
			break
		}
	}
	if !hasConfig {
		return fmt.Errorf("%w: no playwright.config.* at the repo root", ErrNoPlaywrightProject)
	}
	if fi, err := os.Stat(filepath.Join(repo, "node_modules", "@playwright", "test", "package.json")); err == nil && fi.Mode().IsRegular() {
		return nil
	}
	if declaresPlaywrightTest(filepath.Join(repo, "package.json")) {
		return fmt.Errorf("%w: @playwright/test is declared in package.json but not installed in node_modules (the run has no network to install it)", ErrNoPlaywrightProject)
	}
	return fmt.Errorf("%w: @playwright/test is not a dependency", ErrNoPlaywrightProject)
}

func declaresPlaywrightTest(pkgJSON string) bool {
	data, err := os.ReadFile(pkgJSON)
	if err != nil {
		return false
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	_, dep := pkg.Dependencies["@playwright/test"]
	_, dev := pkg.DevDependencies["@playwright/test"]
	return dep || dev
}

// playwrightSpecPath validates spec as a regular file inside repo and returns it
// cleaned and slash-separated. A spec starting with "-" is refused so it cannot
// be read as a Playwright flag.
func playwrightSpecPath(repo, spec string) (string, error) {
	if spec == "" || filepath.IsAbs(spec) || strings.HasPrefix(spec, "/") {
		return "", fmt.Errorf("fanout: playwright: spec must be a path relative to the repo, got %q", spec)
	}
	clean := path.Clean(filepath.ToSlash(spec))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "-") {
		return "", fmt.Errorf("fanout: playwright: spec %q is outside the repo or looks like a flag", spec)
	}
	if fi, err := os.Stat(filepath.Join(repo, filepath.FromSlash(clean))); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("fanout: playwright: spec %q is not a file in the repo", spec)
	}
	return clean, nil
}

// attachScreenshots reads the screenshots tar the run packed and fills the
// bytes of each reported screenshot whose path names a packed file. Packed files
// no attachment names are added as screenshots of their own. A tar cut short by
// the sandbox's collect cap yields the files read before the cut.
func attachScreenshots(res *BrowserResult, tarball []byte) {
	if len(tarball) == 0 {
		return
	}
	tr := tar.NewReader(bytes.NewReader(tarball))
	for {
		h, err := tr.Next()
		if err != nil {
			return
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(tr); err != nil {
			return
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		matched := false
		for i := range res.Screenshots {
			s := &res.Screenshots[i]
			p := path.Clean(filepath.ToSlash(s.Path))
			if len(s.Data) == 0 && (p == name || strings.HasSuffix(p, "/"+name)) {
				s.Data = buf.Bytes()
				matched = true
				break
			}
		}
		if !matched {
			res.Screenshots = append(res.Screenshots, BrowserScreenshot{Name: path.Base(name), Path: name, Data: buf.Bytes()})
		}
	}
}
