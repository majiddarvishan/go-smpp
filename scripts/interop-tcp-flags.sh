#!/usr/bin/env bash
# Opt-in local interoperability diagnostics. Emits TCP SYN/FIN/RST headers
# only; never saves packet bytes, dumps PDU payloads or reads credentials.
set -euo pipefail

usage() {
  echo "Usage: bash scripts/interop-tcp-flags.sh [loopback_tcp_port]" >&2
  echo "Default port: 4000. Run concurrently with scripts/interop-local.sh esme." >&2
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi
if (( $# > 1 )); then
  usage
  exit 2
fi

PORT="${1:-4000}"
if [[ ! "$PORT" =~ ^[0-9]{1,5}$ ]]; then
  echo "Expected numeric port in 1..65535." >&2
  exit 2
fi
PORT=$((10#$PORT))
if (( PORT < 1 || PORT > 65535 )); then
  echo "Expected numeric port in 1..65535." >&2
  exit 2
fi

# IPv4 loopback, default Ethernet-format capture on Linux lo. 54 bytes
# captures the Ethernet + IPv4 + minimum TCP headers; no SMPP body.
# Do not use -q: it suppresses the very TCP flags being investigated.
# Do not add -A, -X, -xx, -w or equivalent raw packet export options.
FILTER="ip and tcp port $PORT and (tcp[13] & 0x07 != 0)"
ARGS=(-i lo -nn -tttt -l -s 54 "$FILTER")

if [[ "${SMPP_INTEROP_TCP_FLAGS_DRY_RUN:-0}" == "1" ]]; then
  printf 'command=tcpdump\n'
  printf 'arg=%s\n' "${ARGS[@]}"
  exit 0
fi

if ! command -v tcpdump >/dev/null 2>&1; then
  echo "tcpdump is required; install it on the local test host." >&2
  exit 2
fi

echo "Capturing loopback IPv4 TCP SYN/FIN/RST header summaries for port $PORT; stop with Ctrl+C." >&2
echo "Output uses local time; compare to the probe's UTC timestamps and numeric TCP ports." >&2
if (( EUID == 0 )); then
  exec tcpdump "${ARGS[@]}"
fi
if ! command -v sudo >/dev/null 2>&1; then
  echo "Need local tcpdump capture permission (root/CAP_NET_RAW); sudo unavailable." >&2
  exit 2
fi
exec sudo tcpdump "${ARGS[@]}"
