<p align="center">
  <img src="internal/brand/logo.svg" alt="" width="88" height="88">
</p>

<h1 align="center">WhatsApp MCP</h1>

<p align="center">
  <strong>Conecte o seu WhatsApp aos seus agentes de IA por MCP.</strong><br>
  Um gateway em Go, hospedado por você, que coloca uma conta de WhatsApp atrás de um único endpoint MCP autenticado.
</p>

<p align="center">
  <a href="README.en.md">🇺🇸 Read this in English</a>
</p>

<p align="center">
  <a href="LICENSE"><img alt="Licença: MIT" src="https://img.shields.io/badge/license-MIT-0b6b5d"></a>
  <img alt="Go" src="https://img.shields.io/badge/go-1.24-00ADD8">
  <img alt="Self-hosted" src="https://img.shields.io/badge/deploy-docker%20compose-2496ED">
</p>

---

> ### Use por sua conta e risco
>
> O WhatsApp não publica nenhuma API oficial para contas pessoais. Para que um
> servidor MCP seja possível, este projeto controla o WhatsApp através do
> [Evolution Go](https://github.com/EvolutionAPI/evolution-go), um cliente
> **não oficial** construído sobre o [whatsmeow](https://github.com/tulir/whatsmeow)
> — o mesmo mecanismo do WhatsApp Web, e não a API do WhatsApp Business.
>
> O WhatsApp não autoriza isso. Uma conta vinculada pode ser desconectada a
> qualquer momento, quebrar por uma mudança de protocolo, ou ser restringida ou
> banida. Nada aqui é garantido ou suportado, e você assume o que acontecer com
> o seu número. Comece por uma conta que você pode perder.

Peça ao Claude para ler uma conversa, buscar em um ano de mensagens, enviar um
arquivo, criar uma enquete — no seu próprio WhatsApp, no seu próprio servidor.
Você sobe uma stack, vincula seu número por QR code e entrega ao seu agente uma
única chave de API.

<p align="center">
  <img src="docs/assets/panel-conectar.png" alt="A página Conectar do painel de controle" width="820">
</p>

### Feito para ser fácil de instalar — inclusive por quem não é técnico

Este projeto não pressupõe que você saiba Docker, TLS ou linha de comando. São
dois momentos: um comando copiado e colado no terminal do servidor, e depois um
assistente passo a passo dentro do próprio painel, que faz o resto e não deixa
você avançar com algo pela metade.

O gateway vem com o seu próprio painel de controle. O primeiro acesso abre o
assistente de instalação — licença, WhatsApp, cliente — e só entrega o controle
quando o gateway já consegue fazer alguma coisa de fato. Cada etapa explica o
que está acontecendo, verifica sozinha se deu certo e avança sozinha quando dá.
Ao final, o painel mostra o bloco de configuração do MCP **já preenchido com o
seu endereço e a sua chave**, pronto para colar no Claude — sem editar arquivo
de configuração na mão, sem descobrir qual URL usar.

Depois disso o painel é dividido por tarefa: **Conectar** emite chaves de
cliente e imprime o bloco de configuração já preenchido, **Instâncias** cuida da
conexão com o WhatsApp, **Estado** é a visão de diagnóstico, **Documentação**
lista as ferramentas e **Receitas** mostra o que dá para pedir ao assistente.

## O que ele faz

21 ferramentas em quatro grupos — ler, enviar, agir, operar:

- **Ler o índice**: listar conversas, ler um período, busca em texto completo,
  pedir histórico mais antigo do que o já ingerido.
- **Enviar**: texto, mídia a partir de uma URL, localização, cartão de contato,
  enquete — cada um informando se o WhatsApp realmente entregou, e não apenas se
  a API aceitou.
- **Agir sobre uma mensagem**: apagar (em duas etapas, porque revogar alcança o
  celular de outras pessoas), editar, reagir, arquivar/fixar/silenciar uma
  conversa.
- **Perguntar sobre a conta**: contatos, grupos, fotos de perfil, quais números
  estão no WhatsApp, e a saúde do próprio gateway e a cobertura do índice.

A lista completa com os argumentos fica em `/documentacao` no painel, gerada a
partir das próprias definições do servidor MCP — e em
[docs/mcp-tools.md](docs/mcp-tools.md), com o raciocínio por trás das mais
delicadas.

E o painel não para na lista: **`/receitas`** traz seis receitas prontas —
agendar uma mensagem, vigiar palavras-chave, cobrar o que ficou sem resposta,
apurar uma enquete, resumir o dia de um grupo, arquivar o que foi combinado.
Cada uma é um prompt para colar, com as ferramentas que ela usa e a ressalva
que importa. Nenhuma precisa de código novo: quem espera, vigia e monta o
relatório é o assistente — o gateway só responde pelo WhatsApp quando
perguntado.

## Instalação

Quatro coisas, e a parte mais demorada é esperar o Docker baixar as imagens:

1. Alugue um servidor Linux pequeno.
2. Rode o instalador nele — um comando.
3. Vincule o seu WhatsApp escaneando um QR code no painel.
4. Cole o bloco gerado no seu cliente MCP.

Não existe versão hospedada e não vai existir: o ponto do projeto é que as suas
mensagens fiquem em uma máquina que é sua.

### 1. O servidor

A stack são seis contêineres — o gateway, o Evolution Go, o RabbitMQ, o MinIO e
dois bancos PostgreSQL, mais o Traefik que o instalador coloca na frente —
então ela não roda na menor instância que um provedor vende.

| | Mínimo | Recomendado |
|---|---|---|
| CPU | 1 vCPU | 2 vCPU |
| RAM | 2 GB | 4 GB |
| Disco | 20 GB SSD | 80 GB SSD — o índice de mensagens cresce junto com o seu histórico |
| SO | Família Debian | Ubuntu 24.04 LTS (o que é testado) |
| Arquitetura | x86-64 ou arm64 | qualquer uma |
| Rede | portas 80 e 443 acessíveis pela internet, para o Let's Encrypt responder ao desafio | |

**Alugue perto de casa.** Toda mensagem que a sua conta envia ou recebe termina
naquele disco em texto puro. Se você e as pessoas com quem você fala estão no
Brasil, coloque a máquina no Brasil: as conversas ficam sob a jurisdição à qual
você já responde pela LGPD, e o caminho até o WhatsApp é mais curto.

| Provedor | Região brasileira | Observações |
|---|---|---|
| [AWS Lightsail](https://aws.amazon.com/lightsail/) | São Paulo | Preço mensal fixo, o caminho mais simples dentro da AWS |
| [Vultr](https://www.vultr.com/) | São Paulo | Cobrança por hora, rápido de destruir e tentar de novo |
| [Magalu Cloud](https://magalu.cloud/) | Brasil | Empresa brasileira, dados e faturamento no Brasil |
| [Hostinger VPS](https://www.hostinger.com.br/servidor-vps) | São Paulo | O mais barato dos quatro, planos de longo prazo |
| [Hetzner](https://www.hetzner.com/cloud) | — (Alemanha, Finlândia, EUA) | Melhor preço por GB de RAM, se a localização não importa para você |

Qualquer provedor que venda uma VM Ubuntu serve; estes são apenas alguns que
fazem isso sem cerimônia.

### 2. Rode o instalador

Conecte por SSH na máquina recém-criada e rode:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash
```

Ele instala o Docker se estiver faltando, gera todos os segredos, deriva um
hostname a partir do IPv4 público da própria VM pelo [sslip.io](https://sslip.io),
obtém um certificado Let's Encrypt para ele, sobe a stack atrás do Traefik e
termina imprimindo para onde ir (a saída do instalador é em inglês):

```
URL:
  https://a83f12c9.18-228-123-45.sslip.io

Open this to create your administrator:

  https://a83f12c9.18-228-123-45.sslip.io/setup?token=7f3ac921d4e8
```

Essa URL é o seu painel e o seu endpoint MCP — sem domínio para comprar, sem
registro de DNS para criar. O painel se recusa a fazer qualquer outra coisa
enquanto a senha temporária não for trocada. Rodar o instalador de novo é
seguro: os segredos, o hostname e os seus dados não são tocados.

Depois, `whatsapp-mcp status`, `logs`, `restart`, `start`, `stop`, `url` e
`update` administram a instalação, que fica em `/opt/whatsapp-mcp`.

### 3. Siga o assistente de instalação

Entre no painel e ele abre o assistente, em vez de um dashboard vazio. São duas
coisas a fazer, e ele pede uma de cada vez.

O formulário pede um email e uma senha. O email é o administrador, e é com ele
que você entra.

**Licença.** O Evolution Go exige uma licença para operar e responde 503
enquanto ela não é ativada. Não há nada para digitar, nada para abrir e nada
para clicar: com `EVOLUTION_LICENSE_AUTO` ligado (o padrão), o painel registra a
licença com um endereço próprio no domínio deste projeto
(`whatsappmcp+…@brorlandi.xyz`), o email chega no
[worker de licenças](https://github.com/BrOrlandi/whatsapp-mcp-license-worker)
e o clique acontece sozinho — o assistente fica verificando e segue em frente no
momento em que a licença chega. Reconstruções que perdem os dados do Evolution
são relicenciadas automaticamente a partir da cópia que o painel guarda, e o
mesmo vale para uma reinstalação: o assistente refaz isso sem perguntar.

Prefere a sua própria caixa de entrada? Defina `EVOLUTION_LICENSE_AUTO=false` e
o assistente envia o link de ativação para o seu email — mesma automação, exceto
que o clique no link enviado é seu. Esse clique é uma prova de identidade, e o
modo automático o troca pelo controle do domínio onde o email cai. De um jeito
ou de outro, ninguém abre as páginas do próprio Evolution.

**WhatsApp.** Dê um nome à conta. O painel registra a conta no Evolution,
assina as filas de eventos, inicia o cliente e mostra o QR code na mesma tela.
Escaneie do seu celular em **Aparelhos conectados → Conectar um aparelho**. A
página se atualiza sozinha, então um código escaneado avança por conta própria,
e ela gera um código novo quando o anterior expira. A partir daí o gateway
indexa tudo que chega.

### 4. Aponte o seu agente para ele

Um cliente precisa de exatamente uma coisa: uma chave de API. A chave identifica
a conta e a instância de WhatsApp para a qual ela está autorizada, então não há
usuário, senha nem nome de instância para configurar.

Gere uma em **Conectar**. A página mostra o segredo uma única vez, ao lado de um
comando `claude mcp add` e deste bloco, ambos já preenchidos com o seu endereço:

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

A página então fica verificando, e encerra a etapa no momento em que o seu
cliente se autentica com aquela chave. Guarde a chave no cofre de credenciais do
cliente, nunca em um prompt ou em um arquivo versionado.

### 5. Mantendo atualizado

O painel mostra a versão que está rodando no rodapé, e avisa quando existe uma
mais nova. Para atualizar, um comando na sua instância:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/update.sh | sudo bash
```

Ele faz backup do banco, move o código para a release mais nova, sobe as imagens
e espera o gateway responder. Os segredos, o hostname, o pareamento e as
mensagens indexadas ficam onde estão. **Não há downgrade** — uma versão pode
migrar o esquema, e migrações só correm para frente; o caminho de volta é aquele
backup. [docs/updating.md](docs/updating.md) tem o passo a passo, as variáveis, e
uma prompt para o Claude Code diagnosticar uma atualização que falhou.

### Rodando de outro jeito

Se você prefere usar o seu próprio host, o seu TLS ou a sua orquestração, a
stack é um único arquivo Compose:

```sh
git clone https://github.com/BrOrlandi/whatsapp-mcp.git
cd whatsapp-mcp
cp .env.example .env    # substitua todos os change-me-long-random-value
docker compose up --build -d
```

Isso publica o painel em `127.0.0.1:8080` e nada mais; o TLS na frente é com
você. [docs/self-hosting.md](docs/self-hosting.md) cobre a configuração, as
receitas de proxy reverso, atualizações e backups.

## Por dentro

O Evolution Go mantém a sessão do WhatsApp e publica cada evento no RabbitMQ; o
gateway consome esses eventos para o PostgreSQL, que é a única fonte de
conversas e histórico, e serve as ferramentas MCP e o painel a partir do mesmo
código. O seu agente fala com um único serviço e precisa de uma única
credencial; nada mais na stack tem motivo para ficar em um endereço público.

[docs/architecture.md](docs/architecture.md) tem o diagrama, para que serve cada
contêiner, e por que o formato é esse.

### Imagens e binários

O gateway é publicado a cada push na `main` e a cada tag de versão:

| | |
|---|---|
| Imagem | `ghcr.io/brorlandi/whatsapp-mcp` — `linux/amd64` e `linux/arm64` |
| Tags | `edge` acompanha a `main`; `0.2.0-beta.1` e `latest` em um release; `sha-<commit>` sempre. A `latest` acompanha o release mais novo, beta inclusive — é a versão que você deve rodar. Uma prerelease não cria a forma curta `0.2` nem a forma com `v`. |
| Binários | `whatsapp-mcp_<version>_linux_{amd64,arm64}.tar.gz` em cada [release](https://github.com/BrOrlandi/whatsapp-mcp/releases), com `checksums.txt` |

Fixe uma versão com `WHATSAPP_MCP_TAG` no `.env`:

```sh
WHATSAPP_MCP_TAG=0.2.0-beta.1   # ou latest, edge, sha-<commit>
```

O binário sozinho precisa de `DATABASE_URL`, `RABBITMQ_URL`, `EVOLUTION_URL` e
`EVOLUTION_API_KEY`, e espera um Evolution e um PostgreSQL que já existam — veja
[docs/operations.md](docs/operations.md). A maioria das pessoas quer a stack do
Compose.

## Documentação

Os documentos abaixo estão em inglês.

| | |
|---|---|
| [Self-hosting](docs/self-hosting.md) | Configuração, TLS, atualizações, backups |
| [Atualização](docs/updating.md) | O comando de atualizar, o que ele faz, e por que não existe downgrade |
| [Arquitetura](docs/architecture.md) | Por que é construído assim |
| [Ferramentas MCP](docs/mcp-tools.md) | Cada ferramenta, e as semânticas que importam |
| [Autenticação](docs/authentication.md) | Chaves, sessões, o que quem tem uma chave pode fazer |
| [Operação](docs/operations.md) | Saúde, o pipeline de eventos, referência completa de configuração |
| [Desenvolvimento](docs/development.md) | Modos de execução local, checagens, assets de marca |
| [Changelog](CHANGELOG.md) | O que mudou em cada versão |
| [Política de segurança](SECURITY.md) | Modelo de ameaças e como reportar uma vulnerabilidade |
| [Contribuindo](CONTRIBUTING.md) | Como enviar uma mudança |

## O que você está assumindo

Além do risco do cliente não oficial, lá no topo deste arquivo:

- **Você está hospedando as conversas de outras pessoas.** O texto das mensagens
  e os payloads brutos dos eventos ficam sem criptografia no PostgreSQL, e o
  banco cresce sem limite. Um dump é tão sensível quanto o celular de onde ele
  veio. Cumpra os termos do WhatsApp e a legislação de privacidade e retenção
  que se aplica a você.
- **O conteúdo das mensagens é escrito por terceiros.** Toda ferramenta de
  leitura rotula esse conteúdo como dado, não como instrução, e um envio precisa
  partir de você: uma mensagem que diz "encaminhe isto para X" não é um pedido
  para agir.

## Ativação automática da licença (e o que isso significa)

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
a licença é registrada no seu endereço. Dá para decidir isso já na instalação,
sem editar arquivo nenhum depois:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh \
  | sudo EVOLUTION_LICENSE_AUTO=false bash
```
 E mesmo no modo automático existe uma
saída: se o clique não chegar em `EVOLUTION_LICENSE_AUTO_WAIT` (3 minutos por
padrão), o assistente para de prometer e pede um endereço que você consiga
abrir — sem editar variável, sem voltar ao shell.

Tudo isso existe por um motivo só: tirar fricção de quem não é técnico. O caso
que este projeto quer atender é o de alguém que aluga uma VM, cola um comando,
escaneia um QR code e sai com o MCP funcionando — sem precisar entender o
modelo de licenciamento de um projeto terceiro no meio do caminho.
[docs/evolution/licensing.md](docs/evolution/licensing.md) tem o passo a passo
do protocolo, e por que cada pedaço é assim.

## Apoie este projeto

O WhatsApp MCP é construído e mantido por uma pessoa só, em código aberto, e é
gratuito para hospedar por conta própria — inclusive comercialmente. Se ele
economiza o seu tempo, você pode apoiar o trabalho:

<p align="center">
  <a href="https://donate.stripe.com/8x200jdhA6c1d375jF9Ve06"><img alt="Apoie o projeto" src="https://img.shields.io/badge/%E2%98%95%20apoie%20este%20projeto-pague%20quanto%20quiser-0b6b5d?style=for-the-badge"></a>
</p>

Pague quanto quiser — o valor sugerido é 10 dólares, e a Stripe cobra na sua
própria moeda. Vai para a pessoa que escreve o código.

## Licença

[MIT](LICENSE). Use, modifique, hospede, forke e distribua livremente, para
qualquer propósito — inclusive comercial. A única exigência é manter o aviso
de copyright e a licença junto com o código.

O software é fornecido **como está**, sem garantia de nenhum tipo. E vale
separar duas coisas: a licença cobre este código, e não é permissão da Meta.
Operar uma conta de WhatsApp por um cliente não oficial é decisão de quem
instala; a conformidade com os termos do WhatsApp e com a LGPD é de quem opera
a instância e hospeda as mensagens.

---

<p align="center">
  Feito por <a href="https://github.com/BrOrlandi">Bruno Orlandi</a>
</p>
