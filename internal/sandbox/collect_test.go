package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCollect_DropsEscapesAndJunk(t *testing.T) {
	got := normalizeCollect([]string{"a.png", "./a.png", "d/../b.txt", "../etc/passwd", "a/../../x", "/abs", "", "..", ".", "x\ny", "-flag"})
	assert.Equal(t, []string{"a.png", "b.txt", "-flag"}, got)
}

func TestCollectFromTree(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("s3"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "shot.png"), []byte("PNG"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "dirlink")))
	require.NoError(t, os.WriteFile(filepath.Join(root, "big"), bytes.Repeat([]byte("x"), CollectMaxFileBytes+10), 0o600))

	files := collectFromTree(root, []string{"shot.png", "link", "dirlink/secret", "../x", "missing", "big"})
	assert.Equal(t, []byte("PNG"), files["shot.png"])
	assert.NotContains(t, files, "link")
	assert.NotContains(t, files, "dirlink/secret")
	assert.NotContains(t, files, "../x")
	assert.NotContains(t, files, "missing")
	assert.Len(t, files["big"], CollectMaxFileBytes, "past the per-file cap is truncated")
}

func TestCollectFromTree_TotalCap(t *testing.T) {
	root := t.TempDir()
	var names []string
	for i := 0; i < 6; i++ {
		n := string(rune('a' + i))
		names = append(names, n)
		require.NoError(t, os.WriteFile(filepath.Join(root, n), bytes.Repeat([]byte("x"), CollectMaxFileBytes), 0o600))
	}
	files := collectFromTree(root, names)
	total := 0
	for _, b := range files {
		total += len(b)
	}
	assert.Equal(t, CollectMaxTotalBytes, total)
	assert.Len(t, files, 4)
}

func TestFrameWriter_SplitsAcrossWrites(t *testing.T) {
	var out bytes.Buffer
	fw := newFrameWriter(&out, "\nMARK:begin", 8)
	for _, chunk := range []string{"hello\n", "\nMA", "RK:be", "gin\nABCDEFGHIJ"} {
		_, _ = fw.Write([]byte(chunk))
	}
	fw.finish()
	assert.Equal(t, "hello\n", out.String())
	assert.Equal(t, "\nABCDEFG", fw.frame.String(), "frame has its own cap")
}

func TestFrameWriter_NoMarkerFlushesEverything(t *testing.T) {
	var out bytes.Buffer
	fw := newFrameWriter(&out, "\nMARK:begin", 8)
	_, _ = fw.Write([]byte("plain out\nMAR"))
	fw.finish()
	assert.Equal(t, "plain out\nMAR", out.String())
}

func tarFrame(t *testing.T, entries []tar.Header, bodies []string) []byte {
	t.Helper()
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	for i := range entries {
		entries[i].Size = int64(len(bodies[i]))
		require.NoError(t, tw.WriteHeader(&entries[i]))
		_, err := tw.Write([]byte(bodies[i]))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return []byte("\n" + base64.StdEncoding.EncodeToString(tb.Bytes()) + "\nM:end\n")
}

func TestParseCollectFrame_RegularOnlyAndWanted(t *testing.T) {
	frame := tarFrame(t, []tar.Header{
		{Name: "./ok.png", Typeflag: tar.TypeReg, Mode: 0o600},
		{Name: "./link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		{Name: "./unrequested", Typeflag: tar.TypeReg, Mode: 0o600},
		{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o600},
	}, []string{"PNG", "", "u", "e"})
	got := parseCollectFrame(frame, "M:end", []string{"ok.png", "link", "../escape"})
	assert.Equal(t, map[string][]byte{"ok.png": []byte("PNG")}, got)
}

func TestParseCollectFrame_GarbageIsEmpty(t *testing.T) {
	assert.Empty(t, parseCollectFrame([]byte("\n!!!not base64\n"), "M:end", []string{"a"}))
}

// fakeDockerFrameBody emits program output, then a frame built from $FRAME_FILE
// using the nonce Run passes as -e ATCR_COLLECT_MARK=..., like the real
// in-container pass would.
func fakeDockerFrameBody() string {
	return `if [ "$1" = "run" ]; then
  for a in "$@"; do case "$a" in ATCR_COLLECT_MARK=*) mark=${a#ATCR_COLLECT_MARK=};; esac; done
  printf 'program output'
  printf '\n%s\n' "$mark:begin"
  cat "$FRAME_FILE"
  printf '\n%s\n' "$mark:end"
  exit 0
fi
exit 0`
}

func newFrameBackend(t *testing.T, tarB64 string) *DockerBackend {
	t.Helper()
	ff := filepath.Join(t.TempDir(), "frame")
	require.NoError(t, os.WriteFile(ff, []byte(tarB64), 0o600))
	t.Setenv("FRAME_FILE", ff)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = writeFakeDocker(t, fakeDockerFrameBody())
	return NewDockerBackend(cfg)
}

func TestDockerBackendRun_CollectsFilesAndStripsFrame(t *testing.T) {
	frame := tarFrame(t, []tar.Header{
		{Name: "./out/shot.png", Typeflag: tar.TypeReg, Mode: 0o600},
		{Name: "./sym", Typeflag: tar.TypeSymlink, Linkname: "x"},
		{Name: "./big", Typeflag: tar.TypeReg, Mode: 0o600},
	}, []string{"PNGDATA", "", strings.Repeat("x", CollectMaxFileBytes+5)})
	frame = bytes.TrimSuffix(frame, []byte("\nM:end\n"))
	b := newFrameBackend(t, string(frame))

	res, err := b.Run(context.Background(), RunSpec{
		Command: []string{"true"}, SnapshotDir: t.TempDir(), Writable: true,
		Collect: []string{"out/shot.png", "sym", "../etc/passwd", "big"},
	})
	require.NoError(t, err)
	assert.Equal(t, "program output", res.Output, "frame is stripped from Output")
	assert.Equal(t, []byte("PNGDATA"), res.Files["out/shot.png"])
	assert.NotContains(t, res.Files, "sym")
	assert.NotContains(t, res.Files, "../etc/passwd")
	assert.Len(t, res.Files["big"], CollectMaxFileBytes)
}

func TestDockerBackendRun_CollectIgnoredUnlessWritable(t *testing.T) {
	b := newFrameBackend(t, "")
	res, err := b.Run(context.Background(), RunSpec{
		Command: []string{"true"}, SnapshotDir: t.TempDir(), Collect: []string{"a"},
	})
	require.NoError(t, err)
	assert.NotNil(t, res.Files)
	assert.Empty(t, res.Files)
	assert.NotContains(t, res.Output, "atcr-collect")
}

func TestDockerRunArgs_CollectAddsNoMount(t *testing.T) {
	cfg := DefaultDockerConfig()
	base := RunSpec{Command: []string{"true"}, SnapshotDir: "/tmp/snap", Writable: true}
	with := base
	with.Collect = []string{"out/shot.png"}

	mounts := func(spec RunSpec) []string {
		args, err := dockerRunArgs(cfg, spec)
		require.NoError(t, err)
		var m []string
		for i, a := range args {
			if a == "-v" || a == "--tmpfs" || a == "--mount" {
				m = append(m, a+" "+args[i+1])
			}
		}
		return m
	}
	assert.Equal(t, mounts(base), mounts(with))

	args, _ := dockerRunArgs(cfg, with)
	joined := strings.Join(args, "\n")
	assert.Contains(t, joined, "ATCR_COLLECT=out/shot.png")
	assert.NotContains(t, joined, "shot.png\n/bin/sh", "path is env data, not interpolated into argv text")

	// Without Writable the argv is byte-identical to a run with no Collect.
	ro := RunSpec{Command: []string{"true"}, SnapshotDir: "/tmp/snap", Collect: []string{"a"}}
	a1, _ := dockerRunArgs(cfg, ro)
	ro.Collect = nil
	a2, _ := dockerRunArgs(cfg, ro)
	assert.Equal(t, a2, a1)
}

// TestCollectFrameShell_RealShell runs the actual in-container framing function
// against a temp "work" dir, so the symlink/escape filtering and the
// tar|base64 wire format are exercised for real, not just mocked.
func TestCollectFrameShell_RealShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell only")
	}
	for _, tool := range []string{"tar", "head", "base64", "dirname"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
	work := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("s3"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(work, "out"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(work, "out", "shot.png"), []byte("PNG"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret"), filepath.Join(work, "link")))
	require.NoError(t, os.Symlink(outside, filepath.Join(work, "dirlink")))
	realWork, err := filepath.EvalSymlinks(work)
	require.NoError(t, err)

	collect := []string{"out/shot.png", "link", "dirlink/secret", "missing"}
	script := collectFrameShell(realWork) + "\n_atcr_collect\n"
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = append(os.Environ(), "ATCR_COLLECT="+strings.Join(collect, "\n"), "ATCR_COLLECT_MARK=MK")
	out, err := cmd.Output()
	require.NoError(t, err)

	var buf bytes.Buffer
	fw := newFrameWriter(&buf, "\nMK:begin", collectFrameCap)
	_, _ = fw.Write(append([]byte("prog out"), out...))
	fw.finish()
	assert.Equal(t, "prog out", buf.String())
	files := parseCollectFrame(fw.frame.Bytes(), "MK:end", collect)
	assert.Equal(t, map[string][]byte{"out/shot.png": []byte("PNG")}, files)
}

func TestCollectPaths_GatedOnWritable(t *testing.T) {
	assert.Nil(t, collectPaths(RunSpec{Collect: []string{"a"}}))
	assert.Equal(t, []string{"a"}, collectPaths(RunSpec{Writable: true, Collect: []string{"a"}}))
}

func TestNormalizeCollect_CapsEntryCount(t *testing.T) {
	var in []string
	for i := 0; i < collectMaxPaths+20; i++ {
		in = append(in, fmt.Sprintf("f%d", i))
	}
	got := normalizeCollect(in)
	require.Len(t, got, collectMaxPaths)
	assert.Equal(t, "f0", got[0])
}

func TestCollectFromTree_TotalCapTruncatesMidFile(t *testing.T) {
	root := t.TempDir()
	var names []string
	sizes := []int{3.5 * 1024 * 1024, 3.5 * 1024 * 1024, 3.5 * 1024 * 1024, 3.5 * 1024 * 1024, CollectMaxFileBytes, 10}
	for i, sz := range sizes {
		n := fmt.Sprintf("f%d", i)
		names = append(names, n)
		require.NoError(t, os.WriteFile(filepath.Join(root, n), bytes.Repeat([]byte("x"), sz), 0o600))
	}
	files := collectFromTree(root, names)
	assert.Len(t, files["f4"], CollectMaxTotalBytes-4*int(3.5*1024*1024), "file crossing the total cap is truncated to the remainder")
	assert.NotContains(t, files, "f5", "files after the budget is spent are dropped")
}

func TestCollectFromTree_MissingRootIsEmpty(t *testing.T) {
	assert.Empty(t, collectFromTree(filepath.Join(t.TempDir(), "nope"), []string{"a"}))
}

func TestDockerBackendRun_ScriptModeCollects(t *testing.T) {
	frame := tarFrame(t, []tar.Header{{Name: "./o.txt", Typeflag: tar.TypeReg, Mode: 0o600}}, []string{"DATA"})
	frame = bytes.TrimSuffix(frame, []byte("\nM:end\n"))
	stdinFile := filepath.Join(t.TempDir(), "stdin")
	t.Setenv("STDIN_FILE", stdinFile)
	t.Setenv("FRAME_FILE", filepath.Join(t.TempDir(), "frame"))
	require.NoError(t, os.WriteFile(os.Getenv("FRAME_FILE"), frame, 0o600))
	cfg := DefaultDockerConfig()
	cfg.DockerPath = writeFakeDocker(t, `if [ "$1" = "run" ]; then cat > "$STDIN_FILE"; `+
		strings.TrimPrefix(fakeDockerFrameBody(), `if [ "$1" = "run" ]; then`))
	b := NewDockerBackend(cfg)

	res, err := b.Run(context.Background(), RunSpec{
		Script: "echo hi\nexit 3\n", SnapshotDir: t.TempDir(), Writable: true, Collect: []string{"o.txt"},
	})
	require.NoError(t, err)
	assert.Equal(t, "program output", res.Output)
	assert.Equal(t, map[string][]byte{"o.txt": []byte("DATA")}, res.Files)

	stdin, err := os.ReadFile(stdinFile)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(stdin), collectScriptPrefix), "script gets the framing prefix")
	assert.True(t, strings.HasSuffix(string(stdin), "echo hi\nexit 3\n"), "script body follows verbatim")
}

func TestOSLevelBackendRun_CollectsFromWritableCopy(t *testing.T) {
	snapshot := t.TempDir()
	cfg := DefaultOSLevelConfig()
	// The shim writes into the writable copy (HOME's sibling `work`, as in the
	// seed test above), including a symlink that must not be collected.
	cfg.ToolPath = writeFakeOSLevel(t, `WORK="$(dirname "$HOME")/work"
mkdir -p "$WORK/out"
printf PNG > "$WORK/out/shot.png"
ln -s /etc/hostname "$WORK/link"
exit 0`)
	cfg.MaxConcurrent = 1
	b := withContainment(t, NewOSLevelBackend(cfg))

	res, err := b.Run(context.Background(), RunSpec{
		Command: []string{"true"}, SnapshotDir: snapshot, Writable: true,
		Collect: []string{"out/shot.png", "link", "../x"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string][]byte{"out/shot.png": []byte("PNG")}, res.Files)

	// Not writable: collection is ignored, Files is an empty non-nil map.
	res, err = b.Run(context.Background(), RunSpec{
		Command: []string{"true"}, SnapshotDir: snapshot, Collect: []string{"out/shot.png"},
	})
	require.NoError(t, err)
	assert.NotNil(t, res.Files)
	assert.Empty(t, res.Files)
}
