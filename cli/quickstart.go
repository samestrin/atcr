package cli

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
	"golang.org/x/term"

	"github.com/samestrin/atcr/internal/pathutil"
	commpersonas "github.com/samestrin/atcr/internal/personas"
	"github.com/samestrin/atcr/internal/quickstart"
	"github.com/samestrin/atcr/internal/registry"
	builtins "github.com/samestrin/atcr/personas"
)

// quickstartOpts carries the inputs for an `atcr quickstart` run. Streams and
// the browser-open hook are injectable so the interactive flow is unit-testable
// without a TTY or a real browser.
type quickstartOpts struct {
	dir            string
	force          bool
	open           bool
	fetchCommunity bool   // fetch-and-pin community personas; false scaffolds from embedded built-ins only (the safe default for tests). The command layer sets this from !--offline.
	preset         string // "" for the synthetic cloud funnel, or a local preset (presetLocal, presetLocal32GB, presetLocal64GB)
	in             io.Reader
	out            io.Writer
	errOut         io.Writer
	openFn         func(string) error
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
			offline, err := cmd.Flags().GetBool("offline")
			if err != nil {
				return err
			}
			preset, err := cmd.Flags().GetString("preset")
			if err != nil {
				return err
			}
			if preset != "" && resolvePreset(preset) == "" {
				return usageError(fmt.Errorf("unknown preset %q: the presets are %q, %q and %q", preset, presetLocal32GB, presetLocal64GB, presetLocal))
			}
			return runQuickstart(quickstartOpts{
				dir:            ".",
				force:          force,
				open:           open,
				fetchCommunity: !offline,
				preset:         preset,
				in:             cmd.InOrStdin(),
				out:            cmd.OutOrStdout(),
				errOut:         cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing configuration and workflow files")
	cmd.Flags().Bool("open", false, "open the provider signup page in a browser")
	cmd.Flags().Bool("offline", false, "skip the community persona fetch; scaffold from embedded built-ins only")
	cmd.Flags().String("preset", "", `"local-32gb" or "local-64gb": set up local models (Ollama) for that much GPU or unified memory, with no network, no API key and telemetry off; "local" is "local-32gb"`)
	return cmd
}

// The local presets: an Ollama provider plus the local personas picked for a
// memory tier, scaffolded with no network and telemetry off. presetLocal is an
// alias of the tier the epic 113.1 pick names.
const (
	presetLocal     = "local"
	presetLocal32GB = "local-32gb"
	presetLocal64GB = "local-64gb"
)

// resolvePreset returns the local preset a --preset value names, following
// the `local` alias, or "" when it names none.
func resolvePreset(preset string) string {
	if preset == presetLocal {
		return presetLocal32GB
	}
	if _, ok := localPresets[preset]; ok {
		return preset
	}
	return ""
}

// localAgent is one agent of a local preset: a local community persona, the
// Ollama tag it is bound to, and the registry fields that tag was benchmarked
// with in benchmarks/local-candidates.yaml (a thinking model needs its
// thinking fields and a max_tokens of at least 16384, or it returns an empty
// review).
type localAgent struct {
	name, model                            string
	maxTokens                              int
	thinkingStyle, thinking, thinkingLevel string
	responseFormat                         string
}

// localPresets is each local preset's roster: the epic 113.1 pick (owner,
// 2026-11-20), one model per persona per tier, scored on benchmarks/standard-v1
// (docs/local-models-benchmark.md). gerald is in neither tier: no candidate
// beat its gemma3 baseline, and gemma3 went to orson. The tag is what
// `ollama pull` fetches, not the persona's namespaced discovery id.
var localPresets = map[string][]localAgent{
	presetLocal32GB: {
		{name: "orson", model: "gemma3:27b-it-q4_K_M", maxTokens: 8192, responseFormat: registry.ResponseFormatJSONObject},
		{name: "liam", model: "gemma4:26b-a4b-it-q4_K_M", maxTokens: 32768, thinkingStyle: registry.ThinkingStyleReasoningEffort, thinking: registry.ThinkingOn, thinkingLevel: registry.ThinkingLevelHigh, responseFormat: registry.ResponseFormatJSONObject},
	},
	presetLocal64GB: {
		{name: "orson", model: "gemma3:27b-it-q4_K_M", maxTokens: 8192, responseFormat: registry.ResponseFormatJSONObject},
		{name: "liam", model: "qwen3.8:27b-q4_K_M", maxTokens: 32768, thinkingStyle: registry.ThinkingStyleReasoningEffort, thinking: registry.ThinkingOn, thinkingLevel: registry.ThinkingLevelHigh, responseFormat: registry.ResponseFormatJSONObject},
	},
}

// localRegistryYAML is the user registry a local preset writes: one `local`
// provider on Ollama's OpenAI-compatible endpoint and one agent per persona in
// the tier. atcr requires api_key_env even for a keyless endpoint, so it names
// OLLAMA_API_KEY and the wizard tells the user to export a placeholder.
func localRegistryYAML(preset string) string {
	var b strings.Builder
	b.WriteString("# atcr user registry — see docs/registry.md\n")
	fmt.Fprintf(&b, "# Written by `atcr quickstart --preset %s`. Every review goes to the Ollama\n", preset)
	b.WriteString("# server on localhost; no code leaves this machine. Ollama needs no key, but\n")
	b.WriteString("# atcr requires the env var to be set, so export a placeholder:\n")
	b.WriteString("#   export OLLAMA_API_KEY=local-no-key-needed\n")
	b.WriteString("providers:\n")
	b.WriteString("  local:\n")
	b.WriteString("    base_url: http://localhost:11434/v1\n")
	b.WriteString("    api_key_env: OLLAMA_API_KEY\n")
	b.WriteString("agents:\n")
	for _, a := range localPresets[preset] {
		fmt.Fprintf(&b, "  %s:\n", a.name)
		fmt.Fprintf(&b, "    persona: %s\n", a.name)
		b.WriteString("    provider: local\n")
		fmt.Fprintf(&b, "    model: %s\n", a.model)
		fmt.Fprintf(&b, "    max_tokens: %d\n", a.maxTokens)
		if a.thinkingStyle != "" {
			fmt.Fprintf(&b, "    thinking_style: %s\n", a.thinkingStyle)
			fmt.Fprintf(&b, "    thinking: %q\n", a.thinking)
			fmt.Fprintf(&b, "    thinking_level: %s\n", a.thinkingLevel)
		}
		if a.responseFormat != "" {
			fmt.Fprintf(&b, "    response_format: %s\n", a.responseFormat)
		}
	}
	return b.String()
}

// hasWorkspaceConfig reports whether dir already holds a project config that
// quickstart should layer onto. A stub atcr wrote on its own (the first-run
// telemetry notice record, see registry.LoadStubConfig) does not count: it is
// replaced by init's writers, keeping its settings (Epic 84.0 F1).
func hasWorkspaceConfig(dir string) bool {
	if _, err := os.Lstat(registry.DefaultProjectConfigPath(dir)); err != nil {
		return false
	}
	_, stub := registry.LoadStubConfig(dir)
	return !stub
}

// runQuickstart orchestrates the onboarding wizard. It first reuses `atcr init`'s
// writers to lay down .atcr/config.yaml and the editable personas, then (in later
// steps) sets up the synthetic provider, guides the user through the API-key env
// var, and scaffolds a CI workflow.
func runQuickstart(o quickstartOpts) error {
	if o.preset != "" {
		return runQuickstartLocal(o)
	}
	// Layer onto an existing .atcr rather than aborting: `atcr init`'s writers are
	// all-or-nothing (any existing target is a hard error without --force), which
	// would block a returning user — and --force would clobber their edited
	// personas. So when a config already exists and force is off, skip the init
	// step and continue with provider/key/workflow setup (mirroring the per-file
	// skip guards used for the registry and workflow below).
	cfgPath := registry.DefaultProjectConfigPath(o.dir)
	if hasWorkspaceConfig(o.dir) && !o.force {
		_, _ = fmt.Fprintf(o.errOut, "Using existing workspace at %s (run with --force to regenerate config + personas).\n", filepath.Dir(cfgPath))
	} else if err := runInit(o.dir, o.force, o.out, o.errOut); err != nil {
		return err
	}

	// Fetch-and-pin the community personas into the resolver's pin dir. When not
	// requested (offline / tests), this reproduces today's embedded-built-in-only
	// scaffold with zero network access.
	if o.fetchCommunity {
		dir, err := personasDir()
		if err != nil {
			return err
		}
		// nil roster → the index-derived community set (Option B / TD-011), the
		// same shared reconciliation source `init` consumes; builtins.Names() below
		// remains the embedded-scaffold / synthetic-registry roster, untouched.
		if err := installCommunityPersonas(personasClient, commpersonas.BaseURL(), dir, nil, o.out, o.errOut); err != nil {
			return err
		}
	}

	manifest, err := quickstart.LoadManifest()
	if err != nil {
		return err
	}

	// The project roster init just wrote lists the persona names; define one
	// synthetic-bound agent per persona so the roster resolves.
	roster := builtins.Names()
	regYAML := quickstart.RegistryYAML(manifest, roster)
	if err := writeUserRegistry(o, regYAML, "synthetic provider + agents", roster); err != nil {
		return err
	}

	if err := keyEnvFlow(o, manifest); err != nil {
		return err
	}

	if err := scaffoldWorkflow(o, manifest, regYAML); err != nil {
		return err
	}
	return nil
}

// runQuickstartLocal is `atcr quickstart --preset local-32gb|local-64gb|local`.
// It scaffolds .atcr/ with the tier's local personas as the roster (prompts from the embedded community
// library, never fetched), turns telemetry off, and writes the local provider
// and agents to the user registry. It makes no network call: there is no
// community fetch, no signup link, and no CI workflow (a hosted runner has no
// Ollama server to reach).
func runQuickstartLocal(o quickstartOpts) error {
	preset := resolvePreset(o.preset)
	if preset == "" {
		return fmt.Errorf("unknown preset %q", o.preset)
	}
	agents := localPresets[preset]
	roster := make([]string, 0, len(agents))
	for _, a := range agents {
		roster = append(roster, a.name)
	}
	cfgPath := registry.DefaultProjectConfigPath(o.dir)
	if hasWorkspaceConfig(o.dir) && !o.force {
		_, _ = fmt.Fprintf(o.errOut, "Using existing workspace at %s (run with --force to regenerate config + personas).\n", filepath.Dir(cfgPath))
		_, _ = fmt.Fprintf(o.errOut, "Its roster is unchanged: add %s to the agents list in %s to review with the local models.\n", strings.Join(roster, ", "), cfgPath)
	} else if err := runInitRoster(o.dir, o.force, roster, builtins.CommunityGet, o.out, o.errOut); err != nil {
		return err
	}

	if err := registry.SetTelemetrySetting(o.dir, false); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(o.out, "  set telemetry = false in %s\n", cfgPath)

	if err := writeUserRegistry(o, localRegistryYAML(preset), "local provider + agents", roster); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(o.out, "\nNext, start Ollama and pull the models (each persona needs its own):")
	_, _ = fmt.Fprintln(o.out, "  ollama serve &")
	for _, a := range agents {
		_, _ = fmt.Fprintf(o.out, "  ollama pull %s   # %s\n", a.model, a.name)
	}
	_, _ = fmt.Fprintln(o.out, "  export OLLAMA_API_KEY=local-no-key-needed   # any non-empty value; Ollama ignores it")
	_, _ = fmt.Fprintln(o.out, "Remove from .atcr/config.yaml any agent whose model you do not pull.")
	return nil
}

// scaffoldWorkflow writes .github/workflows/atcr.yml under the target dir. The
// guard is per-file: an existing workflow is never overwritten without --force,
// and a skip only skips this file — it never aborts the wizard (the .atcr and
// registry setup already ran). A genuine write/stat error is returned.
func scaffoldWorkflow(o quickstartOpts, m *quickstart.Manifest, registryYAML string) error {
	wfPath := filepath.Join(o.dir, ".github", "workflows", "atcr.yml")

	existed, err := createExclusive(wfPath, []byte(quickstart.WorkflowYAML(m, registryYAML)), 0o644, o.force)
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

// stdinIsNotTerminal reports whether in is an *os.File that is not an
// interactive terminal: a pipe, a regular file, or a character device such as
// /dev/null. Any other reader (a test's strings.Reader) counts as answerable.
func stdinIsNotTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	return !term.IsTerminal(int(f.Fd()))
}

// keyEnvFlow shows the referral signup link, optionally opens it, then walks the
// user through setting the provider's API-key environment variable. The key is
// held only transiently: it is echoed back as an `export` line and, if the user
// names a shell profile, appended there — but it is NEVER written into any file
// atcr owns (.atcr/ or registry.yaml), preserving atcr's env-resolved posture.
func keyEnvFlow(o quickstartOpts, m *quickstart.Manifest) error {
	env := m.Provider.APIKeyEnv
	link := m.SignupLink()

	shown := link
	if writerIsTerminal(o.out) {
		shown = osc8(link) // a terminal escape: piped or captured output gets the bare URL
	}
	_, _ = fmt.Fprintf(o.out, "\nSign up for a %s API key:\n  %s\n", m.Provider.Name, shown)
	if o.open {
		openFn := o.openFn
		if openFn == nil {
			openFn = openBrowser
		}
		if err := openFn(link); err != nil {
			_, _ = fmt.Fprintf(o.errOut, "could not open browser (%v) — open the link above manually.\n", err)
		}
	}

	// Off a TTY (an agent's or CI's open pipe, or /dev/null) nobody can answer the prompt, and
	// reading would block until EOF: print the export hint and move on.
	if stdinIsNotTerminal(o.in) {
		_, _ = fmt.Fprintf(o.out, "\nstdin is not a terminal, so quickstart will not prompt for the key. Set it with:\n  export %s=<your-key>\n", env)
		return nil
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
		// EOF (`</dev/null`): nobody answered, so say how to set the key.
		_, _ = fmt.Fprintf(o.out, "\nWhen you have a key, set it with:\n  export %s=<your-key>\n", env)
		return nil
	}
	if key == "" {
		_, _ = fmt.Fprintf(o.out, "\nNo problem. When you have a key, set it with:\n  export %s=<your-key>\n", env)
		return nil
	}

	if writerIsTerminal(o.out) {
		_, _ = fmt.Fprintf(o.out, "\nSet it in your current shell:\n  export %s=%s\n", env, shellSingleQuote(key))
	} else {
		// stdout is piped/redirected/CI-captured — echoing the pasted key here would
		// persist the secret into that capture. Mask the value; an interactive user
		// still gets the copy-pasteable line on a real terminal.
		_, _ = fmt.Fprintf(o.out, "\nSet it in your current shell:\n  export %s=****** (masked — stdout is not a terminal)\n", env)
	}

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
	if err := appendExport(profile, env, key, o.errOut); err != nil {
		_, _ = fmt.Fprintf(o.errOut, "could not append to %s: %v\n", profile, err)
		return nil
	}
	_, _ = fmt.Fprintf(o.out, "Appended the export to %s — open a new shell or `source` it to load the key.\n", profile)
	if err := scanner.Err(); err != nil {
		_, _ = fmt.Fprintf(o.errOut, "input error: %v\n", err)
	}
	return nil
}

// writerIsTerminal reports whether w is backed by a character device (an
// interactive terminal). It decides whether echoing the pasted API key back is
// safe: on a TTY the value only reaches the user's screen, but piped/redirected/
// CI-captured output would persist the secret. Detected via the stdlib
// os.ModeCharDevice bit so no external terminal dependency is required. It
// looks through runMain's stdout wrapper (stdoutFile).
func writerIsTerminal(w io.Writer) bool {
	f, ok := stdoutFile(w)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// expandHome replaces a leading `~` or `~/` with the user's home directory.
// It returns an error if the home directory cannot be determined.
func expandHome(path string) (string, error) {
	return pathutil.ExpandHome(path, os.UserHomeDir)
}

// appendExport appends `export ENV='key'` to the named shell profile, expanding
// a leading ~/ and creating the file if absent. This is the one place the key
// value touches disk, and only into a file the user explicitly named — never a
// file atcr owns.
func appendExport(profile, env, key string, errOut io.Writer) error {
	profile, err := expandHome(profile)
	if err != nil {
		return err
	}
	// Tighten to 0600 only on a file we create — a profile holding a secret should
	// not be group/other-readable. An existing profile the user named keeps its own
	// mode untouched: appendExport was asked to append, not to re-permission a file
	// it did not create.
	_, statErr := os.Stat(profile)
	created := errors.Is(statErr, fs.ErrNotExist)
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
	if created {
		return os.Chmod(profile, 0o600)
	}
	// Pre-existing profile: respect the ownership boundary (do not re-permission a
	// file we did not create), but warn if it is group/other-readable so the user
	// knows the plaintext key was appended to a file other users on the host can read.
	if info, serr := os.Stat(profile); serr == nil && info.Mode().Perm()&0o077 != 0 {
		_, _ = fmt.Fprintf(errOut, "warning: %s is group/other-readable — the exported key may be exposed to other users; consider `chmod 600 %s`\n", profile, profile)
	}
	return nil
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

// writeUserRegistry writes content (the providers + agents named by what) to
// the user registry (~/.config/atcr/registry.yaml). It is non-destructive: an
// existing registry is never clobbered without --force. When one exists and
// force is off, the generated block is printed for the user to merge by hand
// rather than silently overwriting their providers/agents.
func writeUserRegistry(o quickstartOpts, content, what string, roster []string) error {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return err
	}

	existed, err := createExclusive(regPath, []byte(content), 0o644, o.force)
	if err != nil {
		return err
	}
	if existed {
		// Exists and no force: do not touch it. When the registry already defines
		// every roster agent (a second repo under the same HOME), there is nothing
		// to merge. Otherwise show the block to merge, and warn that the roster
		// just written to .atcr/config.yaml references agents the registry lacks —
		// until they are merged, `atcr review` will fail to resolve the roster.
		_, _ = fmt.Fprintf(o.errOut, "\nA registry already exists at %s — not overwriting it (use --force to replace).\n", regPath)
		missing := missingRosterAgents(regPath, o.dir, roster)
		if len(missing) == 0 {
			_, _ = fmt.Fprintf(o.errOut, "It already defines every agent in .atcr/config.yaml (%s); nothing to merge.\n", strings.Join(roster, ", "))
			return nil
		}
		_, _ = fmt.Fprintf(o.errOut, "Add the following %s to it manually:\n", what)
		_, _ = fmt.Fprintf(o.out, "\n%s\n", content)
		_, _ = fmt.Fprintf(o.errOut, "Until you merge these, `atcr review` will fail: .atcr/config.yaml lists agents (%s) your registry does not define yet.\n", strings.Join(missing, ", "))
		return nil
	}
	_, _ = fmt.Fprintf(o.out, "  created %s\n", regPath)
	return nil
}

// missingRosterAgents returns the roster agents the existing registry at
// regPath (merged with root's project overlay, as `atcr review` loads it) does
// not define. A registry that fails to load defines none of them, so the whole
// roster is returned.
func missingRosterAgents(regPath, root string, roster []string) []string {
	reg, err := registry.LoadMergedRegistry(regPath, root)
	if err != nil {
		return roster
	}
	var missing []string
	for _, name := range roster {
		if _, ok := reg.Agents[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}
