"""Spec-completeness judge — the artifact-spine "build-readiness" check.

Scores an artifact's existing sections against the build-critical topics, marking
each ``covered | partial | missing | not_applicable`` so a reviewer sees what is
missing before handoff. The per-topic verdicts come from the model (structured
output, no keyword rules — per agents AGENTS.md); the overall gate state is
enforced deterministically (advisory: any missing/partial required topic ⇒
``warn``, never blocks) with the shared low-confidence downgrade. Mirrors
``quality_gates/delivery_review.py`` and plugs into ``evaluate_all_gates`` as the
``spec_completeness`` gate.
"""

from __future__ import annotations

import json
from typing import Any, Literal

from langchain_core.messages import HumanMessage
from pydantic import BaseModel, Field

from specgate_agents.governance.llm_structured import structured_output_ainvoke
from specgate_agents.governance.prompt_budget import MAX_ARTIFACT_CHARS, cap_model_text
from specgate_agents.governance.quality_gates.judge import (
    JUDGE_MODEL_DEFAULT,
    SPEC_COMPLETENESS_GATE,
    GateEvaluation,
    GateState,
    append_rubric,
    resolve_gate_state,
)

EVAL_SUITE_VERSION = "spec-completeness-v1"

TopicStatus = Literal["covered", "partial", "missing", "not_applicable"]

# Readiness topics. Keys are structural (carried through to the UI checklist +
# contracts vocabulary); descriptions describe the content, located across whatever
# documents exist (the gate reads documents by role, not by filename).
#
# The first seven are the **minimum-executable-contract** — the required-for-handoff
# safeguard. The rest are build-readiness depth.
COMPLETENESS_TOPICS: tuple[tuple[str, str], ...] = (
    ("outcomes", "Goal / outcomes — what is being built and why"),
    ("scope", "Scope — what is in scope"),
    ("non_goals", "Non-goals — explicitly excluded scope"),
    ("acceptance_criteria", "Acceptance criteria — observable, testable pass conditions"),
    ("constraints", "Constraints — technical / timeline / dependency limits"),
    ("rollout_rollback", "Risks, rollout & rollback"),
    ("verification", "Verification — how delivery is tested / confirmed"),
    ("users_roles", "Users & roles"),
    ("workflows", "Workflows"),
    ("screens_states", "Screens & states"),
    ("data_model", "Data model"),
    ("permissions_security", "Permissions & security"),
    ("integrations", "Integrations"),
    ("edge_cases", "Edge cases"),
    ("metrics", "Metrics / success measures"),
    ("observability", "Observability"),
    ("phased_tasks", "Phased task breakdown"),
)


class TopicCoverage(BaseModel):
    topic: str = ""
    status: TopicStatus
    why: str = ""
    where: str = ""


class CompletenessJudgment(BaseModel):
    """Raw structured output from the completeness model."""

    topics: list[TopicCoverage]
    summary: str = ""
    confidence: float = Field(ge=0.0, le=1.0)


COMPLETENESS_PROMPT = """\
You are checking whether a work item is **ready to hand to a coding agent**. The
documents below may follow any structure (any spec format) — your job is to LOCATE
each readiness topic wherever it lives across them, regardless of which document or
heading holds it. Topics tagged **[REQUIRED]** are required for this change type's
automatic governance policy and must not be left unaddressed. If no topic is tagged, treat
the **minimum executable contract** (the first seven: goal, scope, non-goals,
acceptance criteria, constraints, risks, verification) as required; the rest are
build-readiness depth.

For EACH topic below, return a status:
- "covered" — the documents adequately address this topic.
- "partial" — touched but too thin to build from confidently.
- "missing" — not addressed anywhere, and the change type needs it.
- "not_applicable" — this change type genuinely does not need this topic
  (e.g. a docs-only change needs no data model or rollback; a research spike needs
  no phased tasks).

Be generous with "covered" when a topic is adequately addressed — a checklist that
is always half-missing trains reviewers to ignore it. Only flag "missing"/"partial"
for a topic the change type actually needs. Quote the deciding text in `why`, and
name where it lives (the document/heading) in `where`. The decision is yours from
the content — never infer from keywords alone.

Work type: {work_type}

Readiness topics (key — description):
---
{topics}
---

The documents:
---
{artifact}
---

Return:
- topics: one entry per readiness topic above (carry its key), each with status + why + where.
- summary: one sentence for the reviewer (what's complete / what's missing).
- confidence: 0..1, how sure you are overall.
"""


def _resolve_completeness_state(
    topics: list[TopicCoverage],
    confidence: float,
    *,
    required_topics: set[str] | None = None,
) -> GateState:
    """Advisory roll-up: pass only when every *required* topic is covered; any
    required topic that is missing or partial ⇒ warn (never fail). Then apply the
    shared low-confidence downgrade. ``not_applicable`` topics are ignored."""
    if required_topics:
        allowed = set(required_topics)
        required = [t for t in topics if t.topic in allowed and t.status != "not_applicable"]
    else:
        required = [t for t in topics if t.status != "not_applicable"]
    if not required:
        # All topics marked not_applicable is suspicious — surface to reviewer.
        return resolve_gate_state("warn", confidence)
    has_gap = any(t.status in ("missing", "partial") for t in required)
    base: GateState = "warn" if has_gap else "pass"
    return resolve_gate_state(base, confidence)


def _format_topics(required: set[str] | None = None) -> str:
    lines = []
    for key, label in COMPLETENESS_TOPICS:
        tag = " [REQUIRED]" if required and key in required else ""
        lines.append(f"- {key} — {label}{tag}")
    return "\n".join(lines)


async def evaluate_spec_completeness(
    artifact_md: str,
    *,
    model: Any,
    judge_model: str = JUDGE_MODEL_DEFAULT,
    work_type: str = "",
    required_topics: list[str] | None = None,
    config: dict[str, Any] | None = None,
    rubric: str | None = None,
) -> GateEvaluation:
    """Judge the ``spec_completeness`` gate over the full artifact bundle.

    Returns a ``GateEvaluation`` whose ``evidence`` carries the per-topic detail as
    JSON (``{"topics": [...], "summary": ...}``) so the UI can render the checklist.
    ``rubric`` (a bound Skill prompt) is appended as team policy when present.
    """
    required_set = {t for t in (required_topics or []) if t}
    prompt = COMPLETENESS_PROMPT.format(
        work_type=work_type.strip() or "unspecified",
        topics=_format_topics(required_set),
        artifact=cap_model_text(artifact_md.strip(), MAX_ARTIFACT_CHARS),
    )
    prompt = append_rubric(prompt, rubric)
    # judge_model is the recorded label only; inference uses the `model` arg.
    judgment = await structured_output_ainvoke(
        model, CompletenessJudgment, [HumanMessage(content=prompt)], config
    )
    state = _resolve_completeness_state(
        judgment.topics,
        judgment.confidence,
        required_topics=required_set,
    )
    return GateEvaluation(
        gate=SPEC_COMPLETENESS_GATE,
        state=state,
        hint=judgment.summary,
        confidence=judgment.confidence,
        evidence=json.dumps(
            {"topics": [t.model_dump() for t in judgment.topics], "summary": judgment.summary},
            ensure_ascii=False,
        ),
        judge_model=judge_model,
        eval_suite_version=EVAL_SUITE_VERSION,
    )
