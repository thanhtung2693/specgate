<p align="center">
  <img src="app/landing/logo.svg" alt="SpecGate" width="96" />
</p>

<h1 align="center">SpecGate</h1>

<p align="center">
  <strong>Give your coding agent approved scope. Review each criterion before you accept.</strong><br/>
  A Local CLI for solo developers and small teams.
</p>

<p align="center">
  <a href="https://thanhtung2693.github.io/specgate/"><img src="https://img.shields.io/badge/landing-GitHub%20Pages-111111.svg" alt="Landing page"></a>
  <a href="https://github.com/thanhtung2693/specgate/actions/workflows/release-readiness.yml"><img src="https://github.com/thanhtung2693/specgate/actions/workflows/release-readiness.yml/badge.svg" alt="Release readiness"></a>
  <a href="https://skills.sh/thanhtung2693/specgate"><img src="https://skills.sh/b/thanhtung2693/specgate" alt="SpecGate skills on skills.sh"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue.svg" alt="License"></a>
</p>

<p align="center">
  <a href="https://thanhtung2693.github.io/specgate/demo/"><strong>Try the UI demo</strong></a>
  · <a href="docs/using-specgate/guides/try-delivery-proof.md"><strong>Run the delivery proof locally</strong></a>
</p>

Keep writing specs in OpenSpec, Spec Kit, Superpowers, Markdown, or your chosen
tool. Your coding agent implements the change in your IDE. SpecGate records the
approved scope, gives the agent a Context Pack, and keeps the evidence and
human decision in a Local SQLite store.

## Why SpecGate?

When several features move at once, a new agent session can pick up an older
spec or miss a criterion. An agent can say the work is done when the existing
name-normalization tests pass. In the local delivery proof, the approved criterion
**Reject empty names after trimming** still has a skipped required check.
SpecGate shows the evidence gap against that criterion. The agent adds the
missing behavior and test, reruns the checks, and submits again. You decide
against the new review.

The report distinguishes the agent's claims, CLI-observed command results,
and optional selected JUnit cases. Review whether those checks actually
exercise the required behavior and whether you need end-to-end or real-provider
evidence. See [How verification works](docs/using-specgate/concepts/verification.md).

## Quickstart

From your Git project, install the CLI and initialize the Local workspace.
The base workflow needs no Docker, background service, or model API key.

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
specgate init
specgate doctor
```

Using Claude Code? Install the plugin directly from this GitHub repository:

```bash
claude plugin marketplace add thanhtung2693/specgate
claude plugin install specgate@specgate
```

See [IDE plugin installation](docs/using-specgate/guides/install-ide-plugins.md)
for Codex, Cursor, updates, and uninstall. Use one installation method per IDE.

Prefer agent-led setup? Install the `specgate` bootstrap from
[skills.sh](https://skills.sh/thanhtung2693/specgate/specgate), then ask your
agent: **“Set up SpecGate.”** The bootstrap requests approval before installing
the CLI and hands the selected IDE integration to `specgate plugins install`,
so one installer owns the finished plugin.

`specgate init` creates a Local user and workspace in SQLite on this machine.
It binds the current Git checkout and offers IDE plugin setup.

If you skipped IDE setup during init:

```bash
specgate plugins install --agent codex --project-local
specgate doctor
specgate plugins doctor --agent codex --project-local
specgate status
```

Restart selected IDEs after plugin install so new skills, hooks, and rules load.

Already using SpecGate? Run `specgate update` or rerun the installer.
Before upgrading, back up your Local store and read the
[changelog](CHANGELOG.md), especially if you use verification pins, checkpoints,
or source lineage: the first such write upgrades store compatibility.
Refresh CLI-managed IDE files with the same plugin install command and scope,
or update native plugins through their IDE plugin manager.

Once setup is ready, tell your coding agent:

> Prepare this change as SpecGate Quick Work. Show each testable criterion,
> its proposed `@check:name`, and the exact test command. Wait for my approval
> before creating work, editing code, or running any tests. After approval,
> use those bindings, implement, submit evidence, and stop for my review.

Follow the [Quickstart](docs/using-specgate/quickstart.md) for the commands and
decision steps.

## How a work item moves through SpecGate

```text
approve scope and criteria
-> agent implements from the Context Pack
-> agent submits per-criterion evidence and addresses gaps
-> human decides against the exact review
```

Use Quick Work when requirements could slip through or another session needs
to pick up the change. For a typo or a label change, your usual agent and tests
are enough. When the work needs a reviewed spec or plan, publish the package
and approve its exact version.
See the [Local CLI workflow](docs/using-specgate/guides/cli-workflow.md).

## What you can do

- Resume a work reference with its approved scope, evidence gaps, and next
  action. Load pinned documents as needed.
- Run artifact readiness checks with four semantic rubrics your project can
  extend. See [Local project instructions](docs/using-specgate/guides/customize-governance.md#add-local-project-instructions).
- Bind checks to individual criteria and record the exact human decision.
  See [advanced verification](docs/using-specgate/reference/cli.md#advanced-local)
  for optional pins, selected tests, checkpoints and lineage.
- Share an unsigned, read-only [delivery handoff](docs/using-specgate/reference/cli.md#hand-a-review-to-a-teammate-through-the-repository)
  beside a Git diff. Each Local store has its own decisions.

To continue your own Local work on another machine, use `specgate transfer`
to prepare a private snapshot, inspect and receive it into a new store, then
activate it for the intended Git checkout. Move code separately.
[Move a Local store](docs/using-specgate/guides/operate-specgate.md#move-a-local-store-to-another-checkout).

## Review in the browser

Run `specgate ui` from the bound repository to inspect approved context,
criterion evidence and current source, and record a human decision. The
optional browser UI supports English and Vietnamese. It stays on loopback and
trusts local processes and accounts that can reach it.
See [Local review](docs/using-specgate/guides/local-review.md) and
[Trust and security](docs/using-specgate/concepts/trust-and-security.md).

An optional foreground [notification experiment](docs/using-specgate/guides/local-notification-pilot.md)
runs from the source checkout. The participant study is pending.

## Release status

This README describes **v0.3.0**, including the
Local-only distribution, optional browser review, and Local store transfer.
The installer downloads the latest published version from
[GitHub Releases](https://github.com/thanhtung2693/specgate/releases/latest).
See the [v0.3.0 release notes](CHANGELOG.md#030---2026-11-17).

## Previous installations

This distribution retires the Full appliance and remote setup, governance chat,
Knowledge, provider and tracker integrations, statistics reporting, and the
previous portable workspace transfer. Existing Local history and prior
deployment data, configurations, volumes, and backups remain in place. Use a
prior compatible CLI to inspect an existing Full deployment. For a separate Local setup, see
[Operate SpecGate](docs/using-specgate/guides/operate-specgate.md#previous-installations).
Read the [release notes before updating](CHANGELOG.md).

## Repository layout

| Module | Responsibility |
|---|---|
| `app/cli` | CLI, Local SQLite store, and embedded review UI |
| `app/landing` | Public landing page |
| `plugins` | Canonical IDE package and skills |
| `scripts` | Release installer, plugin tooling, and source-only notification pilot |

## Documentation

- [Documentation home](docs/README.md)
- **Use SpecGate:** [User documentation](docs/using-specgate/README.md),
  [Quickstart](docs/using-specgate/quickstart.md),
  [coding-agent workflow](docs/using-specgate/guides/coding-agent-workflow.md),
  and [CLI reference](docs/using-specgate/reference/cli.md).
- **Contribute:** [Contributor documentation](docs/contributing/README.md),
  [setup](docs/contributing/setup.md),
  [architecture](docs/contributing/architecture.md), and
  [contracts](docs/contributing/contracts.md).
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Changelog](CHANGELOG.md)

## Roadmap

- Strengthen per-criterion delivery verification and make evidence gaps easier
  to act on.
- Improve approved-snapshot handoffs and durable acceptance records, with Local
  mode as the default path.

Missing something? [Open an issue](https://github.com/thanhtung2693/specgate/issues).

## License

Apache-2.0 - see [LICENSE](LICENSE).
