# Task notifications

The `notify_user` tool sends one direct message through the configured Slack bot.
Kandev exposes it as `kandev_kandev_plugin_slack_notify_user` on the `kanban-task` and `office-task` surfaces.
Some clients add a server prefix to this name.

## Compatibility and setup

The minimum host and SDK version is **Kandev 0.88.0**.
That release includes `agent_tools`, `AgentToolPlugin`, and verified task/session/workspace context.
The SDK remains part of the Kandev monorepo. CI uses its `v0.88.0` tag.
The plugin requires no additional host capability beyond its existing `state` permission.

1. Use Kandev 0.88.0 or later on the host and its matching agentctl on executors.
2. Add the `im:write` bot scope from the updated Slack app manifest.
3. Reinstall the Slack app into its workspace to grant the new scope.
4. Keep the existing `chat:write` scope and vault-backed `xoxb-` bot token.
5. Check tool discovery from a task session before you enable an automation.

[`conversations.open`](https://docs.slack.dev/reference/methods/conversations.open/) opens the DM with `im:write`.
[`chat.postMessage`](https://docs.slack.dev/reference/methods/chat.postMessage/) sends the message with `chat:write`.
The tool needs no app token, cookie, user token, or agent credential.
Inbound triage still uses its existing configuration and Socket Mode or browser fallback.
Browser-session credentials alone cannot send tool notifications.

The Slack settings card shows these scope requirements for new and existing installations.
The **Test authentication** button checks saved credentials only.
It does not check DM permissions or send a message. Successful authentication does not prove DM delivery.

SSH executors use the normal task MCP connection to the host.
The managed plugin performs Slack requests on the host.
The bot token stays inside the plugin process and the host vault.
The plugin never includes credentials or raw Slack errors in notification results or records.

## Caller responsibilities

The automation selects cards, maps assigned humans to Slack IDs, and builds the message.
It must skip unassigned cards and users without a mapping.
The tool does not scan cards or validate assignment.
The message must include the intended card URL when a notification needs one.

A call has exactly three arguments:

```json
{
  "recipient": "U01234567",
  "message": "Review needed: Fix login redirect https://kandev.example/tasks/123",
  "idempotency_key": "task-123:waiting-event-456"
}
```

| Argument | Rule |
| --- | --- |
| `recipient` | One `U` or `W` Slack user ID, followed by 8–31 uppercase letters or digits. No channel IDs or lists. |
| `message` | Nonblank UTF-8 text, at most 4000 bytes. No `@`, `<`, `>`, or control characters except newline and tab. |
| `idempotency_key` | 1–128 ASCII characters. First character: letter or digit. Remaining characters: letters, digits, `.`, `_`, `:`, `/`, `-`. |

Plain URLs are permitted. Slack markup, mentions, automatic parsing, and link previews are disabled.
The plugin accepts only a DM channel from Slack before it sends a message.

The host derives the caller workspace from the running task session.
Arguments cannot override workspace, task, or session identity.
A notification key belongs to that workspace and recipient, across caller tasks and sessions.
A repeated key with different message content returns `key_conflict` and sends nothing.

Use the same key for the same waiting event across scheduled scans.
A new waiting event needs a new key.
Random keys on each scan defeat duplicate suppression.

## Results and retries

Every result includes `recipient`, `status`, `code`, `detail`, and `duplicate`.
A confirmed delivery also includes `channel` and `message_ts`.
A rate-limit result includes `retry_after_seconds` when Slack supplies a valid delay.
Only `delivered` returns `IsError: false`.

Host limitation: Kandev 0.88.0 and the tested current host omit native `structuredContent` for error results.
The plugin also returns the complete result as JSON text, so callers retain all fields.
Preserving native structured error results requires a separate Kandev host change.
A delivered result means Slack accepted the message. It does not mean the person read it.

| Status | Meaning | Action |
| --- | --- | --- |
| `delivered` | Slack returned a valid receipt. | Keep the key. Repeats return the receipt with `duplicate: true`. |
| `failed` | No message send occurred, or Slack explicitly rejected it. | Correct the cause. For rate limits, wait at least the returned delay. Use a new key for an intentional retry. |
| `uncertain` | The send or saved receipt is unconfirmed. | Check Slack before any new attempt. Never automatically replace the key. |
| `rejected` | Validation, context, configuration, or state access prevented this call from sending. | Correct the cause, then repeat the same key and content. |

Known failures include revoked credentials, missing scopes, disabled users, unreachable DMs, and rate limits.
The plugin stores failed outcomes too. Repeats return the same failure without another Slack call.
An unknown error during `chat.postMessage` is uncertain, even when Slack returns HTTP 200.
HTTP 5xx responses, timeouts, dropped connections, and incomplete receipts are also uncertain after a send starts.

## Durable state and limits

Before Slack requests, the plugin saves a pending record through public Host state.
The state key contains a SHA-256 digest of the caller workspace, recipient, and notification key.
The record contains a content digest and delivery result. It does not contain the message body or bot token.

A single cancellable gate serializes notification calls within the managed plugin process.
Kandev supervises one process per plugin installation.
This design does not support multiple independent hosts sharing one plugin state store.
The public state API provides no cross-process compare-and-swap operation.

A restart or upgrade retains records. An unfinished pending record blocks repeats and reports `uncertain`.
A crash before Slack receives the message can therefore suppress an undelivered notification.
A failed receipt save also reports `uncertain`, even when Slack returned a timestamp.
This design favors duplicate suppression over automatic recovery. It does not promise exactly-once delivery.

Records have no automatic expiry. Storage grows with the number of unique notification keys.
Uninstalling the plugin, deleting its state, or restoring an older backup can remove duplicate protection.
Token rotation does not clear records. Changing the connected Slack workspace requires a review of user mappings and keys.

The host limits each tool call to 30 seconds.
The plugin allows up to 20 seconds for Slack requests and up to 3 seconds to save the result.
A call that expires while it waits for the gate sends nothing.
The plugin never sleeps through a rate limit and never automatically retries a Slack request.

## Validation

Run these commands with a sibling Kandev checkout at `v0.88.0`:

```bash
make test vet
go test -race ./server
make package verify-package
```

Notification tests use a local Slack stub and disk-backed Host state.
They cover concurrent calls, process-state reconstruction, payload conflicts, workspace/recipient isolation, permissions, redaction, and uncertain outcomes.
The existing inbound tests continue to cover Socket Mode and browser fallback.

See [Validation evidence](notification-validation.md) for the tested package and local/SSH runtime checks.

Use a disposable Kandev instance for package installation and task MCP discovery.
Do not use production credentials for that check.
Keep production notification automations paused until a released package passes delivery and duplicate checks in the target environment.
