import { createRegistryClient } from "@/api/client"
import type { components } from "@/api/schema"
import type { Lane, Signal, WorkItem } from "@/data/workspace"
import {
  mapAcceptanceCriterion,
  mapChangeRequestToWorkItem,
  mapContextPackResult,
  mapDeliveryLink,
  mapDeliveryStatus,
  mapFeature,
  mapGateRun,
  mapGovernancePolicy,
  mapNextAction,
  mapStaleWarning,
  mapTrackerLink,
} from "./workboard-mappers"
import {
  emptyRegistryView,
  type AcceptanceCriterionDTO,
  type ContextPackResultDTO,
  type DeliveryLinkDTO,
  type DeliveryStatusDTO,
  type FeatureDTO,
  type GateRunDTO,
  type GovernancePolicyDTO,
  type ListResponse,
  type NextActionDTO,
  type StaleWarningDTO,
  type TrackerLinkDTO,
  type WorkboardView,
  type WorkItemDetailData,
} from "./workboard-types"

function buildLanes(items: WorkItem[]): Lane[] {
  const laneOrder = ["Intake", "Review", "Ready"]

  return laneOrder
    .map((title) => {
      const laneItems = items.filter((item) => item.lifecycle === title)
      return {
        title,
        count: laneItems.length,
        tone: title === "Ready" ? "success" : title === "Review" ? "warning" : "neutral",
        items: laneItems,
      } satisfies Lane
    })
    .filter((lane) => lane.items.length > 0)
}

function buildSignals(items: WorkItem[]): Signal[] {
  // Delivered items are finished work: they are not handoff candidates.
  const active = items.filter((item) => !(item.lifecycle.trim().toLowerCase() === "delivered"))
  const ready = active.filter((item) => item.delivery === "ready" || item.gate === "pass").length
  const blocked = active.filter((item) => item.blocker !== "none").length
  const gateDebt = active.filter((item) => item.gate !== "pass").length

  return [
    { label: "Ready for pickup", value: String(ready), detail: "approved source available", tone: ready > 0 ? "success" : "neutral" },
    { label: "Checks not passing", value: String(gateDebt), detail: "needs attention", tone: gateDebt > 0 ? "warning" : "success" },
    { label: "Waiting on you", value: String(blocked), detail: "not approved yet", tone: blocked > 0 ? "danger" : "success" },
  ]
}

function buildRegistryView(items: WorkItem[]): WorkboardView {
  return {
    workItems: items,
    lanes: buildLanes(items),
    signals: buildSignals(items),
    source: "registry",
    status: "ready",
  }
}

export function buildChangeRequestsPath(workspaceId?: string): string {
  const trimmed = workspaceId?.trim()
  if (!trimmed) throw new Error("workspaceId is required")
  return `/workboard/change-requests?${new URLSearchParams({ workspace_id: trimmed }).toString()}`
}

export async function fetchWorkboard(baseUrl: string, signal: AbortSignal, selectedWorkspaceId?: string): Promise<WorkboardView> {
  const workspaceId = selectedWorkspaceId?.trim()
  if (!workspaceId) return { ...emptyRegistryView }

  const { data, error, response } = await createRegistryClient(baseUrl).GET("/workboard/change-requests", {
    params: { query: { workspace_id: workspaceId } },
    signal,
  })
  if (error || !data) {
    throw new Error(`workboard request failed: ${response.status}`)
  }
  const items = (data.items ?? []).map((item) => mapChangeRequestToWorkItem(item))
  return buildRegistryView(items)
}

function withWorkspace(path: string, workspaceId: string): string {
  const workspace = workspaceId.trim()
  if (!workspace) throw new Error("workspaceId is required")
  return `${path}${path.includes("?") ? "&" : "?"}${new URLSearchParams({ workspace_id: workspace }).toString()}`
}

async function fetchList<T>(baseUrl: string, path: string, workspaceId: string, signal: AbortSignal): Promise<T[]> {
  const scopedPath = withWorkspace(path, workspaceId)
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}${scopedPath}`, { signal })
  if (!response.ok) {
    throw new Error(`${scopedPath} request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListResponse<T>
  return payload.items ?? []
}

export async function fetchWorkItemDetail(baseUrl: string, item: WorkItem, workspaceId: string, signal: AbortSignal): Promise<WorkItemDetailData> {
  const id = encodeURIComponent(item.registryId || item.key)
  const featureId = item.featureId ? encodeURIComponent(item.featureId) : ""
  const staleWarningPath = `/workboard/stale-warnings?${new URLSearchParams({ change_request_id: item.registryId || item.key }).toString()}`
  const [acceptance, nextActions, gateRuns, staleWarnings, trackerLinks, deliveryLinks, policy, deliveryStatus, feature, contextPack] = await Promise.all([
    fetchList<AcceptanceCriterionDTO>(baseUrl, `/workboard/change-requests/${id}/acceptance-criteria`, workspaceId, signal)
      .then((items) => ({ items, status: "ready" as const }))
      .catch(() => ({ items: [] satisfies AcceptanceCriterionDTO[], status: "error" as const })),
    fetchList<NextActionDTO>(baseUrl, `/workboard/change-requests/${id}/next-actions`, workspaceId, signal)
      .then((items) => ({ items, status: "ready" as const }))
      .catch(() => ({ items: [] satisfies NextActionDTO[], status: "error" as const })),
    fetchList<GateRunDTO>(baseUrl, `/workboard/change-requests/${id}/gate-runs?limit=10`, workspaceId, signal)
      .then((items) => ({ items, status: "ready" as const }))
      .catch(() => ({ items: [] satisfies GateRunDTO[], status: "error" as const })),
    fetchList<StaleWarningDTO>(baseUrl, staleWarningPath, workspaceId, signal).catch(
      () => [] satisfies StaleWarningDTO[],
    ),
    fetchList<TrackerLinkDTO>(baseUrl, `/workboard/change-requests/${id}/tracker-links`, workspaceId, signal)
      .then((items) => ({ items, status: "ready" as const }))
      .catch(() => ({ items: [] satisfies TrackerLinkDTO[], status: "error" as const })),
    fetchList<DeliveryLinkDTO>(baseUrl, `/workboard/change-requests/${id}/delivery-links`, workspaceId, signal)
      .then((items) => ({ items, status: "ready" as const }))
      .catch(() => ({ items: [] satisfies DeliveryLinkDTO[], status: "error" as const })),
    fetch(`${baseUrl.replace(/\/$/, "")}${withWorkspace(`/api/v1/work-items/${id}/policy`, workspaceId)}`, { signal })
      .then((response) => {
        if (!response.ok) throw new Error(`work item policy request failed: ${response.status}`)
        return response.json() as Promise<GovernancePolicyDTO>
      })
      .then(mapGovernancePolicy)
      .then((item) => ({ item, status: "ready" as const }))
      .catch(() => ({ item: undefined, status: "error" as const })),
    fetch(`${baseUrl.replace(/\/$/, "")}${withWorkspace(`/api/v1/work-items/${id}/delivery-status?detail=true`, workspaceId)}`, { signal })
      .then((response) => {
        if (!response.ok) throw new Error(`delivery status request failed: ${response.status}`)
        return response.json() as Promise<DeliveryStatusDTO>
      })
      .then(mapDeliveryStatus)
      .then((item) => ({ item, status: "ready" as const }))
      .catch(() => ({ item: undefined, status: "error" as const })),
    featureId
      ? fetch(`${baseUrl.replace(/\/$/, "")}${withWorkspace(`/workboard/features/${featureId}`, workspaceId)}`, { signal })
        .then((response) => {
          if (!response.ok) throw new Error(`feature request failed: ${response.status}`)
          return response.json() as Promise<FeatureDTO>
        })
        .then(mapFeature)
        .then((item) => ({ item: item ?? undefined, status: "ready" as const }))
        .catch(() => ({ item: undefined, status: "error" as const }))
      : Promise.resolve({ item: undefined, status: "ready" as const }),
    fetch(`${baseUrl.replace(/\/$/, "")}${withWorkspace(`/api/v1/work-items/${id}/context-pack`, workspaceId)}`, { signal })
        .then((response) => {
          if (!response.ok) throw new Error(`context pack request failed: ${response.status}`)
          return response.json() as Promise<ContextPackResultDTO>
        })
        .then(mapContextPackResult)
        .then((item) => (item ? { item, status: "ready" as const } : { item: undefined, status: "error" as const }))
        .catch(() => ({ item: undefined, status: "error" as const }))
  ])

  return {
    acceptanceCriteria: acceptance.items.map((criterion, index) => mapAcceptanceCriterion(criterion, index)),
    nextActions: nextActions.items.map((action, index) => mapNextAction(action, index)),
    gateRuns: gateRuns.items.flatMap((run) => {
      const gateRun = mapGateRun(run)
      return gateRun ? [gateRun] : []
    }),
    staleWarnings: staleWarnings.map((warning, index) => mapStaleWarning(warning, index)),
    trackerLinks: trackerLinks.items.flatMap((link) => {
      const trackerLink = mapTrackerLink(link)
      return trackerLink ? [trackerLink] : []
    }),
    deliveryLinks: deliveryLinks.items.flatMap((link) => {
      const deliveryLink = mapDeliveryLink(link)
      return deliveryLink ? [deliveryLink] : []
    }),
    policy: policy.item,
    deliveryStatus: deliveryStatus.item,
    feature: feature.item,
    contextPack: contextPack.item,
    readback: {
      acceptance: acceptance.status,
      nextActions: nextActions.status,
      gateRuns: gateRuns.status,
      trackerLinks: trackerLinks.status,
      deliveryLinks: deliveryLinks.status,
      feature: feature.status,
      policy: policy.status,
      delivery: deliveryStatus.status,
      contextPack: contextPack.status,
    },
    status: "ready",
    source: "registry",
  }
}

export async function recordDeliveryDecision(
  workItemId: string,
  decision: "approve" | "reject",
  actor: string,
  workspaceId: string,
  reviewedGateRunId: string,
  completionFeedbackEventId: string,
  note?: string,
): Promise<void> {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const workspace = workspaceId.trim()
  if (!baseUrl) throw new Error("Doc Registry is not configured.")
  if (!workspace) throw new Error("Workspace is required.")

  const body: components["schemas"]["CLIDeliveryDecisionInputBody"] = {
    decision,
    actor,
    reviewed_gate_run_id: reviewedGateRunId,
    completion_feedback_event_id: completionFeedbackEventId,
    ...(note ? { note } : {}),
  }
  const response = await fetch(
    `${baseUrl.replace(/\/$/, "")}/api/v1/work-items/${encodeURIComponent(workItemId)}/delivery-decision?${new URLSearchParams({ workspace_id: workspace })}`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
  )
  if (response.ok) return

  const problem = await response.json().catch(() => undefined) as { detail?: unknown } | undefined
  const detail = typeof problem?.detail === "string" ? problem.detail.trim() : ""
  throw new Error(detail || `Delivery decision failed (${response.status}).`)
}
