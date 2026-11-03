package localdebt

import (
	"os"
	"testing"
)

// TestMain points HOME at an empty directory for the whole package. The store
// resolver (StoreDir) reads debt.store from the user registry under HOME, so
// without this a developer whose own ~/.config/atcr/registry.yaml sets a store
// would have every persistence test write into that real store. Tests that
// exercise the user tier set their own HOME on top.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "localdebt-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
