package personas

import (
	"fmt"
	"os"

	builtins "github.com/samestrin/atcr/personas"
)

// builtinSet is the O(1) lookup backing isBuiltin. It is populated once at init
// from the immutable names slice so repeated calls do not re-allocate.
var builtinSet = make(map[string]struct{})

func init() {
	for _, n := range builtins.Names() {
		builtinSet[n] = struct{}{}
	}
}

// Remove deletes the installed community persona name from personasDir. It
// refuses built-in persona names and path-traversal names, and reports a clear
// error when the persona is not installed.
func Remove(name, personasDir string) error {
	if isBuiltin(name) {
		return fmt.Errorf("cannot remove built-in persona %q — only community-installed personas can be removed", name)
	}
	dest, err := personaPath(personasDir, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dest); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("persona %q is not installed", name)
		}
		return fmt.Errorf("failed to stat persona %q: %w", name, err)
	}
	if err := os.Remove(dest); err != nil {
		return fmt.Errorf("failed to remove persona %q: %w", name, err)
	}
	return nil
}

// isBuiltin reports whether name is one of the embedded built-in personas.
func isBuiltin(name string) bool {
	_, ok := builtinSet[name]
	return ok
}
