package fanout

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeBrowserBackend is a test-only BrowserBackend; registering it must not
// need any change to the engine.
type fakeBrowserBackend struct {
	name   string
	result BrowserResult
}

func (f *fakeBrowserBackend) Name() string                      { return f.name }
func (f *fakeBrowserBackend) Preflight(ctx context.Context) error { return nil }
func (f *fakeBrowserBackend) Run(ctx context.Context, run BrowserRun) (BrowserResult, error) {
	return f.result, nil
}

// registerTestBrowserBackend registers b and removes it when the test ends, so
// the package-level registry is left as it was found.
func registerTestBrowserBackend(t *testing.T, b BrowserBackend) {
	t.Helper()
	if err := RegisterBrowserBackend(b); err != nil {
		t.Fatalf("RegisterBrowserBackend(%q): %v", b.Name(), err)
	}
	t.Cleanup(func() { unregisterBrowserBackend(b.Name()) })
}

func TestResolveBrowserBackend_RegisteredFakeResolvesByName(t *testing.T) {
	want := &fakeBrowserBackend{
		name: "fake-browser",
		result: BrowserResult{
			Screenshots:     []BrowserScreenshot{{Name: "home", Path: "shots/home.png", Data: []byte("png")}},
			Console:         []ConsoleEntry{{Level: "error", Text: "boom"}},
			NetworkFailures: []NetworkFailure{{URL: "http://127.0.0.1:3000/api", Method: "GET", Status: 500}},
			ExitCode:        1,
		},
	}
	registerTestBrowserBackend(t, want)

	got, err := ResolveBrowserBackend("fake-browser")
	if err != nil {
		t.Fatalf("ResolveBrowserBackend: %v", err)
	}
	if got != want {
		t.Fatalf("ResolveBrowserBackend returned %v, want the registered fake", got)
	}
	res, err := got.Run(context.Background(), BrowserRun{Spec: "e2e/home.spec.ts"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Screenshots) != 1 || len(res.Console) != 1 || len(res.NetworkFailures) != 1 || res.ExitCode != 1 {
		t.Fatalf("Run result = %+v, want the fake's result", res)
	}
}

func TestResolveBrowserBackend_UnknownNameListsRegisteredNames(t *testing.T) {
	registerTestBrowserBackend(t, &fakeBrowserBackend{name: "zeta-browser"})
	registerTestBrowserBackend(t, &fakeBrowserBackend{name: "alpha-browser"})

	b, err := ResolveBrowserBackend("nope")
	if err == nil {
		t.Fatalf("ResolveBrowserBackend(unknown) = %v, want error", b)
	}
	if !errors.Is(err, ErrUnknownBrowserBackend) {
		t.Errorf("error %v does not wrap ErrUnknownBrowserBackend", err)
	}
	msg := err.Error()
	for _, want := range []string{`"nope"`, "alpha-browser", "zeta-browser"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %s", msg, want)
		}
	}
	// Names are listed sorted so the message is stable.
	if strings.Index(msg, "alpha-browser") > strings.Index(msg, "zeta-browser") {
		t.Errorf("error %q lists names unsorted", msg)
	}
}

func TestResolveBrowserBackend_EmptyRegistrySaysNone(t *testing.T) {
	saved := snapshotBrowserBackends()
	t.Cleanup(func() { restoreBrowserBackends(saved) })
	restoreBrowserBackends(nil)

	_, err := ResolveBrowserBackend("playwright")
	if !errors.Is(err, ErrUnknownBrowserBackend) {
		t.Fatalf("err = %v, want ErrUnknownBrowserBackend", err)
	}
	if !strings.Contains(err.Error(), "registered: none") {
		t.Errorf("error %q does not say no backends are registered", err)
	}
}

func TestRegisterBrowserBackend_RejectsNilEmptyAndDuplicate(t *testing.T) {
	registerTestBrowserBackend(t, &fakeBrowserBackend{name: "dup-browser"})

	if err := RegisterBrowserBackend(nil); err == nil {
		t.Error("RegisterBrowserBackend(nil) succeeded, want error")
	}
	if err := RegisterBrowserBackend(&fakeBrowserBackend{name: ""}); err == nil {
		t.Error("RegisterBrowserBackend(empty name) succeeded, want error")
	}
	if err := RegisterBrowserBackend(&fakeBrowserBackend{name: "dup-browser"}); err == nil {
		t.Error("RegisterBrowserBackend(duplicate) succeeded, want error")
	}
}

func TestBrowserBackendNames_Sorted(t *testing.T) {
	saved := snapshotBrowserBackends()
	t.Cleanup(func() { restoreBrowserBackends(saved) })
	restoreBrowserBackends(nil)
	registerTestBrowserBackend(t, &fakeBrowserBackend{name: "b"})
	registerTestBrowserBackend(t, &fakeBrowserBackend{name: "a"})

	got := BrowserBackendNames()
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("BrowserBackendNames() = %v, want [a b]", got)
	}
}
