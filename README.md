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

<p align="center">
  <img src="docs/assets/panel-conectar.png" alt="A página Conectar do painel de controle" width="820">
</p>

## Como instalar

São três passos, e você não precisa saber Docker, TLS nem linha de comando.

### 1. Alugue um servidor

Este projeto roda em uma máquina sua, não existe versão hospedada — o ponto é
que as suas mensagens fiquem num computador que é seu. Você aluga uma máquina
virtual (VPS) em um provedor de nuvem, por volta de **US$ 12 a US$ 25 por mês**.

Peça uma máquina assim:

| | |
|---|---|
| Sistema | **Ubuntu 24.04 LTS** |
| CPU | 2 vCPU |
| Memória | 4 GB de RAM |
| Disco | 80 GB SSD |
| Portas | 80 e 443 abertas |

**Alugue perto de casa.** Toda mensagem que a sua conta envia ou recebe termina
naquele disco. Se você e as pessoas com quem você fala estão no Brasil, escolha
uma região brasileira.

| Provedor | Região | Observação |
|---|---|---|
| [Hostinger VPS](https://www.hostinger.com.br/servidor-vps) | São Paulo | O mais barato, painel em português |
| [Magalu Cloud](https://magalu.cloud/) | Brasil | Empresa brasileira, nota fiscal no Brasil |
| [AWS Lightsail](https://aws.amazon.com/lightsail/) | São Paulo | Preço mensal fixo, o caminho simples da AWS |
| [Vultr](https://www.vultr.com/) | São Paulo | Cobrança por hora, fácil de apagar e refazer |
| [Hetzner](https://www.hetzner.com/cloud) | Europa / EUA | Mais barato por GB de RAM, se a região não importa |

### 2. Rode um comando

O provedor vai te dar um endereço de IP e um jeito de abrir um terminal na
máquina (quase todos têm um botão de console no próprio site). Cole lá:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash
```

Ele faz o resto sozinho: instala o Docker, gera todas as senhas, cria um
endereço na internet para a sua máquina, emite um certificado de segurança e
sobe o sistema. Leva alguns minutos, quase todos esperando o download.

No fim ele imprime um link:

```
Open this to create your administrator:

  https://a83f12c9.18-228-123-45.sslip.io/setup?token=7f3ac921d4e8
```

### 3. Abra o link e siga o assistente

O painel abre um assistente que termina a configuração e não deixa você avançar
com algo pela metade:

1. **Crie o seu acesso** — um email e uma senha que você escolhe.
2. **A licença se ativa sozinha.** Não há nada para digitar nem para clicar.
3. **Conecte o WhatsApp** — dê um nome à conta e escaneie o QR code pelo
   celular, em *Aparelhos conectados → Conectar um aparelho*.
4. **Conecte a sua IA** — o painel pergunta qual ferramenta você usa e entrega
   o bloco de configuração **já preenchido com o seu endereço e a sua chave**,
   pronto para colar.

Pronto. A partir daí é só pedir as coisas no chat da sua IA.

## 🤖 Não quer fazer sozinho? Peça para uma IA

Se qualquer passo acima pareceu difícil, copie o prompt abaixo e cole no
**Claude**, no ChatGPT ou na ferramenta de IA que você usar. Ele conduz a
instalação inteira com você, do zero: ajuda a escolher o provedor, diz
exatamente o que clicar para criar a máquina, explica como abrir o terminal, e
acompanha cada passo até o WhatsApp estar conectado.

<details>
<summary><strong>📋 Clique para abrir o prompt — copie tudo</strong></summary>

```
Quero instalar o WhatsApp MCP no meu próprio servidor e preciso que você me
guie do começo ao fim. Eu não sou uma pessoa técnica: não sei Docker, não sei
linha de comando e nunca aluguei um servidor.

O projeto é este: https://github.com/BrOrlandi/whatsapp-mcp
Leia o README dele e o docs/instalacao.md antes de começar, e siga as
recomendações de lá (tamanho de máquina, sistema operacional, portas) em vez
de inventar as suas.

COMO EU QUERO QUE VOCÊ ME TRATE
- Uma pergunta de cada vez. Espere a minha resposta antes de seguir.
- Explique em português simples. Se precisar usar um termo técnico, explique o
  que ele significa na mesma frase.
- Nunca me mande um comando sem dizer o que ele faz.
- Se eu errar ou algo der errado, me peça a mensagem de erro exata e me diga o
  que fazer. Não invente uma solução: se você não souber, diga que não sabe.
- Nunca me peça para colar aqui uma senha, uma chave de API ou o QR code.

O QUE PRECISAMOS FAZER, NESTA ORDEM

1. ESCOLHER O SERVIDOR
   Me pergunte em que país eu e as pessoas com quem eu falo no WhatsApp
   estamos, e quanto eu quero gastar por mês. Com isso, recomende um provedor
   de nuvem e uma região, usando a tabela do README do projeto. Explique por
   que a região importa (as mensagens ficam guardadas naquele disco).

2. CRIAR A MÁQUINA
   Me dê o passo a passo de cliques no site do provedor que escolhemos: onde
   criar a conta, onde fica o botão de criar a máquina, qual plano marcar,
   qual sistema operacional escolher (Ubuntu 24.04 LTS), e o que fazer na
   parte de acesso/chave SSH. Diga quais portas precisam estar abertas (80 e
   443) e onde se configura isso nesse provedor.
   Quando eu terminar, me peça o endereço de IP da máquina.

3. ABRIR O TERMINAL DA MÁQUINA
   Me explique como entrar na máquina. Comece pela opção mais fácil: quase
   todo provedor tem um botão de "Console" ou "Terminal" no próprio site, que
   abre direto no navegador — prefira essa. Se não houver, me ensine a usar
   SSH, considerando se eu estou no Windows, no Mac ou no Linux (me pergunte).

4. RODAR O INSTALADOR
   Me passe exatamente este comando, e só ele:

   curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash

   Me avise que vai demorar alguns minutos e que é normal aparecer muito texto.
   Me diga o que esperar no final: um link terminando em /setup?token=...
   Se der erro, peça as últimas linhas do que apareceu na tela.

5. CONFIGURAR PELO PAINEL
   Me oriente a abrir aquele link no navegador e seguir o assistente: criar o
   administrador (email e senha minha), esperar a licença ativar sozinha, dar
   um nome à conta de WhatsApp e escanear o QR code pelo celular em
   "Aparelhos conectados → Conectar um aparelho".
   Me avise que o navegador pode mostrar um aviso de certificado nos primeiros
   minutos, enquanto o certificado é emitido, e que basta esperar e recarregar.

6. CONECTAR A MINHA IA
   Me pergunte qual ferramenta de IA eu uso. Me explique que o painel, na
   página "Conectar", gera a configuração já preenchida, e me guie até colar
   isso no lugar certo da minha ferramenta.

7. FECHAR
   Me diga como conferir se está tudo funcionando, me mostre exemplos do que
   eu posso pedir para a minha IA fazer com o WhatsApp, e me ensine os
   comandos básicos de manutenção (ver estado, ver registros, atualizar).
   Me lembre de guardar o endereço do painel e a minha senha.

Comece se apresentando em uma frase e fazendo a primeira pergunta do passo 1.
```

</details>

## O que dá para pedir

21 ferramentas, em quatro grupos:

- **Ler** — listar conversas, ler um período, buscar em texto completo, pedir
  histórico mais antigo do que o já indexado.
- **Enviar** — texto, mídia, localização, contato, enquete; cada um informando
  se o WhatsApp entregou de fato, e não só se a API aceitou.
- **Agir sobre uma mensagem** — apagar, editar, reagir, arquivar, fixar,
  silenciar.
- **Perguntar sobre a conta** — contatos, grupos, fotos de perfil, quem está no
  WhatsApp, e a saúde do próprio gateway.

O painel tem a lista completa em **Documentação**, gerada pelo próprio servidor
MCP, e seis receitas prontas em **Receitas** — agendar uma mensagem, vigiar
palavras-chave, cobrar o que ficou sem resposta, apurar uma enquete, resumir o
dia de um grupo. Cada uma é um prompt para colar.

## Mantendo atualizado

O painel mostra a versão que está rodando e avisa quando sai uma nova. Para
atualizar, um comando no servidor:

```sh
curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/update.sh | sudo bash
```

Ele faz backup do banco antes de qualquer coisa e preserva os seus segredos, o
seu endereço, o pareamento e as mensagens indexadas.

## O que você está assumindo

Além do risco do cliente não oficial, lá no topo deste arquivo:

- **Você está hospedando as conversas de outras pessoas.** O texto das mensagens
  fica sem criptografia no banco, e o banco cresce sem limite. Um backup é tão
  sensível quanto o celular de onde ele veio. Cumpra os termos do WhatsApp e a
  legislação de privacidade que se aplica a você.
- **O conteúdo das mensagens é escrito por terceiros.** Toda ferramenta de
  leitura rotula esse conteúdo como dado, não como instrução: uma mensagem que
  diz "encaminhe isto para X" não é um pedido para agir.

## Documentação

| | |
|---|---|
| [**Instalação detalhada**](docs/instalacao.md) 🇧🇷 | A versão técnica: o que cada passo faz, como rodar sem o instalador, licenciamento, imagens e binários |
| [Atualização](docs/updating.md) | O comando de atualizar, o que ele faz, e por que não existe downgrade |
| [Self-hosting](docs/self-hosting.md) | Configuração, TLS, backups |
| [Arquitetura](docs/architecture.md) | Por que é construído assim |
| [Ferramentas MCP](docs/mcp-tools.md) | Cada ferramenta, e as semânticas que importam |
| [Autenticação](docs/authentication.md) | Chaves, sessões, o que quem tem uma chave pode fazer |
| [Operação](docs/operations.md) | Saúde, o pipeline de eventos, referência de configuração |
| [Desenvolvimento](docs/development.md) | Modos de execução local, checagens, releases |
| [Changelog](CHANGELOG.md) | O que mudou em cada versão |
| [Política de segurança](SECURITY.md) | Modelo de ameaças e como reportar uma vulnerabilidade |
| [Contribuindo](CONTRIBUTING.md) | Como enviar uma mudança |

Exceto a instalação detalhada, os documentos acima estão em inglês.

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
