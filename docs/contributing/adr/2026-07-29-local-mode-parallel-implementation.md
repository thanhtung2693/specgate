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

- **Document identity.** Full keys an artifact document on `(path, role)` and
  hashes both into the snapshot digest. Local enforced uniqueness on `path`
  alone, in validation and again as a SQLite primary key. A single specification
  covering two required roles was accepted by `artifact publish --preview` and
  then rejected by publish, leaving an author whose spec is one document unable
  to complete the artifact route at all.
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
the same scenario. Each looked correct in isolation. That is the signature of
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
to cover the rest of the shared vocabulary: work payload field names, document
identity rules, trust tiers, gate keys, and state names. Cheap, incremental, no
migration. It does not prevent divergence, it makes divergence fail a build.

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

Not yet made.

The author's stated priority is that Local mode works perfectly, because it is
the surface a solo developer evaluates first. That argues against **C** for now:
the risk sits precisely on the mode that must not break.

## Consequences

Until this is decided, treat any change to a shared governance concept as a
change to two implementations. Concretely: when editing document identity,
criterion binding, work payload fields, trust tiers, or assurance phrasing,
change Local and Full in the same commit, and add a derived check to the release
gate so the next divergence fails a build rather than a dogfood session.

Whichever option is chosen, `docs/contributing/contracts.md` remains the written
authority for the shared vocabulary, and a divergence between an implementation
and that document is a defect in the implementation.
