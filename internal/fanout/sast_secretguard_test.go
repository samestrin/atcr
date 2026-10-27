package fanout

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/sast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC5 (Epic 50.0): the Fixer is an LLM, and it reads the reconciled finding's
// Problem, which is built from the scanner message and rule in sast.json. A
// credential the scanner quotes must be redacted before that file is written.
func TestRunReview_SASTSecretIsRedactedBeforeTheReconciledReport(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	srv := mockProvider(t)
	hideGitleaks(t)
	leaky := strings.Replace(sastFixture, "call result is never checked", "hardcoded key "+guardAWSKey, 1)
	withSASTRunner(t, &fakeSASTRunner{sarif: leaky}, nil)

	cfg := sastEnabledConfig(srv.URL)
	cfg.Settings.SecretGuard = "redact"
	var res *ReviewResult
	var err error
	captureStderr(t, func() {
		res, err = RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	})
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(res.Dir, sast.ReportFile))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), guardAWSKey, "sast.json is the reconcile input; it must not hold the key")

	_, err = reconcile.RunReconcile(context.Background(), res.Dir, nil,
		reconcile.Options{Root: repo, ReconciledAt: time.Unix(1000, 0).UTC()})
	require.NoError(t, err)
	got := sastFindingsIn(t, res.Dir)
	require.Len(t, got, 1)
	assert.NotContains(t, got[0].Problem, guardAWSKey, "the Fixer-bound Problem must not hold the key")
	assert.NotContains(t, got[0].Evidence, guardAWSKey)
	assert.Contains(t, got[0].Problem, "hardcoded key "+payload.SecretPlaceholder)
	assert.Contains(t, got[0].Problem, "go.lang.security.unchecked-call")
}

// The reviewer block lists at most 300 bytes of each message, so block can
// pass a review whose credential sits past that cut. The sidecar holds the
// whole message, so under block the finding that holds it is never saved.
func TestPrepareReview_SASTBlockDropsAFindingHoldingASecretPastTheReviewerCut(t *testing.T) {
	repo, base, head := initRepo(t)
	hideGitleaks(t)
	long := "call result is never checked " + strings.Repeat("x", 400) + " " + guardAWSKey
	leaky := strings.Replace(sastFixture, "call result is never checked", long, 1)
	withSASTRunner(t, &fakeSASTRunner{sarif: leaky}, nil)

	cfg := sastEnabledConfig("http://unused")
	cfg.Settings.SecretGuard = "block"
	var prep *PreparedReview
	var err error
	stderr := captureStderr(t, func() {
		prep, err = PrepareReview(context.Background(), cfg, reviewReq(repo, repo, base, head))
	})
	require.NoError(t, err, "the reviewer block never quotes the key, so the review proceeds")
	assert.NoFileExists(t, filepath.Join(prep.Dir, sast.ReportFile), "the only finding held the key, so nothing is saved")
	assert.Contains(t, stderr, "secret_guard: block")
	assert.NotContains(t, stderr, guardAWSKey)
}

func TestWriteSASTSidecar_SecretGuardPolicies(t *testing.T) {
	clean := sast.Finding{File: "a.go", Line: 1, Rule: "r.clean", Message: "unchecked call"}
	leaky := sast.Finding{File: "b.go", Line: 2, Rule: "r." + guardAWSKey, Message: "key " + guardAWSKey + " is hardcoded"}
	redacted := sast.Finding{File: "b.go", Line: 2, Rule: "r." + payload.SecretPlaceholder, Message: "key " + payload.SecretPlaceholder + " is hardcoded"}

	cases := map[string]struct {
		policy string
		want   []sast.Finding // nil: no sidecar
		stderr string
		scans  int
	}{
		"off keeps the text and never scans": {policy: secretGuardOff, want: []sast.Finding{clean, leaky}},
		"warn keeps the text and warns":      {policy: secretGuardWarn, want: []sast.Finding{clean, leaky}, stderr: "saved unchanged to sast.json (secret_guard: warn)", scans: 1},
		"unset is warn":                      {policy: "", want: []sast.Finding{clean, leaky}, stderr: "secret_guard: warn", scans: 1},
		"redact replaces message and rule":   {policy: secretGuardRedact, want: []sast.Finding{clean, redacted}, stderr: "secret_guard: redact", scans: 1},
		"block drops the finding":            {policy: secretGuardBlock, want: []sast.Finding{clean}, stderr: "dropped 1 SAST finding(s)", scans: 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			calls := countingScanner(t, nil)
			dir := t.TempDir()
			stderr := captureStderr(t, func() {
				writeSASTSidecar(dir, sastScan{scanner: "fake", findings: []sast.Finding{clean, leaky}, policy: tc.policy})
			})
			rep, err := sast.ReadReport(dir)
			require.NoError(t, err)
			require.NotNil(t, rep)
			assert.Equal(t, tc.want, rep.Findings)
			total := 0
			for _, n := range calls {
				total += n
			}
			assert.Equal(t, tc.scans, total, "every finding is checked in one scan")
			if tc.stderr != "" {
				assert.Contains(t, stderr, tc.stderr)
				assert.Contains(t, stderr, "aws-access-token")
				assert.Contains(t, stderr, "b.go")
			} else {
				assert.Empty(t, stderr)
			}
			assert.NotContains(t, stderr, guardAWSKey, "a warning never prints the secret")
		})
	}
}

// A hit that runs across the line between a message and its rule is redacted
// on both sides of it.
func TestGuardSASTFindings_HitAcrossFieldsIsRedactedInBoth(t *testing.T) {
	old := scanSecrets
	t.Cleanup(func() { scanSecrets = old })
	scanSecrets = func(_ context.Context, text string, _ *slog.Logger) ([]payload.SecretHit, error) {
		i := strings.Index(text, "tail")
		return []payload.SecretHit{{Kind: "k", Start: i, End: i + len("tail\nhead")}}, nil
	}
	got := guardSASTFindings(secretGuardRedact, []sast.Finding{{File: "a.go", Line: 1, Message: "msg tail", Rule: "head.rule"}})
	require.Len(t, got, 1)
	assert.Equal(t, "msg "+payload.SecretPlaceholder, got[0].Message)
	assert.Equal(t, payload.SecretPlaceholder+".rule", got[0].Rule)
}

// A scan that cannot be trusted fails closed under block, so no sidecar is
// written (and a stale one is removed); warn and redact act on the built-in
// ruleset's hits and say the scan failed.
func TestWriteSASTSidecar_ScanFailure(t *testing.T) {
	leaky := sast.Finding{File: "b.go", Line: 2, Rule: "r", Message: "key " + guardAWSKey}
	scanErr := errors.New("gitleaks exploded")

	t.Run("block writes nothing", func(t *testing.T) {
		countingScanner(t, scanErr)
		dir := t.TempDir()
		require.NoError(t, sast.WriteReport(dir, sast.Report{Scanner: "fake", Findings: []sast.Finding{{File: "a.go", Line: 1, Rule: "r"}}}))
		stderr := captureStderr(t, func() {
			writeSASTSidecar(dir, sastScan{scanner: "fake", findings: []sast.Finding{{File: "a.go", Line: 1, Rule: "r", Message: "m"}}, policy: secretGuardBlock})
		})
		assert.NoFileExists(t, filepath.Join(dir, sast.ReportFile))
		assert.Contains(t, stderr, "gitleaks exploded")
		assert.Contains(t, stderr, "secret_guard: block")
	})
	t.Run("redact still redacts", func(t *testing.T) {
		countingScanner(t, scanErr)
		dir := t.TempDir()
		stderr := captureStderr(t, func() {
			writeSASTSidecar(dir, sastScan{scanner: "fake", findings: []sast.Finding{leaky}, policy: secretGuardRedact})
		})
		rep, err := sast.ReadReport(dir)
		require.NoError(t, err)
		require.NotNil(t, rep)
		assert.Equal(t, "key "+payload.SecretPlaceholder, rep.Findings[0].Message)
		assert.Contains(t, stderr, "gitleaks exploded")
	})
}
