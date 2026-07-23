# Release guide

This guide is for maintainers preparing or publishing a SpecGate release. The
executable companion is
[`../release-readiness.test.mjs`](../release-readiness.test.mjs).

## Go / No-Go

Release only when:

- release-readiness passes;
- every release-facing module passes its test and static-analysis checks;
- the native Windows CLI build and updater regression tests pass;
- public install works from a clean machine or scratch `HOME`;
- no secrets, local config, or generated machine files are staged;
- release images and local appliance bundle match the tag.

## Documentation

Check that:

- root README says SpecGate is a v0.1 early release, CLI-first, and trusted-network by default;
- [Quickstart](../using-specgate/quickstart.md) works without a source checkout;
- IDE plugin docs point to CLI install first, then `specgate plugins install`;
- model setup is optional for core operation;
- uninstall docs explain preserved data and purge behavior;
- current docs are linked from [docs home](../README.md);
- retired terminology and placeholder text are absent from public docs.

Run:

```bash
node --test docs/release-readiness.test.mjs
```

## Source Hygiene

Check:

```bash
git status --short
git diff --check
git diff --cached
```

Do not stage `.env` files, local deployment files, generated IDE
rules, provider tokens, webhook secrets, JWTs, or credentials.

## Module Verification

Run the suites for modules touched by the release:

```bash
cd app/cli && make test
cd app/doc-registry && make test
cd app/agents && uv run pytest -q
cd app/ui && npm run lint && npm run build && npm run test -- --run
```

For cross-module contract changes, also run release-readiness.

The tag-triggered `release.yml` repeats the Doc Registry, CLI, governance
operations, UI, and release-readiness checks in its `verify` job. No publishing
job starts unless that verification gate succeeds; local results remain useful
for iteration but never replace the tag's own evidence. A separate
`windows-latest` job builds the native executable and runs the updater
regressions before GoReleaser may publish CLI assets.

## Packaging

Check that:

- CLI installer prefers the latest stable GitHub Release and falls back to the
  newest public prerelease only before a stable release exists;
- CLI installer sends normal installs to `specgate init` and uses
  `specgate config server` only for an explicit `--server` URL;
- Local CLI packaging includes the complete tracked Codex, Claude Code, and
  Cursor plugin package;
- `deploy/local` contains one `specgate` service, one published
  `SPECGATE_PORT`, and one `specgate-data` volume;
- the appliance image is pinned to the release tag;
- the gateway routes `/api/doc-registry/` and `/api/agents/` internally;
- SpecGate-managed containers, volumes, networks, and images carry
  `org.specgate.managed=true`.

Validate Compose:

```bash
(trap 'rm -f deploy/local/specgate.env' EXIT
 cp deploy/local/specgate.env.example deploy/local/specgate.env
 docker compose --env-file deploy/local/.env.example -f deploy/local/compose.yml config --quiet)
```

## Publish

Start the release by pushing the version tag:

```bash
VERSION=vX.Y.Z
git tag -a "$VERSION" -m "SpecGate $VERSION"
git push origin "$VERSION"
```

Do not create or publish a GitHub Release before pushing the tag. GoReleaser
creates the draft, and `release.yml` publishes it only after all release gates
pass. Publishing through the GitHub UI first exposes unverified assets and
causes the workflow preflight to stop the release.

Check:

- tag points at the intended commit;
- `release.yml` succeeds for the tag;
- the GitHub Release remains a draft until image manifests, the local bundle,
  and the clean-install appliance smoke test all succeed;
- GitHub Release contains CLI artifacts and the local appliance bundle;
- the `specgate` appliance image exists for `linux/amd64` and `linux/arm64`;
- each architecture-specific appliance image carries OCI provenance and SBOM
  attestations;
- each appliance digest passes the release vulnerability gate: fixed
  vulnerabilities rated high or critical block publication;
- image tags match the release tag.
- the mutable `latest` image tag moves only for a stable release and only after
  the clean-install appliance smoke test passes; prereleases never move it.

The workflow publishes the draft only after those checks pass. A failed build,
scan, bundle upload, or smoke test leaves the release private for maintainer
inspection. The blocking scan is limited to fixed high and critical findings;
lower-severity and currently unfixed findings are handled through routine base
image and dependency upgrades.

Workflow-level token access is read-only. Only the jobs that publish GitHub
release assets or GHCR manifests receive the corresponding `contents: write`
or `packages: write` permission.

`.grype.yaml` contains the only temporary exception: Python `CVE-2026-15308`,
whose listed fix is unreleased Python 3.15. Remove it when a supported runtime
ships the fix; every other fixed high or critical finding continues to block a
release.

Because draft assets have no public download URL, the smoke job fetches the CLI
and appliance bundle with the authenticated GitHub CLI, verifies the bundle
checksum, and stages it exactly as `specgate init` would before starting the
appliance. The CLI bounds both downloads and extracted bytes and accepts only
canonical regular-file entries for the four top-level bundle contract files
(`compose.yml`, `.env.example`,
`specgate.env.example`, and `rollback-compatible`); extra, duplicate, linked,
or traversal entries fail before startup.

Keep the workflow URL, release asset list, image digests/platforms, and
`specgate --version` output from a fresh install.

## Fresh-User Validation

Use an isolated `HOME` and a non-default appliance port when possible. Confirm
the local bundle starts exactly one container and creates exactly one named
volume.

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
specgate --version
specgate init --mode full --no-seed --no-input
specgate doctor
specgate local-status
specgate plugins install
specgate plugins doctor
specgate status
specgate work list
specgate artifact list
specgate feature list
specgate skill list
```

Then verify:

- UI loads at `http://localhost:<SPECGATE_PORT>`;
- CLI uses `http://localhost:<SPECGATE_PORT>/api/doc-registry`;
- no Postgres, Doc Registry, or Agents port is published;
- onboarding/user/workspace selection works;
- optional model setup accepts provider, model, and key without printing the key;
- quick, full, small-change, and bugfix workflows can create work, read Context
  Packs, submit delivery evidence, and read delivery review status;
- IDE-agent plugin flow can pick up SpecGate instructions and use the CLI.

## Uninstall Validation

Safe uninstall:

```bash
specgate uninstall
```

Confirm local data is preserved when data removal is not selected.

Purge in a scratch deployment:

```bash
specgate uninstall --purge-data --yes
docker ps -a --filter label=org.specgate.managed=true
docker volume ls --filter label=org.specgate.managed=true
docker network ls --filter label=org.specgate.managed=true
docker image ls --filter label=org.specgate.managed=true
```

Expected result after purge: no SpecGate-managed appliance container, volume,
or network remains. The downloaded appliance image stays in Docker's cache so a
later reinstall does not need to pull it again; users may remove that cache
separately with Docker tooling.

## Blockers

If validation finds a release blocker:

1. Patch narrowly.
2. Update docs and tests in the same change.
3. Run relevant tests and release-readiness.
4. Publish a new tag only when released assets must change.
5. Repeat fresh-user validation against the new assets.

Do not reuse evidence from a previous tag after republishing.
