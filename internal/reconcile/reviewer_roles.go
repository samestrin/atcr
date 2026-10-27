package reconcile

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/atomicfs"
)

// ReviewerRolesJSON is the review-dir sidecar that records each reviewer's
// persona role (Epic 83.0 T5). The review writes it from the registry and
// persona dirs it already loaded, so RunReconcile and `atcr report` can label
// reviewers `name (role)` without loading the registry again (under
// ATCR_REGISTRY_URL that load is an HTTP GET), and the labels name the personas
// the review actually ran.
const ReviewerRolesJSON = "reviewer_roles.json"

// maxReviewerRolesBytes caps the sidecar read. A roster's roles are a few
// hundred bytes; anything near the cap is not a file the review wrote.
const maxReviewerRolesBytes = 1 << 20

type reviewerRolesFile struct {
	Roles map[string]string `json:"roles"`
}

// WriteReviewerRoles writes roles to reviewDir/reviewer_roles.json. An empty
// map writes nothing, so a review whose personas name no role leaves the review
// dir as it was before T5.
func WriteReviewerRoles(reviewDir string, roles map[string]string) error {
	if len(roles) == 0 {
		return nil
	}
	return atomicfs.WriteJSON(filepath.Join(reviewDir, ReviewerRolesJSON), reviewerRolesFile{Roles: roles})
}

// ReadReviewerRoles reads reviewDir/reviewer_roles.json. The roles are display
// metadata, so a sidecar that is missing, not a regular file (a symlink may
// point outside the review dir), over maxReviewerRolesBytes or malformed yields
// nil and the reports keep bare names, never an error.
func ReadReviewerRoles(reviewDir string) map[string]string {
	path := filepath.Join(reviewDir, ReviewerRolesJSON)
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxReviewerRolesBytes+1))
	if err != nil || len(data) > maxReviewerRolesBytes {
		return nil
	}
	var rf reviewerRolesFile
	if err := json.Unmarshal(data, &rf); err != nil || len(rf.Roles) == 0 {
		return nil
	}
	return rf.Roles
}
