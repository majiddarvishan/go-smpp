# Timeout and liveness semantics

All durations are configured through session.Config. A zero value selects the documented default. A negative duration disables that timer where supported by the runtime.

## Request response timeout

ResponseTimeout is the SMPP protocol-response deadline for ordinary request PDUs. It starts after the complete request PDU has been successfully handed to the active transport.

Time spent waiting for request-window capacity or TX-queue admission is not charged to the protocol response timeout; that waiting is governed by the caller context. If the transport fails before full dispatch, the request completes as session/transport loss rather than as a response timeout.

When the response deadline expires, the pending request is removed atomically, its window slot is released exactly once, and the caller receives a typed *session.TimeoutError. A later response with the expired sequence number is late/unmatched.

## Caller context

The caller context governs window waiting, TX admission, and waiting for the response. Cancellation never triggers automatic replay. If cancellation races with a response, timeout, or session loss, exactly one terminal completion wins.

## Session initialization timeout

SessionInitTimeout starts when the session object is created. While the state remains Open or Outbound, reaching the timeout terminates that session with TimeoutSessionInit. A successful bind moves the session out of this timer scope.

Default: 30 seconds.

## Enquire Link

After a bound session has had no SMPP activity for EnquireLinkInterval, the liveness loop sends enquire_link. Its response uses the same sequence correlation and pending machinery as ordinary requests.

EnquireLinkTimeout is the response timeout for the probe. If disabled, ResponseTimeout is used as the fallback.

Defaults: interval 30 seconds; response timeout 10 seconds.

## Inactivity timeout

InactivityTimeout applies only in a bound state. If no inbound or outbound SMPP activity has been recorded for the configured interval, the session closes with TimeoutInactivity.

Default: 2 minutes.

Choose EnquireLinkInterval lower than InactivityTimeout when keepalive traffic should preserve a healthy otherwise-idle session.

## Reconnect interaction

For a dialed client.Client with reconnect enabled, a timeout that terminates the current session may lead to reconnect/rebind according to client.ReconnectPolicy. Requests that belonged to the lost session are not replayed automatically.
