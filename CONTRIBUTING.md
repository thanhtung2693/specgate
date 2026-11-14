# Contributing to SpecGate

Read the [contributor rules](AGENTS.md) and
[contributor documentation](docs/contributing/README.md) before editing.
Keep each change paired with its owning user or contributor documentation.

## Verify changes

```bash
make test
make lint
```

The [testing strategy](docs/contributing/testing.md) covers the Local legacy
store gate, native Windows checks, and rendered browser verification. Follow
the [release guide](docs/contributing/release.md) to prepare a candidate.

Use a conventional commit with the owning scope when asked to commit, for
example `fix(cli): preserve local workspace bindings`.
