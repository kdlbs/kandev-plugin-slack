# Slack plugin repository

Use the Kandev [create-kandev-plugin workflow](https://github.com/kdlbs/kandev/blob/main/.agents/skills/create-kandev-plugin/SKILL.md) and [public authoring guide](https://github.com/kdlbs/kandev/blob/main/docs/public/plugins-authoring.md) when changing this plugin. Keep host/SDK changes in their owning repository.

- Go SDK: `github.com/kdlbs/kandev/pluginsdk v0.1.0`, resolved publicly. Do not add backend replacements or SDK sibling dependencies.
- The SDKs share independent versioning. This plugin has no npm SDK consumer, package manifest, or lockfile; `ui/bundle.js` uses the host's React/UI API directly.
- Read the installed SDK's `sdk-release.json` before upgrades. SDK 0.1.0 requires Kandev 0.99.0 and supports API 1 and 2. Preserve this plugin's API 1 registrations and behavior unless a separate change requests migration.
- Run `go mod tidy`, `make sdk-check check-format vet test build`. Commit generated `go.sum` changes and exact SDK pins after deliberate upgrades.
- Packaging uses backend-owned `plugin-pack` at `.kandev-tooling-ref`, via `KANDEV_TOOLING`. Its checkout must include `apps/backend` and root `pluginsdk`. It is a packaging input, never this plugin's Go SDK replacement.
- Run `make verify-package-host`, `make verify-package`, and `make test-minimum-host KANDEV_HOST_BIN=/path/to/released/kandev` using an isolated host. See [repository baseline](docs/repository-baseline.md) for commands and smoke limits.
- Keep plugin version, release guards, manifest surfaces, package inventory/checksums, config/state keys, and installed-plugin behavior compatible. Do not publish, merge, change catalog versions, or contact external authors without explicit authorization.
