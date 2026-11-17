package astgroup

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
)

// TestMain shares one in-memory wazero compilation cache across every Host the
// tests build (see compilationCache). Without it each NewHost recompiles the
// parser it touches: ~10s per test under -race, which made this package the
// slowest in CI at 11 minutes.
//
// The cache is then warmed with every embedded parser, compiled concurrently.
// Each compile is single-threaded, so leaving it to the first test that needs a
// language serializes ten of them (TestBenchmark_ASTGroupingAccuracy alone
// touched them all, ~110s under -race).
func TestMain(m *testing.M) {
	compilationCache = wazero.NewCompilationCache()
	if err := warmCompilationCache(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = compilationCache.Close(context.Background())
	os.Exit(code)
}

// warmCompilationCache compiles each embedded parser through NewHost, the same
// path the tests take, so the cache entries match the runtime config they use.
func warmCompilationCache() error {
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for lang := range builtinParsers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			h := NewHost()
			defer func() { _ = h.Close() }()
			if _, err := h.Parser(lang); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("astgroup: warm compilation cache for %s: %w", lang, err)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}
