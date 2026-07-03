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

	existed, err := createExclusive(wfPath, []byte(quickstart.WorkflowYAML(m)), 0o644, o.force)
	if err != nil {
		return err
	}
	if existed {
		_, _ = fmt.Fprintf(o.errOut, "\nA workflow already exists at %s — not overwriting it (use --force to replace).\n", wfPath)
		return nil
	}
	_, _ = fmt.Fprintf(o.out, "  created %s\n", wfPath)
	return nil
}

// createExclusive atomically creates path with the given contents, honoring the
// never-overwrite-without-force contract without a check-then-write race. It
// returns existed=true (having written nothing) when the path is already present
// and force is off, so the caller can print its own skip message. The O_EXCL
// open closes the TOCTOU window a separate Lstat+WriteFile leaves open: a file
// or symlink appearing at path is never silently overwritten or followed. When
// force is set, any existing entry is removed first — os.Remove drops the link
// itself, never following it — so the scaffold is never written through a
// pre-planted symlink into an outside (or atcr-owned) file.
func createExclusive(path string, data []byte, perm os.FileMode, force bool) (existed bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	if force {
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			return false, fmt.Errorf("cannot replace %s: %w", path, rmErr)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return true, nil
		}
		return false, fmt.Errorf("failed to write %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, werr := f.Write(data); werr != nil {
		return false, fmt.Errorf("failed to write %s: %w", path, werr)
	}
	if cerr := f.Close(); cerr != nil {
		return false, fmt.Errorf("failed to write %s: %w", path, cerr)
	}
	return false, nil
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

// expandHome replaces a leading `~` or `~/` with the user's home directory.
// It returns an error if the home directory cannot be determined.
func expandHome(path string) (string, error) {
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// appendExport appends `export ENV='key'` to the named shell profile, expanding
// a leading ~/ and creating the file if absent. This is the one place the key
// value touches disk, and only into a file the user explicitly named — never a
// file atcr owns.
func appendExport(profile, env, key string) error {
	profile, err := expandHome(profile)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(profile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "\n# added by atcr quickstart\nexport %s=%s\n", env, shellSingleQuote(key)); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(profile, 0o600)
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
	// Compare the effective write targets, not the lexical paths: appendExport
	// follows symlinks, so a profile that is (or lives behind) a symlink into
	// .atcr/ must be judged by where the write actually lands. The ownership
	// checks below compare by inode identity so a case-variant path (.ATCR on a
	// case-insensitive filesystem) is caught too.
	abs = resolveEffectivePath(abs)
	if atcr, err := filepath.Abs(filepath.Join(dir, ".atcr")); err == nil {
		if pathWithinDir(abs, atcr) {
			return true
		}
	}
	if reg, err := registry.DefaultRegistryPath(); err == nil {
		if regAbs, err := filepath.Abs(reg); err == nil && sameFilePath(abs, regAbs) {
			return true
		}
	}
	return false
}

// pathWithinDir reports whether path is dir itself or lives inside it, compared
// by inode identity rather than lexical prefix — so it holds on case-insensitive
// filesystems (macOS/APFS, Windows) where ./.ATCR and ./.atcr are the same
// directory and a strings.HasPrefix check would miss. path's leaf may not exist
// yet (the profile is created on append), so it walks up path's ancestor chain
// and compares each existing ancestor against dir via os.SameFile.
func pathWithinDir(path, dir string) bool {
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return false
	}
	for p := path; ; {
		if info, err := os.Stat(p); err == nil && os.SameFile(info, dirInfo) {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}

// sameFilePath reports whether a and b are the same existing file by inode
// identity (device+inode), so a case-variant path to the user registry is caught
// on case-insensitive filesystems.
func sameFilePath(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// resolveEffectivePath returns the path a write to abs would actually land on,
// with symlinks resolved. If the leaf itself is a symlink it is dereferenced
// (os.OpenFile would follow it, even to a not-yet-existing target), and any
// symlink in the deepest existing ancestor is resolved via filepath.EvalSymlinks.
// The trailing component(s) may not exist yet — the profile is created on append —
// so only the longest existing prefix is resolved and the remaining lexical
// components are rejoined. Applied to both the candidate profile and the
// atcr-owned paths, it makes the ownership guard robust to a symlink whose target
// lands inside .atcr/ but whose own path does not.
func resolveEffectivePath(abs string) string {
	if fi, err := os.Lstat(abs); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if target, rlErr := os.Readlink(abs); rlErr == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(abs), target)
			}
			abs = filepath.Clean(target)
		}
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	dir := abs
	var tail []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs // reached the root without an existing ancestor
		}
		tail = append([]string{filepath.Base(dir)}, tail...)
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{resolved}, tail...)...)
		}
	}
}

// resolveProfilePath expands a leading ~ and returns the absolute form of a
// user-supplied profile path, or "" if it cannot be resolved.
func resolveProfilePath(profile string) string {
	profile, err := expandHome(profile)
	if err != nil {
		return ""
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

	existed, err := createExclusive(regPath, []byte(content), 0o644, o.force)
	if err != nil {
		return err
	}
	if existed {
		// Exists and no force: do not touch it. Show the block to merge, and warn
		// that the roster just written to .atcr/config.yaml references these agents
		// — until they are merged, `atcr review` will fail to resolve the roster.
		_, _ = fmt.Fprintf(o.errOut, "\nA registry already exists at %s — not overwriting it (use --force to replace).\n", regPath)
		_, _ = fmt.Fprintln(o.errOut, "Add the following synthetic provider + agents to it manually:")
		_, _ = fmt.Fprintf(o.out, "\n%s\n", content)
		_, _ = fmt.Fprintf(o.errOut, "Until you merge these, `atcr review` will fail: .atcr/config.yaml lists agents (%s) your registry does not define yet.\n", strings.Join(roster, ", "))
		return nil
	}
	_, _ = fmt.Fprintf(o.out, "  created %s\n", regPath)
	return nil
}
