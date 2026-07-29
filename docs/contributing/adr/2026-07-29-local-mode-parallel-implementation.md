# ADR: Local mode as a parallel implementation

## Status

Proposed 2026-07-29. Not accepted; this records a decision that is due, not one
that has been made.

## Context

Local mode reimplements the governance model that Doc Registry owns. It is not a
client of the server and it is not a thin subset — it is a second implementation
of the same product concepts against SQLite, in a different language boundary
from the Full-mode services.

Production line counts, tests excluded:

| Concern | Local (`app/cli/internal/local`) | Full |
| --- | --- | --- |
| Store and schema | `store.go` 547 | `internal/storage/db` 5,651 |
| Gate tasks and readiness | `gate_tasks.go` 537, `readiness.go` 126 | `internal/governanceops` 4,268 |
| Delivery review | `delivery.go` 482 | `governanceops` + `quality_gates/delivery_review.py` |
| Work items | `work.go` 352 | `internal/workboard` 409 |
| Artifacts | `artifact.go` 228 | `internal/artifact` 1,061 |
| Policy | `policy.go` 187 | `internal/governanceprofile` 766 |
| Audit | `audit.go` 235 | `governanceops/audit.go` |
| Portable export | `portable.go` 234 | no counterpart |

Local is 2,928 production lines. It is deliberately smaller: no model, no
integrations, no Knowledge, one workspace at a time. That asymmetry is by
design and is not the problem.

The problem is that the same product concept is expressed twice with nothing
holding the two expressions together. Four divergences surfaced in a single
review session, all in `#34`, none caught by any existing test:

- **Document identity.** The two stores disagree about what a document *is*, and
  the disagreement is still open. Local now keys on `(artifact_id, path, role)`;
  Full keys `artifact_files` on `(artifact_id, path)` and reads content with
  `FileContent(ctx, id, path)`, so a stored document holds exactly one role.
  Before this was understood, Full accepted a manifest naming one path under two
  roles, uploaded both objects, kept whichever row won at the database, and
  reported success — a declared role vanished from an approved immutable snapshot
  with nothing reporting it. Full now refuses the duplicate instead. Local
  reached the opposite resolution: it admits one path under several roles,
  because a solo author whose specification is one document otherwise cannot
  satisfy a multi-role policy at all.

  This is the divergence that matters most, and it is not vocabulary. The two
  modes now have genuinely different capabilities, which also means a Local
  workspace using one source for two roles has no faithful representation in
  Full — `portable import` cannot carry it across.
- **Work payload vocabulary.** Full emits `lead_artifact_id`. Local emitted it
  from `work create` but not from `work show` or `work list`, so the field the
  preparation skill instructs an agent to verify was absent on the command the
  agent actually calls.
- **Trust labels.** Full mapped the `deterministic` trust tier to the phrase
  "locally reproduced" — a reproduction claim for a check nothing re-ran — while
  Local had already been corrected to name the observer.
- **Assurance phrasing.** `observed by the SpecGate CLI` and `reported by the
  coding agent, not re-run` are written out in Go for Local and again in Python
  for Full. Full carried the older `(deterministic)` wording for an unknown
  period.

Every one of these was invisible until both modes were driven end to end against
the same scenario. Each looked correct in isolation. The document-identity case
was worse: it survived a first investigation because publish *returned success*,
and only reading back what Full had stored revealed the loss. That is the signature of
parallel implementation without a shared contract, and it is a cost the project
pays in exactly the way that is hardest to notice: silently, in favour of
whichever mode was edited second.

Three mitigations already exist and should be credited before proposing more.
`docs/contributing/contracts.md` is the written authority for shared vocabulary.
The release gate derives several cross-mode checks from code rather than pinning
prose. And `#39` added a check that the assurance phrases agree across the Go and
Python implementations. These reduce recurrence; they do not remove the
duplication.

## Options

**A. Accept the duplication and invest in drift detection.** Keep both
implementations. Extend the derived checks in `docs/release-readiness.test.mjs`
to cover the rest of the shared vocabulary: work payload field names, trust
tiers, gate keys, and state names. Cheap, incremental, no migration. It does not
prevent divergence, it makes divergence fail a build.

Its blind spot is now demonstrated rather than theoretical. A derived check reads
code and docs; it cannot compare a SQLite primary key with a Postgres one. Every
divergence found so far except document identity was vocabulary, and A would have
caught those. Document identity was enforced in two schemas, produced silent data
loss, and A would not have noticed. Choosing A means accepting that
schema-enforced rules need a different mechanism — a cross-store conformance test
that exercises both persistence layers against the same scenario.

**B. Extract a shared contract package.** Move the vocabulary and the rules that
both modes must agree on — document identity, criterion binding, trust tiers,
state names, assurance phrasing — into one Go package that Local imports and Doc
Registry imports. The Python delivery review still has to agree by test, since it
cannot import Go. Removes a real class of divergence at the cost of a new
cross-module dependency, which `AGENTS.md` currently forbids between `app/cli`
and `app/doc-registry`; taking this option means amending that rule
deliberately.

**C. Make Local a client of an embedded server.** Delete the parallel
implementation and have Local run the Doc Registry service in-process against
SQLite. One implementation, no divergence by construction. It is also the largest
change by far: it puts Postgres-shaped persistence on SQLite, pulls the server's
dependency tree into a CLI that currently ships as one static binary with no
Docker requirement, and risks the property that makes Local worth having.

**D. Do nothing.** Legitimate while the product is pre-1.0 and one person holds
both implementations in their head. The cost lands later, on whoever does not.

## Decision

Not yet made. **A**, with a named addition, is the recommendation.

Every divergence found so far except one was vocabulary, and A catches vocabulary
at build time using machinery that already exists and is negative-controlled.
**B** buys compile-time enforcement of the same class at the price of amending
the `AGENTS.md` module-boundary rule, and it cannot cover `delivery_review.py` at
all — the Python side would still have to agree by test, which is A's mechanism.
**C** is correct in the abstract and wrong now: it puts the risk on the mode that
must not break and would cost the single static binary with no Docker
requirement.

The addition A needs is a **cross-store conformance test**: one scenario
exercised against both the SQLite and Postgres persistence layers, asserting they
accept and reject the same manifests and store the same rows. Document identity
proves the derived checks alone are insufficient, because that rule lives in two
schemas rather than in code or docs.

Document identity also has to be settled on its own terms, independently of which
option is chosen. Local admits one path under several roles and Full does not;
one of those is wrong. The product intent stated in `#33` — roles are routing
labels, not one-file-per-concern requirements — argues for Full adopting
`(path, role)`, which means a Postgres key change and a `FileContent` read API
that takes a role.

## Consequences

Until this is decided, treat any change to a shared governance concept as a
change to two implementations. Concretely: when editing document identity,
criterion binding, work payload fields, trust tiers, or assurance phrasing,
change Local and Full in the same commit, and add a derived check to the release
gate so the next divergence fails a build rather than a dogfood session.

Whichever option is chosen, `docs/contributing/contracts.md` remains the written
authority for the shared vocabulary, and a divergence between an implementation
and that document is a defect in the implementation.
