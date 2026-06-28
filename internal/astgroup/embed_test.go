package astgroup

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEmbeddedParsersMatchManifest pins every embedded .wasm plugin to a
// committed parsers/SHA256SUMS manifest. The .wasm binaries are opaque to code
// review (reviewers read src/*.go, not the compiled blob), so this test makes the
// committed binary a verifiable function of a committed checksum: a tampered,
// stale, or accidentally-modified .wasm is caught by `go test ./...` in CI — no
// Wasm build toolchain required in the pipeline. After regenerating a parser with
// parsers/build.sh, refresh the manifest (cd parsers && sha256sum *.wasm >
// SHA256SUMS) and commit all of it together.
func TestEmbeddedParsersMatchManifest(t *testing.T) {
	f, err := os.Open(filepath.Join("parsers", "SHA256SUMS"))
	require.NoError(t, err, "parsers/SHA256SUMS must be committed alongside the .wasm binaries")
	defer func() { _ = f.Close() }()

	want := map[string]string{} // basename -> hex sha256
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		require.Lenf(t, fields, 2, "SHA256SUMS line must be '<hash>  <file>': %q", line)
		want[filepath.Base(fields[1])] = fields[0]
	}
	require.NoError(t, sc.Err())

	// Every embedded parser must be covered by the manifest and match its hash, so
	// adding a parser without a manifest entry also fails the gate.
	for lang, path := range builtinParsers {
		base := filepath.Base(path)
		hash, ok := want[base]
		require.Truef(t, ok, "manifest missing entry for embedded parser %q (%s)", lang, base)
		b, err := parserFS.ReadFile(path)
		require.NoError(t, err)
		sum := sha256.Sum256(b)
		require.Equalf(t, hash, hex.EncodeToString(sum[:]),
			"embedded %s does not match committed parsers/SHA256SUMS — regenerate it after build.sh", base)
	}
}
