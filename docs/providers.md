# Providers

> Status: draft — bones for Epic 1.0 task 13; provider examples verified at implementation time.

atcr talks to any OpenAI-compatible `/chat/completions` endpoint. There are two ways to wire a roster, and the right one depends on how heterogeneous it is.

## Direct (always works, zero infrastructure)

Each provider is a `base_url` + an environment variable holding the key:

```yaml
# ~/.config/atcr/registry.yaml
providers:
  openai:
    base_url: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
  openrouter:
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_API_KEY
  local:
    base_url: http://localhost:11434/v1   # ollama, llama.cpp, vllm, ...
    api_key_env: OLLAMA_API_KEY
```

Keys are resolved from the environment at invoke time — never stored, never written to disk by atcr.

> **Local endpoints still need an `api_key_env` placeholder.** `api_key_env` is required on every provider, and atcr errors at invoke time if the named variable is unset or empty — even for the keyless `local` endpoint above. Export any non-empty value before running (`export OLLAMA_API_KEY=local-no-key-needed`); the value never travels beyond wherever `base_url` points, so with `base_url` on `localhost` nothing leaves the machine. This is the zero-egress path for the shipped `local` personas (`gerald`, `orson`, `liam`) — see [personas-install.md](personas-install.md#local--privacy-first-zero-data-egress--opt-in).

## When you need what

- **Prompt-based panel review: nothing is required.** Direct provider blocks like the ones above are the complete setup — no proxy, no infrastructure.
- **Tool-calling reviewers (shipped, opt-in per agent via `tools: true`): a normalizing proxy is recommended — not required — for maximum compatibility.** Function-calling dialects vary enough across providers that a proxy meaningfully raises the fraction of your roster that tool-calls cleanly.

## The recommended proxy: LiteLLM (or similar)

Route providers through a normalizing proxy such as [LiteLLM](https://github.com/BerriAI/litellm) and point atcr at the proxy as a single provider. A proxy buys:

- **Tool-call normalization.** Providers diverge on function-calling dialects: parallel tool calls, tool-call JSON emitted as plain text in `content`, off-spec "compatible" servers. A proxy flattens most of this before atcr sees it.
- **Reasoning/thinking token normalization.** Reasoning models return thinking in different fields (`reasoning_content`, thinking blocks, vendor extensions). atcr extracts findings from `message.content`, falling back to `reasoning_content` only when `content` is empty (a reasoning model that exhausted its output budget); a proxy standardizes where reasoning lands so it never pollutes responses or wastes budget unpredictably.
- **One endpoint, one key.** A 12-model roster becomes one `base_url` and one env var.
- **Provider-side rate limiting and model aliases.** Rate limits handled at the proxy can shrink or eliminate the `serial_agents` lane; aliases let the registry survive model deprecations without edits.

## Privacy and redaction (PII / secrets)

Review payloads are source code — and diffs are disproportionately likely to contain the sensitive parts (config, credentials being rotated, customer fixtures). When the roster includes third-party providers, consider:

- **Proxy-level redaction guardrails.** LiteLLM integrates [Microsoft Presidio](https://github.com/microsoft/presidio) as a guardrail hook, masking PII in requests before they leave your network — transparent to atcr.
- **Do NOT use the default entity set on code.** Presidio's defaults are tuned for prose and will mangle diffs: in testing on a code sample they flagged `port=8080` as `LOCATION` (0.85), `user.email` as `URL`, an IP as both `IP_ADDRESS` and `PHONE_NUMBER`, and double-flagged a credit card as bank/driver-license — while **missing an actual `sk-live-…` API key entirely**. Masking those code tokens degrades review quality for zero security benefit.
- **Recommended default: a high-precision entity set only.** Restrict masking to entities that don't collide with code identifiers. This masks real PII cleanly and leaves code untouched:

  ```yaml
  # litellm config.yaml
  guardrails:
    - guardrail_name: presidio-mask
      litellm_params:
        guardrail: presidio
        mode: pre_call
        default_on: true
        pii_entities_config:
          EMAIL_ADDRESS: MASK
          CREDIT_CARD: MASK
          US_SSN: MASK
          IBAN_CODE: MASK
          US_BANK_NUMBER: MASK
          CRYPTO: MASK
  ```
  ```bash
  # litellm .env  (presidio-analyzer / -anonymizer reachable on the proxy's network)
  PRESIDIO_ANALYZER_API_BASE=http://presidio-analyzer:3000
  PRESIDIO_ANONYMIZER_API_BASE=http://presidio-anonymizer:3000
  ```
  Explicitly **exclude** `PERSON`, `LOCATION`, `URL`, `PHONE_NUMBER`, `IP_ADDRESS`, `DATE_TIME` — all high-noise on source. Roll out with `mode: logging_only` first if you want to observe detections before enforcing.
- **Presidio is a PII masker, not a secret scanner.** It does not reliably catch API keys / tokens / high-entropy secrets. For those, a **pre-commit secret scanner (e.g. [gitleaks](https://github.com/gitleaks/gitleaks)) is the real control** — it stops secrets from ever entering a diff. Use both: gitleaks for secrets, the tuned presidio set for residual PII.
- **The masking tradeoff:** reviewers see placeholders, so a finding *about* a masked value can itself be redacted away. The tuned entity set keeps this to genuinely-sensitive tokens, not code.
- **Roster routing as policy.** Per-agent provider bindings mean sensitive repos can run an all-local roster (ollama/vllm) while open-source repos use frontier APIs — same tool, different registry.

## Defensive posture regardless of proxy

The proxy is a recommendation, not a load-bearing assumption. atcr parses provider responses defensively: strict severity-prefix extraction from `message.content` only, malformed-response retries, per-agent fallbacks, and graceful degrade for models that mishandle tools.
