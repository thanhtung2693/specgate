# Changelog

All notable user-facing changes are recorded here. SpecGate follows semantic
versioning.

## [0.1.5] - 2026-09-06

`v0.1.5` is the supported stable successor to `v0.1.4`.

### Added

- Local work can expose one resume packet with its scope, acceptance criteria,
  pinned-document index, verification contract, and next action. Agents can
  fetch one immutable document by its indexed path and role instead of loading
  the entire Context Pack repeatedly.
- Optional immutable verification contracts bind Local `@check` criteria to
  reviewed `sh` commands and repository-relative working directories before
  delivery evidence exists.
- Local `doctor` now checks repository bindings, a POSIX shell, and both
  global and project IDE integration files. It gives an exact recovery command
  for a stale workspace binding.
- Local initialization can install IDE files at global or project scope, and
  retains that scope in verification and recovery commands.

### Changed

- Local delivery requires the exact review ID shown in status before a human
  decision. Submission validates work, workspace, Context Pack, and any pinned
  verification contract before it can execute reported checks.
- Delivery skills use the Local resume packet and request pinned documents only
  when needed, reducing duplicate CLI reads and agent context.
- IDE installation guidance now recommends native marketplaces for Codex and
  Claude Code when the IDE should manage updates and enablement. The SpecGate
  CLI remains the offline and project-integration installer; Cursor continues
  to use documented skills directories.
- UI, CLI, agent, and Doc Registry dependencies were refreshed.

### Upgrade from 0.1.4

- Run `specgate update`, or rerun the public installer. `v0.1.5` sorts after
  `v0.1.4`, so the CLI update check and installer select it normally.
- Existing Local SQLite stores open in place; the new verification-contract
  table is additive. Existing work remains `unconfigured` until a human pins a
  contract.
- Refresh CLI-managed IDE files with `specgate plugins install` and start a
  new IDE session. Update native Codex or Claude plugins through their native
  plugin manager.

[0.1.5]: https://github.com/thanhtung2693/specgate/compare/v0.1.4...v0.1.5
