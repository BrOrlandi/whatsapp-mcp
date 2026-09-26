#!/usr/bin/env bash
# The host half of "Atualizar" in the panel.
#
# systemd starts this when the panel drops request.json into the shared
# directory (whatsapp-mcp-updater.path). It claims the request, moves the
# deployment to the version asked for, and writes status.json as it goes so
# the panel can show progress. It runs as root because updating means driving
# Docker; that is exactly why this lives on the host and not in the gateway.
#
# The request is written by the gateway, so it is treated as untrusted input:
# the only field used is the target version, which has to match a strict
# semantic-version pattern and name a release GitHub has published. Nothing
# from the file is ever passed to a shell unquoted.
#
# Two methods, chosen when the agent was installed (install-updater.sh):
#   install  an install.sh checkout: runs update.sh, which backs up, moves the
#            checkout and the image, restarts and waits for /healthz
#   dokploy  a Dokploy compose service: backs up, sets WHATSAPP_MCP_TAG in the
#            service's env through Dokploy's API, redeploys, and waits for
#            the public /healthz to report the new version
set -uo pipefail

CONF="${CONF:-/etc/whatsapp-mcp/updater.env}"
# shellcheck disable=SC1090
[ -f "${CONF}" ] && . "${CONF}"
UPDATE_DIR="${UPDATE_DIR:-/var/lib/whatsapp-mcp/update}"
METHOD="${UPDATE_METHOD:-install}"
INSTALL_DIR="${INSTALL_DIR:-/opt/whatsapp-mcp}"
REPO="${REPO:-BrOrlandi/whatsapp-mcp}"
REQUEST="${UPDATE_DIR}/request.json"
STATUS="${UPDATE_DIR}/status.json"
LOG="${UPDATE_DIR}/update.log"

[ -f "${REQUEST}" ] || exit 0

field() { sed -n "s/^[[:space:]]*\"$1\":[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$2" | head -1; }
ID=$(field id "${REQUEST}")
TARGET=$(field target "${REQUEST}")
# Claimed before anything else, so the path unit does not start this again
# for the same request while it runs.
rm -f "${REQUEST}"

STARTED=$(date -u +%Y-%m-%dT%H:%M:%SZ)
: > "${LOG}"

json_string() {
    # Escapes a line for a JSON string: backslash, quote, and drops control
    # characters, which is all update.sh's output can contain.
    printf '"%s"' "$(printf '%s' "$1" | tr -d '\000-\011\013-\037' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr '\n' ' ')"
}

status() {
    local state="$1" phase="$2" message="${3:-}" lines="" line first=true
    while IFS= read -r line; do
        [ -n "${line}" ] || continue
        if ${first}; then first=false; else lines="${lines},"; fi
        lines="${lines}$(json_string "${line}")"
    done < <(tail -n 15 "${LOG}" 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g')
    cat > "${STATUS}.tmp" <<JSON
{
  "id": $(json_string "${ID}"),
  "target": $(json_string "${TARGET}"),
  "state": $(json_string "${state}"),
  "phase": $(json_string "${phase}"),
  "message": $(json_string "${message}"),
  "log": [${lines}],
  "started_at": "${STARTED}",
  "updated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
JSON
    chmod 0644 "${STATUS}.tmp"
    mv "${STATUS}.tmp" "${STATUS}"
}

fail() { status failed "$1" "$2"; exit 1; }

printf '%s' "${ID}" | grep -Eq '^[0-9a-f]{16}$' || fail "checking the request" "O pedido não tem um id válido."
printf '%s' "${TARGET}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$' \
    || fail "checking the request" "A versão pedida não é uma versão de release."

status running "Conferindo a versão ${TARGET} no GitHub"
if ! curl -fsS -o /dev/null --max-time 20 "https://api.github.com/repos/${REPO}/releases/tags/v${TARGET}"; then
    fail "checking the release" "A versão ${TARGET} não é uma release publicada no GitHub, ou o GitHub não respondeu."
fi

# Runs a command with its output in the log, refreshing status.json with the
# latest line every few seconds.
run_logged() {
    local label="$1"; shift
    "$@" >>"${LOG}" 2>&1 </dev/null &
    local pid=$!
    while kill -0 "${pid}" 2>/dev/null; do
        status running "${label}"
        sleep 3
    done
    wait "${pid}"
}

case "${METHOD}" in
install)
    [ -f "${INSTALL_DIR}/update.sh" ] || fail "finding the installation" "Não há instalação em ${INSTALL_DIR}."
    # update.sh moves the checkout it lives in, so it runs from a copy: bash
    # reads a script as it goes, and the file changing underneath it would
    # have it execute half of one version and half of another.
    COPY=$(mktemp /tmp/whatsapp-mcp-update.XXXXXX)
    cp "${INSTALL_DIR}/update.sh" "${COPY}"
    if run_logged "Atualizando para ${TARGET}" env INSTALL_DIR="${INSTALL_DIR}" REPO_REF="v${TARGET}" bash "${COPY}"; then
        rm -f "${COPY}"
        status succeeded "Concluída" "A versão ${TARGET} está rodando."
    else
        rm -f "${COPY}"
        fail "updating" "A atualização falhou. O backup do banco e o comando para voltar estão no registro abaixo."
    fi
    ;;
dokploy)
    : "${DOKPLOY_API_URL:?}" "${DOKPLOY_API_KEY:?}" "${DOKPLOY_COMPOSE_ID:?}" "${DOKPLOY_PROJECT:?}" "${PUBLIC_URL:?}"
    command -v python3 >/dev/null || fail "checking the host" "O método dokploy precisa de python3 no servidor."

    status running "Fazendo backup do banco"
    BACKUP_DIR="${BACKUP_DIR:-/var/backups/whatsapp-mcp}"
    mkdir -p "${BACKUP_DIR}" && chmod 0700 "${BACKUP_DIR}"
    BACKUP="${BACKUP_DIR}/before-${TARGET}-$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
    DB_CONTAINER="${DOKPLOY_PROJECT}-postgres-mcp-1"
    if ! docker exec "${DB_CONTAINER}" sh -c 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' 2>>"${LOG}" | gzip > "${BACKUP}"; then
        fail "backing up" "O backup do banco falhou, então nada foi alterado."
    fi
    printf 'backup: %s\n' "${BACKUP}" >>"${LOG}"

    status running "Trocando a versão no Dokploy"
    if ! DOKPLOY_TARGET="${TARGET}" python3 - >>"${LOG}" 2>&1 <<'PY'
import json, os, urllib.request
url, key, cid, tag = os.environ["DOKPLOY_API_URL"].rstrip("/"), os.environ["DOKPLOY_API_KEY"], os.environ["DOKPLOY_COMPOSE_ID"], os.environ["DOKPLOY_TARGET"]
def call(path, body=None):
    req = urllib.request.Request(url + "/api/" + path, headers={"x-api-key": key, "Content-Type": "application/json"},
                                 data=None if body is None else json.dumps(body).encode(), method="GET" if body is None else "POST")
    with urllib.request.urlopen(req, timeout=30) as r:
        raw = r.read()
        return json.loads(raw) if raw else None
compose = call("compose.one?composeId=" + cid)
lines = [l for l in (compose.get("env") or "").splitlines() if not l.startswith("WHATSAPP_MCP_TAG=")]
lines.append("WHATSAPP_MCP_TAG=" + tag)
call("compose.update", {"composeId": cid, "env": "\n".join(lines)})
call("compose.deploy", {"composeId": cid})
print("dokploy: WHATSAPP_MCP_TAG=" + tag + ", deploy queued")
PY
    then
        fail "calling Dokploy" "O Dokploy recusou a troca de versão. O backup está em ${BACKUP}."
    fi

    status running "Esperando a versão ${TARGET} subir"
    for _ in $(seq 1 100); do
        running=$(curl -fsS --max-time 5 "${PUBLIC_URL%/}/healthz" 2>/dev/null | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')
        if [ "${running}" = "${TARGET}" ]; then
            printf 'healthz reports %s\n' "${running}" >>"${LOG}"
            status succeeded "Concluída" "A versão ${TARGET} está rodando. No repositório de infraestrutura, o pin do compose continua na versão anterior; o WHATSAPP_MCP_TAG do Dokploy é que manda agora."
            exit 0
        fi
        status running "Esperando a versão ${TARGET} subir"
        sleep 6
    done
    fail "waiting" "A versão ${TARGET} não respondeu em 10 minutos. O backup está em ${BACKUP}; veja o deploy no Dokploy."
    ;;
*)
    fail "checking the agent" "Método de atualização desconhecido: ${METHOD}."
    ;;
esac
