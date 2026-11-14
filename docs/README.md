# SpecGate documentation

Start with the path that matches your goal. You do not need to read the whole
documentation set before using SpecGate.

## I want to use SpecGate

[Using SpecGate](using-specgate/README.md) helps developers, reviewers, and
operators run SpecGate and use it with a coding agent.

Start here when you want to:

- install the CLI and initialize a Local store;
- connect Codex, Claude Code, or Cursor;
- create governed work and review delivery evidence;
- add Local rubric instructions or back up Local state;
- troubleshoot gates or look up CLI behavior.

New users should begin with the
[Quickstart](using-specgate/quickstart.md).

## I want to contribute to SpecGate

[Contributing to SpecGate](contributing/README.md) helps contributors modify the
repository, understand its architecture, and maintain releases.

Start here when you want to:

- prepare a development environment;
- understand module boundaries and shared contracts;
- change the data model or architecture;
- choose and run repository tests;
- record an architecture decision or publish a release.

New contributors should begin with
[Contributor setup](contributing/setup.md).

## Local trust boundary

The review UI listens on loopback and assumes a trusted machine. See
[Trust and security](using-specgate/concepts/trust-and-security.md).
