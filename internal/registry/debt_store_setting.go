package registry

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// DebtConfig is the optional `debt` block (Epic 92 T4), declared on both
// ProjectConfig and Registry so the strict (KnownFields) loaders accept a file
// that carries it. Store names the local debt store directory; the trust rules
// for each tier (a project value must stay inside the repository, a user value
// may point anywhere) are applied by localdebt.StoreDir, the one resolver, not
// here.
type DebtConfig struct {
	Store string `yaml:"store,omitempty"`
}

// LoadProjectDebtStore returns the raw debt.store value from .atcr/config.yaml
// under root, WITHOUT the strict roster load (a debt command needs no roster).
// "" means unset: the file is absent, or has no debt block or no store key. A
// malformed value is an error, never a silent fall-through to the next tier.
func LoadProjectDebtStore(root string) (string, error) {
	return loadDebtStore(DefaultProjectConfigPath(root), ".atcr/config.yaml")
}

// LoadUserDebtStore returns the raw debt.store value from the LOCAL user
// registry (~/.config/atcr/registry.yaml), with LoadProjectDebtStore's
// semantics.
//
// It deliberately ignores ATCR_REGISTRY_URL. The store path is a per-user,
// per-machine choice — that is why the user tier may name a path outside the
// repository — so a shared team registry must not be able to choose it for
// everyone. It also keeps a debt command and a reconcile persist from paying
// for an HTTP fetch the rest of the run already made once.
func LoadUserDebtStore() (string, error) {
	path, err := DefaultRegistryPath()
	if err != nil {
		return "", err
	}
	return loadDebtStore(path, userRegistryLabel)
}

// loadDebtStore decodes only the debt.store value at path. Sibling keys are
// ignored, so a roster-less or partial file still resolves. label names the
// file in errors in place of its absolute (username-bearing) path.
func loadDebtStore(path, label string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	var probe struct {
		Debt *struct {
			Store yaml.Node `yaml:"store"`
		} `yaml:"debt"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return "", fmt.Errorf("parse %s debt setting: %w", label, err)
	}
	// A zero Node (Kind 0) is an absent store key.
	if probe.Debt == nil || probe.Debt.Store.Kind == 0 {
		return "", nil
	}
	n := probe.Debt.Store
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return "", nil
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return "", fmt.Errorf("parse %s debt setting: debt.store must be a path string", label)
	}
	return n.Value, nil
}
