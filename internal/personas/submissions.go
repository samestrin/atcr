package personas

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"gopkg.in/yaml.v3"
)

// SubmissionStatus is the `submitted` marker for a persona a user contributed via
// `atcr personas submit`: fixture-passing but not yet maintainer-vetted. It is a
// DISTINCT type from PersonaMeta and ORTHOGONAL to PersonaMeta.Source — Source
// answers "where did this come from" (built-in|community|project), while this
// marker answers "has a maintainer vetted it yet." It is deliberately not a field
// on PersonaMeta and never a fourth Source value (AC 03-01); it is persisted as a
// YAML sidecar under SubmissionsDir(), outside the vetted personas/community/ tree
// so graduation stays an explicit maintainer action (AC 03-03).
type SubmissionStatus struct {
	Persona       string    `yaml:"persona"`        // source persona name (may be namespaced)
	Version       string    `yaml:"version"`        // version submitted ("-" when the unit declares none)
	Submitter     string    `yaml:"submitter"`      // gh login of the authenticated submitter
	FixturePassed bool      `yaml:"fixture_passed"` // the local fixture gate passed before submission
	SubmittedAt   time.Time `yaml:"submitted_at"`   // submission timestamp (UTC)
}

// SubmissionsDir returns the per-user submitted-marker storage directory. It is a
// `submissions/` sibling of PersonasDir() (i.e. ~/.config/atcr/submissions), a
// runtime location OUTSIDE the vetted personas/community/ tree — so an automated
// submit never writes into the graduated library and graduation remains an
// explicit maintainer promotion (AC 03-03). It is derived from the same
// DefaultRegistryPath() root as PersonasDir so the two stay siblings. The
// directory is not created here.
func SubmissionsDir() (string, error) {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return "", fmt.Errorf("resolving registry path: %w", err)
	}
	return filepath.Join(filepath.Dir(regPath), "submissions"), nil
}

// WriteSubmissionMarker persists status for status.Persona under dir as a YAML
// sidecar. The persona name is validated and contained within dir via personaPath
// (the same guard install/submit use, rejecting empty/absolute/traversal names
// before any path is built), the storage directory is created 0700 if absent
// (AC 03-02 Scenario 3), and the write goes through writeFileAtomic EXCLUSIVELY —
// sibling-temp-then-rename, 0600, symlink-refusing (AC 03-02 Scenario 2 / Edge
// Case 1). A re-submission atomically replaces the prior marker (Edge Case 2).
func WriteSubmissionMarker(dir string, status SubmissionStatus) error {
	dest, err := personaPath(dir, status.Persona)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(status)
	if err != nil {
		return fmt.Errorf("encoding submission marker for %q: %w", status.Persona, err)
	}
	// A namespaced name creates intermediate directories under dir. Refuse to write
	// through a pre-planted symlink at any intermediate component (mirroring
	// writePersonaUnit) so the marker can never be redirected outside the storage
	// dir — the leaf file is separately guarded by writeFileAtomic's own Lstat check.
	if err := refuseSymlinkedIntermediate(dest, status.Persona); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return fmt.Errorf("creating submissions directory: %w", err)
	}
	return writeFileAtomic(dest, data)
}

// ReadSubmission reads the `submitted` marker for name under dir. A persona that
// has never been submitted yields (nil, false, nil) — a clear zero-value result,
// not an error (AC 03-03 Edge Case 1). A present-but-unreadable/unparseable marker
// returns an error.
func ReadSubmission(dir, name string) (*SubmissionStatus, bool, error) {
	path, err := personaPath(dir, name)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading submission marker for %q: %w", name, err)
	}
	var status SubmissionStatus
	if err := yaml.Unmarshal(data, &status); err != nil {
		return nil, false, fmt.Errorf("parsing submission marker for %q: %w", name, err)
	}
	return &status, true, nil
}

// ListSubmissions returns every `submitted` marker under dir. It is the
// separately-named extension point for surfacing submitted status — deliberately
// NOT wired into List/ListTiers, whose Source-based output stays unchanged whether
// or not markers exist (AC 03-03). A missing directory yields no rows and no error
// (no submissions made yet). A file that cannot be parsed is skipped with a joined
// warning, mirroring listCommunity, so one corrupt marker never aborts the listing.
func ListSubmissions(dir string) ([]SubmissionStatus, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read submissions directory %s: %w", dir, err)
	}
	var out []SubmissionStatus
	var warnings []error
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil // skip directories and symlinks (may point outside the storage dir)
		}
		if strings.ToLower(filepath.Ext(path)) != ".yaml" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			warnings = append(warnings, fmt.Errorf("could not read submission marker %q: %w", path, readErr))
			return nil
		}
		var status SubmissionStatus
		if unmarshalErr := yaml.Unmarshal(data, &status); unmarshalErr != nil {
			warnings = append(warnings, fmt.Errorf("could not parse submission marker %q: %w", path, unmarshalErr))
			return nil
		}
		out = append(out, status)
		return nil
	})
	if walkErr != nil {
		return out, walkErr
	}
	return out, errors.Join(warnings...)
}

// personaUnitVersion resolves the version field of the installed persona unit for
// name under personasDir, for stamping into a submission marker. A name that does
// not resolve, a missing file, or a unit with no version all yield "-" (mirroring
// listCommunity's default) rather than an error — the marker's version is best-
// effort attribution, never a gate.
func personaUnitVersion(personasDir, name string) string {
	p, err := personaPath(personasDir, name)
	if err != nil {
		return "-"
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "-"
	}
	var fm personaFileMeta
	if yaml.Unmarshal(data, &fm) == nil && strings.TrimSpace(fm.Version) != "" {
		return fm.Version
	}
	return "-"
}
