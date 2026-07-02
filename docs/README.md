# SpecGate documentation

SpecGate is the governance layer between approved intent and coding agents.
These guides help you run the project, connect your IDE, and adapt its
governance to your team.

> SpecGate is designed for a trusted network. Read
> [Trust and security](concepts/trust-and-security.md) before exposing services.

## Start here

[Quickstart](quickstart.md) takes you from a clean machine to a running SpecGate
deployment, optional model setup, and connected coding IDE.

The shortest happy path is:

1. install the CLI and run `specgate init`;
2. choose a local user and workspace;
3. install the IDE integration globally;
4. create, review, hand off, and verify work through the `specgate` CLI.

## Choose your path

### Try SpecGate

- [Quickstart](quickstart.md)
- [How SpecGate works](concepts/how-specgate-works.md)
- [Artifacts and Context Packs](concepts/artifacts-and-context-packs.md)
- [SpecGate feature status](features.md)

### Connect a coding agent

- [Install SpecGate in your coding IDE](guides/install-ide-plugins.md)
- [Use SpecGate with a coding agent](guides/coding-agent-workflow.md)
- [Use the SpecGate CLI](guides/cli-workflow.md)

### Extend governance

- [Governance and gates](concepts/governance-and-gates.md)

### Operate a deployment

- [Operate SpecGate](guides/operate-specgate.md)
- [Configure models](guides/configure-models.md)
- [Connect delivery integrations](guides/connect-integrations.md)
- [Trust and security](concepts/trust-and-security.md)

### Contribute

- [Contributor setup](guides/contributor-setup.md)
- [Repository contribution guide](../CONTRIBUTING.md)
- [Maintainer internals](internals/README.md)

## The delivery loop

```text
Publish artifact
→ resolve governance
→ run readiness
→ human review and approval
→ generate Context Pack
→ coding agent implements
→ submit delivery evidence
→ delivery review
→ reconcile or complete
```

Read [How SpecGate works](concepts/how-specgate-works.md) for the complete
picture.

## Concepts

- [How SpecGate works](concepts/how-specgate-works.md)
- [Artifacts and Context Packs](concepts/artifacts-and-context-packs.md)
- [Governance and gates](concepts/governance-and-gates.md)
- [Trust and security](concepts/trust-and-security.md)

## Guides

- [Use the SpecGate CLI](guides/cli-workflow.md)
- [Use SpecGate with a coding agent](guides/coding-agent-workflow.md)
- [Install IDE plugins](guides/install-ide-plugins.md)
- [Configure models](guides/configure-models.md)
- [Connect integrations](guides/connect-integrations.md)
- [Operate SpecGate](guides/operate-specgate.md)
- [Contributor setup](guides/contributor-setup.md)

## Reference

- [CLI reference](reference/cli.md)
- [Governance reference](reference/governance.md)
- [Evidence reference](reference/evidence.md)
- [Configuration reference](reference/configuration.md)
- [Glossary](reference/glossary.md)

## Maintainer internals

Contracts, data models, module specifications, conformance fixtures, test
strategy, and design history remain available under
[Maintainer internals](internals/README.md). They are not required for normal
product use.
