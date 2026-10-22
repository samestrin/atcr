package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/atomicfs"
)

// PersonaStampFile is the stamp's file name inside a personas directory
// (`.atcr/personas/.stamp.json`). It is not a `.md` file, so the persona
// listing (internal/personas listProject) never reports it as a persona.
const PersonaStampFile = ".stamp.json"

// PersonaStamp records what `atcr init` wrote into a personas directory: each
// persona file name it wrote (`_base.md` included) mapped to the sha256 of the
// exact content written (Epic 35.16.16). It is derived from the content rather
// than a hand-maintained version string, so it cannot disagree with what
// shipped. Comparing it against the embedded content and the file on disk is
// what tells a copy that fell behind an embedded edit from one the operator
// edited by hand.
type PersonaStamp struct {
	// Files maps a persona file name (base name, e.g. "bruce.md", "_base.md")
	// to the lowercase hex sha256 of the content init wrote.
	Files map[string]string `json:"files"`
}

// NewPersonaStamp returns an empty stamp ready for Record.
func NewPersonaStamp() PersonaStamp {
	return PersonaStamp{Files: map[string]string{}}
}

// HashPersonaContent returns the lowercase hex sha256 of content, the form
// every stamp entry holds.
func HashPersonaContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Record sets name's entry to the hash of content, replacing any earlier
// entry for that file and leaving every other entry untouched — a re-run
// records only the files it actually wrote.
func (s *PersonaStamp) Record(name string, content []byte) {
	if s.Files == nil {
		s.Files = map[string]string{}
	}
	s.Files[name] = HashPersonaContent(content)
}

// PersonaStampPath returns the stamp's path inside personasDir.
func PersonaStampPath(personasDir string) string {
	return filepath.Join(personasDir, PersonaStampFile)
}

// ReadPersonaStamp reads the stamp in personasDir. ok is false when there is no
// usable stamp — the file is missing, not a regular file, over
// maxPersonaStampBytes, unreadable, unparseable, or has no "files" object. That
// is deliberately not an error: a workspace initialized before this stamp
// existed has none, and staleness detection must degrade to its fallback for it
// rather than fail. A damaged stamp is treated the same way, since it can no
// longer say what init wrote.
func ReadPersonaStamp(personasDir string) (stamp PersonaStamp, ok bool) {
	data, _, err := readPersonaStampFile(PersonaStampPath(personasDir))
	if err != nil {
		return PersonaStamp{}, false
	}
	return parsePersonaStamp(data)
}

// PersonaStampDamaged reports whether personasDir holds a stamp file that
// exists but is unusable: not a regular file (a symlink, dangling or not, a
// FIFO, a device, a directory), over maxPersonaStampBytes, unreadable, or its
// path cannot be looked up for a reason other than being absent, as well as
// unparseable or with no "files" object. ReadPersonaStamp folds that case into
// "no stamp" on purpose; this is the separate signal that lets a caller say the
// stamp is damaged rather than stay silent. A missing stamp (or a missing
// personasDir) is not damaged.
func PersonaStampDamaged(personasDir string) bool {
	data, present, err := readPersonaStampFile(PersonaStampPath(personasDir))
	if !present {
		return false
	}
	if err != nil {
		return true
	}
	_, ok := parsePersonaStamp(data)
	return !ok
}

// maxPersonaStampBytes caps how much of a stamp either read takes. A stamp is a
// few KB; .atcr/personas/ is tracked, so a cloned repo can carry a huge one, and
// reading it whole must not be possible. It matches the refresh path's cap on a
// persona copy (cli/init.go maxPersonaCopyBytes).
const maxPersonaStampBytes = 1 << 20

// readPersonaStampFile is the one read both stamp readers share. It Lstats path
// and opens only a regular file, so a stamp that is a FIFO, a device or a
// symlink to either is refused before an open that could block or a read that
// never ends, and it reads at most maxPersonaStampBytes+1 bytes. present is
// false only when nothing is at path (or a directory above it is missing);
// any other Lstat error, a non-regular file, a file swapped between the Lstat
// and the open, a read error or a stamp over the cap is present with err set.
func readPersonaStampFile(path string) (data []byte, present bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, true, err
	}
	if !info.Mode().IsRegular() {
		return nil, true, fmt.Errorf("%s: not a regular file (%s)", path, info.Mode().Type())
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return nil, true, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, true, fmt.Errorf("%s: changed between lstat and open", path)
	}
	data, err = io.ReadAll(io.LimitReader(f, maxPersonaStampBytes+1))
	if err != nil {
		return nil, true, err
	}
	if len(data) > maxPersonaStampBytes {
		return nil, true, fmt.Errorf("%s: over %d bytes", path, maxPersonaStampBytes)
	}
	return data, true, nil
}

// parsePersonaStamp is the one rule for what counts as a usable stamp: it
// parses as JSON and carries a "files" object.
func parsePersonaStamp(data []byte) (stamp PersonaStamp, ok bool) {
	if err := json.Unmarshal(data, &stamp); err != nil || stamp.Files == nil {
		return PersonaStamp{}, false
	}
	return stamp, true
}

// WritePersonaStamp writes stamp to personasDir by an atomic temp-file write
// (atomicfs.WriteJSON), so a reader never sees a partial stamp. Callers write
// it last, after the persona files it describes.
func WritePersonaStamp(personasDir string, stamp PersonaStamp) error {
	if stamp.Files == nil {
		stamp.Files = map[string]string{}
	}
	return atomicfs.WriteJSON(PersonaStampPath(personasDir), stamp)
}
