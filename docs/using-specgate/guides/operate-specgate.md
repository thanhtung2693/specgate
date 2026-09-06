# Operate SpecGate

Use this guide to start, stop, back up, upgrade, or remove SpecGate.

## Operate Local CLI

Local mode has no service to start or stop. From the bound repository, run
`specgate doctor` to check the store, workspace binding, shell, and IDE files.
Use `specgate work resume <work-ref> --json` to pick up a specific work item.

To upgrade the CLI, run `specgate update` or rerun the installer. Users on
`v0.1.4` can upgrade directly to `v0.1.7`; existing SQLite stores open in place
with an additive verification-contract table. Existing work remains
`unconfigured` until a human pins a contract. See the [changelog](../../../CHANGELOG.md).

Refresh CLI-managed plugins using the same agent and global/project scope used
at installation. Update native Codex or Claude Code plugins through their IDE
plugin manager, then start a new IDE session.

For a backup, stop all CLI and IDE-agent operations that write Local state,
then copy the entire selected Local store directory and CLI configuration to a
private backup location. Honor custom `--local-dir`, `SPECGATE_LOCAL_DIR`, and
`SPECGATE_CONFIG_PATH` settings; do not assume the repository's `.specgate/`
directory contains the store. Keep SQLite sidecar files with the database if
present. See [Configuration](../reference/configuration.md#cli).

Portable workspace export is a migration format, not a complete Local backup.
It refuses workspaces with pinned verification contracts because Full mode
cannot preserve those contracts. Switching to Full does not delete the Local
store, but it does not move its history automatically either.

## Install the Full appliance

Use the CLI:

```bash
specgate init --mode full
```

The Full appliance runs in one Docker container with one public port and one
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

Governance-chat threads are ephemeral in the v0.1 appliance and reset when it
restarts. The Full UI does not expose chat history. Artifacts, work items,
approvals, delivery evidence, settings, and Knowledge live in the managed
volume and are included in backups.

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

## Share the appliance with teammates

An appliance trusts its network until you issue credentials. To let two or three
developers work against one host, give each a credential and have them store it:

```bash
# on the appliance operator's machine
specgate workspace credential mai        # prints the secret once
specgate workspace credential mai --revoke   # when they leave

# on that developer's machine
specgate config credential mai           # prompts for the secret
```

The first credential turns authentication on for the API and the browser alike;
revoking the last turns it off. From then on the name on an approval or an
acceptance is the authenticated one, not a supplied string. Read
[Trust and security](../concepts/trust-and-security.md#sharing-one-appliance-with-a-few-developers)
for the two limits that still apply, including why the appliance belongs on a
private overlay network or behind TLS once it leaves loopback.

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
