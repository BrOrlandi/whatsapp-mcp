#!/usr/bin/env bash
# Installs the host agent behind the panel's "Atualizar" button.
#
#   sudo bash deploy/install-updater.sh install   # an install.sh checkout
#   sudo bash deploy/install-updater.sh dokploy   # a Dokploy compose service
#
# install.sh and update.sh run the first form themselves. The dokploy form
# expects /etc/whatsapp-mcp/updater.env to hold DOKPLOY_API_URL,
# DOKPLOY_API_KEY, DOKPLOY_COMPOSE_ID, DOKPLOY_PROJECT and PUBLIC_URL; it is
# root-only, because the Dokploy key can redeploy anything on that host.
#
# Running it again is safe: it replaces the agent and the units with this
# checkout's copies and restarts the watch.
set -euo pipefail

METHOD="${1:-install}"
case "${METHOD}" in install|dokploy) ;; *) echo "usage: $0 install|dokploy" >&2; exit 2 ;; esac
[ "$(id -u)" -eq 0 ] || { echo "run this with sudo" >&2; exit 1; }
command -v systemctl >/dev/null || { echo "this host has no systemd; the panel will keep showing the SSH command" >&2; exit 0; }

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UPDATE_DIR="${UPDATE_DIR:-/var/lib/whatsapp-mcp/update}"
# The gateway runs as uid 10001 (see the Dockerfile) and has to write its
# requests here; the agent runs as root and can write anywhere.
GATEWAY_UID=10001

install -d -m 0755 -o "${GATEWAY_UID}" -g "${GATEWAY_UID}" "${UPDATE_DIR}"
install -d -m 0755 /usr/local/lib/whatsapp-mcp /etc/whatsapp-mcp
install -m 0755 "${HERE}/updater.sh" /usr/local/lib/whatsapp-mcp/updater.sh

CONF=/etc/whatsapp-mcp/updater.env
touch "${CONF}"
chmod 0600 "${CONF}"
set_conf() {
    if grep -q "^$1=" "${CONF}"; then
        sed -i "s|^$1=.*|$1=$2|" "${CONF}"
    else
        printf '%s=%s\n' "$1" "$2" >> "${CONF}"
    fi
}
set_conf UPDATE_METHOD "${METHOD}"
set_conf UPDATE_DIR "${UPDATE_DIR}"
if [ "${METHOD}" = "install" ]; then
    set_conf INSTALL_DIR "${INSTALL_DIR:-/opt/whatsapp-mcp}"
else
    for name in DOKPLOY_API_URL DOKPLOY_API_KEY DOKPLOY_COMPOSE_ID DOKPLOY_PROJECT PUBLIC_URL; do
        grep -q "^${name}=." "${CONF}" || { echo "${CONF} is missing ${name}" >&2; exit 1; }
    done
fi

cat > /etc/systemd/system/whatsapp-mcp-updater.service <<UNIT
[Unit]
Description=whatsapp-mcp update requested from the panel
After=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/lib/whatsapp-mcp/updater.sh
TimeoutStartSec=30min
UNIT

cat > /etc/systemd/system/whatsapp-mcp-updater.path <<UNIT
[Unit]
Description=Watch for whatsapp-mcp update requests from the panel

[Path]
PathExists=${UPDATE_DIR}/request.json

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now whatsapp-mcp-updater.path >/dev/null
systemctl restart whatsapp-mcp-updater.path

# agent.json is what makes the panel show the button instead of the command.
cat > "${UPDATE_DIR}/agent.json" <<JSON
{
  "method": "${METHOD}",
  "installed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
JSON
chmod 0644 "${UPDATE_DIR}/agent.json"
echo "update agent installed (${METHOD}); the panel's update button is live"
