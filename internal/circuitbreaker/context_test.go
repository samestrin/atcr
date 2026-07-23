package circuitbreaker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderFromContext_NonStringTypeReturnsEmpty(t *testing.T) {
	// ProviderFromContext uses a type assertion .(string) which returns ("", false)
	// when the value is not a string. This test verifies the fallback behavior.
	ctx := context.WithValue(context.Background(), providerKey{}, 42)
	got := ProviderFromContext(ctx)
	require.Equal(t, "", got, "ProviderFromContext must return empty string when value is not a string")
}
