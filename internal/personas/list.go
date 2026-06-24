package personas

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/registry"
	builtins "github.com/samestrin/atcr/personas"
	"gopkg.in/yaml.v3"
)

// PersonaMeta is one row of `atcr personas list`.
type PersonaMeta struct {
	Name     string
	Version  string
	Source   string // "built-in" | "community"
	Language []string
}

// personaFileMeta captures the persona-file metadata read from an installed
// community YAML (a superset of registry.AgentConfig).
type personaFileMeta struct {
	Version  string   `yaml:"version"`
	Language []string `yaml:"language"`
}

// List returns metadata for the nine built-in personas plus every community
// persona installed under personasDir. A missing directory yields just the
// built-ins (no error). A directory that exists but cannot be walked yields the
// built-ins gathered so far plus a non-nil error, so the caller can warn yet
// still render the built-ins.
func List(personasDir string) ([]PersonaMeta, error) {
	names := builtins.Names()
	metas := make([]PersonaMeta, 0, len(names))
	for _, n := range names {
		metas = append(metas, PersonaMeta{Name: n, Version: "built-in", Source: "built-in"})
	}
	community, err := listCommunity(personasDir)
	metas = append(metas, community...)
	return metas, err
}

func listCommunity(personasDir string) ([]PersonaMeta, error) {
	if _, err := os.Stat(personasDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read personas directory %s: %w", personasDir, err)
	}
	var out []PersonaMeta
	walkErr := filepath.WalkDir(personasDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil // silently skip non-YAML files (.DS_Store, .gitkeep, ...)
		}
		rel, err := filepath.Rel(personasDir, path)
		if err != nil {
			return nil
		}
		name := filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
		meta := PersonaMeta{Name: name, Version: "-", Source: "community"}
		if data, readErr := os.ReadFile(path); readErr == nil {
			var fm personaFileMeta
			if yaml.Unmarshal(data, &fm) == nil {
				if strings.TrimSpace(fm.Version) != "" {
					meta.Version = fm.Version
				}
				for _, l := range fm.Language {
					if c := registry.NormalizeLanguageToken(l); c != "" {
						meta.Language = append(meta.Language, c)
					}
				}
			}
		}
		out = append(out, meta)
		return nil
	})
	return out, walkErr
}
