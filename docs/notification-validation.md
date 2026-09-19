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

## Remaining deployment validation

No real Slack workspace participated in these tests.
Actual Slack scope grants, user reachability, successful DM delivery, and repeated-call suppression need a test with the released plugin.
The change does not release a package, install into production, or enable a production automation.
