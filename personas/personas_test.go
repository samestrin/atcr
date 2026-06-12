package personas

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNames_ReturnsAllSix(t *testing.T) {
	names := Names()
	require.Len(t, names, 6)
	require.Equal(t, "bruce", names[0])
}

func TestGet_KnownPersona(t *testing.T) {
	s, err := Get("bruce")
	require.NoError(t, err)
	require.NotEmpty(t, s)
}

func TestGet_UnknownPersona(t *testing.T) {
	_, err := Get("nonexistent")
	require.Error(t, err)
}

func TestBase(t *testing.T) {
	s, err := Base()
	require.NoError(t, err)
	require.NotEmpty(t, s)
}
