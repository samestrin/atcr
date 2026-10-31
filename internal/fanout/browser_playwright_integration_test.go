package fanout

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/sandbox"
)

// playwrightImageEnv names the Docker image the opt-in end-to-end test runs
// Playwright in, for example mcr.microsoft.com/playwright:v1.48.0-noble. Unset,
// the test skips: CI has no Node or browser image.
const playwrightImageEnv = "ATCR_PLAYWRIGHT_IMAGE"

// playwrightVersionEnv names the @playwright/test version the test installs
// into its fixture repo. Unset, it is read from the image tag (":v1.48.0-...").
// It must match the browsers the image ships.
const playwrightVersionEnv = "ATCR_PLAYWRIGHT_VERSION"

// TestIntegration_PlaywrightBackend_RealImage runs the backend end to end, in
// the real Docker sandbox and a real Playwright image, where every other test
// uses a fake sandbox. The fixture repo gets @playwright/test installed by a
// separate, networked docker run first, since the sandboxed run has no network.
func TestIntegration_PlaywrightBackend_RealImage(t *testing.T) {
	image := strings.TrimSpace(os.Getenv(playwrightImageEnv))
	if image == "" {
		t.Skipf("%s is not set", playwrightImageEnv)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not available in PATH")
	}
	version := strings.TrimSpace(os.Getenv(playwrightVersionEnv))
	if version == "" {
		if m := regexp.MustCompile(`:v(\d+\.\d+\.\d+)`).FindStringSubmatch(image); m != nil {
			version = m[1]
		}
	}
	if version == "" {
		t.Fatalf("set %s: the tag of %q carries no version", playwrightVersionEnv, image)
	}

	repo := writeTree(t, map[string]string{
		"package.json":         `{"private":true}`,
		"playwright.config.js": "module.exports = { use: { screenshot: 'only-on-failure' } };\n",
		"e2e/home.spec.js": `const { test, expect } = require('@playwright/test');

test('renders', async ({ page }) => {
  await page.setContent('<h1>hello</h1>');
  await expect(page.locator('h1')).toHaveText('hello');
});

test('fails with a screenshot', async ({ page }, testInfo) => {
  await testInfo.attach('console', { body: JSON.stringify([{ type: 'error', text: 'boom' }]), contentType: 'application/json' });
  await page.setContent('<h1>hello</h1>');
  await expect(page.locator('h1')).toHaveText('goodbye', { timeout: 1000 });
});
`,
		"e2e/empty.spec.js": "// no tests\n",
	})
	// The sandbox user (nobody) reads the snapshot; t.TempDir is owner-only.
	if err := os.Chmod(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	install := exec.Command("docker", "run", "--rm", "-v", repo+":/r", "-w", "/r",
		"--user", userSpec(), "-e", "HOME=/tmp", "-e", "npm_config_cache=/tmp/.npm",
		image, "npm", "install", "--no-save", "--no-audit", "--no-fund", "@playwright/test@"+version)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install @playwright/test@%s: %v\n%s", version, err, out)
	}

	cfg := sandbox.DefaultDockerConfig()
	cfg.Image = image
	cfg.Memory = "2g"
	cfg.PidsLimit = 1024
	cfg.ScratchSize = "512m"
	cfg.Timeout = 5 * time.Minute
	b := NewPlaywrightBackend(sandbox.NewDockerBackend(cfg))
	ctx := context.Background()
	if err := b.Preflight(ctx); err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	t.Run("a failing test reports its failure, screenshot and console", func(t *testing.T) {
		res, err := b.Run(ctx, BrowserRun{RepoDir: repo, Spec: "e2e/home.spec.js", PreviewURL: "http://127.0.0.1:1", Timeout: 5 * time.Minute})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.TimedOut || res.ExitCode == 0 {
			t.Fatalf("result header = exit %d, timed out %v; want a failed run", res.ExitCode, res.TimedOut)
		}
		if len(res.FailedTests) != 1 || !strings.Contains(res.FailedTests[0].Title, "fails with a screenshot") {
			t.Fatalf("failed tests = %+v, want the one failing test", res.FailedTests)
		}
		shot := false
		for _, s := range res.Screenshots {
			shot = shot || len(s.Data) > 0
		}
		if !shot {
			t.Fatalf("screenshots = %+v, want the failure screenshot's bytes", res.Screenshots)
		}
		if len(res.Console) != 1 || res.Console[0].Text != "boom" {
			t.Fatalf("console = %+v, want the attached entry", res.Console)
		}
	})

	t.Run("a spec with no tests is an error", func(t *testing.T) {
		res, err := b.Run(ctx, BrowserRun{RepoDir: repo, Spec: "e2e/empty.spec.js", Timeout: 5 * time.Minute})
		if err == nil {
			t.Fatalf("Run = %+v, want an error for a run in which no test ran", res)
		}
	})
}

// userSpec is the host user as docker's --user value, so the files the
// install writes into the fixture repo stay removable by t.TempDir's cleanup.
func userSpec() string {
	return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
}
