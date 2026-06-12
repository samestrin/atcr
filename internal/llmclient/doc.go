// Package llmclient is the OpenAI-compatible chat-completions client: Bearer
// auth resolved from env vars at invoke time, retry on 429/5xx with tuned
// backoff, and per-agent temperature/timeout.
package llmclient
