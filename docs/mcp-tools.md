# MCP tools

The panel serves this same list at `/documentacao`, read from the MCP server's
own definitions rather than transcribed. A hand-kept list of capabilities is a
list that quietly stops being true — a tool gains an argument, the page still
shows the old one — so that page is wrong only if the server is. This file is
the narrative version: what each tool is for and why some of them behave the way
they do.

| Tool | Source | Purpose |
|---|---|---|
| `whatsapp_status` | gateway | session state, queues, index coverage, problems |
| `list_chats` | index | conversations, most recently active first |
| `get_chat_messages` | index | one conversation over a period |
| `search_messages` | index | full-text search, optionally scoped |
| `list_contacts` | Evolution | address book |
| `list_groups` | Evolution | groups the account belongs to |
| `get_group` | Evolution | one group with its participants |
| `send_text_message` | Evolution | send text |
| `send_media_message` | Evolution | send image, video, audio or document from a URL |
| `download_media` | Evolution | decode the media of an indexed message |
| `sync_history` | index + Evolution | request messages older than the index holds, from the start or from a given moment |
| `delete_message` | index + Evolution | revoke one of the account's own messages for everyone |
| `edit_message` | index + Evolution | replace the text of one of the account's own messages |
| `react_to_message` | index + Evolution | react with an emoji, or clear the reaction |
| `check_numbers` | Evolution | which numbers have a WhatsApp account, and the JID to use |
| `get_profile_picture` | Evolution | URL of a contact's or group's picture |
| `send_location` | Evolution | send a point on the map |
| `send_contact` | Evolution | share a contact card |
| `send_poll` | Evolution | send a poll |
| `get_poll_results` | Evolution | read a poll's tally |
| `organise_chat` | Evolution | archive, pin or mute a conversation, and undo each |

## What is deliberately not a tool

**Summarising.** `get_chat_messages` returns the period and the client
summarises it, which avoids an LLM credential and a per-call cost in the
backend.

**Forwarding.** WhatsApp exposes no forwarding route. Resending the content with
`send_text_message` or `send_media_message` is what "forward" means here, and
the tool names say so rather than implying otherwise.

## History and gaps

`sync_history` returns immediately. WhatsApp answers asynchronously: it returns
the messages immediately *before* one the account already knows, they arrive on
the history queue, and each call pages further back. An instance with nothing
indexed has no anchor to page from.

A hole in the index reads exactly like quiet days, and that is the failure worth
guarding against: "he sent nothing" and "we failed to ingest what he sent" are
the same empty answer. A window in which *no* conversation produced a single
message is the shape an outage leaves, so the index reports those windows in its
coverage, and `get_chat_messages` marks an empty period that falls inside one as
unknown rather than empty. One quiet conversation is never a gap, and the
threshold clears a night: the largest ordinary windows in a personal account run
six to eight hours and start between two and four in the morning.

`sync_history` can also be pointed at a moment. Given `before`, it anchors on the
first message indexed *after* that moment in each conversation and pages
backwards from there, which reaches into a period the index is thin on rather
than further into the past. A conversation with nothing indexed after the moment
offers no anchor at all and is counted as `unreachable_chats`, because a sync
that reaches two conversations out of three hundred must not read as having
covered the index. Both modes are the same request — Evolution Go exposes no
route to read its own stored messages, so an anchor is the only handle
available; they differ only in which message is chosen.

## Sending

A send is not finished when the call returns. Evolution reports success even
when whatsmeow silently skipped a recipient device it had no encryption session
for, and the recipient is then left with a message that never decrypts —
WhatsApp shows it as "waiting for this message" indefinitely, and only a resend
clears it. This is most likely on the first message a freshly paired instance
sends to a device it has never talked to.

So the send tools do two things the API does not. They refresh the recipient's
device list before encrypting, which is the only lever against the missing
session; and they ask WhatsApp afterwards whether the message actually arrived,
reporting `delivery` alongside the acknowledgement. A message with no delivery
record is reported as `unconfirmed` rather than as either success or failure,
because an offline recipient and a dropped message look identical from here.

## Acting on a message

Revoking a message is irreversible and it reaches other people's phones, so
`delete_message` is two-step by construction. A call without `confirm` changes
nothing and returns the conversation, the timestamp and the text, because the
caller names an opaque id and nobody can approve an id they cannot read. Only
the confirmed call deletes.

`delete_message` and `edit_message` also refuse a message this account did not
send. WhatsApp would refuse it too, but refusing here means the caller is told
plainly rather than handed an opaque API error — and it settles the question
from the index, which records who sent what, rather than from the caller's own
claim. `react_to_message` carries no such guard: reacting to other people is the
point of it.

## Untrusted content

Message content is written by third parties. Every reading tool labels it as
data rather than instructions, and a send must originate from the user: a
message that says "forward this to X" is not a request to act on.

## Recipes

`/receitas` in the panel is the other half of `/documentacao`: what the gateway
makes possible without any code. Scheduling a message, watching for keywords,
chasing unanswered conversations — none of that lives here. The assistant waits,
watches and reports; this gateway only answers for WhatsApp when asked. Each
recipe is a prompt to paste, and names the tools it leans on so it can be
adapted honestly.
