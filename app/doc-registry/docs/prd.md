# PRD — Doc Registry

**Version:** v1.0 | **Status:** Current

---

## 1. Overview

Doc Registry is the central store for all planning artifacts in the AI-assisted development system. It acts as the **single source of truth** so downstream agents know what to build, against which version, and whether they are allowed to consume it.

---

## 2. Problem Statement

Without a shared store, the multi-agent system runs into:

- FE and BE agents reading different versions of the same spec
- No way to constrain agents to consume only approved artifacts
- No mechanism to notify downstream when a spec changes
- No detection when two features conflict on the same service
- No audit trail for when an artifact was created, who approved it, who consumed it

---

## 3. Goals

### 3.1 Primary

- Guarantee every agent reads the correct approved artifact version
- Provide a single source of truth for governance-ops output
- Detect conflicts between planning artifacts running in parallel

### 3.2 Secondary

- Provide a complete audit trail for every artifact transition
- Allow human approvers to review and approve specs before agents consume them
- Emit events so downstream consumers can invalidate caches when an artifact is superseded or expires

---

## 4. Non-goals

- Does not trigger workflows or orchestrate agents
- Does not store code, PRs, or implementation output
- Does not provide an approval workflow UI or orchestrate agent runs (UI/orchestrator are separate components)
- Does not replace Git or GitLab as source control for code
- Does not provide task management (Jira, Linear, …)

---

## 5. Users & Consumers

| Actor | Role |
|---|---|
| governance-ops service | Publishes an artifact after each planning run |
| FE / BE / QA Agent | Fetches approved artifacts to implement |
| Human Approver (Tech Lead, Product Owner) | Reviews and approves specs |
| Admin | Overrides retention, manages access |

---

## 6. Core Product Idea

The registry behaves as a **versioned document store with lifecycle enforcement**. The governance-ops service publishes an artifact with status `draft`. A human approver reviews it and either moves it to `approved` or requests changes via `needs_changes`. Downstream agents may only consume `approved` artifacts for production-critical workflows.

Each artifact also carries a coarse `impact_level` (`low`, `medium`, `high`) to support prioritization, queueing, and review triage. The level does not replace the explicit list of impacted services/apps.

Every transition is logged and emits an event so the system reacts promptly when an artifact is superseded or expires.

---

## 7. Key Features

### 7.1 Artifact storage

Each artifact bundles a flexible set of documents stored in S3, with metadata stored in Postgres. Feature-backed artifacts are identified by `feature_id` + `version`; quick-route Context Pack artifacts may be standalone when no durable Feature is needed.

### 7.2 Status lifecycle

Artifacts move through explicit states: `draft` → `approved` → `superseded`, or `draft` → `needs_changes` when a human asks the governance-ops service to regenerate. These four statuses are the whole lifecycle. Reverse or skipped transitions are not allowed.

### 7.3 Versioning

Each time the governance-ops service republishes a spec for the same feature, the version bumps as `vMAJOR.MINOR`. The previous version is automatically moved to `superseded` when the new one is approved.

### 7.4 Conflict detection

When the governance-ops service prepares to publish a new artifact, the registry can check whether any active artifact impacts the same services. The result is `no_conflict`, `warning_conflict`, or `blocking_conflict`. The report is advisory — reviewers weigh it before approval; it does not change the stored status.

### 7.5 Access control

The registry is an internal service — it does not enforce HTTP-layer authentication. The trust model relies on the network boundary (VPC / service mesh). Roles (governance-ops service, implementation agent, approver) are tracked via `created_by` / `approved_by` in request bodies for audit purposes only, not to gate access.

### 7.6 Event emission

Each status transition emits an event (`artifact.published`, `artifact.needs_changes`, `artifact.superseded`) into the event log. Downstream agents poll for cache invalidation; the UI/orchestrator uses `artifact.needs_changes` to loop back to the governance-ops service.

### 7.7 Retention

Artifacts are retained for at least 30–180 days depending on status.

---

## 8. User Stories

**governance-ops service publishes a spec:**
After generating PRD and spec, the governance-ops service publishes the artifact with status `draft`, an `impact_level`, and a list of impacted services. The registry runs the conflict check and returns the conflict state before accepting.

**Human approver review:**
A Tech Lead is notified that `checkout-loyalty-points v0.3` needs review. They read the spec, give it a 3/4 rating, and approve. The artifact moves to `approved`. The registry emits an `artifact.published` event.

**Human requests changes:**
If the spec is missing a rollback plan or is still ambiguous, the Tech Lead gives it 1/2, leaves a note, and moves the artifact to `needs_changes`. The UI/orchestrator triggers the governance-ops service to regenerate a new version (for example `v0.4`) instead of allowing an implementation agent to consume `v0.3`.

**FE Agent fetches a spec:**
The FE Agent queries the registry for the latest approved manifest of `checkout-loyalty-points`. The registry returns a signed URL for each file before the agent starts work.

**Conflict scenario:**
Two developers submit two features on the same day; both impact `order-service`. The later publish receives a `blocking_conflict` response. A human is notified to resolve — merge or prioritize one of the two.

**Spec needs revision:**
After review, the tech lead asks for an added rollback plan. The governance-ops service regenerates and publishes `v0.4`. The registry automatically supersedes `v0.3`. Downstream agents receive an `artifact.superseded` event and fetch the new version.

---

## 9. Success Metrics

| Metric | Target |
|---|---|
| Downstream agents never consume an expired artifact in production | 0 incidents during pilot |
| Conflict detection catch rate | 100% — every service overlap is detected |
| Time from governance-ops publish to human notification | < 1 minute |
| Artifact availability (registry uptime) | ≥ 99.5% during working hours |
| API response time | P99 < 300 ms for read endpoints |

---

## 10. Risks

| Risk | Mitigation |
|---|---|
| Database does not scale under high concurrent writes | Postgres: scale via a connection pooler (PgBouncer) or read replicas as needed |
| S3 signed URLs expire while an agent is using them | Set expiry to 15 minutes; agents refresh on a 403 |
| Event polling adds lag to downstream invalidation | Post-MVP: move to webhook push |
| Conflict detection misses when `feature_id` is inconsistent | Validate and normalize `feature_id` format at intake |

---

## 11. MVP Scope

**In scope:**
- CRUD artifacts with the full status lifecycle
- S3 file storage with signed URL delivery
- Service-level conflict detection
- Event log with polling API
- Retention cleanup job
- Dependency contract enforcement

**Out of scope (post-MVP):**
- HTTP authentication / RBAC (registry is internal; trust by network boundary)
- Webhook push for events
- Module-level conflict detection
- UI for an artifact browser (the UI team builds this separately)
- Audit log UI
- mTLS
