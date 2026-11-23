package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome expands a leading "~" or "~/" in p to the user's home directory.
// A quoted '~' reaches atcr unexpanded by the shell, and taken literally it
// creates a directory named "~" in the working directory. "~user" forms are
// refused rather than guessed at; any other path is returned unchanged.
func ExpandHome(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	rest := p[1:]
	if rest != "" && rest[0] != '/' && rest[0] != filepath.Separator {
		return "", &ValidationError{Field: "path", Value: p, Message: `a leading "~" is expanded only as "~" or "~/..."; write the full path instead`}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expanding %q: %w", p, err)
	}
	return filepath.Join(home, rest), nil
}
