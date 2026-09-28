package doctor

import (
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
)

// internal/registry is a leaf and cannot import payload, so its thinking
// budget warning compares against its own copy of the review's default output
// cap. The copy must match the cap the review actually applies.
func TestRegistryDefaultMaxTokens_MatchesPayload(t *testing.T) {
	assert.Equal(t, payload.DefaultOutputTokens, registry.DefaultMaxTokens)
}
