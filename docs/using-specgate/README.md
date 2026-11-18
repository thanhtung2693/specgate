# Using SpecGate

SpecGate is a Local CLI for handing approved work to your coding agent and
reviewing its delivery. Solo developers and small teams keep their existing
authoring tools, IDEs, Git, and CI. Each Local store records its own approved
snapshots, criterion evidence, and human decisions.

## Start here

Follow the [Quickstart](quickstart.md) to set up the CLI and review one work item
with your coding agent.

The shortest setup path is:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
specgate init
specgate doctor
specgate status
```

`init` binds your Git checkout to a Local workspace with SQLite. The CLI
workflow needs no Docker, model API key, or background service.

For optional browser inspection and human review in English or Vietnamese, run
`specgate ui` from the bound project. It starts a loopback listener on a trusted
machine. See [Local review](guides/local-review.md) and
[Trust and security](concepts/trust-and-security.md).

These docs describe the current source. The installer downloads a published
release. Check the [changelog](../../CHANGELOG.md) for changes awaiting
release and [Operate SpecGate](guides/operate-specgate.md) for backup and
legacy recovery.

## Understand the ideas

- [Who SpecGate is for](concepts/who-specgate-is-for.md) shows how solo
  developers and small teams review evidence gaps.
- [How SpecGate works](concepts/how-specgate-works.md) explains the delivery
  loop and product boundaries.
- [Artifacts and Context Packs](concepts/artifacts-and-context-packs.md)
  explains how approved intent becomes coding-agent context.
- [Governance and gates](concepts/governance-and-gates.md) explains policy,
  readiness, evidence, and review.
- [How verification works](concepts/verification.md) explains verdicts and
  their limits.

## Get something done

- [Use the SpecGate CLI](guides/cli-workflow.md)
- [Run a delivery evidence gap and repair it](guides/try-delivery-proof.md)
- [Observe selected tests in Local mode](guides/observe-selected-tests.md)
- [Use SpecGate with a coding agent](guides/coding-agent-workflow.md)
- [Try Local review notifications](guides/local-notification-pilot.md) from the
  source checkout.
- [Install SpecGate in your coding IDE](guides/install-ide-plugins.md)
- [Customize governance](guides/customize-governance.md)
- [Respond to gate failures](guides/respond-to-gate-failures.md)
- [Operate SpecGate](guides/operate-specgate.md)

## Look up exact behavior

- [CLI reference](reference/cli.md)
- [Configuration reference](reference/configuration.md)
- [Governance reference](reference/governance.md)
- [Gate catalog](reference/gates.md)
- [Evidence reference](reference/evidence.md)
- [Feature status](reference/feature-status.md)
- [Glossary](reference/glossary.md)

To modify SpecGate itself, switch to the
[contributor documentation](../contributing/README.md).
