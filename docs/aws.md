# Installing on AWS

🇧🇷 [Leia em português](aws.pt-BR.md)

AWS sells virtual machines two ways. **Lightsail** is the simple one: a flat
monthly price with the disk, a public IPv4 and the traffic included. **EC2** is
the full one, billed by the hour, with every piece (disk, IP, traffic) charged
separately. Use Lightsail for this project.

Prices below are from September 2026. Check
[aws.amazon.com/lightsail/pricing](https://aws.amazon.com/lightsail/pricing)
before you sign up.

## Which plan

| Plan (`bundleId`) | Memory | Disk | Traffic | Price | |
|---|---|---|---|---|---|
| `micro_3_0` | 1 GB | 40 GB | 2 TB | **US$ 7/month** | The floor. Works with the swap the installer adds |
| `small_3_0` | 2 GB | 60 GB | 3 TB | US$ 12/month | Headroom for an account with a lot of history |
| `medium_3_0` | 4 GB | 80 GB | 4 TB | US$ 24/month | Only if the machine runs other things too |

- **The price is the same in every region**, São Paulo included. EC2 in São
  Paulo costs about 60% more than in the US; Lightsail does not.
- **New accounts get 3 months free** on the US$ 5, US$ 7 and US$ 12 Linux plans
  (one per account). After that, the regular price applies.
- **Do not pick an "IPv6-only" plan**, cheaper as they are. The installer needs
  a public IPv4 for the `sslip.io` hostname and the certificate.
- Once the gateway is running, the stack uses about 300 MB of RAM. On a 1 GB
  machine the installer adds 2 GB of swap by itself, to get through the spike
  of the first history sync.

**Media is not stored on the server.** When a tool asks for a voice note or a
photo, the gateway fetches it from WhatsApp at that moment. There is no S3 or
extra storage to pay for.

## Which region

**A regular AWS account.** Pick the region closest to you and to the people you
talk to. Every message ends up on that disk, so the region also decides which
jurisdiction your conversations sit under.

**A new account from [builder.aws.com](https://builder.aws.com/)** — the "new
AWS experience", with Google, GitHub or Apple sign-in and "projects":

- The project's region is **fixed**, set from the account's contact address.
  The machine cannot be created anywhere else. Find yours under *AWS Settings →
  View all projects → Overview → Additional info → Region*.
- Lightsail is included in that experience's Free Tier.
- If you set a **spend limit** (from US$ 20), AWS **pauses the project** when
  spending reaches it, and your WhatsApp goes offline with it. Keep the limit
  well above the plan's price.

## Path 1: through the AWS website

1. In the [Lightsail console](https://lightsail.aws.amazon.com/), click
   **Create instance**.
2. Pick the region, **Linux/Unix**, **OS Only** and **Ubuntu 24.04 LTS**.
3. Pick the plan (US$ 7 or US$ 12, see the table above), name the instance and
   click **Create instance**.
4. **Pin the IP before installing.** On the instance's *Networking* tab, click
   **Attach static IP**. A Lightsail public IP changes every time the machine
   is stopped and started, and the panel's address is derived from it. A
   static IP is free while it is attached to an instance.
5. Still under *Networking*, in the IPv4 firewall, **add the HTTPS rule
   (port 443)**. Lightsail opens 22 and 80 by default, but not 443.
6. Click **Connect using SSH**. A terminal opens in the browser. Paste:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash
   ```

7. At the end the installer prints a `https://…sslip.io/setup?token=…` link.
   Open it and follow the wizard, as in the [README](../README.en.md#3-open-the-link-and-follow-the-wizard).

## Path 2: with an AI agent that runs commands

If you use Claude Code, Codex or another agent that runs commands on your
computer, it can do all of this through the AWS CLI. You only need to sign in
through the browser.

### Signing in

On a new builder.aws.com account, the project page has an **agent setup
prompt**. Paste it into your agent: it installs the AWS CLI and signs you in.
The prompt also carries the project's region.

On a regular account:

```sh
aws login --region sa-east-1 --profile whatsapp-mcp
```

This opens the browser for you to sign in. The credentials last 12 hours and
renew by themselves for up to 90 days. If the sign-in page shows *"Something
went wrong… appeal system"*, open the link in a regular browser, without a
private window, a VPN or an embedded browser. If that does not help, run the
command in your own terminal with `--remote`. That mode shows a code on the
page for you to paste into the terminal.

### Creating the machine

The commands below take `AWS_REGION` and `NAME` as variables. On a
builder.aws.com account, `AWS_REGION` has to be the project's region.

```sh
export AWS_PROFILE=whatsapp-mcp AWS_REGION=sa-east-1 NAME=whatsapp-mcp

aws lightsail create-instances --instance-names "$NAME" \
  --availability-zone "${AWS_REGION}a" \
  --blueprint-id ubuntu_24_04 --bundle-id micro_3_0

until [ "$(aws lightsail get-instance-state --instance-name "$NAME" \
           --query state.name --output text)" = running ]; do sleep 5; done

# Pin the IP before installing: the panel's address is derived from it.
aws lightsail allocate-static-ip --static-ip-name "$NAME-ip"
aws lightsail attach-static-ip --static-ip-name "$NAME-ip" --instance-name "$NAME"

aws lightsail put-instance-public-ports --instance-name "$NAME" --port-infos \
  '[{"fromPort":22,"toPort":22,"protocol":"tcp"},
    {"fromPort":80,"toPort":80,"protocol":"tcp"},
    {"fromPort":443,"toPort":443,"protocol":"tcp"}]'
```

### Running the installer

The instance uses the region's default key pair. Download the key and connect
over SSH. In our tests, `sshd` refused the short-lived certificate from
`get-instance-access-details`, so use the default key:

```sh
aws lightsail download-default-key-pair --query privateKeyBase64 --output text > lightsail.pem
chmod 600 lightsail.pem
IP=$(aws lightsail get-static-ip --static-ip-name "$NAME-ip" --query staticIp.ipAddress --output text)

ssh -i lightsail.pem -o StrictHostKeyChecking=accept-new ubuntu@"$IP" \
  'curl -fsSL https://raw.githubusercontent.com/BrOrlandi/whatsapp-mcp/main/install.sh | sudo bash'
```

The installer's last screen carries the `/setup?token=…` link. The agent hands
it to you to open. From there, the panel's wizard does the rest.

`lightsail.pem` gives full access to the machine. Keep it somewhere safe or
delete it when you are done. You can download it again whenever you need it.

## Costs that catch people out

- **Stopping the instance does not stop the bill.** Lightsail charges for a
  stopped instance too. To stop paying, delete it.
- **A detached static IP is charged.** It is only free while attached to an
  instance. When you delete the machine, release the IP as well.
- **Snapshots are charged** per GB stored, per month.
- Going over the plan's traffic allowance is charged per GB. A WhatsApp account
  comes nowhere near 2 TB.

## Deleting everything

```sh
aws lightsail delete-instance --instance-name "$NAME" --force-delete-add-ons
aws lightsail release-static-ip --static-ip-name "$NAME-ip"
# The default key pair costs nothing; delete it only if nothing else uses it:
aws lightsail delete-key-pair --key-pair-name LightsailDefaultKeyPair \
  --expected-fingerprint "$(aws lightsail get-key-pairs --include-default-key-pair \
     --query 'keyPairs[?name==`LightsailDefaultKeyPair`].fingerprint' --output text)"
```

Deleting the instance deletes the messages with it. To keep anything, take the
backup described in [self-hosting.md](self-hosting.md#backups) first.
