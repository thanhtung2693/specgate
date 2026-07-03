# OSS release checklist

Use this checklist before publishing an open-source SpecGate release. It covers
docs, code, security, CI, installers, and post-publish validation.

The executable companion is
[`../release-readiness.test.mjs`](../release-readiness.test.mjs).

## Go/no-go rule

Ship only when:

- release-readiness passes;
- changed modules pass their relevant tests;
- public install paths work from a clean machine or scratch HOME;
- no secrets, local config, or generated machine files are staged;
- release images and Compose bundle match the tag being published.

## Documentation

- Root README says the release is alpha, CLI-first, and trusted-network by
  default.
- [Quickstart](../quickstart.md) installs from the public CLI installer and can
  be followed without a source checkout.
- IDE plugin install docs point at the public plugin installer.
- Model setup is documented as optional for core operation.
- Uninstall docs explain preserved data, purge behavior, and Docker label-based
  cleanup.
- Concept, guide, and reference docs are linked from [docs home](../README.md).
- Retired terminology and placeholder text are absent from release-facing docs.

Run:

```bash
node --test docs/release-readiness.test.mjs
```

## Source hygiene

- `.env` files, local deployment files, local MCP files, and generated IDE rules
  are ignored.
- No API keys, provider tokens, webhook secrets, JWTs, or credentials are
  staged.
- Generated artifacts are committed only when they are release assets or
  canonical fixtures.
- Code changes have matching docs at the narrowest useful layer.

Useful checks:

```bash
git status --short
git diff --check
git diff --cached
```

## Module verification

Run the suites for modules touched by the release:

```bash
cd app/cli && make test
cd app/doc-registry && make test
cd app/agents && uv run pytest -q
cd app/ui && npm run lint && npm run build && npm run test -- --run
```

For cross-module contract changes, run the affected suites and the
release-readiness gate.

## Packaging

- CLI installer resolves alpha prereleases from GitHub Releases, not only
  `/latest`.
- Release Compose uses pinned release images:
  `ghcr.io/thanhtung2693/doc-registry`, `ghcr.io/thanhtung2693/agents`, and
  `ghcr.io/thanhtung2693/ui`.
- Compose supports non-default host ports and side-by-side local stacks.
- Containers, volumes, networks, and SpecGate service images carry
  `org.specgate.managed=true` labels.
- Doc Registry image owns `/data` as the app user.
- UI image uses same-origin API defaults and nginx proxying.

Validate a bundle before upload:

```bash
docker compose -f deploy/compose/compose.yml config --quiet
```

## Publish

- Tag points at the intended commit.
- `release.yml` succeeds for the tag.
- GitHub Release contains CLI artifacts and the Compose bundle.
- Multi-platform images exist for `linux/amd64` and `linux/arm64`.
- Image tags match the release tag.

Evidence to keep:

- workflow run URL and final state;
- release asset list;
- image names, digests, and platforms;
- `specgate --version` output from a fresh install.

## Fresh-user validation

Use an isolated HOME and non-default ports when possible.

Minimum flow:

```bash
curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh
specgate --version
specgate init --no-seed --no-input
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

- UI loads at the configured UI port;
- release automation has exercised `specgate doctor --fix --yes` after stopping a
  fresh CLI-managed stack;
- onboarding/user/workspace selection works;
- optional model setup accepts a provider, model, and key without printing the
  key;
- quick, full, small-change, and bugfix workflows can create work, read Context
  Packs, submit delivery evidence, and read delivery review status;
- IDE-agent plugin flow can pick up SpecGate instructions and use the CLI.

## Uninstall validation

Run safe uninstall first:

```bash
specgate uninstall
```

Confirm local data is preserved when data removal is not selected.

Then test purge in a scratch deployment:

```bash
specgate uninstall --purge-data --yes
docker ps -a --filter label=org.specgate.managed=true
docker volume ls --filter label=org.specgate.managed=true
docker network ls --filter label=org.specgate.managed=true
docker image ls --filter label=org.specgate.managed=true
```

Expected result after purge: no SpecGate-managed containers, volumes, networks,
or service images remain. Shared base images should remain unless they carry
SpecGate labels.

## Deferred / backlog

Ideas kept off the public roadmap until real user pull justifies them. Not
commitments; recorded here so the context is not lost.

- **Feature Overview auto-generation.** A generator that writes a Feature's
  `## Overview` narrative from its canonical spec (grounded, no fabrication,
  human-editable) and refreshes it on canonical change. Prototyped as
  `board/feature_summary.py` and removed in the alpha simplification
  (`157f45c7`). The store endpoint (`PUT /workboard/features/{id}/summary`)
  and the `feature_summary_outdated` staleness warning remain live, so a
  rebuild only needs the generator, a canonical-change trigger, a real setting
  behind it, and tests. Deprioritized: it is retention/depth for larger teams,
  not core-loop acquisition, and an auto-written narrative must not undercut
  the "verdicts mean something" honesty of the governed loop.

## Blocker handling

If validation finds a release blocker:

1. patch narrowly;
2. update docs and tests in the same change;
3. run relevant tests and release-readiness;
4. publish a new tag only when released assets must change;
5. repeat fresh-user validation against the new assets.

Do not reuse evidence from a previous tag after republishing.
