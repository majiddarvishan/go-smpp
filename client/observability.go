package client

// MetricsSnapshot is a lock-free point-in-time view of client lifecycle
// counters. Per-session protocol traffic counters are exposed by
// session.Session.Metrics.
type MetricsSnapshot struct {
	Reconnects        uint64
	ReconnectFailures uint64
}
