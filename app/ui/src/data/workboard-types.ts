import type { components } from "@/api/schema"
import type { Lane, Signal, WorkItem } from "@/data/workspace"

export type ChangeRequestDTO = Partial<components["schemas"]["ChangeRequest"]>

export type AcceptanceCriterionDTO = {
  id?: string
  text?: string
  done?: boolean
  source?: string
  verification_binding?: string
}

export type NextActionDTO = {
  gate?: string
  state?: string
  hint?: string
  action_endpoint?: string
}

export type GateRunDTO = {
  id?: string
  gate?: string
  state?: string
  hint?: string
  executor?: string
  action_endpoint?: string
  evidence_json?: string
  created_at?: string
}

export type StaleWarningDTO = {
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

export type DeliveryStatusDTO = {
  change_request_id?: string
  gate_run_id?: string
  completion_feedback_event_id?: string
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

export type TrackerLinkDTO = {
  identifier?: string
  url?: string
  state?: string
  tracker_state?: string
}

export type DeliveryLinkDTO = {
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

export type FeatureDTO = {
  id?: string
  key?: string
  name?: string
  summary?: string
  status?: string
  version?: number
  canonical_artifact_id?: string
}

export type ContextPackWarningDTO = {
  code?: string
  message?: string
  artifact_id?: string
}

export type ContextPackProvenanceDTO = {
  document_id?: string
  title?: string
  version?: string
  document_type?: string
  authority_level?: string
  is_latest?: boolean
  freshness?: string
  knowledge_store_uri?: string
}

export type ContextPackResultDTO = {
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

export type ListResponse<T> = {
  items?: T[]
}

export type AcceptanceCriterionSummary = {
  id: string
  text: string
  done: boolean
  source: string
  // A criterion bound to a check takes its verdict from that check result. An
  // unbound one can only ever carry the implementing agent's claim, so the
  // review surface has to say which is which rather than let absence imply it.
  verificationBinding?: string
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
  gateRunId?: string
  completionFeedbackEventId?: string
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

export type ContextPackSummary = {
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

export type WorkItemReadbackStatus = {
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
    { label: "Checks not passing", value: "0", detail: "needs attention", tone: "success" },
    { label: "Waiting on you", value: "0", detail: "not approved yet", tone: "success" },
  ],
  source: "registry",
  status: "ready",
}

export type WorkboardData = WorkboardView & {
  refresh: () => void
  refreshing: boolean
  lastRefreshedAt?: string
  refreshError?: string
  refreshGeneration: number
}
