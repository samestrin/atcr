package sandbox

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the SHAPE of the opt-in loopback grant (RunSpec.LoopbackPorts)
// and the host-side refusal that goes with it. As with oslevel_profile_test.go,
// shape is not enforcement: the kernel's answer is asserted only by
// TestIntegration_OSLevelDarwin_LoopbackGrantIsPortAndAddressScoped.

func TestRunSpecValidate_LoopbackPorts(t *testing.T) {
	base := RunSpec{Command: []string{"true"}, SnapshotDir: "/snap"}
	for _, tc := range []struct {
		name  string
		ports []int
		ok    bool
	}{
		{name: "none", ports: nil, ok: true},
		{name: "lowest", ports: []int{1}, ok: true},
		{name: "dev server", ports: []int{3000, 5173}, ok: true},
		{name: "highest", ports: []int{65535}, ok: true},
		// 0 asks the kernel for an ephemeral port, which no profile rule can name
		// in advance; a grant on it would have to be a wildcard.
		{name: "zero", ports: []int{0}, ok: false},
		{name: "negative", ports: []int{-1}, ok: false},
		{name: "too high", ports: []int{65536}, ok: false},
		{name: "duplicate", ports: []int{3000, 3000}, ok: false},
		{name: "too many", ports: []int{1, 2, 3, 4, 5, 6, 7, 8, 9}, ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := base
			spec.LoopbackPorts = tc.ports
			err := spec.validate()
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// networkAllowLines returns every allow rule in profile that names a network
// operation.
func networkAllowLines(profile string) []string {
	var out []string
	for _, line := range strings.Split(profile, "\n") {
		if strings.HasPrefix(line, "(allow network") {
			out = append(out, line)
		}
	}
	return out
}

func TestSandboxExecProfile_LoopbackPortsAreTCPAndPortScoped(t *testing.T) {
	cfg, spec := profileFixture(t)
	spec.LoopbackPorts = []int{3000, 5173}

	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)

	// Exactly these rules and nothing else: bind and accept on the loopback
	// port, connect to the loopback port. No wildcard port, no UDP, no unix
	// socket, no off-host address.
	want := []string{
		`(allow network-bind (local tcp "localhost:3000"))`,
		`(allow network-inbound (local tcp "localhost:3000"))`,
		`(allow network-outbound (remote tcp "localhost:3000"))`,
		`(allow network-bind (local tcp "localhost:5173"))`,
		`(allow network-inbound (local tcp "localhost:5173"))`,
		`(allow network-outbound (remote tcp "localhost:5173"))`,
	}
	assert.Equal(t, want, networkAllowLines(profile))
	for _, line := range networkAllowLines(profile) {
		assert.NotContains(t, line, "*", "a loopback grant must name its port, never a wildcard")
	}

	// The deny stays, and every allow comes after it: sandbox-exec applies the
	// last matching rule, so an allow above the deny would be dead.
	denyIdx := strings.Index(profile, profileDenyNetwork)
	require.GreaterOrEqual(t, denyIdx, 0, "the explicit network deny must survive a loopback grant")
	for _, rule := range want {
		assert.Greater(t, strings.Index(profile, rule), denyIdx, "%s must follow %s", rule, profileDenyNetwork)
	}
}

func TestSandboxExecProfile_NoLoopbackPortsMeansNoNetworkAllow(t *testing.T) {
	cfg, spec := profileFixture(t)
	spec.LoopbackPorts = []int{}
	profile, err := sandboxExecProfile(cfg, spec)
	require.NoError(t, err)
	assert.Empty(t, networkAllowLines(profile), "an empty port list must keep the profile deny-all")
}

func TestBwrapArgs_LoopbackPortsChangeNothing(t *testing.T) {
	// bwrap's --unshare-net already gives the run its own loopback, so the
	// dev server and the browser can reach each other there and no host
	// service can be reached at all. The field must not change the argv.
	cfg, spec := bwrapFixture(t)
	without, err := bwrapArgs(cfg, spec)
	require.NoError(t, err)
	spec.LoopbackPorts = []int{3000}
	with, err := bwrapArgs(cfg, spec)
	require.NoError(t, err)
	assert.Equal(t, without, with)
}

// heldLoopbackPort returns a port a host process is listening on at 127.0.0.1.
func heldLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// freeLoopbackPort returns a port nothing was listening on a moment ago.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

func TestOSLevelBackendRun_LoopbackPortHeldByTheHost(t *testing.T) {
	// On darwin the grant is a hole in the HOST's loopback: if a host service
	// already listens on the port, the workload's connect reaches that service
	// instead of its own dev server. The run is refused before anything spawns.
	// On linux the run has a private loopback, so a host listener is unreachable
	// and the run goes ahead.
	held := heldLoopbackPort(t)
	free := freeLoopbackPort(t)
	for _, tc := range []struct {
		name    string
		goos    string
		port    int
		refused bool
	}{
		{name: "darwin held", goos: "darwin", port: held, refused: true},
		{name: "darwin free", goos: "darwin", port: free, refused: false},
		{name: "linux held", goos: "linux", port: held, refused: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "spawned")
			cfg := DefaultOSLevelConfig()
			cfg.ToolPath = writeFakeOSLevel(t, "touch '"+marker+"'\nexit 0")
			b := NewOSLevelBackend(cfg)
			b.goos = tc.goos
			var seen OSLevelConfig
			captureContainmentCfg(t, &seen)

			_, err := b.Run(context.Background(), RunSpec{
				Command:       []string{"true"},
				SnapshotDir:   t.TempDir(),
				LoopbackPorts: []int{tc.port},
			})
			_, statErr := os.Stat(marker)
			if tc.refused {
				require.Error(t, err)
				assert.True(t, errors.Is(err, ErrOSLevelLoopbackPortHeld), "got %v", err)
				assert.Contains(t, err.Error(), strconv.Itoa(tc.port), "the refusal must name the port")
				assert.True(t, os.IsNotExist(statErr), "nothing may spawn once the port is found held")
				return
			}
			require.NoError(t, err)
			assert.NoError(t, statErr, "the run must go ahead")
		})
	}
}

// localNonLoopbackIPv4 returns an address of this host that is not loopback,
// or "" when it has none.
func localNonLoopbackIPv4(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	require.NoError(t, err)
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
			return ipn.IP.String()
		}
	}
	return ""
}

func TestCheckLoopbackPortsFree_CoversTheHostsOwnAddresses(t *testing.T) {
	// Measured on the macOS CI runner (PR #753, first run): under a grant on
	// port P, the workload connected to a host listener on the runner's own
	// LAN address at port P. sandbox-exec's "localhost" covers traffic macOS
	// routes over lo0, which includes the host's own interface addresses. So a
	// host service bound only to a LAN address is reachable through the grant,
	// and the held-port check has to look there too, not just at 127.0.0.1.
	ip := localNonLoopbackIPv4(t)
	if ip == "" {
		t.Skip("this host has no non-loopback IPv4 address")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	err = checkLoopbackPortsFree(context.Background(), []int{port})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOSLevelLoopbackPortHeld), "got %v", err)
	assert.Contains(t, err.Error(), ip, "the refusal must name the address that answered")
}

func TestCheckLoopbackPortsFree_StopsWhenTheContextEnds(t *testing.T) {
	// Each probe is bounded at loopbackDialTimeout, but they run one after
	// another, so under a firewall that drops rather than refuses, ports x hosts
	// probes could hold up the run for many seconds. The check takes the run's
	// context and stops at its deadline. It then fails closed: a check that did
	// not finish cannot say the ports are free.
	origIfaces, origDial := netInterfaces, loopbackDial
	t.Cleanup(func() { netInterfaces, loopbackDial = origIfaces, origDial })
	netInterfaces = func() ([]net.Interface, error) { return nil, nil } // 127.0.0.1 and ::1 only
	var dials int
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials++
		// A dropped SYN: no answer until the context or the per-probe bound
		// gives up.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(loopbackDialTimeout):
			return nil, errors.New("i/o timeout")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := checkLoopbackPortsFree(ctx, []int{3000, 5173})
	require.Error(t, err, "a check cut short must not report the ports free")
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "got %v", err)
	assert.True(t, errors.Is(err, ErrOSLevelLoopbackCheckUnavailable), "got %v", err)
	assert.False(t, errors.Is(err, ErrOSLevelLoopbackPortHeld), "no port was found held: %v", err)
	assert.Equal(t, 1, dials, "no probe may start once the context has ended")
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestCheckLoopbackPortsFree_CancelledContextFailsClosed(t *testing.T) {
	// A dial on a cancelled context fails at once, the same as a refused one.
	// Read as "refused", every port would look free.
	free := freeLoopbackPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := checkLoopbackPortsFree(ctx, []int{free})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "got %v", err)
	assert.True(t, errors.Is(err, ErrOSLevelLoopbackCheckUnavailable), "got %v", err)
}

func TestCheckLoopbackPortsFree_AddressListFailureHasItsOwnSentinel(t *testing.T) {
	// Without the host's address list the check cannot cover what the grant
	// reaches, so it refuses. That refusal is not "a port is held": it gets its
	// own sentinel so the message says what went wrong.
	boom := errors.New("boom")
	for _, tc := range []struct {
		name   string
		ifaces func() ([]net.Interface, error)
		addrs  func(net.Interface) ([]net.Addr, error)
		want   string
	}{
		{
			name:   "interfaces",
			ifaces: func() ([]net.Interface, error) { return nil, boom },
			addrs:  func(net.Interface) ([]net.Addr, error) { return nil, nil },
		},
		{
			name:   "addresses of one interface",
			ifaces: func() ([]net.Interface, error) { return []net.Interface{{Name: "en7"}}, nil },
			addrs:  func(net.Interface) ([]net.Addr, error) { return nil, boom },
			want:   "en7",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origIfaces, origAddrs := netInterfaces, interfaceAddrs
			t.Cleanup(func() { netInterfaces, interfaceAddrs = origIfaces, origAddrs })
			netInterfaces, interfaceAddrs = tc.ifaces, tc.addrs

			err := checkLoopbackPortsFree(context.Background(), []int{3000})
			require.Error(t, err, "the check must fail closed")
			assert.True(t, errors.Is(err, ErrOSLevelLoopbackCheckUnavailable), "got %v", err)
			assert.False(t, errors.Is(err, ErrOSLevelLoopbackPortHeld), "no port was found held: %v", err)
			assert.ErrorIs(t, err, boom)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestCheckLoopbackPortsFree_DocumentsTheCheckThenBindRace(t *testing.T) {
	// The check is a probe, not a reservation: sandbox-exec cannot hand the
	// workload a socket the host bound first, so a host process can still take
	// the port after the check. The function's own doc comment must say so,
	// where a caller reading it will see it.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "oslevel.go", nil, parser.ParseComments)
	require.NoError(t, err)
	var doc string
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "checkLoopbackPortsFree" {
			doc = fn.Doc.Text()
		}
	}
	require.NotEmpty(t, doc, "checkLoopbackPortsFree has no doc comment")
	for _, want := range []string{"not a reservation", "after the check"} {
		assert.Contains(t, doc, want)
	}
}
