# Public SDK migration validation

Date: 2026-11-05. Executor: Linux amd64, Go `go1.26.6` (module Go `1.26.0`).
Started from current `origin/master` commit
`5144b8aa5df69538f4491524231f56b3b3dae9ae`, with a clean attached task branch.
There was no plugin-local `AGENTS.md` at that commit or the remote default;
focused repository instructions are added here.

## Contract and scope

Read the parent repository's `create-kandev-plugin` workflow, public plugin
[authoring guide](https://github.com/kdlbs/kandev/blob/main/docs/public/plugins-authoring.md),
manifest guide, current template build guidance, and approved
[template migration handoff](https://github.com/kdlbs/kandev/blob/main/docs/plans/plugin-sdk-publishing/template-migration.md).
PR [#4534](https://github.com/kdlbs/kandev/pull/4534) merged on 2026-10-28.
Public SDK publication/consumer verification was confirmed by the user's
[recovery run](https://github.com/kdlbs/kandev/actions/runs/42089596215/attempts/3)
and the [SDK release](https://github.com/kdlbs/kandev/releases/tag/pluginsdk/v0.1.0).

Public module `github.com/kdlbs/kandev/pluginsdk v0.1.0` resolves to source
`f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb`. Its installed `sdk-release.json`
records SDK `0.1.0`, host release `0.99.0`, minimum host `0.99.0`, and API versions
`[1,2]`. This migration uses that minimum and retains API 1 and plugin version
0.3.0. It preserves registrations, capabilities, config/state keys, assets,
Socket Mode, fallback triage, and notification behavior.

No npm/package.json/lockfile/frontend SDK consumer exists. The checked-in
`ui/bundle.js` is unchanged and uses host-provided React/UI. No npm install,
frontend typecheck/test, or lock regeneration command applies. No direct
protocol imports needed migration; SDK-generated protocol dependencies now
resolve under the public SDK path.

## Source and dependency checks — passed

Fresh standalone clone at `/tmp/slack-sdk-migration/standalone/plugin`, with no
`../kandev` checkout and `GOWORK=off`:

```sh
git clone --no-local --branch feature/migrate-slack-to-pub-cfd . /tmp/slack-sdk-migration/standalone/plugin
cd /tmp/slack-sdk-migration/standalone/plugin
GOWORK=off GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org go mod download
GOWORK=off go mod tidy
git diff --exit-code -- go.mod go.sum
GOWORK=off make sdk-check check-format vet test build
GOWORK=off go mod verify
GOWORK=off go list -m all
GOWORK=off go list -deps ./server/...
test ! -e ../kandev
```

Tidy produced no changes; module verification returned `all modules verified`.
`make test` passed the Go suite and package/release verifier tests. The checker
reported `SDK 0.1.0: compatible (minimum Kandev 0.99.0)`. The dependency list
contains `github.com/kdlbs/kandev/pluginsdk` and its
`proto/kandev/plugin/v1` package, with no old backend module or replacements.

Additional checks in the attached worktree:

```sh
go test ./server/...
go vet ./server/...
go test -race ./server/... -timeout 120s
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/ci.yml .github/workflows/build.yml .github/workflows/release.yml
git diff --check
```

All passed. Actionlint was initially resolved from @latest to v1.7.12; the
recorded replay command now pins that version and was rerun during review. New normalized-manifest fixtures
failed first with the expected package-icon and missing archive-version
messages; they pass after the verifier corrections. Existing negative tests
continue to reject missing/corrupt/unexpected files, checksums, wrong platform
sets, and release identity mismatches.

An additional public resolution used a fresh module cache:

```sh
GOWORK=off GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org \
  GOMODCACHE=/tmp/slack-sdk-migration/empty-module-cache \
  go mod download -json github.com/kdlbs/kandev/pluginsdk@v0.1.0
```

Result: release source `f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb`, module sum
`h1:Pov5zE2gGf53FslhpcoGgx6E3nu6kjizT69x3MlEEEs=`, go.mod sum
`h1:V0Y6IoJvwa+OuZdfgD3Ck2EgQut7S3jSu8OLQRo7xxs=`.

## Packaging and released minimum-host smoke

Packaging input is a separate sparse checkout at
`/tmp/slack-sdk-migration/kandev-tooling`, pinned by `.kandev-tooling-ref` to
`f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb`. It includes `apps/backend` and root
`pluginsdk`. `plugin-pack` runs in its own backend module; the plugin still uses
its public SDK dependency. Packaging is explicitly not checkout-free.

From the standalone clone:

```sh
GOWORK=off make verify-package-host KANDEV_TOOLING=/tmp/slack-sdk-migration/kandev-tooling
cp kandev-plugin-slack-0.3.0.tar.gz /tmp/slack-sdk-migration/host-package.tar.gz
GOWORK=off make test-minimum-host KANDEV_TOOLING=/tmp/slack-sdk-migration/kandev-tooling KANDEV_HOST_BIN=/tmp/slack-sdk-migration/host/kandev/bin/kandev
GOWORK=off make verify-package KANDEV_TOOLING=/tmp/slack-sdk-migration/kandev-tooling
sh scripts/verify-release-version.sh v0.3.0 kandev-plugin-slack-0.3.0.tar.gz
tar -tvzf kandev-plugin-slack-0.3.0.tar.gz
sha256sum kandev-plugin-slack-0.3.0.tar.gz
```

Host-only construction, exact file inventory, and every internal SHA-256 passed.
The newer packager normalizes YAML and restricts a host-only manifest to its
single executable; verification now accepts that form and the original form.
An initial cross-build exposed this executor's forced `CGO_ENABLED=1`; package
builds now explicitly use `CGO_ENABLED=0` for portable binaries.

Full five-platform construction, inventory, internal checksums, and release
identity checks passed for Linux amd64/arm64, Darwin amd64/arm64, and Windows
amd64. The archive has 17 files (including checksums.txt); no build tooling or
SDK source is shipped. Full archive SHA-256:
`ef6eaec76ac76165db95c8d1e6a57e2a175ba1d80bc554cc0d79470caf4d61f1`
(initial migration archive; review follow-up hashes are recorded below).

The official `kandev-linux-x64.tar.gz` and its published SHA-256 file were
fetched using `gh release download v0.99.0 --repo kdlbs/kandev`, verified with
`sha256sum -c kandev-linux-x64.tar.gz.sha256`, and extracted. The executable
reported `v0.99.0`; the disposable instance's `/health` reported the same.
It used a temporary home/database and loopback port, with no Slack credentials.

Both the host-only archive and the complete five-platform archive passed the
released-host smoke (the final run used the updated version-aware smoke script):

- Package admission with minimum host 0.99.0 and API 1; managed subprocess active.
- Host config/state roundtrips and status webhook returning unconfigured.
- Test webhook returning HTTP 400 without credentials; scan returning HTTP 202.
- Wrong-method HTTP 405 and undeclared-webhook HTTP 404.
- Serving the unchanged settings UI bundle from the installed archive.
- Config update/restart, disable/re-enable, config retention, restored webhooks.
- Clean uninstall from the disposable instance.

Host archive SHA-256:
`9183fba00629a8300e04f70c915fc7605aadf9dc9145e40d4ff3779029c46f14`
(initial migration archive; review follow-up hashes are recorded below).

## Review hardening verification

The initial head's CI passed all five checks. Its automated review identified
additional verifier and smoke-evidence gaps; those claims are superseded by the
following verification. Regression fixtures first failed on executable comments
and dangling source-version quotes. Updated checks now reject dangling icon,
executable, and source/archive version quotes and propagate malformed executable
parser failures before sorting, while accepting YAML comments.

`make test-minimum-host` now builds/verifies its host archive as a prerequisite.
The strengthened Linux smoke identifies the installed executable through `/proc`
and compares both PID and kernel start time after config PATCH. It supplies a
deliberately incomplete synthetic bot token (without an app token), which fails
local validation before Slack I/O. This forces the plugin to persist an error
and nonempty checkedAt through Host state. The identical checkedAt must survive
an observed process exit and replacement on disable/re-enable. A two-second
interval ensures that recreating lost state cannot accidentally reuse the
second-resolution timestamp. These assertions passed; no real Slack credential or external message was used.

Review verification commands (all passed):

```sh
make check-format vet test
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .github/workflows/ci.yml .github/workflows/build.yml .github/workflows/release.yml
make test-minimum-host KANDEV_TOOLING=/tmp/slack-sdk-migration/kandev-tooling KANDEV_HOST_BIN=/tmp/slack-sdk-migration/host/kandev/bin/kandev
make verify-package KANDEV_TOOLING=/tmp/slack-sdk-migration/kandev-tooling
sh scripts/verify-release-version.sh v0.3.0 kandev-plugin-slack-0.3.0.tar.gz
git diff --check
```

The repeated package checks cover the updated packaged documentation and
unchanged runtime behavior. Review follow-up archive SHA-256 values:

- Host-only: `24a2ab61eda4df4d6d6290687c1f43be5d1183b24569e75c61f40a4bed60dfc5`.
- Five-platform: `51836b5e52aa0a1ab05fd687e0422565c3a392a648196eb0b9170ea3770ccc4e`.

The final timestamp-precision assertion was also verified directly against the
same host-only artifact (scripts and this report are not packaged):

```sh
python3 scripts/smoke-minimum-host.py --host /tmp/slack-sdk-migration/host/kandev/bin/kandev --package /tmp/slack-sdk-migration/review-host-package.tar.gz
```

## Limits and external actions

Released-host smoke does not revalidate browser rendering, task-agent MCP tool
routing, SSH executors, real Slack scope grants, Socket Mode authentication,
triage creation, DM delivery, or external repeated-call suppression. The Go
suite covers triage and notification outcomes with fake Host/HTTP fixtures;
that is not live external-service evidence. Historical tests are retained,
with their dates and host versions, in `docs/notification-validation.md`.

The PR adds CI jobs for standalone source checks, complete packaging, and the
released minimum-host smoke. CI status is reported on the PR; local checks do
not establish a CI pass. No plugin release/tag, SDK publication, catalog version
change, merge, external author notification, or delegated session was performed.
