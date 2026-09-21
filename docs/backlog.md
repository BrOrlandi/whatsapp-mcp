# Backlog de funcionalidades

Funcionalidades acordadas para depois do MVP do MCP remoto. Nenhuma delas deve
atrasar a entrega descrita em `remote-mcp-auth-pending.md`; elas estão aqui para
que as decisões de arquitetura do MVP não fechem portas desnecessariamente.

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

## 5. Versionamento e atualização da instância

Issue: a criar.

Hoje uma instância instalada não sabe dizer o que está rodando, e quem a
mantém não tem um comando único para movê-la adiante. `whatsapp-mcp update`
existe, mas atualiza para "o topo do branch", que não é um nome que alguém
possa pedir, conferir ou repetir. Esta pendência fecha as duas pontas: dar um
número à versão e dar um comando que leve a instância até ela.

A primeira versão nomeada é **0.1.0-beta**. Beta é a declaração honesta do
estado: o esquema do banco ainda pode mudar entre versões, e é isso que impede
de chamar de 1.0.

### 5.1 De onde vem o número

A tag git é a fonte da verdade; nada de arquivo `VERSION` no repositório, que
só cria uma segunda verdade para divergir da primeira. O que o binário carrega
é o resultado de `git describe --tags --always --dirty`, passado ao
`-X main.version` que o `Dockerfile` e o `release.yml` já usam. Isso dá três
formas úteis sem inventar nenhum mecanismo novo:

- `0.1.0-beta` — exatamente na tag, o que um release publica.
- `0.1.0-beta-12-gabc1234` — doze commits depois dela, que é o que a imagem
  `edge` deve dizer de si mesma em vez do sha cru que diz hoje.
- `dev` — build local sem git.

O `release.yml` precisa de dois ajustes para a série beta. O `checkout@v4` vem
raso por padrão e `git describe` não enxerga tag nenhuma sem `fetch-depth: 0`
e `fetch-tags: true`. E o `flavor: latest=` atual move `latest` para qualquer
tag `v*`, o que faria uma pré-release virar o padrão de quem não fixa tag:
durante a beta, `latest` só deve andar com uma decisão explícita, não como
efeito colateral de publicar `v0.1.0-beta`. Confirmar também o que o
`docker/metadata-action` faz com `{{major}}.{{minor}}` numa pré-release, que
ele costuma pular.

`CHANGELOG.md` na raiz, escrito a partir dos commits desde a tag anterior, é o
que transforma um número em informação. Sem ele, "0.2.0" não diz a ninguém se
vale a pena atualizar.

### 5.2 Onde o número aparece

- No painel, no rodapé (`{{define "foot"}}` em `internal/httpapi/templates.go`,
  ao lado do link do repositório), porque é a única superfície que o operador
  olha todo dia.
- Em `/healthz` e `/readyz`, num campo `version`, para que um monitor externo
  consiga responder "qual versão está rodando" sem abrir o painel.
- Em `whatsapp-mcp version`, que hoje imprime o sha curto e passa a imprimir o
  mesmo `git describe`.

A versão precisa chegar ao `httpapi` como configuração, e não como variável
global do pacote `main`: hoje ela mora em `cmd/whatsapp-mcp/main.go` e nada
abaixo dela a enxerga.

Um passo adiante, com custo próprio: o painel consultar a última release
publicada no GitHub e mostrar um aviso quando a instância estiver atrás, com o
comando de atualização pronto para copiar. Isso adiciona uma chamada externa a
partir do servidor de alguém, então é opção de configuração, com falha
silenciosa e cache, nunca uma dependência da página carregar.

### 5.3 O comando de atualização

O alvo é um comando que se manda para alguém por mensagem e que a pessoa cola
na instância dela:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/update.sh | sudo bash
```

Simétrico ao `install.sh`, e com as mesmas regras que já valem lá: ler o
`stdin` é fatal num script que vem por pipe, então todo comando que lê stdin
leva `</dev/null`; nada de segredo é regenerado; `.env`, `hostname` e os
volumes são intocados.

O que ele faz, na ordem:

1. Encontra a instalação (`/opt/whatsapp-mcp`, sobrescritível por
   `INSTALL_DIR`) e recusa-se a fazer qualquer coisa se ela não existir —
   atualizar não é instalar, e adivinhar aqui seria pior do que falhar.
2. Registra a versão atual, para poder dizer de onde para onde foi, e para o
   rollback saber a que voltar.
3. Faz backup do PostgreSQL do gateway (`pg_dump`) antes de qualquer migração,
   com o nome carimbado pela versão de origem. Migração de esquema é a parte
   irreversível de uma atualização; é ela que justifica o backup obrigatório e
   não opcional.
4. `git fetch` e checkout da referência pedida: por padrão a última tag de
   release, e não o topo do branch. `REPO_REF=main` continua disponível para
   quem quer o `edge`. Fixar em tag é o que torna a atualização repetível e
   auditável.
5. `docker compose ... up -d --pull always`, com os mesmos dois arquivos de
   compose que o instalador usa, e `WHATSAPP_MCP_TAG` alinhado à referência
   escolhida — hoje o compose fica em `edge` mesmo numa instalação que se
   pretende estável.
6. Reinstala `/usr/local/bin/whatsapp-mcp` a partir de
   `deploy/whatsapp-mcp-cli.sh`, porque o próprio CLI muda entre versões.
7. Espera `/healthz` responder, com prazo. Se não responder, imprime as últimas
   linhas do log do gateway e diz, em uma linha só, como voltar à versão
   anterior. Rollback automático fica de fora: com migração já aplicada ele
   restauraria um banco à revelia do operador, o que é pior do que parar e
   avisar.
8. Imprime `0.1.0-beta → 0.2.0` e o link do CHANGELOG.

`whatsapp-mcp update` passa a ser uma chamada para esse mesmo script, não uma
segunda implementação. Duas lógicas de atualização divergem na primeira
correção que só uma das duas receber.

Uma regra que precisa estar escrita antes de existirem duas versões no mundo:
**não há downgrade**. Uma migração aplicada não volta. O caminho de volta é
restaurar o backup do passo 3, e é isso que a mensagem de falha deve dizer.

### 5.4 A variante por agente

Além do comando colado na máquina, o outro caminho é uma prompt que a pessoa
cola num Claude Code com acesso SSH à instância dela. Vale a pena porque é o
caminho que diagnostica: quando a atualização falha, alguém precisa ler log,
e uma prompt pode fazer isso onde um script só pode desistir.

O desenho precisa ser conservador. A prompt não improvisa a atualização: ela
executa o `update.sh`, e só entra em investigação se ele falhar. O que ela
acrescenta é ler o log, dizer o que quebrou, e propor — nunca executar sozinha
— uma restauração do backup. Um agente com acesso root à instância de produção
de alguém é exatamente o lugar onde uma ação destrutiva pede confirmação
explícita.

Ela vive versionada no repositório, em `docs/updating.md`, junto com o
one-liner, para que as duas formas contem a mesma história e envelheçam juntas.

### 5.5 O que documentar

`docs/self-hosting.md` tem uma seção "Upgrading" de cinco linhas que ensina
`git pull && docker compose up -d`. Ela passa a apontar para o comando novo. A
seção de backup ganha a ligação com o passo 3, porque é o mesmo dump, e é a
mesma coisa que salva o operador nos dois casos.
