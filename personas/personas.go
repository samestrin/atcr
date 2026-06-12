// Package personas ships the six embedded default reviewer personas and the
// shared base template. `atcr init` installs editable copies; the prompt
// resolution chain falls back to these embedded versions when no file
// overrides them.
package personas

import (
	"embed"
	"fmt"
)

//go:embed *.md
var files embed.FS

// canonical order: generalist first, style last.
var names = []string{"bruce", "greta", "kai", "mira", "dax", "otto"}

// Names returns the six embedded persona names in canonical order.
func Names() []string {
	out := make([]string, len(names))
	copy(out, names)
	return out
}

// Get returns the embedded persona template for name.
func Get(name string) (string, error) {
	data, err := files.ReadFile(name + ".md")
	if err != nil {
		return "", fmt.Errorf("internal error: embedded persona %s not found: %w", name, err)
	}
	return string(data), nil
}

// Base returns the shared base persona template (_base.md).
func Base() (string, error) {
	data, err := files.ReadFile("_base.md")
	if err != nil {
		return "", fmt.Errorf("internal error: embedded persona _base not found: %w", err)
	}
	return string(data), nil
}
