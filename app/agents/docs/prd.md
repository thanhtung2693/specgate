# PRD - Agent Module

**Status:** current
**Document type:** product intent

## 1. Purpose

The agent module is SpecGate's headless governance-ops service. It runs
readiness gates, quick work creation, Context Pack
assembly helpers, governance chat, and delivery review. It does not own durable
state; Doc Registry does.

## 2. Goals

- Judge artifact readiness from policy snapshots, role-tagged documents, and
  team Skill rubrics.
- Create quick-route work only when enough acceptance criteria exist or can be
  drafted by the settings-backed governance model.
- Review completed delivery against canonical acceptance criteria, checks, and
  evidence.
- Keep governance chat narrow: read governed artifacts, compare versions, and
  explain readiness gates without drafting product docs.

## 3. Non-goals

- Durable artifact/workboard storage.
- Human review UI.
- Replacing CLI/IDE agent implementation workflows.
- Keyword/rule-based routing over user content.
- Generic project management or ticket tracking.

## 4. Users

| User | Need |
| --- | --- |
| Governance operator | Ask questions, run/read gates, understand blockers |
| Reviewer | See readiness and delivery verdicts with evidence |
| CLI/UI | Call deterministic HTTP operations |
| IDE coding agent | Consume Context Packs and delivery-review gaps |

## 5. Core Outcomes

- Gate outputs are traceable to artifact content, the frozen automatic policy,
  and its exact Skill rubric.
- Delivery verdicts are deterministic where checks bind to criteria, and
  evidence trust is explicit.
- Model errors fail conservatively and do not create durable work on thin evidence.
- LangGraph chat stays a thin support surface over governed data.
