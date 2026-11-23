package localdebt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStoreDir_UserTildeExpands is the EST-8260 probe: `debt.store: ~/td` in
// the user registry created <repo>/~/td-all/ because the literal "~" was taken
// as a relative path.
func TestStoreDir_UserTildeExpands(t *testing.T) {
	root, home := storeDirEnv(t)
	setUserStore(t, home, "debt:\n  store: ~/td\n")
	got, err := StoreDir(root)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "td"), got)
	_, statErr := os.Lstat(filepath.Join(root, "~"))
	assert.True(t, os.IsNotExist(statErr), "no literal ~ directory in the repo")
}

func TestStoreDir_UserTildeUserRejected(t *testing.T) {
	root, home := storeDirEnv(t)
	setUserStore(t, home, "debt:\n  store: ~bob/td\n")
	_, err := StoreDir(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "debt.store")
}

// The project tier must stay inside the repository, so "~" is refused rather
// than taken as a directory literally named "~".
func TestStoreDir_ProjectTildeRejected(t *testing.T) {
	for _, v := range []string{"~", "~/td", "~bob/td"} {
		t.Run(v, func(t *testing.T) {
			root, _ := storeDirEnv(t)
			setProjectStore(t, root, "debt:\n  store: '"+v+"'\n")
			_, err := StoreDir(root)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "debt.store")
			assert.Contains(t, err.Error(), "~")
		})
	}
}
