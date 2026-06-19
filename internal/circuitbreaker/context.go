package circuitbreaker

import "context"

// providerKey is the unexported context key for the per-call provider name, so
// no other package can collide with or overwrite it.
type providerKey struct{}

// NewContext returns a context carrying the logical provider name for the call.
// The fan-out engine sets it once per agent invocation; llmclient.send reads it
// to key the per-provider breaker. An empty provider (or a context with no value)
// disables the breaker for that call — which is the correct behaviour for a
// diagnostic like `atcr doctor` that must probe every endpoint regardless of
// circuit state.
func NewContext(ctx context.Context, provider string) context.Context {
	return context.WithValue(ctx, providerKey{}, provider)
}

// ProviderFromContext returns the provider name set by NewContext, or "" when no
// provider was attached (the breaker then no-ops for that call).
func ProviderFromContext(ctx context.Context) string {
	p, _ := ctx.Value(providerKey{}).(string)
	return p
}
