# UI Contributor Rules

Extends the root [contributor rules](../../AGENTS.md). This file applies only to
changes under `app/ui/`.

## Stack and structure

- Vite 8, React 19, TypeScript, Tailwind CSS 4, shadcn/Radix, React Router 7,
  assistant-ui, and Node.js 26+.
- Layout and navigation live in `src/components/layout/`.
- Governance-chat surfaces live in `src/components/agent/`.
- Providers live in `src/providers/`.
- shadcn-generated primitives live in `src/components/ui/`; prefer the shadcn
  CLI for generated component changes and keep local customization explicit.

## Data and action boundaries

- Runtime work, review, artifact, and settings surfaces use real backend data or
  explicit loading, empty, and error states. Demo rows belong only in tests or
  deliberate fixtures.
- Governance-chat actions are advisory: explain state, blockers, readiness,
  artifact context, and delivery next steps.
- Durable transitions call their owning backend/CLI workflow directly. Do not
  disguise artifact approval, gate execution, delivery decisions, tracker
  handoff, or Context Pack creation as chat prompts.
- Repository implementation and source-spec authoring remain IDE/CLI concerns.
  Do not add UI work-item/spec creation without an explicit product-contract
  change and durable backend owner.
- Context Packs are exact-version IDE/coding-agent handoffs. UI wording must not
  claim a pack was generated unless the corresponding backend operation ran.
- Attachments are governed supplemental references. Do not turn artifact
  previews or chat paste into implicit upload/delete/audience-mutation flows.
  Intent, scope, acceptance-criteria, or design-contract changes require a new
  immutable artifact version through the owning workflow.

Before adding a control, identify its real data source, owner, durable outcome,
and failure state. Do not add speculative controls or decorative interaction
that implies unsupported behavior.

## Governance-chat regression discipline

- The deterministic local adapter is for isolated layout/component work, not
  evidence that real LangGraph streaming works.
- For delayed, blank, duplicated, or all-at-once responses, inspect the browser
  assistant-ui state and the matching LangSmith trace for the same `thread_id`.
- Probe the live `/runs/stream` modes before deciding whether the defect belongs
  to UI normalization or the agents backend.
- Do not add scenario-specific synthetic responses for failures that depend on
  real backend events. Follow
  [`docs/contributing/testing.md`](../../docs/contributing/testing.md#langgraph-streaming-probe).

## Tests and documentation

```bash
npm run test -- --run
npm run lint
npm run build
```

- Add targeted Vitest coverage for behavior changes.
- For layout, routing, streaming, onboarding, settings, or readback changes,
  verify against a rendered app at desktop and one narrow viewport.
- Compare stored API/LangGraph state with rendered state when ownership is
  unclear.
- Update `docs/spec.md` for UI contracts and `README.md` for contributor
  commands. Update `docs/using-specgate/` when the product workflow or public
  wording changes.
