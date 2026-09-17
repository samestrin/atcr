package cli

import (
	"context"
	"errors"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
)

// errRepoStateCheckpointUnsupported is returned when --checkpoint is combined with
// a repo-state-v1 suite.
var errRepoStateCheckpointUnsupported = errors.New("--checkpoint is not supported for a repo-state-v1 suite")

var errRepoStateNotImplemented = errors.New("repo-state run not implemented")

// checkRepoStateFlags rejects flag combinations the repo-state tier cannot honor.
func checkRepoStateFlags(suiteFormat, checkpointPath string) error {
	return errRepoStateNotImplemented
}

// executeRepoStateBenchmarkRun executes a repo-state-v1 suite end to end.
func executeRepoStateBenchmarkRun(ctx context.Context, cfg *fanout.ReviewConfig, completer fanout.Completer, suitePath string, generatedAt time.Time) (*benchmark.RunResult, error) {
	return nil, errRepoStateNotImplemented
}
