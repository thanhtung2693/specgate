# Operate SpecGate

Use this guide to start, stop, back up, upgrade, or remove SpecGate.

## Install the Full appliance

Use the CLI:

```bash
specgate init --mode full
```

The complete product runs in one Docker container with one public port and one
named volume. The CLI manages installation and lifecycle commands. For the
no-Docker Local workflow, use the [quickstart](../quickstart.md).

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

Only the public port is exposed. To choose it at first install:

```bash
SPECGATE_PORT=13000 specgate init --mode full
```

To change it later, edit the port setting in the deployment `.env`:

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

For a CLI-managed appliance, `doctor` can still report component health when
the public gateway is unavailable.

### Local logs

From the deployment directory:

```bash
docker compose ps
docker compose logs -f --tail=200 specgate
```

Check the appliance log before changing its port or configuration.

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

This removes the managed appliance, its data volume, and its deployment
directory. Container images remain in Docker's cache. SpecGate refuses to
remove directories or Docker resources it does not own.

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

### The appliance is unhealthy

Run `specgate doctor`, then inspect the appliance log. Do not expose additional
container ports to work around a failed health check.

### Artifact uploads fail

- run `specgate doctor`;
- inspect the appliance log for the first storage error.

### Workspace-scoped Knowledge search returns no results

- configure embeddings — see [Configure models](configure-models.md);
- run `specgate doctor`;
- retry indexing the affected document after correcting model settings.

## Continue

- [Configuration reference](../reference/configuration.md)
- [Trust and security](../concepts/trust-and-security.md)
- [Connect delivery integrations](connect-integrations.md)
