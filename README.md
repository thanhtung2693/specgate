<p align="center">
  <img src="app/ui/public/logo.svg" alt="SpecGate" width="96" />
</p>

<h1 align="center">SpecGate</h1>

<p align="center">
  <strong>The governance layer between approved intent and coding agents.</strong><br/>
  Any spec format in. Approved context out. Delivery evidence back.
</p>

<p align="center">
  <a href="https://thanhtung2693.github.io/specgate/"><img src="https://img.shields.io/badge/landing-GitHub%20Pages-111111.svg" alt="Landing page"></a>
  <a href="https://github.com/thanhtung2693/specgate/actions/workflows/release-readiness.yml"><img src="https://github.com/thanhtung2693/specgate/actions/workflows/release-readiness.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue.svg" alt="License"></a>
</p>

<p align="center">
  <a href="https://thanhtung2693.github.io/specgate/">Visit the SpecGate landing page</a>
</p>

SpecGate is a local-first control plane for AI-assisted software delivery. It
keeps approved intent versioned, gives coding agents one governed Context Pack,
and records delivery evidence against acceptance criteria. The goal is simple:
agents can move fast, but they build against the exact version humans approved.

> **Status: alpha** — suitable for local evaluation and self-hosted
> workflow trials. APIs and UI may change. The supported alpha path is
> **CLI-first**. The web UI is available for review, artifact inspection,
> settings, governance chat, and workflow scanning. Contributions, feedback, and
> collaboration are welcome at thanhtung2693@gmail.com.

SpecGate complements OpenSpec, Spec Kit, Kiro, custom Markdown, and other
authoring workflows. Those tools help shape work; SpecGate governs which
artifact version was approved, what context the coding agent received, and
whether delivery matched the approved acceptance criteria.

Teams can still work without SpecGate by combining specs, trackers, PR review,
CI, chat, and manual discipline. SpecGate becomes useful when that convention
needs to become a durable record of approved handoff, evidence, verdicts,
reconciliation, and audit.

## How it works

```text
artifact version
→ governance and readiness checks
→ human approval
→ Context Pack for the coding agent
→ delivery evidence and review
→ reconciliation or completion
```

In practice:

1. An IDE agent or authoring tool publishes a versioned artifact.
2. SpecGate resolves policy and checks whether the artifact is ready.
3. A human approves the exact version when policy requires it.
4. A coding agent reads the approved Context Pack through the `specgate` CLI.
5. Delivery evidence returns to SpecGate — one `specgate delivery submit` runs
   the report → gates → review tail — for verdicts, reconciliation, and audit.

## What works today

- CLI-first local setup with `specgate init`.
- Local user and workspace selection for attribution and filtering.
- Artifact versioning, governance policy, readiness gates, and Context Packs.
- Global IDE plugins for Claude Code, Cursor, and Codex.
- Delivery evidence, trust stamping, and delivery review.
- A governance-value readout: `specgate stats` reports first-pass yield, what
  gates and reviews caught, rework depth, and cycle time from real run data.
- Optional server-side model configuration for assisted gates, summaries, and
  review automation.
- Experimental integrations, knowledge search, governance operations, and web UI
  surfaces.

SpecGate does not replace your spec authoring tool, issue tracker, coding IDE,
pull request review, or CI. It records the governed handoff and delivery review
across those systems.

## Quickstart

**Prerequisite:** Docker with Docker Compose v2. You do not need a source
checkout, Go, Node.js, Python, or a model API key to start the local stack.

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
specgate init
```

`specgate init` downloads the release Compose bundle, creates local environment
files and secrets, starts the stack, asks for your local user/workspace, and
lets you choose whether to seed demo data and install IDE plugins.

To remove a trial install:

```bash
specgate uninstall
```

The default uninstall stops the stack and removes user-local SpecGate CLI config
and IDE plugin files. It keeps artifact/spec data. In an interactive terminal,
the command shows a checklist for plugin files, local data, and Docker images.
Use the destructive form only after backing up any artifacts or specs you want
to keep:

```bash
specgate uninstall --purge-data --yes
```

The destructive form removes the deployment directory, Docker volumes, and
SpecGate service images. Artifact metadata, work items, and settings live in
Postgres; artifact/spec file contents live in the Doc Registry data volume.

If you skipped IDE setup during init, run:

```bash
specgate plugins install
specgate doctor
specgate plugins doctor
specgate status
```

You can also run the public IDE installer directly:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/plugins/install.sh | sh
```

Restart any selected IDE after installing or refreshing plugins so new skills,
hooks, and rules are loaded.

The deterministic governance core works without a server-side model. Configure a
model later when you want route suggestions, summaries, semantic readiness
gates, or model-backed delivery review to run server-side.

Continue with the [full quickstart](docs/quickstart.md).

> SpecGate currently expects a trusted network. Do not expose the general
> Doc Registry HTTP surface or web UI directly to the public internet.

## UI (experimental)

![SpecGate UI artifact inspection](docs/assets/readme/specgate-ui-artifact-library.png)

## Architecture

| Module | Stack | Responsibility |
|---|---|---|
| `app/doc-registry` | Go | Artifacts, versions, policy, evidence, integrations, REST and MCP |
| `app/agents` | Python · LangGraph | Governance-ops, semantic gates, delivery review, reconciliation |
| `app/ui` | Vite · React | Human review, artifact inspection, governance chat, settings, and operations |
| `app/cli` | Go · Cobra | User and coding-agent interface to SpecGate |

## Documentation

- [Documentation home](docs/README.md)
- [Quickstart](docs/quickstart.md)
- [How SpecGate works](docs/concepts/how-specgate-works.md)
- [Use SpecGate with a coding agent](docs/guides/coding-agent-workflow.md)
- [Operate SpecGate](docs/guides/operate-specgate.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)

## License

Apache-2.0 — see [LICENSE](LICENSE).
