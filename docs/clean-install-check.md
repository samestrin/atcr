# Clean-install check (v1.0)

A walk through the README quickstart in a clean environment, following only the README and `docs/`. Re-run it before each release.

**Environment:** empty `HOME`, empty `GOPATH`/`GOBIN`, PATH holding only Go 1.25 and the system tools, a throwaway git repository with a `main` branch and a `feat` branch holding one commit. No Docker was available on the build host, so the isolation is a scratch `HOME` and `GOPATH` rather than a container.

| Step | Command | Result |
|------|---------|--------|
| Install | `go install ./cmd/atcr` (the checkout; `@latest` resolves to the tagged release once 1.0.0 is cut) | Builds in 9 s; `atcr version` runs and prints the one-time telemetry notice |
| Onboard | `atcr quickstart --offline` (Enter at the key prompt) | Writes `.atcr/config.yaml` (keeping `telemetry_notice_shown: true` from the notice), nine personas, `~/.config/atcr/registry.yaml` and `.github/workflows/atcr.yml` |
| Verify | `atcr doctor` | Lists nine agents, each `missing_key` with the hint to set `LLM_SYNTHETIC_API_KEY` |
| Review | `LLM_SYNTHETIC_API_KEY=<invalid> atcr review` | Reaches the provider; all nine agents fail with HTTP 401, as expected for an invalid key |

The onboard step was also run in a second fresh repository without `atcr version` first, as the README orders it: the notice prints, then quickstart scaffolds the nine personas and `atcr doctor` lists nine agents.

**Earlier failure, now fixed:** the first recorded run (Epic 84.0 T5) found that the first-run telemetry notice's record in `.atcr/config.yaml` made quickstart report `Using existing workspace` and write no personas or roster, so `atcr doctor` stopped with `no agents selected`. `atcr init` and `atcr quickstart` now treat a config holding only that record (and any `atcr config set` opt-outs) as unscaffolded, and carry its settings into the new config.

**Not covered:** a successful review against a live provider, because the run had no valid Synthetic key. The review step ran with `ATCR_TELEMETRY=0` so the check sent no usage ping. The 401 shows the config, key lookup and request path work.
