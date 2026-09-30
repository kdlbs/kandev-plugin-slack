# Slack plugin for Kandev

Turn Slack conversations into Kandev tasks. Mention the bot or use `/kandev`, and the plugin asks your selected utility agent where the task belongs.

The plugin reads the relevant Slack conversation, creates a task, and replies with the task link. The Slack app uses Socket Mode, so Kandev does not need a public Slack webhook URL.

```text
You:    @Kandev the Safari login redirect loops for SSO users
Kandev: Filed this in Platform > Engineering > Backlog as
        "Fix SSO login redirect loop on Safari". PLAT-482
```

This example is illustrative. No live Slack workspace participated in plugin validation.

## Requirements

- Use Kandev `0.88.0` or later. Task notifications need its agent tool API.
- Use a host that supports Linux amd64/arm64, macOS amd64/arm64, or Windows amd64 plugin binaries.
- Use a Slack workspace that permits custom apps, or use the unofficial browser-session fallback.

The plugin requests these Kandev capabilities:

| Capability | Use |
| --- | --- |
| Read workspaces, workflows, and repositories | Show the utility agent the current task topology. |
| Create tasks | Add the triaged task to Kandev. |
| Invoke an agent | Ask the selected utility agent for one placement decision. |
| Store secrets | Read Slack credentials for API calls. |
| Store state | Resume fallback polling and keep notification outcomes across restarts. |

The Slack app needs these bot scopes:

| Scope | Use |
| --- | --- |
| `app_mentions:read` | Receive `@Kandev` mentions. |
| `channels:history`, `groups:history` | Read public and private channel context. |
| `chat:write` | Reply to mentions and slash commands. |
| `im:write` | Open a direct message for task notifications. |
| `reactions:write` | Add the `:eyes:` acknowledgement during triage. |
| `commands` | Receive `/kandev`. |

Socket Mode also needs an app-level token with `connections:write`.

## Install the plugin

1. Download `kandev-plugin-slack-<version>.tar.gz` from a GitHub Release.
2. Open **Settings > Plugins** in Kandev.
3. Install the package archive.
4. Open Slack settings and select a triage agent.

The package includes the five platform binaries, plugin manifest, Slack app manifest, UI bundle, icon, README, and documentation.

## Create and configure a Slack app

1. Open [api.slack.com/apps](https://api.slack.com/apps).
2. Select **Create New App > From a manifest**.
3. Choose your workspace and paste [`slack-app-manifest.yaml`](slack-app-manifest.yaml).
4. Open **Basic Information > App-Level Tokens**.
5. Generate a token with `connections:write`. This token starts with `xapp-`.
6. Install the app to your workspace.
7. Copy the **Bot User OAuth Token** from **OAuth & Permissions**. This token starts with `xoxb-`.
8. Enter both tokens in **Settings > Plugins > Slack**.
9. Select a triage agent and save the settings.
10. Invite the bot to each channel that it must read with `/invite @Kandev`.

Slack applies scope changes after you reinstall the app. The two tokens have different uses. The settings page reports which token needs attention.

![Slack plugin settings](docs/settings.png)

![Slack token validation](docs/validation.png)

The **Listening** status means that the Socket Mode connection is open. The plugin tests saved credentials with Slack `auth.test`. It does not send a test message.

## Trigger task triage

- Mention `@Kandev <request>` in a channel where the bot is a member.
- Use `/kandev <request>` in a channel. The reply is private to you.
- Mention the bot in a thread to include that thread. A channel mention includes about 20 earlier messages.
- Use `/kandev` in a channel where the bot is not a member. The plugin reads recent channel context and replies through the Slack `response_url`.

Kandev invokes the selected utility agent once for each request. The plugin gives it the Slack text and the available workspace, workflow, column, and repository names. The plugin checks the agent's decision against that topology before it creates a task.

The plugin shows repository names to the utility agent. The task API does not accept a repository, so the created task has no repository.

The plugin acknowledges each Socket Mode event before triage and deduplicates requests by channel, timestamp, and instruction. Routine disconnect frames are redialled.

## Browser-session fallback

Use this mode only when your workspace forbids custom Slack apps. Enter a browser session token that starts with `xoxc-` and the `d` cookie from the same logged-in Slack tab.

This mode is unofficial and is not supported by Slack. It polls `search.messages` for your messages that start with the configured command prefix, which defaults to `!kandev`. It does not receive events or send task notifications. Sign-out or cookie rotation stops polling.

If you fill both paths, the plugin always uses the Slack app. Clear the app tokens before you select the browser fallback.

## Task notifications

Agents can call `notify_user` to send one Slack direct message through the configured bot. The call requires Kandev `0.88.0` or later and the `chat:write` and `im:write` scopes. The notification feature stores outcome keys in Kandev state across restarts.

Read [Task notifications](docs/notifications.md) for the tool arguments, results, retry limits, and validation details.

## Data and credentials

The plugin reads Slack content after a mention, slash command, or fallback poll. It sends the selected conversation text and Kandev task topology to the selected utility agent.

Kandev marks Slack tokens and the fallback cookie as secret settings. The plugin process receives their cleartext values while it runs. Only invite the bot to channels that it must read.

The browser-session fallback uses a user session token and cookie. Treat both values as passwords. Do not use this mode with a personal account unless your workspace permits it.

## Troubleshooting

| Problem | What to do |
| --- | --- |
| Socket Mode is not listening | Make sure that the app token starts with `xapp-`, has `connections:write`, and Slack Socket Mode is enabled. |
| Slack rejects a token | Put the `xapp-` token in the app-level field and the `xoxb-` token in the bot field. |
| Slack reports a missing scope | Add the scope to the Slack app. Then reinstall the app. Save the new bot token if Slack issued one. |
| The bot misses a mention | Invite it to the channel and make sure that the app manifest includes `app_mentions:read` and the needed history scopes. |
| Fallback polling stops | Copy a new `xoxc-` token and `d` cookie from the same signed-in browser session. |
| A task notification fails | Add `chat:write` and `im:write` to the Slack app. Then reinstall the app. Read the notification result before retrying. |
| Kandev rejects a package as already installed | Uninstall the existing plugin before you install a local package with the same ID and version. |

## Develop and package

Use Go `1.26.0`, Git, and Make. The UI bundle is checked into `ui/bundle.js`. This repository has no separate npm test, typecheck, or build command.

The Go module uses `../kandev/apps/backend`. Create a sibling Kandev checkout at the fixed source pin:

```sh
git clone https://github.com/kdlbs/kandev.git ../kandev
git -C ../kandev checkout --detach "$(cat .kandev-sdk-ref)"
make check-sdk-pin
```

Run the local checks from this repository root:

```sh
make check-format
go mod tidy
git diff --exit-code -- go.mod go.sum
make vet
make test
make build
make verify-package-host
make verify-package
```

`make test` runs the Go tests and negative tests for package and release verification. `make verify-package-host` builds and checks one host archive. `make verify-package` cross-compiles every declared platform and checks the complete package inventory and checksums.

The CI workflows use `.kandev-sdk-ref` for SDK builds. They also test and package against Kandev `v0.88.0`, the existing runtime minimum. See [Repository baseline](docs/repository-baseline.md) for the source pin and CI details.

To install a local package while testing, first uninstall the existing plugin with the same ID and version. Then upload the archive produced by `make package`:

```sh
curl -X DELETE localhost:<port>/api/plugins/kandev-plugin-slack
curl -F package=@kandev-plugin-slack-0.2.1.tar.gz localhost:<port>/api/plugins/install
```

## Release process

Maintainers run the `release` workflow from `master` to select a version bump. The workflow checks the candidate package before it updates `master` or pushes a version tag. A pushed `v*` tag also runs package and version checks before GitHub creates release assets.

**Release hold: do not merge until a stable Kandev release includes PR #3943 and this package has been validated against that release.** Do not dispatch the release workflow while this hold remains active.

## License

This repository does not include a `LICENSE` file. Contact the repository maintainers to confirm reuse terms.
