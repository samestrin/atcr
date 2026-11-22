package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/samestrin/atcr/internal/registry"
)

// quickstartInput is the canned interactive input for a non-interactive test
// run: an empty key line (skip key entry) followed by an empty shell-profile
// choice (skip profile append). Extended per task as the flow grows.
const quickstartInput = "\n\n"

func TestQuickstart_CommandWiring(t *testing.T) {
	cmd := newQuickstartCmd()
	assert.Equal(t, "quickstart", cmd.Name(), "command is named quickstart")

	// It must be registered on the root command tree.
	root := NewRootCmd()
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "quickstart" {
			found = true
			break
		}
	}
	assert.True(t, found, "quickstart is registered on the root command")
}

func TestQuickstart_ReusesInitWriters(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir()) // registry writes land in a throwaway home
	out := &bytes.Buffer{}
	err := runQuickstart(quickstartOpts{
		dir:    dir,
		in:     strings.NewReader(quickstartInput),
		out:    out,
		errOut: &bytes.Buffer{},
	})
	require.NoError(t, err)

	// The .atcr side is produced by reusing init's writers: config + personas.
	cfg, err := registry.LoadProjectConfig(filepath.Join(dir, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.Agents, "roster is populated by the reused init writer")
	assert.FileExists(t, filepath.Join(dir, ".atcr", "personas", "bruce.md"))
	assert.FileExists(t, filepath.Join(dir, ".atcr", "personas", "_base.md"))
}

// TestQuickstart_FetchFailure_AbortsBeforeProviderSetup covers AC 01-04
// Scenario 2: a fetch failure aborts runQuickstart before the synthetic-provider
// setup, so no registry.yaml is written.
func TestQuickstart_FetchFailure_AbortsBeforeProviderSetup(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := statusServer(t, nil, map[string]int{"/index.json": 500})
	t.Setenv("ATCR_PERSONAS_URL", srv.URL)
	destDir := filepath.Join(home, ".config", "atcr", "personas")
	oldDir := personasDir
	personasDir = func() (string, error) { return destDir, nil }
	t.Cleanup(func() { personasDir = oldDir })

	err := runQuickstart(quickstartOpts{
		dir:            dir,
		fetchCommunity: true,
		in:             strings.NewReader(quickstartInput),
		out:            &bytes.Buffer{},
		errOut:         &bytes.Buffer{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--offline")
	assert.NoFileExists(t, filepath.Join(home, ".config", "atcr", "registry.yaml"),
		"provider setup must not run after a fetch failure")
}

// TestQuickstart_OfflineFlag_Registered covers AC 01-03: `atcr quickstart`
// exposes an --offline flag.
func TestQuickstart_OfflineFlag_Registered(t *testing.T) {
	require.NotNil(t, newQuickstartCmd().Flags().Lookup("offline"), "--offline registered on quickstart")
}

// TestQuickstart_Offline_ZeroNetwork covers AC 01-03: the offline path (the flag
// maps to fetchCommunity=false) makes zero network calls and still scaffolds the
// embedded built-ins.
func TestQuickstart_Offline_ZeroNetwork(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	oldClient := personasClient
	personasClient = failingHTTPClient{t}
	t.Cleanup(func() { personasClient = oldClient })

	require.NoError(t, runQuickstart(quickstartOpts{
		dir:            dir,
		fetchCommunity: false,
		in:             strings.NewReader(quickstartInput),
		out:            &bytes.Buffer{},
		errOut:         &bytes.Buffer{},
	}))
	assert.FileExists(t, filepath.Join(dir, ".atcr", "personas", "bruce.md"))
}

// TestQuickstart_OmitsInitRegistryNextStep: quickstart writes the registry
// itself, so the init step it reuses must not tell the user to go define one.
func TestQuickstart_OmitsInitRegistryNextStep(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldClient := personasClient
	personasClient = failingHTTPClient{t}
	t.Cleanup(func() { personasClient = oldClient })

	out := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir:            t.TempDir(),
		fetchCommunity: false,
		in:             strings.NewReader(quickstartInput),
		out:            out,
		errOut:         &bytes.Buffer{},
	}))
	assert.NotContains(t, out.String(), "Next: define providers and agents")
}

// TestQuickstart_FetchAndPin_InstallsCommunity covers AC 01-02 Scenario 2:
// runQuickstart (online) inherits the fetch-and-pin behavior — a community
// persona advertised in the index is installed into the community pin dir before
// the synthetic-provider setup proceeds.
func TestQuickstart_FetchAndPin_InstallsCommunity(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	// A community index advertising a roster (built-in) name so the fetch has a
	// match; other roster names are skipped with a warning.
	index := `[{"name":"bruce","version":"2.0.0","description":"d","path":"bruce.yaml","provider":"synthetic","model":"x"}]`
	srv := unitServer(t, index, map[string]string{"/bruce.yaml": communityUnitYAML})
	t.Setenv("ATCR_PERSONAS_URL", srv.URL)

	destDir := filepath.Join(home, ".config", "atcr", "personas")
	oldDir := personasDir
	personasDir = func() (string, error) { return destDir, nil }
	t.Cleanup(func() { personasDir = oldDir })

	require.NoError(t, runQuickstart(quickstartOpts{
		dir:            dir,
		fetchCommunity: true,
		in:             strings.NewReader(quickstartInput),
		out:            &bytes.Buffer{},
		errOut:         &bytes.Buffer{},
	}))

	assert.FileExists(t, filepath.Join(destDir, "bruce.yaml"), "community persona fetched and pinned during quickstart")
}

// TestQuickstart_FinishesWhenCatalogHasPersonaThisBinaryCannotValidate covers
// epic 84.0 AC3: the online catalog is newer than the binary and carries a
// persona with a role this binary does not know. Quickstart skips it and still
// finishes, writing the registry.
func TestQuickstart_FinishesWhenCatalogHasPersonaThisBinaryCannotValidate(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	index := `[{"name":"bruce","version":"2.0.0","description":"d","path":"bruce.yaml"},{"name":"nadia","version":"1.0.0","description":"d","path":"nadia.yaml"}]`
	srv := unitServer(t, index, map[string]string{"/bruce.yaml": communityUnitYAML, "/nadia.yaml": futureRoleUnitYAML})
	t.Setenv("ATCR_PERSONAS_URL", srv.URL)

	destDir := filepath.Join(home, ".config", "atcr", "personas")
	oldDir := personasDir
	personasDir = func() (string, error) { return destDir, nil }
	t.Cleanup(func() { personasDir = oldDir })

	errOut := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir:            dir,
		fetchCommunity: true,
		in:             strings.NewReader(quickstartInput),
		out:            &bytes.Buffer{},
		errOut:         errOut,
	}))

	assert.FileExists(t, filepath.Join(destDir, "bruce.yaml"))
	assert.NoFileExists(t, filepath.Join(destDir, "nadia.yaml"))
	assert.FileExists(t, filepath.Join(home, ".config", "atcr", "registry.yaml"), "quickstart finished and wrote the registry")
	assert.Contains(t, errOut.String(), `persona "nadia"`)
}

// TestQuickstart_Online_InstallsNonEmptyCommunityRoster covers AC 07-01
// Scenario 2 end-to-end through `runQuickstart`: online quickstart installs the
// identical non-empty, index-derived community roster as `init`, via the same
// shared reconciliation source (not builtins.Names()). Against an index disjoint
// from the built-ins, today it would pin zero — this proves the reconciled path.
func TestQuickstart_Online_InstallsNonEmptyCommunityRoster(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	index := `[
	  {"name":"anthony","version":"1.2.0","description":"d","path":"anthony.yaml"},
	  {"name":"sonny","version":"1.2.0","description":"d","path":"sonny.yaml"}
	]`
	srv := unitServer(t, index, map[string]string{
		"/anthony.yaml": communityUnitYAML,
		"/sonny.yaml":   communityUnitYAML,
	})
	t.Setenv("ATCR_PERSONAS_URL", srv.URL)

	destDir := filepath.Join(home, ".config", "atcr", "personas")
	oldDir := personasDir
	personasDir = func() (string, error) { return destDir, nil }
	t.Cleanup(func() { personasDir = oldDir })

	require.NoError(t, runQuickstart(quickstartOpts{
		dir:            dir,
		fetchCommunity: true,
		in:             strings.NewReader(quickstartInput),
		out:            &bytes.Buffer{},
		errOut:         &bytes.Buffer{},
	}))

	assert.Equal(t, []string{"anthony", "sonny"}, communityPinNames(t, destDir),
		"online quickstart installs the same index-derived roster as init")
}

// TestQuickstart_Online_NoSkipWarnings covers AC 07-02 Scenario 2 end-to-end
// through `runQuickstart`: the errOut stream contains zero "not found in community
// index — skipping" warnings against the real index. Transparent vacuous-RED — it
// passes because 7.2's index-derived roster already removed the misleading warning
// (same mechanism); its value is the regression guard against a reverted roster.
func TestQuickstart_Online_NoSkipWarnings(t *testing.T) {
	srv := realCommunityServer(t)
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATCR_PERSONAS_URL", srv.URL)

	destDir := filepath.Join(home, ".config", "atcr", "personas")
	oldDir := personasDir
	personasDir = func() (string, error) { return destDir, nil }
	t.Cleanup(func() { personasDir = oldDir })

	errOut := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir:            dir,
		fetchCommunity: true,
		in:             strings.NewReader(quickstartInput),
		out:            &bytes.Buffer{},
		errOut:         errOut,
	}))

	assert.NotContains(t, errOut.String(), "not found in community index",
		"online quickstart emits no misleading skip warnings against the real index")
	// Positive guard: reject a silent zero-install regression (which would also
	// emit zero warnings) by asserting a non-empty install.
	assert.NotEmpty(t, communityPinNames(t, destDir),
		"online quickstart installs a non-empty community roster (not a silent zero-install)")
}

func TestQuickstart_WritesSyntheticRegistry(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, runQuickstart(quickstartOpts{
		dir:    dir,
		in:     strings.NewReader(quickstartInput),
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}))

	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	reg, err := registry.LoadRegistry(regPath)
	require.NoError(t, err, "quickstart writes a valid registry.yaml")
	_, ok := reg.Providers["synthetic"]
	assert.True(t, ok, "synthetic provider is defined")

	// Every roster entry in the generated project config must resolve against
	// the generated registry — otherwise the first review would fail.
	cfg, err := registry.LoadProjectConfig(filepath.Join(dir, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.NoError(t, cfg.ValidateAgainst(reg), "roster resolves against synthetic registry")
}

func TestQuickstart_RegistryGuard_SkipsExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte("# user's own registry\n"), 0o644))

	errOut := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir:    dir,
		in:     strings.NewReader(quickstartInput),
		out:    &bytes.Buffer{},
		errOut: errOut,
	}))

	// The existing registry must be untouched (no clobber).
	data, err := os.ReadFile(regPath)
	require.NoError(t, err)
	assert.Equal(t, "# user's own registry\n", string(data), "existing registry left intact")
	// The notice goes to stderr. This once checked stdout for "registry.yaml",
	// which only init's "Next: define providers" hint satisfied; quickstart no
	// longer prints that hint.
	assert.Contains(t, errOut.String(), "A registry already exists at "+regPath, "user is told the snippet was not applied")
}

func TestQuickstart_ScaffoldsWorkflow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: strings.NewReader(quickstartInput), out: &bytes.Buffer{}, errOut: &bytes.Buffer{},
	}))
	wf := filepath.Join(dir, ".github", "workflows", "atcr.yml")
	data, err := os.ReadFile(wf)
	require.NoError(t, err, "workflow scaffolded")
	assert.Contains(t, string(data), "LLM_SYNTHETIC_API_KEY: ${{ secrets.LLM_SYNTHETIC_API_KEY }}")
}

func TestQuickstart_WorkflowGuard_SkipsExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	wf := filepath.Join(dir, ".github", "workflows", "atcr.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(wf), 0o755))
	require.NoError(t, os.WriteFile(wf, []byte("# my own workflow\n"), 0o644))

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	// Must NOT abort the whole quickstart — the registry step still runs.
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: strings.NewReader(quickstartInput), out: out, errOut: errOut,
	}))

	data, err := os.ReadFile(wf)
	require.NoError(t, err)
	assert.Equal(t, "# my own workflow\n", string(data), "existing workflow left intact")
	assert.Contains(t, errOut.String()+out.String(), "atcr.yml", "user told the workflow was skipped")
}

func TestQuickstart_WorkflowGuard_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	wf := filepath.Join(dir, ".github", "workflows", "atcr.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(wf), 0o755))
	require.NoError(t, os.WriteFile(wf, []byte("# my own workflow\n"), 0o644))

	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, force: true, in: strings.NewReader(quickstartInput), out: &bytes.Buffer{}, errOut: &bytes.Buffer{},
	}))
	data, err := os.ReadFile(wf)
	require.NoError(t, err)
	assert.Contains(t, string(data), "LLM_SYNTHETIC_API_KEY", "force overwrote with the scaffold")
}

func TestQuickstart_ScaffoldWorkflow_ForceDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	// Pre-plant a symlink at the workflow path pointing at an outside file, then
	// run with --force. A check-then-write (Lstat + WriteFile) would follow the
	// symlink and clobber the outside target; an atomic create must not.
	wf := filepath.Join(dir, ".github", "workflows", "atcr.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(wf), 0o755))
	outside := filepath.Join(t.TempDir(), "victim.txt")
	require.NoError(t, os.WriteFile(outside, []byte("PRECIOUS"), 0o644))
	require.NoError(t, os.Symlink(outside, wf))

	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, force: true, in: strings.NewReader(quickstartInput),
		out: &bytes.Buffer{}, errOut: &bytes.Buffer{},
	}))

	// The outside file must NOT have been written through the symlink.
	victim, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "PRECIOUS", string(victim), "force must not write through a pre-planted symlink")

	// The workflow path is now a regular file holding the scaffold, not a symlink.
	info, err := os.Lstat(wf)
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&os.ModeSymlink, "symlink replaced by a regular file")
	data, err := os.ReadFile(wf)
	require.NoError(t, err)
	assert.Contains(t, string(data), "LLM_SYNTHETIC_API_KEY")
}

func TestQuickstart_PrintsSignupLink(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	out := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: strings.NewReader(quickstartInput), out: out, errOut: &bytes.Buffer{},
	}))
	assert.Contains(t, out.String(), "https://synthetic.new/", "signup link is shown")
}

// TestQuickstart_SignupLinkPlainWhenNotTTY: an OSC 8 hyperlink is a terminal
// escape, so piped or captured output gets the bare URL with no escape bytes.
func TestQuickstart_SignupLinkPlainWhenNotTTY(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: t.TempDir(), in: strings.NewReader(quickstartInput), out: out, errOut: &bytes.Buffer{},
	}))
	assert.Contains(t, out.String(), "https://synthetic.new/")
	assert.NotContains(t, out.String(), "\x1b", "no OSC 8 escape when stdout is not a terminal")
}

func TestQuickstart_Open_InvokesOpenFn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	var opened string
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, open: true, in: strings.NewReader(quickstartInput),
		out: &bytes.Buffer{}, errOut: &bytes.Buffer{},
		openFn: func(u string) error { opened = u; return nil },
	}))
	assert.Equal(t, "https://synthetic.new/", opened, "--open passes the signup link to the opener")
}

func TestQuickstart_KeyEntry_ExportAndProfileAppend(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := filepath.Join(home, "profile.sh")

	out := &bytes.Buffer{}
	// Paste a key, then name a shell profile to append the export to.
	in := strings.NewReader("MYSECRETKEY\n" + profile + "\n")
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: in, out: out, errOut: &bytes.Buffer{},
	}))

	// The export instruction is shown to the user.
	assert.Contains(t, out.String(), "export LLM_SYNTHETIC_API_KEY=")

	// The chosen shell profile received the export with the key value.
	prof, err := os.ReadFile(profile)
	require.NoError(t, err)
	assert.Contains(t, string(prof), "export LLM_SYNTHETIC_API_KEY='MYSECRETKEY'")

	// SECURITY INVARIANT: the key value must NEVER be persisted into atcr's own
	// config files — only the env var name lives there.
	regBytes, err := os.ReadFile(filepath.Join(home, ".config", "atcr", "registry.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(regBytes), "MYSECRETKEY", "key never written to registry.yaml")
	cfgBytes, err := os.ReadFile(filepath.Join(dir, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(cfgBytes), "MYSECRETKEY", "key never written to config.yaml")
}

func TestQuickstart_KeyEcho_MaskedWhenNotTTY(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	out := &bytes.Buffer{} // a buffer is not a character device → not a terminal
	// Paste a key, then skip the profile-append prompt (empty line).
	in := strings.NewReader("MYSECRETKEY\n\n")
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: in, out: out, errOut: &bytes.Buffer{},
	}))

	assert.NotContains(t, out.String(), "MYSECRETKEY",
		"the pasted key must not be echoed to non-terminal (piped/CI-captured) stdout")
	assert.Contains(t, out.String(), "export LLM_SYNTHETIC_API_KEY=",
		"the export guidance line is still shown, just with the value masked")
}

func TestQuickstart_NoKey_PrintsInstructions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	out := &bytes.Buffer{}
	// Empty key line → skip entry; no profile prompt is consumed.
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: strings.NewReader("\n"), out: out, errOut: &bytes.Buffer{},
	}))
	assert.Contains(t, out.String(), "export LLM_SYNTHETIC_API_KEY=<your-key>",
		"instructions to set the env var later are shown when no key is pasted")
}

func TestQuickstart_LayersOntoExistingAtcr(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	// Simulate a prior `atcr init` with an edited persona.
	require.NoError(t, runInit(dir, false, &bytes.Buffer{}, &bytes.Buffer{}))
	persona := filepath.Join(dir, ".atcr", "personas", "bruce.md")
	require.NoError(t, os.WriteFile(persona, []byte("EDITED BY USER"), 0o644))

	// quickstart (no --force) must NOT abort and must NOT clobber the edited persona.
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: strings.NewReader(quickstartInput), out: &bytes.Buffer{}, errOut: &bytes.Buffer{},
	}))

	data, err := os.ReadFile(persona)
	require.NoError(t, err)
	assert.Equal(t, "EDITED BY USER", string(data), "existing edited persona preserved")
	// Provider setup still ran (registry written into the throwaway home).
	assert.FileExists(t, filepath.Join(os.Getenv("HOME"), ".config", "atcr", "registry.yaml"))
}

func TestQuickstart_RegistryGuard_WarnsRosterWontResolve(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte("# user's own registry\n"), 0o644))

	errOut := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: strings.NewReader(quickstartInput), out: &bytes.Buffer{}, errOut: errOut,
	}))
	assert.Contains(t, errOut.String(), "atcr review` will fail",
		"user is warned the roster will not resolve until they merge the block")
}

func TestQuickstart_KeyEntry_RefusesAtcrOwnedProfilePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	atcrCfg := filepath.Join(dir, ".atcr", "config.yaml")

	errOut := &bytes.Buffer{}
	// User pastes a key, then (foolishly) names an atcr-owned file as the profile.
	in := strings.NewReader("MYSECRETKEY\n" + atcrCfg + "\n")
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: in, out: &bytes.Buffer{}, errOut: errOut,
	}))

	assert.Contains(t, errOut.String(), "Refusing to write the key")
	// INVARIANT: the key value must not have been appended to the atcr config.
	cfg, err := os.ReadFile(atcrCfg)
	require.NoError(t, err)
	assert.NotContains(t, string(cfg), "MYSECRETKEY", "key never written into an atcr-owned file")
}

func TestQuickstart_KeyEntry_RefusesSymlinkIntoAtcr(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	// A profile path that is a symlink pointing into the .atcr workspace. The
	// symlink's own path is lexically outside .atcr, so a filepath.Abs-only guard
	// waves it through — then appendExport (which follows symlinks) writes the key
	// through the link into an atcr-owned file, defeating the invariant.
	link := filepath.Join(dir, "innocent-profile.sh")
	atcrTarget := filepath.Join(dir, ".atcr", "leaked.sh")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".atcr"), 0o755))
	require.NoError(t, os.Symlink(atcrTarget, link))

	errOut := &bytes.Buffer{}
	in := strings.NewReader("MYSECRETKEY\n" + link + "\n")
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: in, out: &bytes.Buffer{}, errOut: errOut,
	}))

	assert.Contains(t, errOut.String(), "Refusing to write the key",
		"a profile symlinked into .atcr/ must be refused")
	// The key must never have been written through the symlink into .atcr/.
	if data, err := os.ReadFile(atcrTarget); err == nil {
		assert.NotContains(t, string(data), "MYSECRETKEY", "key never written into .atcr via a symlink")
	}
}

func TestQuickstart_RegistryGuard_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte("# user's own registry\n"), 0o644))

	require.NoError(t, runQuickstart(quickstartOpts{
		dir:    dir,
		force:  true,
		in:     strings.NewReader(quickstartInput),
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}))

	reg, err := registry.LoadRegistry(regPath)
	require.NoError(t, err, "force overwrites with a valid synthetic registry")
	_, ok := reg.Providers["synthetic"]
	assert.True(t, ok, "synthetic provider present after force overwrite")
}

// errorAfterReader returns data once, then returns err on subsequent reads.
// It simulates a stream that fails mid-way through the interactive wizard.
type errorAfterReader struct {
	data []byte
	err  error
	done bool
}

func (r *errorAfterReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.done = true
	return n, nil
}

func TestQuickstart_ReadLine_SurfacesScannerError(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	in := &errorAfterReader{data: []byte("MYSECRETKEY\n"), err: errors.New("injected read error")}
	errOut := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir:    dir,
		in:     in,
		out:    &bytes.Buffer{},
		errOut: errOut,
	}))
	assert.Contains(t, errOut.String(), "injected read error")
}

func TestQuickstart_ProfileIsAtcrOwned_CaseInsensitiveFilesystem(t *testing.T) {
	dir := t.TempDir()
	atcrDir := filepath.Join(dir, ".atcr")
	require.NoError(t, os.MkdirAll(atcrDir, 0o755))

	// Only meaningful where the filesystem is case-insensitive (macOS/APFS,
	// Windows): there ./.ATCR and ./.atcr are the same directory, so writing to
	// .ATCR/config.yaml lands inside the atcr-owned .atcr. On a case-sensitive FS
	// (typical Linux CI) the two are genuinely distinct dirs and the guard
	// correctly reports not-owned, so the assertion does not apply — skip it.
	upper := filepath.Join(dir, ".ATCR")
	if _, err := os.Stat(upper); err != nil {
		t.Skip("filesystem is case-sensitive; case-collision guard not applicable")
	}

	owned := profileIsAtcrOwned(filepath.Join(dir, ".ATCR", "config.yaml"), dir)
	assert.True(t, owned, "a profile under a case-variant of .atcr must be reported atcr-owned")
}

func TestQuickstart_ResolveProfilePath_ExpandsBareTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	assert.Equal(t, home, resolveProfilePath("~"))
	assert.Equal(t, filepath.Join(home, "foo"), resolveProfilePath("~/foo"))
}

func TestQuickstart_ResolveProfilePath_PropagatesHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	assert.Empty(t, resolveProfilePath("~/foo"), "resolveProfilePath should fail when home dir cannot be determined")
}

func TestQuickstart_AppendExportAndGuardResolveSamePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := "~/.atcr-test-profile"
	require.NoError(t, appendExport(profile, "TEST_KEY", "secret", &bytes.Buffer{}))
	expected := resolveProfilePath(profile)
	assert.FileExists(t, expected)
	assert.Equal(t, filepath.Join(home, ".atcr-test-profile"), expected)
}

func TestQuickstart_AppendExport_PreservesExistingProfileMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := filepath.Join(home, ".zshrc")
	require.NoError(t, os.WriteFile(profile, []byte("# existing profile\n"), 0o644))

	require.NoError(t, appendExport(profile, "TEST_KEY", "secret", &bytes.Buffer{}))

	info, err := os.Stat(profile)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o644), info.Mode().Perm(),
		"an existing profile's mode must be left untouched — appendExport was asked to append, not to re-permission a file it did not create")
}

func TestQuickstart_AppendExport_CreatesNewProfileAt0600(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := filepath.Join(home, ".new-profile")

	require.NoError(t, appendExport(profile, "TEST_KEY", "secret", &bytes.Buffer{}))

	info, err := os.Stat(profile)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(),
		"a profile appendExport creates holds a secret, so it must be restricted to 0600")
}

// TestQuickstart_AppendExport_WarnsOnGroupReadableExistingProfile: when the user
// names a pre-existing shell profile, appendExport still leaves its mode untouched
// (the ownership boundary at quickstart.go:304-307), but must WARN when that file is
// group/other-readable so the user knows the plaintext key was appended to a file
// other users can read. A profile already at 0600 is safe and draws no warning.
func TestQuickstart_AppendExport_WarnsOnGroupReadableExistingProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("group-readable existing profile warns, mode untouched", func(t *testing.T) {
		profile := filepath.Join(home, ".zshrc")
		require.NoError(t, os.WriteFile(profile, []byte("# existing\n"), 0o644))
		var warn bytes.Buffer
		require.NoError(t, appendExport(profile, "TEST_KEY", "secret", &warn))
		info, err := os.Stat(profile)
		require.NoError(t, err)
		assert.Equal(t, fs.FileMode(0o644), info.Mode().Perm(), "mode left untouched")
		assert.Contains(t, warn.String(), "group/other-readable",
			"warns that the key sink is group/other-readable")
	})

	t.Run("already-0600 existing profile does not warn", func(t *testing.T) {
		profile := filepath.Join(home, ".bashrc")
		require.NoError(t, os.WriteFile(profile, []byte("# existing\n"), 0o600))
		var warn bytes.Buffer
		require.NoError(t, appendExport(profile, "TEST_KEY", "secret", &warn))
		assert.Empty(t, warn.String(), "a 0600 profile is already safe — no warning")
	})
}

// failingTransport fails the test on any request that reaches the stdlib HTTP
// stack. Installed as http.DefaultTransport, it catches a network call from any
// client that does not go through personasClient.
type failingTransport struct{ t *testing.T }

func (f failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("unexpected network call to %s: --preset local must make none", r.URL)
	return nil, errors.New("network disabled in test")
}

// runPresetLocal runs `atcr quickstart --preset local` (plus extra flags)
// through the real command tree, in a fresh project dir with HOME pointed at
// home and every HTTP path wired to fail.
func runPresetLocal(t *testing.T, home string, extra ...string) (dir string, out, errOut *bytes.Buffer, err error) {
	t.Helper()
	return runPreset(t, home, presetLocal, extra...)
}

// runPreset is runPresetLocal for any --preset value.
func runPreset(t *testing.T, home, preset string, extra ...string) (dir string, out, errOut *bytes.Buffer, err error) {
	t.Helper()
	dir = t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", home)
	t.Setenv("ATCR_PERSONAS_URL", "http://127.0.0.1:1") // a fetch would hit this
	oldClient := personasClient
	personasClient = failingHTTPClient{t}
	t.Cleanup(func() { personasClient = oldClient })
	oldTransport := http.DefaultTransport
	http.DefaultTransport = failingTransport{t}
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	out, errOut = &bytes.Buffer{}, &bytes.Buffer{}
	root := NewRootCmd()
	root.SetArgs(append([]string{"quickstart", "--preset", preset}, extra...))
	root.SetIn(strings.NewReader(quickstartInput))
	root.SetOut(out)
	root.SetErr(errOut)
	return dir, out, errOut, root.Execute()
}

// wantLocalAgent is the registry agent a local preset must write: the picked
// Ollama tag and the fields benchmarks/local-candidates.yaml benchmarked it
// with (epic 113.1 AC6).
type wantLocalAgent struct {
	model, thinkingStyle, thinking, thinkingLevel string
	maxTokens                                     int
}

// wantLocalPresets is the owner's pick (epic 113.1 AC6, 2026-11-20): orson on
// gemma3 in both tiers, liam on gemma4-26b-a4b (32 GB) or qwen3.8-27b (64 GB),
// gerald in neither until a model beats its gemma3 baseline.
var wantLocalPresets = map[string]map[string]wantLocalAgent{
	"local-32gb": {
		"orson": {model: "gemma3:27b-it-q4_K_M", maxTokens: 8192},
		"liam":  {model: "gemma4:26b-a4b-it-q4_K_M", thinkingStyle: "reasoning_effort", thinking: "on", thinkingLevel: "high", maxTokens: 32768},
	},
	"local-64gb": {
		"orson": {model: "gemma3:27b-it-q4_K_M", maxTokens: 8192},
		"liam":  {model: "qwen3.8:27b-q4_K_M", thinkingStyle: "reasoning_effort", thinking: "on", thinkingLevel: "high", maxTokens: 32768},
	},
}

// assertLocalPresetRun checks one `--preset` run on a fresh HOME: a valid
// registry with only the local provider and the tier's agents, a roster that
// resolves against it, telemetry off, and no cloud signup or CI workflow.
func assertLocalPresetRun(t *testing.T, preset string, want map[string]wantLocalAgent) {
	t.Helper()
	home := t.TempDir()
	dir, out, _, err := runPreset(t, home, preset)
	require.NoError(t, err)

	reg, err := registry.LoadRegistry(filepath.Join(home, ".config", "atcr", "registry.yaml"))
	require.NoError(t, err, "--preset %s writes a valid registry.yaml", preset)
	local, ok := reg.Providers["local"]
	require.True(t, ok, "local provider is defined")
	assert.Equal(t, "http://localhost:11434/v1", local.BaseURL)
	assert.Equal(t, "OLLAMA_API_KEY", local.APIKeyEnv)
	_, synthetic := reg.Providers["synthetic"]
	assert.False(t, synthetic, "no cloud provider in the local preset")

	require.Len(t, reg.Agents, len(want))
	roster := make([]string, 0, len(want))
	for name, w := range want {
		roster = append(roster, name)
		a, ok := reg.Agents[name]
		require.True(t, ok, "agent %s is defined", name)
		assert.Equal(t, "local", a.Provider, "agent %s binds the local provider", name)
		assert.Equal(t, w.model, a.Model, "agent %s binds the picked Ollama tag", name)
		assert.Equal(t, name, a.Persona)
		assert.Equal(t, w.thinkingStyle, a.ThinkingStyle, "agent %s thinking_style", name)
		assert.Equal(t, w.thinking, a.Thinking, "agent %s thinking", name)
		assert.Equal(t, w.thinkingLevel, a.ThinkingLevel, "agent %s thinking_level", name)
		assert.Equal(t, registry.ResponseFormatJSONObject, a.ResponseFormat, "agent %s response_format", name)
		require.NotNil(t, a.MaxTokens, "agent %s max_tokens", name)
		assert.Equal(t, w.maxTokens, *a.MaxTokens, "agent %s max_tokens", name)
		assert.FileExists(t, filepath.Join(dir, ".atcr", "personas", name+".md"), "the embedded %s prompt is scaffolded", name)
		assert.Contains(t, out.String(), "ollama pull "+w.model, "the pull line for %s is printed", name)
	}

	// The roster must resolve against the registry, or the first review fails.
	cfg, err := registry.LoadProjectConfig(filepath.Join(dir, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.ElementsMatch(t, roster, cfg.Agents)
	assert.NoError(t, cfg.ValidateAgainst(reg), "roster resolves against the local registry")

	telemetry, err := registry.LoadTelemetrySetting(dir)
	require.NoError(t, err)
	require.NotNil(t, telemetry, "telemetry is set explicitly")
	assert.False(t, *telemetry, "telemetry is off")

	assert.NotContains(t, out.String(), "Sign up for", "no cloud signup link in the local preset")
	assert.NoFileExists(t, filepath.Join(dir, ".github", "workflows", "atcr.yml"), "no cloud CI workflow in the local preset")
}

// TestQuickstart_PresetLocal_FreshHome covers Epic 36.3 AC1 and epic 113.1
// AC7: on a fresh HOME with a transport that fails on any request, each local
// preset exits 0, writes a valid registry with a local provider and the tier's
// picked agents, sets telemetry off, and makes no network call. `local` is an
// alias of `local-32gb` (AC6).
func TestQuickstart_PresetLocal_FreshHome(t *testing.T) {
	for _, preset := range []string{"local-32gb", "local-64gb"} {
		t.Run(preset, func(t *testing.T) { assertLocalPresetRun(t, preset, wantLocalPresets[preset]) })
	}
	t.Run("local", func(t *testing.T) { assertLocalPresetRun(t, "local", wantLocalPresets["local-32gb"]) })
}

// TestQuickstart_PresetLocal_ExistingRegistryNotOverwritten covers Epic 36.3
// AC2: an existing registry is not overwritten without --force; the block to
// merge is printed instead.
func TestQuickstart_PresetLocal_ExistingRegistryNotOverwritten(t *testing.T) {
	home := t.TempDir()
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte("# user's own registry\n"), 0o644))

	_, out, errOut, err := runPresetLocal(t, home)
	require.NoError(t, err)

	data, err := os.ReadFile(regPath)
	require.NoError(t, err)
	assert.Equal(t, "# user's own registry\n", string(data), "existing registry left intact")
	assert.Contains(t, errOut.String(), "not overwriting")
	assert.Contains(t, out.String(), "  local:\n    base_url: http://localhost:11434/v1\n", "the local provider block is printed to merge")
	for _, name := range []string{"orson", "liam"} {
		assert.Contains(t, out.String(), "  "+name+":\n", "agent %s is in the printed block", name)
	}
}

// TestQuickstart_PresetLocal_ForceOverwritesRegistry: with --force the existing
// registry is replaced by the local one.
func TestQuickstart_PresetLocal_ForceOverwritesRegistry(t *testing.T) {
	home := t.TempDir()
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0o755))
	require.NoError(t, os.WriteFile(regPath, []byte("# user's own registry\n"), 0o644))

	_, _, _, err := runPresetLocal(t, home, "--force")
	require.NoError(t, err)
	reg, err := registry.LoadRegistry(regPath)
	require.NoError(t, err)
	assert.Contains(t, reg.Providers, "local")
}

// TestQuickstart_PresetLocal_ExistingConfigGetsTelemetryOff: a returning user's
// existing .atcr/config.yaml is kept (no --force), but telemetry is still
// switched off in it.
func TestQuickstart_PresetLocal_ExistingConfigGetsTelemetryOff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, runInit(dir, false, &bytes.Buffer{}, &bytes.Buffer{}))
	errOut := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir:    dir,
		preset: presetLocal,
		in:     strings.NewReader(quickstartInput),
		out:    &bytes.Buffer{},
		errOut: errOut,
	}))
	assert.Contains(t, errOut.String(), "Using existing workspace")
	assert.Contains(t, errOut.String(), "add orson, liam to the agents list", "the user is told the kept roster lacks the local agents")
	telemetry, err := registry.LoadTelemetrySetting(dir)
	require.NoError(t, err)
	require.NotNil(t, telemetry)
	assert.False(t, *telemetry, "telemetry is off in the existing config")
}

func TestQuickstart_UnknownPreset_IsUsageError(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := NewRootCmd()
	root.SetArgs([]string{"quickstart", "--preset", "cloud"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown preset "cloud"`)
	assert.Equal(t, exitUsage, exitCode(err), "an unknown preset is a usage error")
}

// presetTierTag is the index.json tag a local persona carries when it is in a
// preset, and presetBenchTier the benchmarks/local-candidates.yaml tier the
// preset's tags were scored on.
var (
	presetTierTag   = map[string]string{presetLocal32GB: "tier-32gb", presetLocal64GB: "tier-64gb"}
	presetBenchTier = map[string]string{presetLocal32GB: "gpu-32gb", presetLocal64GB: "unified-64gb"}
)

// TestLocalPresets_MatchBenchmarkProfiles covers epic 113.1 AC7: every agent a
// local preset writes runs its tag with the profile it was benchmarked with.
// A tag missing from benchmarks/local-candidates.yaml, a tag not scored on the
// preset's tier, or a max_tokens/thinking/response_format field that differs
// from the candidate's fails here.
func TestLocalPresets_MatchBenchmarkProfiles(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "benchmarks", "local-candidates.yaml"))
	require.NoError(t, err)
	var file struct {
		Candidates []struct {
			OllamaTag      string   `yaml:"ollama_tag"`
			Tiers          []string `yaml:"tiers"`
			ThinkingStyle  string   `yaml:"thinking_style"`
			Thinking       string   `yaml:"thinking"`
			ThinkingLevel  string   `yaml:"thinking_level"`
			MaxTokens      int      `yaml:"max_tokens"`
			ResponseFormat string   `yaml:"response_format"`
		} `yaml:"candidates"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &file))

	require.Len(t, localPresets, len(presetTierTag), "every local preset has a tier tag and a benchmark tier")
	for preset, agents := range localPresets {
		require.NotEmpty(t, agents, "preset %s has agents", preset)
		for _, a := range agents {
			var found bool
			for _, c := range file.Candidates {
				if c.OllamaTag != a.model {
					continue
				}
				found = true
				assert.Contains(t, c.Tiers, presetBenchTier[preset], "%s/%s: %s was not benchmarked on the preset's tier", preset, a.name, a.model)
				assert.Equal(t, c.MaxTokens, a.maxTokens, "%s/%s max_tokens", preset, a.name)
				assert.Equal(t, c.ThinkingStyle, a.thinkingStyle, "%s/%s thinking_style", preset, a.name)
				assert.Equal(t, c.Thinking, a.thinking, "%s/%s thinking", preset, a.name)
				assert.Equal(t, c.ThinkingLevel, a.thinkingLevel, "%s/%s thinking_level", preset, a.name)
				assert.Equal(t, c.ResponseFormat, a.responseFormat, "%s/%s response_format", preset, a.name)
			}
			assert.True(t, found, "%s/%s: %s is not a candidate in benchmarks/local-candidates.yaml", preset, a.name, a.model)
		}
	}
}

// TestLocalPresets_MatchCommunityPersonas covers epic 113.1 AC7: the presets,
// the community persona files, index.json and docs/personas-install.md name
// the same tags. A local persona's index.json tier tags are exactly the
// presets it is in; one in the `local` alias tier binds that tier's tag in its
// persona file and index entry (local/<family>-<variant> for <family>:<variant>);
// every preset tag has an `ollama pull` line in the install guide.
func TestLocalPresets_MatchCommunityPersonas(t *testing.T) {
	assert.Equal(t, presetLocal32GB, resolvePreset(presetLocal), "`local` aliases the tier the pick names (AC6)")

	raw, err := os.ReadFile(filepath.Join("..", "personas", "community", "index.json"))
	require.NoError(t, err)
	var index []struct {
		Name     string   `json:"name"`
		Path     string   `json:"path"`
		Provider string   `json:"provider"`
		Model    string   `json:"model"`
		Tags     []string `json:"tags"`
	}
	require.NoError(t, json.Unmarshal(raw, &index))

	tagOf := func(preset, name string) (string, bool) {
		for _, a := range localPresets[preset] {
			if a.name == name {
				return a.model, true
			}
		}
		return "", false
	}

	localPersonas := map[string]bool{}
	for _, e := range index {
		if e.Provider != "local" {
			continue
		}
		localPersonas[e.Name] = true
		for preset, tierTag := range presetTierTag {
			_, in := tagOf(preset, e.Name)
			assert.Equal(t, in, slices.Contains(e.Tags, tierTag), "index.json %s: tag %q must be present exactly when it is in preset %s", e.Name, tierTag, preset)
		}

		yraw, err := os.ReadFile(filepath.Join("..", "personas", "community", e.Path))
		require.NoError(t, err)
		var y struct {
			Model string `yaml:"model"`
		}
		require.NoError(t, yaml.Unmarshal(yraw, &y))
		assert.Equal(t, e.Model, y.Model, "%s: persona file and index.json model", e.Name)

		if tag, in := tagOf(resolvePreset(presetLocal), e.Name); in {
			assert.Equal(t, "local/"+strings.Replace(tag, ":", "-", 1), e.Model, "%s: index.json model names the `local` tier tag", e.Name)
		}
	}

	doc, err := os.ReadFile(filepath.Join("..", "docs", "personas-install.md"))
	require.NoError(t, err)
	for preset, agents := range localPresets {
		for _, a := range agents {
			assert.True(t, localPersonas[a.name], "%s/%s is a provider:local persona in index.json", preset, a.name)
			assert.Contains(t, string(doc), "ollama pull "+a.model, "docs/personas-install.md documents the %s/%s pull", preset, a.name)
		}
	}
}

// TestLocalPresets_DocsNamePickedTags covers the docs half of epic 113.1 AC7:
// docs/personas-install.md and docs/air-gapped.md name every picked tag and
// both tier presets, so a future re-pick that forgets either guide fails here
// instead of shipping stale install instructions. The `ollama pull` lines are
// guarded separately by TestLocalPresets_MatchCommunityPersonas.
func TestLocalPresets_DocsNamePickedTags(t *testing.T) {
	for _, page := range []string{"personas-install.md", "air-gapped.md"} {
		raw, err := os.ReadFile(filepath.Join("..", "docs", page))
		require.NoError(t, err)
		doc := string(raw)
		for preset, agents := range localPresets {
			assert.Contains(t, doc, preset, "docs/%s names preset %s", page, preset)
			for _, a := range agents {
				assert.Contains(t, doc, a.model, "docs/%s names the %s/%s tag %s", page, preset, a.name, a.model)
			}
		}
	}
}

// TestQuickstart_UnknownLocalPresetOpt: a caller that skips the command's
// preset check still gets an error, not the 32 GB roster.
func TestQuickstart_UnknownLocalPresetOpt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := runQuickstart(quickstartOpts{dir: t.TempDir(), preset: "local-16gb", in: strings.NewReader(quickstartInput), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown preset "local-16gb"`)
}
