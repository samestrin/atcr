package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/quickstart"
	"github.com/samestrin/atcr/internal/registry"
	builtins "github.com/samestrin/atcr/personas"
)

// quickstartOpts carries the inputs for an `atcr quickstart` run. Streams and
// the browser-open hook are injectable so the interactive flow is unit-testable
// without a TTY or a real browser.
type quickstartOpts struct {
	dir    string
	force  bool
	open   bool
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	openFn func(string) error
}

// newQuickstartCmd builds `atcr quickstart`: the interactive onboarding wizard.
// It scaffolds the same .atcr/ workspace as `atcr init` (reusing its writers),
// then layers on an interactive synthetic-provider + key-env setup and a CI
// workflow scaffold so a new user reaches their first review quickly.
func newQuickstartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quickstart",
		Short: "Interactive onboarding: scaffold config, provider, and a CI workflow",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return err
			}
			open, err := cmd.Flags().GetBool("open")
			if err != nil {
				return err
			}
			return runQuickstart(quickstartOpts{
				dir:    ".",
				force:  force,
				open:   open,
				in:     cmd.InOrStdin(),
				out:    cmd.OutOrStdout(),
				errOut: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing configuration and workflow files")
	cmd.Flags().Bool("open", false, "open the provider signup page in a browser")
	return cmd
}

// runQuickstart orchestrates the onboarding wizard. It first reuses `atcr init`'s
// writers to lay down .atcr/config.yaml and the editable personas, then (in later
// steps) sets up the synthetic provider, guides the user through the API-key env
// var, and scaffolds a CI workflow.
func runQuickstart(o quickstartOpts) error {
	// Layer onto an existing .atcr rather than aborting: `atcr init`'s writers are
	// all-or-nothing (any existing target is a hard error without --force), which
	// would block a returning user — and --force would clobber their edited
	// personas. So when a config already exists and force is off, skip the init
	// step and continue with provider/key/workflow setup (mirroring the per-file
	// skip guards used for the registry and workflow below).
	cfgPath := registry.DefaultProjectConfigPath(o.dir)
	if _, statErr := os.Lstat(cfgPath); statErr == nil && !o.force {
		_, _ = fmt.Fprintf(o.errOut, "Using existing workspace at %s (run with --force to regenerate config + personas).\n", filepath.Dir(cfgPath))
	} else if err := runInit(o.dir, o.force, o.out, o.errOut); err != nil {
		return err
	}

	manifest, err := quickstart.LoadManifest()
	if err != nil {
		return err
	}

	// The project roster init just wrote lists the persona names; define one
	// synthetic-bound agent per persona so the roster resolves.
	if err := writeSyntheticRegistry(o, manifest, builtins.Names()); err != nil {
		return err
	}

	if err := keyEnvFlow(o, manifest); err != nil {
		return err
	}

	if err := scaffoldWorkflow(o, manifest); err != nil {
		return err
	}
	return nil
}

// scaffoldWorkflow writes .github/workflows/atcr.yml under the target dir. The
// guard is per-file: an existing workflow is never overwritten without --force,
// and a skip only skips this file — it never aborts the wizard (the .atcr and
// registry setup already ran). A genuine write/stat error is returned.
func scaffoldWorkflow(o quickstartOpts, m *quickstart.Manifest) error {
	wfPath := filepath.Join(o.dir, ".github", "workflows", "atcr.yml")

	_, statErr := os.Lstat(wfPath)
	switch {
	case statErr == nil && !o.force:
		_, _ = fmt.Fprintf(o.errOut, "\nA workflow already exists at %s — not overwriting it (use --force to replace).\n", wfPath)
		return nil
	case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
		return fmt.Errorf("cannot check %s: %w", wfPath, statErr)
	}

	if err := os.MkdirAll(filepath.Dir(wfPath), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(wfPath), err)
	}
	if err := os.WriteFile(wfPath, []byte(quickstart.WorkflowYAML(m)), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", wfPath, err)
	}
	_, _ = fmt.Fprintf(o.out, "  created %s\n", wfPath)
	return nil
}

// keyEnvFlow shows the referral signup link, optionally opens it, then walks the
// user through setting the provider's API-key environment variable. The key is
// held only transiently: it is echoed back as an `export` line and, if the user
// names a shell profile, appended there — but it is NEVER written into any file
// atcr owns (.atcr/ or registry.yaml), preserving atcr's env-resolved posture.
func keyEnvFlow(o quickstartOpts, m *quickstart.Manifest) error {
	env := m.Provider.APIKeyEnv
	link := m.SignupLink()

	_, _ = fmt.Fprintf(o.out, "\nSign up for a %s API key:\n  %s\n", m.Provider.Name, osc8(link))
	if o.open {
		openFn := o.openFn
		if openFn == nil {
			openFn = openBrowser
		}
		if err := openFn(link); err != nil {
			_, _ = fmt.Fprintf(o.errOut, "could not open browser (%v) — open the link above manually.\n", err)
		}
	}

	scanner := bufio.NewScanner(o.in)
	readLine := func(prompt string) (string, bool) {
		_, _ = fmt.Fprint(o.out, prompt)
		if scanner.Scan() {
			return strings.TrimSpace(scanner.Text()), true
		}
		return "", false
	}

	key, keyOK := readLine(fmt.Sprintf("\nPaste your API key (or press Enter to set %s yourself later): ", env))
	if !keyOK {
		if err := scanner.Err(); err != nil {
			_, _ = fmt.Fprintf(o.errOut, "could not read input: %v\n", err)
		}
		return nil
	}
	if key == "" {
		_, _ = fmt.Fprintf(o.out, "\nNo problem. When you have a key, set it with:\n  export %s=<your-key>\n", env)
		return nil
	}

	_, _ = fmt.Fprintf(o.out, "\nSet it in your current shell:\n  export %s=%s\n", env, shellSingleQuote(key))

	profile, profileOK := readLine("\nAppend this export to a shell profile? Enter a path (or Enter to skip): ")
	if !profileOK {
		if err := scanner.Err(); err != nil {
			_, _ = fmt.Fprintf(o.errOut, "could not read input: %v\n", err)
		}
		return nil
	}
	if profile == "" {
		return nil
	}
	// Guard the invariant: the key must never land in a file atcr owns, even if
	// the user names one at this prompt.
	if profileIsAtcrOwned(profile, o.dir) {
		_, _ = fmt.Fprintf(o.errOut, "Refusing to write the key into an atcr-owned file (%s) — choose a shell profile like ~/.zshrc instead.\n", profile)
		return nil
	}
	if err := appendExport(profile, env, key); err != nil {
		_, _ = fmt.Fprintf(o.errOut, "could not append to %s: %v\n", profile, err)
		return nil
	}
	_, _ = fmt.Fprintf(o.out, "Appended the export to %s — open a new shell or `source` it to load the key.\n", profile)
	if err := scanner.Err(); err != nil {
		_, _ = fmt.Fprintf(o.errOut, "input error: %v\n", err)
	}
	return nil
}

// appendExport appends `export ENV='key'` to the named shell profile, expanding
// a leading ~/ and creating the file if absent. This is the one place the key
// value touches disk, and only into a file the user explicitly named — never a
// file atcr owns.
func appendExport(profile, env, key string) error {
	if strings.HasPrefix(profile, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		profile = filepath.Join(home, profile[2:])
	}
	f, err := os.OpenFile(profile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "\n# added by atcr quickstart\nexport %s=%s\n", env, shellSingleQuote(key))
	return err
}

// profileIsAtcrOwned reports whether the shell-profile path the user named would
// resolve inside the .atcr/ workspace or to the user registry — the files whose
// key-free posture the wizard must preserve. It is the enforcement point behind
// the "key never in an atcr-owned file" invariant at the profile prompt.
func profileIsAtcrOwned(profile, dir string) bool {
	abs := resolveProfilePath(profile)
	if abs == "" {
		return false
	}
	if atcr, err := filepath.Abs(filepath.Join(dir, ".atcr")); err == nil {
		if abs == atcr || strings.HasPrefix(abs, atcr+string(os.PathSeparator)) {
			return true
		}
	}
	if reg, err := registry.DefaultRegistryPath(); err == nil {
		if regAbs, err := filepath.Abs(reg); err == nil && abs == regAbs {
			return true
		}
	}
	return false
}

// resolveProfilePath expands a leading ~/ and returns the absolute form of a
// user-supplied profile path, or "" if it cannot be resolved.
func resolveProfilePath(profile string) string {
	if strings.HasPrefix(profile, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			profile = filepath.Join(home, profile[2:])
		}
	}
	abs, err := filepath.Abs(profile)
	if err != nil {
		return ""
	}
	return abs
}

// shellSingleQuote wraps s in single quotes safe for POSIX shells, escaping any
// embedded single quote via the '\” idiom so a key with odd characters cannot
// break out of the quoting (or forge shell into running injected commands).
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// osc8 wraps a URL in an OSC-8 terminal hyperlink whose visible text is the URL
// itself, so it renders clickable in supporting terminals and still shows the
// plain URL everywhere else.
func osc8(url string) string {
	return "\x1b]8;;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\"
}

// openBrowser opens url in the platform default browser. It returns promptly
// (Start, not Run) so the wizard never blocks on the browser process.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// writeSyntheticRegistry writes the synthetic provider + agents to the user
// registry (~/.config/atcr/registry.yaml). It is non-destructive: an existing
// registry is never clobbered without --force. When one exists and force is off,
// the generated block is printed for the user to merge by hand rather than
// silently overwriting their providers/agents.
func writeSyntheticRegistry(o quickstartOpts, m *quickstart.Manifest, roster []string) error {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return err
	}
	content := quickstart.RegistryYAML(m, roster)

	_, statErr := os.Lstat(regPath)
	switch {
	case statErr == nil && !o.force:
		// Exists and no force: do not touch it. Show the block to merge, and warn
		// that the roster just written to .atcr/config.yaml references these agents
		// — until they are merged, `atcr review` will fail to resolve the roster.
		_, _ = fmt.Fprintf(o.errOut, "\nA registry already exists at %s — not overwriting it (use --force to replace).\n", regPath)
		_, _ = fmt.Fprintln(o.errOut, "Add the following synthetic provider + agents to it manually:")
		_, _ = fmt.Fprintf(o.out, "\n%s\n", content)
		_, _ = fmt.Fprintf(o.errOut, "Until you merge these, `atcr review` will fail: .atcr/config.yaml lists agents (%s) your registry does not define yet.\n", strings.Join(roster, ", "))
		return nil
	case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
		return fmt.Errorf("cannot check %s: %w", regPath, statErr)
	}

	if err := os.MkdirAll(filepath.Dir(regPath), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(regPath), err)
	}
	if err := os.WriteFile(regPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", regPath, err)
	}
	_, _ = fmt.Fprintf(o.out, "  created %s\n", regPath)
	return nil
}
