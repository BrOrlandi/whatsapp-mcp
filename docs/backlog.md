# Backlog de funcionalidades

Funcionalidades acordadas para depois do MVP do MCP remoto. Nenhuma delas deve
atrasar esse MVP; elas estão aqui para que as decisões de arquitetura não
fechem portas desnecessariamente.

A referência de produto para as três é o projeto `whatsapp-manager`, que já
resolve parte delas sobre a Evolution API v2 com Node.js, BullMQ e PocketBase.
Aqui a implementação será em Go, sobre a Evolution Go e o PostgreSQL que o
gateway já mantém.

## 1. Transcrição de áudio

Issue: https://github.com/BrOrlandi/whatsapp-mcp/issues/1

Transcrever mensagens de voz sob demanda, expondo o texto às ferramentas de
leitura e à busca textual.

- A Evolution já entrega o áudio em base64 por uma rota de mídia; o gateway
  envia esse conteúdo a uma API externa de transcrição (Whisper ou equivalente)
  e persiste o resultado.
- A chave da API de transcrição é uma variável de ambiente do backend, como
  todas as demais credenciais internas. Ela nunca aparece na configuração do
  cliente MCP.
- O texto transcrito entra no mesmo índice de busca das mensagens de texto, de
  forma que `search_messages` passe a encontrar conteúdo falado.
- Transcrição custa dinheiro por minuto de áudio: decidir se roda
  automaticamente na ingestão, sob demanda por ferramenta, ou ambos com um
  limite configurável.

## 2. Agendamento de mensagens

Issue: https://github.com/BrOrlandi/whatsapp-mcp/issues/2

Programar o envio de uma mensagem para um horário futuro, incluindo recorrência.

- Depende das ferramentas de envio do MVP (`send_text_message`,
  `send_media_message`) já estarem prontas e validadas.
- O agendamento é estado durável: a tabela vive no PostgreSQL do gateway e o
  agendador reprograma tudo que estiver pendente ao subir o processo, para que
  um restart não perca envios.
- Um envio agendado precisa ser cancelável e listável antes de disparar.
- Regra de segurança que vale desde o desenho: o agendamento só pode ser criado
  por instrução explícita do usuário. Conteúdo de mensagem recebida é dado não
  confiável e nunca origina um envio.

## 3. Monitoramento de palavras-chave

Issue: https://github.com/BrOrlandi/whatsapp-mcp/issues/3

Observar o fluxo de mensagens que já chega pelo RabbitMQ e alertar quando um
termo configurado for mencionado em uma conversa ou grupo.

- É a funcionalidade que melhor aproveita a ingestão existente: o consumidor já
  vê cada mensagem antes de persistir, então a avaliação das regras acontece no
  mesmo ponto, sem varredura periódica do banco.
- As regras (termo, escopo de conversa/grupo, sensibilidade a maiúsculas) ficam
  no PostgreSQL e são gerenciadas pelo painel.
- Definir o canal de notificação. As opções são uma ferramenta MCP que lista
  alertas acumulados desde a última consulta, um webhook de saída, ou ambos. A
  primeira é a mais simples e não adiciona dependência externa.

## 4. Retenção de dados

Issue: https://github.com/BrOrlandi/whatsapp-mcp/issues/4

O banco cresce sem limite no MVP. A limpeza fica para quando houver volume real
para dimensionar o teto.

- Rotina diária que apaga dados históricos até o banco voltar abaixo de um
  limite configurado.
- A regra que não pode ser violada: **apagar apenas mensagens, nunca as
  conversas**. A lista de conversas, com nome, JID e data da última atividade, é
  o que permite ao usuário saber que uma conversa existe mesmo quando o conteúdo
  antigo já saiu. Perder isso transformaria a limpeza em perda de contexto, e não
  em liberação de espaço.
- Isso implica separar a identidade da conversa do conteúdo das mensagens no
  esquema, de modo que apagar as segundas não apague a primeira. O esquema atual
  deriva a conversa do `chat_jid` de cada mensagem, então uma tabela própria de
  conversas passa a ser necessária antes de a limpeza existir.
- Apagar também o payload bruto em `events` correspondente às mensagens
  removidas, que é onde está a maior parte do volume.
- A data da mensagem mais antiga retida alimenta `history_since`, já exposto
  pelas ferramentas de leitura desde o MVP.

## Nota sobre o armazenamento

As três cabem no PostgreSQL que o gateway já opera, e nenhuma justifica um
banco adicional. SQLite foi cogitado por simplicidade, mas o processo já abre
uma conexão com o PostgreSQL para a ingestão e a busca textual; um segundo
mecanismo de persistência adicionaria uma superfície de backup e migração sem
resolver nada que o primeiro não resolva.

## 5. CI que roda os testes

Issue: a criar.

Hoje o único workflow é o `release.yml`, que publica imagem e binários. Nada
roda `go vet`, `go test`, `-race`, `go build` ou `docker compose config` num
pull request — e o `CONTRIBUTING.md` diz "Run what CI would run", o que não é
verdade enquanto isso não existir.

O conteúdo já está definido: é o `just check`. Falta um `ci.yml` disparado em
`pull_request` e em `push` para `main`, com `actions/setup-go` lendo a versão
do `go.mod` (como o release já faz) e cache de módulos.

Importante para abrir o projeto: PR de terceiro hoje entra sem nenhuma
checagem automática, e a revisão manual vira o único portão.
