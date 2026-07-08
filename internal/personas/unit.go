package personas

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/registry"
)

// validateFetchedPrompt enforces the C3 untrusted-input guardrails on a fetched
// community custom prompt BEFORE it is written to disk. It delegates to
// registry.ValidateFetchedPersonaPrompt so install-time and resolve-time share one
// allowlist (length cap + known-template-variable allowlist) and can never drift:
// the required persona variables are permitted (the authoring contract mandates
// them), any other {{ }} action or unbalanced brace is rejected. Rejection is a
// descriptive error, never a silent truncation or transform.
func validateFetchedPrompt(data []byte) error {
	return registry.ValidateFetchedPersonaPrompt(string(data))
}

// FetchPersonaMD fetches <baseURL>/<name>.md — a community persona's co-located
// custom reviewer prompt. A 404 is returned as ErrPersonaNotFound so callers can
// treat a missing prompt as "binding-only" (the persona ships no custom prompt),
// which is valid per Clarification C1. The name is validated before any network
// access so the fetch boundary is self-guarding regardless of caller discipline.
func FetchPersonaMD(client HTTPClient, baseURL, name string) ([]byte, error) {
	if err := validatePersonaName(name); err != nil {
		return nil, fmt.Errorf("invalid persona name: %w", err)
	}
	segments := strings.Split(name, "/")
	escaped := make([]string, len(segments))
	for i, seg := range segments {
		escaped[i] = url.PathEscape(seg)
	}
	safeName := strings.Join(escaped, "/")
	data, err := fetch(client, strings.TrimRight(baseURL, "/")+"/"+safeName+".md", ErrPersonaNotFound)
	if err != nil {
		if errors.Is(err, ErrPersonaNotFound) {
			return nil, fmt.Errorf("persona prompt %q %w", name, ErrPersonaNotFound)
		}
		return nil, fmt.Errorf("failed to fetch persona prompt %q: %w", name, err)
	}
	return data, nil
}

// InstallUnit installs a community persona as a single self-contained unit: the
// <name>.yaml metadata plus its co-located <name>.md custom prompt when present
// (Clarification C2 — one installable unit, one delivery path). The YAML is
// validated with the registry agent validator BEFORE any disk write, so malformed
// or malicious community content never reaches disk. A binding-only persona (no
// co-located .md, i.e. a 404 for <name>.md) installs its YAML alone without error.
//
// The pair is installed together: both are fetched and the YAML validated before
// either is written, and a failure to write the .md rolls back the .yaml so no
// partial unit is left on an error return. (True crash-atomicity across two files
// is not attempted; a SIGKILL between the two renames could leave a lone .yaml.)
// Each file is written atomically (staged to a sibling temp file and renamed) so a
// reader never observes a partial write. When the persona is binding-only (no
// co-located .md upstream), any stale <name>.md from a prior install is removed so
// the on-disk unit always matches what was fetched — never a leftover custom
// prompt fed into an LLM.
//
// A "bundle/"-prefixed name is rejected (defense in depth, mirroring Install): a
// bundle must be expanded via InstallBundle, never round-tripped through the
// single-unit path.
func InstallUnit(client HTTPClient, baseURL, name, destDir string) error {
	if strings.HasPrefix(name, "bundle/") {
		return fmt.Errorf("%q is a bundle; install it via the bundle path, not as a single persona", name)
	}
	yamlDest, err := personaPath(destDir, name)
	if err != nil {
		return err
	}
	yamlData, err := FetchPersonaYAML(client, baseURL, name)
	if err != nil {
		return err
	}
	if err := registry.ValidateCommunityPersonaYAML(name, yamlData); err != nil {
		return fmt.Errorf("persona %q failed validation: %w", name, err)
	}

	return writePersonaUnit(client, baseURL, name, yamlDest, yamlData)
}

// writePersonaUnit writes a fetched persona unit (yaml + optional co-located
// .md) to yamlDest. It fetches the co-located custom prompt, validates it
// against the C3 guardrails, and either writes it or removes a stale .md for
// binding-only personas. A failure to write the .md rolls back the .yaml so
// no partial unit is left behind. This is the shared paired-write tail used by
// InstallUnit and Upgrade.
func writePersonaUnit(client HTTPClient, baseURL, name, yamlDest string, yamlData []byte) error {
	// The co-located custom prompt is optional: a 404 means binding-only.
	mdData, mdErr := FetchPersonaMD(client, baseURL, name)
	hasMD := mdErr == nil
	if mdErr != nil && !errors.Is(mdErr, ErrPersonaNotFound) {
		return mdErr
	}
	// A fetched custom prompt is untrusted input: enforce the C3 guardrails before
	// anything is written, so an invalid prompt never reaches disk.
	if hasMD {
		if err := validateFetchedPrompt(mdData); err != nil {
			return fmt.Errorf("persona %q: %w", name, err)
		}
	}

	// Refuse to write through a symlinked intermediate directory component: the
	// name is attacker-influenced (untrusted index) and may be namespaced, so a
	// pre-planted symlink at an intermediate segment could redirect MkdirAll and the
	// writes below outside the personas dir. The leaf file is separately guarded by
	// writeFileAtomic's own Lstat check.
	if err := refuseSymlinkedIntermediate(yamlDest, name); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(yamlDest), 0o700); err != nil {
		return fmt.Errorf("failed to create personas directory: %w", err)
	}
	// Snapshot any prior YAML before it is overwritten. On a re-install/upgrade the
	// existing unit is already valid, so if the co-located .md write fails after the
	// .yaml has been replaced, the rollback must RESTORE the prior working YAML — not
	// delete it, which would leave the user with no persona where one worked moments
	// earlier. A first-time install has no prior YAML and is rolled back by removing
	// the partial file, as before.
	priorYAML, priorErr := os.ReadFile(yamlDest)
	if err := writeFileAtomic(yamlDest, yamlData); err != nil {
		return err
	}
	mdDest := strings.TrimSuffix(yamlDest, ".yaml") + ".md"
	if hasMD {
		if err := writeFileAtomic(mdDest, mdData); err != nil {
			if priorErr == nil {
				_ = writeFileAtomic(yamlDest, priorYAML) // restore the prior working unit
			} else {
				_ = os.Remove(yamlDest) // roll back a first-time partial unit
			}
			return err
		}
	} else if err := os.Remove(mdDest); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Binding-only upstream: drop any stale co-located prompt from a prior
		// install so the resolver never feeds an outdated custom prompt.
		return fmt.Errorf("failed to remove stale persona prompt %s: %w", mdDest, err)
	}
	return nil
}

// refuseSymlinkedIntermediate errors if any intermediate directory component that
// the persona NAME contributes (its "/"-separated segments before the leaf file)
// is a symlink. The name comes from the untrusted community index and personaPath
// + MkdirAll create/traverse nested dirs, so a pre-planted symlink at an
// intermediate segment could redirect the write outside the personas dir. The leaf
// file itself is guarded by writeFileAtomic's Lstat check; this closes the
// intermediate-component gap. A flat (non-namespaced) name has no intermediate
// components and is a no-op.
func refuseSymlinkedIntermediate(yamlDest, name string) error {
	segs := strings.Split(name, "/")
	dir := filepath.Dir(yamlDest)
	for i := 0; i < len(segs)-1; i++ {
		fi, err := os.Lstat(dir)
		switch {
		case errors.Is(err, os.ErrNotExist):
			// Not yet created — nothing planted at this level.
		case err != nil:
			return fmt.Errorf("stat persona path %s: %w", dir, err)
		case fi.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("refusing to install persona through symlinked path component %s", dir)
		}
		dir = filepath.Dir(dir)
	}
	return nil
}

// writeFileAtomic writes data to dest atomically: it refuses to write through a
// pre-existing symlink (TOCTOU guard — persona text is fed into LLM prompts, so
// following a symlink could write outside the personas dir), stages to a sibling
// temp file with 0600 perms, then renames into place so a reader never sees a
// partially-written file. The parent directory must already exist.
func writeFileAtomic(dest string, data []byte) error {
	if fi, lerr := os.Lstat(dest); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write persona to symlink at %s", dest)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create persona temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write persona temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to set persona temp file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close persona temp file: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("failed to write persona to %s: %w", dest, err)
	}
	return nil
}
