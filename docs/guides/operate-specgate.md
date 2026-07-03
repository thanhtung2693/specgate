# Operate SpecGate

Use this guide when you need to start, stop, back up, upgrade, or remove a
local or small-team SpecGate deployment.

## Choose CLI-managed or manual Compose

### CLI-managed

Best for evaluation and straightforward self-hosting:

```bash
specgate init
```

The CLI manages a deployment directory, Compose bundle, environment files,
secrets, startup, and optional demo data.

### Manual Compose

Best when infrastructure automation manages environment files, image versions,
networks, volumes, and reverse proxies.

See the [release Compose guide](../../deploy/README.md).

## Start, inspect, stop, and restart a CLI-managed stack

```bash
specgate local-status
specgate up
specgate down
```

`down` stops the stack and preserves persistent data. `up` starts it again and
waits for health checks.

Check CLI-to-service compatibility:

```bash
specgate doctor
```

## Service URLs

Default host ports:

| Service | URL |
|---|---|
| Doc Registry and Swagger | `http://localhost:8080` |
| Governance-ops | `http://localhost:2024` |
| Postgres | `localhost:5432` |

Optional Redis, object-storage, and queue-monitor services appear only when the
selected deployment enables them.

## Choose queue and storage drivers

SpecGate can run lean with PostgreSQL and local disk.

### Queue

- `QUEUE_DRIVER=sync` — process webhook work inline; no Redis required.
- `QUEUE_DRIVER=redis` — use Redis/asynq for asynchronous webhook processing.

To use the bundled Redis container with Compose, enable the `redis` profile and
point Doc Registry at it:

```bash
COMPOSE_PROFILES=redis
QUEUE_DRIVER=redis
REDIS_URL=redis://redis:6379/2
```

### Blob storage

- `STORAGE_DRIVER=local` — store artifact and upload blobs under
  `BLOB_DATA_ROOT`.
- `STORAGE_DRIVER=s3` — use S3 or MinIO.

To use the bundled MinIO container with Compose, enable the `s3` profile:

```bash
COMPOSE_PROFILES=s3
STORAGE_DRIVER=s3
S3_ENDPOINT=http://minio:9000
S3_ACCESS_KEY=specgate-minio
S3_SECRET_KEY=specgate-minio-dev-only
```

### Knowledge search

- `KNOWLEDGE_DRIVER=pgvector` — index knowledge in PostgreSQL.
- `KNOWLEDGE_DRIVER=none` — disable vector search.

Choose the simplest drivers that meet your workload. Moving existing data
between drivers is an operational migration, not a toggle to change casually.

## Configure environment and secrets

Important values:

- `SETTINGS_ENCRYPTION_KEY` — required 32-byte hex key used to encrypt settings;
- `POSTGRES_DSN` — Doc Registry database;
- `ADMIN_SECRET` — optional evidence-actor administration;
- provider OAuth client IDs and secrets;
- storage or Redis credentials when those drivers are enabled.

Generate an encryption key:

```bash
openssl rand -hex 32
```

Back up this key with the database. Losing it makes encrypted settings
unrecoverable.

Model provider and embedding keys are normally configured via `agents.env` keys
or the settings API (`PUT /settings`), not deployment environment files. See
[Configure models](configure-models.md).

## Persist and back up data

Back up:

- PostgreSQL data, including artifact metadata, work items, settings,
  evidence, and gate history;
- local blob directory or S3 bucket, including artifact/spec document contents;
- deployment environment files;
- `SETTINGS_ENCRYPTION_KEY`;
- integration and OAuth configuration.

For the default CLI-managed Compose stack, persistent data is stored in these
volumes:

| Volume | Contents |
|---|---|
| `postgres-data` | artifact rows, work items, features, settings, evidence, gate history |
| `doc-registry-data` | artifact/spec document blobs under `/data/blobs` |

Compose volumes survive `specgate down` and normal `docker compose down`. They
do not survive `specgate uninstall --purge-data --yes`.

Before upgrades:

1. record current image version;
2. back up database and blobs;
3. preserve environment files;
4. confirm rollback procedure;
5. upgrade;
6. run `specgate doctor`;
7. inspect key workflows.

## Upgrade the CLI and services

Update CLI and IDE integration:

```bash
specgate update
```

For CLI-managed services, `specgate update` also refreshes the Compose bundle,
pins the latest release, pulls images, and restarts services. Then verify:

```bash
specgate doctor
```

Avoid floating `latest` for important deployments. Pin a release so rollback is
predictable.

## Resolve port conflicts

The source-checkout setup detects common host-port conflicts. CLI-managed
deployments can use a dedicated directory and edited Compose environment.

Common defaults:

- `DOC_REGISTRY_PORT=8080`
- `AGENTS_PORT=2024`
- `UI_PORT=3000`
- `POSTGRES_PORT=5432`
- `SPECGATE_COMPOSE_PROJECT=specgate` in release-bundle deployments

After changing Doc Registry or agents host ports, ensure the UI build and CLI
server URL point to the new addresses.

## Read service logs

From the deployment directory:

```bash
docker compose ps
docker compose logs doc-registry
docker compose logs agents
docker compose logs postgres
```

Use `--tail` or `-f` when following an active issue:

```bash
docker compose logs -f --tail=200 doc-registry
```

Do not paste logs publicly before checking for tokens, signed URLs, repository
names, or business content.

## Remove a deployment safely

Stopping is non-destructive:

```bash
specgate down
```

Removing Compose volumes or local blob data permanently deletes artifact/spec
state.

**Before deleting data:**

1. confirm the deployment directory;
2. back up database and blobs;
3. confirm no other deployment shares the volumes or bucket;
4. stop the services or use the uninstall command;
5. pass the destructive flag only after backup is confirmed.

### Remove user-local setup but keep data

```bash
specgate uninstall
```

Interactive mode shows a checklist for:

- IDE plugin files;
- local data, including Docker volumes and deployment files;
- Docker images for SpecGate services.

Leave local data unchecked when you want to preserve artifacts, specs, work
items, settings, and evidence.

### Purge everything in automation

```bash
specgate uninstall --purge-data --yes
```

This removes:

- SpecGate-managed containers;
- SpecGate-managed Docker volumes;
- SpecGate-managed Docker networks;
- the deployment directory;
- SpecGate service images referenced by that deployment.

Containers, volumes, and networks are filtered by `org.specgate.managed=true`
and the deployment compose project so one local stack does not purge another.
Shared base images such as Postgres or Redis are not removed.

### Verify cleanup

```bash
docker ps -a --filter label=org.specgate.managed=true --filter label=org.specgate.project=specgate
docker volume ls --filter label=org.specgate.managed=true --filter label=org.specgate.project=specgate
docker network ls --filter label=org.specgate.managed=true --filter label=org.specgate.project=specgate
docker image ls --filter label=org.specgate.managed=true
```

The container, volume, and network commands should show no resources for the
purged project after a full purge. Image output may still show images used by
another SpecGate stack or retained by Docker because another container
references them.

## Troubleshooting

### `specgate doctor` reports unavailable

- run `specgate doctor --fix` to start or repair CLI-managed local services;
- run `specgate local-status`;
- inspect `docker compose ps`;
- check Doc Registry logs;
- confirm `specgate config set server` points to the correct URL.

### Agents service is unhealthy

- check model/runtime environment;
- check LangSmith requirements for the selected self-hosted image;
- inspect Postgres/Redis connectivity if using the durable runtime;
- inspect `docker compose logs agents`.

### Artifact uploads fail

- for local storage, verify `BLOB_DATA_ROOT` exists and is writable;
- for S3, verify endpoint, bucket, credentials, region, and path-style setting.

### Knowledge search returns no results

- confirm `KNOWLEDGE_DRIVER=pgvector`;
- configure embeddings — see [Configure models](configure-models.md);
- check embedding dimension matches the selected model;
- reindex affected knowledge after changing model dimensions.

### Webhooks are slow

`QUEUE_DRIVER=sync` processes inline. Switch to Redis only when you need
asynchronous throughput and are ready to operate Redis and the worker.

## Continue

- [Configuration reference](../reference/configuration.md)
- [Trust and security](../concepts/trust-and-security.md)
- [Connect delivery integrations](connect-integrations.md)
