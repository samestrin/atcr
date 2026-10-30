package webhook

import (
	"errors"
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
