package security

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RefuseSymlinkUnder returns an error when any existing component of rel
// below root is a symlink (dangling or not). atcr writes its own state under
// fixed repo paths such as .atcr/reviews, and a cloned repository can ship
// those as symlinks: following one would write outside the project. root
// itself may be a symlink (the caller chose it); a missing component ends the
// check, since nothing below it exists to be a link.
func RefuseSymlinkUnder(root, rel string) error {
	p := root
	for _, seg := range strings.Split(filepath.ToSlash(filepath.Clean(rel)), "/") {
		if seg == "" || seg == "." {
			continue
		}
		p = filepath.Join(p, seg)
		info, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("cannot check %s: %w", p, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink: refusing to write through it", p)
		}
	}
	return nil
}
