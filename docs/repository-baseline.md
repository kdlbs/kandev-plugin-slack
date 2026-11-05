# Repository baseline

## Public SDK and runtime floor

The plugin pins `github.com/kdlbs/kandev/pluginsdk v0.1.0`. Go compilation,
tests, vet, and the compatibility checker resolve the public module without a
Kandev checkout, local replacement, or workspace override. The plugin module
does not import backend internals or generated protobuf packages directly.

The installed SDK's `sdk-release.json` records SDK `0.1.0`, host release
`0.99.0`, minimum Kandev version `0.99.0`, and supported API versions `[1, 2]`.
The manifest therefore requires Kandev `0.99.0` and retains API `1`. The SDK
version is independent of the plugin's `0.3.0` version and the host version.
Existing binaries retain their original host compatibility; newly built
packages use the declared SDK-wide floor.

The settings page uses `plugin-settings`. This migration preserves the plugin
ID, author, API version, config/state keys, capabilities, permissions, task
notification tool, Socket Mode, browser-session fallback, and UI bundle. API v1
webhook access defaults remain compatible; an API v2/access change is separate.

## Standalone source checks

Use Go 1.26.0 or a compatible later Go 1.26 patch, Git, and Make:

```sh
go mod download
go mod tidy
git diff --exit-code -- go.mod go.sum
make sdk-check check-format vet test build
```

`sdk-check` runs the pinned public checker after dependencies are installed:

```sh
go run github.com/kdlbs/kandev/pluginsdk/cmd/kandev-sdk-check@v0.1.0 --manifest manifest.yaml
```

The frontend is a checked-in JavaScript bundle using host-owned React/UI. There
is no `package.json`, npm SDK dependency, lockfile, UI test, or typecheck command.
Do not add an npm dependency merely to run the Go-only compatibility check.

Upgrade SDK dependencies deliberately: install the exact new version, regenerate
module/lock files as applicable, inspect the installed release records, update
the host floor, rerun the compatibility checker and tests, and smoke test the
new package on that minimum host. Go/npm SDK releases share independent
versioning even though this plugin only consumes Go.

## Packaging-only tooling input

`plugin-pack` belongs to the backend. `.kandev-tooling-ref` separately pins its
source to `f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb` (the v0.99.0 release
commit). Packaging still requires this explicit checkout; SDK compilation does
not make packaging checkout-free.

```sh
git clone --filter=blob:none --sparse https://github.com/kdlbs/kandev.git /tmp/slack-packaging-tools
git -C /tmp/slack-packaging-tools sparse-checkout set apps/backend pluginsdk
git -C /tmp/slack-packaging-tools checkout --detach "$(cat .kandev-tooling-ref)"
make verify-package-host KANDEV_TOOLING=/tmp/slack-packaging-tools
make verify-package KANDEV_TOOLING=/tmp/slack-packaging-tools
```

`KANDEV_TOOLING` defaults to `../kandev` for existing local layouts. Packaging
checks that checkout's exact commit and runs the tool in the backend's own Go
module. The backend's internal SDK replacement requires the root `pluginsdk`
directory; this does not change the plugin's public module graph.

The all-platform archive contains the manifest, Slack app manifest, UI bundle,
marketplace icon/notice, README, documentation, and all five declared binaries.
The verifier checks exact inventory and SHA-256 coverage for every file. The
host-only packager normalizes YAML and narrows runtime declarations to the host
platform; verifier fixtures cover both normalized and original manifests.
`make test` includes package/release verifier negative tests.

## Minimum host and CI

CI's source job installs the public SDK and runs tidy, compatibility, formatting,
vet, and tests without a Kandev source checkout. Package/build/release jobs
retain only the pinned packaging input. Their sparse checkout includes both
`apps/backend` and `pluginsdk`.

Minimum-host jobs download the official v0.99.0 Linux x64 runtime, verify its
published SHA-256, construct a host package, then run:

```sh
make test-minimum-host KANDEV_HOST_BIN=/path/to/kandev/bin/kandev
```

The script insists on v0.99.0 and uses a temporary home, database, and loopback
port. It checks package admission, API v1 subprocess startup, Host state/config
roundtrips, status/test/scan webhooks, invalid route/method handling, bundle
serving, config restart, disable/re-enable with config retention, and uninstall.
It uses no Slack credentials. It does not prove live Slack authentication,
Socket Mode delivery, task-agent MCP routing, SSH, or browser rendering. The Go
suite covers triage and notification delivery with fake Host/HTTP fixtures;
those checks are separate from released-host smoke evidence.

The old `kandev_min_host` fake and SDK substitution checks were removed: source
compilation against an older backend SDK does not exercise an installed plugin
on its declared minimum runtime.

## Releases

The existing manual and tag release guards remain: validate before committing
release metadata, tagging, or uploading assets. This migration does not bump or
publish the plugin, modify the catalog, or authorize merging. Historical
notification validation is dated in `notification-validation.md`; it is not
current migration evidence. See the migration PR for exact commands and results.
