#!/usr/bin/env bash
# Opens an SSH tunnel to the Evolution Go running on the server.
#
# Evolution has no published port and no public domain: it lives on the
# project's internal Docker network, reachable only by the gateway. That is
# deliberate. To develop against it, this script asks the host for the
# container's address on the Docker bridge — which the host can reach even
# though the outside world cannot — and forwards a local port to it.
set -euo pipefail

HOST="${1:-ubuntu@example.com}"
LOCAL_PORT="${2:-4001}"
CONTAINER="${EVOLUTION_CONTAINER:-evolution-go}"

echo "Procurando o container ${CONTAINER} em ${HOST}…"
remote_ip=$(ssh "$HOST" "docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' \$(docker ps -qf name=${CONTAINER} | head -1) | awk '{print \$1}'")

if [ -z "$remote_ip" ]; then
  echo "Não encontrei o container ${CONTAINER} em ${HOST}." >&2
  echo "Confira o nome com: ssh ${HOST} 'docker ps --format \"{{.Names}}\"'" >&2
  exit 1
fi

echo "Container em ${remote_ip}. Túnel: http://127.0.0.1:${LOCAL_PORT} → ${remote_ip}:4000"
echo "Deixe este terminal aberto. Ctrl+C encerra o túnel."
exec ssh -N -L "${LOCAL_PORT}:${remote_ip}:4000" "$HOST"
