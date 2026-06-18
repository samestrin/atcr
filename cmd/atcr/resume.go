package main

import "strings"

// resolveResumeDir maps the --resume anchor to a review directory. The literal
// "latest" (and an empty value) resolve the .atcr/latest pointer, matching the
// documented `atcr review --resume latest` form; any other value is delegated to
// resolveReviewDir, which accepts a bare review id (resolved under
// .atcr/reviews/) or an explicit path used verbatim, and verifies the directory
// holds a sources/ tree (otherwise a clear exit-2 usage error). Note that an
// explicit anchor of "latest" can never be a real id: ReviewID always prefixes
// the date (<YYYY-MM-DD>_<slug>), so reserving the word for the pointer is safe.
func resolveResumeDir(anchor string) (string, error) {
	a := strings.TrimSpace(anchor)
	if a == "" || a == "latest" {
		return resolveReviewDir("") // empty arg → .atcr/latest
	}
	return resolveReviewDir(a)
}
