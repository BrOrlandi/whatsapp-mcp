#!/usr/bin/env bash
#
# Management command for a whatsapp-mcp installed by install.sh. Installed to
# /usr/local/bin/whatsapp-mcp; it is only a thin wrapper that runs Compose in
# the install directory with the right overlay, so nobody has to remember the
# file list.
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/whatsapp-mcp}"
COMPOSE=(docker compose -f docker-compose.yml -f deploy/docker-compose.public.yml)

[ -d "${INSTALL_DIR}" ] || { echo "No installation at ${INSTALL_DIR}." >&2; exit 1; }
cd "${INSTALL_DIR}"

usage() {
    cat <<'USAGE'
whatsapp-mcp <command>

  status     containers, health and the public URL
  logs [svc] follow the logs, of everything or of one service
  restart    restart the stack
  stop       stop the stack, keeping the data
  start      start it again
  update     move to the newest release, keeping secrets and data
  url        print the public URL
  version    print the running version
USAGE
}

case "${1:-}" in
    status)
        "${COMPOSE[@]}" ps
        printf '\nURL: https://%s\n' "$(cat hostname 2>/dev/null || echo 'unknown')"
        ;;
    logs)    shift; "${COMPOSE[@]}" logs -f --tail 200 "$@" ;;
    restart) "${COMPOSE[@]}" restart ;;
    stop)    "${COMPOSE[@]}" stop ;;
    start)   "${COMPOSE[@]}" up -d ;;
    update)
        # update.sh is the update, and this is one way to reach it. The other is
        # the one-liner people are sent, which curls the same script; two
        # implementations would diverge at the first fix only one of them got.
        # The copy in the checkout is used when it is there, so an update can be
        # run on a machine with no outbound network.
        shift
        export INSTALL_DIR
        if [ -x ./update.sh ]; then
            exec ./update.sh "$@"
        fi
        curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/update.sh | bash
        ;;
    url)     cat hostname 2>/dev/null | sed 's|^|https://|' ;;
    version)
        # What the container reports, which is what is actually serving. The
        # checkout is the fallback for a stopped stack, and says what would run.
        reported=$("${COMPOSE[@]}" exec -T whatsapp-mcp \
            wget -qO- http://127.0.0.1:8080/healthz 2>/dev/null </dev/null \
            | sed -n 's/.*"version" *: *"\([^"]*\)".*/\1/p' | head -1) || true
        if [ -n "${reported}" ]; then
            printf '%s\n' "${reported}"
        else
            git describe --tags --always 2>/dev/null | sed 's/^v//' || echo unknown
        fi
        ;;
    ""|-h|--help|help) usage ;;
    *) echo "Unknown command: $1" >&2; usage >&2; exit 1 ;;
esac
