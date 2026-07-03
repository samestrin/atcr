package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	root := newRootCmd()
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

	out := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir:    dir,
		in:     strings.NewReader(quickstartInput),
		out:    out,
		errOut: &bytes.Buffer{},
	}))

	// The existing registry must be untouched (no clobber).
	data, err := os.ReadFile(regPath)
	require.NoError(t, err)
	assert.Equal(t, "# user's own registry\n", string(data), "existing registry left intact")
	assert.Contains(t, out.String(), "registry.yaml", "user is told the snippet was not applied")
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

func TestQuickstart_PrintsSignupLink(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	out := &bytes.Buffer{}
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: strings.NewReader(quickstartInput), out: out, errOut: &bytes.Buffer{},
	}))
	assert.Contains(t, out.String(), "https://synthetic.new/", "signup link is shown")
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
	require.NoError(t, appendExport(profile, "TEST_KEY", "secret"))
	expected := resolveProfilePath(profile)
	assert.FileExists(t, expected)
	assert.Equal(t, filepath.Join(home, ".atcr-test-profile"), expected)
}

func TestQuickstart_AppendExport_ChmodsExistingProfileTo0600(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := filepath.Join(home, ".zshrc")
	require.NoError(t, os.WriteFile(profile, []byte("# existing profile\n"), 0o644))

	require.NoError(t, appendExport(profile, "TEST_KEY", "secret"))

	info, err := os.Stat(profile)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "existing profile must be restricted after appending a secret")
}
