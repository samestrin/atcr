- On macOS, a sandboxed run with loopback ports is no longer refused just
  because the host runs a VPN such as Tailscale. A held-port probe that timed
  out at Tailscale's address on a point-to-point (tunnel) interface, such as
  its `fd7a:115c:a1e0:...` address on a `utun` interface, made the check fail
  closed.
  A dial to that address goes into the tunnel, not over lo0, so the loopback
  grant cannot reach it. Such an address is now treated like a utun0 link-local
  one: a timeout there does not refuse the run, and the address is probed once
  per check. A timeout at 127.0.0.1, ::1 or a LAN address still refuses the run.
  (EST-8465)
