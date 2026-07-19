# Using SpecGate

Use these docs to set up SpecGate, connect a coding agent, review delivery, and
keep a local or team installation healthy. You do not need a source checkout
unless a guide specifically asks for one.

## Start here

New to SpecGate? Follow the [Quickstart](quickstart.md). It takes you from an
empty machine to one reviewed work item.

The shortest setup path is:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
specgate init
specgate doctor
specgate status
```

`init` starts with Local CLI by default: one local SQLite store and no Docker
or server. Choose Full appliance during setup when you need the browser,
governance chat, Knowledge, integrations, or shared workspaces.

SpecGate assumes a trusted machine or private network. Read
[Trust and security](concepts/trust-and-security.md) before exposing a service
outside that boundary.

## Understand the ideas

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
- [Use SpecGate with a coding agent](guides/coding-agent-workflow.md)
- [Install SpecGate in your coding IDE](guides/install-ide-plugins.md)
- [Configure models](guides/configure-models.md)
- [Respond to gate failures](guides/respond-to-gate-failures.md)
- [Connect delivery integrations](guides/connect-integrations.md)
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
