# Instalação detalhada

O [README](../README.md) tem o caminho curto: alugue uma máquina, cole um
comando, siga o assistente. Este documento é o mesmo caminho explicado — o que
cada passo faz, o que ele decide por você, e o que fazer quando você quer
decidir por conta própria.

É a versão para quem quer entender antes de rodar, ou para quem já tem uma
opinião sobre onde o TLS termina.

---

## O que vai subir na sua máquina

Seis contêineres, mais o Traefik que o instalador coloca na frente:

| Contêiner | Para quê |
|---|---|
| `whatsapp-mcp` | O gateway: serve o endpoint MCP, o painel e as sondas de saúde |
| `evolution-go` | Mantém a sessão do WhatsApp e publica cada evento |
| `rabbitmq` | A fila por onde os eventos chegam ao gateway |
| `postgres-mcp` | O índice de mensagens, as chaves, a conta do painel |
| `postgres-evolution` | O estado do Evolution |
| `minio` | Onde a mídia recebida é guardada |
| `traefik` | TLS e o certificado Let's Encrypt |

É por isso que a stack não roda na menor instância que um provedor vende.

## 1. O servidor

| | Mínimo | Recomendado |
|---|---|---|
| CPU | 1 vCPU | 2 vCPU |
| RAM | 2 GB | 4 GB |
| Disco | 20 GB SSD | 80 GB SSD — o índice cresce junto com o seu histórico |
| Sistema | Família Debian | Ubuntu 24.04 LTS (o que é testado) |
| Arquitetura | x86-64 ou arm64 | qualquer uma |
| Rede | portas 80 e 443 acessíveis pela internet | |

As portas 80 e 443 precisam estar abertas porque é assim que o Let's Encrypt
prova que a máquina é sua: ele bate na porta 80 do endereço para o qual o
certificado foi pedido. Sem isso não há certificado, e sem certificado o
navegador recusa o painel.

**Alugue perto de casa.** Toda mensagem que a sua conta envia ou recebe termina
naquele disco em texto puro. Se você e as pessoas com quem você fala estão no
Brasil, coloque a máquina no Brasil: as conversas ficam sob a jurisdição à qual
você já responde pela LGPD, e o caminho até o WhatsApp é mais curto.

| Provedor | Região brasileira | Observações |
|---|---|---|
| [AWS Lightsail](https://aws.amazon.com/lightsail/) | São Paulo | Preço mensal fixo, o caminho mais simples dentro da AWS |
| [Vultr](https://www.vultr.com/) | São Paulo | Cobrança por hora, rápido de destruir e tentar de novo |
| [Magalu Cloud](https://magalu.cloud/) | Brasil | Empresa brasileira, dados e faturamento no Brasil |
| [Hostinger VPS](https://www.hostinger.com.br/servidor-vps) | São Paulo | O mais barato dos cinco, planos de longo prazo |
| [Hetzner](https://www.hetzner.com/cloud) | — (Alemanha, Finlândia, EUA) | Melhor preço por GB de RAM, se a localização não importa para você |

Qualquer provedor que venda uma VM Ubuntu serve; estes são apenas alguns que
fazem isso sem cerimônia.

Sobre o firewall: filtre no provedor (Cloud Firewall da DigitalOcean, security
group da AWS, e assim por diante), não com o `ufw` dentro da máquina. O Docker
escreve as próprias regras de NAT, que são avaliadas antes das do `ufw` — um
`ufw deny 80` deixa a porta 80 aberta do mesmo jeito. O instalador registra as
regras mas deixa o `ufw` desligado de propósito, e
[docs/self-hosting.md](self-hosting.md) explica por quê.

## 2. O instalador

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash
```

O que ele faz, na ordem:

1. Confere o sistema, a arquitetura e o espaço em disco.
2. Instala o Docker, se estiver faltando, e o habilita para voltar depois de um
   reboot.
3. Clona o projeto em `/opt/whatsapp-mcp`.
4. Descobre o IPv4 público da máquina e deriva um hostname a partir dele pelo
   [sslip.io](https://sslip.io) — `a83f12c9.18-228-123-45.sslip.io`. Nenhum
   domínio para comprar, nenhum registro de DNS para criar.
5. Gera todos os segredos no `.env` e os guarda com permissão 600.
6. Sobe a stack atrás do Traefik, que pede o certificado.
7. Instala o comando `whatsapp-mcp`.
8. Imprime o link de setup, com um token que guarda o formulário de primeiro
   acesso.

```
URL:
  https://a83f12c9.18-228-123-45.sslip.io

Open this to create your administrator:

  https://a83f12c9.18-228-123-45.sslip.io/setup?token=7f3ac921d4e8
```

Essa URL é o seu painel e o seu endpoint MCP. Rodar o instalador de novo é
seguro: os segredos, o hostname e os seus dados não são tocados.

A saída do instalador é em inglês.

### Variáveis que ele aceita

Passe antes do `bash` para decidir na hora da instalação, sem editar arquivo
depois:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh \
  | sudo EVOLUTION_LICENSE_AUTO=false bash
```

| Variável | Para quê |
|---|---|
| `EVOLUTION_LICENSE_AUTO` | `false` manda o link de licença para o seu email em vez de ativar sozinho |
| `EVOLUTION_LICENSE_EMAIL_DOMAIN` | O domínio dos endereços de licença, se você operar o seu próprio worker |
| `INSTALL_DIR` | Onde instalar. Padrão `/opt/whatsapp-mcp` |
| `REPO_REF` | Qual referência do repositório instalar. Padrão `main` |

### O comando de manutenção

```sh
whatsapp-mcp status     # contêineres, saúde e a URL pública
whatsapp-mcp logs       # acompanha os registros; aceita um serviço
whatsapp-mcp version    # a versão que está servindo
whatsapp-mcp update     # move para a release mais nova
whatsapp-mcp restart    # reinicia
whatsapp-mcp stop       # para, preservando os dados
whatsapp-mcp start      # sobe de novo
whatsapp-mcp url        # imprime a URL pública
```

## 3. O assistente

O primeiro acesso abre um assistente em vez de um painel vazio. Cada etapa
explica o que está acontecendo, verifica sozinha se deu certo e avança sozinha
quando dá.

**Administrador.** Um email e uma senha que você escolhe. O email é a sua
identidade no painel. O token na URL guarda esse formulário porque ele está
publicado na internet, e deixa de significar qualquer coisa assim que existe um
administrador — o que é também por que rodar o instalador de novo pode imprimir
o link outra vez sem ser uma porta de entrada.

**Licença.** O Evolution Go exige uma licença para operar e responde 503
enquanto ela não é ativada. Com `EVOLUTION_LICENSE_AUTO` ligado, que é o padrão,
não há nada para digitar, abrir ou clicar — veja
[Licenciamento automático](#licenciamento-automático) abaixo.

**WhatsApp.** Dê um nome à conta. O painel registra a conta no Evolution,
assina as filas de eventos, inicia o cliente e mostra o QR code na mesma tela.
Escaneie do seu celular em **Aparelhos conectados → Conectar um aparelho**. A
página se atualiza sozinha, então um código escaneado avança por conta própria,
e ela gera um código novo quando o anterior expira. A partir daí o gateway
indexa tudo que chega.

## 4. Apontar o agente

Um cliente precisa de exatamente uma coisa: uma chave de API. A chave identifica
a conta e a instância de WhatsApp para a qual ela está autorizada, então não há
usuário, senha nem nome de instância para configurar.

A página **Conectar** pergunta onde a conexão será usada, emite a chave sem
você pedir e abre as instruções da ferramenta escolhida — com um comando
`claude mcp add` e este bloco, ambos já preenchidos com o seu endereço:

```json
{
  "mcpServers": {
    "whatsapp": {
      "type": "http",
      "url": "https://whatsapp.example.com/mcp",
      "headers": { "Authorization": "Bearer wamcp-…" }
    }
  }
}
```

A página fica verificando e encerra a etapa no momento em que o seu cliente se
autentica com aquela chave. O segredo aparece uma única vez: guarde no cofre de
credenciais do cliente, nunca em um prompt ou em um arquivo versionado.

Cada ferramenta tem a sua própria conexão, listada pelo nome que ela deu de si
mesma no handshake MCP — "Claude Desktop", não um prefixo de chave. Desconectar
uma vale na hora e não mexe nas outras.

## Rodando de outro jeito

Se você prefere usar o seu próprio host, o seu TLS ou a sua orquestração, a
stack é um único arquivo Compose:

```sh
git clone https://github.com/BrOrlandi/whatsapp-mcp.git
cd whatsapp-mcp
cp .env.example .env    # substitua todos os change-me-long-random-value
docker compose up --build -d
```

Isso publica o painel em `127.0.0.1:8080` e nada mais; o TLS na frente é com
você. [docs/self-hosting.md](self-hosting.md) cobre a configuração, as receitas
de proxy reverso e os backups.

### Imagens e binários

O gateway é publicado a cada push na `main` e a cada tag de versão:

| | |
|---|---|
| Imagem | `ghcr.io/brorlandi/whatsapp-mcp` — `linux/amd64` e `linux/arm64` |
| Tags | `edge` acompanha a `main`; `0.2.0-beta.1` e `latest` em um release; `sha-<commit>` sempre. A `latest` acompanha o release mais novo, beta inclusive — é a versão que você deve rodar. |
| Binários | `whatsapp-mcp_<version>_linux_{amd64,arm64}.tar.gz` em cada [release](https://github.com/BrOrlandi/whatsapp-mcp/releases), com `checksums.txt` |

Fixe uma versão com `WHATSAPP_MCP_TAG` no `.env`:

```sh
WHATSAPP_MCP_TAG=0.2.0-beta.1   # ou latest, edge, sha-<commit>
```

O binário sozinho precisa de `DATABASE_URL`, `RABBITMQ_URL`, `EVOLUTION_URL` e
`EVOLUTION_API_KEY`, e espera um Evolution e um PostgreSQL que já existam — veja
[docs/operations.md](operations.md). A maioria das pessoas quer a stack do
Compose.

## Por dentro

O Evolution Go mantém a sessão do WhatsApp e publica cada evento no RabbitMQ; o
gateway consome esses eventos para o PostgreSQL, que é a única fonte de
conversas e histórico, e serve as ferramentas MCP e o painel a partir do mesmo
código. O seu agente fala com um único serviço e precisa de uma única
credencial; nada mais na stack tem motivo para ficar em um endereço público.

[docs/architecture.md](architecture.md) tem o diagrama, para que serve cada
contêiner, e por que o formato é esse.

## Licenciamento automático

O Evolution Go, que é quem realmente fala com o WhatsApp, exige uma licença
para operar e responde 503 até ser ativado. Conseguir essa licença envolve um
passo que não dá para pular de forma honesta: o servidor de licenciamento da
Evolution manda um *magic link* por email, e o clique nesse link é a prova de
identidade pela qual a licença é emitida.

Esse é exatamente o tipo de passo que faz alguém leigo desistir no meio da
instalação — abrir o gerenciador do Evolution, entender o que é uma licença,
achar o email, clicar no link certo. Então, por padrão, este projeto faz isso
por você:

- A cada instalação o painel registra a licença em um endereço próprio deste
  projeto, `whatsappmcp+<aleatório>@brorlandi.xyz` — um endereço novo por
  instalação, para que cada deploy seja o seu próprio registro.
- O Cloudflare Email Routing entrega o email daquele endereço ao
  [whatsapp-mcp-license-worker](https://github.com/BrOrlandi/whatsapp-mcp-license-worker),
  um Email Worker que encontra o link na mensagem e faz o mesmo GET que um
  navegador faria. O mesmo clique, só que no servidor. Ele é um projeto à
  parte, sob licença MIT, e o README de lá descreve o que o worker aceita e o
  que ele ignora — inclusive o detalhe de que o link chega reescrito pelo
  rastreador de email da Evolution, e não como URL do licenciador.
- O servidor de licenciamento redireciona para o callback do painel, o
  assistente percebe e segue em frente. Você não digitou email, não abriu caixa
  de entrada e não clicou em nada.

A credencial resultante é ativada no *seu* Evolution e uma cópia fica no *seu*
banco — por isso uma reconstrução que perca o volume do Evolution é
relicenciada sozinha, sem perguntar nada.

**O que você está trocando.** Aquele clique é uma prova de identidade, e o modo
automático o troca pelo controle do domínio onde o email cai — ou seja, a
licença fica registrada em um endereço deste projeto, e não em um seu. O que
passa por ali é apenas o email de ativação do Evolution: nenhuma mensagem sua
de WhatsApp, nenhuma chave de API do seu gateway e nenhum dado do seu servidor
chegam perto do worker. Ainda assim, é uma dependência externa, e ela é
explicitada aqui de propósito.

**Não quer isso?** `EVOLUTION_LICENSE_AUTO=false` e o assistente manda o link
para o seu email — mesma automação em tudo o mais, exceto que o clique é seu e
a licença é registrada no seu endereço. E mesmo no modo automático existe uma
saída: se o clique não chegar em `EVOLUTION_LICENSE_AUTO_WAIT` (3 minutos por
padrão), o assistente para de prometer e pede um endereço que você consiga
abrir — sem editar variável, sem voltar ao shell.

[docs/evolution/licensing.md](evolution/licensing.md) tem o passo a passo do
protocolo, e por que cada pedaço é assim.

## Quando algo dá errado

**O navegador diz que o certificado é inválido.** O Let's Encrypt leva alguns
minutos na primeira vez. Espere e recarregue. Se persistir,
`whatsapp-mcp logs traefik` diz o que o desafio respondeu — quase sempre a
porta 80 fechada no firewall do provedor.

**O instalador para dizendo que algo já escuta na porta 80 ou 443.** Outra
coisa na máquina ocupou a porta. Pare esse serviço e rode de novo.

**O painel abre mas o Evolution responde 503.** É a licença. O assistente
mostra em que pé está; se a ativação automática não chegou em três minutos, ele
oferece o caminho manual na própria tela.

**O QR code expira antes de eu escanear.** A página gera outro sozinha. Deixe-a
aberta e escaneie o novo.

[docs/operations.md](operations.md) tem a referência completa de configuração,
as sondas de saúde e o resto da lista.
