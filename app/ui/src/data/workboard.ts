import { useEffect, useMemo, useState } from "react"

import {
  type Lane,
  type Signal,
  type WorkItem,
} from "@/data/workspace"
import { formatDateTime } from "@/lib/format"

type ChangeRequestDTO = {
  id?: string
  key?: string
  feature_id?: string
  work_type?: string
  title?: string
  intent_md?: string
  acceptance_criteria_json?: string
  lead_artifact_id?: string
  context_pack_artifact_id?: string
  created_by?: string
  created_at?: string
  updated_at?: string
  phase?: string
  tracker_status?: string
}

type ListChangeRequestsResponse = {
  items?: ChangeRequestDTO[]
}

type WorkspaceDTO = {
  id?: string
  slug?: string
  name?: string
}

type AcceptanceCriterionDTO = {
  id?: string
  text?: string
  done?: boolean
  source?: string
}

type NextActionDTO = {
  gate?: string
  state?: string
  hint?: string
  action_endpoint?: string
}

type GateRunDTO = {
  id?: string
  gate?: string
  state?: string
  hint?: string
  executor?: string
  action_endpoint?: string
  proposal_ref?: string
  evidence_json?: string
  created_at?: string
}

type StaleWarningDTO = {
  code?: string
  severity?: string
  message?: string
  feature_id?: string
  change_request_id?: string
  artifact_id?: string
}

type GovernancePolicyLineageDTO = {
  key?: string
  version?: string
  digest?: string
}

type GovernancePolicyDTO = {
  governance_level?: string
  title?: string
  summary?: string
  reasons?: string[]
  obligations?: string[]
  policy_lineage?: GovernancePolicyLineageDTO[]
}

type DeliveryCriterionDTO = {
  criterion_id?: string
  text?: string
  verdict?: string
  why?: string
}

type DeliveryCheckDTO = {
  name?: string
  status?: string
  detail?: string
}

type DeliveryStatusDTO = {
  change_request_id?: string
  found?: boolean
  verdict?: string
  hint?: string
  confidence?: number
  reviewed_at?: string
  outstanding_md?: string
  per_criterion?: DeliveryCriterionDTO[]
  checks?: DeliveryCheckDTO[]
}

type TrackerLinkDTO = {
  lane?: string
  identifier?: string
  url?: string
  state?: string
  tracker_state?: string
}

type FeatureDTO = {
  id?: string
  key?: string
  name?: string
  summary?: string
  status?: string
  version?: number
  canonical_artifact_id?: string
  summary_md?: string
  summary_source_version?: string
}

type ContextPackWarningDTO = {
  code?: string
  message?: string
  artifact_id?: string
}

type ContextPackProvenanceDTO = {
  document_id?: string
  title?: string
  version?: string
  document_type?: string
  authority_level?: string
  is_latest?: boolean
  freshness?: string
  knowledge_store_uri?: string
}

type ContextPackResultDTO = {
  state?: string
  markdown?: string
  knowledge_provenance?: ContextPackProvenanceDTO[]
  warnings?: ContextPackWarningDTO[]
  lane?: string
  change_request_id?: string
  feature_id?: string
  source_artifact_id?: string
  context_pack_artifact_id?: string
  context_pack_uri?: string
  artifact_id?: string
  kind?: string
  governance_level?: string
}

type ListResponse<T> = {
  items?: T[]
}

export type ContextPackSummary = {
  id: string
  title: string
  uri: string
  evidence: string
}

export type AcceptanceCriterionSummary = {
  id: string
  text: string
  done: boolean
  source: string
}

export type NextActionSummary = {
  gate: string
  state: string
  hint: string
  actionEndpoint?: string
}

export type GateRunSummary = {
  id: string
  gate: string
  state: string
  hint: string
  executor?: string
  actionEndpoint?: string
  proposalRef?: string
  evidence?: string
  createdAt?: string
}

export type StaleWarningSummary = {
  id: string
  code: string
  severity: string
  message: string
  featureId?: string
  changeRequestId?: string
  artifactId?: string
}

export type TrackerLinkSummary = {
  lane?: string
  identifier: string
  url: string
  state: string
  trackerState?: string
}

export type GovernancePolicyLineageSummary = {
  key: string
  version?: string
  digest?: string
}

export type GovernancePolicySummary = {
  level: string
  title: string
  summary: string
  reasons: string[]
  obligations: string[]
  lineage: GovernancePolicyLineageSummary[]
}

export type DeliveryCriterionSummary = {
  id: string
  text: string
  verdict: string
  why?: string
}

export type DeliveryCheckSummary = {
  name: string
  status: string
  detail?: string
}

export type DeliveryStatusSummary = {
  found: boolean
  verdict?: string
  hint?: string
  confidence?: number
  reviewedAt?: string
  outstandingMd?: string
  criteria: DeliveryCriterionSummary[]
  checks: DeliveryCheckSummary[]
}

export type FeatureContextSummary = {
  id: string
  key: string
  name: string
  status: string
  version?: number
  summary?: string
  summaryMd?: string
  canonicalArtifactId?: string
  summarySourceVersion?: string
}

export type ContextPackWarningSummary = {
  id: string
  code: string
  message: string
  artifactId?: string
}

export type ContextPackProvenanceSummary = {
  id: string
  title: string
  version?: string
  documentType?: string
  authorityLevel?: string
  freshness?: string
  knowledgeStoreUri?: string
}

export type CanonicalContextPackSummary = {
  state: string
  markdown: string
  uri?: string
  changeRequestId?: string
  featureId?: string
  sourceArtifactId?: string
  contextPackArtifactId?: string
  governanceLevel?: string
  warnings: ContextPackWarningSummary[]
  knowledgeProvenance: ContextPackProvenanceSummary[]
}

export type WorkItemDetailData = {
  acceptanceCriteria: AcceptanceCriterionSummary[]
  nextActions: NextActionSummary[]
  gateRuns: GateRunSummary[]
  staleWarnings: StaleWarningSummary[]
  trackerLinks: TrackerLinkSummary[]
  policy?: GovernancePolicySummary
  deliveryStatus?: DeliveryStatusSummary
  feature?: FeatureContextSummary
  contextPack?: CanonicalContextPackSummary
  readback: WorkItemReadbackStatus
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type WorkItemReadbackStatus = {
  acceptance: "ready" | "loading" | "error"
  nextActions: "ready" | "loading" | "error"
  gateRuns: "ready" | "loading" | "error"
  trackerLinks: "ready" | "loading" | "error"
  feature: "ready" | "loading" | "error"
  policy: "ready" | "loading" | "error"
  delivery: "ready" | "loading" | "error"
  contextPack: "ready" | "loading" | "error"
}

export type WorkboardView = {
  workItems: WorkItem[]
  lanes: Lane[]
  signals: Signal[]
  contextPacks: ContextPackSummary[]
  source: "registry"
  status: "ready" | "loading" | "error"
}

export function contextPackUriForWorkItem(item: Pick<WorkItem, "key" | "registryId">): string {
  return `specgate://context-pack/${item.registryId || item.key}`
}

export type CreateWorkResult = {
  item: WorkItem
  persisted: boolean
}

export type RouteClassification = {
  changeRequestId?: string
  route: WorkItem["route"]
  confidence: number
  rationale: string
}

type GenerateContextPackResponse = {
  artifact?: {
    id?: string
    artifact_id?: string
  }
  change_request?: ChangeRequestDTO
  content_md?: string
  warnings?: unknown[]
}

const emptyRegistryView: WorkboardView = {
  workItems: [],
  lanes: [],
  signals: [
    { label: "Ready for handoff", value: "0", detail: "handoff candidates", tone: "neutral" },
    { label: "Open gate failures", value: "0", detail: "needs attention", tone: "success" },
    { label: "Blocked by ambiguity", value: "0", detail: "open blockers", tone: "success" },
  ],
  contextPacks: [],
  source: "registry",
  status: "ready",
}

function parseStringList(value: string | undefined): string[] {
  if (!value) return []

  try {
    const parsed = JSON.parse(value) as unknown
    if (Array.isArray(parsed)) {
      return parsed.filter((item): item is string => typeof item === "string" && item.trim().length > 0)
    }
  } catch {
    return []
  }

  return []
}

function routeForWorkType(workType: string | undefined): WorkItem["route"] {
  if (workType === "new_feature" || workType === "feature_change") return "full"
  return "quick"
}

function lifecycleForChangeRequest(item: ChangeRequestDTO): string {
  if (item.phase) return item.phase
  if (item.context_pack_artifact_id) return "Handoff"
  if (item.lead_artifact_id) return "Ready"
  return "Intake"
}

// Server-derived phase for change requests whose latest delivery review
// passed. Additive: old servers never send it and nothing else changes.
function isDeliveredPhase(phase: string | undefined): boolean {
  return phase?.trim().toLowerCase() === "delivered"
}

function deliveryForChangeRequest(item: ChangeRequestDTO): WorkItem["delivery"] {
  if (isDeliveredPhase(item.phase)) return "passed"
  if (item.phase === "Review") return "needs_changes"
  if (item.context_pack_artifact_id) return "ready"
  return "not_started"
}

function gateForChangeRequest(item: ChangeRequestDTO): WorkItem["gate"] {
  if (isDeliveredPhase(item.phase)) return "pass"
  if (item.context_pack_artifact_id) return "pass"
  if (item.phase === "Review") return "fail"
  return "pending"
}

function summaryFromIntent(value: string | undefined): string {
  const cleaned = value?.replaceAll(/\s+/g, " ").trim()
  if (!cleaned) return "No intent summary recorded yet."
  return cleaned.length > 180 ? `${cleaned.slice(0, 177)}...` : cleaned
}

export function mapChangeRequestToWorkItem(item: ChangeRequestDTO): WorkItem {
  const key = item.key || item.id || "unkeyed"
  const lifecycle = lifecycleForChangeRequest(item)
  const contextPackId = item.context_pack_artifact_id ? `cp-${key}` : undefined

  return {
    registryId: item.id,
    featureId: item.feature_id,
    key,
    title: item.title || "Untitled work item",
    route: routeForWorkType(item.work_type),
    owner: item.created_by || "Unassigned",
    agent: "Governance",
    lifecycle,
    status: item.tracker_status || lifecycle.toLowerCase(),
    gate: gateForChangeRequest(item),
    delivery: deliveryForChangeRequest(item),
    blocker: contextPackId || isDeliveredPhase(item.phase) ? "none" : "needs governance progress",
    age: formatDateTime(item.created_at ?? item.updated_at),
    updated: formatDateTime(item.updated_at),
    contextPackId,
    skills: [],
    summary: summaryFromIntent(item.intent_md),
    acceptance: parseStringList(item.acceptance_criteria_json),
    activity: [
      item.lead_artifact_id ? "Lead artifact linked" : "Waiting for lead artifact",
      item.context_pack_artifact_id ? "Context Pack generated" : "Context Pack not built",
    ],
  }
}

export function mapAcceptanceCriterion(item: AcceptanceCriterionDTO, index: number): AcceptanceCriterionSummary {
  return {
    id: item.id || `ac-${index + 1}`,
    text: item.text || "Untitled acceptance criterion",
    done: item.done ?? false,
    source: item.source || "unknown",
  }
}

export function mapNextAction(item: NextActionDTO, index: number): NextActionSummary {
  return {
    gate: item.gate || `gate_${index + 1}`,
    state: item.state || "pending",
    hint: item.hint || "No gate hint recorded.",
    actionEndpoint: item.action_endpoint,
  }
}

export function mapGateRun(item: GateRunDTO): GateRunSummary | null {
  const id = item.id?.trim()
  if (!id) return null

  return {
    id,
    gate: item.gate || "unknown",
    state: item.state || "pending",
    hint: item.hint || "No gate-run hint recorded.",
    executor: item.executor,
    actionEndpoint: item.action_endpoint,
    proposalRef: item.proposal_ref,
    evidence: item.evidence_json,
    createdAt: item.created_at,
  }
}

export function mapStaleWarning(item: StaleWarningDTO, index: number): StaleWarningSummary {
  const code = item.code || "unknown"
  const changeRequestId = item.change_request_id
  const artifactId = item.artifact_id
  const scope = changeRequestId || item.feature_id || "scope"
  const suffix = artifactId || "none"
  const hasStableCode = Boolean(item.code)

  return {
    id: hasStableCode ? `${code}-${scope}-${suffix}-${index}` : `stale-warning-${index + 1}`,
    code,
    severity: item.severity || "info",
    message: item.message || "No freshness detail recorded.",
    featureId: item.feature_id,
    changeRequestId,
    artifactId,
  }
}

export function mapGovernancePolicy(item: GovernancePolicyDTO | undefined): GovernancePolicySummary | undefined {
  if (!item) return undefined
  const hasPolicyData = Boolean(
    item.governance_level?.trim() ||
    item.title?.trim() ||
    item.summary?.trim() ||
    item.reasons?.some((reason) => reason.trim().length > 0) ||
    item.obligations?.some((obligation) => obligation.trim().length > 0) ||
    item.policy_lineage?.some((entry) => entry.key?.trim() || entry.version?.trim() || entry.digest?.trim()),
  )
  if (!hasPolicyData) return undefined

  const title = item.title?.trim() || "Governance policy"
  const summary = item.summary?.trim() || "No policy explanation recorded."

  return {
    level: item.governance_level?.trim() || "standard",
    title,
    summary,
    reasons: (item.reasons ?? []).filter((reason) => reason.trim().length > 0),
    obligations: (item.obligations ?? []).filter((obligation) => obligation.trim().length > 0),
    lineage: (item.policy_lineage ?? []).flatMap((entry) => {
      const key = entry.key?.trim()
      if (!key) return []
      return [{
        key,
        version: entry.version?.trim() || undefined,
        digest: entry.digest?.trim() || undefined,
      }]
    }),
  }
}

function mapDeliveryStatus(item: DeliveryStatusDTO | undefined): DeliveryStatusSummary | undefined {
  if (!item) return undefined
  if (item.found === false) {
    return { found: false, criteria: [], checks: [] }
  }
  const hasStatusData = Boolean(
    item.verdict?.trim() ||
    item.hint?.trim() ||
    item.reviewed_at?.trim() ||
    item.outstanding_md?.trim() ||
    item.per_criterion?.length ||
    item.checks?.length,
  )
  if (!hasStatusData) return undefined

  return {
    found: item.found ?? true,
    verdict: item.verdict?.trim() || undefined,
    hint: item.hint?.trim() || undefined,
    confidence: typeof item.confidence === "number" ? item.confidence : undefined,
    reviewedAt: item.reviewed_at?.trim() || undefined,
    outstandingMd: item.outstanding_md?.trim() || undefined,
    criteria: (item.per_criterion ?? []).map((criterion, index) => ({
      id: criterion.criterion_id?.trim() || `criterion-${index + 1}`,
      text: criterion.text?.trim() || "Untitled criterion",
      verdict: criterion.verdict?.trim() || "unknown",
      why: criterion.why?.trim() || undefined,
    })),
    checks: (item.checks ?? []).map((check, index) => ({
      name: check.name?.trim() || `check-${index + 1}`,
      status: check.status?.trim() || "unknown",
      detail: check.detail?.trim() || undefined,
    })),
  }
}

export function mapTrackerLink(item: TrackerLinkDTO): TrackerLinkSummary | null {
  const identifier = item.identifier?.trim()
  const url = item.url?.trim()
  if (!identifier || !url) return null

  return {
    lane: item.lane,
    identifier,
    url,
    state: item.state || "unknown",
    trackerState: item.tracker_state,
  }
}

export function mapFeature(item: FeatureDTO): FeatureContextSummary | null {
  const id = item.id?.trim() || item.key?.trim()
  if (!id) return null

  return {
    id,
    key: item.key?.trim() || id,
    name: item.name?.trim() || item.key?.trim() || id,
    status: item.status || "unknown",
    version: item.version,
    summary: item.summary,
    summaryMd: item.summary_md,
    canonicalArtifactId: item.canonical_artifact_id,
    summarySourceVersion: item.summary_source_version,
  }
}

function mapContextPackWarning(item: ContextPackWarningDTO, index: number): ContextPackWarningSummary {
  const code = item.code || "context_pack_warning"

  return {
    id: `${code}-${item.artifact_id || "pack"}-${index}`,
    code,
    message: item.message || "Context Pack warning recorded.",
    artifactId: item.artifact_id,
  }
}

function mapContextPackProvenance(item: ContextPackProvenanceDTO, index: number): ContextPackProvenanceSummary {
  const id = item.document_id || item.knowledge_store_uri || `knowledge-${index + 1}`

  return {
    id,
    title: item.title || id,
    version: item.version,
    documentType: item.document_type,
    authorityLevel: item.authority_level,
    freshness: item.freshness,
    knowledgeStoreUri: item.knowledge_store_uri,
  }
}

function mapContextPackResult(item: ContextPackResultDTO): CanonicalContextPackSummary | undefined {
  if (item.state !== "assembled" || !item.markdown?.trim()) return undefined

  return {
    state: item.state,
    markdown: item.markdown,
    uri: item.context_pack_uri,
    changeRequestId: item.change_request_id,
    featureId: item.feature_id,
    sourceArtifactId: item.source_artifact_id,
    contextPackArtifactId: item.context_pack_artifact_id,
    governanceLevel: item.governance_level,
    warnings: (item.warnings ?? []).map((warning, index) => mapContextPackWarning(warning, index)),
    knowledgeProvenance: (item.knowledge_provenance ?? []).map((row, index) => mapContextPackProvenance(row, index)),
  }
}

function emptyRegistryDetail(status: WorkItemDetailData["status"]): WorkItemDetailData {
  return {
    acceptanceCriteria: [],
    nextActions: [],
    gateRuns: [],
    staleWarnings: [],
    trackerLinks: [],
    readback: readbackStatusForDetail(status),
    status,
    source: "registry",
  }
}

function readbackStatusForDetail(status: WorkItemDetailData["status"]): WorkItemReadbackStatus {
  return {
    acceptance: status,
    nextActions: status,
    gateRuns: status,
    trackerLinks: status,
    feature: status,
    policy: status,
    delivery: status,
    contextPack: status,
  }
}

function buildLanes(items: WorkItem[]): Lane[] {
  const laneOrder = ["Intake", "Draft", "Review", "Ready", "Handoff"]

  return laneOrder
    .map((title) => {
      const laneItems = items.filter((item) => item.lifecycle === title)
      return {
        title,
        count: laneItems.length,
        tone: title === "Handoff" || title === "Ready" ? "success" : title === "Review" ? "warning" : "neutral",
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
    { label: "Ready for handoff", value: String(ready), detail: "handoff candidates", tone: ready > 0 ? "success" : "neutral" },
    { label: "Open gate failures", value: String(gateDebt), detail: "needs attention", tone: gateDebt > 0 ? "warning" : "success" },
    { label: "Blocked by ambiguity", value: String(blocked), detail: "open blockers", tone: blocked > 0 ? "danger" : "success" },
  ]
}

function buildContextPacks(items: WorkItem[]): ContextPackSummary[] {
  return items
    .filter((item) => item.contextPackId)
    .map((item) => ({
      id: item.contextPackId!,
      title: item.title,
      uri: contextPackUriForWorkItem(item),
      evidence: "Context Pack artifact linked in Doc Registry",
    }))
}

function buildRegistryView(items: WorkItem[]): WorkboardView {
  return {
    workItems: items,
    lanes: buildLanes(items),
    signals: buildSignals(items),
    contextPacks: buildContextPacks(items),
    source: "registry",
    status: "ready",
  }
}

function workTypeForRoute(route: WorkItem["route"]) {
  return route === "quick" ? "cleanup" : "new_feature"
}

export function buildChangeRequestsPath(workspaceId?: string): string {
  const trimmed = workspaceId?.trim()
  if (!trimmed) return "/workboard/change-requests"
  return `/workboard/change-requests?${new URLSearchParams({ workspace_id: trimmed }).toString()}`
}

async function fetchSelectedWorkspaceId(baseUrl: string, signal: AbortSignal): Promise<string | null> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/api/v1/workspaces`, { signal })
  if (!response.ok) return null

  const payload = (await response.json()) as ListResponse<WorkspaceDTO>
  const workspace = payload.items?.find((item) => item.id?.trim())
  return workspace?.id?.trim() || null
}

async function fetchWorkboard(baseUrl: string, signal: AbortSignal, selectedWorkspaceId?: string): Promise<WorkboardView> {
  const workspaceId = selectedWorkspaceId?.trim() || await fetchSelectedWorkspaceId(baseUrl, signal).catch(() => null)
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}${buildChangeRequestsPath(workspaceId ?? undefined)}`, { signal })
  if (!response.ok) {
    throw new Error(`workboard request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListChangeRequestsResponse
  const items = (payload.items ?? []).map((item) => mapChangeRequestToWorkItem(item))
  return buildRegistryView(items)
}

async function postJSON<T>(baseUrl: string, path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal,
  })
  if (!response.ok) {
    throw new Error(`${path} request failed: ${response.status}`)
  }

  return (await response.json()) as T
}

async function patchJSON<T>(baseUrl: string, path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}${path}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal,
  })
  if (!response.ok) {
    throw new Error(`${path} request failed: ${response.status}`)
  }

  return (await response.json()) as T
}

export async function classifyWorkItemRoute(item: WorkItem, signal?: AbortSignal): Promise<RouteClassification | null> {
  const baseUrl = import.meta.env.VITE_LANGGRAPH_API_URL as string | undefined
  if (!baseUrl) return null

  const id = encodeURIComponent(item.registryId || item.key)
  const result = await postJSON<{
    change_request_id?: string
    route?: WorkItem["route"]
    confidence?: number
    rationale?: string
  }>(baseUrl, `/workboard/change-requests/${id}/classify-route`, {}, signal)

  return {
    changeRequestId: result.change_request_id,
    route: result.route === "quick" ? "quick" : "full",
    confidence: typeof result.confidence === "number" ? result.confidence : 0,
    rationale: result.rationale || "No rationale returned.",
  }
}

export async function updateWorkItemRoute(
  item: WorkItem,
  route: WorkItem["route"],
  signal?: AbortSignal,
): Promise<CreateWorkResult | null> {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  if (!baseUrl) return null

  const id = encodeURIComponent(item.registryId || item.key)
  const result = await patchJSON<ChangeRequestDTO>(baseUrl, `/workboard/change-requests/${id}`, {
    work_type: workTypeForRoute(route),
  }, signal)

  return {
    item: mapChangeRequestToWorkItem(result),
    persisted: true,
  }
}

export async function createQuickContextPack(item: WorkItem, signal?: AbortSignal): Promise<CreateWorkResult | null> {
  const baseUrl = import.meta.env.VITE_LANGGRAPH_API_URL as string | undefined
  if (!baseUrl) return null

  const id = encodeURIComponent(item.registryId || item.key)
  const result = await postJSON<GenerateContextPackResponse>(
    baseUrl,
    `/workboard/change-requests/${id}/context-pack`,
    {
      quick_mode: true,
      source_evidence: item.summary,
    },
    signal,
  )

  if (result.change_request) {
    return {
      item: mapChangeRequestToWorkItem(result.change_request),
      persisted: true,
    }
  }

  throw new Error("Context Pack response did not include the updated work item; no browser-local handoff state was created.")
}

async function fetchList<T>(baseUrl: string, path: string, signal: AbortSignal): Promise<T[]> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}${path}`, { signal })
  if (!response.ok) {
    throw new Error(`${path} request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListResponse<T>
  return payload.items ?? []
}

export async function fetchWorkItemDetail(baseUrl: string, item: WorkItem, signal: AbortSignal): Promise<WorkItemDetailData> {
  const id = encodeURIComponent(item.registryId || item.key)
  const featureId = item.featureId ? encodeURIComponent(item.featureId) : ""
  const staleWarningPath = `/workboard/stale-warnings?${new URLSearchParams({ change_request_id: item.registryId || item.key }).toString()}`
  const [acceptance, nextActions, gateRuns, staleWarnings, trackerLinks, policy, deliveryStatus, feature, contextPack] = await Promise.all([
    fetchList<AcceptanceCriterionDTO>(baseUrl, `/workboard/change-requests/${id}/acceptance-criteria`, signal)
      .then((items) => ({ items, status: "ready" as const }))
      .catch(() => ({ items: [] satisfies AcceptanceCriterionDTO[], status: "error" as const })),
    fetchList<NextActionDTO>(baseUrl, `/workboard/change-requests/${id}/next-actions`, signal)
      .then((items) => ({ items, status: "ready" as const }))
      .catch(() => ({ items: [] satisfies NextActionDTO[], status: "error" as const })),
    fetchList<GateRunDTO>(baseUrl, `/workboard/change-requests/${id}/gate-runs?limit=10`, signal)
      .then((items) => ({ items, status: "ready" as const }))
      .catch(() => ({ items: [] satisfies GateRunDTO[], status: "error" as const })),
    fetchList<StaleWarningDTO>(baseUrl, staleWarningPath, signal).catch(
      () => [] satisfies StaleWarningDTO[],
    ),
    fetchList<TrackerLinkDTO>(baseUrl, `/workboard/change-requests/${id}/tracker-links`, signal)
      .then((items) => ({ items, status: "ready" as const }))
      .catch(() => ({ items: [] satisfies TrackerLinkDTO[], status: "error" as const })),
    fetch(`${baseUrl.replace(/\/$/, "")}/api/v1/work-items/${id}/policy`, { signal })
      .then((response) => {
        if (!response.ok) throw new Error(`work item policy request failed: ${response.status}`)
        return response.json() as Promise<GovernancePolicyDTO>
      })
      .then(mapGovernancePolicy)
      .then((item) => ({ item, status: "ready" as const }))
      .catch(() => ({ item: undefined, status: "error" as const })),
    fetch(`${baseUrl.replace(/\/$/, "")}/api/v1/work-items/${id}/delivery-status?detail=true`, { signal })
      .then((response) => {
        if (!response.ok) throw new Error(`delivery status request failed: ${response.status}`)
        return response.json() as Promise<DeliveryStatusDTO>
      })
      .then(mapDeliveryStatus)
      .then((item) => ({ item, status: "ready" as const }))
      .catch(() => ({ item: undefined, status: "error" as const })),
    featureId
      ? fetch(`${baseUrl.replace(/\/$/, "")}/workboard/features/${featureId}`, { signal })
        .then((response) => {
          if (!response.ok) throw new Error(`feature request failed: ${response.status}`)
          return response.json() as Promise<FeatureDTO>
        })
        .then(mapFeature)
        .then((item) => ({ item: item ?? undefined, status: "ready" as const }))
        .catch(() => ({ item: undefined, status: "error" as const }))
      : Promise.resolve({ item: undefined, status: "ready" as const }),
    item.contextPackId
      ? fetch(`${baseUrl.replace(/\/$/, "")}/api/v1/work-items/${id}/context-pack`, { signal })
        .then((response) => {
          if (!response.ok) throw new Error(`context pack request failed: ${response.status}`)
          return response.json() as Promise<ContextPackResultDTO>
        })
        .then(mapContextPackResult)
        .then((item) => (item ? { item, status: "ready" as const } : { item: undefined, status: "error" as const }))
        .catch(() => ({ item: undefined, status: "error" as const }))
      : Promise.resolve({ item: undefined, status: "ready" as const }),
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
    policy: policy.item,
    deliveryStatus: deliveryStatus.item,
    feature: feature.item,
    contextPack: contextPack.item,
    readback: {
      acceptance: acceptance.status,
      nextActions: nextActions.status,
      gateRuns: gateRuns.status,
      trackerLinks: trackerLinks.status,
      feature: feature.status,
      policy: policy.status,
      delivery: deliveryStatus.status,
      contextPack: contextPack.status,
    },
    status: "ready",
    source: "registry",
  }
}

export function useWorkboardData(selectedWorkspaceId?: string): WorkboardView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<WorkboardView>(() => (baseUrl ? { ...emptyRegistryView, status: "loading" } : emptyRegistryView))

  useEffect(() => {
    if (!baseUrl) {
      setView(emptyRegistryView)
      return
    }

    const controller = new AbortController()
    setView({ ...emptyRegistryView, status: "loading" })

    void fetchWorkboard(baseUrl, controller.signal, selectedWorkspaceId)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView({ ...emptyRegistryView, status: "error" })
      })

    return () => controller.abort()
  }, [baseUrl, selectedWorkspaceId])

  return useMemo(() => view, [view])
}

export function useWorkItemDetail(item: WorkItem, enabled: boolean): WorkItemDetailData {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [detail, setDetail] = useState<WorkItemDetailData>(() => (baseUrl ? emptyRegistryDetail("loading") : emptyRegistryDetail("ready")))

  useEffect(() => {
    if (!baseUrl) {
      setDetail(emptyRegistryDetail("ready"))
      return
    }
    if (!enabled) {
      setDetail(emptyRegistryDetail("ready"))
      return
    }

    const controller = new AbortController()
    setDetail(emptyRegistryDetail("loading"))

    void fetchWorkItemDetail(baseUrl, item, controller.signal)
      .then((registryDetail) => setDetail(registryDetail))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setDetail(emptyRegistryDetail("error"))
      })

    return () => controller.abort()
  }, [baseUrl, enabled, item])

  return useMemo(() => detail, [detail])
}
