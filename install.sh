#!/usr/bin/env bash
#
# WhatsApp MCP — one-command install on a clean Linux VM.
#
#   curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash
#
# Installs Docker if it is missing, clones this project into /opt/whatsapp-mcp,
# generates every secret, derives a hostname from the machine's own public IPv4
# through sslip.io, gets a Let's Encrypt certificate for it, and starts the
# stack behind Traefik. Re-running it is safe: existing secrets, the hostname
# and the administrator are left alone.
set -euo pipefail

# This script is meant to be piped into bash, where stdin holds the
# not-yet-read remainder of the script itself. So any command that reads stdin
# eats the rest of the install and exits 0 — a failure that looks exactly like
# success. Every such command below gets </dev/null; closing stdin globally is
# not an option here, because that is where bash is reading this from.

REPO_URL="${REPO_URL:-https://github.com/BrOrlandi/whatsapp-mcp.git}"
REPO_REF="${REPO_REF:-main}"
INSTALL_DIR="${INSTALL_DIR:-/opt/whatsapp-mcp}"
CLI_PATH="/usr/local/bin/whatsapp-mcp"
COMPOSE_FILES=(-f docker-compose.yml -f deploy/docker-compose.public.yml)

step()  { printf '\n\033[1;36m==>\033[0m %s\n' "$*"; }
info()  { printf '    %s\n' "$*"; }
warn()  { printf '\033[1;33m    warning:\033[0m %s\n' "$*" >&2; }
fail()  { printf '\n\033[1;31mInstallation failed at: %s\033[0m\n' "$*" >&2; exit 1; }

trap 'fail "${CURRENT_STEP:-an unknown step}"' ERR

# ---------------------------------------------------------------- preflight

CURRENT_STEP="checking privileges"
step "Checking the machine"
[ "$(id -u)" -eq 0 ] || fail "checking privileges: run this with sudo"

CURRENT_STEP="checking the operating system"
if [ -r /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
else
    fail "checking the operating system: /etc/os-release is missing"
fi
case "${ID:-}:${VERSION_ID:-}" in
    ubuntu:24.04) info "Ubuntu 24.04 LTS — the tested target." ;;
    ubuntu:*|debian:*) warn "${PRETTY_NAME:-this system} is not Ubuntu 24.04 LTS; continuing, but that is what is tested." ;;
    *) fail "checking the operating system: ${PRETTY_NAME:-this system} is not Debian or Ubuntu" ;;
esac

case "$(uname -m)" in
    x86_64|aarch64) ;;
    *) fail "checking the architecture: $(uname -m) has no image published for it" ;;
esac

CURRENT_STEP="checking free disk space"
AVAILABLE_MB=$(df -Pm /opt 2>/dev/null | awk 'NR==2 {print $4}' || echo 0)
[ "${AVAILABLE_MB:-0}" -ge 5000 ] || warn "less than 5 GB free on /opt (${AVAILABLE_MB} MB); the images and the databases need room."

# ---------------------------------------------------------------- packages

CURRENT_STEP="installing base packages"
step "Installing base packages"
export DEBIAN_FRONTEND=noninteractive

# A cloud VM boots straight into unattended-upgrades and apt-daily, which hold
# the apt locks for the first minute or two of its life — exactly when someone
# pastes this command into a fresh machine. Waiting is the whole fix: apt is
# told to block rather than fail, and we also wait for the lists lock, which
# DPkg::Lock::Timeout does not cover.
APT_OPTS=(-o DPkg::Lock::Timeout=600)
wait_for_apt() {
    local waited=0
    while fuser /var/lib/dpkg/lock-frontend /var/lib/apt/lists/lock \
                /var/cache/apt/archives/lock >/dev/null 2>&1; do
        if [ "$waited" -eq 0 ]; then
            info "waiting for the system's own package updates to finish…"
        fi
        if [ "$waited" -ge 600 ]; then
            fail "installing base packages: apt is still locked after 10 minutes. Check: systemctl status unattended-upgrades"
        fi
        sleep 5
        waited=$((waited + 5))
    done
    [ "$waited" -gt 0 ] && info "apt is free after ${waited}s"
    return 0
}

wait_for_apt
apt-get "${APT_OPTS[@]}" update -qq
apt-get "${APT_OPTS[@]}" install -y -qq --no-install-recommends ca-certificates curl git openssl >/dev/null
info "ca-certificates, curl, git, openssl"

CURRENT_STEP="installing Docker"
step "Installing Docker"
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    info "Docker Engine and Compose v2 are already present: $(docker --version)"
else
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL "https://download.docker.com/linux/${ID}/gpg" -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/${ID} ${VERSION_CODENAME} stable" \
        > /etc/apt/sources.list.d/docker.list
    wait_for_apt
    apt-get "${APT_OPTS[@]}" update -qq
    apt-get "${APT_OPTS[@]}" install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin >/dev/null
    info "installed $(docker --version)"
fi
# The stack has to come back by itself after a reboot, which needs both the
# daemon enabled and `restart: unless-stopped` on the containers.
systemctl enable --now docker >/dev/null 2>&1 || warn "could not enable the Docker service; the stack may not return after a reboot."

# ---------------------------------------------------------------- sources

CURRENT_STEP="fetching the project"
step "Fetching the project into ${INSTALL_DIR}"
if [ -d "${INSTALL_DIR}/.git" ]; then
    git -C "${INSTALL_DIR}" remote set-url origin "${REPO_URL}"
    git -C "${INSTALL_DIR}" fetch --quiet origin "${REPO_REF}"
    git -C "${INSTALL_DIR}" checkout --quiet -B "${REPO_REF}" "origin/${REPO_REF}"
    info "updated to $(git -C "${INSTALL_DIR}" rev-parse --short HEAD)"
else
    mkdir -p "$(dirname "${INSTALL_DIR}")"
    git clone --quiet --branch "${REPO_REF}" --depth 1 "${REPO_URL}" "${INSTALL_DIR}"
    info "cloned at $(git -C "${INSTALL_DIR}" rev-parse --short HEAD)"
fi
cd "${INSTALL_DIR}"
chmod 750 "${INSTALL_DIR}"

# ---------------------------------------------------------------- identity

CURRENT_STEP="determining the public address"
step "Determining the public address"
detect_ip() {
    local source ip
    # Several sources, because one of them being down must not stop an install.
    for source in \
        "https://api.ipify.org" \
        "https://ifconfig.me/ip" \
        "https://ipv4.icanhazip.com" \
        "https://checkip.amazonaws.com"
    do
        ip=$(curl -4 -fsS --max-time 8 "$source" 2>/dev/null | tr -d '[:space:]') || continue
        if [[ "$ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
            printf '%s' "$ip"
            return 0
        fi
    done
    return 1
}
PUBLIC_IP=$(detect_ip) || fail "determining the public address: no source could report this machine's public IPv4"
info "public IPv4: ${PUBLIC_IP}"

CURRENT_STEP="deriving the hostname"
# The id and the hostname are written once. A reinstall or an update must not
# move the URL out from under a configured MCP client.
if [ -f install-id ]; then
    INSTALL_ID=$(tr -d '[:space:]' < install-id)
    info "reusing the existing installation id ${INSTALL_ID}"
else
    INSTALL_ID=$(openssl rand -hex 4)
    printf '%s\n' "${INSTALL_ID}" > install-id
    chmod 600 install-id
fi
if [ -f hostname ]; then
    PUBLIC_HOST=$(tr -d '[:space:]' < hostname)
    info "reusing the existing hostname ${PUBLIC_HOST}"
    EXPECTED_HOST="${INSTALL_ID}.${PUBLIC_IP//./-}.sslip.io"
    if [ "${PUBLIC_HOST}" != "${EXPECTED_HOST}" ]; then
        warn "the machine's public IP has changed. The URL stays ${PUBLIC_HOST}; delete ${INSTALL_DIR}/hostname and re-run to move it to ${EXPECTED_HOST}."
    fi
else
    PUBLIC_HOST="${INSTALL_ID}.${PUBLIC_IP//./-}.sslip.io"
    printf '%s\n' "${PUBLIC_HOST}" > hostname
    chmod 600 hostname
    info "hostname: ${PUBLIC_HOST}"
fi

# ---------------------------------------------------------------- secrets

CURRENT_STEP="generating secrets"
step "Preparing configuration"
secret() { openssl rand -base64 36 | tr -d '/+=\n' | cut -c1-32; }

read_env() { [ -f .env ] && sed -n "s/^$1=\(.*\)$/\1/p" .env | head -1; }

if [ -f .env ]; then
    info "keeping the secrets already in .env"
    SETUP_TOKEN=$(read_env SETUP_TOKEN)
else
    SETUP_TOKEN=$(openssl rand -hex 12)
    umask 077
    cat > .env <<ENV
# Generated by install.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ). Never commit this file.
EVOLUTION_API_KEY=$(secret)
EVOLUTION_DB_USER=evolution
EVOLUTION_DB_PASSWORD=$(secret)
EVOLUTION_AUTH_DB_NAME=evogo_auth
EVOLUTION_USERS_DB_NAME=evogo_users

RABBITMQ_USER=whatsapp
RABBITMQ_PASSWORD=$(secret)
RABBITMQ_VHOST=whatsapp

MINIO_ROOT_USER=whatsapp-media
MINIO_ROOT_PASSWORD=$(secret)
MINIO_BUCKET=evolution-media

MCP_DB_USER=whatsapp_mcp
MCP_DB_PASSWORD=$(secret)
MCP_DB_NAME=whatsapp_mcp

PUBLIC_URL=https://${PUBLIC_HOST}
PUBLIC_HOST=${PUBLIC_HOST}
LETSENCRYPT_EMAIL=admin@${PUBLIC_HOST}

# Guards the first-run form, which is otherwise open to whoever reaches this
# URL first. It stops meaning anything once an administrator exists.
SETUP_TOKEN=${SETUP_TOKEN}

FRESHNESS_WINDOW=5m
STATUS_POLL_INTERVAL=15s
EVOLUTION_TIMEOUT=5s
ENV
    umask 022
    info "generated .env with fresh secrets"
fi
chmod 600 .env
chown root:root .env install-id hostname 2>/dev/null || true

# The hostname may have been written before .env existed on a partial run.
grep -q "^PUBLIC_HOST=${PUBLIC_HOST}$" .env || sed -i "s|^PUBLIC_HOST=.*|PUBLIC_HOST=${PUBLIC_HOST}|" .env
grep -q "^PUBLIC_URL=https://${PUBLIC_HOST}$" .env || sed -i "s|^PUBLIC_URL=.*|PUBLIC_URL=https://${PUBLIC_HOST}|" .env

# ---------------------------------------------------------------- firewall

CURRENT_STEP="configuring the firewall"
step "Ports"
info "this stack publishes 80 and 443, and nothing else"
# The rules are registered so that enabling ufw later does not lock anyone out,
# but ufw is deliberately not enabled. Docker writes its own DNAT rules into
# the nat table, which is evaluated before ufw's filter rules, so ufw does not
# govern a published container port at all: `ufw deny 80` leaves 80 open. The
# only thing it would actually filter here is sshd — the one service you need.
# Enabling it would buy the appearance of a firewall and not the substance.
if command -v ufw >/dev/null 2>&1; then
    ufw allow 22/tcp  >/dev/null 2>&1 || true
    ufw allow 80/tcp  >/dev/null 2>&1 || true
    ufw allow 443/tcp >/dev/null 2>&1 || true
    if ufw status 2>/dev/null | head -1 | grep -q inactive; then
        info "ufw rules registered for 22, 80 and 443; ufw left inactive on purpose"
    else
        info "ufw: 22, 80 and 443 allowed"
    fi
fi
info "filter at your provider instead — it sits in front of the machine, where"
info "Docker cannot route around it"

# ---------------------------------------------------------------- DNS check

CURRENT_STEP="validating the hostname"
step "Validating ${PUBLIC_HOST}"
RESOLVED=$(getent ahostsv4 "${PUBLIC_HOST}" 2>/dev/null | awk 'NR==1 {print $1}' || true)
if [ -z "${RESOLVED}" ]; then
    fail "validating the hostname: ${PUBLIC_HOST} does not resolve. sslip.io may be unreachable from this machine, or a local resolver may be rewriting it."
fi
if [ "${RESOLVED}" != "${PUBLIC_IP}" ]; then
    fail "validating the hostname: ${PUBLIC_HOST} resolves to ${RESOLVED}, not to ${PUBLIC_IP}. Let's Encrypt would fail the HTTP challenge."
fi
info "${PUBLIC_HOST} resolves to ${PUBLIC_IP}"

if ss -lnt 2>/dev/null | awk '{print $4}' | grep -qE '(^|:)(80|443)$'; then
    if ! docker ps --format '{{.Names}}' 2>/dev/null | grep -q '^whatsapp-mcp-traefik'; then
        fail "validating the hostname: something other than this stack is already listening on port 80 or 443. Stop it and re-run."
    fi
fi

# ---------------------------------------------------------------- start

CURRENT_STEP="starting the stack"
step "Pulling images and starting the stack"
docker compose "${COMPOSE_FILES[@]}" up -d --pull missing </dev/null
info "containers are up"

CURRENT_STEP="waiting for the gateway"
step "Waiting for the gateway to answer"
for attempt in $(seq 1 60); do
    # </dev/null matters more than it looks: this script is normally piped into
    # bash, so stdin *is* the rest of the script, and `compose exec` forwards
    # stdin to the container — swallowing every line after this one and exiting
    # 0 as though the install had finished. Everything below simply never ran.
    if docker compose "${COMPOSE_FILES[@]}" exec -T whatsapp-mcp \
        wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1 </dev/null; then
        info "the gateway is healthy"
        break
    fi
    [ "${attempt}" -lt 60 ] || fail "waiting for the gateway: it never answered /healthz. Logs: whatsapp-mcp logs"
    sleep 3
done

CURRENT_STEP="waiting for the certificate"
step "Waiting for the TLS certificate"
CERT_OK=false
for attempt in $(seq 1 40); do
    if curl -fsS --max-time 8 "https://${PUBLIC_HOST}/healthz" >/dev/null 2>&1; then
        CERT_OK=true
        break
    fi
    sleep 5
done
if [ "${CERT_OK}" = true ]; then
    info "https://${PUBLIC_HOST} is serving a valid certificate"
else
    warn "the certificate is not ready yet. Let's Encrypt can take a few minutes; check with: whatsapp-mcp logs traefik"
fi

# ---------------------------------------------------------------- cli

CURRENT_STEP="installing the whatsapp-mcp command"
install -m 0755 deploy/whatsapp-mcp-cli.sh "${CLI_PATH}"
info "installed ${CLI_PATH}"

# ---------------------------------------------------------------- done

printf '\n'
printf '==================================================\n'
printf ' whatsapp-mcp installed successfully\n'
printf '==================================================\n\n'
printf 'URL:\n  https://%s\n\n' "${PUBLIC_HOST}"
if [ -n "${SETUP_TOKEN}" ]; then
    # This link is the one thing that matters on a fresh install, so it gets
    # the space. The token is in it, so there is nothing to copy by hand, and
    # it stops meaning anything the moment an administrator exists — which is
    # also why re-running the installer can print it again without being a way
    # in past the administrator you already created.
    printf 'Open this to create your administrator:\n\n'
    printf '  https://%s/setup?token=%s\n\n' "${PUBLIC_HOST}" "${SETUP_TOKEN}"
    printf 'You choose the username and the password. If an administrator already\n'
    printf 'exists, the link simply sends you to the sign-in page.\n\n'
fi
printf 'Installation directory:\n  %s\n\n' "${INSTALL_DIR}"
printf 'Useful commands:\n'
printf '  whatsapp-mcp status\n'
printf '  whatsapp-mcp logs\n'
printf '  whatsapp-mcp restart\n'
printf '  whatsapp-mcp update\n\n'
printf 'Ports: this stack needs 80 and 443 open, and nothing else. Keep your\n'
printf 'provider firewall (DigitalOcean Cloud Firewall, AWS security group, and\n'
printf 'so on) to 80, 443 and whichever port you reach SSH on. Never publish the\n'
printf 'databases, RabbitMQ, MinIO or Evolution.\n\n'
printf 'Next:\n'
printf '  1. Open the link above and create your administrator.\n'
printf '  2. Activate Evolution Go - it requires a licence and answers 503\n'
printf '     until then. See https://github.com/EvolutionAPI/evolution-go\n'
printf '  3. Create an instance and scan the QR code from WhatsApp under\n'
printf '     Linked devices -> Link a device.\n'
printf '  4. Generate an API key under "Conectar" and paste the block it\n'
printf '     gives you into your MCP client.\n'
printf '==================================================\n'
