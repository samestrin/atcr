package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// manifestFile is the provenance file at the review-dir root (not under
// reconciled/), matching internal/fanout's path.
const manifestFile = "manifest.json"

// verifyStage is the stage name the verify run records in the manifest.
const verifyStage = "verify"

// UpdateManifestStage appends "verify" to the manifest's Stages list,
// idempotently (AC 03-04). The manifest lives at reviewDir/manifest.json (root),
// not under reconciled/. It reads and unmarshals the existing manifest, appends
// "verify" only if absent, and re-writes via payload.WriteManifest (atomic). A
// manifest that predates the stages field (parsed as empty) is seeded with
// "review" first so the result is ["review", "verify"]. A missing manifest is
// returned as os.ErrNotExist; a malformed one as a parse error, leaving the file
// untouched.
func UpdateManifestStage(reviewDir string) error {
	path := filepath.Join(reviewDir, manifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return err // includes os.ErrNotExist
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parsing manifest.json: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	rawStages, _ := m["stages"].([]any)
	stages := make([]string, 0, len(rawStages))
	for _, s := range rawStages {
		if str, ok := s.(string); ok {
			stages = append(stages, str)
		}
	}
	for _, s := range stages {
		if s == verifyStage {
			return nil // already recorded — idempotent no-op, no rewrite
		}
	}
	if len(stages) == 0 {
		stages = []string{"review"}
	}
	m["stages"] = append(stages, verifyStage)
	return writeJSONAtomic(path, m)
}
