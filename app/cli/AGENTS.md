# CLI Contributor Rules

Extends the root [contributor rules](../../AGENTS.md). This file applies only to
changes under `app/cli/`.

## Stack and ownership

- Go 1.26+, Cobra, Huh, TOML configuration, and SQLite.
- `internal/command/` owns command composition, human interaction, and the
  embedded loopback review surface.
- `internal/local/` owns SQLite behavior.
- `internal/config/` owns project and user configuration.
- `internal/output/` owns machine-readable envelopes and exit codes.

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
- Read JSON input files through `readJSONBodyFile`. Keep decoding and
  null-object handling consistent.
- Preserve workspace precedence and require explicit workspace identity for
  workspace-scoped operations.
- Write configuration atomically with mode `0600`.
- Released update checks must remain suppressible and must not pollute JSON
  output, CI, or development builds.

## Filesystem and credential safety

- Keep prior remote configuration intact; it does not select an active transport.
- Never print secret values. Status output reports only set/not-set state.
- Commands that touch plugins, IDE configuration,
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
- Changes to Local workflow vocabulary or loopback API payloads update
  `docs/contributing/contracts.md`.
- Plugin asset changes start in `plugins/`; run `make sync-plugins` and
  `make check-plugins` instead of editing embedded copies.
