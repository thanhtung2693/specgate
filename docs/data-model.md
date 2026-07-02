# Data Model

This document describes the shared logical data model for the monorepo.

## Why it exists

- Keep the meaning of core entities stable across the CLI, Doc Registry, governance-ops, and UI
- Separate data shape from API shape and from implementation details
- Give governance and coding agents a single place to inspect canonical entities before generating or consuming specs

## Core entities

### Feature

- Stable user-facing product capability: a durable id plus a human-stable key
- Examples: `checkout-loyalty-points`, `pricing-v2`
- Used as the cross-module grouping key
- Points at the approved source of truth through `canonical_artifact_id`

### Change request (work item)

- The unit of governed work: title, description, acceptance criteria, work type, and links to its Feature, lead artifact, and Context Pack
- Board phase (Intake → Draft → Review → Ready → Handoff) is derived on read from artifact and pack state; there is no persisted work-item status enum
- New locally created work items may carry `created_by` and `workspace_id` for attribution and selection scoping

### Artifact

- A versioned document bundle: flexible documents (`{path, role}`) inside a fixed governance envelope (feature linkage, `version`, `status`, provenance, resolved gates-profile snapshot)
- Each artifact row is one immutable published revision, identified by `feature_id + version`, with revision lineage (`parent_artifact_id`, `lineage_root_id`)
- Status lifecycle has four states: `draft`, `needs_changes`, `approved`, `superseded`
- Human review decisions (approve, request changes) act on the exact version and are recorded with reviewer and timestamp
- Carries an overall `impact_level`: `low`, `medium`, or `high`

### Artifact edit session

- A draft-only working copy used to propose changes to an existing artifact (manual edits, review-requested revisions, gate proposals)
- Approving a session produces a new artifact version; discarding leaves the approved artifact untouched
- All artifact mutations flow through the proposal → review → version loop; approved artifacts are never mutated in place

### Gate run

- One store for every gate evaluation snapshot (`gate`, `state`, `hint`, `evidence_json`), scoped by subject kind to a work item or an artifact
- Work-item rows cover workboard readiness gates and post-build delivery review; artifact rows cover artifact readiness gates and IDE-agent gate-task results (a submitted result is a run, recorded with the submitting executor)
- Combine deterministic checks with optional LLM evaluations; results are point-in-time records, not live state
- Delivery review verdicts are recorded per acceptance criterion
- Gate task dispatch/queue state (frozen inputs, TTL) stays separate; a task is pending until its result run exists

### Context Pack

- The canonical implementation handoff brief for a change request
- Hydrated from the approved lead/canonical artifact: intent, acceptance criteria, scope and blast radius, risks, design references, domain vocabulary, and applicable skills
- Read by coding agents through `specgate work context` or the `specgate://context-pack/{change_request_id}` resource

### Delivery evidence (feedback events)

- Append-only events linked to a work item: completion reports with one claim and evidence per acceptance criterion, ambiguity blocks, doc-update signals, and inbound tracker/git status changes

### User

- A local person record used for attribution and task ownership
- Has an internal stable ID, normalized username, display name, and optional email
- Usernames are deployment-unique, lowercased, 3-40 characters, and may contain letters, numbers, `_`, or `-`
- Authentication and role-based access control are outside the current data model; provider login can link to the internal ID later

### Workspace

- A local collaboration boundary selected by the CLI
- Has an internal stable ID, slug, name, and membership rows connecting users
- CLI work-item list surfaces use the selected workspace as a filter by default
- Current behavior is attribution and selection only; it does not enforce tenant isolation

### Integration

- A connected external provider (GitLab, GitHub, Linear) with credentials, selected resources (projects/repositories), and — for GitLab and GitHub — a per-integration webhook secret
- Backs inbound delivery and tracker signals plus `tracker_links` rows; trackers are optional and never gate the governed loop

### Skill

- A reusable rubric/prompt record in the Doc Registry skill registry
- Surfaced to IDEs through the `specgate://skills` resource and referenced by governance profiles; user-editable, with optional seeded starters

### Settings

- Deployment-level key/value configuration (model provider keys, MCP configuration, governance toggles) stored server-side with sensitive values encrypted at rest

### Event

- Immutable record of state changes and workflow milestones (for example `artifact.published`, `feature.canonical_changed`)
- Used for UI notifications, audit history, and downstream automation

## Suggested relationships

```text
Feature
  ├─ Artifact (immutable versions with lineage)
  │    ├─ Edit sessions → new versions
  │    ├─ Readiness runs
  │    └─ Events
  └─ Change request
       ├─ Context Pack
       ├─ Gate runs and delivery review verdicts
       ├─ Delivery evidence (feedback events)
       └─ Tracker links (via integrations)

Workspace
  └─ User membership
```

## Design principles

- Prefer immutable published versions over in-place mutation
- Keep `feature_id` stable across modules
- Keep review, gate, and delivery state explicit instead of implicit
- Store references separately from generated content when possible
- Treat events and delivery evidence as append-only records

## What belongs elsewhere

- API endpoint shapes belong in [`contracts.md`](contracts.md)
- Product intent and implementation behavior belong in module `prd.md` and `spec.md`
