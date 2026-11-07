#!/usr/bin/env bash
set -euo pipefail
umask 077

MODE="${1:-all}"
SUBMIT_WAIT="${SMPP_INTEROP_SUBMIT_WAIT:-15s}"
case "$MODE" in
  all|smsc|esme) ;;
  *)
    echo "Usage: $0 [all|smsc|esme]" >&2
    exit 2
    ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

prompt_secret() {
  local variable="$1"
  local prompt="$2"
  local value="${!variable:-}"
  if [[ -n "$value" ]]; then
    return
  fi
  if [[ ! -t 0 ]]; then
    echo "Missing $variable. Run from a terminal or provide it in the environment." >&2
    exit 2
  fi
  read -r -s -p "$prompt" value
  printf '\n'
  if [[ -z "$value" ]]; then
    echo "A non-empty password is required." >&2
    exit 2
  fi
  export "$variable=$value"
  unset value
}

if [[ "$MODE" == "all" || "$MODE" == "smsc" ]]; then
  prompt_secret SMPP_INTEROP_SMSC_PASSWORD "SMSC simulator bind password: "
fi
if [[ "$MODE" == "all" || "$MODE" == "esme" ]]; then
  prompt_secret SMPP_INTEROP_ESME_PASSWORD "ESME simulator bind password: "
fi

RUN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/go-smpp-interop.XXXXXX")"
chmod 700 "$RUN_DIR"
export SMPP_INTEROP_REPORT_PATH="$RUN_DIR/report.json"
export SMPP_INTEROP_CANDIDATE_PATH="$RUN_DIR/manifest-candidate.json"

cat <<EOF
go-smpp local interoperability probe
Mode: $MODE
SMSC peer: ${SMPP_INTEROP_SMSC_ADDRESS:-127.0.0.1:3000}
go-smpp server listener: ${SMPP_INTEROP_ESME_LISTEN_ADDRESS:-127.0.0.1:4000}
ESME submit observation window: ${SUBMIT_WAIT}

Before continuing:
- Start the SMSC simulator for mode all or smsc.
- Keep the ESME simulator stopped until the runner prints READY.
- Both peers must be SMPP 3.4, plain TCP, and configured for one TRX connection.
- No real subscriber data is required; the probe sends synthetic test addresses and text.
- The runner never writes passwords or unsanitized raw PDUs to the report.
EOF

set +e
go run -tags=interop ./cmd/interop-probe -mode "$MODE" -submit-wait "$SUBMIT_WAIT" 2>&1 | tee "$RUN_DIR/run.log"
RUN_STATUS=${PIPESTATUS[0]}
set -e

ARCHIVE="${RUN_DIR}.tar.gz"
tar -czf "$ARCHIVE" -C "$(dirname "$RUN_DIR")" "$(basename "$RUN_DIR")"
chmod 600 "$ARCHIVE"
echo
echo "Sanitized evidence archive: $ARCHIVE"
echo "Exit status: $RUN_STATUS"
exit "$RUN_STATUS"
