- On macOS, the held-port check before a sandboxed run with loopback ports
  again refuses the run when a probe times out at a point-to-point interface
  address outside Tailscale's ranges (100.64.0.0/10 and fd7a:115c:a1e0::/48),
  such as a PPP, L2TP or container tun link. Only Tailscale's addresses on a
  point-to-point interface, and zone-scoped IPv6 link-local addresses, may
  time out without refusing the run. The interface addresses are now probed
  concurrently, so several dead tunnel addresses cost one 500 ms probe timeout
  rather than one each, and the refusal names the kind of address that timed
  out (for example "IPv4 link-local address of en0"). (epic 160)
