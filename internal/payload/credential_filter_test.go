package payload

import (
	"context"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// credentialFixture commits a source file next to a .env and a private key,
// with no .gitignore/.atcrignore, then changes all three: the pre-release
// probe's shape, where a committed .env reached the model unchanged.
func credentialFixture(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = initRepo(t)
	write(t, dir, "main.go", goFileV1)
	write(t, dir, ".env", "DB_PASSWORD=hunter2hunter2\n")
	write(t, dir, "certs/server.key", "key v1\n")
	base = commitAll(t, dir, "v1")
	write(t, dir, "main.go", goFileV2)
	write(t, dir, ".env", "DB_PASSWORD="+strings.Join([]string{"Sup3rS3", "cretPassw0rd"}, "")+"\n")
	write(t, dir, "certs/server.key", "key v2\n")
	head = commitAll(t, dir, "v2")
	return dir, base, head
}

func TestBuildEntries_ExcludesCredentialFiles(t *testing.T) {
	dir, base, head := credentialFixture(t)
	for name, opts := range map[string][]RangeOption{
		"default":   nil,
		"no-ignore": {WithoutIgnoreFilter()},
	} {
		t.Run(name, func(t *testing.T) {
			rb := NewRangeBuilder(context.Background(), dir, base, head, opts...)
			entries, err := rb.BuildEntries(ModeDiff)
			require.NoError(t, err)
			assert.Equal(t, []string{"main.go"}, entryPaths(entries))

			out, err := BuildDiff(context.Background(), dir, base, head)
			require.NoError(t, err)
			assert.NotContains(t, out, "Sup3rS3cretPassw0rd")
			assert.NotContains(t, out, "server.key")
		})
	}
}

func TestEnumerateRepoFiles_ExcludesCredentialFiles(t *testing.T) {
	dir, _, _ := credentialFixture(t)
	for _, noIgnore := range []bool{false, true} {
		entries, _, err := enumerateRepoFiles(context.Background(), dir, log.Discard(), noIgnore, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"main.go"}, sortedPaths(entries), "noIgnore=%v", noIgnore)
	}
}
