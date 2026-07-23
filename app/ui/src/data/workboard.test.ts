import { afterEach, describe, expect, it, vi } from "vitest"

import {
  buildChangeRequestsPath,
  fetchWorkboard,
  fetchWorkItemDetail,
  mapAcceptanceCriterion,
  mapChangeRequestToWorkItem,
  mapGateRun,
  mapFeature,
  mapGovernancePolicy,
  mapDeliveryStatus,
  mapDeliveryLink,
  repositoryObservation,
  mapStaleWarning,
  mapTrackerLink,
} from "@/data/workboard"
import type { WorkItem } from "@/data/workspace"

describe("workboard data adapter", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("scopes live workboard requests to the selected workspace", () => {
    expect(buildChangeRequestsPath("ws-current")).toBe("/workboard/change-requests?workspace_id=ws-current")
    expect(() => buildChangeRequestsPath("")).toThrow("workspaceId is required")
  })

  it("does not query the registry when no workspace is selected", async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)

    await expect(fetchWorkboard("http://registry.test", new AbortController().signal)).resolves.toMatchObject({
      workItems: [],
      status: "ready",
      source: "registry",
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("maps Doc Registry change requests into shell work items", () => {
    const item = mapChangeRequestToWorkItem(
      {
        id: "cr-155",
        key: "SG-155",
        work_type: "bug_fix",
        title: "Doc Registry migration cleanup",
        intent_md: "Clean migration behavior and quiet expected record-not-found test logs.",
        acceptance_criteria_json: JSON.stringify(["Doc registry CI passes", "Expected misses are quiet"]),
        created_by: "Backend",
        created_at: "2026-06-26T04:00:00.000Z",
        updated_at: "2026-06-26T05:00:00.000Z",
        phase: "Ready",
      },
    )

    expect(item).toMatchObject({
      key: "SG-155",
      title: "Doc Registry migration cleanup",
      route: "quick",
      createdBy: "Backend",
      lifecycle: "Ready",
      gate: "pass",
      delivery: "ready",
      blocker: "none",
      updated: expect.stringContaining("2026"),
      registryId: "cr-155",
      acceptance: ["Doc registry CI passes", "Expected misses are quiet"],
      activity: expect.arrayContaining(["Quick-route Context Pack derives on demand from persisted work"]),
    })
    expect(item.activity).toContain("Quick route uses persisted work as the handoff source")
    expect(item.activity).not.toContain("Waiting for lead artifact")
  })

  it("retains the exact lead artifact association", () => {
    const item = mapChangeRequestToWorkItem({
      id: "cr-155",
      key: "SG-155",
      title: "Exact artifact delivery",
      lead_artifact_id: "artifact-155",
    })

    expect(item.leadArtifactId).toBe("artifact-155")
  })

  it("maps the server-derived delivered phase to a passed delivery", () => {
    const item = mapChangeRequestToWorkItem({
      id: "cr-160",
      key: "SG-160",
      work_type: "cleanup",
      title: "Delivered settings polish",
      phase: "delivered",
    })

    expect(item).toMatchObject({
      key: "SG-160",
      lifecycle: "delivered",
      gate: "pass",
      delivery: "accepted",
      blocker: "none",
    })
  })

  it("maps the authoritative delivery review snapshot from list rows", () => {
    const item = mapChangeRequestToWorkItem({
      id: "cr-review",
      key: "CR-REVIEW",
      title: "Needs human review",
      delivery_review: {
        verdict: "needs_human_review",
        hint: "Missing one browser check.",
        reviewed_at: "2026-07-03T14:10:28Z",
      },
    })

    expect(item.delivery).toBe("needs_changes")
    expect(item.deliveryVerdict).toBe("needs_human_review")
    expect(item.deliveryHint).toBe("Missing one browser check.")
  })

  it("does not turn a platform delivery pass into a Review-phase gate failure", () => {
    const item = mapChangeRequestToWorkItem({
      id: "cr-ready",
      key: "CR-READY",
      title: "Ready for acceptance",
      phase: "Review",
      delivery_review: {
        verdict: "pass",
        hint: "Delivery evidence is ready for human review.",
        reviewed_at: "2026-07-19T12:00:00Z",
      },
    })

    expect(item.gate).toBe("pass")
    expect(item.delivery).toBe("passed")
  })

  it("keeps incomplete change requests visible with conservative defaults", () => {
    const item = mapChangeRequestToWorkItem({ id: "cr-1", title: "Clarify setup" })

    expect(item.key).toBe("cr-1")
    expect(item.route).toBe("quick")
    expect(item.lifecycle).toBe("Intake")
    expect(item.gate).toBe("pending")
    expect(item.delivery).toBe("not_started")
    expect(item.blocker).toBe("needs governance progress")
    expect(item.acceptance).toEqual([])
  })

  it("maps work item detail endpoint rows", () => {
    expect(mapAcceptanceCriterion({ id: "ac-1", text: "Ship evidence", done: true, source: "human" }, 0)).toEqual({
      id: "ac-1",
      text: "Ship evidence",
      done: true,
      source: "human",
    })

    expect(mapGateRun({ id: "run-1", gate: "delivery_review", state: "fail", hint: "Missing checks" })).toMatchObject({
      id: "run-1",
      gate: "delivery_review",
      state: "fail",
      hint: "Missing checks",
    })
    expect(mapGateRun({ gate: "delivery_review", state: "fail", hint: "Missing checks" })).toBeNull()

    expect(mapTrackerLink({ identifier: "ENG-123", url: "https://example.test/ENG-123", state: "opened" })).toEqual({
      identifier: "ENG-123",
      url: "https://example.test/ENG-123",
      state: "opened",
      trackerState: undefined,
    })
    expect(mapTrackerLink({ identifier: "ENG-123", url: "https://example.test/ENG-123" })).not.toHaveProperty("lane")

    expect(mapFeature({ id: "feature-1", key: "SG-1", name: "Checkout" })).toMatchObject({
      id: "feature-1",
      key: "SG-1",
      name: "Checkout",
    })
    expect(mapFeature({ name: "Missing feature reference" })).toBeNull()
  })

  it("keeps persisted delivery-link heads and derives display-only repository observation", () => {
    const exact = mapDeliveryLink({
      external_key: "specgate/ui#42",
      title: "Expose Linear handoff",
      url: "https://github.test/specgate/ui/pull/42",
      state: "merged",
      source_branch: "codex/ui",
      target_branch: "main",
      head_sha: "submitted-head",
      merge_commit_sha: "merge-head",
      updated_at: "2026-07-21T09:00:00Z",
    })
    expect(exact).toMatchObject({ headSha: "submitted-head", mergeCommitSha: "merge-head" })
    expect(repositoryObservation(exact!, "submitted-head")).toBe("exact")
    expect(repositoryObservation({ ...exact!, headSha: "SUBMITTED-HEAD" }, " submitted-head ")).toBe("exact")
    expect(repositoryObservation({ ...exact!, state: "opened" }, "submitted-head")).toBe("open")
    expect(repositoryObservation({ ...exact!, headSha: "different-head" }, "submitted-head")).toBe("stale")
    expect(repositoryObservation(exact!, undefined)).toBe("missing-receipt")
    expect(repositoryObservation({ ...exact!, state: "closed" }, "submitted-head")).toBe("closed")
  })

  it("preserves delivery trust provenance from the registry", () => {
    expect(
      mapDeliveryStatus({
        change_request_id: "cr-155",
        gate_run_id: "gate-run-155",
        completion_feedback_event_id: "completion-155",
        found: true,
        verdict: "pass",
        assurance_sources: ["repository_observed"],
        reason_code: "review_completed",
        judge_model: "agent_attested",
        executor: "platform",
        actor: "governance-agent",
        git_receipt: {
          availability: "available",
          base_revision: "base-1",
          branch: "codex/trust",
          changed_files: ["app/ui/src/data/workboard.ts"],
          diff_digest: "sha256:digest",
          head_revision: "head-1",
          repository: "specgate",
          warnings: [],
        },
        peer_review: {
          agent_name: "review-agent",
          reviewed_at: "2026-07-19T08:00:00Z",
          state: "stale",
        },
        per_criterion: [
          {
            criterion_id: "ac-1",
            text: "Keep provenance visible",
            verdict: "met",
            trust_tier: "grounded",
            verification_binding: "app/ui/src/data/workboard.ts:496",
          },
        ],
      }),
    ).toMatchObject({
      found: true,
      gateRunId: "gate-run-155",
      completionFeedbackEventId: "completion-155",
      verdict: "pass",
      assuranceSources: ["repository_observed"],
      reasonCode: "review_completed",
      judgeModel: "agent_attested",
      executor: "platform",
      actor: "governance-agent",
      gitReceipt: {
        headRevision: "head-1",
        branch: "codex/trust",
      },
      peerReview: {
        agentName: "review-agent",
        state: "stale",
      },
      criteria: [
        {
          id: "ac-1",
          trustTier: "grounded",
          verificationBinding: "app/ui/src/data/workboard.ts:496",
        },
      ],
    })
  })

  it("does not synthesize live governance policy lineage keys", () => {
    expect(
      mapGovernancePolicy({
        governance_level: "standard",
        title: "Standard governance",
        policy_lineage: [
          { version: "v1", digest: "missing-key" },
          { key: "delivery_policy", version: "v2", digest: "digest-2" },
        ],
      }),
    ).toMatchObject({
      level: "standard",
      lineage: [
        {
          key: "delivery_policy",
          version: "v2",
          digest: "digest-2",
        },
      ],
    })
  })

  it("maps stale warnings into read-only freshness signals", () => {
    expect(
      mapStaleWarning(
        {
          code: "linked_knowledge_newer",
          severity: "warning",
          message: "Context Pack is stale after a delivery branch changed.",
          feature_id: "feature-1",
          change_request_id: "cr-live",
          artifact_id: "artifact-pack",
        },
        0,
      ),
    ).toEqual({
      id: "linked_knowledge_newer-cr-live-artifact-pack-0",
      code: "linked_knowledge_newer",
      severity: "warning",
      message: "Context Pack is stale after a delivery branch changed.",
      featureId: "feature-1",
      changeRequestId: "cr-live",
      artifactId: "artifact-pack",
    })

    expect(mapStaleWarning({}, 1)).toMatchObject({
      id: "stale-warning-2",
      code: "unknown",
      severity: "info",
      message: "No freshness detail recorded.",
    })
  })

  it("does not fill live work item detail with sample acceptance criteria", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/acceptance-criteria")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [] })))
      }
      if (url.includes("/next-actions")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [] })))
      }
      if (url.includes("/gate-runs")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [] })))
      }
      if (url.includes("/tracker-links")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [] })))
      }
      if (url.includes("/delivery-links")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{
          external_key: "specgate/ui#42", title: "Merged UI", url: "https://github.test/specgate/ui/pull/42", state: "merged",
          source_branch: "codex/ui", target_branch: "main", head_sha: "submitted-head", merge_commit_sha: "merge-head", updated_at: "2026-07-21T09:00:00Z",
        }] })))
      }
      if (url.includes("/stale-warnings")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [] })))
      }
      return Promise.resolve(new Response("{}", { status: 404 }))
    })
    vi.stubGlobal("fetch", fetchMock)

    const detail = await fetchWorkItemDetail(
      "http://registry.test",
      {
        registryId: "cr-live-empty",
        key: "CR-LIVE-EMPTY",
        title: "Live item without criteria",
        route: "quick",
        createdBy: "Product",
        agent: "Codex",
        lifecycle: "Intake",
        status: "route suggested",
        gate: "pending",
        delivery: "not_started",
        blocker: "needs governance progress",
        age: "1m",
        updated: "just now",
        skills: [],
        summary: "Registry detail should stay empty when registry rows are empty.",
        acceptance: ["sample criteria must not leak"],
        activity: [],
      } satisfies WorkItem,
      "ws-live",
      new AbortController().signal,
    )

    expect(detail.source).toBe("registry")
    expect(detail.acceptanceCriteria).toEqual([])
    expect(detail.deliveryLinks).toEqual([expect.objectContaining({ headSha: "submitted-head", mergeCommitSha: "merge-head" })])
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/workboard/change-requests/cr-live-empty/acceptance-criteria?workspace_id=ws-live",
      expect.any(Object),
    )
    for (const [input] of fetchMock.mock.calls) {
      expect(String(input)).toContain("workspace_id=ws-live")
    }
  })
})
