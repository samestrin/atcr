package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/atomicfs"
)

// manifestFile is the provenance file at the review-dir root (not under
// reconciled/), matching internal/fanout's path.
const manifestFile = "manifest.json"

// verifyStage is the stage name the verify run records in the manifest.
const verifyStage = "verify"

// computeManifestStageBytes reads reviewDir/manifest.json, appends "verify" to
// the stages list (seeding "review" first if absent), and returns the path plus
// the bytes to write. Returns noOp=true when "verify" is already present so the
// caller can skip the artifact in a writeGroupAtomic batch.
func computeManifestStageBytes(reviewDir string) (path string, data []byte, noOp bool, err error) {
	path = filepath.Join(reviewDir, manifestFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, false, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", nil, false, fmt.Errorf("parsing manifest.json: %w", err)
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
			return path, nil, true, nil // already recorded — no rewrite needed
		}
	}
	if len(stages) == 0 {
		stages = []string{"review"}
	}
	m["stages"] = append(stages, verifyStage)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", nil, false, err
	}
	return path, append(out, '\n'), false, nil
}

// UpdateManifestStage appends "verify" to the manifest's Stages list,
// idempotently (AC 03-04). The manifest lives at reviewDir/manifest.json (root),
// not under reconciled/. A manifest that predates the stages field (parsed as
// empty) is seeded with "review" first so the result is ["review", "verify"]. A
// missing manifest is returned as os.ErrNotExist; a malformed one as a parse
// error, leaving the file untouched.
func UpdateManifestStage(reviewDir string) error {
	path, data, noOp, err := computeManifestStageBytes(reviewDir)
	if err != nil {
		return err
	}
	if noOp {
		return nil
	}
	return atomicfs.WriteFileAtomic(path, data)
}
