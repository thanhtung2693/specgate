# Architecture

Use this explanation when changing a SpecGate module or reviewing a
cross-module design. It describes runtime responsibilities, sources of truth,
and trust boundaries; it is not an installation guide.

SpecGate is a local-first governance layer between approved intent and coding
agents. It keeps product scope, artifact versions, Context Packs, delivery
evidence, and review verdicts in one place while users keep their existing IDE,
tracker, and git workflow.

## Runtime

```text
Developer / IDE agent -> specgate CLI -> private SQLite store
Optional browser -> loopback listener inside specgate ui -> same Local owners
```

The CLI stores artifact snapshots, work, checks, evidence, and review
decisions in the selected Local SQLite store. IDE agents call the CLI through
embedded skills and execute frozen semantic-readiness tasks. The optional
embedded browser UI fixes the store, workspace and repository at startup.
The static landing page and IDE package source own no product state.

At artifact preview and publication, the command reads bounded project rubric
files under the selected Git root. Effective rubric text and the relative
source are immutable snapshot data. Task dispatch reads this frozen policy.
Structural checks and human decision authority stay with the CLI.

## Trust Boundaries

Local identity is cooperative attribution. IDE-agent output is evidence.
The store records human decisions separately.

The optional Local UI listens only on loopback. Exact Host validation, a private
browser session cookie, same-origin checks and a CSRF header protect its small
API allowlist against hostile browser origins. Session credentials do not
appear in review URLs. The listener trusts all local accounts and processes
that can reach loopback; it does not authenticate the OS user. Local identity
remains cooperative attribution. The browser
cannot select an arbitrary store, workspace, filesystem path, shell command or
update source. Immutable documents are read through their pinned index.
Discovery descriptors contain only a public session identity and loopback URL,
never browser credentials. Unix checks private directory/file modes; Windows
uses inherited per-user directory ACLs with the same bounded path, file and
live-session validation. These permissions protect discovery metadata, not HTTP
access. A Windows cross-build does not verify native ACL behavior.

Local review projects existing status and acceptance inspection. HTTP decisions
carry the displayed review, completion and acceptance basis; concurrent changes
require a fresh inspection instead of applying a decision to newer evidence.
Authoring specifications, creating work and running coding agents stay in the
CLI/IDE.

Enhanced Local assurance records stay inside the same SQLite trust boundary.
The CLI captures an opted-in JUnit report and watched inputs, parses a bounded
subset into normalized observations, and binds them to the existing work and
review. It never treats a report as runner attestation. Checkpoint manifests
and acceptance bases are stored locally and projected compactly; the basis is
re-read transactionally before a human decision. The first enhanced write makes
a disclosed backup and installs an old-writer guard, so older CLIs cannot
silently mutate upgraded state. The bounded handoff format refuses work
carrying enhanced records it cannot preserve.

Use transaction-scoped readers for impact and acceptance projections so the
review and its evidence come from the same SQLite snapshot. Read historical acceptance records through the same inspection
as the displayed review, rather than independently resolving the latest review
again. Default checkpoint lookup is checkout-scoped; explicit checkpoint IDs
use work/workspace-scoped reads. Do not reintroduce an unqualified latest
checkpoint reader. Resume compares captured checkout content and Git trees,
not just changed-path membership. Keep v1 serialization and old-writer guards
while older records and released CLIs remain supported; those are compatibility
and data-safety boundaries, not disposable development scaffolding.

The Local store owns checkout generation. A private staged receive commits a
new random generation and its write-compatibility guard in one transaction.
The existing enhanced format remains readable and writable by supporting
CLIs. A received store uses enhanced format v2, which older enhanced binaries
reject before mutating the database. Its guarded tables also require the
generation-aware writer capability. Historical rows and their original
identities are carried forward. New checkout identities combine the canonical
checkout path with the current generation. Stores that have never been
received keep their path-only identities.

Local snapshot transfer reuses SQLite's consistent snapshot operation.
Only a bounded manifest and database travel. The CLI stages and validates
them before publication to a new name and validates the received SQLite
file read-only before changing compatibility in private staging. Transfer
trust assumes control of the local account and a trusted source archive.
Checksums establish consistency, and normalized Git provenance constrains
project binding. A receipt records original inventory and the actual
receiving generation. No account or shared trust service issues it.

Activation reaches only the selected Local config file. It checks the
effective store and workspace, re-reads config immediately before atomic
replacement, then verifies the saved binding. Cooperating local users
must stop other writers during a switch. A config re-read is not a
compare-and-swap against unrelated processes, and checkout generation never
locks a source store or detects writes it makes offline.
See [Trust and security](../using-specgate/concepts/trust-and-security.md).

## Workflow

```text
artifact-backed: publish -> readiness -> approve exact version -> create linked work
quick: enter explicit criteria -> create quick work

both routes: Context Pack -> implementation -> submit evidence
             -> delivery review -> human decision
```

Quick work has no lead artifact and requires explicit acceptance criteria.
Artifact-backed work pins one fully approved artifact version.

Public delivery surfaces are the CLI and its embedded Local review UI. Previous
remote configurations and deployments remain user data. The current CLI does
not connect to them. Use [operation and recovery guidance](../using-specgate/guides/operate-specgate.md#previous-installations).

## Related

- [Contracts](contracts.md)
- [Data model](data-model.md)
- [Operate SpecGate](../using-specgate/guides/operate-specgate.md)
- [Testing strategy](testing.md)
