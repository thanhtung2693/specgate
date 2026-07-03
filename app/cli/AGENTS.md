# cli — Agent Rules

Extends root [../AGENTS.md](../AGENTS.md). Read that first; this file only adds module-specific conventions.

## Stack

- Go 1.26 (self-contained `go.mod` under `app/cli/`).
- CLI framework: **cobra v1.10.2**.
- Interactive prompts: **huh v1.0.0** (`internal/interactive/`).
- Output contract: JSON envelopes (`internal/output/`), exit codes 0–6.
- HTTP client: custom wrapper over `net/http` (`internal/client/`).

## Architecture rules

- `app/cli/` is a **separate Go module** — never import from `app/doc-registry/` or `app/agents/`.
- All server interaction goes through the versioned REST facades in `internal/client/` — never call doc-registry internals directly.
- Output must always be a well-formed JSON envelope in `--json` mode (schema: `internal/output/output.go`).
- Exit codes are defined in `internal/output/`: 0=OK, 1=governance_failed, 2=usage, 3=not_found, 4=conflict, 5=unavailable, 6=incompatible.
- `--json` implies `--plain` and `--no-input`; enforce this in `PersistentPreRunE` in `internal/command/root.go`.
- Server URL precedence: `--server` flag > `SPECGATE_SERVER` env > `os.UserConfigDir()/specgate/config.json` > `http://localhost:8080`.
- Config is written atomically with mode 0600 — see `internal/config/config.go`.
- Build metadata (`Version`, `Commit`, `Date`) injected via ldflags into `internal/buildinfo/buildinfo.go`.
- Released CLI builds may check GitHub releases for human/plain update warnings.
  The check is cached and can be disabled with `SPECGATE_NO_UPDATE_CHECK=1`;
  `--json`, `CI=true`, and `dev` builds suppress it.
- `specgate update` is the user-facing "update everything" path: CLI from
  GitHub, IDE plugins from the public plugin registry, and the local release
  Compose stack (Doc Registry, agents, UI, and future bundle services) when a
  deployment directory exists.

## Environment variables

| Variable | Purpose |
|----------|---------|
| `SPECGATE_SERVER` | Overrides the saved server URL. |
| `SPECGATE_NO_UPDATE_CHECK` | Disables the public GitHub release freshness check when truthy. |
| `CI` | Suppresses human-facing public update checks when truthy. |

## Package layout

```
cmd/specgate/main.go        process entry point
internal/buildinfo/         version/commit/date ldflags target
internal/config/            config file (load, save, resolve server)
internal/output/            JSON envelope, exit codes, ExitError
internal/command/           cobra root command + Deps struct
  root.go                   NewRootCommand, ExecuteForCode, DefaultDeps
  system.go                 system subcommands, `version`, `uninstall`, and CLI update warnings
  stats.go                  `stats` governance-value readout (reviewed items, first-pass yield, catches, rework, ambiguity saves, cycle time, recent-catch ledger) from GET /api/v1/stats
  work.go                   work subcommands: `work create-quick` (positional title + repeatable --ac, interactive AC loop), `work archive` for retiring one or more work refs; `work policy` as the canonical user-facing policy explanation; `work list` renders the status board's attention section
  artifact.go               artifact subcommands; interactive impact_declaration collection on publish
  gates.go                  unified gates family: work-item LLM gates (`gates run|status|history`), artifact readiness (`gates check`), and artifact gate tasks (`gates tasks list|show|submit-result|preview|dispatch`)
  delivery.go               delivery subcommands: report (plus --init completion.json scaffold), submit (report → gates → review → status chain), review, status
  confirm.go                requireConfirm — confirmation prompts are TTY-only; non-interactive runs proceed (--yes accepted for compatibility)
  policy.go                 advanced policy subcommands: `policy list`
  model.go                  model configuration subcommands: `model set`, `model show`
  identity.go               user/workspace subcommands and selected identity config helpers
  plugins.go                IDE plugin install and doctor subcommands
internal/client/            typed HTTP client for /api/v1/ plus served plugin package files
internal/interactive/       huh-based prompts; ImpactAnswers + CollectImpactDeclaration
```

## Testing

- Run: `make test` (`go test -race -count=1 ./...`).
- Tests are co-located with source (`_test.go` in the same package or `_test` suffix package).
- Use `t.Setenv()` for env isolation — never mutate global state.
- Tests that assert human-facing GitHub update warnings must clear `CI`; GitHub
  Actions sets it, and released CLI builds intentionally suppress public update
  checks in CI.
- Use `t.TempDir()` for file-system isolation.
- Command tests use `ExecuteForCode(NewRootCommand(deps), ...)` with injected fake deps — never call `os.Exit` in tests.

## Commands

| Make target | Purpose                          |
|-------------|----------------------------------|
| `make build`| Compile to `bin/specgate`        |
| `make test` | Tests with race detector         |
| `make lint` | `go vet ./...`                   |
| `make fmt`  | `gofmt -s -w .`                  |
| `make tidy` | `go mod tidy`                    |

## When adding / changing

- **New command** → add `Register*` function in the relevant `internal/command/*.go` file; register in `NewRootCommand`. Update the command table in this AGENTS.md.
- **New env var** → add to `internal/config/config.go`, `.env.example` if one exists, and the env table in this file.
- **New exit code** → add constant in `internal/output/output.go` and update `errorCodeToExit` map. Keep this AGENTS.md exit-code table in sync.

## Safety

- Never persist credentials in config. Tokens go in the OS keychain — not in `config.json`.
- Never log full JWT or API key content.
