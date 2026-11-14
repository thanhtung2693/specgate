# Contributor setup

Read the [repository rules](../../AGENTS.md) and the owning module's instructions
before editing. Use the Go toolchain selected in the CLI module, Node.js 26
for the JavaScript checks, and Git. Canonical plugin tooling also uses Python 3
and a POSIX shell.

## Build and verify

```bash
make build
make test
make lint
```

For isolated Local tests or dogfood, create a disposable Git checkout and use
a separate temporary configuration file via `SPECGATE_CONFIG_PATH`. Initialize
a separate Local store using `specgate init --local-dir`. See
[Local operation](../using-specgate/guides/operate-specgate.md) for backup
and recovery details. Manage contributor binaries separately from an installed
release.

## Update plugins

Edit the canonical source under `plugins/`. Regenerate metadata if the canonical
metadata changes, then sync the CLI embed:

```bash
make generate-plugins
make sync-plugins
make check-plugins
```

IDE files written by a product install belong to the user or their repository.
Verify installation with disposable homes and checkouts.

## Continue

- [Testing strategy](testing.md)
- [Architecture](architecture.md)
- [Release guide](release.md)
