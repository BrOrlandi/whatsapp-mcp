# Instalando na AWS

🇺🇸 [Read this in English](aws.md)

A AWS vende máquinas virtuais de dois jeitos. O **Lightsail** é o caminho
simples: preço mensal fixo, com disco, IPv4 e tráfego incluídos. O **EC2** é o
caminho completo, cobrado por hora, e cada peça (disco, IP, tráfego) vem
separada. Para este projeto, use o Lightsail.

Os preços abaixo são de setembro de 2026. Confira em
[aws.amazon.com/lightsail/pricing](https://aws.amazon.com/lightsail/pricing)
antes de contratar.

## Qual plano

| Plano (`bundleId`) | Memória | Disco | Tráfego | Preço | |
|---|---|---|---|---|---|
| `micro_3_0` | 1 GB | 40 GB | 2 TB | **US$ 7/mês** | O mínimo. Funciona com o swap que o instalador cria |
| `small_3_0` | 2 GB | 60 GB | 3 TB | US$ 12/mês | Folga para contas com muito histórico |
| `medium_3_0` | 4 GB | 80 GB | 4 TB | US$ 24/mês | Só se for rodar outras coisas na mesma máquina |

- **O preço é o mesmo em todas as regiões**, São Paulo incluída. No EC2, São
  Paulo sai cerca de 60% mais caro que os EUA; no Lightsail, não.
- **Contas novas têm 3 meses grátis** nos planos Linux de US$ 5, US$ 7 e US$ 12
  (um por conta). Depois disso, passa a cobrar o preço normal.
- **Não escolha os planos "IPv6-only"**, mesmo sendo mais baratos. O instalador
  precisa de um IPv4 público para o endereço `sslip.io` e para o certificado.
- Com o gateway rodando, a stack ocupa cerca de 300 MB de RAM. Numa máquina de
  1 GB o instalador cria 2 GB de swap sozinho, para aguentar o pico da primeira
  sincronização do histórico.

A **mídia não fica guardada no servidor**. Quando uma ferramenta pede um áudio
ou uma foto, o gateway busca o arquivo no WhatsApp naquele momento. Então não
é preciso contratar S3 nem espaço extra para mídia.

## Qual região

**Conta AWS comum.** Escolha a região mais perto de você e das pessoas com
quem você fala. No Brasil, isso é São Paulo (`sa-east-1`). Toda mensagem fica
guardada naquele disco, então a região também define sob qual jurisdição as
suas conversas ficam.

**Conta nova pelo [builder.aws.com](https://builder.aws.com/)**, a "nova
experiência da AWS", com login pelo Google, GitHub ou Apple e com "projetos":

- A região do projeto é **fixa** e vem do endereço de contato da conta. Não dá
  para criar a máquina em outra região. Veja qual é a sua em *AWS Settings →
  View all projects → Overview → Additional info → Region*.
- O Lightsail está liberado no Free Tier dessa experiência.
- Se você definir um **limite de gasto** (a partir de US$ 20), a AWS **pausa o
  projeto** quando o gasto chega nele, e o seu WhatsApp sai do ar junto. Deixe
  o limite bem acima do preço do plano.

## Caminho 1: pelo site da AWS

1. No [console do Lightsail](https://lightsail.aws.amazon.com/), clique em
   **Create instance**.
2. Escolha a região, **Linux/Unix**, **OS Only** e **Ubuntu 24.04 LTS**.
3. Escolha o plano (US$ 7 ou US$ 12, veja a tabela acima), dê um nome à
   instância e clique em **Create instance**.
4. **Fixe o IP antes de instalar.** Na aba *Networking* da instância, clique em
   **Attach static IP**. O IP público do Lightsail muda toda vez que a máquina
   é parada e ligada de novo, e o endereço do painel é derivado dele. O IP
   fixo é gratuito enquanto estiver ligado a uma instância.
5. Ainda em *Networking*, no firewall IPv4, **adicione a regra HTTPS
   (porta 443)**. O Lightsail já vem com 22 e 80 abertas, mas não com a 443.
6. Clique em **Connect using SSH**. Abre um terminal no navegador. Cole:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash
   ```

7. No fim, o instalador imprime o link `https://…sslip.io/setup?token=…`.
   Abra e siga o assistente, como no [README](../README.md#3-abra-o-link-e-siga-o-assistente).

## Caminho 2: com um agente de IA que roda comandos

Se você usa o Claude Code, o Codex ou outro agente que executa comandos no seu
computador, ele consegue fazer tudo pelo AWS CLI. Você só precisa entrar na
conta pelo navegador.

### Login

Numa conta nova do builder.aws.com, a página do projeto tem um **prompt de
configuração para agentes**. Cole esse prompt no agente: ele instala o AWS CLI
e faz o login. O prompt também informa a região do projeto.

Numa conta comum:

```sh
aws login --region sa-east-1 --profile whatsapp-mcp
```

O comando abre o navegador para você entrar. As credenciais valem 12 horas e se
renovam sozinhas por até 90 dias. Se a página de login mostrar *"Something went
wrong… appeal system"*, abra o link num navegador comum, sem aba anônima, VPN
ou navegador embutido. Se não resolver, rode o comando num terminal seu com
`--remote`. Esse modo mostra um código na página para você colar no terminal.

### Criar a máquina

Os comandos abaixo usam `AWS_REGION` e `NAME` como variáveis. Numa conta do
builder.aws.com, `AWS_REGION` é obrigatoriamente a região do projeto.

```sh
export AWS_PROFILE=whatsapp-mcp AWS_REGION=sa-east-1 NAME=whatsapp-mcp

aws lightsail create-instances --instance-names "$NAME" \
  --availability-zone "${AWS_REGION}a" \
  --blueprint-id ubuntu_24_04 --bundle-id micro_3_0

until [ "$(aws lightsail get-instance-state --instance-name "$NAME" \
           --query state.name --output text)" = running ]; do sleep 5; done

# Fixa o IP antes de instalar: o endereço do painel é derivado dele.
aws lightsail allocate-static-ip --static-ip-name "$NAME-ip"
aws lightsail attach-static-ip --static-ip-name "$NAME-ip" --instance-name "$NAME"

aws lightsail put-instance-public-ports --instance-name "$NAME" --port-infos \
  '[{"fromPort":22,"toPort":22,"protocol":"tcp"},
    {"fromPort":80,"toPort":80,"protocol":"tcp"},
    {"fromPort":443,"toPort":443,"protocol":"tcp"}]'
```

### Rodar o instalador

A instância usa o par de chaves padrão da região. Baixe a chave e entre por
SSH. Nos nossos testes, o certificado temporário de
`get-instance-access-details` foi recusado pelo `sshd`, então use a chave
padrão:

```sh
aws lightsail download-default-key-pair --query privateKeyBase64 --output text > lightsail.pem
chmod 600 lightsail.pem
IP=$(aws lightsail get-static-ip --static-ip-name "$NAME-ip" --query staticIp.ipAddress --output text)

ssh -i lightsail.pem -o StrictHostKeyChecking=accept-new ubuntu@"$IP" \
  'curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash'
```

A última tela do instalador traz o link `/setup?token=…`. O agente entrega esse
link para você abrir. A partir daí, o assistente do painel conduz o resto.

A chave `lightsail.pem` dá acesso total à máquina. Guarde num lugar seguro ou
apague quando terminar. Dá para baixar de novo quando precisar.

## Custos que pegam de surpresa

- **Parar a instância não para a cobrança.** O Lightsail cobra instância
  parada também. Para parar de pagar, é preciso apagar.
- **IP fixo solto é cobrado.** O IP fixo só é gratuito enquanto estiver ligado a
  uma instância. Ao apagar a máquina, libere o IP também.
- **Snapshot é cobrado por GB** armazenado, por mês.
- Passar da franquia de tráfego do plano é cobrado por GB. Uma conta de
  WhatsApp fica muito longe de 2 TB.

## Apagar tudo

```sh
aws lightsail delete-instance --instance-name "$NAME" --force-delete-add-ons
aws lightsail release-static-ip --static-ip-name "$NAME-ip"
# O par de chaves padrão não custa nada; apague só se nada mais o usa:
aws lightsail delete-key-pair --key-pair-name LightsailDefaultKeyPair \
  --expected-fingerprint "$(aws lightsail get-key-pairs --include-default-key-pair \
     --query 'keyPairs[?name==`LightsailDefaultKeyPair`].fingerprint' --output text)"
```

Apagar a instância apaga as mensagens junto. Se quiser guardar alguma coisa,
faça antes o backup descrito em [self-hosting.md](self-hosting.md#backups).
