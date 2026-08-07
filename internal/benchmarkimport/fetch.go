package benchmarkimport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DatasetURL is the canonical location of aacr-bench's expert-verified
// positive samples (Apache-2.0). See benchmarks/standard-v1/NOTICE.md.
const DatasetURL = "https://raw.githubusercontent.com/alibaba/aacr-bench/main/dataset/positive_samples.json"

// maxDatasetBytes bounds the dataset download. The real file is ~1.1 MB; the
// ceiling keeps a redirect to something unexpected from being read into memory.
const maxDatasetBytes = 64 << 20

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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDatasetBytes))
	if err != nil {
		return nil, fmt.Errorf("reading dataset: %w", err)
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
	url := fmt.Sprintf("%s/repos/%s/%s/compare/%s...%s", strings.TrimSuffix(host, "/"), owner, repo, base, head)

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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("compare %s/%s %s...%s: unexpected status %s", owner, repo, base, head, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// CloneFetcher is the documented fallback for when the compare API is
// unavailable (rate limit exhausted, network policy, or a repository whose
// compare range GitHub refuses to render). It reproduces the same diff from a
// blobless clone, which is slower and far heavier on disk.
type CloneFetcher struct {
	// WorkDir holds the per-repository clones. Defaults to a temp directory.
	WorkDir string
	// BaseURL overrides the clone host. Defaults to https://github.com; tests
	// and mirrors point it elsewhere.
	BaseURL string
}

func (f *CloneFetcher) cloneURL(owner, repo string) string {
	base := f.BaseURL
	if base == "" {
		base = "https://github.com"
	}
	return fmt.Sprintf("%s/%s/%s.git", strings.TrimSuffix(base, "/"), owner, repo)
}

// FetchDiff implements DiffFetcher by cloning and diffing locally.
func (f *CloneFetcher) FetchDiff(ctx context.Context, owner, repo, base, head string) ([]byte, error) {
	work := f.WorkDir
	if work == "" {
		var err error
		work, err = os.MkdirTemp("", "aacr-clone-")
		if err != nil {
			return nil, fmt.Errorf("creating clone workdir: %w", err)
		}
	}
	dir := filepath.Join(work, owner+"__"+repo)

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		clone := exec.CommandContext(ctx, "git", "clone", "--filter=blob:none", "--no-checkout",
			f.cloneURL(owner, repo), dir)
		if out, err := clone.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("cloning %s/%s: %w: %s", owner, repo, err, strings.TrimSpace(string(out)))
		}
	}

	// PR head commits are frequently unreachable from any branch tip, so fetch
	// the exact objects rather than relying on the default refspec.
	fetch := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--depth=1", "origin", base, head)
	if out, err := fetch.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("fetching %s..%s in %s/%s: %w: %s", base, head, owner, repo, err, strings.TrimSpace(string(out)))
	}

	diff := exec.CommandContext(ctx, "git", "-C", dir, "diff", base+".."+head)
	out, err := diff.Output()
	if err != nil {
		return nil, fmt.Errorf("diffing %s..%s in %s/%s: %w", base, head, owner, repo, err)
	}
	return out, nil
}
