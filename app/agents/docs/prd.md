# PRD — Agent Module

**Version:** v0.2 | **Status:** Current

## 1. Purpose

The agent module is a **headless governance-ops service** SpecGate calls server-side. It governs existing artifacts — it does not create them. Creation happens in the developer's IDE (Cursor, Claude Code, Codex, OpenSpec, Spec Kit).

## 2. Goals

- Run readiness gates over published artifacts (per-profile, role-based)
- Run post-build delivery review (AC met/unmet/unclear)
- Classify proportional ceremony (route classifier)
- Compile Context Pack handoffs for IDE consumption
- Expose a thin domain-specific chat surface for governance questions

## 3. Non-goals

- Generating PRDs, specs, or implementation plans (IDE agents do this)
- Serving as the human review UI
- Replacing Doc Registry lifecycle enforcement
- Becoming a generic task tracker or drafting assistant

## 4. Users

- Planning operator (asks governance questions, reviews gate results)
- Tech lead approver (reviews proposals, approves transitions)
- IDE coding agent (consumes governance-ops outputs: context packs, gate verdicts)

## 5. Core outcomes

- Readiness gates run against the artifact's profile snapshot and report per-topic verdicts
- Delivery review judges built results against acceptance criteria
- Feedback events produce draft proposals without overwriting approved artifacts
- Context Packs assemble on-read for IDE handoff
- Governance-ops explains verdicts, compares versions, surfaces conflicts
