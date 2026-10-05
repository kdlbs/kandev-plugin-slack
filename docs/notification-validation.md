# Notification validation

Validation date: 2026-09-19.

## Build and tests

The plugin builds against Kandev SDK `v0.88.0`, commit
`cab9eaf19d997bb4c8020dd263ddc60d5b035b64`, with Go 1.26.0.

The following checks passed:

```bash
make test vet
go test -race ./server -timeout 60s
go mod tidy
git diff --exit-code -- go.mod go.sum
test -z "$(gofmt -l ./server)"
make package verify-package
```

The archive contains Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 binaries.
Package verification checked all five executables and every file checksum.

The notification tests use HTTP Slack fixtures and JSON files for durable Host state.
They cover recipient/content validation, successful sends, concurrent duplicates, restart recovery, workspace/recipient isolation, and payload conflicts.
They also cover rate limits, revoked credentials, permissions, disabled users, credential redaction, failed state writes, dropped connections, and timeouts.

## Disposable host and SSH checks

Runtime host: Kandev `v0.95.0-5-gd355a672b1`, commit
`d355a672b1db624048f8ff87bc456a1729f78cb0`.

The test used a separate `KANDEV_HOME_DIR`, database, plugin directory, and backend port.
The five-platform archive installed successfully through `/api/plugins/install`.
A local task and an SSH task ran the standard mock agent.
The SSH target used the repository's `kandev-sshd:e2e` image, with agentctl uploaded by Kandev.

Both task MCP endpoints passed the same check:

```bash
python3 scripts/check-notification-tool.py http://127.0.0.1:<task-agentctl-port>/mcp
python3 scripts/check-notification-tool.py http://127.0.0.1:<ssh-forward-port>/mcp
```

Observed output:

```text
Discovered: kandev_kandev_plugin_slack_notify_user
Managed plugin invocation: invalid_arguments (no Slack request)
Host schema rejected channel recipients and workspace overrides
```

A separate valid-argument call returned `bot_not_configured` from the plugin on both executors.
No Slack token existed in these test instances.
The checks therefore prove package installation, discovery, routing, validation, and error propagation without sending external messages.

A prepared session without a started agent did not have a backend MCP stream.
The discovery check passed after the task agent started.

## Host compatibility finding

The tested host drops native `structuredContent` when a plugin sets `IsError`.
The plugin returns complete JSON fallback text to preserve recipient, status, duplicate, and retry fields on these hosts.
A separate Kandev fix preserves native structured fields alongside the error flag.
The plugin works with either behavior.

## Stable host release smoke

Validation date: 2026-10-05.

Kandev `v0.97.0` was published on 2026-10-04 at release commit
`e43881c7555372897b57ec51c705f1e05da43c40`. The official
[`kandev-linux-x64.tar.gz`](https://github.com/kdlbs/kandev/releases/tag/v0.97.0)
asset passed its published SHA-256 check. The extracted executable reported
`v0.97.0`, and the isolated runtime returned HTTP 200 from `/health` with
`version: v0.97.0`.

The host-only Slack package from this PR installed into that disposable Linux
amd64 host. The package identity remained `kandev-plugin-slack` version
`0.2.1`, with `min_kandev_version: 0.88.0`. The exact PR head and archive
SHA-256 are recorded in the PR validation notes.

The installed settings page was exercised in desktop Chromium 154 at
1440×1000 and Playwright's Pixel 7 phone profile at 412×915 with touch enabled
and a coarse pointer. The status card rendered as “Not configured”; all four
Slack secret fields were empty. Desktop keyboard navigation reached
“Test authentication” with a visible focus ring and activated it with Enter.
That unconfigured test returned HTTP 400, while “Scan now” returned HTTP 202
with `scheduled: true`. The phone profile had no horizontal overflow and its
touch activation returned the same unconfigured result. No browser request to
Slack was observed.

For configuration lifecycle coverage, a synthetic utility-agent record in the
disposable host was selected and the non-secret fallback command prefix was
changed from `!kandev` to `!stable-smoke`. The config PATCH returned HTTP 200
and the plugin subprocess restarted. No Slack app token, bot token, session
token, or session cookie was set. The plugin stayed unconfigured. The plugin
was then disabled and re-enabled through Settings; its subprocess stopped and
started again, the row returned to Active, and the settings status card
rendered after re-enable. The host continued to report `v0.97.0`.

The v0.97.0 host log emitted its API v1 deprecation warning: API v1 uses the
deprecated public-webhook-access default. This PR preserves API version 1 and
its existing webhook behavior; migration to explicit access or API v2 is a
separate compatibility change.

The Pixel 7 result is browser device emulation, not a physical phone test. No
real Slack account, credential, workspace, or message was used. Real workspace
scope grants, Socket Mode authentication, DM delivery, and repeated-call
suppression remain unvalidated. No production host or automation was changed,
and no plugin release was published.
