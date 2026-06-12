package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidMode(t *testing.T) {
	assert.True(t, ValidMode("diff"))
	assert.True(t, ValidMode("blocks"))
	assert.True(t, ValidMode("files"))
	assert.False(t, ValidMode("DIFF"))
	assert.False(t, ValidMode(""))
	assert.False(t, ValidMode("nope"))
}
