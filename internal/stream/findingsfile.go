package stream

import (
	"fmt"
	"io"
	"os"
)

// readFindingsFile reads a selected findings file. A var so tests can act
// between selection and the read.
var readFindingsFile = readRegularFile

// readRegularFile opens path once and reads it only when the opened handle is a
// regular file. Selection checked the path with Lstat, but the file can change
// before the read; checking the handle closes that window. On unix the open
// refuses a symlink (O_NOFOLLOW) and does not block on a FIFO (O_NONBLOCK).
func readRegularFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|openNoFollowNonBlock, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return io.ReadAll(f)
}
