package fanout

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/sandbox"
)

// fakeSandbox is a sandbox.Backend that records every RunSpec it is handed and
// returns a canned result, or blocks forever when hang is set.
type fakeSandbox struct {
	name         string
	preflightErr error
	result       sandbox.RunResult
	err          error
	hang         chan struct{} // when non-nil, Run blocks on it and ignores ctx

	mu        sync.Mutex
	runs      []sandbox.RunSpec
	preflight int
}

func (f *fakeSandbox) Name() string {
	if f.name == "" {
		return "fake-sandbox"
	}
	return f.name
}

func (f *fakeSandbox) Preflight(ctx context.Context) error {
	f.mu.Lock()
	f.preflight++
	f.mu.Unlock()
	return f.preflightErr
}

func (f *fakeSandbox) Run(ctx context.Context, spec sandbox.RunSpec) (sandbox.RunResult, error) {
	f.mu.Lock()
	f.runs = append(f.runs, spec)
	f.mu.Unlock()
	if f.hang != nil {
		<-f.hang
		return sandbox.RunResult{}, errors.New("released after the test")
	}
	return f.result, f.err
}

func (f *fakeSandbox) runCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}

// writeTree creates files (relative path -> content) under a fresh temp dir.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// playwrightRepo is a repo with a resolvable Playwright project and one spec.
func playwrightRepo(t *testing.T) string {
	return writeTree(t, map[string]string{
		"playwright.config.ts": "export default {}\n",
		"package.json":         `{"devDependencies":{"@playwright/test":"^1.48.0"}}`,
		"node_modules/@playwright/test/package.json": `{"name":"@playwright/test"}`,
		"e2e/home.spec.ts":                           "// spec\n",
	})
}

// listTree returns every path under dir, for asserting nothing was written.
func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func tarOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(files[n])), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(files[n]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPlaywrightBackend_RefusesDarwinOSLevel(t *testing.T) {
	repo := playwrightRepo(t)
	run := BrowserRun{RepoDir: repo, Spec: "e2e/home.spec.ts", PreviewURL: "http://127.0.0.1:3000"}

	t.Run("real OSLevelBackend", func(t *testing.T) {
		b := newPlaywrightBackendForOS(sandbox.NewOSLevelBackend(sandbox.DefaultOSLevelConfig()), "darwin")
		if err := b.Preflight(context.Background()); !errors.Is(err, ErrBrowserBackendUnsupported) {
			t.Fatalf("Preflight err = %v, want ErrBrowserBackendUnsupported", err)
		}
		if _, err := b.Run(context.Background(), run); !errors.Is(err, ErrBrowserBackendUnsupported) {
			t.Fatalf("Run err = %v, want ErrBrowserBackendUnsupported", err)
		}
	})

	t.Run("nothing runs", func(t *testing.T) {
		// A backend reporting the OS-level name is refused the same way, which
		// lets the fake prove the sandbox is never reached.
		fake := &fakeSandbox{name: "os-level"}
		b := newPlaywrightBackendForOS(fake, "darwin")
		before := listTree(t, repo)
		if err := b.Preflight(context.Background()); !errors.Is(err, ErrBrowserBackendUnsupported) {
			t.Fatalf("Preflight err = %v, want ErrBrowserBackendUnsupported", err)
		}
		if _, err := b.Run(context.Background(), run); !errors.Is(err, ErrBrowserBackendUnsupported) {
			t.Fatalf("Run err = %v, want ErrBrowserBackendUnsupported", err)
		}
		if fake.runCount() != 0 || fake.preflight != 0 {
			t.Fatalf("sandbox was used: %d runs, %d preflights", fake.runCount(), fake.preflight)
		}
		if after := listTree(t, repo); !slices.Equal(before, after) {
			t.Fatalf("repo changed: before %v after %v", before, after)
		}
	})

	t.Run("linux OS-level is not refused", func(t *testing.T) {
		fake := &fakeSandbox{name: "os-level", result: sandbox.RunResult{Files: map[string][]byte{playwrightReportPath: []byte(`{"suites":[]}`)}}}
		b := newPlaywrightBackendForOS(fake, "linux")
		if _, err := b.Run(context.Background(), run); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if fake.runCount() != 1 {
			t.Fatalf("runs = %d, want 1", fake.runCount())
		}
	})
}

func TestPlaywrightBackend_NoProject(t *testing.T) {
	cases := map[string]map[string]string{
		"empty repo": {"e2e/home.spec.ts": "// spec\n"},
		"no config": {
			"package.json": `{"devDependencies":{"@playwright/test":"^1.48.0"}}`,
			"node_modules/@playwright/test/package.json": `{}`,
			"e2e/home.spec.ts":                           "// spec\n",
		},
		"config but no @playwright/test": {
			"playwright.config.js": "module.exports = {}\n",
			"package.json":         `{"devDependencies":{"vitest":"^1"}}`,
			"e2e/home.spec.ts":     "// spec\n",
		},
		"declared but not installed": {
			"playwright.config.ts": "export default {}\n",
			"package.json":         `{"devDependencies":{"@playwright/test":"^1.48.0"}}`,
			"e2e/home.spec.ts":     "// spec\n",
		},
		"config is a directory": {
			"playwright.config.ts/x":                     "",
			"node_modules/@playwright/test/package.json": `{}`,
			"e2e/home.spec.ts":                           "// spec\n",
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			repo := writeTree(t, files)
			before := listTree(t, repo)
			fake := &fakeSandbox{}
			b := newPlaywrightBackendForOS(fake, "linux")
			_, err := b.Run(context.Background(), BrowserRun{RepoDir: repo, Spec: "e2e/home.spec.ts"})
			if !errors.Is(err, ErrNoPlaywrightProject) {
				t.Fatalf("Run err = %v, want ErrNoPlaywrightProject", err)
			}
			if fake.runCount() != 0 {
				t.Fatalf("sandbox ran %d times, want 0", fake.runCount())
			}
			if after := listTree(t, repo); !slices.Equal(before, after) {
				t.Fatalf("repo changed (a file was written): before %v after %v", before, after)
			}
		})
	}
}

func TestPlaywrightBackend_RunsNpxPlaywrightInSandbox(t *testing.T) {
	repo := playwrightRepo(t)
	fixture, err := os.ReadFile(filepath.Join("testdata", "playwright_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	shot := ".atcr-browser/test-results/home-failed-1.png"
	report := bytes.ReplaceAll(fixture, []byte("/work/test-results/home-failed-1.png"), []byte("/work/"+shot))
	png := []byte("\x89PNG fake")
	extra := []byte("\x89PNG extra")
	fake := &fakeSandbox{result: sandbox.RunResult{
		ExitCode: 1,
		Files: map[string][]byte{
			playwrightReportPath:      report,
			playwrightScreenshotsPath: tarOf(t, map[string][]byte{shot: png, ".atcr-browser/test-results/extra.png": extra}),
		},
	}}
	b := newPlaywrightBackendForOS(fake, "linux")

	res, err := b.Run(context.Background(), BrowserRun{RepoDir: repo, Spec: "e2e/home.spec.ts", PreviewURL: "http://127.0.0.1:3000"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.runCount() != 1 {
		t.Fatalf("runs = %d, want 1", fake.runCount())
	}
	got := fake.runs[0]

	npx := []string{"npx", "playwright", "test", "e2e/home.spec.ts", "--reporter=json", "--output=" + playwrightOutputDir}
	if !containsRun(got.Command, npx) {
		t.Fatalf("argv %q does not contain %q", got.Command, npx)
	}
	// The whole RunSpec is pinned, so any allowance added later (a network
	// opt-in, a host mount) fails here rather than slipping through.
	want := sandbox.RunSpec{
		Command:     got.Command,
		Timeout:     DefaultPlaywrightTimeout,
		SnapshotDir: repo,
		Writable:    true,
		Collect:     []string{playwrightReportPath, playwrightScreenshotsPath},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RunSpec = %+v\nwant     %+v", got, want)
	}
	shell := strings.Join(got.Command, " ")
	for _, s := range []string{"NODE_PATH=/src/node_modules", "PLAYWRIGHT_JSON_OUTPUT_NAME=" + playwrightReportPath} {
		if !strings.Contains(shell, s) {
			t.Fatalf("command %q missing %q", shell, s)
		}
	}
	if strings.Contains(shell, "127.0.0.1:3000") {
		t.Fatalf("preview URL interpolated into the command: %q", shell)
	}

	if res.Backend != "playwright" || res.ExitCode != 1 || res.TimedOut {
		t.Fatalf("result header = %q/%d/%v", res.Backend, res.ExitCode, res.TimedOut)
	}
	if len(res.Console) != 2 || len(res.NetworkFailures) != 1 || len(res.FailedTests) == 0 {
		t.Fatalf("parsed result = %+v", res)
	}
	if len(res.Screenshots) != 2 {
		t.Fatalf("screenshots = %+v, want the reported one and the extra file", res.Screenshots)
	}
	if res.Screenshots[0].Path != "/work/"+shot || !bytes.Equal(res.Screenshots[0].Data, png) {
		t.Fatalf("reported screenshot = %+v, want its bytes attached", res.Screenshots[0])
	}
	if !bytes.Equal(res.Screenshots[1].Data, extra) {
		t.Fatalf("extra screenshot = %+v", res.Screenshots[1])
	}
}

func containsRun(haystack, needle []string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if slices.Equal(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func TestPlaywrightBackend_PassesPreviewURLByEnv(t *testing.T) {
	repo := playwrightRepo(t)
	fake := &fakeSandbox{result: sandbox.RunResult{Files: map[string][]byte{playwrightReportPath: []byte(`{"suites":[]}`)}}}
	b := newPlaywrightBackendForOS(fake, "linux")
	if _, err := b.Run(context.Background(), BrowserRun{RepoDir: repo, Spec: "e2e/home.spec.ts", PreviewURL: "http://localhost:5173/a b'$(x)"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	cmd := fake.runs[0].Command
	if !slices.Contains(cmd, "ATCR_PREVIEW_URL=http://localhost:5173/a b'$(x)") {
		t.Fatalf("preview URL not passed as one env argv element: %q", cmd)
	}
}

func TestPlaywrightBackend_HungRunIsCancelledAtTimeout(t *testing.T) {
	repo := playwrightRepo(t)
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	fake := &fakeSandbox{hang: hang}
	b := newPlaywrightBackendForOS(fake, "linux")

	start := time.Now()
	res, err := b.Run(context.Background(), BrowserRun{RepoDir: repo, Spec: "e2e/home.spec.ts", Timeout: 50 * time.Millisecond})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run err = %v, want a timed-out result", err)
	}
	if !res.TimedOut || res.Backend != "playwright" {
		t.Fatalf("result = %+v, want TimedOut from the playwright backend", res)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %v, want it cut at the 50ms timeout", elapsed)
	}
	if got := fake.runs[0].Timeout; got != 50*time.Millisecond {
		t.Fatalf("sandbox Timeout = %v, want the run's 50ms so the sandbox kills the process group", got)
	}
}

func TestPlaywrightBackend_SandboxReportsTimeout(t *testing.T) {
	repo := playwrightRepo(t)
	fake := &fakeSandbox{result: sandbox.RunResult{TimedOut: true, ExitCode: 124}}
	res, err := newPlaywrightBackendForOS(fake, "linux").Run(context.Background(), BrowserRun{RepoDir: repo, Spec: "e2e/home.spec.ts"})
	if err != nil {
		t.Fatalf("Run err = %v", err)
	}
	if !res.TimedOut || res.ExitCode != 124 {
		t.Fatalf("result = %+v, want timed out", res)
	}
}

func TestPlaywrightBackend_ParentCancelIsAnError(t *testing.T) {
	repo := playwrightRepo(t)
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	b := newPlaywrightBackendForOS(&fakeSandbox{hang: hang}, "linux")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	_, err := b.Run(ctx, BrowserRun{RepoDir: repo, Spec: "e2e/home.spec.ts", Timeout: time.Minute})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run err = %v, want context.Canceled", err)
	}
}

func TestPlaywrightBackend_Errors(t *testing.T) {
	repo := playwrightRepo(t)
	cases := []struct {
		name string
		spec string
		sb   *fakeSandbox
		want string
	}{
		{"empty spec", "", &fakeSandbox{}, "spec"},
		{"absolute spec", "/etc/passwd", &fakeSandbox{}, "spec"},
		{"escaping spec", "../x.spec.ts", &fakeSandbox{}, "spec"},
		{"flag spec", "--config=evil.ts", &fakeSandbox{}, "spec"},
		{"missing spec", "e2e/nope.spec.ts", &fakeSandbox{}, "spec"},
		{"sandbox fault", "e2e/home.spec.ts", &fakeSandbox{err: errors.New("docker gone")}, "docker gone"},
		{"no report", "e2e/home.spec.ts", &fakeSandbox{result: sandbox.RunResult{ExitCode: 1, Output: "Error: Cannot find module '@playwright/test'"}}, "Cannot find module"},
		{"bad report", "e2e/home.spec.ts", &fakeSandbox{result: sandbox.RunResult{Files: map[string][]byte{playwrightReportPath: []byte("not json")}}}, "parse playwright report"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newPlaywrightBackendForOS(tc.sb, "linux").Run(context.Background(), BrowserRun{RepoDir: repo, Spec: tc.spec})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run err = %v, want it to mention %q", err, tc.want)
			}
			if strings.HasPrefix(tc.name, "sandbox") || strings.HasPrefix(tc.name, "no report") || strings.HasPrefix(tc.name, "bad report") {
				return
			}
			if tc.sb.runCount() != 0 {
				t.Fatalf("sandbox ran for an invalid spec")
			}
		})
	}
}

func TestPlaywrightBackend_RelativeRepoDirRefused(t *testing.T) {
	fake := &fakeSandbox{}
	_, err := newPlaywrightBackendForOS(fake, "linux").Run(context.Background(), BrowserRun{RepoDir: "rel", Spec: "e2e/home.spec.ts"})
	if err == nil || fake.runCount() != 0 {
		t.Fatalf("Run err = %v runs = %d, want a refusal before any run", err, fake.runCount())
	}
}

func TestPlaywrightBackend_Preflight(t *testing.T) {
	t.Run("docker with the default image is refused", func(t *testing.T) {
		b := NewPlaywrightBackend(sandbox.NewDockerBackend(sandbox.DefaultDockerConfig()))
		if err := b.Preflight(context.Background()); !errors.Is(err, ErrPlaywrightImageRequired) {
			t.Fatalf("Preflight err = %v, want ErrPlaywrightImageRequired", err)
		}
	})
	t.Run("docker with no image is refused", func(t *testing.T) {
		cfg := sandbox.DefaultDockerConfig()
		cfg.Image = "  "
		b := NewPlaywrightBackend(sandbox.NewDockerBackend(cfg))
		if err := b.Preflight(context.Background()); !errors.Is(err, ErrPlaywrightImageRequired) {
			t.Fatalf("Preflight err = %v, want ErrPlaywrightImageRequired", err)
		}
	})
	t.Run("docker with an operator image reaches the sandbox preflight", func(t *testing.T) {
		cfg := sandbox.DefaultDockerConfig()
		cfg.Image = "mcr.microsoft.com/playwright:v1.48.0-jammy"
		cfg.DockerPath = filepath.Join(t.TempDir(), "no-docker")
		b := NewPlaywrightBackend(sandbox.NewDockerBackend(cfg))
		err := b.Preflight(context.Background())
		if err == nil || errors.Is(err, ErrPlaywrightImageRequired) {
			t.Fatalf("Preflight err = %v, want the sandbox's own failure", err)
		}
	})
	t.Run("sandbox preflight failure propagates", func(t *testing.T) {
		fake := &fakeSandbox{preflightErr: errors.New("no bwrap")}
		if err := newPlaywrightBackendForOS(fake, "linux").Preflight(context.Background()); err == nil || !strings.Contains(err.Error(), "no bwrap") {
			t.Fatalf("Preflight err = %v", err)
		}
	})
	t.Run("nil sandbox", func(t *testing.T) {
		if err := NewPlaywrightBackend(nil).Preflight(context.Background()); err == nil {
			t.Fatal("Preflight with no sandbox succeeded")
		}
		if _, err := NewPlaywrightBackend(nil).Run(context.Background(), BrowserRun{}); err == nil {
			t.Fatal("Run with no sandbox succeeded")
		}
	})
	t.Run("name", func(t *testing.T) {
		if got := NewPlaywrightBackend(&fakeSandbox{}).Name(); got != "playwright" {
			t.Fatalf("Name = %q", got)
		}
	})
}
