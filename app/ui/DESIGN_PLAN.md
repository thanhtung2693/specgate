# SpecGate UI Design Plan

## Goal

Make the UI a secondary catch-up and review surface beside the CLI. The UI should help users quickly understand governed work state, review evidence, and approve or inspect transitions without turning every backend object into a primary destination.

The shared center of gravity is the work item. Operators review many work items through queues and saved perspectives. Developers usually enter from the CLI or a direct link and need one complete work-item cockpit.

## Product Posture

The CLI remains the primary execution interface. The UI is optimized for two entry rhythms:

- operators/reviewers catching up across many work items;
- developers catching up on one work item before returning to the CLI;
- seeing what needs human attention;
- reviewing artifacts, gates, and delivery evidence visually;
- opening focused detail when a work item needs inspection;
- asking the governance agent contextual questions.

The UI should not become a command catalog. It should reduce ambiguity and context switching for humans who need to understand the SDLC state.

## Navigation Model

Recommended primary sidebar:

| Nav item | Route | Reason |
| --- | --- | --- |
| Work | `/work` | Main catch-up view and operational queue for work items |
| Reviews | `/reviews` | Operator queue for delivery reviews, readiness/gate failures, and approvals needing attention |
| Artifacts | `/artifacts` | Durable document/version browser for PRDs, specs, tasks, and artifact state |
| Settings | `/settings` | Workspace, user, integrations, models, and agent setup |

Avoid a hard role switch such as "Operator mode" and "Developer mode." Use saved perspectives instead:

- My attention;
- Ready for pickup;
- In implementation;
- Needs review;
- Blocked;
- Recently changed;
- Opened from CLI;
- Current item.

If a developer opens a work item from the CLI, the UI should land directly on the work item detail, not force them through the Work page.

Recommended secondary or contextual surfaces:

| Surface | Placement | Reason |
| --- | --- | --- |
| Context Packs | Work item detail, delivery detail, artifact links, command action | Context Packs are handoff objects, not a daily destination; they still need one-click discoverability from the work they explain |
| Gates | Work item detail, artifact detail, review detail, Work attention filters, Reviews queue | Gates are usually evaluated in context; gate failures still belong in catch-up triage |
| Skills | Settings section, agent setup health, contextual provenance badges | Skills are platform capability/configuration unless skill management becomes a frequent workflow |

## Page And Modal Rules

Use pages when the user may compare, deep-link, refresh, or return later.

Use drawers/detail panels when the user starts from a list and needs inspection without losing list context.

Use modals only for short-lived tools or bounded decisions.

| Surface | Recommended container |
| --- | --- |
| Work | Page |
| Reviews | Page |
| Artifacts | Page |
| Settings | Page |
| Work item detail | Full route-backed page |
| Artifact detail | Route-backed detail page or split detail panel |
| Context Pack detail | Route-backed detail page; drawer preview from queues |
| Gate run detail | Route-backed drawer/detail panel |
| Delivery review detail | Route-backed drawer/detail panel |
| Governance agent | Global modal for quick questions; contextual side panel on review/detail surfaces |
| Approve / publish / promote / run gate | Confirmation modal after context is reviewed elsewhere |
| Small create/edit forms | Modal when submit/cancel is enough; page/drawer when multi-step |

## Work Design

The Work page should answer: "What changed, what is blocked, and what needs me?"

Recommended layout:

1. **Saved perspectives**
   - My attention;
   - Ready for pickup;
   - In implementation;
   - Needs review;
   - Blocked;
   - Recently changed;
   - Opened from CLI when a CLI deep link provides context.

2. **Table-first queue**
   - item;
   - route;
   - lifecycle status;
   - owner or agent;
   - latest gate;
   - delivery verdict;
   - updated time;
   - blocker state.

   Cards can exist as an optional board view, but the default should favor comparison and scanning.

3. **Attention queue**
   - blocked items;
   - failed delivery reviews;
   - pending approvals;
   - gate failures;
   - recently completed agent handoffs needing human review.

   The attention queue should support focused filters:

   - all attention;
   - blocked;
   - gate failures;
   - delivery failures;
   - pending approvals;
   - agent handoffs.

4. **Active work lanes**
   - intake;
   - shaping;
   - handoff;
   - review.

5. **Right inspector panel**
   - selected work item summary;
   - lifecycle timeline;
   - current blocker or next action;
   - latest artifact, Context Pack, gate, and delivery-review state;
   - links to full detail.

6. **Recent activity**
   - agent feedback;
   - artifact status changes;
   - gate runs;
   - delivery verdicts.

7. **Quick actions**
   - create quick work;
   - generate context pack;
   - run readiness gates;
   - open delivery review.

## Work Item Detail Design

The work item detail is the developer cockpit and the shared audit context for operators. It should be a route-backed page, not a modal, because developers may open it from CLI, refresh it, cite it, or come back later.

Recommended layout:

- header with title, route, lifecycle status, owner, agent, and last updated;
- primary next action such as Run gates, Approve, Generate Context Pack, or Review delivery;
- main column with overview, acceptance criteria, lifecycle timeline, comments, and implementation feedback;
- sticky side panel with current gate state, Context Pack status, delivery evidence state, and CLI resume affordance.

Tabs:

- Summary;
- Context;
- Context Pack;
- Artifacts;
- Gates;
- Delivery;
- Activity.

The detail view must include a persistent Handoff / Context Pack entry so developers can answer "what exactly was handed to the agent?" in one click.

## Reviews Design

Reviews deserves a page because operators need a cross-item queue for human attention, approvals, failed gates, and delivery review gaps. Delivery status remains visible inside each work item, but the global destination should be named around the user's job: review.

Recommended layout:

- delivery review queue grouped by status;
- readiness and gate failure queues;
- pending approvals;
- evidence summary per change request;
- automated checks and manual evidence;
- acceptance criteria satisfaction;
- outstanding review gaps;
- verdict history;
- link back to work item, artifact, and Context Pack.

Review detail should open in a route-backed drawer or detail page so reviewers can compare state and return to the queue.

Reviews owns human review queues, implementation claims, evidence, verdicts, and review gaps. Work owns lifecycle triage across all active work. Keeping that boundary clear prevents the two pages from competing.

## Artifacts Design

Artifacts should be browsable, but not the default way users discover work.

The label can stay "Artifacts" while the audience is CLI/spec fluent. If reviewers struggle with the term, rename the visible nav label to "Artifacts & Specs" while keeping route and internal naming stable.

Recommended layout:

- filters for artifact type, status, route, owner, and related work item;
- artifact list with current version and readiness state;
- detail view with document tabs, version history, gate state, and linked work items;
- quick jump from artifact to related delivery or Context Pack.

Artifacts should prioritize readable document state and version confidence over raw metadata density.

## Gates Design

Keep Gates contextual at first.

Recommended placements:

- Work cards show concise gate state;
- work item detail has a Gates tab;
- artifact detail shows readiness/quality gates;
- review detail shows delivery-review gate outcome and outstanding gaps.

Promote Gates to primary nav only if users need a cross-work-item gate operations queue.

Gate failures should still be visible as first-class attention items on Work.

## Skills Design

Do not keep Skills as primary nav unless skill management becomes a frequent operator workflow.

Recommended placement:

- Settings > Agent Skills for installed skills, plugin source, version, and refresh actions;
- Work or Settings card for agent setup health;
- Work/Reviews contextual badges for skills used by the agent;
- governance agent composer mentions for skill references.

This keeps the UI focused on governed work while preserving visibility into the agent system.

## Governance Agent Design

The governance agent should remain available but not dominate the app.

Recommended behavior:

- open as assistant-ui modal from the header for quick global questions;
- appear as a non-blocking side panel on route-backed review/detail surfaces where the user needs to compare chat with evidence;
- support `@` mentions for work items, artifacts, skills, and Context Packs;
- support `/` prompts for gates, handoff, delivery evidence, and route checks;
- preserve enough context from the current route or selected work item to answer without requiring copy/paste.

The modal is appropriate for quick access from the shell. A side panel is better when the user is actively reviewing artifacts, gates, or delivery evidence.

## Mobile And Tablet

Mobile should prioritize catch-up over dense editing.

Recommended behavior:

- sidebar becomes sheet;
- Work attention queue appears before lanes;
- detail opens as full-height drawer/sheet;
- tables become stacked rows with stable labels;
- modal agent remains available from header;
- primary actions stay icon-first with accessible labels.

## Implementation Order

1. Update navigation to Work, Reviews, Artifacts, Settings.
2. Move Context Packs and Gates into work item/detail surfaces.
3. Move Skills into Settings and contextual badges.
4. Redesign Work around saved perspectives, table-first queue, attention filters, inspector panel, and recent activity.
5. Add route-backed detail structure for work items, Context Packs, gate runs, and delivery reviews.
6. Build Reviews queue and review detail.
7. Expand Artifacts page around filters and readable version state.
8. Add contextual governance-agent mentions for Context Packs and current route state.
9. Add contextual governance-agent side panel to review/detail surfaces.

## Verification

The design is successful when:

- a returning user can identify the next human-needed action in under 30 seconds;
- a developer can open a work item and find Context Pack, artifacts, gates, delivery, and activity without using primary nav;
- a reviewer can inspect delivery evidence and verdict history without losing their queue;
- a CLI deep link can open directly into a complete work-item cockpit;
- primary nav contains only high-frequency surfaces;
- CLI users can still treat the UI as optional context, not required ceremony.
