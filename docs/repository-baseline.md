# Repository baseline

This document records the SDK source, runtime floor, package checks, and release process for this plugin.

## SDK source and runtime floor

`.kandev-sdk-ref` pins the Kandev source checkout used by CI and package builds to `570600439036e81f8e9e1c63f15c4abce8a6c846`. That commit merged [Kandev PR #3943](https://github.com/kdlbs/kandev/pull/3943), which adds the host Action API.

The source pin is separate from the runtime minimum in `manifest.yaml`. The plugin keeps its existing `min_kandev_version: "0.88.0"`, which provides the agent tool API used by task notifications. CI and release validation also build and package against the `v0.88.0` host tag. The source pin does not raise the runtime minimum.

The Slack settings page uses the `plugin-settings` slot. This change does not add an Action or change plugin registrations. It keeps the plugin ID, API version, config keys, state, capabilities, permissions, Socket Mode behavior, browser-session fallback, and localization unchanged.

## Local SDK checkout

The Go module uses a relative replacement at `../kandev/apps/backend`. Create a Kandev checkout beside this repository, then select the source pin:

```sh
git clone https://github.com/kdlbs/kandev.git ../kandev
git -C ../kandev checkout --detach "$(cat .kandev-sdk-ref)"
make check-sdk-pin
```

Use Go `1.26.0`. Do not use a moving branch for the SDK checkout. CI reads `.kandev-sdk-ref` and checks out that exact commit for backend tests and package builds.

## Go module tidy

Run `go mod tidy` with the fixed source checkout and Go `1.26.0`. It updates the plugin module to the dependency versions required by that Kandev source: Gorilla WebSocket `v1.5.4-0.20250319132907-e064f32e3674`, `x/net` `v0.58.0`, `x/sys` `v0.48.0`, `x/text` `v0.42.0`, genproto RPC `v0.0.0-20260526163538-3dc84a4a5aaa`, and gRPC `v1.83.1`.

The gRPC and genproto updates overlap open Slack PR #3. This baseline includes those tidy results with the other versions that the pinned backend requires. These module versions do not change the runtime host minimum.

## Local commands

Run these commands from the plugin repository root with the source pin checkout:

```sh
make check-format
go mod tidy
git diff --exit-code -- go.mod go.sum
go mod tidy
git diff --exit-code -- go.mod go.sum
make vet
make test
make build
make package-host
make verify-package-host
make verify-package
```

`make test` runs the Go tests and negative tests for package and release verification. `make verify-package-host` checks the current host archive. `make verify-package` builds and checks all five manifest platforms.

CI also runs `make vet-minimum-host` and `make test-go-minimum-host` against Kandev `v0.88.0`. These targets select the test fake that matches that release's non-variadic `InvokeUtilityAgent` interface. They check the same plugin code against the minimum host SDK.

The UI is a single checked-in JavaScript bundle. This repository has no separate UI test or typecheck command. Package verification requires `ui/bundle.js` and rejects any unexpected package files.

## Package contents

The all-platform package contains the manifest, Slack app manifest, UI bundle, marketplace icon and notice, README, documentation, and five platform binaries. Its verifier checks the declared platform set, exact file inventory, and SHA-256 entry for every file.

The package verifier and release-version verifier have negative tests. CI calls them through `make test-package-verifier` and `make test-release-version`, which are also included in `make test`.

## Release process and hold

The `release` workflow accepts a version bump from `master` or a pushed `v*` tag. A manual release builds and checks the candidate package before it commits release metadata or pushes a tag. A pushed tag runs the same package and version checks before GitHub creates release assets. The workflow serializes releases and does not cancel an active release.

**Release hold: do not merge until a stable Kandev release includes PR #3943 and this package has been validated against that release.** Do not dispatch the release workflow while this hold remains active.
