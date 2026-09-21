#!/usr/bin/env bash
#
# WhatsApp MCP — update an installation made by install.sh.
#
#   curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/update.sh | sudo bash
#
# Moves an existing installation to the newest published release. Secrets, the
# hostname, the WhatsApp pairing and every indexed message are left alone: only
# the code and the images move. The gateway's database is dumped before
# anything starts, because a release may migrate the schema and a migration
# does not come back.
#
# REPO_REF pins what to move to — a tag for a specific release, `main` to
# follow the edge build. The default is the newest release tag.
set -euo pipefail

# Like install.sh, this is meant to be piped into bash, where stdin holds the
# rest of the script. Any command that reads stdin eats the remainder of the
# update and exits 0 — a failure indistinguishable from success. So every such
# command below gets </dev/null.

INSTALL_DIR="${INSTALL_DIR:-/opt/whatsapp-mcp}"
CLI_PATH="/usr/local/bin/whatsapp-mcp"
COMPOSE_FILES=(-f docker-compose.yml -f deploy/docker-compose.public.yml)
BACKUP_KEEP="${BACKUP_KEEP:-5}"
REPOSITORY_URL="https://github.com/BrOrlandi/whatsapp-mcp"

step()  { printf '\n\033[1;36m==>\033[0m %s\n' "$*"; }
info()  { printf '    %s\n' "$*"; }
warn()  { printf '\033[1;33m    warning:\033[0m %s\n' "$*" >&2; }
fail()  { printf '\n\033[1;31mUpdate failed at: %s\033[0m\n' "$*" >&2; exit 1; }

trap 'fail "${CURRENT_STEP:-an unknown step}"' ERR

# ---------------------------------------------------------------- preflight

CURRENT_STEP="checking privileges"
step "Checking the installation"
[ "$(id -u)" -eq 0 ] || fail "checking privileges: run this with sudo"

CURRENT_STEP="finding the installation"
# Updating is not installing. An installation that is not there means either
# the wrong machine or the wrong directory, and guessing at either would be
# worse than stopping: install.sh is the command for a machine with nothing on
# it, and it is the one that knows how to generate secrets and pick a hostname.
[ -d "${INSTALL_DIR}/.git" ] || fail "finding the installation: there is no whatsapp-mcp at ${INSTALL_DIR}. To install one, run install.sh instead; to update one somewhere else, set INSTALL_DIR."
cd "${INSTALL_DIR}"
[ -f .env ] || fail "finding the installation: ${INSTALL_DIR}/.env is missing, so this is not a finished installation. Re-run install.sh."
command -v docker >/dev/null 2>&1 || fail "finding the installation: docker is not installed on this machine"
info "installation at ${INSTALL_DIR}"

# ---------------------------------------------------------------- versions

CURRENT_STEP="reading the current version"
step "Reading the current version"
read_env() { sed -n "s/^$1=\(.*\)$/\1/p" .env | head -1; }

# What is running is what the container reports, not what the checkout says:
# the two differ whenever an image was pulled without the source moving, or the
# other way round. The checkout is the fallback for a stack that is stopped.
running_version() {
    local reported
    reported=$(docker compose "${COMPOSE_FILES[@]}" exec -T whatsapp-mcp \
        wget -qO- http://127.0.0.1:8080/healthz 2>/dev/null </dev/null \
        | sed -n 's/.*"version" *: *"\([^"]*\)".*/\1/p' | head -1) || true
    if [ -n "${reported}" ]; then
        printf '%s' "${reported}"
        return
    fi
    reported=$(git describe --tags --always 2>/dev/null | sed 's/^v//') || true
    printf '%s' "${reported:-unknown}"
}
FROM_VERSION=$(running_version)
FROM_COMMIT=$(git rev-parse --short HEAD)
# The image tag in use right now, which is what a rollback has to name. It is
# not the same string as the version: a build past a tag reports
# "0.1.0-beta-12-gabc1234" and no such image was ever published.
FROM_IMAGE_TAG=$(read_env WHATSAPP_MCP_TAG)
: "${FROM_IMAGE_TAG:=edge}"
info "running ${FROM_VERSION} (${FROM_COMMIT}), image tag ${FROM_IMAGE_TAG}"

CURRENT_STEP="finding the release to move to"
step "Finding the release to move to"
# Tags are what this fetches for: a --depth 1 clone has none, and without them
# there is no release to name. --force lets a moved tag be corrected rather
# than failing the whole update.
git fetch --quiet --tags --force origin
# A checkout sitting on a release tag is detached, which is every installation
# that has been updated once. main is the right answer for it: that is the
# branch releases are tagged on, and following a different one is a developer's
# choice, made with REPO_REF rather than inherited.
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo main)
case "${BRANCH}" in HEAD|"") BRANCH=main ;; esac

if [ -n "${REPO_REF:-}" ]; then
    TARGET_REF="${REPO_REF}"
    info "moving to ${TARGET_REF}, as REPO_REF asks"
else
    # The newest tag reachable from the branch's own history, which is what a
    # release is: a name put on a commit that is already on the branch. Sorting
    # tag names would need semver-aware rules that git only half has; asking
    # the history needs none.
    TARGET_REF=$(git describe --tags --abbrev=0 "origin/${BRANCH}" 2>/dev/null || true)
    if [ -z "${TARGET_REF}" ]; then
        warn "no release has been tagged yet; following origin/${BRANCH} instead"
        TARGET_REF="origin/${BRANCH}"
    fi
fi
git rev-parse --quiet --verify "${TARGET_REF}^{commit}" >/dev/null \
    || fail "finding the release to move to: ${TARGET_REF} is not a commit, tag or branch in this repository"
TO_COMMIT=$(git rev-parse --short "${TARGET_REF}^{commit}")
TO_VERSION=$(printf '%s' "${TARGET_REF#origin/}" | sed 's/^v//')
info "target ${TARGET_REF} (${TO_COMMIT})"

# An update must never move an instance backwards. install.sh installs the head
# of main, which runs ahead of the newest release for as long as main has
# commits the release does not; aiming at that release from there would be a
# downgrade wearing the word "update" — with migrations already applied, and no
# way back but the dump. Being asked for a specific ref is different: that is
# someone naming what they want, and they get it.
if [ -z "${REPO_REF:-}" ] && [ "${FORCE:-}" != "true" ] \
   && git merge-base --is-ancestor "${TARGET_REF}^{commit}" HEAD 2>/dev/null; then
    if [ "${TO_COMMIT}" = "${FROM_COMMIT}" ]; then
        printf '\nAlready on %s (%s). Nothing to do.\n' "${FROM_VERSION}" "${FROM_COMMIT}"
    else
        printf '\nThis instance is at %s (%s), which is already ahead of the newest\n' "${FROM_VERSION}" "${FROM_COMMIT}"
        printf 'release, %s. Nothing to update to.\n' "${TARGET_REF}"
        printf '\nThat is what an installation following main looks like between releases.\n'
        printf 'To follow main from here on:  REPO_REF=main\n'
    fi
    printf 'To move anyway, naming what you want:  REPO_REF=%s\n' "${TARGET_REF}"
    exit 0
fi

# ---------------------------------------------------------------- backup

CURRENT_STEP="backing up the database"
step "Backing up the gateway database"
# This is the step that makes the update reversible, and it is why there is no
# downgrade path: a release may migrate the schema, migrations here only run
# forward, and this dump is the only way back to the shape the old version
# understood. It holds message text, so it is treated like the phone.
MCP_DB_USER=$(read_env MCP_DB_USER)
MCP_DB_NAME=$(read_env MCP_DB_NAME)
: "${MCP_DB_NAME:=whatsapp_mcp}"
if [ -z "${MCP_DB_USER}" ]; then
    fail "backing up the database: MCP_DB_USER is not in .env, so the dump cannot be taken. Nothing has changed."
fi
mkdir -p backups
chmod 700 backups
BACKUP_FILE="backups/whatsapp-mcp_${FROM_VERSION}_$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
if docker compose "${COMPOSE_FILES[@]}" ps --status running --services 2>/dev/null </dev/null | grep -qx postgres-mcp; then
    umask 077
    if docker compose "${COMPOSE_FILES[@]}" exec -T postgres-mcp \
        pg_dump -U "${MCP_DB_USER}" "${MCP_DB_NAME}" </dev/null 2>/dev/null | gzip > "${BACKUP_FILE}"; then
        umask 022
        chmod 600 "${BACKUP_FILE}"
        info "wrote ${INSTALL_DIR}/${BACKUP_FILE} ($(du -h "${BACKUP_FILE}" | cut -f1))"
        info "it contains message text — keep it as private as the phone"
    else
        umask 022
        rm -f "${BACKUP_FILE}"
        fail "backing up the database: pg_dump failed, so the update stops here rather than migrating a schema with no way back. Check: whatsapp-mcp logs postgres-mcp"
    fi
    # Old dumps are deleted last, so a failure above never costs the previous
    # backup as well as this one.
    ls -1t backups/*.sql.gz 2>/dev/null | tail -n "+$((BACKUP_KEEP + 1))" | xargs -r rm -f
else
    BACKUP_FILE=""
    warn "the database container is not running, so there is nothing to dump. Continuing: a stopped stack has no schema in flight."
fi

# ---------------------------------------------------------------- sources

CURRENT_STEP="updating the source"
step "Updating the source"
# The working tree is the operator's; a local edit is theirs to keep, and this
# refuses rather than throwing it away. .env, hostname, install-id and backups
# are ignored by the repository, so they never count as changes here.
if ! git diff --quiet HEAD -- 2>/dev/null; then
    fail "updating the source: ${INSTALL_DIR} has local changes. Commit or discard them (git -C ${INSTALL_DIR} checkout -- .) and run this again. Your backup is at ${BACKUP_FILE:-none taken}."
fi
git checkout --quiet --detach "${TARGET_REF}^{commit}"
info "checked out ${TO_COMMIT}"

CURRENT_STEP="pinning the image"
# Compose defaults to `edge`, which follows main. An installation that has just
# been moved to a release tag must run that release's image, not whatever main
# built last night.
if [ "${TARGET_REF}" = "origin/${BRANCH}" ] || [ "${TARGET_REF}" = "${BRANCH}" ]; then
    IMAGE_TAG="edge"
else
    IMAGE_TAG="${TO_VERSION}"
fi
if grep -q "^WHATSAPP_MCP_TAG=" .env; then
    sed -i "s|^WHATSAPP_MCP_TAG=.*|WHATSAPP_MCP_TAG=${IMAGE_TAG}|" .env
else
    printf 'WHATSAPP_MCP_TAG=%s\n' "${IMAGE_TAG}" >> .env
fi
info "image pinned to ghcr.io/brorlandi/whatsapp-mcp:${IMAGE_TAG}"

# ---------------------------------------------------------------- start

CURRENT_STEP="pulling images and restarting"
step "Pulling images and restarting"
docker compose "${COMPOSE_FILES[@]}" up -d --pull always --remove-orphans </dev/null
info "containers are up"

CURRENT_STEP="installing the whatsapp-mcp command"
# The wrapper changes between versions like anything else, so it is reinstalled
# rather than left at whichever version happened to install it.
install -m 0755 deploy/whatsapp-mcp-cli.sh "${CLI_PATH}"
info "refreshed ${CLI_PATH}"

CURRENT_STEP="waiting for the gateway"
step "Waiting for the gateway to answer"
HEALTHY=false
for attempt in $(seq 1 60); do
    if docker compose "${COMPOSE_FILES[@]}" exec -T whatsapp-mcp \
        wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1 </dev/null; then
        HEALTHY=true
        break
    fi
    sleep 3
done

if [ "${HEALTHY}" != true ]; then
    # Rolling back on its own is the wrong move here: the new version has
    # already started, which means its migrations have already run, and putting
    # the old code back over a migrated schema breaks it in a second way.
    # Restoring the dump is a decision with data loss in it — every message
    # since the backup — and that decision is the operator's.
    printf '\n\033[1;31mThe gateway did not answer /healthz after the update.\033[0m\n\n' >&2
    docker compose "${COMPOSE_FILES[@]}" logs --tail 40 whatsapp-mcp >&2 </dev/null || true
    printf '\nThe stack is on %s. To go back to %s:\n\n' "${TO_VERSION}" "${FROM_VERSION}" >&2
    printf '  cd %s\n' "${INSTALL_DIR}" >&2
    printf '  git checkout --detach %s\n' "${FROM_COMMIT}" >&2
    if [ -n "${BACKUP_FILE}" ]; then
        printf '  sed -i "s|^WHATSAPP_MCP_TAG=.*|WHATSAPP_MCP_TAG=%s|" .env\n' "${FROM_IMAGE_TAG}" >&2
        printf '  docker compose -f docker-compose.yml -f deploy/docker-compose.public.yml up -d --pull always\n' >&2
        printf '\nThat puts the old code back. If %s migrated the schema, the old\n' "${TO_VERSION}" >&2
        printf 'code will not read it, and the database has to be restored too --\n' >&2
        printf 'which discards every message received since the backup:\n\n' >&2
        printf '  gunzip -c %s | docker compose -f docker-compose.yml -f deploy/docker-compose.public.yml exec -T postgres-mcp psql -U %s %s\n\n' \
            "${BACKUP_FILE}" "${MCP_DB_USER}" "${MCP_DB_NAME}" >&2
    fi
    exit 1
fi
info "the gateway is healthy"

NEW_VERSION=$(running_version)

# ---------------------------------------------------------------- done

printf '\n'
printf '==================================================\n'
printf ' whatsapp-mcp updated\n'
printf '==================================================\n\n'
printf '  %s  ->  %s\n\n' "${FROM_VERSION}" "${NEW_VERSION}"
if [ -n "${BACKUP_FILE}" ]; then
    printf 'Backup taken before the update:\n  %s/%s\n\n' "${INSTALL_DIR}" "${BACKUP_FILE}"
fi
printf 'What changed:\n  %s/blob/main/CHANGELOG.md\n\n' "${REPOSITORY_URL}"
if [ -f hostname ]; then
    printf 'Panel:\n  https://%s\n\n' "$(cat hostname)"
fi
printf 'There is no downgrade. A release may migrate the schema, and migrations\n'
printf 'only run forward; the way back is the backup above.\n'
printf '==================================================\n'
