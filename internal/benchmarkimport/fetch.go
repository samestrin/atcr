package benchmarkimport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/gitexec"
)

// DatasetURL is the canonical location of aacr-bench's review-comment
// positive samples (Apache-2.0). See benchmarks/standard-v1/NOTICE.md.
//
// Pinned to an immutable commit rather than main: Sample() shuffles the whole
// record pool, so a single record added or removed upstream would change the
// entire selection and make the committed suite unreproducible.
const DatasetURL = "https://raw.githubusercontent.com/alibaba/aacr-bench/b3072489eace26efca8bcf2b1ac6a24ba64f82c1/dataset/positive_samples.json"

// maxDatasetBytes bounds the dataset download. The real file is ~1.1 MB; the
// ceiling keeps a redirect to something unexpected from being read into memory.
const maxDatasetBytes = 64 << 20

// maxDiffBytes bounds a fetched diff. It mirrors benchmark.MaxDiffBytes — the
// runner's per-diff ceiling — and is a var so tests can lower it.
var maxDiffBytes = benchmark.MaxDiffBytes

// FetchDataset downloads positive_samples.json. This is an authoring-time
// action — the suite it produces is committed, so no test and no benchmark run
// depends on this reaching the network.
func FetchDataset(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building dataset request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading dataset: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading dataset: unexpected status %s", resp.Status)
	}
	// Bounded so an oversized payload fails here as a size problem rather than
	// surfacing downstream as an opaque "unexpected end of JSON input" from
	// ParseDataset — the same +1 pattern the compare fetcher uses below.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDatasetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading dataset: %w", err)
	}
	if len(body) > maxDatasetBytes {
		return nil, fmt.Errorf("dataset exceeds the %d-byte ceiling", maxDatasetBytes)
	}
	return body, nil
}

// CompareAPIFetcher is the primary diff source: GitHub's compare endpoint
// returns the same unified diff a local clone would produce, without cloning
// repositories that run to multiple gigabytes.
type CompareAPIFetcher struct {
	Client *http.Client
	// Token authenticates the request. Optional, but the unauthenticated rate
	// limit (60/hr) is low enough that a real ingestion run needs one.
	Token string
	// baseURL overrides the API host in tests. Empty means api.github.com.
	baseURL string
}

// FetchDiff implements DiffFetcher against api.github.com's compare endpoint.
func (f *CompareAPIFetcher) FetchDiff(ctx context.Context, owner, repo, base, head string) ([]byte, error) {
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	host := f.baseURL
	if host == "" {
		host = "https://api.github.com"
	}
	// Escaped so a crafted owner/repo value in the dataset cannot redirect the
	// call with "../" segments.
	url := fmt.Sprintf("%s/repos/%s/%s/compare/%s...%s", strings.TrimSuffix(host, "/"),
		neturl.PathEscape(owner), neturl.PathEscape(repo), neturl.PathEscape(base), neturl.PathEscape(head))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building compare request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3.diff")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if f.Token != "" {
		req.Header.Set("Authorization", "Bearer "+f.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("compare %s/%s: %w", owner, repo, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// A 404 here means the compare range no longer resolves — the PR's commits
	// were force-pushed away or garbage-collected. That is this record's problem
	// alone, so it is reported as unavailable rather than failing the ingestion.
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("compare %s/%s %s...%s: %w", owner, repo, base, head, ErrDiffUnavailable)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("compare %s/%s %s...%s: unexpected status %s", owner, repo, base, head, resp.Status)
	}
	// Bounded so an oversized PR fails here, at the record that caused it,
	// rather than after the suite is written and committed.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiffBytes+1))
	if err != nil {
		return nil, fmt.Errorf("compare %s/%s: reading diff: %w", owner, repo, err)
	}
	if int64(len(body)) > maxDiffBytes {
		return nil, fmt.Errorf("compare %s/%s %s...%s: diff exceeds the %d-byte runner ceiling",
			owner, repo, base, head, maxDiffBytes)
	}
	return body, nil
}

// CloneFetcher is the documented fallback for when the compare API is
// unavailable (rate limit exhausted, network policy, or a repository whose
// compare range GitHub refuses to render). It reproduces the same diff from a
// blobless clone, which is slower and far heavier on disk.
type CloneFetcher struct {
	// WorkDir holds the per-repository clones. Defaults to a temp directory
	// created once on first use and reused for the rest of the run.
	WorkDir string
	// BaseURL overrides the clone host. Defaults to https://github.com; tests
	// and mirrors point it elsewhere.
	BaseURL string

	tempOnce sync.Once
	tempDir  string
	tempErr  error
}

// workDir resolves the clone root exactly once. Allocating a fresh temp
// directory per call would defeat the per-repository clone cache below and
// leave one full clone on disk for every case ingested.
func (f *CloneFetcher) workDir() (string, error) {
	if f.WorkDir != "" {
		return f.WorkDir, nil
	}
	f.tempOnce.Do(func() {
		f.tempDir, f.tempErr = os.MkdirTemp("", "aacr-clone-")
	})
	return f.tempDir, f.tempErr
}

// Cleanup removes the self-allocated temp clone root — blobless clones run to
// gigabytes, so leaving one per repository on disk permanently is not an
// option. Call once at the end of the run. A caller-supplied WorkDir is the
// caller's to manage and is never touched.
func (f *CloneFetcher) Cleanup() error {
	if f.WorkDir != "" || f.tempDir == "" {
		return nil
	}
	return os.RemoveAll(f.tempDir)
}

func (f *CloneFetcher) cloneURL(owner, repo string) string {
	base := f.BaseURL
	if base == "" {
		base = "https://github.com"
	}
	return fmt.Sprintf("%s/%s/%s.git", strings.TrimSuffix(base, "/"), owner, repo)
}

// isUnavailableRef reports whether git's fetch output means the requested
// object no longer exists upstream, as opposed to a network or auth failure.
func isUnavailableRef(msg string) bool {
	return strings.Contains(msg, "couldn't find remote ref") ||
		strings.Contains(msg, "not our ref")
}

// FetchDiff implements DiffFetcher by cloning and diffing locally.
func (f *CloneFetcher) FetchDiff(ctx context.Context, owner, repo, base, head string) ([]byte, error) {
	work, err := f.workDir()
	if err != nil {
		return nil, fmt.Errorf("creating clone workdir: %w", err)
	}
	dir := filepath.Join(work, owner+"__"+repo)

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		clone := gitexec.CommandContextFn(ctx, "clone", "--filter=blob:none", "--no-checkout",
			f.cloneURL(owner, repo), dir)
		if out, err := clone.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("cloning %s/%s: %w: %s", owner, repo, err, strings.TrimSpace(string(out)))
		}
	}

	// PR head commits are frequently unreachable from any branch tip, so fetch
	// the exact objects rather than relying on the default refspec. The depth is
	// not pinned to 1: the three-dot diff below needs the merge base, which a
	// single-commit fetch cannot supply.
	fetch := gitexec.CommandContextFn(ctx, "-C", dir, "fetch", "origin", base, head)
	if out, err := fetch.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		// A ref the remote no longer has is this record's problem alone — the
		// PR was force-pushed or garbage-collected — so it is reported as
		// unavailable and skipped, mirroring the compare fetcher's 404.
		if isUnavailableRef(msg) {
			return nil, fmt.Errorf("fetching %s..%s in %s/%s: %w: %s", base, head, owner, repo, ErrDiffUnavailable, msg)
		}
		return nil, fmt.Errorf("fetching %s..%s in %s/%s: %w: %s", base, head, owner, repo, err, msg)
	}

	// Three-dot, matching the compare API's base...head semantics. Two-dot would
	// inject the reverse of base-side commits whenever the base branch moved
	// after the fork point, producing different bytes — and therefore a
	// different reproducibility hash — than the primary fetcher. The hardening
	// flags are the repo-wide convention for diff-family call sites (see
	// internal/gitexec's package doc): the bytes produced here land in a
	// committed benchmark diff and its published hash.
	diff := gitexec.CommandContextFn(ctx, "-C", dir, "diff", "--no-ext-diff", "--no-color", "--no-textconv", base+"..."+head)
	var stderr bytes.Buffer
	diff.Stderr = &stderr
	stdout, err := diff.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("diffing %s..%s in %s/%s: %w", base, head, owner, repo, err)
	}
	if err := diff.Start(); err != nil {
		return nil, fmt.Errorf("diffing %s..%s in %s/%s: %w", base, head, owner, repo, err)
	}
	out, readErr := io.ReadAll(io.LimitReader(stdout, maxDiffBytes+1))
	waitErr := diff.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("diffing %s..%s in %s/%s: reading diff: %w", base, head, owner, repo, readErr)
	}
	if waitErr != nil {
		// git's own stderr is the diagnostic (e.g. "no merge base" on a partial
		// clone); a bare "exit status 128" sends the operator nowhere.
		return nil, fmt.Errorf("diffing %s..%s in %s/%s: %w: %s", base, head, owner, repo, waitErr, strings.TrimSpace(stderr.String()))
	}
	// Bounded like the compare fetcher, so an oversized PR fails at the record
	// that caused it instead of exhausting memory or being committed and
	// failing later inside ReproHashManifest.
	if int64(len(out)) > maxDiffBytes {
		return nil, fmt.Errorf("diffing %s..%s in %s/%s: diff exceeds the %d-byte runner ceiling",
			base, head, owner, repo, maxDiffBytes)
	}
	return out, nil
}
