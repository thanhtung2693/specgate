# Release guide

SpecGate releases the Local CLI. A tagged build ships Darwin and Linux tarballs
for amd64 and arm64, a Windows amd64 ZIP, a SHA-256 checksum manifest, and the
POSIX `install-cli.sh`. The optional browser UI and matching IDE files are
embedded in each binary. The foreground notification pilot is source-only.

## Prepare the candidate

- Add an authored changelog section for the candidate version. Update
  `plugins/package.json` to the same version and run `make sync-plugins`
  and `make check-plugins`. Keep user-facing setup, command and recovery
  documentation aligned with the CLI, and distinguish the candidate from
  the published version in the README.
- Run `make test`, `make lint`, the Local legacy compatibility gate, and the
  relevant native/browser checks from [Testing](testing.md).
- Verify the public landing and interactive demo. The Pages workflow builds
  the demo from the current Local review presentation before uploading the
  site.
- Create a local GoReleaser snapshot and check all five archives, their checksum
  entries, installer help, and the host binary's reported version. Run the
  [delivery proof](../using-specgate/guides/try-delivery-proof.md) using that
  executable and its disposable fixture. A repaired delivery still ends at
  human review.
- Validate a representative existing Local store in a copy, preserving the
  original. Exercise Local transfer in both directions between disposable
  bound checkouts with separate configurations. Verify that the receiving
  store requires a transfer-capable CLI and that activation preserves the
  source store. Check Local setup with a separate configuration when a
  legacy remote configuration is present. Preserve old deployments and data.

## Publish an approved version

Use a signed or annotated tag for the approved commit and confirm that the
changelog has that version's notes. Publishing requires separate authorization.

```bash
git tag -a "$VERSION" -m "$VERSION"
git push origin "$VERSION"
```

Do not create or publish a GitHub Release before the workflow. A published
release for that tag fails preflight before building. GoReleaser creates or
reuses exactly one draft, replacing matching assets on a retry. Linux smoke
downloads the draft assets, checks all five archives, verifies the installer
matches the tagged script, and runs the candidate through the Local delivery
proof. The release waits for native Windows CLI and transfer verification and
the draft smoke.
On failure, inspect the existing draft and failed gate before retrying the
same workflow. A prerelease tag stays a prerelease.

After successful publication, verify the public tag, asset names, checksums and
release notes in GitHub Releases. Run the public installer only against an
isolated install directory when validating a released version.

## Uninstall validation

The ordinary Local uninstall removes managed global setup and preserves the
store. To test data purge, create an isolated fixture with unrelated files
beside the database and verify those files survive. See
[Local CLI uninstall](../using-specgate/reference/cli.md#local-cli-uninstall).
