package sandbox

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/log"
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

func TestCheckLoopbackPortsFree_DialTimeoutFailsClosed(t *testing.T) {
	// A loopback connect is answered or refused at once, so a probe that hits
	// loopbackDialTimeout while the run's context is still live means something
	// dropped the SYN: a host firewall, not an empty port. Read as "refused",
	// the check would report the port free when it never learned whether a
	// host service listens there.
	origIfaces, origDial := netInterfaces, loopbackDial
	t.Cleanup(func() { netInterfaces, loopbackDial = origIfaces, origDial })
	netInterfaces = func() ([]net.Interface, error) { return nil, nil } // 127.0.0.1 and ::1 only
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		// What net.Dialer returns when its own Timeout fires.
		return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
	}

	err := checkLoopbackPortsFree(context.Background(), []int{3000})
	require.Error(t, err, "a timed-out probe must not report the port free")
	assert.True(t, errors.Is(err, ErrOSLevelLoopbackCheckUnavailable), "got %v", err)
	assert.False(t, errors.Is(err, ErrOSLevelLoopbackPortHeld), "no port was found held: %v", err)
	assert.ErrorIs(t, err, os.ErrDeadlineExceeded)
	assert.Contains(t, err.Error(), "port 3000 on 127.0.0.1", "the refusal must name the probe that timed out")
}

func TestCheckLoopbackPortsFree_InterfaceAddressTimeoutIsNotLocal(t *testing.T) {
	// Measured on the macOS CI runner (PR #849, first run): every probe to the
	// IPv6 link-local address of utun0, a tunnel interface, timed out, on every
	// port. A scoped link-local dial leaves through its interface rather than
	// lo0, so nothing on the host answers it, and the loopback grant does not
	// reach it either. Only a timeout at 127.0.0.1 or ::1 means a firewall
	// dropped loopback traffic; failing closed on this one would refuse every
	// darwin run with loopback ports on a host with a VPN or tunnel interface.
	origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
	t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
	netInterfaces = func() ([]net.Interface, error) { return []net.Interface{{Name: "utun0"}}, nil }
	interfaceAddrs = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::e493:3041:b67b:a5b2"), Mask: net.CIDRMask(64, 128)}}, nil
	}
	var timedOut []string
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.Contains(addr, "%utun0") {
			timedOut = append(timedOut, addr)
			return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}

	require.NoError(t, checkLoopbackPortsFree(context.Background(), []int{49178}))
	assert.Equal(t, []string{"[fe80::e493:3041:b67b:a5b2%utun0]:49178"}, timedOut, "the interface address is still probed")
}

func TestCheckLoopbackPortsFree_TunnelInterfaceTimeoutIsNotLocal(t *testing.T) {
	// Seen on the owner's Mac (EST-8465): every probe to the Tailscale address
	// on a utun interface, fd7a:115c:a1e0::/48, timed out, so darwin runs with
	// loopback ports were refused. A point-to-point (tunnel) interface's own
	// address is reached through the tunnel, not lo0, the same as the utun0
	// link-local case: nothing on the host answers there and the loopback grant
	// does not reach it. Each such address is probed once per call.
	origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
	t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Name: "utun4", Flags: net.FlagUp | net.FlagPointToPoint}}, nil
	}
	interfaceAddrs = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("100.101.102.103"), Mask: net.CIDRMask(32, 32)},
			&net.IPNet{IP: net.ParseIP("fd7a:115c:a1e0::1234"), Mask: net.CIDRMask(128, 128)},
		}, nil
	}
	var mu sync.Mutex
	var timedOut []string
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.Contains(addr, "100.101.102.103") || strings.Contains(addr, "fd7a:115c") {
			mu.Lock()
			timedOut = append(timedOut, addr)
			mu.Unlock()
			return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}

	require.NoError(t, checkLoopbackPortsFree(context.Background(), []int{3000, 3001}))
	// Interface addresses are probed concurrently, so the dial order between
	// them is not defined; which addresses were dialed, and how often, is.
	assert.ElementsMatch(t, []string{"100.101.102.103:3000", "[fd7a:115c:a1e0::1234]:3000"}, timedOut,
		"each tunnel address is probed, and once it has timed out not again")
}

// timeoutOnlyAt returns a loopbackDial that times out at want and refuses
// everywhere else.
func timeoutOnlyAt(want string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, _, err := net.SplitHostPort(addr); err == nil && host == want {
			return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}
}

func TestCheckLoopbackPortsFree_PointToPointOutsideTunnelRangesFailsClosed(t *testing.T) {
	// The point-to-point exemption covers what was measured (EST-8465):
	// Tailscale's addresses, in 100.64.0.0/10 and fd7a:115c:a1e0::/48, on a utun
	// interface. Any other point-to-point address (an L2TP or PPP link, a tun
	// device a CI harness or container runtime brings up) may be locally
	// routed, so a timeout there still refuses the run. So does a
	// tunnel-range address on an interface that is not point-to-point, such
	// as an ISP's CGNAT address on en0.
	for _, tc := range []struct {
		name  string
		ifc   net.Interface
		ip    string
		bits  int
		class string
	}{
		{"ppp0 IPv4", net.Interface{Name: "ppp0", Flags: net.FlagUp | net.FlagPointToPoint}, "203.0.113.7", 32, "point-to-point address of ppp0"},
		{"ppp0 IPv6", net.Interface{Name: "ppp0", Flags: net.FlagUp | net.FlagPointToPoint}, "2001:db8::7", 128, "point-to-point address of ppp0"},
		{"utun just below 100.64.0.0/10", net.Interface{Name: "utun4", Flags: net.FlagUp | net.FlagPointToPoint}, "100.63.255.255", 32, "point-to-point address of utun4"},
		{"utun just above 100.64.0.0/10", net.Interface{Name: "utun4", Flags: net.FlagUp | net.FlagPointToPoint}, "100.128.0.0", 32, "point-to-point address of utun4"},
		{"utun just outside fd7a:115c:a1e0::/48", net.Interface{Name: "utun4", Flags: net.FlagUp | net.FlagPointToPoint}, "fd7a:115c:a1df:ffff::1", 128, "point-to-point address of utun4"},
		{"utun in fd7a:115c::/48 but not Tailscale's /48", net.Interface{Name: "utun4", Flags: net.FlagUp | net.FlagPointToPoint}, "fd7a:115c::1", 128, "point-to-point address of utun4"},
		{"CGNAT address on broadcast en0", net.Interface{Name: "en0", Flags: net.FlagUp | net.FlagBroadcast}, "100.101.102.103", 10, "address of en0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ip, size := net.ParseIP(tc.ip), 128
			if v4 := ip.To4(); v4 != nil {
				ip, size = v4, 32
			}
			addr := &net.IPNet{IP: ip, Mask: net.CIDRMask(tc.bits, size)}
			require.True(t, addr.Contains(addr.IP), "fixture %v: mask length must match the address family", addr)
			origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
			t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
			netInterfaces = func() ([]net.Interface, error) { return []net.Interface{tc.ifc}, nil }
			interfaceAddrs = func(net.Interface) ([]net.Addr, error) { return []net.Addr{addr}, nil }
			loopbackDial = timeoutOnlyAt(tc.ip)

			err := checkLoopbackPortsFree(context.Background(), []int{3000})
			require.Error(t, err, "a timeout at a point-to-point address outside the tunnel ranges must not report the port free")
			assert.True(t, errors.Is(err, ErrOSLevelLoopbackCheckUnavailable), "got %v", err)
			assert.False(t, errors.Is(err, ErrOSLevelLoopbackPortHeld), "no port was found held: %v", err)
			assert.ErrorIs(t, err, os.ErrDeadlineExceeded)
			assert.Contains(t, err.Error(), "port 3000 on "+tc.ip, "the refusal must name the probe that timed out")
			assert.Contains(t, err.Error(), tc.class, "the refusal must name the address class")
		})
	}
}

func TestCheckLoopbackPortsFree_IPv4LinkLocalTimeoutFailsClosed(t *testing.T) {
	// A deliberate decision (epic 160, Q5), not an accident of the zone test:
	// 169.254/16 is IPv4 link-local, which macOS self-assigns when DHCP fails
	// or a bridge comes up with no peer. Unlike an IPv6 link-local address it
	// carries no zone, the dial is not scoped to an interface, and the host
	// routes it to itself over lo0 like a LAN address. A timeout there means
	// a firewall dropped the SYN, so the run is refused, and the refusal says
	// the address is IPv4 link-local so an operator can tell which class it was.
	ip := "169.254.10.20"
	addr := &net.IPNet{IP: net.ParseIP(ip).To4(), Mask: net.CIDRMask(16, 32)}
	require.True(t, addr.Contains(addr.IP), "fixture %v: mask length must match the address family", addr)
	origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
	t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
	netInterfaces = func() ([]net.Interface, error) { return []net.Interface{{Name: "en0"}}, nil }
	interfaceAddrs = func(net.Interface) ([]net.Addr, error) { return []net.Addr{addr}, nil }
	loopbackDial = timeoutOnlyAt(ip)

	err := checkLoopbackPortsFree(context.Background(), []int{3000})
	require.Error(t, err, "a timeout at an IPv4 link-local address must not report the port free")
	assert.True(t, errors.Is(err, ErrOSLevelLoopbackCheckUnavailable), "got %v", err)
	assert.False(t, errors.Is(err, ErrOSLevelLoopbackPortHeld), "no port was found held: %v", err)
	assert.ErrorIs(t, err, os.ErrDeadlineExceeded)
	assert.Contains(t, err.Error(), "port 3000 on "+ip, "the refusal must name the probe that timed out")
	assert.Contains(t, err.Error(), "IPv4 link-local address of en0", "the refusal must name the address class")
}

// deadWave is a fake clock for dead interface addresses. A dial at a dead
// address blocks until want such dials are in flight at once; that wave then
// costs one loopbackDialTimeout of virtual time and every dial in it times
// out. Serial probing never fills a wave of more than one, so its first dial
// blocks until the test's safety context ends.
type deadWave struct {
	mu      sync.Mutex
	want    int
	arrived int
	dials   int
	release chan struct{}
	elapsed time.Duration
}

func (w *deadWave) dial(ctx context.Context, network string) (net.Conn, error) {
	w.mu.Lock()
	w.dials++
	w.arrived++
	ch := w.release
	if w.arrived == w.want {
		w.elapsed += loopbackDialTimeout
		w.arrived = 0
		close(w.release)
		w.release = make(chan struct{})
	}
	w.mu.Unlock()
	select {
	case <-ch:
		return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestCheckLoopbackPortsFree_DeadInterfaceAddressesCostOneTimeout(t *testing.T) {
	// A Mac with a VPN stack has several tunnel addresses, each of which
	// burns the full loopbackDialTimeout. Probed one after another, N of them
	// cost N timeouts before the workload starts, on every run. The interface
	// addresses are probed concurrently, so N dead ones cost about one; each
	// is still probed once per call and skipped for later ports.
	const n = 8
	var ifaces []net.Interface
	byName := map[string]net.Addr{}
	dead := map[string]bool{}
	for i := range n {
		name := "utun" + strconv.Itoa(i)
		ifaces = append(ifaces, net.Interface{Name: name, Flags: net.FlagUp | net.FlagPointToPoint})
		ip := net.IPv4(100, 101, 102, byte(10+i)).To4()
		byName[name] = &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}
		dead[ip.String()] = true
	}
	origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
	t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
	netInterfaces = func() ([]net.Interface, error) { return ifaces, nil }
	interfaceAddrs = func(ifc net.Interface) ([]net.Addr, error) { return []net.Addr{byName[ifc.Name]}, nil }
	wave := &deadWave{want: n, release: make(chan struct{})}
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, _, _ := net.SplitHostPort(addr); dead[host] {
			return wave.dial(ctx, network)
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}

	// Real time only bounds a failure: serial probing blocks here.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, checkLoopbackPortsFree(ctx, []int{3000, 5173, 8080}))
	assert.Equal(t, loopbackDialTimeout, wave.elapsed, "N dead interface addresses must cost one loopbackDialTimeout, not N")
	assert.Equal(t, n, wave.dials, "each dead address is probed once per call")
}

func TestCheckLoopbackPortsFree_InterfaceProbesAreBounded(t *testing.T) {
	// Concurrent probing is bounded: a host with many addresses does not open
	// a socket per address at once. Every address is still probed.
	const n = 3*loopbackProbeConcurrency + 1
	var addrs []net.Addr
	for i := range n {
		addrs = append(addrs, &net.IPNet{IP: net.IPv4(192, 168, 1, byte(10+i)).To4(), Mask: net.CIDRMask(24, 32)})
	}
	origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
	t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
	netInterfaces = func() ([]net.Interface, error) { return []net.Interface{{Name: "en0"}}, nil }
	interfaceAddrs = func(net.Interface) ([]net.Addr, error) { return addrs, nil }
	var inFlight, peak, dials atomic.Int32
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		cur := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}

	require.NoError(t, checkLoopbackPortsFree(context.Background(), []int{3000}))
	assert.LessOrEqual(t, int(peak.Load()), loopbackProbeConcurrency, "no more than loopbackProbeConcurrency probes in flight")
	assert.Equal(t, int32(n+2), dials.Load(), "every address, plus 127.0.0.1 and ::1")
}

func TestCheckLoopbackPortsFree_ConcurrentResultsAreJudgedInAddressOrder(t *testing.T) {
	// The interface probes for a port run together, but their results are read
	// in address order, as a serial check would read them: a held port at the
	// first address is reported even when a later address times out, and the
	// answer does not depend on which probe finishes first.
	origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
	t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
	netInterfaces = func() ([]net.Interface, error) { return []net.Interface{{Name: "en0"}}, nil }
	interfaceAddrs = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.IPv4(192, 168, 1, 20).To4(), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.IPv4(192, 168, 1, 21).To4(), Mask: net.CIDRMask(24, 32)},
		}, nil
	}
	var closed atomic.Bool
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		switch addr {
		case "192.168.1.20:3000":
			c1, c2 := net.Pipe()
			_ = c2.Close()
			return closeSpy{c1, &closed}, nil
		case "192.168.1.21:3000":
			return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}

	for range 20 {
		closed.Store(false)
		err := checkLoopbackPortsFree(context.Background(), []int{3000})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrOSLevelLoopbackPortHeld), "got %v", err)
		assert.Contains(t, err.Error(), "port 3000 on 192.168.1.20")
		assert.True(t, closed.Load(), "the probe's connection is closed")
	}
}

// closeSpy records that a probe's connection was closed.
type closeSpy struct {
	net.Conn
	closed *atomic.Bool
}

func (c closeSpy) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

func TestCheckLoopbackPortsFree_RoutableInterfaceAddressTimeoutFailsClosed(t *testing.T) {
	// The utun0 exemption is for zone-suffixed link-local addresses and the
	// Tailscale addresses of point-to-point (tunnel) interfaces only. A host's
	// routable LAN address is reached over lo0 (PR #753), so a probe
	// there is answered or refused at once; a timeout means a firewall in
	// stealth mode dropped the SYN, and a service bound to that address may
	// still be reachable through the grant. The port's state is unknown.
	// IPv4 link-local (169.254/16) fails closed too; it has its own test,
	// TestCheckLoopbackPortsFree_IPv4LinkLocalTimeoutFailsClosed.
	for _, tc := range []struct {
		ip   string
		mask net.IPMask
	}{
		{"192.168.1.20", net.CIDRMask(24, 32)},
		{"2001:db8::20", net.CIDRMask(64, 128)},
	} {
		ip := tc.ip
		t.Run(ip, func(t *testing.T) {
			addr := &net.IPNet{IP: net.ParseIP(ip), Mask: tc.mask}
			// A mask of the wrong family (4 bytes for an IPv6 address) makes
			// an IPNet that contains nothing, not even its own address.
			require.True(t, addr.Contains(addr.IP), "fixture %v: mask length must match the address family", addr)
			origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
			t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
			netInterfaces = func() ([]net.Interface, error) { return []net.Interface{{Name: "en0"}}, nil }
			interfaceAddrs = func(net.Interface) ([]net.Addr, error) {
				return []net.Addr{addr}, nil
			}
			loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
				if strings.Contains(addr, ip) {
					return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
				}
				return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
			}

			err := checkLoopbackPortsFree(context.Background(), []int{3000})
			require.Error(t, err, "a timeout at a routable interface address must not report the port free")
			assert.True(t, errors.Is(err, ErrOSLevelLoopbackCheckUnavailable), "got %v", err)
			assert.False(t, errors.Is(err, ErrOSLevelLoopbackPortHeld), "no port was found held: %v", err)
			assert.ErrorIs(t, err, os.ErrDeadlineExceeded)
			assert.Contains(t, err.Error(), "port 3000 on "+ip, "the refusal must name the probe that timed out")
			assert.Contains(t, err.Error(), "address of en0", "the refusal must name the address class")
		})
	}
}

func TestCheckLoopbackPortsFree_LinkLocalTimeoutIsProbedOncePerCall(t *testing.T) {
	// Each probe to utun0's link-local address burns the full
	// loopbackDialTimeout. Once it has timed out, probing it again on the next
	// port learns nothing and costs another 500ms, so a dead address is probed
	// once per call. Every other address is still probed on every port.
	origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
	t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
	netInterfaces = func() ([]net.Interface, error) { return []net.Interface{{Name: "utun0"}}, nil }
	interfaceAddrs = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::e493:3041:b67b:a5b2"), Mask: net.CIDRMask(64, 128)}}, nil
	}
	var timedOut, refused []string
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.Contains(addr, "%utun0") {
			timedOut = append(timedOut, addr)
			return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
		}
		refused = append(refused, addr)
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}

	require.NoError(t, checkLoopbackPortsFree(context.Background(), []int{3000, 5173, 8080}))
	assert.Equal(t, []string{"[fe80::e493:3041:b67b:a5b2%utun0]:3000"}, timedOut, "a dead link-local address is probed once per call")
	assert.Len(t, refused, 6, "127.0.0.1 and ::1 are still probed on every port")
}

func TestCheckLoopbackPortsFree_DeadLinkLocalSkipIsLoggedOncePerCall(t *testing.T) {
	// Skipping a dead link-local or tunnel address for later ports is silent
	// otherwise: the call returns nil having never learned whether anything
	// listens there on those ports. Each skipped address is logged once per
	// call, naming the ports left unchecked, so an operator can see the
	// unchecked class.
	origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
	t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
	netInterfaces = func() ([]net.Interface, error) { return []net.Interface{{Name: "utun0"}}, nil }
	interfaceAddrs = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::e493:3041:b67b:a5b2"), Mask: net.CIDRMask(64, 128)}}, nil
	}
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.Contains(addr, "%utun0") {
			return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))

	require.NoError(t, checkLoopbackPortsFree(ctx, []int{3000, 5173, 8080}))
	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "fe80::e493:3041:b67b:a5b2%utun0"), "one line per dead address per call: %s", out)
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "unchecked_ports=\"[5173 8080]\"", "the line names the ports left unchecked: %s", out)

	// A dead address on the last port leaves no port unchecked: nothing to log.
	buf.Reset()
	require.NoError(t, checkLoopbackPortsFree(ctx, []int{3000}))
	assert.Empty(t, buf.String())
}

func TestCheckLoopbackPortsFree_LinkLocalThatAnswersIsProbedOnEveryPort(t *testing.T) {
	// The once-per-call guard is for a timeout only. A refused connect at a
	// link-local address is a real answer, so later ports are still probed
	// there, and a listener there is still found.
	origIfaces, origAddrs, origDial := netInterfaces, interfaceAddrs, loopbackDial
	t.Cleanup(func() { netInterfaces, interfaceAddrs, loopbackDial = origIfaces, origAddrs, origDial })
	netInterfaces = func() ([]net.Interface, error) { return []net.Interface{{Name: "en0"}}, nil }
	interfaceAddrs = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)}}, nil
	}
	var linkLocal []string
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.Contains(addr, "%en0") {
			linkLocal = append(linkLocal, addr)
			if strings.HasSuffix(addr, ":5173") {
				c1, c2 := net.Pipe()
				_ = c2.Close()
				return c1, nil
			}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}

	err := checkLoopbackPortsFree(context.Background(), []int{3000, 5173})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOSLevelLoopbackPortHeld), "got %v", err)
	assert.Contains(t, err.Error(), "port 5173 on fe80::1%en0")
	assert.Equal(t, []string{"[fe80::1%en0]:3000", "[fe80::1%en0]:5173"}, linkLocal)
}

func TestCheckLoopbackPortsFree_IPv6LoopbackTimeoutFailsClosed(t *testing.T) {
	// The timeout rule covers both loopback addresses, not only 127.0.0.1.
	origIfaces, origDial := netInterfaces, loopbackDial
	t.Cleanup(func() { netInterfaces, loopbackDial = origIfaces, origDial })
	netInterfaces = func() ([]net.Interface, error) { return nil, nil }
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == "[::1]:3000" {
			return nil, &net.OpError{Op: "dial", Net: network, Err: os.ErrDeadlineExceeded}
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}

	err := checkLoopbackPortsFree(context.Background(), []int{3000})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOSLevelLoopbackCheckUnavailable), "got %v", err)
	assert.Contains(t, err.Error(), "port 3000 on ::1")
}

func TestCheckLoopbackPortsFree_RefusedDialMeansFree(t *testing.T) {
	// The other side of the timeout rule: a refused connect is a real answer,
	// and the port is free.
	origIfaces, origDial := netInterfaces, loopbackDial
	t.Cleanup(func() { netInterfaces, loopbackDial = origIfaces, origDial })
	netInterfaces = func() ([]net.Interface, error) { return nil, nil }
	var dials int
	loopbackDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials++
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
	}

	require.NoError(t, checkLoopbackPortsFree(context.Background(), []int{3000, 5173}))
	assert.Equal(t, 4, dials, "every port at 127.0.0.1 and ::1")
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
