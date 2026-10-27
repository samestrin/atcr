package sandbox

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Collect bounds. They are fixed, not configurable: a collected artifact (a
// screenshot, a trace) is evidence the host holds in memory, so the caps are a
// containment property of the sandbox rather than a caller preference.
const (
	// CollectMaxFileBytes caps one collected file. A larger file is truncated to
	// this length, not dropped.
	CollectMaxFileBytes = 4 << 20
	// CollectMaxTotalBytes caps the sum of all collected file bytes in one run.
	// Once spent, later files are dropped and the file that crosses it is
	// truncated.
	CollectMaxTotalBytes = 16 << 20
	// collectMaxPaths bounds len(RunSpec.Collect); extra entries are ignored.
	collectMaxPaths = 64
	// collectTarSlack is the headroom over CollectMaxTotalBytes granted to the
	// in-container tar stream for its 512-byte headers and block padding.
	collectTarSlack = 128 << 10
)

// normalizeCollect returns the usable entries of RunSpec.Collect: cleaned,
// slash-separated, relative to the writable tree root, deduplicated, at most
// collectMaxPaths of them. Anything that is empty, absolute, contains a NUL or
// newline (the Docker framing passes the list newline-separated), or that
// escapes the tree after cleaning ("..", "a/../../b") is dropped.
func normalizeCollect(paths []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" || strings.ContainsAny(p, "\x00\n") || strings.HasPrefix(p, "/") {
			continue
		}
		c := path.Clean(filepath.ToSlash(p))
		if c == "." || c == ".." || strings.HasPrefix(c, "../") || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
		if len(out) == collectMaxPaths {
			break
		}
	}
	return out
}

// collectPaths is the Collect list that will actually be honoured for spec:
// nil unless the run is Writable (the collected tree is the writable copy).
func collectPaths(spec RunSpec) []string {
	if !spec.Writable {
		return nil
	}
	return normalizeCollect(spec.Collect)
}

// fileCollector accumulates files under the per-file and total byte caps.
type fileCollector struct {
	files map[string][]byte
	total int
}

func newFileCollector() *fileCollector { return &fileCollector{files: map[string][]byte{}} }

// add reads r (truncating at the per-file cap and the remaining total budget)
// and stores it under name. It reports false once the total budget is spent.
func (c *fileCollector) add(name string, r io.Reader) bool {
	remaining := CollectMaxTotalBytes - c.total
	if remaining <= 0 {
		return false
	}
	limit := CollectMaxFileBytes
	if remaining < limit {
		limit = remaining
	}
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)))
	if err != nil && len(data) == 0 {
		return true
	}
	c.files[name] = data
	c.total += len(data)
	return true
}

// collectFromTree reads the requested regular files out of root (the writable
// copy on the host). Symlinks, non-regular files, and any path whose parent
// directory resolves outside root are skipped.
func collectFromTree(root string, paths []string) map[string][]byte {
	c := newFileCollector()
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return c.files
	}
	for _, p := range normalizeCollect(paths) {
		full := filepath.Join(realRoot, filepath.FromSlash(p))
		fi, err := os.Lstat(full)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(full))
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(realRoot, parent); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		f, err := os.Open(full)
		if err != nil {
			continue
		}
		more := c.add(p, f)
		_ = f.Close()
		if !more {
			break
		}
	}
	return c.files
}

// collectFrameShell returns the fixed shell text defining _atcr_collect, the
// in-container framing pass DockerBackend runs after the workload. /work is a
// tmpfs and the container is --rm, so the files must leave on stdout before the
// container exits: it tars the requested regular files, caps the stream, and
// base64-encodes it between two marker lines. The path list arrives in
// $ATCR_COLLECT (newline-separated, already normalized by the host to relative
// clean paths) and the markers in $ATCR_COLLECT_MARK; none of it is
// interpolated into this text. Symlinks, and paths whose directory resolves
// outside workDir, are skipped here as well as on the host.
func collectFrameShell(workDir string) string {
	return `_atcr_collect() { [ -n "$ATCR_COLLECT" ] || return 0; cd -P ` + workDir + ` 2>/dev/null || return 0; set --; _atcr_ifs=$IFS; IFS='
'; set -f; for _atcr_p in $ATCR_COLLECT; do _atcr_p=./$_atcr_p; [ -f "$_atcr_p" ] && [ ! -L "$_atcr_p" ] || continue; _atcr_d=$(cd -P "$(dirname "$_atcr_p")" 2>/dev/null && pwd -P) || continue; case $_atcr_d in ` + workDir + `|` + workDir + `/*) ;; *) continue ;; esac; set -- "$@" "$_atcr_p"; done; IFS=$_atcr_ifs; set +f; [ $# -gt 0 ] || return 0; printf '\n%s\n' "$ATCR_COLLECT_MARK:begin"; tar -cf - "$@" 2>/dev/null | head -c ` + strconv.Itoa(CollectMaxTotalBytes+collectTarSlack) + ` | base64; printf '\n%s\n' "$ATCR_COLLECT_MARK:end"; }`
}

// collectFrameCap is the host-side bound on the captured frame: the base64
// expansion (4/3, plus a newline per 76 columns) of the capped tar stream.
const collectFrameCap = (CollectMaxTotalBytes+collectTarSlack)/3*4 + (CollectMaxTotalBytes+collectTarSlack)/57 + 4096

// collectCommandExec is the Writable+Collect Command-mode `sh -c` text. Unlike
// writableSetupExec it cannot exec the command (the shell must survive to frame
// the files), so it runs it, remembers the status, frames, and exits with the
// command's own status. The command still arrives positionally as "$@".
var collectCommandExec = collectFrameShell("/work") + "; " + writableSeedCopy +
	`; cd /work || exit 125; "$@"; _atcr_rc=$?; _atcr_collect; exit $_atcr_rc`

// collectScriptPrefix is the Writable+Collect Script-mode stdin prefix. The
// framing pass is an EXIT trap so a script that calls `exit` itself is still
// framed, and the script's exit status is preserved.
var collectScriptPrefix = collectFrameShell("/work") + "; " + writableSeedCopy +
	"; cd /work; trap _atcr_collect EXIT\n"

// frameWriter forwards everything to w until it sees the begin marker, then
// diverts the rest (the framed files) into a separate bounded buffer, so the
// frame has its own cap and never counts against, or is cut by, the Output
// budget. Safe for concurrent Write.
type frameWriter struct {
	mu      sync.Mutex
	w       io.Writer
	marker  []byte
	cap     int
	pending []byte
	inFrame bool
	frame   bytes.Buffer
}

func newFrameWriter(w io.Writer, marker string, frameCap int) *frameWriter {
	return &frameWriter{w: w, marker: []byte(marker), cap: frameCap}
}

func (f *frameWriter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inFrame {
		f.divert(p)
		return len(p), nil
	}
	f.pending = append(f.pending, p...)
	if i := bytes.Index(f.pending, f.marker); i >= 0 {
		_, _ = f.w.Write(f.pending[:i])
		f.inFrame = true
		rest := f.pending[i+len(f.marker):]
		f.pending = nil
		f.divert(rest)
		return len(p), nil
	}
	// Hold back only what could still be the start of a marker.
	if keep := len(f.marker) - 1; len(f.pending) > keep {
		cut := len(f.pending) - keep
		_, _ = f.w.Write(f.pending[:cut])
		f.pending = append([]byte(nil), f.pending[cut:]...)
	}
	return len(p), nil
}

func (f *frameWriter) divert(p []byte) {
	if room := f.cap - f.frame.Len(); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		f.frame.Write(p)
	}
}

// finish flushes any held-back bytes that turned out not to be a marker.
func (f *frameWriter) finish() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.inFrame && len(f.pending) > 0 {
		_, _ = f.w.Write(f.pending)
		f.pending = nil
	}
}

// parseCollectFrame decodes the framed payload (base64 of a tar stream, with
// the end marker possibly missing if the frame was capped) into files. Only
// regular entries named in want are kept; the same caps as the host path apply.
// A malformed frame yields whatever decoded cleanly before the damage.
func parseCollectFrame(frame []byte, endMarker string, want []string) map[string][]byte {
	if i := bytes.Index(frame, []byte(endMarker)); i >= 0 {
		frame = frame[:i]
	}
	var b64 []byte
	for _, ch := range frame {
		if ch != '\n' && ch != '\r' && ch != ' ' {
			b64 = append(b64, ch)
		}
	}
	b64 = b64[:len(b64)/4*4] // a capped frame may end mid-quantum
	raw := make([]byte, base64.StdEncoding.DecodedLen(len(b64)))
	n, _ := base64.StdEncoding.Decode(raw, b64)
	raw = raw[:n]

	allowed := map[string]bool{}
	for _, w := range normalizeCollect(want) {
		allowed[w] = true
	}
	c := newFileCollector()
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := tr.Next()
		if err != nil { // EOF, or a truncated/damaged stream: keep what was read
			break
		}
		if h.Typeflag != tar.TypeReg { // symlinks, hardlinks, dirs, devices
			continue
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if !allowed[name] {
			continue
		}
		if !c.add(name, tr) {
			break
		}
	}
	return c.files
}
