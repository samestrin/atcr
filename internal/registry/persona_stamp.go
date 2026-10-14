package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
// usable stamp — the file is missing, unreadable, unparseable, or has no
// "files" object. That is deliberately not an error: a workspace initialized
// before this stamp existed has none, and staleness detection must degrade to
// its fallback for it rather than fail. A damaged stamp is treated the same
// way, since it can no longer say what init wrote.
func ReadPersonaStamp(personasDir string) (stamp PersonaStamp, ok bool) {
	data, err := os.ReadFile(PersonaStampPath(personasDir))
	if err != nil {
		return PersonaStamp{}, false
	}
	return parsePersonaStamp(data)
}

// PersonaStampDamaged reports whether personasDir holds a stamp file that
// exists but is unusable: unreadable, unparseable, or with no "files" object.
// ReadPersonaStamp folds that case into "no stamp" on purpose; this is the
// separate signal that lets a caller say the stamp is damaged rather than
// stay silent. A missing stamp (or a missing personasDir) is not damaged.
func PersonaStampDamaged(personasDir string) bool {
	path := PersonaStampPath(personasDir)
	if _, err := os.Stat(path); err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	_, ok := parsePersonaStamp(data)
	return !ok
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
