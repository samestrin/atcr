package personas

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// embeddedSnapshot is the checked-in catalog snapshot compiled into the binary,
// so the default `atcr models check` path reads the catalog with zero network I/O
// and is always present (a missing snapshot cannot happen for the default path).
// It is the same fixture the resolver tests serve through httptest, and the file
// `atcr models refresh` (Phase 8) regenerates.
//
//go:embed testdata/catalog_snapshot.json
var embeddedSnapshot []byte

// envCatalogSnapshot overrides the embedded catalog snapshot with a file path.
// Tests point it at a temp fixture — including a missing or corrupt one — to
// exercise SnapshotModels' load/parse error paths without touching the checked-in
// snapshot; an operator can also point it at a freshly refreshed snapshot.
const envCatalogSnapshot = "ATCR_CATALOG_SNAPSHOT"

// SnapshotModels parses the checked-in catalog snapshot into the model list with
// zero network I/O. It reads the embedded snapshot by default; when
// ATCR_CATALOG_SNAPSHOT names a file, it reads that instead. A missing file
// surfaces as "failed to load catalog snapshot"; malformed JSON as "failed to
// parse catalog snapshot" (both command failures, mapped to exit 2 by the caller).
func SnapshotModels() ([]CatalogModel, error) {
	data := embeddedSnapshot
	if p := strings.TrimSpace(os.Getenv(envCatalogSnapshot)); p != "" {
		d, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("failed to load catalog snapshot: %w", err)
		}
		data = d
	}
	var resp catalogResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse catalog snapshot: %w", err)
	}
	return resp.Data, nil
}

// snapshotModelOut is the on-disk wire shape `atcr models refresh` writes, with the
// same JSON keys CatalogModel consumes (mirrors catalogModelJSON) so a refreshed
// file round-trips through SnapshotModels/FetchModels unchanged.
type snapshotModelOut struct {
	ID             string  `json:"id"`
	CanonicalSlug  string  `json:"canonical_slug"`
	Created        int64   `json:"created"`
	ExpirationDate *string `json:"expiration_date"`
}

// MarshalSnapshot renders models as the {"data":[...]} snapshot envelope with
// stable, human-readable 2-space indentation and a trailing newline — the format
// `atcr models refresh` writes and every resolver test consumes.
func MarshalSnapshot(models []CatalogModel) ([]byte, error) {
	out := struct {
		Data []snapshotModelOut `json:"data"`
	}{Data: make([]snapshotModelOut, len(models))}
	for i, m := range models {
		// CatalogModel and snapshotModelOut share an identical field layout (the
		// latter only adds JSON tags), so a struct conversion is exact.
		out.Data[i] = snapshotModelOut(m)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// WriteSnapshot marshals models into the snapshot envelope and writes it to path
// ATOMICALLY (temp file + rename in the same directory, via writeFileAtomic), so a
// partial or failed write can never truncate or corrupt an existing snapshot — the
// prior file survives any marshal or write failure. Used by `atcr models refresh`.
func WriteSnapshot(path string, models []CatalogModel) error {
	data, err := MarshalSnapshot(models)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}
