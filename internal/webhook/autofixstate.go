package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/atomicfs"
)

var (
	// ErrAutoFixClaimed is returned by Claim for a Linear issue and state that
	// already started an auto-fix. Each auto-fix opens a new pull request, so
	// a replayed or repeated status change must not start another.
	ErrAutoFixClaimed = errors.New("webhook: auto-fix already started for this issue and state")
	// ErrAutoFixDailyCap is returned by Claim once the day's job cap is used.
	ErrAutoFixDailyCap = errors.New("webhook: auto-fix daily job cap reached")
	// ErrAutoFixDailyTokenCap is returned by Claim once the tokens charged to
	// the day (AddTokens) reach AutoFixState.DailyTokenCap.
	ErrAutoFixDailyTokenCap = errors.New("webhook: auto-fix daily token cap reached")
)

// autoFixStateVersion is the on-disk format version. A file with any other
// version is refused rather than guessed at.
const autoFixStateVersion = 1

// autoFixReplayHorizon is how long a claimed issue id plus state id pair is
// remembered. The seen set only has to outlast a replay of the same status
// change. Linear's retries of a failed delivery span hours, not weeks, and
// the receiver rejects a delivery whose signed timestamp is more than a minute
// off (linear.MaxTimestampSkew), so a captured delivery cannot be replayed
// later either; 30 days is far longer than both. Without a horizon the set, and the file
// rewritten on every claim, grows forever. Past it, moving an issue back to
// the same state starts a new auto-fix.
const autoFixReplayHorizon = 30 * 24 * time.Hour

// autoFixStateFile is the JSON kept on disk.
type autoFixStateFile struct {
	Version int `json:"version"`
	// Seen maps a Linear issue id to the state ids that started an auto-fix
	// for it, each with the time of the claim. Claim drops a pair once that
	// time is autoFixReplayHorizon old.
	Seen map[string]map[string]time.Time `json:"seen"`
	// Day is the UTC date (YYYY-MM-DD) Count belongs to.
	Day string `json:"day,omitempty"`
	// Count is the number of jobs claimed on Day.
	Count int `json:"count"`
	// Tokens is the token usage charged on Day. A file written before token
	// accounting has none, which reads as zero.
	Tokens int `json:"tokens,omitempty"`
}

// AutoFixState is the auto-fix job state that must survive a daemon restart:
// the Linear issue id plus state id pairs that already started a job, and the
// number of jobs started and tokens charged on the current UTC day. Every
// successful Claim and AddTokens is written to disk before it returns.
//
// One daemon owns one state file; AutoFixState is safe for concurrent use
// within that process, not across processes.
type AutoFixState struct {
	// DailyTokenCap is the token usage per UTC day at which Claim refuses
	// further jobs; zero is no cap. Set it before the first Claim.
	DailyTokenCap int

	path     string
	dailyCap int

	mu   sync.Mutex
	data autoFixStateFile
}

// OpenAutoFixState loads the state at path, or starts empty when the file
// does not exist. dailyCap is the most jobs Claim allows per UTC day and must
// be at least 1. A file that cannot be read or parsed is an error: without
// the seen set a replay could open a second fix, so the store fails closed.
func OpenAutoFixState(path string, dailyCap int) (*AutoFixState, error) {
	if path == "" {
		return nil, errors.New("webhook: auto-fix state path is empty")
	}
	if dailyCap < 1 {
		return nil, fmt.Errorf("webhook: auto-fix daily cap %d is below 1", dailyCap)
	}
	s := &AutoFixState{path: path, dailyCap: dailyCap}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.data = autoFixStateFile{Version: autoFixStateVersion}
	case err != nil:
		return nil, fmt.Errorf("webhook: read auto-fix state: %w", err)
	default:
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("webhook: parse auto-fix state %s: %w", path, err)
		}
		if s.data.Version != autoFixStateVersion {
			return nil, fmt.Errorf("webhook: auto-fix state %s has version %d, want %d", path, s.data.Version, autoFixStateVersion)
		}
	}
	if s.data.Seen == nil {
		s.data.Seen = map[string]map[string]time.Time{}
	}
	return s, nil
}

// Claim records that an auto-fix job starts for issueID moving to stateID at
// now, and counts it against now's UTC day. It returns ErrAutoFixClaimed when
// the pair was claimed before (checked first, so a repeat never reads as the
// cap), ErrAutoFixDailyCap when the day's job cap is used and
// ErrAutoFixDailyTokenCap when the tokens charged to the day have reached
// DailyTokenCap. The token cap is checked at claim time only: a job that
// starts under it is not stopped. A refused claim, or
// one whose write to disk fails, records nothing. A pair claimed
// autoFixReplayHorizon or longer before now counts as unclaimed, and a
// successful Claim prunes every such pair from the state it writes.
func (s *AutoFixState) Claim(issueID, stateID string, now time.Time) error {
	if issueID == "" || stateID == "" {
		return errors.New("webhook: auto-fix claim needs an issue id and a state id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if at, ok := s.data.Seen[issueID][stateID]; ok && !replayExpired(at, now) {
		return fmt.Errorf("%w: issue %s state %s", ErrAutoFixClaimed, issueID, stateID)
	}
	day := utcDay(now)
	count := s.countOn(day)
	if count >= s.dailyCap {
		return fmt.Errorf("%w: %d of %d on %s", ErrAutoFixDailyCap, count, s.dailyCap, day)
	}
	tokens := s.tokensOn(day)
	if s.DailyTokenCap > 0 && tokens >= s.DailyTokenCap {
		return fmt.Errorf("%w: %d of %d tokens on %s", ErrAutoFixDailyTokenCap, tokens, s.DailyTokenCap, day)
	}

	next := s.data
	next.Seen = make(map[string]map[string]time.Time, len(s.data.Seen)+1)
	for issue, claimed := range s.data.Seen {
		states := make(map[string]time.Time, len(claimed)+1)
		for state, at := range claimed {
			if !replayExpired(at, now) {
				states[state] = at
			}
		}
		if len(states) > 0 {
			next.Seen[issue] = states
		}
	}
	if next.Seen[issueID] == nil {
		next.Seen[issueID] = map[string]time.Time{}
	}
	next.Seen[issueID][stateID] = now.UTC()
	next.Day, next.Count, next.Tokens = day, count+1, tokens

	if err := writeAutoFixState(s.path, next); err != nil {
		return err
	}
	s.data = next
	return nil
}

// Count returns the number of jobs claimed on now's UTC day.
func (s *AutoFixState) Count(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.countOn(utcDay(now))
}

// AddTokens charges tokens of usage to now's UTC day and writes it to disk
// before it returns. A write that fails charges nothing. The charge is not
// tied to a claim: a job claimed before midnight and finished after it is
// charged to the day it finished.
func (s *AutoFixState) AddTokens(tokens int, now time.Time) error {
	if tokens < 0 {
		return fmt.Errorf("webhook: auto-fix token usage %d is negative", tokens)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	day := utcDay(now)
	next := s.data
	next.Day, next.Count, next.Tokens = day, s.countOn(day), addCapped(s.tokensOn(day), tokens)
	if err := writeAutoFixState(s.path, next); err != nil {
		return err
	}
	s.data = next
	return nil
}

// Tokens returns the token usage charged to now's UTC day.
func (s *AutoFixState) Tokens(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokensOn(utcDay(now))
}

func (s *AutoFixState) tokensOn(day string) int {
	if s.data.Day != day {
		return 0
	}
	return s.data.Tokens
}

// addCapped is a+b for non-negative a and b, held at math.MaxInt instead of
// wrapping.
func addCapped(a, b int) int {
	if a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}

func (s *AutoFixState) countOn(day string) int {
	if s.data.Day != day {
		return 0
	}
	return s.data.Count
}

func utcDay(t time.Time) string { return t.UTC().Format(time.DateOnly) }

// replayExpired reports whether a claim made at is past the replay horizon at
// now. A claim time after now (a clock set back) is kept.
func replayExpired(at, now time.Time) bool { return now.Sub(at) >= autoFixReplayHorizon }

// writeAutoFixState writes data to path 0600 through atomicfs with Sync set,
// so a crash leaves either the old state or the new one. Unlike the
// regenerable review artifacts, this file is not rebuilt from anywhere: losing
// a claim could open a second fix pull request.
func writeAutoFixState(path string, data autoFixStateFile) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("webhook: encode auto-fix state: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("webhook: create auto-fix state directory: %w", err)
	}
	if err := atomicfs.WriteFileAtomicWith(path, raw, atomicfs.WriteOptions{Mode: 0o600, Sync: true}); err != nil {
		return fmt.Errorf("webhook: write auto-fix state: %w", err)
	}
	return nil
}
