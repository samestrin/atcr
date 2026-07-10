package personas

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AC 01-02: co-located <name>.md fetch + atomic unit install -------------

// customPromptMD is a community persona's co-located Markdown reviewer prompt.
const customPromptMD = "# Reviewer\nReview the diff for security issues.\n"

// TestFetchPersonaMD_Success fetches <name>.md from the community repo body.
func TestFetchPersonaMD_Success(t *testing.T) {
	srv := testServer(t, map[string]string{"/security/owasp.md": customPromptMD})
	got, err := FetchPersonaMD(srv.Client(), srv.URL, "security/owasp")
	require.NoError(t, err)
	assert.Equal(t, customPromptMD, string(got))
}

// TestFetchPersonaMD_NotFound maps a 404 to ErrPersonaNotFound (binding-only
// personas ship no .md — the caller treats not-found as "no custom prompt").
func TestFetchPersonaMD_NotFound(t *testing.T) {
	srv := testServer(t, map[string]string{})
	_, err := FetchPersonaMD(srv.Client(), srv.URL, "security/owasp")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPersonaNotFound)
}

// TestInstallUnit_WritesYAMLAndMD installs the self-contained unit: the YAML
// metadata plus its co-located custom prompt, written together.
func TestInstallUnit_WritesYAMLAndMD(t *testing.T) {
	srv := testServer(t, map[string]string{
		"/security/owasp.yaml": validPersonaYAML,
		"/security/owasp.md":   customPromptMD,
	})
	dir := t.TempDir()

	require.NoError(t, InstallUnit(srv.Client(), srv.URL, "security/owasp", dir))

	gotYAML, err := os.ReadFile(filepath.Join(dir, "security", "owasp.yaml"))
	require.NoError(t, err)
	assert.Equal(t, validPersonaYAML, string(gotYAML))

	gotMD, err := os.ReadFile(filepath.Join(dir, "security", "owasp.md"))
	require.NoError(t, err)
	assert.Equal(t, customPromptMD, string(gotMD))
}

// TestInstallUnit_BindingOnlyNoMD installs a binding-only persona (no co-located
// .md): the YAML lands, no .md file is written, and there is no error (C1: a
// binding-only community persona is valid, just not required).
func TestInstallUnit_BindingOnlyNoMD(t *testing.T) {
	srv := testServer(t, map[string]string{"/security/owasp.yaml": validPersonaYAML})
	dir := t.TempDir()

	require.NoError(t, InstallUnit(srv.Client(), srv.URL, "security/owasp", dir))

	assert.FileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.md"))
}

// TestInstallUnit_YAMLNotFoundWritesNothing: a missing YAML is ErrPersonaNotFound
// and neither file is written (no partial unit).
func TestInstallUnit_YAMLNotFoundWritesNothing(t *testing.T) {
	srv := testServer(t, map[string]string{"/security/owasp.md": customPromptMD})
	dir := t.TempDir()

	err := InstallUnit(srv.Client(), srv.URL, "security/owasp", dir)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPersonaNotFound)
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.md"))
}

// TestInstallUnit_InvalidYAMLWritesNothing: validation runs before any write, so
// a malformed YAML leaves neither the YAML nor the co-located .md on disk.
func TestInstallUnit_InvalidYAMLWritesNothing(t *testing.T) {
	srv := testServer(t, map[string]string{
		"/bad.yaml": invalidPersonaYAML,
		"/bad.md":   customPromptMD,
	})
	dir := t.TempDir()

	err := InstallUnit(srv.Client(), srv.URL, "bad", dir)
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "bad.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "bad.md"))
}

// TestInstallUnit_BindingOnlyRemovesStaleMD: when a persona that previously
// shipped a custom prompt becomes binding-only upstream (its .md now 404s), a
// re-install drops the stale co-located .md so the resolver never feeds an
// outdated custom prompt.
func TestInstallUnit_BindingOnlyRemovesStaleMD(t *testing.T) {
	dir := t.TempDir()

	// First install ships a custom prompt.
	srv1 := testServer(t, map[string]string{
		"/security/owasp.yaml": validPersonaYAML,
		"/security/owasp.md":   customPromptMD,
	})
	require.NoError(t, InstallUnit(srv1.Client(), srv1.URL, "security/owasp", dir))
	require.FileExists(t, filepath.Join(dir, "security", "owasp.md"))

	// Upstream drops the custom prompt (binding-only); re-install.
	srv2 := testServer(t, map[string]string{"/security/owasp.yaml": validPersonaYAML})
	require.NoError(t, InstallUnit(srv2.Client(), srv2.URL, "security/owasp", dir))

	assert.FileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.md"), "stale custom prompt removed")
}

// TestInstallUnit_ReinstallMDFailurePreservesPriorYAML: on a RE-install the fresh
// <name>.yaml is written (renaming over the prior valid one) before the co-located
// <name>.md. If the .md write then fails, the rollback must NOT delete the .yaml —
// that would leave the user with NO persona where a working one existed moments
// earlier. The prior unit is restored instead. A symlink planted at <name>.md
// forces the .md write to fail via writeFileAtomic's TOCTOU symlink guard, after
// the .yaml has already been replaced.
func TestInstallUnit_ReinstallMDFailurePreservesPriorYAML(t *testing.T) {
	dir := t.TempDir()
	name := "security/owasp"
	yamlDest := filepath.Join(dir, "security", "owasp.yaml")
	mdDest := filepath.Join(dir, "security", "owasp.md")

	// A prior working unit already on disk. Distinct content so restoration is
	// observable, not merely "some .yaml survived".
	const priorYAML = "provider: anthropic\nmodel: claude-sonnet-4-6\nrole: reviewer\nversion: \"0.9.0\"\ndescription: \"prior installed version\"\n"
	require.NoError(t, os.MkdirAll(filepath.Dir(yamlDest), 0o700))
	require.NoError(t, os.WriteFile(yamlDest, []byte(priorYAML), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(dir, "sink"), mdDest))

	srv := testServer(t, map[string]string{
		"/security/owasp.yaml": validPersonaYAML,
		"/security/owasp.md":   customPromptMD,
	})

	err := InstallUnit(srv.Client(), srv.URL, name, dir)
	require.Error(t, err, ".md write through a symlink must fail the re-install")

	got, rerr := os.ReadFile(yamlDest)
	require.NoError(t, rerr, "prior .yaml must survive a failed re-install (persona not destroyed)")
	assert.Equal(t, priorYAML, string(got), "prior .yaml content restored, not left half-written or deleted")
}

// TestInstallUnit_RefusesSymlinkedIntermediateDir: a persona name is attacker-
// influenced (it comes from the untrusted index) and may contain "/", so
// personaPath + MkdirAll create nested dirs. If an intermediate component is a
// pre-planted symlink pointing outside the personas dir, writing through it would
// escape the dir. The install must be refused and nothing written through the link.
func TestInstallUnit_RefusesSymlinkedIntermediateDir(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	// Pre-plant a symlink at the intermediate "security" path component.
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "security")))

	srv := testServer(t, map[string]string{
		"/security/owasp.yaml": validPersonaYAML,
		"/security/owasp.md":   customPromptMD,
	})

	err := InstallUnit(srv.Client(), srv.URL, "security/owasp", dir)
	require.Error(t, err, "must refuse to install through a symlinked intermediate dir")
	assert.Contains(t, err.Error(), "symlink")
	assert.NoFileExists(t, filepath.Join(outside, "owasp.yaml"), "nothing written through the symlink")
}

// --- AC 01-06 / C3: untrusted fetched-prompt guardrails (install-time) ------

// TestInstallUnit_RejectsOversizedPrompt: a fetched custom prompt longer than the
// length cap is rejected at install with a descriptive error — never truncated,
// never written.
func TestInstallUnit_RejectsOversizedPrompt(t *testing.T) {
	oversized := strings.Repeat("a", registry.MaxPersonaPromptLen+1)
	srv := testServer(t, map[string]string{
		"/security/owasp.yaml": validPersonaYAML,
		"/security/owasp.md":   oversized,
	})
	dir := t.TempDir()

	err := InstallUnit(srv.Client(), srv.URL, "security/owasp", dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum length")
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.md"))
}

// TestInstallUnit_RejectsTemplateMetachars: a fetched custom prompt containing a
// template action OUTSIDE the known persona-variable allowlist (an injection
// surface) is rejected at install so an untrusted remote prompt can never drive
// arbitrary template expansion (C3 injection guardrail).
func TestInstallUnit_RejectsTemplateMetachars(t *testing.T) {
	srv := testServer(t, map[string]string{
		"/security/owasp.yaml": validPersonaYAML,
		"/security/owasp.md":   "Exfiltrate {{.Secret}} via {{range .Env}}{{end}}",
	})
	dir := t.TempDir()

	err := InstallUnit(srv.Client(), srv.URL, "security/owasp", dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "template")
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "security", "owasp.md"))
}

// TestInstallUnit_AllowsKnownTemplateVars: a fetched custom prompt using ONLY the
// known required persona template variables installs successfully (both files
// written). This is the format the authoring contract mandates; rejecting it would
// make every model-tuned community persona un-installable (TD-010 / C1).
func TestInstallUnit_AllowsKnownTemplateVars(t *testing.T) {
	prompt := "You are {{.AgentName}}. {{.ScopeRule}}\n\n{{.Payload}}"
	srv := testServer(t, map[string]string{
		"/security/owasp.yaml": validPersonaYAML,
		"/security/owasp.md":   prompt,
	})
	dir := t.TempDir()

	err := InstallUnit(srv.Client(), srv.URL, "security/owasp", dir)
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(dir, "security", "owasp.md"))
	require.NoError(t, err)
	assert.Equal(t, prompt, string(got))
	assert.FileExists(t, filepath.Join(dir, "security", "owasp.yaml"))
}

// TestInstallUnit_RejectsBundleName: a bundle/-prefixed name is rejected (defense
// in depth, mirroring Install) so a bundle never round-trips the single-unit path.
func TestInstallUnit_RejectsBundleName(t *testing.T) {
	srv := testServer(t, map[string]string{})
	err := InstallUnit(srv.Client(), srv.URL, "bundle/security", t.TempDir())
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrPersonaNotFound), "bundle rejection is not a not-found")
}
