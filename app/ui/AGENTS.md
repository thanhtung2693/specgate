# ui — Agent Rules

Extends root [../../AGENTS.md](../../AGENTS.md). Read that first; this file only adds module-specific conventions.

## Stack

- Vite 8 + React 19 + TypeScript 6.
- Tailwind CSS 4 via `@tailwindcss/vite`.
- shadcn/ui 4 with Radix primitives and `lucide-react`.
- Routing: `react-router-dom` 7.
- Governance agent surface: `@assistant-ui/react` primitives with `@assistant-ui/react-langgraph` when `VITE_LANGGRAPH_API_URL` is configured, falling back to the local deterministic adapter for layout work.
- Node >= 24.0.0.

## Structure

- App shell and navigation live in `src/components/layout/`.
- Assistant surfaces live in `src/components/agent/`.
- Runtime work, review, and artifact surfaces must use Doc Registry data or
  explicit empty/error placeholders. Do not add bundled sample rows to those
  surfaces; sample/demo data belongs only in tests or deliberate fixtures.
- Providers live in `src/providers/`.
- shadcn-generated files live in `src/components/ui/` and should be changed by the shadcn CLI, not hand-edited.

## Page workflow

For page-completion work, follow the user-approved loop before editing:

1. Analyze the current page, existing data flow, and intended user workflow.
2. Design the smallest usable interaction model with concrete, working CTAs.
   Use `$design-taste-frontend`, `$minimalist-ui`, and
   `$high-end-visual-design` as restraint filters, not as permission to add
   decoration.
3. Discuss the proposal with a subagent from a user perspective.
4. Review the plan again for simplicity, missing states, unnecessary steps, and
   SpecGate action boundaries.
5. Implement, update docs, and verify.

Complete pages one at a time unless the user changes scope. Current order:
Artifacts, Reviews, then Work.

Before adding any component, control, card, modal, or action, confirm it has a
necessary job: a user task, real data, direct backend/CLI workflow transition,
or allowed advisory governance-agent prompt. For every action, name the owner,
outcome, durability, and SpecGate boundary before implementing it. Do not add UI
for speculative future behavior.

Work-item creation is not part of the UI workspace for now. The main authoring
workspace is IDE + CLI; developers and IDE agents create or pick up work through
those flows. Do not re-add UI creation unless the product workflow and durable
backend owner are explicit.

Artifact attachment management is not part of the artifact inspection modal.
Attachments are explicit, governed feature references, not automatic captures
from IDE-agent chat. A random image, document, or log pasted into an IDE agent
stays local/session context unless a user or agent deliberately pins it to
SpecGate with an audience (`gate`, `coding_agent`, or `both`). If the material
changes intent, scope, acceptance criteria, or design contract, route it through
the reviewed artifact-proposal flow instead of storing it as a loose attachment.

## Action boundaries

Keep UI actions clear about who owns the outcome:

- Governance-agent actions are advisory only: ask about gate state, blockers,
  handoff readiness, review gaps, artifact context, and delivery next steps.
  Buttons that send a prompt to the governance agent use the robot icon.
- Durable workflow transitions must be direct UI/backend/CLI actions, not chat
  prompts. This includes creating work, approving/rejecting artifacts, running
  gates, creating tracker handoffs, persisting gate snapshots, and copying or
  downloading Context Pack handoff material.
- Context Packs are the IDE/coding-agent handoff contract. Do not label an
  agent-chat action as "generate Context Pack" unless it calls a backend
  endpoint that creates the pack. Without that endpoint, use advisory wording
  such as "Ask what blocks handoff."
- IDE coding agents own repository implementation after handoff: read the
  Context Pack, edit files, run tests, update docs, and report evidence through
  CLI/MCP flows.
- Attachments are supplemental references only. UI surfaces may show pinned
  attachments and explain their audience, but upload/delete/audience mutation
  belongs in a deliberate feature-evidence management flow, not in an artifact
  preview modal or governance-chat prompt.

## Verification

- `npm run lint`
- `npm run test`
- `npm run build`
- For visual changes, run `npm run dev` and verify desktop plus one narrow viewport.
