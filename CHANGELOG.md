# Changelog

All notable user-facing changes are recorded here. SpecGate follows semantic
versioning.

## Unreleased

## [0.2.6] - 2026-10-25

Reissued with delivery-evidence clarity and launch-readiness fixes. This
replaces the assets originally published as v0.2.6 on 2026-10-10.

### Added

- A runnable Local delivery-proof demo shows an explicitly skipped criterion,
  then repairs code, tests and the completion report using the same Context
  Pack. It retains isolated state and stops before human acceptance.
- A selected-JUnit Local walkthrough covers missing-test evidence, reruns,
  checkpoint resume, watched-input drift and exact-basis human review.
- Local checkpoints can compare exact dirty-file content across work sessions;
  unavailable checkout snapshots are rejected instead of stored as baselines.
- Optional Local verification pins bind `@check` criteria to reviewed commands.
  Observed JUnit runs record selected test cases and watched-input freshness.
- Declared source lineage supports exact-pair artifact impact inspection and
  identifies bounded overlap between open work items. Unknown legacy source
  inventories remain unavailable; malformed lineage arrays are rejected
  rather than silently dropped.
- Human acceptance can bind an immutable basis of the selected verification,
  checkpoint, and impact material. Legacy decision fields remain absent when
  this enhanced basis is not required.

### Improved

- Default Local status and resume display criterion claims, provenance,
  citation grounding and gaps beside the existing verdict and reason. Detail
  adds paths and selected cases without duplicating the criterion list.
- Public copy and landing examples clarify Local freshness limits and the
  distinction between failed evidence and human risk acceptance.
- Local resume shows newer artifact versions without changing approved scope,
  distinguishes missing or unavailable evidence, and keeps large path deltas
  compact unless detail is requested.
- IDE installers include supplemental skill references and diagnose missing
  reference files. Codex configuration updates preserve unrelated TOML,
  marketplace metadata, and file permissions; uninstall preserves user-owned
  references and shared marketplace configuration.
- CLI, Doc Registry, agents, UI, and release-image dependencies were refreshed.

### Fixed

- Landing demo tabs retain manual selections when initial viewport observation
  arrives late.
- Local risk categories use structured outcomes; words in check names,
  testcase names and paths cannot create false failed or stale observations.
- The delivery-proof demo validates completion input before changing repair
  files and quotes paths containing spaces or apostrophes in its repair command.
- Selected JUnit failures retain the CLI-observed reason through delivery
  submission, persisted evidence, and status readback, including missing tests.
- Local doctor diagnoses a logged-out identity without selecting one and
  reports the actual store it opened.
- Artifact inline diffs enforce comparison and render budgets; oversized
  documents remain available through version source inspection and copy.

### Security

- Go compiler and image builds use 1.27.3 for patched standard-library security
  fixes; the minimum Go language/dependency version remains 1.26.4.
  CI uses Staticcheck 0.8.2 to support the updated compiler export format.
- Agents images use certifi 2026.10.23, removing compromised emSign CA trust
  covered by GHSA-6q48-6c7w-9gw2.
- Appliance supervisor state is root-owned, restart counters are validated
  before arithmetic, and diagnostic temporary files are created exclusively.
  Unsafe existing state links are rejected without deleting operator data.
- Git origin credentials are removed from new receipts and projected exports;
  Gemini embedding credentials no longer appear in request URLs or errors.
- Windows browser opening uses the native API instead of a command shell and
  requires an absolute HTTP(S) URL without control characters.

### Upgrade notes

- This reissue keeps version v0.2.6. Verify the installed build commit when
  replacing an existing v0.2.6 installation.
- The first enhanced Local write backs up and upgrades the entire Local store.
  Older CLIs cannot write that store; portable and handoff v1 exports refuse
  enhanced records instead of silently losing them. Back up before upgrading.
- Refresh CLI-managed IDE files with `specgate plugins install` and start a
  new IDE session. Native marketplace installations should update through
  their own plugin manager.

## [0.2.5] - 2026-09-11

- Local source coverage exposes unassigned, in-progress, delivered, and
  deferred requirements with exact work links. CLI and IDE guidance now makes
  Local verification steps clearer.

## [0.1.8] - 2026-09-09

- Restored IDE session-hook routing in the released plugins and CLI installer.

## [0.1.7] - 2026-09-07

`v0.1.7` is the supported stable successor to `v0.1.4`.

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
- Local artifacts can pin source requirements to immutable document snapshots.
  Work acceptance criteria map them with `@source:<id>`, and coverage reports
  whether every requirement is assigned and delivered.

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
- Local artifact preview validates and displays source requirements. Portable
  export refuses source-criterion inventories because portable/v1 cannot carry
  that proof into Full mode.
- UI, CLI, agent, and Doc Registry dependencies were refreshed.

### Security

- The Local appliance now builds Doc Registry with Go `1.26.6`, removing the
  fixed High Go standard-library vulnerabilities that blocked release image
  scanning.

### Upgrade from 0.1.4

- Run `specgate update`, or rerun the public installer. `v0.1.7` sorts after
  `v0.1.4`, so the CLI update check and installer select it normally.
- Existing Local SQLite stores open in place; the new verification-contract
  table is additive. Existing work remains `unconfigured` until a human pins a
  contract.
- Refresh CLI-managed IDE files with `specgate plugins install` and start a
  new IDE session. Update native Codex or Claude plugins through their native
  plugin manager.

[0.2.6]: https://github.com/thanhtung2693/specgate/releases/tag/v0.2.6
[0.2.5]: https://github.com/thanhtung2693/specgate/releases/tag/v0.2.5
[0.1.8]: https://github.com/thanhtung2693/specgate/releases/tag/v0.1.8
[0.1.7]: https://github.com/thanhtung2693/specgate/compare/v0.1.4...v0.1.7
