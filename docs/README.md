# SpecGate documentation

SpecGate is the governance layer between approved intent and coding agents.
Use these docs to install it, understand the delivery loop, operate a local
deployment, and inspect the command and policy contracts.

> SpecGate is designed for trusted networks. Read
> [Trust and security](concepts/trust-and-security.md) before exposing any
> service outside your machine or private network.

## Start here

If this is your first run, follow the [Quickstart](quickstart.md). It installs
the CLI, starts a local stack, creates your local user and workspace, connects
an IDE agent, covers optional model setup, and walks through one governed work
item.

The shortest path:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
specgate init
specgate doctor
specgate plugins install
specgate status
```

## Find the right document

### Learn the product

- [Quickstart](quickstart.md) — first successful local run.
- [How SpecGate works](concepts/how-specgate-works.md) — the delivery loop and
  product boundaries.
- [Artifacts and Context Packs](concepts/artifacts-and-context-packs.md) — how
  approved documents become coding-agent context.
- [Governance and gates](concepts/governance-and-gates.md) — policy,
  readiness, delivery review, and evidence.
- [How verification works](concepts/verification.md) — what context the
  checkers receive, how verdicts are derived, and the honest limits.

### Complete a task

- [Install IDE plugins](guides/install-ide-plugins.md)
- [Use the SpecGate CLI](guides/cli-workflow.md)
- [Use SpecGate with a coding agent](guides/coding-agent-workflow.md)
- [Configure models](guides/configure-models.md)
- [Respond to gate failures](guides/respond-to-gate-failures.md)
- [Customize governance](guides/customize-governance.md)
- [Connect delivery integrations](guides/connect-integrations.md)
- [Operate SpecGate](guides/operate-specgate.md)
- [Contributor setup](guides/contributor-setup.md)

### Look up exact behavior

- [CLI reference](reference/cli.md)
- [Configuration reference](reference/configuration.md)
- [Governance reference](reference/governance.md)
- [Gate catalog](reference/gates.md)
- [Evidence reference](reference/evidence.md)
- [Glossary](reference/glossary.md)
- [Contracts](contracts.md)
- [Data model](data-model.md)
- [Testing strategy](testing.md)

### Maintain or release SpecGate

- [Maintainer internals](internals/README.md)
- [OSS release checklist](internals/oss-release-checklist.md)

## Delivery loop

```text
publish artifact
→ resolve governance
→ run readiness checks
→ approve the exact artifact version
→ hand a Context Pack to the coding agent
→ submit delivery evidence
→ review delivery against acceptance criteria
→ reconcile or complete
```

## Data and cleanup

Artifact/spec data is persistent. Default local deployments store:

| Location | Contents |
|---|---|
| `postgres-data` Docker volume | artifact metadata, work items, features, settings, evidence, gate history |
| `doc-registry-data` Docker volume | artifact/spec document contents under `/data/blobs` |

`specgate uninstall` keeps this data unless you select local data removal in the
interactive checklist or run:

```bash
specgate uninstall --purge-data --yes
```

Back up both volumes before purging data you care about. See
[Operate SpecGate](guides/operate-specgate.md#remove-a-deployment-safely).
