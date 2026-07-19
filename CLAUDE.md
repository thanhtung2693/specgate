# Claude Code Contributor Entry Point

This file configures Claude Code when it contributes to the SpecGate
repository. It is not product guidance for people using SpecGate.

Read and follow @AGENTS.md first. Then load every nested rule file that owns the
files being changed:

- Doc Registry: @app/doc-registry/AGENTS.md
- Governance operations: @app/agents/AGENTS.md
- Governance package internals:
  @app/agents/src/specgate_agents/governance/AGENTS.md
- CLI: @app/cli/AGENTS.md
- Web UI: @app/ui/AGENTS.md

The landing page, deployment packaging, plugins, and repository documentation
currently inherit @AGENTS.md without another nested rule file. Use
@docs/contributing/README.md for durable contributor guidance and
@docs/using-specgate/README.md only when checking product behavior or updating
user documentation.

Do not duplicate shared contributor policy here. Update `AGENTS.md` or the
owning nested `AGENTS.md` instead.
