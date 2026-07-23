
import { createRegistryClient } from "@/api/client"
import type { components } from "@/api/schema"
import {
  type Lane,
  type Signal,
  type WorkItem,
} from "@/data/workspace"
import { formatDateTime } from "@/lib/format"

type ChangeRequestDTO = Partial<components["schemas"]["ChangeRequest"]>

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

export type GovernancePolicyDTO = {
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
  trust_tier?: string
  verification_binding?: string
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
  assurance_sources?: string[] | null
  evidence_verdict?: string
  reason_code?: string
  hint?: string
  confidence?: number
  reviewed_at?: string
  outstanding_md?: string
  judge_model?: string
  executor?: string
  actor?: string
  note?: string
  summary?: string
  git_receipt?: {
    availability?: string
    base_revision?: string
    branch?: string
    changed_files?: string[] | null
    diff_digest?: string
    head_revision?: string
    repository?: string
    warnings?: string[] | null
  }
  peer_review?: {
    agent_name?: string
    reviewed_at?: string
    state?: string
  }
  per_criterion?: DeliveryCriterionDTO[]
  checks?: DeliveryCheckDTO[]
}

type TrackerLinkDTO = {
  identifier?: string
  url?: string
  state?: string
  tracker_state?: string
}

type DeliveryLinkDTO = {
  external_key?: string
  title?: string
  url?: string
  state?: string
  source_branch?: string
  target_branch?: string
  head_sha?: string
  merge_commit_sha?: string
  updated_at?: string
}

type FeatureDTO = {
  id?: string
  key?: string
  name?: string
  summary?: string
  status?: string
  version?: number
  canonical_artifact_id?: string
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
  change_request_id?: string
  feature_id?: string
  source_artifact_id?: string
  artifact_id?: string
  kind?: string
  governance_level?: string
}

type ListResponse<T> = {
  items?: T[]
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
  identifier: string
  url: string
  state: string
  trackerState?: string
}

export type DeliveryLinkSummary = {
  externalKey: string
  title: string
  url: string
  state: string
  sourceBranch?: string
  targetBranch?: string
  headSha?: string
  mergeCommitSha?: string
  updatedAt?: string
}

export type RepositoryObservation = "open" | "exact" | "stale" | "missing-receipt" | "closed"

type GovernancePolicyLineageSummary = {
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

type DeliveryCriterionSummary = {
  id: string
  text: string
  verdict: string
  why?: string
  trustTier?: string
  verificationBinding?: string
}

type DeliveryCheckSummary = {
  name: string
  status: string
  detail?: string
}

export type DeliveryStatusSummary = {
  found: boolean
  verdict?: string
  assuranceSources?: string[]
  evidenceVerdict?: string
  reasonCode?: string
  hint?: string
  confidence?: number
  reviewedAt?: string
  outstandingMd?: string
  judgeModel?: string
  executor?: string
  actor?: string
  note?: string
  summary?: string
  gitReceipt?: {
    availability?: string
    baseRevision?: string
    branch?: string
    changedFiles: string[]
    diffDigest?: string
    headRevision?: string
    repository?: string
    warnings: string[]
  }
  peerReview?: {
    agentName?: string
    reviewedAt?: string
    state?: string
  }
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
  canonicalArtifactId?: string
}

type ContextPackWarningSummary = {
  id: string
  code: string
  message: string
  artifactId?: string
}

type ContextPackProvenanceSummary = {
  id: string
  title: string
  version?: string
  documentType?: string
  authorityLevel?: string
  freshness?: string
  knowledgeStoreUri?: string
}

type ContextPackSummary = {
  state: string
  markdown: string
  changeRequestId?: string
  featureId?: string
  sourceArtifactId?: string
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
  deliveryLinks: DeliveryLinkSummary[]
  policy?: GovernancePolicySummary
  deliveryStatus?: DeliveryStatusSummary
  feature?: FeatureContextSummary
  contextPack?: ContextPackSummary
  readback: WorkItemReadbackStatus
  status: "ready" | "loading" | "error"
  source: "registry"
}

type WorkItemReadbackStatus = {
  acceptance: "ready" | "loading" | "error"
  nextActions: "ready" | "loading" | "error"
  gateRuns: "ready" | "loading" | "error"
  trackerLinks: "ready" | "loading" | "error"
  deliveryLinks: "ready" | "loading" | "error"
  feature: "ready" | "loading" | "error"
  policy: "ready" | "loading" | "error"
  delivery: "ready" | "loading" | "error"
  contextPack: "ready" | "loading" | "error"
}

export type WorkboardView = {
  workItems: WorkItem[]
  lanes: Lane[]
  signals: Signal[]
  source: "registry"
  status: "ready" | "loading" | "error"
}

export const emptyRegistryView: WorkboardView = {
  workItems: [],
  lanes: [],
  signals: [
    { label: "Ready for pickup", value: "0", detail: "approved source available", tone: "neutral" },
    { label: "Open gate failures", value: "0", detail: "needs attention", tone: "success" },
    { label: "Blocked by ambiguity", value: "0", detail: "open blockers", tone: "success" },
  ],
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
  if (item.lead_artifact_id) return "Ready"
  return "Intake"
}

function normalizedPhase(phase: string | undefined): string {
  return phase?.trim().toLowerCase() || ""
}

// Server-derived phase for change requests whose current completion received
// explicit human acceptance. Additive: servers that omit it keep the previous
// grouping.
function isDeliveredPhase(phase: string | undefined): boolean {
  return normalizedPhase(phase) === "delivered"
}

function isReadyPhase(phase: string | undefined): boolean {
  return normalizedPhase(phase) === "ready"
}

function deliveryForChangeRequest(item: ChangeRequestDTO): WorkItem["delivery"] {
  if (isDeliveredPhase(item.phase)) return "accepted"
  const verdict = item.delivery_review?.verdict?.trim()
  if (verdict === "pass") return "passed"
  if (verdict === "fail" || verdict === "needs_human_review" || verdict === "needs_changes") return "needs_changes"
  if (isReadyPhase(item.phase)) return "ready"
  if (item.phase === "Review") return "needs_changes"
  return "not_started"
}

function gateForChangeRequest(item: ChangeRequestDTO): WorkItem["gate"] {
  const deliveryVerdict = item.delivery_review?.verdict?.trim()
  if (deliveryVerdict === "pass") return "pass"
  if (deliveryVerdict === "fail" || deliveryVerdict === "needs_human_review" || deliveryVerdict === "needs_changes") return "fail"
  if (isDeliveredPhase(item.phase)) return "pass"
  if (isReadyPhase(item.phase)) return "pass"
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

  return {
    registryId: item.id,
    featureId: item.feature_id,
    leadArtifactId: item.lead_artifact_id,
    key,
    title: item.title || "Untitled work item",
    route: routeForWorkType(item.work_type),
    createdBy: item.created_by || "Unknown",
    agent: "Governance",
    lifecycle,
    status: item.tracker_status || lifecycle.toLowerCase(),
    gate: gateForChangeRequest(item),
    delivery: deliveryForChangeRequest(item),
    deliveryVerdict: item.delivery_review?.verdict?.trim() || undefined,
    deliveryHint: item.delivery_review?.hint?.trim() || undefined,
    blocker: isDeliveredPhase(item.phase) || isReadyPhase(item.phase) ? "none" : "needs governance progress",
    age: formatDateTime(item.created_at ?? item.updated_at),
    updated: formatDateTime(item.updated_at),
    skills: [],
    summary: summaryFromIntent(item.intent_md),
    acceptance: parseStringList(item.acceptance_criteria_json),
    activity: [
      item.lead_artifact_id
        ? "Lead artifact linked"
        : item.work_type === "bug_fix"
          ? "Quick route uses persisted work as the handoff source"
          : "Waiting for lead artifact",
      item.lead_artifact_id
        ? "Context Pack derives on demand from the approved artifact"
        : item.work_type === "bug_fix"
          ? "Quick-route Context Pack derives on demand from persisted work"
          : "Context Pack becomes available from an approved artifact",
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

function mapNextAction(item: NextActionDTO, index: number): NextActionSummary {
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

export function mapDeliveryStatus(item: DeliveryStatusDTO | undefined): DeliveryStatusSummary | undefined {
  if (!item) return undefined
  if (item.found === false) {
    return { found: false, assuranceSources: [], criteria: [], checks: [] }
  }
  const hasStatusData = Boolean(
    item.verdict?.trim() ||
    item.assurance_sources?.length ||
    item.evidence_verdict?.trim() ||
    item.reason_code?.trim() ||
    item.hint?.trim() ||
    item.reviewed_at?.trim() ||
    item.outstanding_md?.trim() ||
    item.judge_model?.trim() ||
    item.executor?.trim() ||
    item.note?.trim() ||
    item.git_receipt?.head_revision?.trim() ||
    item.peer_review?.state?.trim() ||
    item.per_criterion?.length ||
    item.checks?.length,
  )
  if (!hasStatusData) return undefined

  return {
    found: item.found ?? true,
    verdict: item.verdict?.trim() || undefined,
    assuranceSources: (item.assurance_sources ?? []).map((source) => source.trim()).filter(Boolean),
    evidenceVerdict: item.evidence_verdict?.trim() || undefined,
    reasonCode: item.reason_code?.trim() || undefined,
    hint: item.hint?.trim() || undefined,
    confidence: typeof item.confidence === "number" ? item.confidence : undefined,
    reviewedAt: item.reviewed_at?.trim() || undefined,
    outstandingMd: item.outstanding_md?.trim() || undefined,
    judgeModel: item.judge_model?.trim() || undefined,
    executor: item.executor?.trim() || undefined,
    actor: item.actor?.trim() || undefined,
    note: item.note?.trim() || undefined,
    summary: item.summary?.trim() || undefined,
    gitReceipt: item.git_receipt
      ? {
          availability: item.git_receipt.availability?.trim() || undefined,
          baseRevision: item.git_receipt.base_revision?.trim() || undefined,
          branch: item.git_receipt.branch?.trim() || undefined,
          changedFiles: item.git_receipt.changed_files ?? [],
          diffDigest: item.git_receipt.diff_digest?.trim() || undefined,
          headRevision: item.git_receipt.head_revision?.trim() || undefined,
          repository: item.git_receipt.repository?.trim() || undefined,
          warnings: item.git_receipt.warnings ?? [],
        }
      : undefined,
    peerReview: item.peer_review
      ? {
          agentName: item.peer_review.agent_name?.trim() || undefined,
          reviewedAt: item.peer_review.reviewed_at?.trim() || undefined,
          state: item.peer_review.state?.trim() || undefined,
        }
      : undefined,
    criteria: (item.per_criterion ?? []).map((criterion, index) => ({
      id: criterion.criterion_id?.trim() || `criterion-${index + 1}`,
      text: criterion.text?.trim() || "Untitled criterion",
      verdict: criterion.verdict?.trim() || "unknown",
      why: criterion.why?.trim() || undefined,
      trustTier: criterion.trust_tier?.trim() || undefined,
      verificationBinding: criterion.verification_binding?.trim() || undefined,
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
    identifier,
    url,
    state: item.state || "unknown",
    trackerState: item.tracker_state,
  }
}

export function mapDeliveryLink(item: DeliveryLinkDTO): DeliveryLinkSummary | null {
  const externalKey = item.external_key?.trim()
  const url = item.url?.trim()
  if (!externalKey || !url) return null

  return {
    externalKey,
    title: item.title?.trim() || externalKey,
    url,
    state: item.state?.trim() || "unknown",
    sourceBranch: item.source_branch?.trim() || undefined,
    targetBranch: item.target_branch?.trim() || undefined,
    headSha: item.head_sha?.trim() || undefined,
    mergeCommitSha: item.merge_commit_sha?.trim() || undefined,
    updatedAt: item.updated_at?.trim() || undefined,
  }
}

export function repositoryObservation(link: DeliveryLinkSummary, latestCompletionHead?: string): RepositoryObservation {
  const state = link.state.trim().toLowerCase()
  if (state === "opened" || state === "open") return "open"
  if (state === "closed") return "closed"
  if (!latestCompletionHead?.trim()) return "missing-receipt"
  return link.headSha?.trim().toLowerCase() === latestCompletionHead.trim().toLowerCase() ? "exact" : "stale"
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
    canonicalArtifactId: item.canonical_artifact_id,
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

function mapContextPackResult(item: ContextPackResultDTO): ContextPackSummary | undefined {
  if (!item.state) return undefined

  return {
    state: item.state,
    markdown: item.markdown || "",
    changeRequestId: item.change_request_id,
    featureId: item.feature_id,
    sourceArtifactId: item.source_artifact_id,
    governanceLevel: item.governance_level,
    warnings: (item.warnings ?? []).map((warning, index) => mapContextPackWarning(warning, index)),
    knowledgeProvenance: (item.knowledge_provenance ?? []).map((row, index) => mapContextPackProvenance(row, index)),
  }
}

export function emptyRegistryDetail(status: WorkItemDetailData["status"]): WorkItemDetailData {
  return {
    acceptanceCriteria: [],
    nextActions: [],
    gateRuns: [],
    staleWarnings: [],
    trackerLinks: [],
    deliveryLinks: [],
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
    deliveryLinks: status,
    feature: status,
    policy: status,
    delivery: status,
    contextPack: status,
  }
}

function buildLanes(items: WorkItem[]): Lane[] {
  const laneOrder = ["Intake", "Draft", "Review", "Ready"]

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
    { label: "Open gate failures", value: String(gateDebt), detail: "needs attention", tone: gateDebt > 0 ? "warning" : "success" },
    { label: "Blocked by ambiguity", value: String(blocked), detail: "open blockers", tone: blocked > 0 ? "danger" : "success" },
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

export type WorkboardData = WorkboardView & {
  refresh: () => void
  refreshing: boolean
  lastRefreshedAt?: string
  refreshError?: string
  refreshGeneration: number
}
