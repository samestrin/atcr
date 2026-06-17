# Logging

atcr emits all operational diagnostics through a single structured-logging sink
(`internal/log`, wrapping `log/slog`). Logs go to **stderr**; stdout is reserved
for command output and — in `atcr serve` — the MCP protocol stream. Two settings
control logging:

| Setting | Type | Values | Default |
|---------|------|--------|---------|
| `LOG_LEVEL` | environment variable | `debug`, `info`, `warn`, `error` | `info` |
| `--log-format` | persistent flag (all subcommands) | `text`, `json` | `text` |

An invalid `LOG_LEVEL` or `--log-format` is a usage error (exit 2) reported
before any subcommand runs.

## Levels

`LOG_LEVEL` sets the minimum severity emitted:

- `LOG_LEVEL=debug` — everything, including per-agent diagnostics and provider
  retry detail. Use this to diagnose a failing review.
- `LOG_LEVEL=info` (default) — normal operational lines.
- `LOG_LEVEL=warn` — warnings and errors only.
- `LOG_LEVEL=error` — errors only; info and warn are suppressed.

`LOG_LEVEL` is read from the environment rather than a flag so verbosity can be
raised per-invocation without changing the command line.

## Formats

- `--log-format=text` (default) — human-readable lines for local runs.
- `--log-format=json` — newline-delimited JSON (one object per line) for
  machine parsing in CI. Each line carries at least `level` and `msg`.

## Request correlation

Every log line emitted during a review carries a `review_id` attribute, and
every line emitted inside an agent invocation also carries an `agent_name`
attribute. This lets you grep all activity for one run:

```bash
LOG_LEVEL=debug atcr review 2> review.log
grep 'review_id=<id>' review.log          # all lines for one review
grep 'agent_name=security' review.log     # one agent's activity
```

## Redaction

Logging is the single redaction point. Before any record is emitted — at any
level — the sink scrubs:

- **Secrets** — bearer tokens (`Authorization: Bearer …`) and `sk-`-style API
  keys are replaced with `[redacted]`.
- **Absolute paths** — paths under the review root are rendered relative to that
  root, so repo locations do not leak into logs or CI output.

Because redaction runs at the sink, no call site can bypass it by logging a
secret directly.

## Recipes

Debug a failing review locally:

```bash
LOG_LEVEL=debug atcr review
```

Machine-readable logs for CI:

```bash
LOG_LEVEL=debug atcr review --log-format=json 2> review.jsonl
```

MCP server mode keeps stdout protocol-only; logs still go to stderr:

```bash
LOG_LEVEL=debug atcr serve 2> serve.log
```

## See also

- [`internal/errors`](../internal/errors/README.md) — error classification
  taxonomy (transient vs. permanent vs. user vs. system) used by the llmclient
  and surfaced in logs.
