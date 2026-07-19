# Contributing to SpecGate

This section is for contributors who modify, test, review, or release the
SpecGate repository. Product installation and day-to-day workflows belong in
the [user documentation](../using-specgate/README.md).

Only durable, reusable contributor guidance belongs here: architecture,
contracts, data model, setup, testing, release process, and accepted ADRs.
Per-change design drafts, implementation plans, handoff notes, completion
receipts, and agent scratch files do not belong here. Keep transient work under
the gitignored `.specgate/` directory or in the agent conversation; keep source
specifications in their authoring framework's user-selected path.

## Start here

1. Read the repository [agent rules](../../AGENTS.md) and
   [contribution guide](../../CONTRIBUTING.md).
2. Follow [Contributor setup](setup.md).
3. Read the module `AGENTS.md` and specification for every module you touch.
4. Use the [testing strategy](testing.md) to select verification.

## Understand the system

- [Architecture](architecture.md) describes runtime responsibilities and trust
  boundaries.
- [Contracts](contracts.md) records vocabulary and behavior shared across
  modules.
- [Data model](data-model.md) describes product entities and storage ownership.
- [Architecture decisions](adr/README.md) records decisions that constrain
  future work.

## Change the repository

- Update the narrowest documentation layer that owns the changed behavior.
- Update [Contracts](contracts.md) when API vocabulary or cross-module behavior
  changes.
- Add or update an ADR when an architectural decision changes.
- Run the narrowest useful tests first, then broaden according to
  [Testing](testing.md).

## Release

Use the [Release guide](release.md) when preparing or publishing a version.

For commands and product workflows, use the
[SpecGate user documentation](../using-specgate/README.md).
