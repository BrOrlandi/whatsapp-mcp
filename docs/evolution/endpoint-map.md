# Mapa de endpoints da Evolution Go 0.7.2

Levantado a partir de `swagger.yaml` neste diretório e da documentação oficial
em `https://docs.evolutionfoundation.com.br/evolution-go/`. Este documento
existe para que o desenho das ferramentas MCP parta do que a Evolution Go
realmente oferece, e não do que a Evolution API v2 oferecia.

## Autenticação e seleção de instância

A autenticação é feita pelo header `apikey`. A documentação descreve a chave
como "global ou específica da instância", e nenhuma rota de envio, chat, grupo
ou usuário aceita parâmetro de instância — nem no caminho, nem na query, nem no
corpo.

A consequência é que **a instância é selecionada pela própria chave usada na
requisição**. A chave global serve para as rotas de administração
(`/instance/all`, `/instance/create`); as operações do dia a dia usam o token da
instância, que é definido no campo `token` de `POST /instance/create` ou
atribuído pela Evolution na criação.

O backend guarda esses tokens e nunca os expõe ao cliente MCP.

O `swagger.yaml` gerado pelo swaggo não declara `securityDefinitions` nem o
header `apikey` nas rotas. Isso é omissão da geração, não ausência do
requisito.

## Descoberta central: não existe leitura de histórico

A Evolution Go **não expõe rota para listar conversas nem para buscar
mensagens**. Não há equivalente de `/chat/findChats`, `/chat/findMessages` ou
`/chat/findContacts` da Evolution API v2, que o projeto de referência
`whatsapp-manager` utiliza. As únicas rotas que devolvem mensagens são
`/newsletter/messages`, restrita a newsletters, e `POST /chat/history-sync`, que
não devolve nada de forma síncrona: ela pede ao WhatsApp um sincronismo de
histórico, cujo resultado chega como eventos.

Isso inverte o papel do PostgreSQL no projeto. Ele deixa de ser um índice
opcional para busca textual e passa a ser a **única** fonte capaz de listar
conversas e ler histórico. A ingestão por RabbitMQ é o mecanismo central da
arquitetura, não um complemento.

`POST /chat/history-sync` é o caminho para preencher o histórico anterior à
subida do stack. Aceita `count` e `messageInfo`, e o resultado chega pela fila
como qualquer outro evento.

## Não existe encaminhamento de mensagem

Não há rota de forward. O que existe é o campo `forwardingScore` no corpo de
`/send/text` e das demais rotas de envio, que marca visualmente a mensagem como
encaminhada. Encaminhar, na prática, é reenviar o conteúdo com esse campo
preenchido — o que não preserva a identidade da mensagem original e não funciona
para mídia sem baixar e reenviar o arquivo.

## O que a Evolution Go oferece ao vivo

### Instância

| Rota | Método | Uso no projeto |
|---|---|---|
| `/instance/all` | GET | listar instâncias no painel |
| `/instance/create` | POST | criar instância pelo painel |
| `/instance/qr` | GET | exibir QR de pareamento no painel |
| `/instance/pair` | POST | pareamento por código, alternativa ao QR |
| `/instance/connect` | POST | conectar |
| `/instance/disconnect` | POST | desconectar |
| `/instance/reconnect` | POST | reconectar |
| `/instance/forcereconnect/{instanceId}` | POST | recuperação |
| `/instance/status` | GET | estado da conexão |
| `/instance/info/{instanceId}` | GET | detalhes |
| `/instance/logout` | DELETE | encerrar sessão do WhatsApp |
| `/instance/delete/{instanceId}` | DELETE | remover instância |
| `/instance/logs/{instanceId}` | GET | diagnóstico |
| `/instance/{instanceId}/advanced-settings` | GET, PUT | configuração |

Esse conjunto cobre toda a gestão de instância, o que torna viável o painel
próprio sem nenhum acesso ao Manager da Evolution.

### Contatos e usuários

| Rota | Método | Uso |
|---|---|---|
| `/user/contacts` | GET | listar contatos |
| `/user/info` | POST | dados de um contato |
| `/user/check` | POST | verificar se um número tem WhatsApp |
| `/user/avatar` | POST | foto de um contato |
| `/user/blocklist` | GET | bloqueados |
| `/user/block`, `/user/unblock` | POST | bloquear e desbloquear |
| `/user/privacy` | GET, POST | privacidade |
| `/user/profileName`, `/user/profilePicture`, `/user/profileStatus` | POST | perfil próprio |

### Grupos

| Rota | Método | Uso |
|---|---|---|
| `/group/list` | GET | listar grupos |
| `/group/myall` | GET | grupos do usuário |
| `/group/info` | POST | dados do grupo, inclusive participantes |
| `/group/invitelink` | POST | link de convite |
| `/group/create`, `/group/join`, `/group/leave` | POST | ciclo de vida |
| `/group/name`, `/group/description`, `/group/photo`, `/group/settings` | POST | administração |
| `/group/participant` | POST | administrar participantes |

### Envio

| Rota | Método | Uso |
|---|---|---|
| `/send/text` | POST | mensagem de texto |
| `/send/media` | POST | imagem, vídeo, documento, áudio |
| `/send/sticker` | POST | figurinha |
| `/send/contact` | POST | contato |
| `/send/link` | POST | link com prévia |
| `/send/location` | POST | localização |
| `/send/poll` | POST | enquete |
| `/send/button`, `/send/list`, `/send/carousel` | POST | mensagens interativas |
| `/send/status/text`, `/send/status/media` | POST | status |

### Operações sobre mensagem

| Rota | Método | Uso |
|---|---|---|
| `/message/downloadmedia` | POST | baixar mídia, base para transcrição de áudio |
| `/message/markread` | POST | marcar como lida |
| `/message/react` | POST | reagir |
| `/message/edit` | POST | editar |
| `/message/delete` | POST | apagar para todos |
| `/message/status` | POST | estado de entrega |
| `/message/markplayed` | POST | marcar áudio como ouvido |
| `/message/presence` | POST | digitando, gravando |

### Chat

`/chat/archive`, `/chat/unarchive`, `/chat/mute`, `/chat/unmute`, `/chat/pin`,
`/chat/unpin` e `/chat/history-sync`. Todas são operações de estado; nenhuma lê
conversas.

### Outros

Labels (`/label/*`, `/unlabel/*`), newsletters (`/newsletter/*`), comunidades
(`/community/*`), enquetes (`/polls/{pollMessageId}/results`), chamadas
(`/call/reject`) e licença (`/license/*`).

## Consequências para o desenho das ferramentas MCP

1. Listar conversas e ler histórico vêm do PostgreSQL, alimentado pelo
   RabbitMQ. Não há alternativa ao vivo.
2. Contatos e grupos vêm da Evolution ao vivo, porque ela expõe essas listas.
3. Envio vem da Evolution.
4. O histórico anterior à subida do stack depende de `POST /chat/history-sync`,
   e a cobertura precisa ser comunicada ao cliente: as ferramentas devem
   informar desde quando o índice tem dados.
5. Encaminhar mensagem não existe como operação; se for oferecido, é reenvio de
   conteúdo, e isso deve ficar explícito no nome e na descrição da ferramenta.

## Sincronismo de histórico sob demanda

`POST /chat/history-sync` é o único caminho para trazer mensagens anteriores à
entrada da instância no nosso stack. O corpo é:

```json
{
  "messageInfo": {
    "Chat": "5511999999999@s.whatsapp.net",
    "IsFromMe": false,
    "IsGroup": false,
    "ID": "3EB0C5A277F7F9B6C599",
    "Timestamp": "2025-11-11T10:00:00Z"
  },
  "count": 50
}
```

Todos os campos de `messageInfo` são obrigatórios. No código
(`pkg/chat/service/chat_service.go`), o serviço monta um `types.MessageInfo` com
`Chat`, `IsFromMe`, `IsGroup`, `ID` e `Timestamp`, chama
`client.BuildHistorySyncRequest(&messageInfo, data.Count)` e envia o resultado
como mensagem de peer para o próprio dispositivo.

A semântica é a do whatsmeow: pede as `count` mensagens **imediatamente
anteriores** à mensagem referenciada. É paginação para trás, ancorada em uma
mensagem que já se conhece.

Três consequências práticas:

1. **É preciso uma âncora.** Para uma conversa sem nenhuma mensagem conhecida
   não há o que referenciar. A primeira âncora vem do sincronismo inicial que o
   WhatsApp envia sozinho no pareamento, entregue como evento `HistorySync`. A
   partir dela, o backfill é iterativo: pega-se a mensagem mais antiga que já se
   tem na conversa, pede-se `count` anteriores, repete-se.
2. **A resposta HTTP não traz o histórico.** Ela devolve apenas o ack do envio
   (`Timestamp`, `ID`, `ServerID`). As mensagens chegam de forma assíncrona como
   eventos `HistorySync`, pela fila.
3. **Os limites não são configuráveis por aqui.** `fullSyncDaysLimit`,
   `recentSyncDaysLimit`, `onDemandReady` e `initialSyncMaxMessagesPerChat`
   pertencem ao `DeviceProps_HistorySyncConfig` do whatsmeow. O
   `AdvancedSettings` exposto pela Evolution Go contém apenas `alwaysOnline`,
   `ignoreGroups`, `ignoreStatus`, `msgRejectCall`, `readMessages` e
   `rejectCall`. O alcance do histórico é o que o WhatsApp permitir, e o
   telefone precisa estar acessível.

## Eventos e filas do RabbitMQ

Em modo global (`AMQP_GLOBAL_ENABLED=true`), a Evolution declara uma fila
durável do tipo quorum por evento, com o nome em minúsculas. O mapeamento está
em `pkg/events/rabbitmq/rabbitmq_producer.go`:

| `AMQP_GLOBAL_EVENTS` | Filas criadas |
|---|---|
| `MESSAGE` | `message` |
| `SEND_MESSAGE` | `sendmessage` |
| `HISTORY_SYNC` | `historysync` |
| `READ_RECEIPT` | `receipt` |
| `PRESENCE` | `presence` |
| `CHAT_PRESENCE` | `chatpresence`, `archive` |
| `CONNECTION` | `connected`, `pairsuccess`, `temporaryban`, `loggedout`, `connectfailure`, `disconnected` |
| `QRCODE` | `qrcode`, `qrtimeout`, `qrsuccess` |
| `CONTACT` | `contact`, `pushname` |
| `GROUP` | `groupinfo`, `joinedgroup` |
| `LABEL` | `labeledit`, `labelassociationchat`, `labelassociationmessage` |
| `NEWSLETTER` | `newsletterjoin`, `newsletterleave` |
| `CALL` | `calloffer`, `callaccept`, `callterminate`, `calloffernotice`, `callrelaylatency` |

`AMQP_SPECIFIC_EVENTS` tem prioridade sobre `AMQP_GLOBAL_EVENTS` e cria uma fila
por nome de evento, sem o mapeamento acima.

Existe também um modo por instância, em que a fila se chama
`<instanceId>.<evento>` em minúsculas. O projeto usa o modo global.

O `docker-compose.yml` já declara
`AMQP_GLOBAL_EVENTS: "MESSAGE,SEND_MESSAGE,HISTORY_SYNC,CONNECTION"`, e o
consumidor atual lê apenas `message`. Ou seja, as filas `sendmessage`,
`historysync` e as seis de conexão estão sendo criadas e alimentadas sem
consumidor. Sendo duráveis, crescem indefinidamente.

As filas de conexão são a forma mais direta de saber o estado real da instância,
sem depender de polling em `/instance/status`.
