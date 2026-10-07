# Webhooks

A webhook is an address that receives a `POST` for every new message, so a
script of yours can act on it as it arrives — reply, log, notify somewhere
else — instead of asking the MCP tools. The tools only answer when asked; the
server's MCP instructions point a client here when the user wants something to
happen on its own.

They are configured in the control panel, under **Configurações › Webhooks**:
add, test, turn off and delete each one, and see its last delivery. The
**Documentação** button there opens `/webhooks/documentacao`, with an example of
every kind of delivery, the fields, how to check the signature and a minimal
receiver in Python. The deliveries are the same as WhatsApp MCP Local's, so a
script written for one works with the other.

## What a webhook receives

| Setting | Meaning |
|---|---|
| `url` | The address, `http://` or `https://`. The **gateway's server** posts to it, so it must be reachable from there — a public address, or one on the server's own network |
| `events` | `message` (new messages, the default), `reaction` (reactions) and `receipt` (the account's messages delivered, read or played) |
| `include_own` | Also deliver the messages the account itself sends, from the phone or through the tools |
| `chats` | Only these conversations (JIDs); empty means all of them |
| `enabled` | Off without deleting it |

Each delivery is a JSON `POST` with these headers:

- `X-WhatsApp-MCP-Event`: `message`, `reaction` or `receipt`;
- `X-WhatsApp-MCP-Delivery`: the delivery's id;
- `X-WhatsApp-MCP-Signature`: `sha256=` followed by the HMAC-SHA256 of the
  body with the webhook's secret, in hex. Check it before trusting the body.
  The secret is shown once, when the webhook is created.
- `User-Agent`: `whatsapp-mcp/<version>`.

```json
{
  "event": "message",
  "delivery_id": "9f2c41d07a3b8e65",
  "sent_at": "2026-10-07T12:00:01Z",
  "instance_id": "8f4c…",
  "message": {
    "id": "3EB0C767D26A1D8B1E",
    "chat_jid": "5511912345678@s.whatsapp.net",
    "chat_name": "Maria Silva",
    "group": false,
    "timestamp": "2026-10-07T12:00:00Z",
    "from_me": false,
    "sender_jid": "5511912345678@s.whatsapp.net",
    "sender_name": "Maria Silva",
    "text": "Oi! Você vem amanhã?",
    "reply_to": { "id": "3EB0A1…", "text": "Combinado então" }
  }
}
```

`instance_id` names the WhatsApp instance the message belongs to, since one
server can hold several; it is the only field Local does not send. A message
with media carries `media` (`type`, `mime_type`, `filename`, `bytes`,
`caption`), not the file: a script that needs it asks for it through the MCP
(`download_media`). An edited message arrives again with `edited: true` and the
new text; one deleted for everyone with `revoked: true`. A reaction carries
`reaction` (`to`, `emoji`, empty when removed); a receipt, `receipt`
(`message_ids`, `type`: `delivered`, `read`, `read-self`, `played`). Only new
messages are delivered: a history sync is not.

## Delivery

Each webhook has its own queue, delivered in order. Any `2xx` answer counts as
delivered; answer within 3 seconds and do the heavy work afterwards. A failed
delivery is retried at growing intervals, up to 10 times in under a minute,
with the later messages waiting. On the 10th failure the webhook is **turned
off** and its pending queue **discarded**; the panel shows why, and turning it
back on starts with an empty queue. **Testar** sends one test delivery (with
`"test": true`) that does not count towards turning it off.

The queues live in the gateway's memory: a restart of the gateway drops what
was still waiting. Messages that arrive while the gateway is down reach the
index when it comes back (RabbitMQ holds them), and are delivered then.

## Where it happens

The RabbitMQ consumer hands every event to the webhook manager after the
database transaction that indexes it commits (`internal/rabbit`), so a webhook
never hears about a message the index does not hold. `internal/webhook` turns
the Evolution event into the delivery, queues, signs and retries. Read
receipts come from the `receipt` queue, which needs `READ_RECEIPT` in
Evolution's `AMQP_GLOBAL_EVENTS` (the stack's `docker-compose.yml` sets it).

The panel routes (`/api/webhooks…`) need the panel login and refuse requests
from another site. Whoever can log into the panel can point the server at any
address its network reaches, which is the same trust the panel already gives
the administrator.
