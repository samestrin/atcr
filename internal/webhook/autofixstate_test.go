package webhook

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openState(t *testing.T, path string, dailyCap int) *AutoFixState {
	t.Helper()
	s, err := OpenAutoFixState(path, dailyCap)
	if err != nil {
		t.Fatalf("OpenAutoFixState: %v", err)
	}
	return s
}

func TestAutoFixStateRefusesRepeatClaimAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "autofix.json")
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

	s := openState(t, path, 5)
	if err := s.Claim("ISS-1", "state-done", now); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	reopened := openState(t, path, 5)
	if err := reopened.Claim("ISS-1", "state-done", now.Add(48*time.Hour)); !errors.Is(err, ErrAutoFixClaimed) {
		t.Fatalf("repeat claim after reopen: got %v, want ErrAutoFixClaimed", err)
	}
	// The same issue moving to another state is a new claim.
	if err := reopened.Claim("ISS-1", "state-review", now); err != nil {
		t.Fatalf("same issue, other state: %v", err)
	}
	// Another issue in the same state is a new claim.
	if err := reopened.Claim("ISS-2", "state-done", now); err != nil {
		t.Fatalf("other issue, same state: %v", err)
	}
}

func TestAutoFixStateDailyCapResetsNextDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	day1 := time.Date(2026, 10, 27, 23, 0, 0, 0, time.UTC)
	const dailyCap = 2

	s := openState(t, path, dailyCap)
	for i, id := range []string{"ISS-1", "ISS-2"} {
		if err := s.Claim(id, "st", day1.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("claim %d: %v", i+1, err)
		}
	}
	if err := s.Claim("ISS-3", "st", day1.Add(10*time.Minute)); !errors.Is(err, ErrAutoFixDailyCap) {
		t.Fatalf("cap-plus-one claim: got %v, want ErrAutoFixDailyCap", err)
	}
	// The refused claim was not recorded: the counter survives a reopen at
	// the cap, and ISS-3 can still be claimed the next day.
	reopened := openState(t, path, dailyCap)
	if err := reopened.Claim("ISS-3", "st", day1.Add(30*time.Minute)); !errors.Is(err, ErrAutoFixDailyCap) {
		t.Fatalf("cap-plus-one claim after reopen: got %v, want ErrAutoFixDailyCap", err)
	}
	day2 := day1.Add(2 * time.Hour) // 01:00 UTC the next day
	if err := reopened.Claim("ISS-3", "st", day2); err != nil {
		t.Fatalf("cap-plus-one claim the next day: %v", err)
	}
	if got := reopened.Count(day2); got != 1 {
		t.Fatalf("next-day count = %d, want 1", got)
	}
}

func TestAutoFixStateDuplicateDoesNotSpendCap(t *testing.T) {
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	s := openState(t, filepath.Join(t.TempDir(), "autofix.json"), 1)
	if err := s.Claim("ISS-1", "st", now); err != nil {
		t.Fatal(err)
	}
	// At the cap, a repeat is reported as a repeat, not as the cap.
	if err := s.Claim("ISS-1", "st", now); !errors.Is(err, ErrAutoFixClaimed) {
		t.Fatalf("repeat at cap: got %v, want ErrAutoFixClaimed", err)
	}
	if got := s.Count(now); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
}

func TestAutoFixStateDayIsUTC(t *testing.T) {
	// Two times on different local days in UTC+2 but on one UTC day count
	// against one day's cap.
	plus2 := time.FixedZone("UTC+2", 2*60*60)
	a := time.Date(2026, 10, 27, 1, 30, 0, 0, plus2)  // 2026-10-26 23:30 UTC
	b := time.Date(2026, 10, 26, 23, 30, 0, 0, plus2) // 2026-10-26 21:30 UTC
	s := openState(t, filepath.Join(t.TempDir(), "autofix.json"), 1)
	if err := s.Claim("ISS-1", "st", b); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim("ISS-2", "st", a); !errors.Is(err, ErrAutoFixDailyCap) {
		t.Fatalf("same UTC day: got %v, want ErrAutoFixDailyCap", err)
	}
}

func TestOpenAutoFixStateRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenAutoFixState(filepath.Join(dir, "a.json"), 0); err == nil {
		t.Error("cap 0: want error")
	}
	if _, err := OpenAutoFixState("", 1); err == nil {
		t.Error("empty path: want error")
	}
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A state file that cannot be read fails closed: no store, so no job.
	if _, err := OpenAutoFixState(corrupt, 1); err == nil {
		t.Error("corrupt file: want error")
	}
	future := filepath.Join(dir, "future.json")
	if err := os.WriteFile(future, []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAutoFixState(future, 1); err == nil {
		t.Error("unknown version: want error")
	}
}

func TestAutoFixStateRejectsEmptyIDs(t *testing.T) {
	s := openState(t, filepath.Join(t.TempDir(), "autofix.json"), 3)
	now := time.Now()
	for _, c := range [][2]string{{"", "st"}, {"ISS-1", ""}} {
		if err := s.Claim(c[0], c[1], now); err == nil {
			t.Errorf("Claim(%q, %q): want error", c[0], c[1])
		}
	}
	if got := s.Count(now); got != 0 {
		t.Fatalf("count = %d, want 0", got)
	}
}

func TestAutoFixStateWriteFailureRecordsNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "autofix.json")
	s := openState(t, path, 3)
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	// Replace the state directory's file slot with a directory, so the
	// rename over it fails.
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim("ISS-1", "st", now); err == nil {
		t.Fatal("claim with unwritable state: want error")
	}
	if got := s.Count(now); got != 0 {
		t.Fatalf("count after failed write = %d, want 0", got)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	// Nothing was remembered, so the claim succeeds once the file can be written.
	if err := s.Claim("ISS-1", "st", now); err != nil {
		t.Fatalf("claim after the write recovers: %v", err)
	}
}

func TestAutoFixStateFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	s := openState(t, path, 1)
	if err := s.Claim("ISS-1", "st", time.Now()); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("state file mode = %o, want 600", perm)
	}
}

func TestAutoFixStateConcurrentClaimsOneWins(t *testing.T) {
	s := openState(t, filepath.Join(t.TempDir(), "autofix.json"), 100)
	now := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Claim("ISS-1", "st", now) == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent claims won = %d, want 1", wins)
	}
}

func TestAutoFixStateClaimPrunesPastReplayHorizon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	old := now.Add(-autoFixReplayHorizon - time.Hour)
	recent := now.Add(-24 * time.Hour)

	s := openState(t, path, 5)
	if err := s.Claim("ISS-OLD", "st", old); err != nil {
		t.Fatalf("old claim: %v", err)
	}
	if err := s.Claim("ISS-NEW", "st", recent); err != nil {
		t.Fatalf("recent claim: %v", err)
	}
	if err := s.Claim("ISS-3", "st", now); err != nil {
		t.Fatalf("claim that prunes: %v", err)
	}

	// The pruned state is what reaches disk: only the old pair is gone.
	reopened := openState(t, path, 5)
	if _, ok := reopened.data.Seen["ISS-OLD"]; ok {
		t.Errorf("claim older than %v still in the seen set", autoFixReplayHorizon)
	}
	for _, id := range []string{"ISS-NEW", "ISS-3"} {
		if _, ok := reopened.data.Seen[id]["st"]; !ok {
			t.Errorf("%s pruned, want kept", id)
		}
	}
	if err := reopened.Claim("ISS-NEW", "st", now); !errors.Is(err, ErrAutoFixClaimed) {
		t.Fatalf("repeat inside the horizon: got %v, want ErrAutoFixClaimed", err)
	}
}

func TestAutoFixStateClaimAllowedAgainPastReplayHorizon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	first := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	s := openState(t, path, 5)
	if err := s.Claim("ISS-1", "st", first); err != nil {
		t.Fatal(err)
	}
	// Just inside the horizon the pair is still remembered.
	if err := s.Claim("ISS-1", "st", first.Add(autoFixReplayHorizon-time.Minute)); !errors.Is(err, ErrAutoFixClaimed) {
		t.Fatalf("repeat inside the horizon: got %v, want ErrAutoFixClaimed", err)
	}
	// Past it, the remembered claim has expired, even before any prune ran.
	if err := s.Claim("ISS-1", "st", first.Add(autoFixReplayHorizon+time.Minute)); err != nil {
		t.Fatalf("repeat past the horizon: %v", err)
	}
}

func TestAutoFixStateDailyTokenCapRefusesNextClaim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

	s := openState(t, path, 10)
	s.SetDailyTokenCap(1000)
	if err := s.Claim("ISS-1", "st", now); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := s.AddTokens(999, now); err != nil {
		t.Fatalf("AddTokens: %v", err)
	}
	// One token under the cap still allows a claim.
	if err := s.Claim("ISS-2", "st", now); err != nil {
		t.Fatalf("claim under the token cap: %v", err)
	}
	if err := s.AddTokens(1, now); err != nil {
		t.Fatalf("AddTokens: %v", err)
	}
	if got := s.Tokens(now); got != 1000 {
		t.Fatalf("Tokens = %d, want 1000", got)
	}
	err := s.Claim("ISS-3", "st", now)
	if !errors.Is(err, ErrAutoFixDailyTokenCap) {
		t.Fatalf("claim at the token cap: got %v, want ErrAutoFixDailyTokenCap", err)
	}
	if errors.Is(err, ErrAutoFixDailyCap) {
		t.Fatalf("token cap reads as the job cap: %v", err)
	}
	// The refused claim recorded nothing, and the count stays where it was.
	if got := s.Count(now); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}
	// A repeat is still a repeat, not the token cap.
	if err := s.Claim("ISS-1", "st", now); !errors.Is(err, ErrAutoFixClaimed) {
		t.Fatalf("repeat at the token cap: got %v, want ErrAutoFixClaimed", err)
	}
	// The next UTC day starts at zero tokens.
	tomorrow := now.Add(24 * time.Hour)
	if got := s.Tokens(tomorrow); got != 0 {
		t.Fatalf("next-day Tokens = %d, want 0", got)
	}
	if err := s.Claim("ISS-3", "st", tomorrow); err != nil {
		t.Fatalf("claim the next day: %v", err)
	}
}

func TestAutoFixStateNoTokenCapNeverRefuses(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	s := openState(t, filepath.Join(t.TempDir(), "autofix.json"), 10)
	if err := s.AddTokens(1<<40, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim("ISS-1", "st", now); err != nil {
		t.Fatalf("claim with no token cap: %v", err)
	}
}

func TestAutoFixStateTokensSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

	s := openState(t, path, 10)
	if err := s.Claim("ISS-1", "st", now); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTokens(700, now); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTokens(500, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	reopened := openState(t, path, 10)
	reopened.SetDailyTokenCap(1200)
	if got := reopened.Tokens(now); got != 1200 {
		t.Fatalf("Tokens after reopen = %d, want 1200", got)
	}
	if got := reopened.Count(now); got != 1 {
		t.Fatalf("Count after reopen = %d, want 1", got)
	}
	if err := reopened.Claim("ISS-2", "st", now.Add(2*time.Hour)); !errors.Is(err, ErrAutoFixDailyTokenCap) {
		t.Fatalf("claim after reopen at the token cap: got %v, want ErrAutoFixDailyTokenCap", err)
	}
}

// TestAutoFixStateLoadsFileWithoutTokens covers a state file written before
// token accounting: it loads with zero tokens for its day.
func TestAutoFixStateLoadsFileWithoutTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	old := `{"version":1,"seen":{},"day":"2026-11-17","count":3}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	s := openState(t, path, 10)
	if got := s.Tokens(now); got != 0 {
		t.Fatalf("Tokens = %d, want 0", got)
	}
	if got := s.Count(now); got != 3 {
		t.Fatalf("Count = %d, want 3", got)
	}
}

// TestAutoFixStateTokensAcrossMidnight covers a job claimed one UTC day and
// charged the next: the spend counts against the day it is charged, and the
// new day's job count starts at zero.
func TestAutoFixStateTokensAcrossMidnight(t *testing.T) {
	day1 := time.Date(2026, 11, 17, 23, 50, 0, 0, time.UTC)
	day2 := day1.Add(20 * time.Minute)
	s := openState(t, filepath.Join(t.TempDir(), "autofix.json"), 1)
	if err := s.Claim("ISS-1", "st", day1); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTokens(50, day1); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTokens(40, day2); err != nil {
		t.Fatal(err)
	}
	if got := s.Tokens(day2); got != 40 {
		t.Fatalf("day-2 Tokens = %d, want 40", got)
	}
	if got := s.Count(day2); got != 0 {
		t.Fatalf("day-2 Count = %d, want 0", got)
	}
	if err := s.Claim("ISS-2", "st", day2); err != nil {
		t.Fatalf("day-2 claim: %v", err)
	}
	if got := s.Tokens(day2); got != 40 {
		t.Fatalf("day-2 Tokens after claim = %d, want 40", got)
	}
}

func TestAutoFixStateAddTokensRejectsNegative(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	s := openState(t, filepath.Join(t.TempDir(), "autofix.json"), 1)
	if err := s.AddTokens(-1, now); err == nil {
		t.Fatal("AddTokens(-1) succeeded")
	}
	if got := s.Tokens(now); got != 0 {
		t.Fatalf("Tokens = %d, want 0", got)
	}
}

func TestAutoFixStateAddTokensWriteFailureRecordsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	s := openState(t, path, 1)
	// A directory in the file's slot makes the rename over it fail.
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTokens(10, now); err == nil {
		t.Fatal("AddTokens with unwritable state: want error")
	}
	if got := s.Tokens(now); got != 0 {
		t.Fatalf("Tokens after failed write = %d, want 0", got)
	}
}

// TestAutoFixStateRefusedTokenClaimSurvivesReopen pins that a claim refused
// at the token cap wrote nothing: a reopen from the same path sees only the
// successful claims and charges, and the refused issue still claims the next
// day.
func TestAutoFixStateRefusedTokenClaimSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

	s := openState(t, path, 10)
	s.SetDailyTokenCap(1000)
	for _, id := range []string{"ISS-1", "ISS-2"} {
		if err := s.Claim(id, "st", now); err != nil {
			t.Fatalf("claim %s: %v", id, err)
		}
	}
	if err := s.AddTokens(1000, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim("ISS-3", "st", now); !errors.Is(err, ErrAutoFixDailyTokenCap) {
		t.Fatalf("claim at the token cap: got %v, want ErrAutoFixDailyTokenCap", err)
	}

	// The refusal is not on disk: the reopened count and tokens are those of
	// the two successful claims and the charge, not of the refusal.
	reopened := openState(t, path, 10)
	reopened.SetDailyTokenCap(1000)
	if got := reopened.Count(now); got != 2 {
		t.Fatalf("Count after reopen = %d, want 2", got)
	}
	if got := reopened.Tokens(now); got != 1000 {
		t.Fatalf("Tokens after reopen = %d, want 1000", got)
	}
	if _, ok := reopened.data.Seen["ISS-3"]; ok {
		t.Fatal("refused claim recorded in the seen set")
	}
	if err := reopened.Claim("ISS-3", "st", now); !errors.Is(err, ErrAutoFixDailyTokenCap) {
		t.Fatalf("claim at the token cap after reopen: got %v, want ErrAutoFixDailyTokenCap", err)
	}
	// ISS-3 was never claimed, so it still claims the next day.
	day2 := now.Add(24 * time.Hour)
	if err := reopened.Claim("ISS-3", "st", day2); err != nil {
		t.Fatalf("ISS-3 the next day: %v", err)
	}
	if got := reopened.Count(day2); got != 1 {
		t.Fatalf("next-day Count = %d, want 1", got)
	}
}

// TestAutoFixStateTokenCapIsConfigurationNotState pins that DailyTokenCap is
// configuration, not persisted state: the file keeps the usage, and the cap
// is enforced after a restart only because the owner sets it again from
// config after open (cli/webhook.go:196).
func TestAutoFixStateTokenCapIsConfigurationNotState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

	s := openState(t, path, 10)
	s.SetDailyTokenCap(1000)
	if err := s.Claim("ISS-1", "st", now); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTokens(1000, now); err != nil {
		t.Fatal(err)
	}

	// Reopen without setting DailyTokenCap by hand: the usage survives, the
	// cap does not.
	reopened := openState(t, path, 10)
	if reopened.DailyTokenCap() != 0 {
		t.Fatalf("DailyTokenCap after reopen = %d, want 0 (configuration, not state)", reopened.DailyTokenCap())
	}
	if got := reopened.Tokens(now); got != 1000 {
		t.Fatalf("Tokens after reopen = %d, want 1000", got)
	}
	// With no cap configured, the persisted usage refuses nothing.
	if err := reopened.Claim("ISS-2", "st", now); err != nil {
		t.Fatalf("claim with no cap configured: %v", err)
	}
	// Once the owner configures the cap again, the persisted usage enforces
	// it. This is how the cap survives a restart.
	reopened.SetDailyTokenCap(1000)
	if err := reopened.Claim("ISS-3", "st", now); !errors.Is(err, ErrAutoFixDailyTokenCap) {
		t.Fatalf("claim after the cap is reconfigured: got %v, want ErrAutoFixDailyTokenCap", err)
	}
}

// TestAutoFixStateClaimStartsNewTokenDay pins the rollover where the first
// call of the new day is a Claim, with no AddTokens before it: the new day
// starts at zero tokens and the claim succeeds even though the previous day
// ended at the cap.
func TestAutoFixStateClaimStartsNewTokenDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	day1 := time.Date(2026, 11, 17, 23, 50, 0, 0, time.UTC)
	day2 := day1.Add(20 * time.Minute) // 00:10 UTC the next day

	s := openState(t, path, 5)
	s.SetDailyTokenCap(50)
	if err := s.Claim("ISS-1", "st", day1); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTokens(50, day1); err != nil {
		t.Fatal(err)
	}
	// Day 1 is at the token cap. The first call of day 2 is this Claim.
	if err := s.Claim("ISS-2", "st", day2); err != nil {
		t.Fatalf("first claim of day 2: %v", err)
	}
	if got := s.Tokens(day2); got != 0 {
		t.Fatalf("day-2 Tokens = %d, want 0", got)
	}
	if got := s.Count(day2); got != 1 {
		t.Fatalf("day-2 Count = %d, want 1", got)
	}
	// The rollover is what reaches disk.
	reopened := openState(t, path, 5)
	if got := reopened.Tokens(day2); got != 0 {
		t.Fatalf("day-2 Tokens after reopen = %d, want 0", got)
	}
	if got := reopened.Count(day2); got != 1 {
		t.Fatalf("day-2 Count after reopen = %d, want 1", got)
	}
}

// TestAutoFixStateConcurrentAddTokensAndClaim races AddTokens and Claim on
// one handle (run under -race): the final token total is exactly the sum of
// the successful charges, and the count is the number of successful claims.
func TestAutoFixStateConcurrentAddTokensAndClaim(t *testing.T) {
	s := openState(t, filepath.Join(t.TempDir(), "autofix.json"), 100)
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	var mu sync.Mutex
	charged, wins := 0, 0
	for i := 1; i <= 30; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := s.AddTokens(n, now); err == nil {
				mu.Lock()
				charged += n
				mu.Unlock()
			}
		}(i)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := s.Claim(fmt.Sprintf("ISS-%d", n), "st", now); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if got := s.Tokens(now); got != charged {
		t.Fatalf("Tokens after concurrent use = %d, want %d (sum of successful charges)", got, charged)
	}
	if got := s.Count(now); got != wins {
		t.Fatalf("Count after concurrent use = %d, want %d (successful claims)", got, wins)
	}
}

// TestAutoFixStateConcurrentSetDailyTokenCapAndClaim races SetDailyTokenCap
// with Claim and DailyTokenCap on one handle (run under -race): the cap is
// read and written under the state's mutex, so the race detector stays quiet,
// and the last cap set is the one in force.
func TestAutoFixStateConcurrentSetDailyTokenCapAndClaim(t *testing.T) {
	s := openState(t, filepath.Join(t.TempDir(), "autofix.json"), 100)
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func(n int) {
			defer wg.Done()
			s.SetDailyTokenCap(1000 + n)
		}(i)
		go func(n int) {
			defer wg.Done()
			if err := s.Claim(fmt.Sprintf("ISS-%d", n), "st", now); err != nil {
				t.Errorf("claim ISS-%d under the token cap: %v", n, err)
			}
		}(i)
		go func() {
			defer wg.Done()
			_ = s.DailyTokenCap()
		}()
	}
	wg.Wait()
	s.SetDailyTokenCap(10)
	if got := s.DailyTokenCap(); got != 10 {
		t.Fatalf("DailyTokenCap = %d, want 10", got)
	}
	if err := s.AddTokens(10, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Claim("ISS-cap", "st", now); !errors.Is(err, ErrAutoFixDailyTokenCap) {
		t.Fatalf("claim at the token cap: got %v, want ErrAutoFixDailyTokenCap", err)
	}
}

// TestAutoFixStateZeroTokenCapAcrossRollover pins that a zero DailyTokenCap
// never refuses a claim, on either side of a UTC rollover and whatever usage
// is charged. A negative cap cannot arrive from config
// (internal/registry/config.go:2091 rejects it); set directly, it reads as no
// cap, not as a cap already exceeded.
func TestAutoFixStateZeroTokenCapAcrossRollover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	day1 := time.Date(2026, 11, 17, 22, 0, 0, 0, time.UTC)
	s := openState(t, path, 10)
	s.SetDailyTokenCap(0) // zero is no cap
	if err := s.AddTokens(1<<40, day1); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"ISS-1", "ISS-2", "ISS-3"} {
		if err := s.Claim(id, "st", day1.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("day-1 claim %s: %v", id, err)
		}
	}
	day2 := day1.Add(3 * time.Hour) // 01:00 UTC the next day
	if err := s.AddTokens(1<<40, day2); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"ISS-4", "ISS-5", "ISS-6"} {
		if err := s.Claim(id, "st", day2.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("day-2 claim %s: %v", id, err)
		}
	}
	s.SetDailyTokenCap(-1)
	if err := s.Claim("ISS-7", "st", day2.Add(time.Hour)); err != nil {
		t.Fatalf("claim with a negative cap: %v", err)
	}
}

// TestAutoFixStateAddTokensSaturates pins that a charge past math.MaxInt
// still records the day as saturated, in memory and on disk, and says so with
// ErrAutoFixTokenOverflow.
func TestAutoFixStateAddTokensSaturates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	s := openState(t, path, 1)
	if err := s.AddTokens(math.MaxInt-1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTokens(5, now); !errors.Is(err, ErrAutoFixTokenOverflow) {
		t.Fatalf("AddTokens past math.MaxInt = %v, want ErrAutoFixTokenOverflow", err)
	}
	if got := s.Tokens(now); got != math.MaxInt {
		t.Fatalf("Tokens = %d, want math.MaxInt", got)
	}
	if got := openState(t, path, 1).Tokens(now); got != math.MaxInt {
		t.Fatalf("Tokens after reopen = %d, want math.MaxInt", got)
	}
}

// TestAutoFixStateAddTokensExactMaxInt pins that a total of exactly
// math.MaxInt is not an overflow.
func TestAutoFixStateAddTokensExactMaxInt(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	s := openState(t, filepath.Join(t.TempDir(), "autofix.json"), 1)
	if err := s.AddTokens(math.MaxInt-1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTokens(1, now); err != nil {
		t.Fatalf("AddTokens to exactly math.MaxInt: %v", err)
	}
	if got := s.Tokens(now); got != math.MaxInt {
		t.Fatalf("Tokens = %d, want math.MaxInt", got)
	}
}

// TestAutoFixStateAddTokensOverflowWriteFailureRecordsNothing pins that an
// overflowing charge whose write fails returns the write error, not the
// overflow, and leaves the day's total as it was.
func TestAutoFixStateAddTokensOverflowWriteFailureRecordsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "autofix.json")
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	s := openState(t, path, 1)
	if err := s.AddTokens(math.MaxInt-1, now); err != nil {
		t.Fatal(err)
	}
	// A directory in the file's slot makes the rename over it fail.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := s.AddTokens(5, now)
	if err == nil {
		t.Fatal("AddTokens with unwritable state: want error")
	}
	if errors.Is(err, ErrAutoFixTokenOverflow) {
		t.Fatalf("AddTokens with unwritable state = %v, want the write error without ErrAutoFixTokenOverflow", err)
	}
	if got := s.Tokens(now); got != math.MaxInt-1 {
		t.Fatalf("Tokens after failed write = %d, want math.MaxInt-1", got)
	}
}
