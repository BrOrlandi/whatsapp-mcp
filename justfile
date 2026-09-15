# WhatsApp MCP — tarefas de desenvolvimento local.
#
# Três modos de rodar, do mais simples ao mais completo:
#
#   just preview      painel com dados falsos, sem nenhuma dependência
#   just dev-remote   gateway local usando a Evolution do servidor (via túnel)
#   just dev          stack inteira local, com WhatsApp próprio para parear
#
# `just` sem argumentos lista tudo.

set dotenv-load := true
set positional-arguments

compose := "docker compose -f docker-compose.yml -f docker-compose.dev.yml"

# Porta em que o gateway sobe localmente.
port := env_var_or_default("DEV_PORT", "8080")
# Porta local da Evolution: 4000 quando ela roda aqui, 4001 quando vem por túnel.
evolution_port := env_var_or_default("EVOLUTION_PORT", "4000")
tunnel_port := env_var_or_default("TUNNEL_PORT", "4001")
db_port := env_var_or_default("MCP_DB_PORT", "5433")
queue_port := env_var_or_default("RABBITMQ_PORT", "5672")

# Lista as tarefas disponíveis.
default:
    @just --list --unsorted

# ---------------------------------------------------------------- preparação

# Cria um .env local com segredos aleatórios, se ainda não existir.
env:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -f .env ]; then
      echo ".env já existe — nada a fazer."
      exit 0
    fi
    secret() { openssl rand -hex 24; }
    cat > .env <<ENVFILE
    # Gerado por \`just env\`. Local apenas — nunca commite este arquivo.
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

    PUBLIC_URL=http://127.0.0.1:{{port}}
    FRESHNESS_WINDOW=5m
    STATUS_POLL_INTERVAL=15s
    EVOLUTION_TIMEOUT=5s
    ENVFILE
    echo ".env criado com segredos aleatórios."

# ------------------------------------------------------------------- serviços

# Sobe as dependências em Docker (Postgres, RabbitMQ, Evolution, MinIO).
up: env
    {{compose}} up -d postgres-mcp rabbitmq postgres-evolution minio evolution-go
    @echo
    @just _wait

# Sobe só Postgres e RabbitMQ — para usar com a Evolution do servidor.
up-lite: env
    {{compose}} up -d postgres-mcp rabbitmq
    @echo
    @just _wait

# Espera as dependências ficarem saudáveis.
_wait:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Aguardando as dependências…"
    for _ in $(seq 1 60); do
      if {{compose}} ps --format '{{{{.Health}}}}' 2>/dev/null | grep -qv '^healthy$' ; then sleep 2; else break; fi
    done
    {{compose}} ps

# Derruba as dependências, preservando os volumes.
down:
    {{compose}} down

# Derruba tudo e apaga os volumes. Perde o pareamento e as mensagens indexadas.
reset:
    {{compose}} down -v

# Acompanha os logs das dependências.
logs *services:
    {{compose}} logs -f {{services}}

# ---------------------------------------------------------------- execução

# Roda o gateway contra a stack local. Precisa de `just up` antes.
dev:
    #!/usr/bin/env bash
    set -euo pipefail
    export LISTEN_ADDR=":{{port}}"
    export PUBLIC_URL="http://127.0.0.1:{{port}}"
    export DATABASE_URL="postgres://${MCP_DB_USER}:${MCP_DB_PASSWORD}@127.0.0.1:{{db_port}}/${MCP_DB_NAME}?sslmode=disable"
    export RABBITMQ_URL="amqp://${RABBITMQ_USER}:${RABBITMQ_PASSWORD}@127.0.0.1:{{queue_port}}/${RABBITMQ_VHOST}"
    export EVOLUTION_URL="http://127.0.0.1:{{evolution_port}}"
    echo "Painel em http://127.0.0.1:{{port}}  ·  MCP em http://127.0.0.1:{{port}}/mcp"
    go run ./cmd/whatsapp-mcp

# Roda o gateway usando a Evolution do servidor, pelo túnel.
dev-remote:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! curl -fsS -m 3 "http://127.0.0.1:{{tunnel_port}}/swagger/index.html" >/dev/null 2>&1; then
      echo "O túnel não está aberto na porta {{tunnel_port}}." >&2
      echo "Abra em outro terminal: just tunnel" >&2
      exit 1
    fi
    export LISTEN_ADDR=":{{port}}"
    export PUBLIC_URL="http://127.0.0.1:{{port}}"
    export DATABASE_URL="postgres://${MCP_DB_USER}:${MCP_DB_PASSWORD}@127.0.0.1:{{db_port}}/${MCP_DB_NAME}?sslmode=disable"
    export RABBITMQ_URL="amqp://${RABBITMQ_USER}:${RABBITMQ_PASSWORD}@127.0.0.1:{{queue_port}}/${RABBITMQ_VHOST}"
    export EVOLUTION_URL="http://127.0.0.1:{{tunnel_port}}"
    export EVOLUTION_API_KEY="${EVOLUTION_REMOTE_API_KEY:-$EVOLUTION_API_KEY}"
    # Leituras ao vivo e envio funcionam; a ingestão não. A Evolution do
    # servidor publica na fila do servidor, não nesta, então o índice local
    # fica vazio. Para mexer em ingestão, use `just dev` com a stack completa.
    echo "Usando a Evolution do servidor: leituras ao vivo e envio funcionam."
    echo "A ingestão de mensagens fica parada — a Evolution publica na fila do servidor."
    echo "Painel em http://127.0.0.1:{{port}}"
    go run ./cmd/whatsapp-mcp

# Abre o túnel SSH até a Evolution do servidor. Deixe rodando em outro terminal.
tunnel host="ubuntu@example.com":
    ./scripts/evolution-tunnel.sh {{host}} {{tunnel_port}}

# Sobe só o painel, com dados falsos: para mexer em layout e texto.
preview:
    @echo "Painel de demonstração em http://127.0.0.1:8090  (usuário: admin / senha: senha segura 123)"
    go run ./cmd/panel-preview

# ------------------------------------------------------------------- inspeção

# Mostra o estado do gateway local.
health:
    @curl -fsS "http://127.0.0.1:{{port}}/healthz" | python3 -m json.tool

# Chama o MCP local: `just mcp` ou `just mcp tools/call whatsapp_status`.
mcp method="tools/list" tool="":
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -z "${WHATSAPP_MCP_KEY:-}" ]; then
      echo "Defina WHATSAPP_MCP_KEY com uma chave gerada no painel." >&2
      exit 1
    fi
    if [ -n "{{tool}}" ]; then
      params='{"name":"{{tool}}","arguments":{}}'
    else
      params='{}'
    fi
    curl -fsS -X POST "http://127.0.0.1:{{port}}/mcp" \
      -H "Authorization: Bearer ${WHATSAPP_MCP_KEY}" \
      -H 'Content-Type: application/json' \
      -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"{{method}}\",\"params\":${params}}" \
      | python3 -m json.tool

# Abre um psql no banco do gateway.
psql:
    {{compose}} exec postgres-mcp psql -U "${MCP_DB_USER}" -d "${MCP_DB_NAME}"

# ------------------------------------------------------------------ qualidade

# Formata o código.
fmt:
    gofmt -w cmd internal

# Roda os testes.
test:
    go test ./...

# Roda os testes com o detector de corrida.
race:
    go test -race ./...

# Compila o binário em ./bin.
build:
    go build -o ./bin/whatsapp-mcp ./cmd/whatsapp-mcp

# Tudo que precisa passar antes de um commit.
check: fmt
    go vet ./...
    go test ./...
    go test -race ./...
    go build ./cmd/whatsapp-mcp
    {{compose}} config >/dev/null
    git diff --check
    @echo "Tudo verde."
