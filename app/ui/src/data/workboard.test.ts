import { afterEach, describe, expect, it, vi } from "vitest"

import {
  buildChangeRequestsPath,
  contextPackUriForWorkItem,
  fetchWorkItemDetail,
  mapAcceptanceCriterion,
  mapChangeRequestToWorkItem,
  mapGateRun,
  mapFeature,
  mapGovernancePolicy,
  mapNextAction,
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
    expect(buildChangeRequestsPath("")).toBe("/workboard/change-requests")
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
        context_pack_artifact_id: "artifact-context-pack",
        created_by: "Backend",
        created_at: "2026-06-26T04:00:00.000Z",
        updated_at: "2026-06-26T05:00:00.000Z",
        phase: "Handoff",
      },
    )

    expect(item).toMatchObject({
      key: "SG-155",
      title: "Doc Registry migration cleanup",
      route: "quick",
      owner: "Backend",
      lifecycle: "Handoff",
      gate: "pass",
      delivery: "ready",
      blocker: "none",
      updated: expect.stringContaining("2026"),
      contextPackId: "cp-SG-155",
      registryId: "cr-155",
      acceptance: ["Doc registry CI passes", "Expected misses are quiet"],
    })
  })

  it("uses the registry id for copied Context Pack URIs", () => {
    const item = mapChangeRequestToWorkItem({
      id: "d6425c9b-2790-40e8-be74-ac4afe5ca849",
      key: "CR-1D0256D8",
      context_pack_artifact_id: "artifact-context-pack",
      title: "Lean OSS storage Phase 0",
    })

    expect(contextPackUriForWorkItem(item)).toBe(
      "specgate://context-pack/d6425c9b-2790-40e8-be74-ac4afe5ca849",
    )
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

    expect(mapNextAction({ gate: "delivery_pack", state: "pending", hint: "Build pack" }, 0)).toEqual({
      gate: "delivery_pack",
      state: "pending",
      hint: "Build pack",
      actionEndpoint: undefined,
    })

    expect(mapGateRun({ id: "run-1", gate: "delivery_review", state: "fail", hint: "Missing checks" })).toMatchObject({
      id: "run-1",
      gate: "delivery_review",
      state: "fail",
      hint: "Missing checks",
    })
    expect(mapGateRun({ gate: "delivery_review", state: "fail", hint: "Missing checks" })).toBeNull()

    expect(mapTrackerLink({ lane: "fe", identifier: "ENG-123", url: "https://example.test/ENG-123", state: "opened" })).toEqual({
      lane: "fe",
      identifier: "ENG-123",
      url: "https://example.test/ENG-123",
      state: "opened",
      trackerState: undefined,
    })

    expect(mapFeature({ id: "feature-1", key: "SG-1", name: "Checkout" })).toMatchObject({
      id: "feature-1",
      key: "SG-1",
      name: "Checkout",
    })
    expect(mapFeature({ name: "Missing feature reference" })).toBeNull()
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
          code: "context_pack_stale",
          severity: "warning",
          message: "Context Pack is stale after a delivery branch changed.",
          feature_id: "feature-1",
          change_request_id: "cr-live",
          artifact_id: "artifact-pack",
        },
        0,
      ),
    ).toEqual({
      id: "context_pack_stale-cr-live-artifact-pack-0",
      code: "context_pack_stale",
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
        owner: "Product",
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
      new AbortController().signal,
    )

    expect(detail.source).toBe("registry")
    expect(detail.acceptanceCriteria).toEqual([])
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/workboard/change-requests/cr-live-empty/acceptance-criteria",
      expect.any(Object),
    )
  })
})
