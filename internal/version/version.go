// Package version holds the ATCR build version, surfaced in the public
// leaderboard submission envelope (Epic 10.0) as atcr_version so a submission is
// self-describing about which build produced it.
//
// Version is a var (not a const) so a release build can stamp it at link time:
//
//	go build -ldflags "-X github.com/samestrin/atcr/internal/version.Version=1.2.3" ./cmd/atcr
//
// The default "0.0.0" is a deliberate neutral placeholder: a dev build is not
// linked from a release tag, so it has no version to report and reports 0.0.0
// rather than a misleading real-looking one. docs/release-process.md documents
// the bare vX.Y.Z tag series a release build is stamped from.
package version

// Version is the ATCR build version. Overridden at link time for releases; the
// dev/default value is the neutral "0.0.0".
var Version = "0.0.0"
