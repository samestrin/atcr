package benchmarkimport

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// Run wires flags and streams to an ingestion. It returns a process exit code
// (0 success, 1 error) and never calls os.Exit, so it stays testable.
func Run(ctx context.Context, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("ingest-alibaba-benchmark", flag.ContinueOnError)
	fs.SetOutput(errOut)

	var (
		dataset = fs.String("dataset", "", "path to a local positive_samples.json (default: download from aacr-bench)")
		outDir  = fs.String("out", "benchmarks/standard-v1", "suite directory to write")
		suite   = fs.String("suite", "standard-v1", "suite identity written to suite.json")
		version = fs.String("suite-version", "1.0.0", "suite_version written to suite.json")
		limit   = fs.Int("limit", 18, "number of PR records to sample")
		seed    = fs.Int64("seed", 20260807, "sampling seed; the same seed always selects the same records")
		useGit  = fs.Bool("clone", false, "use the git-clone fallback instead of the GitHub compare API")
	)

	if err := fs.Parse(args); err != nil {
		// -h/-help is a successful request for usage, not a parse failure.
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	fail := func(err error) int {
		_, _ = fmt.Fprintln(errOut, "ingest:", err)
		return 1
	}

	var raw []byte
	var err error
	if *dataset != "" {
		raw, err = os.ReadFile(*dataset)
		if err != nil {
			return fail(fmt.Errorf("reading dataset: %w", err))
		}
	} else {
		_, _ = fmt.Fprintln(errOut, "ingest: downloading", DatasetURL)
		raw, err = FetchDataset(ctx, nil, DatasetURL)
		if err != nil {
			return fail(err)
		}
	}

	records, err := ParseDataset(raw)
	if err != nil {
		return fail(err)
	}

	sampled, err := Sample(records, *limit, *seed)
	if err != nil {
		return fail(err)
	}
	_, _ = fmt.Fprintf(errOut, "ingest: sampled %d of %d records (seed %d)\n", len(sampled), len(records), *seed)

	var fetcher DiffFetcher
	if *useGit {
		cf := &CloneFetcher{}
		defer func() { _ = cf.Cleanup() }()
		fetcher = cf
	} else {
		fetcher = &CompareAPIFetcher{Token: githubToken()}
	}

	res, err := BuildSuite(ctx, Options{
		Records:      sampled,
		OutDir:       *outDir,
		Suite:        *suite,
		SuiteVersion: *version,
		Fetcher:      fetcher,
	})
	if err != nil {
		return fail(err)
	}

	_, _ = fmt.Fprintf(out, "wrote %d case(s) to %s (%d skipped: no mappable category; %d unavailable upstream)\n",
		res.CasesWritten, *outDir, res.Skipped, res.Unavailable)
	return 0
}

func githubToken() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
