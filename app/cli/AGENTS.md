# CLI Contributor Rules

Extends the root [contributor rules](../../AGENTS.md). This file applies only to
changes under `app/cli/`.

## Stack and ownership

- Go 1.26+, Cobra, Huh, TOML configuration, and a custom `net/http` client.
- `app/cli/` is an independent Go module. Never import Doc Registry or agents
  internals.
- `internal/command/` owns command composition and human interaction.
- `internal/client/` owns versioned Full-mode REST calls.
- `internal/local/` owns Local-mode SQLite behavior.
- `internal/config/` owns project and user configuration.
- `internal/output/` owns machine-readable envelopes and exit codes.
- `internal/deploy/` owns released appliance lifecycle and recovery packages.

The public command catalog belongs in
[`docs/using-specgate/reference/cli.md`](../../docs/using-specgate/reference/cli.md);
do not duplicate it here.

## Command contracts

- Machine mode must always emit a valid JSON envelope. `--json` implies
  `--plain` and `--no-input`.
- Exit codes are stable API: `0` success, `1` governance failure, `2` usage,
  `3` not found, `4` conflict, `5` unavailable, and `6` incompatible.
- Commands reject positional arguments by default. Any command that accepts
  them must declare the policy explicitly.
- Map HTTP failures through `apiExitError`. Read JSON input files through
  `readJSONBodyFile`; do not duplicate decoding and null-object handling.
- Local mode resolves before creating an HTTP client and must not silently call
  a remote service. Full mode uses only `internal/client/`.
- Preserve documented server and workspace precedence. Workspace identity must
  be explicit in every workspace-scoped local or remote operation; never fall
  back across workspaces.
- Write configuration atomically with mode `0600`.
- Released update checks must remain suppressible and must not pollute JSON
  output, CI, or development builds.

## Filesystem and credential safety

- Never persist provider API keys or other credentials in CLI configuration.
  `model set` may send a key only to the explicitly selected trusted server,
  where server settings own encryption and redaction.
- Never print secret values. Status output reports only set/not-set state.
- Commands that touch plugins, IDE configuration, deployment directories,
  `.specgate/`, backups, or user home files must use scoped paths and preserve
  unrelated content.
- Cleanup and uninstall must distinguish SpecGate-owned transient/runtime state
  from user-owned specifications and project files. Destructive selections need
  confirmation in interactive use and explicit flags in automation.
- Recovery and secret-bearing files use mode `0600`.

## Testing

```bash
make test
make lint
make build
```

- Use `ExecuteForCode(NewRootCommand(deps), ...)`; never call `os.Exit` in
  command tests.
- Use `t.Setenv` and `t.TempDir` for isolation.
- Inject `Deps.UserHomeDir` for plugin, update, cleanup, and uninstall tests.
  Never let tests touch a contributor's real `.codex`, `.agents`, `.cursor`,
  `.claude`, or `.specgate` directories.
- Tests for update warnings must control `CI` explicitly.
- Add regression tests proving cleanup/install/uninstall preserves unrelated
  files whenever those boundaries change.

## Documentation

- Command syntax, output, mode support, or workflow changes update the CLI
  reference and affected user guide in `docs/using-specgate/`.
- Configuration precedence or environment changes update the configuration
  reference, owning config tests, and `.env.example` when applicable.
- Cross-service request/response changes update
  `docs/contributing/contracts.md`.
- Plugin asset changes start in `/plugins`; run `make sync-plugins` and
  `make check-plugins` instead of editing embedded copies.
