package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeProjectDebtConfig(t *testing.T, root, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(DefaultProjectConfigPath(root), []byte(body), 0o644))
}

func writeUserDebtRegistry(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "registry.yaml"), []byte(body), 0o644))
}

func TestLoadProjectDebtStore(t *testing.T) {
	cases := []struct {
		name, body, want string
		wantErr          bool
	}{
		{name: "nested key", body: "agents: [a]\ndebt:\n  store: planning/debt\n", want: "planning/debt"},
		{name: "no debt block", body: "agents: [a]\n", want: ""},
		{name: "debt block without store", body: "debt: {}\n", want: ""},
		{name: "null store", body: "debt:\n  store:\n", want: ""},
		{name: "store not a string", body: "debt:\n  store: [a, b]\n", wantErr: true},
		{name: "debt not a mapping", body: "debt: planning/debt\n", wantErr: true},
		{name: "unparseable", body: "debt: [\n", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeProjectDebtConfig(t, root, tc.body)
			got, err := LoadProjectDebtStore(root)
			if tc.wantErr {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), root, "the error must not carry the absolute config path")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLoadProjectDebtStore_MissingConfigIsUnset(t *testing.T) {
	got, err := LoadProjectDebtStore(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

func TestLoadUserDebtStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := LoadUserDebtStore()
	require.NoError(t, err, "a missing user registry is unset, not an error")
	assert.Equal(t, "", got)

	writeUserDebtRegistry(t, home, "providers: {}\nagents: {}\ndebt:\n  store: /srv/planning/debt\n")
	got, err = LoadUserDebtStore()
	require.NoError(t, err)
	assert.Equal(t, "/srv/planning/debt", got)

	writeUserDebtRegistry(t, home, "debt:\n  store: 7\n")
	_, err = LoadUserDebtStore()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), home)
}

// The user registry is read from the local file even when ATCR_REGISTRY_URL
// points the rest of the registry at a shared team copy: the store path is
// per-user, so a shared registry must not be able to choose it, and a debt
// command must not pay for an HTTP fetch.
func TestLoadUserDebtStore_IgnoresRemoteRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATCR_REGISTRY_URL", "http://127.0.0.1:1/registry.yaml")
	writeUserDebtRegistry(t, home, "debt:\n  store: ../planning/debt\n")

	got, err := LoadUserDebtStore()
	require.NoError(t, err)
	assert.Equal(t, "../planning/debt", got)
}

// The strict loaders decode with KnownFields, so the debt block must be declared
// on both structs: an undeclared key would make every review in a configured repo
// fail with "field debt not found".
func TestDebtBlockIsKnownField(t *testing.T) {
	root := t.TempDir()
	writeProjectDebtConfig(t, root, "agents: [a]\ndebt:\n  store: planning/debt\n")
	cfg, err := LoadProjectConfig(DefaultProjectConfigPath(root))
	require.NoError(t, err)
	require.NotNil(t, cfg.Debt)
	assert.Equal(t, "planning/debt", cfg.Debt.Store)

	home := t.TempDir()
	writeUserDebtRegistry(t, home, "providers:\n  p:\n    api_key_env: K\nagents:\n  a:\n    provider: p\n    model: m\ndebt:\n  store: /srv/planning/debt\n")
	reg, err := LoadRegistry(filepath.Join(home, ".config", "atcr", "registry.yaml"))
	require.NoError(t, err)
	require.NotNil(t, reg.Debt)
	assert.Equal(t, "/srv/planning/debt", reg.Debt.Store)
}
