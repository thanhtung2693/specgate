# ADR: Completion-bound delivery acceptance

## Status

Accepted 2026-07-19.

This decision supersedes only the unqualified precedence statement in
[Delivery-verdict trust model](2026-07-07-delivery-trust-model.md) that a human
decision outranks every later platform run.

## Context

A human must be able to approve or reject an exact reviewed delivery without a
same-receipt model rerun silently changing that decision. Rework also has to
remain possible: after rejection, the implementing agent must be able to submit
a corrected completion and start a new review cycle.

Treating human authority as permanent across all future completions either
blocks rework forever or displays evidence from one completion beside the Git
receipt from another. Letting every later platform run win has the opposite
failure: a rerun can revoke a human decision and trigger terminal side effects
without another person acting.

## Decision

The durable `coding_agent.completed` feedback-event ID identifies one delivery
cycle.

- A platform `delivery_review` records the exact
  `completion_feedback_event_id` it reviewed.
- A human approve/reject run carries forward that completion ID, the reviewed
  gate-run ID, and the platform evidence verdict.
- Within the same completion cycle, the human decision is authoritative even
  if the platform review is rerun.
- A later completion with a different non-empty ID starts a new cycle. Its
  platform review becomes authoritative and awaits a new human decision.
- If the latest completion has no matching review, status reports
  `reason_code=delivery_review_outdated`; no human decision is accepted against
  the older review.
- The human decision compare-and-swaps the exact reviewed gate-run ID and
  latest completion-event ID while completion and review persistence share the
  change-request lock.
- Runs without a completion binding are never authoritative. Rerun delivery
  review to create an exact binding before deciding.
- Human acceptance closes the work item's completion stream. Further code
  changes start in a new work item rather than invalidating archive or tracker
  side effects.

Evidence assessment and human authority remain separate. A person may accept
an advisory or false-negative evidence verdict, but the evidence verdict stays
visible as `evidence_verdict` rather than being rewritten as a pass.

Archive and linked-tracker completion are terminal effects. They occur only
after a human approve decision, never after a platform evidence pass.

## Consequences

- Corrected work has a resumable reject → report → review → accept loop.
- Status, Context Packs, Git receipts, and human decisions describe the same
  completion cycle.
- Same-completion review reruns cannot silently undo a human decision.
- Automatic archive and tracker transitions cannot precede human acceptance.
- Older unbound records remain readable without guessing receipt identity.
