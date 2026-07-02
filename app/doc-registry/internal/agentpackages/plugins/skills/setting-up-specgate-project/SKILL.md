---
name: setting-up-specgate-project
description: Setup. Use when onboarding SpecGate to a repository, installing or refreshing IDE plugins for a project, clarifying canonical docs/tracker mirrors/readiness rules, or preparing an agent to use SpecGate in an unfamiliar repo.
---

# Setting Up SpecGate Project

## Invocation

Invocation mode: explicit setup entry point. Use it when a user asks to install,
refresh, or map SpecGate for a repository, or when the router finds that the
project's docs, tracker mirrors, readiness rules, or vocabulary are unknown.

1. Map the project context.

Read only enough repository context to identify the governing sources: root and
module `AGENTS.md`, docs entry points, module specs, README files, ADRs,
tracker/integration docs, and existing SpecGate plugin/install docs. Prefer
existing repo conventions over inventing a new setup structure.

Completion criterion: canonical docs, module boundaries, tracker/work mirrors,
and likely ownership areas are listed with file paths or marked missing.

2. Identify the readiness contract.

Find what must be true before an agent can pick up work: quality gates, human
approval, acceptance criteria, Context Pack freshness, doc-update rules, and
module-specific verification commands. Separate hard stops from helpful habits.

Completion criterion: every readiness rule is classified as `hard_stop`,
`required_practice`, or `local_convention`, with source evidence.

3. Capture domain language.

List domain terms, glossary files, status names, policy names, and workflow
phrases the agent should reuse. Challenge fuzzy terms by naming the ambiguity
rather than normalizing it silently.

Completion criterion: the setup map contains the vocabulary needed to read or
write Context Packs without changing product meaning.

4. Produce a project setup map.

Return a concise summary in whatever shape fits the findings, covering:
canonical docs, tracker/work mirrors, readiness rules, verification commands,
domain vocabulary, Context Pack inputs, and open gaps or questions.

Do not change SpecGate server storage, SpecGate UI, SpecGate schemas, or Context
Pack schema under this skill. If the target repository needs persistent setup
documentation, propose the narrowest follow-up work item or repo-doc update.

Completion criterion: the map is actionable for a fresh coding agent and every
gap has a named owner or follow-up question.
