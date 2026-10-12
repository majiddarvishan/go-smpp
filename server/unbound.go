package server

import (
	"net"
	"net/netip"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// hostKey reduces a remote address to the IP alone: the port is dropped, an
// IPv4-mapped IPv6 address becomes its IPv4 form, and any IPv6 zone is removed,
// so one peer is one key however it connected.
func hostKey(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	if tcp, ok := addr.(*net.TCPAddr); ok {
		if ap := tcp.AddrPort(); ap.IsValid() {
			return ap.Addr().Unmap().WithZone("").String()
		}
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return a.Unmap().WithZone("").String()
	}
	return host
}

// exemptFromUnboundCap reports whether host (a hostKey) is in a network that
// Config.UnboundCapExempt excludes from the cap.
func (c Config) exemptFromUnboundCap(host string) bool {
	if len(c.UnboundCapExempt) == 0 {
		return false
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, p := range c.UnboundCapExempt {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// unboundFromLocked counts sessions from host that have not completed a bind.
// s.mu must be held. It scans only that host's sessions, so the cost follows
// one peer's connections, not the size of the server.
func (s *Server) unboundFromLocked(host string) int {
	n := 0
	for sess := range s.byHost[host] {
		if sess.State() == protocol.StateOpen {
			n++
		}
	}
	return n
}
