// Package version holds the ATCR build version, surfaced in the public
// leaderboard submission envelope (Epic 10.0) as atcr_version so a submission is
// self-describing about which build produced it.
//
// Version is a var (not a const) so a release build can stamp it at link time:
//
//	go build -ldflags "-X github.com/samestrin/atcr/internal/version.Version=1.2.3" ./cmd/atcr
//
// The default "0.0.0" is a deliberate neutral placeholder: the repo carries no
// git tags and go.mod declares no semver, so an unstamped (dev) build reports
// 0.0.0 rather than a misleading real-looking version.
package version

// Version is the ATCR build version. Overridden at link time for releases; the
// dev/default value is the neutral "0.0.0".
var Version = "0.0.0"
