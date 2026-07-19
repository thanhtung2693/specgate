# SpecGate deployment guide

For day-to-day operations, see
[Operate SpecGate](../docs/using-specgate/guides/operate-specgate.md). This page
documents the supported deployment shape.

## Full appliance installation

`specgate init --mode full` runs the complete product in one Docker container,
exposes one host port, and persists state in one named volume.
Postgres, Doc Registry, Agents, the web UI, and the gateway are supervised inside
the appliance. Redis and MinIO are not required.

### Prerequisites

- Docker with Docker Compose v2
- Network access to download the CLI and appliance image

A model provider key is optional. The deterministic governance workflow works
without one.

### Start locally

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
specgate init --mode full
specgate local-status
specgate doctor
```

The deployment directory is `~/.specgate` by default. Choose another directory
with `specgate init --mode full --dir <path>`, or seed the demo data with
`specgate init --mode full --seed`.

The CLI downloads the release bundle from `deploy/local`, creates the settings
encryption key, pulls the pinned published appliance image, starts the
`specgate` appliance, and saves its gateway API URL. Contributor-only `dev`
bundles use the locally built image instead. Interactive init asks for the
topology; use `--mode full` for automation.

### Public endpoint

The appliance publishes `http://localhost:3000` by default:

| Path | Purpose |
|---|---|
| `/` | Web UI |
| `/api/doc-registry/` | Doc Registry API |
| `/api/agents/` | Agents API |
| `/integrations/oauth-callback` | Provider OAuth return |
| `/integrations/{integration}/resources/{resource}/{provider}/webhook` | Managed provider webhook receiver |

The CLI uses:

```bash
specgate config server http://localhost:3000/api/doc-registry
```

Change the one public port in the deployment `.env` before startup:

```dotenv
SPECGATE_PORT=13000
```

The matching CLI server is then
`http://localhost:13000/api/doc-registry`. No database or internal service port
is published by the local appliance.

### Data and lifecycle

The named `specgate-data` volume contains both database and artifact data:

| Path | Contents |
|---|---|
| `/data/postgres` | metadata, work items, settings, evidence, and gate history |
| `/data/registry` | artifact and spec document contents |

The appliance overrides the base image's unused `/var/lib/postgresql` volume
with a temporary filesystem. Docker therefore creates no anonymous PostgreSQL
volume; all durable appliance data remains in the labelled `specgate-data`
volume.

Stopping is non-destructive:

```bash
specgate down
specgate up
```

Upgrade the CLI, IDE plugins, appliance bundle, and appliance image with:

```bash
specgate update
```

Before replacement, the updater writes one mode-`0600` recovery archive under
the deployment `backups/` directory. It contains a validated logical Postgres
dump, registry blobs, the active Compose files, version/port configuration, and
`specgate.env` with the settings encryption key. Doc Registry and Agents are
briefly quiesced while the data payload is captured. After starting the target,
the updater checks both gateway APIs. Each release bundle declares whether it
is safe to restart the previous image after the target has run migrations. A
compatible release rolls back automatically; otherwise the updater leaves the
target bundle installed and reports the recovery archive path instead of
starting an old binary against potentially newer data.

`specgate doctor` reads component diagnostics through the gateway and falls
back to the loopback health adapter inside the managed container when nginx is
the failing component.

Safe uninstall keeps local data unless you select data removal:

```bash
specgate uninstall
```

To permanently remove the appliance and its volume in automation, back up first:

```bash
specgate uninstall --purge-data --yes
```

### Local troubleshooting

```bash
specgate local-status
specgate doctor
docker compose -f ~/.specgate/compose.yml ps
docker compose -f ~/.specgate/compose.yml logs -f --tail=200 specgate
```

If the appliance does not become healthy, its combined log identifies the
failing internal process. Routine five-second health probes are intentionally
not written to the container log; component state and the retained last-failure
record provide those diagnostics. Do not publish additional container ports as
a repair.

## Managed resource labels

The appliance labels SpecGate-managed resources with
`org.specgate.managed=true`, the Compose project, and the component name. The
CLI uses those labels to constrain cleanup to the selected local deployment.

```bash
docker ps -a --filter label=org.specgate.managed=true
docker volume ls --filter label=org.specgate.managed=true
docker network ls --filter label=org.specgate.managed=true
docker image ls --filter label=org.specgate.managed=true
```
