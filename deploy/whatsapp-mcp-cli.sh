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
  update     pull the latest code and rebuild, keeping secrets and data
  url        print the public URL
  version    print the installed commit
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
        # Secrets, the hostname and the volumes are untouched: only the code and
        # the images move. Migrations run when the gateway starts.
        git fetch --quiet origin
        git checkout --quiet -B "$(git rev-parse --abbrev-ref HEAD)" "origin/$(git rev-parse --abbrev-ref HEAD)"
        "${COMPOSE[@]}" up -d --pull always  # update: always fetch the newer image
        printf '\nUpdated to %s\n' "$(git rev-parse --short HEAD)"
        ;;
    url)     cat hostname 2>/dev/null | sed 's|^|https://|' ;;
    version) git rev-parse --short HEAD 2>/dev/null || echo unknown ;;
    ""|-h|--help|help) usage ;;
    *) echo "Unknown command: $1" >&2; usage >&2; exit 1 ;;
esac
