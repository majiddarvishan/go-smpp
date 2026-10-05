// Package transport provides plain TCP and optional TLS-over-TCP adapters for
// SMPP sessions.
//
// The boundary remains net.Conn-compatible so callers may supply custom ordered
// byte streams with TCP-like semantics. Plain *net.TCPConn sessions can use
// net.Buffers scatter/gather writes for bounded TX batching. X.25 is
// intentionally out of scope.
//
// # TLS
//
// SMPP has no confidentiality of its own: bind passwords and message content
// cross a plain connection in clear text. Use TLS on any link you do not
// fully control.
//
// TLS policy is entirely caller-supplied. DialTLS and ListenTLS hand the
// *tls.Config you give them to crypto/tls untouched; this package never
// weakens it, tightens it, or fills in a default. That is deliberate (crypto
// policy belongs to the deployment), and it is why the baseline below is
// something you write, not something you get.
//
// Recommended baseline, server side (server.Config.TLSConfig, ListenTLS):
//
//	&tls.Config{
//		Certificates: []tls.Certificate{certificate},
//		MinVersion:   tls.VersionTLS12, // tls.VersionTLS13 if every peer supports it
//	}
//
// Recommended baseline, client side (client.Config.TLSConfig, DialTLS):
//
//	&tls.Config{
//		RootCAs:    pool,               // the CAs that issued the SMSC's certificate
//		ServerName: "smsc.example.com", // required, see below
//		MinVersion: tls.VersionTLS12,
//	}
//
// Things that are easy to get wrong:
//
//   - A nil TLSConfig means plain TCP, not "default TLS". In server.Config and
//     client.Config the TLS path is taken only when TLSConfig is non-nil, so a
//     forgotten field silently gives you an unencrypted connection. Check the
//     link, not just the config.
//   - Set MinVersion explicitly. The default floor differs between Go releases
//     and GODEBUG settings, so do not rely on it.
//   - Keep peer certificate verification on, and never set InsecureSkipVerify
//     outside tests. With it off, anyone on the path can impersonate the SMSC
//     and collect the bind password. Leave RootCAs nil to use the system roots,
//     or set it to your own pool.
//   - DialTLS does not derive ServerName from the address, unlike
//     crypto/tls.Dial. With ServerName empty and InsecureSkipVerify false the
//     handshake fails with an error naming ServerName. Set it to the name in
//     the server's certificate.
//   - For a bilateral link, mutual TLS is a plain pass-through: set
//     ClientAuth: tls.RequireAndVerifyClientCert and ClientCAs on the server,
//     and Certificates on the client. It supplements, not replaces, the SMPP
//     bind authentication.
//   - TLS protects the connection, not what you do with the password
//     afterwards. See session.PacketTracer before enabling raw packet tracing.
//
// Each statement above is pinned by tls_baseline_test.go in this package.
package transport
