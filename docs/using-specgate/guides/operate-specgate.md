# Operate SpecGate

Use this guide to start, stop, back up, upgrade, or remove SpecGate.

## Install the Full appliance

Use the CLI:

```bash
specgate init --mode full
```

The complete product runs in one Docker container with one public port and one
named volume. The CLI manages the deployment directory, appliance bundle,
settings encryption key, startup, and optional demo data. For the no-Docker
Local CLI workflow, use the [quickstart](../quickstart.md).

## Operate the appliance

Start, inspect, stop, and restart:

```bash
specgate local-status
specgate doctor
specgate down
specgate up
```

`down` preserves the `specgate-data` volume. `up` starts the appliance and waits
for its health check.

### Public URL

The default public origin is `http://localhost:3000`:

| Path | Purpose |
|---|---|
| `/` | Web UI |
| `/api/doc-registry/` | CLI and Doc Registry API |
| `/api/agents/` | Agents API |
| `/integrations/oauth-callback` | Provider OAuth return |
| `/integrations/{integration}/resources/{resource}/{provider}/webhook` | Managed provider webhook receiver |

Postgres and internal service listeners are not published. To choose a public
port at first install, pass it to `init`; SpecGate stores it in the appliance
deployment `.env` for later lifecycle commands:

```bash
SPECGATE_PORT=13000 specgate init --mode full
```

To change an existing appliance, edit its deployment `.env` before startup:

```dotenv
SPECGATE_PORT=13000
```

Then run `specgate up`; it refreshes the derived local review URL and saved CLI
gateway. An explicit custom or remote `specgate config server` value remains
untouched.

### Local persistence and backup

The `specgate-data` volume holds the appliance's durable local data. Create a
consistent archive from the running appliance, then preserve the deployment
environment separately:

```bash
cd ~/.specgate
docker compose exec -T specgate /usr/local/bin/specgate-backup > specgate-backup.tar.gz
cp specgate.env specgate.env.backup
```

Keep the copied `specgate.env` beside the archive. The appliance briefly pauses
its application services while it captures a consistent state, then starts them
again.

Governance-chat threads are ephemeral in the v0.1 appliance: they are
available while the appliance is running, but reset when it restarts. Artifacts,
work items, approvals, delivery evidence, settings, and Knowledge live in the
managed volume and are included in backups.

Before an upgrade:

1. record the current appliance version;
2. run `specgate update`; before stopping the old appliance it writes a
   mode-`0600` recovery package under `~/.specgate/backups` containing the
   validated data payload, active deployment files, and `specgate.env`;
3. let the updater complete its readiness and gateway smoke checks. If either
   fails, it restarts the previous bundle only when the target release declares
   that rollback safe. Otherwise it preserves the target deployment for
   diagnosis and prints the recovery archive path;
4. run `specgate doctor` for an additional operator-facing check.

Preview or selectively remove old recovery packages without touching appliance
data or unrelated files:

```bash
specgate cleanup --backups --dry-run
specgate cleanup --backups --item <archive-name> --yes
```

For a CLI-managed appliance, `doctor` can retrieve the same component report
from inside the container when the public nginx gateway itself is unavailable.

The current local-appliance release is a clean initialization boundary; it does
not import data from the older multi-container local layout.

### Local logs

From the deployment directory:

```bash
docker compose ps
docker compose logs -f --tail=200 specgate
```

The appliance log combines its supervised internal processes. Check it before
changing port mappings or container configuration.

Component startup and migration state is available from
`/healthz/components`. The response also retains the last internal service
failure reason across container restarts. After five consecutive failures of
an essential service, the appliance exits and lets Compose restart the complete
dependency unit.

## Remove a local deployment safely

Stopping is non-destructive:

```bash
specgate down
```

Interactive uninstall lets you remove IDE plugin files separately from the
local deployment and data:

```bash
specgate uninstall
```

Leave local data unchecked to preserve artifacts, specs, work items, settings,
and evidence.

To purge the managed deployment and data in automation, back up first:

```bash
specgate uninstall --purge-data --yes
```

This removes the managed appliance container, volume, network, and deployment
directory for the selected Compose project. Container images remain in Docker's
cache. Cleanup is constrained by `org.specgate.managed=true` and
`org.specgate.project=<project>` labels. Directory removal also requires
SpecGate's ownership marker; an arbitrary directory, user home, filesystem root,
or Git repository root is refused.

Verify cleanup:

```bash
docker ps -a --filter label=org.specgate.managed=true --filter label=org.specgate.project=specgate
docker volume ls --filter label=org.specgate.managed=true --filter label=org.specgate.project=specgate
docker network ls --filter label=org.specgate.managed=true --filter label=org.specgate.project=specgate
```

## Troubleshooting

### `specgate doctor` reports unavailable

- run `specgate doctor --fix` to repair or start the CLI-managed appliance;
- run `specgate local-status`;
- inspect `docker compose ps` and `docker compose logs specgate`;
- confirm the configured server ends in `/api/doc-registry`.

### An internal service is unhealthy

Inspect `/healthz/components` and the appliance log. A local installation
should not expose an internal service port to work around a failed health
check.

For an appliance deployment, inspect the combined service log and its
configuration before changing ports or storage settings.

### Artifact uploads fail

- for appliance-managed storage, run `specgate doctor` and inspect the combined
  appliance log;
- for S3-compatible storage, verify endpoint, bucket, credentials, region, and
  path-style settings.

### Workspace-scoped Knowledge search returns no results

- confirm `KNOWLEDGE_DRIVER=pgvector`;
- configure embeddings — see [Configure models](configure-models.md);
- check the embedding dimension matches the selected model;
- reindex affected knowledge after changing model dimensions.

## Continue

- [Configuration reference](../reference/configuration.md)
- [Trust and security](../concepts/trust-and-security.md)
- [Connect delivery integrations](connect-integrations.md)
