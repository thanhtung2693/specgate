# SpecGate feature status

This reference describes the capabilities in the current source. See
[release status](../../../README.md#release-status) for availability.

## Established paths

- Initialize a Local store, select a user and workspace, and bind a checkout.
- Create quick work with explicit criteria or publish and approve an exact
  artifact version for an immutable Context Pack.
- Run deterministic checks and frozen IDE readiness tasks. Store their
  origin separately from a human approval.
- Resume approved work, inspect pinned documents, submit criterion evidence, and
  make the exact reviewed delivery decision through the Change facade.
- Install or verify the embedded Codex, Claude Code, and Cursor packages.
  Ordinary uninstall preserves Local data.
- Review longer work with optional verification pins, selected JUnit cases,
  watched inputs, checkpoints, source lineage, and an exact acceptance basis.
  The first enhanced write creates a disclosed pre-upgrade backup.
- Export or read an unsigned bounded delivery review request through Git. It
  is separate from Local store backups and human decisions.

## Newer surfaces

- Optional [Local browser review](../guides/local-review.md) for approved
  scope, criterion evidence, human decisions, and Local installation maintenance.
- [Local project rubrics](../guides/customize-governance.md#add-local-project-instructions)
  add instructions to four existing semantic gates and freeze them at publication.
- [Local store transfer](../guides/operate-specgate.md#move-a-local-store-to-another-checkout)
  carries an entire selected store and a work bookmark into a fresh store.
  Activate that store explicitly for the destination checkout.

## Optional experiment

The [Local notification pilot](../guides/local-notification-pilot.md) runs in the
foreground from the source checkout using Node.js 26 or later. The participant
study is pending.

## Deferred from the Change facade

`change prepare` is not available. Agents use the artifact and gate commands to
prepare the exact snapshot and explicit work contract; `change approve` then
coordinates approval, canonical promotion, work creation, and Context Pack
verification.

## Retired distribution features

The Full appliance, remote service setup, governance chat, Knowledge,
provider and tracker integrations, statistics reporting, and the previous
portable workspace transfer are retired. See [previous installations](../guides/operate-specgate.md#previous-installations)
for access to existing data.

## Related

- [Quickstart](../quickstart.md)
- [Trust and security](../concepts/trust-and-security.md)
- [CLI reference](cli.md)
