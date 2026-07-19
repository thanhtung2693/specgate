# ADR: Delivery-verdict trust model

## Status

Historical trust model accepted 2026-07-07. Its provider assumptions are
superseded by the later minimal team-integration boundary.

The later
[minimal team integration boundary](2026-07-20-minimal-team-integrations.md)
excludes provider CI ingestion and narrows `repository_observed` to a merged PR
or MR whose head matches the latest completion receipt. User-cited or externally
supplied CI output remains evidence; it is not a SpecGate assurance source.

Implemented capabilities:

- per-criterion deterministic check bindings;
- local evidence grounding with excerpt and digest;
- model- or agent-produced semantic review;
- exact-head merged PR/MR repository observation;
- bound peer-review evidence;
- explicit human delivery approval or rejection.

## Context

An implementation agent can claim that every acceptance criterion is complete,
but a claim alone does not prove the implementation. Model judges help interpret
subjective criteria, yet their verdicts vary with model quality, thinking level,
and report wording. Objective criteria should use objective checks where
possible, while humans retain explicit override authority during alpha.

SpecGate must make the provenance of each verdict visible without pretending
that every signal has equal strength.

## Decision

Each acceptance criterion records the strongest trust signal available:

| Trust signal | Meaning |
| --- | --- |
| `agent_reported` | The implementation agent says the criterion is complete. |
| `grounded` | A cited local path was opened and stored with excerpt/digest metadata. |
| `peer_reviewed` | A different agent reviewed the exact completion receipt and covered the canonical criteria. |
| `deterministic` | A human-authored binding resolved to an observed passing or failing check. |
| `repository_observed` | A marked, merged PR/MR observed the exact latest completion head. |
| `human_decision` | A human explicitly approved or rejected delivery. |

The semantic judge is one evidence source, not equivalent to deterministic
checks or a human decision. Overall delivery resolution remains deterministic:

- any failed check or unmet criterion fails;
- unclear or missing criteria require human review;
- otherwise all criteria must be met;
- low-confidence semantic results may require human review;
- the frozen evidence policy may impose a stronger corroboration bar.

Automatic policy has two evidence bars:

- `attested_ok`: a platform-model review of an evidence-backed report plus green
  checks may pass; model-less agent-attested review still needs a bound peer or
  human decision;
- `corroborated_required`: a pass also needs a marked merged PR/MR whose head
  matches the latest completion receipt, or canonical deterministic bindings
  for every criterion.

Enhanced governance uses the stronger bar. Human approval can resolve an
advisory or false-negative result, and human delivery decisions outrank later
model runs.

## Deterministic bindings

An acceptance criterion may carry `verification_binding`, naming one check in
the delivery report:

- matching check `pass` → criterion `met`, trust `deterministic`;
- matching check `fail` → criterion `unmet`, trust `deterministic`;
- missing or skipped check → criterion `unclear`;
- no binding → semantic or agent-claim path.

Bindings are human-authored or human-confirmed. An IDE/model suggestion must not
silently create an authoritative binding, because selecting a check that does
not exercise the criterion would create a confident but invalid pass.

## Evidence grounding and peer review

The CLI opens each cited local evidence path before report submission and stores
a short excerpt plus SHA-256 digest. This prevents fabricated paths and makes
the delivery record inspectable, but it does not prove that a check exercised
the criterion.

A `coding_agent.peer_reviewed` event is accepted only when it:

- comes from a different named agent than the latest completion reporter;
- binds the exact latest completion feedback event and Git receipt;
- covers every canonical acceptance criterion exactly once.

Peer review remains review evidence. It does not approve delivery by itself.

## Human decision

Human delivery approval/rejection writes a `delivery_review` gate run with
human executor/trust and explicit actor/note. Human decisions outrank later
platform or agent reviews so a rerun cannot silently undo the decision.

The later
[completion-bound delivery acceptance ADR](2026-07-19-completion-bound-delivery-acceptance.md)
supersedes that unqualified precedence rule: human authority is stable within
one exact completion cycle, while a corrected completion starts a new cycle.

Identity fields are asserted inside the trusted deployment. They provide audit
attribution and cooperative same-agent guards, not internet-grade
authentication.

## Positioning

SpecGate makes AI-agent delivery reviewable and evidence-backed. It provides a
human-reviewable record with acceptance-criteria evidence, deterministic
checks, provenance, and a traceable verdict. It does not replace code review or
claim that semantic judgments prove the implementation.

## Consequences

- Objective criteria can earn stronger trust without model cost.
- Subjective criteria remain reviewable instead of being falsely presented as
  proven.
- Enhanced governance can reject self-reported-only passes.
- Humans can resolve model uncertainty and override automated verdicts.
- Verification must test deterministic resolution and precedence directly;
  using an LLM to verify its own verdict logic would be circular.
