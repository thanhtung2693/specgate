# ADR: Server-as-governance-truth (spec source of truth)

## Status

Accepted 2026-07-08. Decided after testing the feature-backed governance flow
with two independent advisor reviews grounded in this repo; both converged on
the same option.

## Context

SpecGate stores approved specs as **server-side artifacts** (Doc Registry); the
Context Pack inlines the approved spec and instructs agents that it "outranks
stale repo docs." Repo docs (module `docs/spec.md`, `README.md`, `AGENTS.md`)
are a separate, convention-maintained layer that can drift from the approved
artifact — the `spec_repo_drift` readiness gate exists to detect exactly that.

Nearly all other spec-driven-development tools (OpenSpec, spec-kit, superpowers)
anchor specs **in-repo** at tool-owned paths, so spec and implementation move in
one commit/PR and drift is prevented *by construction* — but they have no
server, no centralized approval workflow, no audit trail, and no cross-repo or
multi-agent story. Internal testing surfaced the tension: SpecGate was paying for a
drift problem the competition designed away, and the choice of truth-home was an
accident of implementation rather than a recorded decision.
This repo's own `AGENTS.md` also tells coding agents to implement per repo
`docs/spec.md`, creating a "two truths" reading unless the hierarchy is stated.

Options considered:

- **(a) Server-as-truth** — commit to the current architecture; the drift gate
  is the reconciler.
- **(b) Hybrid repo mirror** — sync approved specs into the repo (e.g.
  `.specgate/specs/`) for offline/git-native access and deterministic
  mirror-vs-server diffing; adds a sync surface (writer, timing, merge
  conflicts, gitignore interplay). Both advisor reviews independently judged
  this "a second product surface masquerading as a scoping fix."
- **(c) Repo-as-truth** — specs live in-repo; the server ingests/indexes them
  and approvals reference a commit SHA + path; drift impossible by
  construction; a large rearchitecture that forfeits the centralized-approval
  differentiators.

**Decision criterion: what record wins in a dispute?** For team-scale,
multi-agent, cross-repo governance — SpecGate's differentiated job — the
winning record must be the server artifact plus its approval/evidence trail.
Targeting teams makes the answer *more* server-centric, not less. (If the
product were lightweight single-repo SDD, Git should win and option (c) would
be correct — that is the revisit trigger for the chosen architecture.)

## Decision

1. For governed work, the **approved Doc Registry artifact and the generated
   Context Pack are the authoritative implementation contract**; repo docs are
   supporting developer documentation and must be reconciled when they drift.
   "Server-as-governance-truth" — the server owns the *contract*, not every
   spec-writing workflow (drafting can happen anywhere, including in-repo
   brainstorm docs, until publish).
2. We choose this because SpecGate's differentiated job is centralized
   approval, immutable policy snapshots, audit, and cross-repo/multi-agent
   handoff, which require durable state outside any single repository.
3. The accepted cost is repo-doc drift; **`spec_repo_drift` is a warn-only
   reconciliation gate, not a prevention mechanism or a competitive moat**.
4. We will **not** build repo-as-truth or a writable repo mirror until customer
   evidence shows git-native/offline spec review is purchase-blocking. If a
   repo export is ever added, it is a **read-only generated export** stamped
   with artifact id/version/digest — never a competing writable spec path.
5. **Revisit trigger:** if design partners value spec/code co-commit more than
   centralized approval/audit, revisit this ADR and pivot toward repo-ingest
   governance (option c).

## Consequences

- The Context Pack hierarchy line ("approved spec outranks repo docs") is now a
  recorded architectural decision, not folklore; `AGENTS.md`-style repo-doc
  discipline remains required *documentation* practice, reconciled via the
  drift gate.
- Drift findings stay `warn` and ride the existing readiness aggregate +
  Context Pack surfacing; no blocking semantics are added on top of them.
- The committed `.specgate/` repo config (server URL, workspace binding) is the
  repo's *pointer to* the governance server — the team-system direction
  (workspace sharing and peer-reviewed delivery evidence) builds on that
  pointer without moving the truth-home.
- Positioning follows the same line: the defensible value is audited,
  evidence-gated agent handoff for teams — approval that is *attributed and
  auditable* within a trusted deployment, not identity-secure authentication.
