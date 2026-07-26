import type { WorkItem } from "@/data/workspace"
import { formatDateTime } from "@/lib/format"
import type {
  AcceptanceCriterionDTO,
  AcceptanceCriterionSummary,
  ChangeRequestDTO,
  ContextPackProvenanceDTO,
  ContextPackProvenanceSummary,
  ContextPackResultDTO,
  ContextPackSummary,
  ContextPackWarningDTO,
  ContextPackWarningSummary,
  DeliveryLinkDTO,
  DeliveryLinkSummary,
  DeliveryStatusDTO,
  DeliveryStatusSummary,
  FeatureContextSummary,
  FeatureDTO,
  GateRunDTO,
  GateRunSummary,
  GovernancePolicyDTO,
  GovernancePolicySummary,
  NextActionDTO,
  NextActionSummary,
  RepositoryObservation,
  StaleWarningDTO,
  StaleWarningSummary,
  TrackerLinkDTO,
  TrackerLinkSummary,
  WorkItemDetailData,
  WorkItemReadbackStatus,
} from "./workboard-types"

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
    gateRunId: item.gate_run_id?.trim() || undefined,
    completionFeedbackEventId: item.completion_feedback_event_id?.trim() || undefined,
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

export function mapContextPackResult(item: ContextPackResultDTO): ContextPackSummary | undefined {
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
