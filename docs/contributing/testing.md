# Testing strategy

This reference is for contributors choosing verification for a SpecGate
change. Run the cheapest check that can fail for the behavior you touched, then
broaden only when the change crosses module or contract boundaries.

## Test layers

| Layer | Use for | Command |
|---|---|---|
| CLI unit and harness tests | Commands, SQLite, and embedded review behavior | `make test` in the CLI module |
| Release readiness | Packaging, public docs, installer, and pilot | `node --test docs/release-readiness.test.mjs scripts/install-cli.test.mjs scripts/local-review-notifications.test.mjs` |
| Browser walkthrough | Local review UI and landing paths | Render desktop and narrow viewports and compare with CLI readback |

Go 1.27.3 is selected through the CLI module's toolchain directive. Use
Staticcheck v0.8.2 with it. Compiler or SQLite dependency changes require the
full CLI suite, real legacy-writer check, and isolated Local delivery smoke.

## Module commands

### CLI

```bash
cd app/cli
make test
make lint
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
```

The CLI suite covers command behavior, JSON envelopes, exit codes, local
configuration, user/workspace selection, plugin installation, and uninstall
cleanup.

CLI command fixtures isolate streams, config, working directory, and home.
Direct printer fixtures use `NewWithColor` with an explicit ANSI capability;
root-command tests still exercise terminal/environment capability detection.
Local delivery-decision fixtures call `DecideDeliveryWithBasis`, the production
entry point; an empty digest exercises the supported unenhanced review-ID path.

Enhanced Local-store changes also require a real pre-enhancement CLI, not only
raw SQLite guard tests. CLI CI and release verification run the shared gate:

```bash
bash .github/scripts/check-legacy-cli.sh
```

The gate builds two immutable revisions in a temporary directory. The v0.1.4
baseline predates enhanced stores and exercises the first enhanced upgrade
and backup recovery. The v0.2.6 baseline supports enhanced stores and predates
transfer. It checks that an earlier enhanced writer refuses a received store
with exit code 6 and leaves the database byte-for-byte unchanged. A shallow
checkout fetches missing pinned commits. Temporary baseline files are removed
on exit. Review each baseline's writer capabilities before replacing it.

For a separately built old executable, run:

```bash
cd app/cli
SPECGATE_LEGACY_CLI=/absolute/path/to/old/specgate go test ./internal/local -run 'Test(LegacyBinaryCannotMutateEnhancedStore|PreEnhancedBackupRestoresLegacyWritableSnapshot)$' -v -count=1
SPECGATE_PRE_TRANSFER_CLI=/absolute/path/to/pre-transfer/specgate go test ./internal/local -run '^TestGenerationStoreCompatibilityRefusesBeforeMutation$' -v -count=1
```

The test isolates home, config and database, checks that the binary starts,
then attempts old startup/read, workspace/work creation, verification, review
and acceptance against an enhanced store. Each must encounter the capability
guard without changing any stored rows. Executable-backed checks are opt-in,
so run the shared gate for downgrade evidence.

The recovery test copies the pre-enhanced backup into a separate temporary
directory, verifies that the old CLI can write there, and compares the original
work and Context Pack with the restored data. The first checkpoint and later
work are absent from that snapshot; the live upgraded database and original
backup remain untouched. This verifies snapshot recovery, not a lossless
downgrade or a procedure for overwriting a user's running database.

The barrier-only `EnableEnhancedStore` fixture exists only in test builds.
Production enhanced writes use `WithEnhancedWrite` so the compatibility marker,
mutation guards, and first enhanced record commit together.

When upgrading `modernc.org/sqlite`, keep `modernc.org/libc` at the exact version
required by that driver's `go.mod`. Rerun the full CLI suite, the released-binary
compatibility check, and Local read/write dogfood; a successful build alone does
not verify stored-data compatibility.

JUnit parser fixtures open temporary directory handles and exercise
`ObserveJUnitReportFromRoot`, the same reader used by delivery checks. Production
opens that handle before executing the runner; parser-only fixtures do not
prove the runner's path-replacement boundary. Keep command-level boundary tests
when changing report collection.

Native Windows updater, browser-opening, cache-cleanup, or Local transfer
changes also require the Windows-target build check:

```bash
cd app/cli
GOOS=windows GOARCH=amd64 go build -o /tmp/specgate.exe ./cmd/specgate
GOOS=windows GOARCH=amd64 go test -c ./internal/command -o /tmp/specgate-command.test.exe
GOOS=windows GOARCH=amd64 go test -c ./internal/local -o /tmp/specgate-local.test.exe
```

The CLI and release workflows run updater, browser-boundary, open-command,
Local UI cache-cleanup, and transfer command and generation tests on
`windows-latest`. The release job requires that native check before building
assets. Windows tests and native browser integration require Windows execution.
The browser tests inject
ShellExecute to verify the fixed verb, exact URL document argument, absent
parameters and native-error handling without opening a real browser.

## Release readiness

```bash
make check-plugins
make test
make lint
bash .github/scripts/check-legacy-cli.sh
```

The installer suite uses disposable install directories and offline release
fixtures to exercise checksum-verified replacement and failure paths.

Use the source-checkout [Local delivery proof](../using-specgate/guides/try-delivery-proof.md)
with the candidate binary. Its first submission blocks on missing evidence.
The repair submission reaches the human review boundary. Verify the exact
next actor and preserve any decision for the human.

For a rendered Local review, use a disposable bound Git checkout and private
temporary Local configuration and state. Start the candidate `specgate ui`
process, inspect the review, and compare stable IDs and authority with CLI
status. Check desktop and narrow layout, keyboard focus, and action conflict
behavior where relevant. A browser smoke validates rendering and routing.

## Related

- [Contracts](contracts.md)
- [Governance reference](../using-specgate/reference/governance.md)
- [Operate SpecGate](../using-specgate/guides/operate-specgate.md)
- [Release guide](release.md)
