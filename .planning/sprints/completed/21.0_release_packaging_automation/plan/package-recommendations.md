# Package Recommendations

Generated: 2026-07-12
Plan: 21.0_release_packaging_automation

## Recommended

### goreleaser (CLI tool, invoked via `goreleaser/goreleaser-action` in CI — not a go.mod dependency)

- **Category:** Release automation / cross-platform build tooling
- **Handles:** AC3 (cross-platform binaries from a tag) and AC4 (tag-triggered GitHub Release publish) in a single declarative `.goreleaser.yaml`. Builds `GOOS`/`GOARCH` matrices, archives, checksums, and creates the GitHub Release with generated notes — the standard tool for this exact job in the Go ecosystem.
- **Install:** `goreleaser/goreleaser-action@v6` in the new tag-triggered workflow (no local install needed for CI; optionally `go install github.com/goreleaser/goreleaser/v2@latest` for local dry-runs via `goreleaser release --snapshot --clean`)
- **Integration point:** New `.github/workflows/release.yml`, invoked after the existing checkout/setup-go steps (reused from `.github/workflows/ci.yml` per the `based_on` convention already established by `.github/workflows/reconcile-module.yml`)
- **Reason:** Purpose-built for exactly this problem (cross-compiled Go binaries + GitHub Releases from a tag), avoids hand-rolling a `GOOS`/`GOARCH` build matrix and release-upload script, and is the de facto standard — high maturity, low integration risk, directly closes AC3+AC4 with one config file.
- **Scores:** maturity 9/10, complexity_saved 8/10, integration_risk 2/10

## Not Recommended

No other high-ROI gaps identified. This plan is packaging/CI infrastructure, not application logic — no new go.mod dependencies are needed for AC1 (tag/version convention decision — documentation only), AC2 (ldflags wiring — uses only the Go toolchain's existing `-ldflags "-X ..."` mechanism against `internal/version.Version` and `cmd/atcr`'s local version var), or AC5 (release-process documentation).
