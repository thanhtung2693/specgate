# Doc Registry

Doc Registry is SpecGate's Go service for governed artifacts, work items,
delivery evidence, Knowledge, integrations, and durable workspace state.

It is a module in the SpecGate monorepo. Its released image, canonical IDE
plugin assets, UI API-contract generation, and contributor workflows are owned
at the repository root; this directory is not a standalone distribution.

## Start here

- [Developer reference](docs/README.md) — module layout, configuration, local
  commands, and operational flags.
- [Product requirements](docs/prd.md) — product scope and non-goals.
- [Technical specification](docs/spec.md) — persistence, lifecycle, and trust
  boundaries.
- [REST API contract](docs/api.md) — HTTP surface and payloads.
- [Contributor setup](../../docs/contributing/setup.md) — source-stack setup
  for the full monorepo.

For a focused module check, run:

```bash
make test
make lint
```

Product installation and coding-agent workflows are documented under
[`docs/using-specgate/`](../../docs/using-specgate/README.md), not here.
