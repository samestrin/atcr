# Clean-install check (v1.0)

A walk through the README quickstart in a clean environment, following only the README and `docs/`. Re-run it before each release.

**Environment:** empty `HOME`, empty `GOPATH`/`GOBIN`, PATH holding only Go 1.25 and the system tools, a throwaway git repository with a `main` branch and a `feat` branch holding one commit. No Docker was available on the build host, so the isolation is a scratch `HOME` and `GOPATH` rather than a container.

| Step | Command | Result |
|------|---------|--------|
| Install | `go install ./cmd/atcr` (the checkout; `@latest` resolves to the tagged release once 1.0.0 is cut) | Builds in 11 s, `atcr version` runs |
| Onboard | `atcr quickstart --offline` | **Failed:** printed `Using existing workspace`, left `.atcr/` holding only a stub `config.yaml` and no personas |
| Verify | `atcr doctor` | **Failed:** `no agents selected` |
| Onboard (fixed) | `ATCR_TELEMETRY=0 atcr quickstart --offline` | Writes `.atcr/config.yaml`, nine personas, `~/.config/atcr/registry.yaml` and `.github/workflows/atcr.yml` |
| Verify | `atcr doctor` | Lists nine agents, each `missing_key` with the hint to set `LLM_SYNTHETIC_API_KEY` |
| Review | `LLM_SYNTHETIC_API_KEY=<invalid> atcr review` | Reaches the provider; all nine agents fail with HTTP 401, as expected for an invalid key |

**Cause of the failure:** the first-run telemetry notice (`cli/telemetry.go`) records `telemetry_notice_shown` in `.atcr/config.yaml` before quickstart looks for an existing workspace, so quickstart treats the stub as a returning user's workspace. The README now documents the workaround; the code fix is filed as technical debt.

**Not covered:** a successful review against a live provider, because the run had no valid Synthetic key. The 401 shows the config, key lookup and request path work.
