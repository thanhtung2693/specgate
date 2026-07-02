type GovernanceProfileDTO = {
  namespace?: string
  key?: string
  full_key?: string
  version?: string
  display_name?: string
  change_type?: string
  required_roles?: string[]
  required_topics?: string[]
  required_evidence?: string[]
  enabled_gates?: string[]
  source?: string
  approval_policy?: string
  evidence_policy?: string
}

type GovernanceProfileResponse = {
  items?: GovernanceProfileDTO[]
  body?: {
    items?: GovernanceProfileDTO[]
  }
}

type GovernancePolicyLevelDTO = {
  governance_level?: string
  display_name?: string
  approval_policy?: string
  evidence_policy?: string
  required_roles?: string[]
  required_topics?: string[]
  required_evidence?: string[]
  enabled_gates?: string[]
}

type GovernancePolicyLevelResponse = {
  levels?: GovernancePolicyLevelDTO[]
  body?: {
    levels?: GovernancePolicyLevelDTO[]
  }
}

type GovernanceGateHealthDTO = {
  gate_key?: string
  override_count?: number
}

type GovernancePolicyHealthDTO = {
  policy_id?: string
  total_feedback?: number
  override_count?: number
  rejected_evidence_count?: number
  post_merge_rollback_count?: number
  escaped_defect_count?: number
  gate_breakdown?: GovernanceGateHealthDTO[]
}

type GovernancePolicyHealthResponse = {
  policies?: GovernancePolicyHealthDTO[]
  body?: {
    policies?: GovernancePolicyHealthDTO[]
  }
}

type GovernanceOutcomeFeedbackDTO = {
  id?: string
  work_item_id?: string
  artifact_id?: string
  policy_id?: string
  type?: string
  gate_key?: string
  reason?: string
  actor?: string
  recorded_at?: string
}

type GovernanceOutcomeFeedbackResponse = {
  items?: GovernanceOutcomeFeedbackDTO[]
  body?: {
    items?: GovernanceOutcomeFeedbackDTO[]
  }
}

export type GovernanceProfileSummary = {
  id: string
  displayName: string
  changeType: string
  version: string
  source: string
  approvalPolicy: string
  evidencePolicy: string
  requiredRoles: string[]
  requiredTopics: string[]
  requiredEvidence: string[]
  enabledGates: string[]
}

export type GovernancePolicyLevelSummary = {
  level: string
  displayName: string
  approvalPolicy: string
  evidencePolicy: string
  requiredRoles: string[]
  requiredTopics: string[]
  requiredEvidence: string[]
  enabledGates: string[]
}

export type GovernanceGateHealthSummary = {
  gateKey: string
  overrideCount: number
}

export type GovernancePolicyHealthSummary = {
  policyId: string
  totalFeedback: number
  overrideCount: number
  rejectedEvidenceCount: number
  postMergeRollbackCount: number
  escapedDefectCount: number
  gateBreakdown: GovernanceGateHealthSummary[]
}

export type GovernanceOutcomeFeedbackSummary = {
  id: string
  workItemId: string
  artifactId?: string
  policyId?: string
  type: string
  gateKey?: string
  reason?: string
  actor?: string
  recordedAt?: string
}

export type GovernanceCatalog = {
  profiles: GovernanceProfileSummary[]
  policyLevels: GovernancePolicyLevelSummary[]
  policyHealth: GovernancePolicyHealthSummary[]
  outcomeFeedback: GovernanceOutcomeFeedbackSummary[]
}

function mapProfile(profile: GovernanceProfileDTO): GovernanceProfileSummary | null {
  const id = profile.full_key?.trim() || [profile.namespace?.trim(), profile.key?.trim()].filter(Boolean).join("/")
  if (!id) return null

  return {
    id,
    displayName: profile.display_name || profile.key || id,
    changeType: profile.change_type || "general",
    version: profile.version || "unknown",
    source: profile.source || "unknown",
    approvalPolicy: profile.approval_policy || "unknown",
    evidencePolicy: profile.evidence_policy || "unknown",
    requiredRoles: profile.required_roles ?? [],
    requiredTopics: profile.required_topics ?? [],
    requiredEvidence: profile.required_evidence ?? [],
    enabledGates: profile.enabled_gates ?? [],
  }
}

function mapPolicyLevel(level: GovernancePolicyLevelDTO): GovernancePolicyLevelSummary | null {
  const id = level.governance_level?.trim()
  if (!id) return null

  return {
    level: id,
    displayName: level.display_name || id,
    approvalPolicy: level.approval_policy || "unknown",
    evidencePolicy: level.evidence_policy || "unknown",
    requiredRoles: level.required_roles ?? [],
    requiredTopics: level.required_topics ?? [],
    requiredEvidence: level.required_evidence ?? [],
    enabledGates: level.enabled_gates ?? [],
  }
}

function mapPolicyHealth(policy: GovernancePolicyHealthDTO, index: number): GovernancePolicyHealthSummary {
  return {
    policyId: policy.policy_id || "unscoped",
    totalFeedback: policy.total_feedback ?? 0,
    overrideCount: policy.override_count ?? 0,
    rejectedEvidenceCount: policy.rejected_evidence_count ?? 0,
    postMergeRollbackCount: policy.post_merge_rollback_count ?? 0,
    escapedDefectCount: policy.escaped_defect_count ?? 0,
    gateBreakdown: (policy.gate_breakdown ?? []).map((gate, gateIndex) => ({
      gateKey: gate.gate_key || `gate-${index + 1}-${gateIndex + 1}`,
      overrideCount: gate.override_count ?? 0,
    })),
  }
}

function mapOutcomeFeedback(item: GovernanceOutcomeFeedbackDTO): GovernanceOutcomeFeedbackSummary | null {
  const id = item.id?.trim()
  const workItemId = item.work_item_id?.trim()
  if (!id || !workItemId) return null

  return {
    id,
    workItemId,
    artifactId: item.artifact_id,
    policyId: item.policy_id,
    type: item.type || "unknown",
    gateKey: item.gate_key,
    reason: item.reason,
    actor: item.actor,
    recordedAt: item.recorded_at,
  }
}

async function fetchOptionalJSON<T>(url: string, signal?: AbortSignal): Promise<T | null> {
  const response = await fetch(url, { signal }).catch(() => null)
  if (!response?.ok) return null
  return response.json() as Promise<T>
}

export async function loadGovernanceCatalog(baseUrl: string, signal?: AbortSignal): Promise<GovernanceCatalog> {
  const base = baseUrl.replace(/\/$/, "")
  const [profilesResponse, levelsResponse, policyHealthPayload, outcomeFeedbackPayload] = await Promise.all([
    fetch(`${base}/governance-profiles`, { signal }),
    fetch(`${base}/api/v1/policies/levels`, { signal }),
    fetchOptionalJSON<GovernancePolicyHealthResponse>(`${base}/api/v1/policy-health`, signal),
    fetchOptionalJSON<GovernanceOutcomeFeedbackResponse>(`${base}/api/v1/outcome-feedback`, signal),
  ])

  if (!profilesResponse.ok) throw new Error(`governance profiles request failed: ${profilesResponse.status}`)
  if (!levelsResponse.ok) throw new Error(`policy levels request failed: ${levelsResponse.status}`)

  const profilesPayload = (await profilesResponse.json()) as GovernanceProfileResponse
  const levelsPayload = (await levelsResponse.json()) as GovernancePolicyLevelResponse
  const profiles = profilesPayload.items ?? profilesPayload.body?.items ?? []
  const levels = levelsPayload.levels ?? levelsPayload.body?.levels ?? []
  const policyHealth = policyHealthPayload?.policies ?? policyHealthPayload?.body?.policies ?? []
  const outcomeFeedback = outcomeFeedbackPayload?.items ?? outcomeFeedbackPayload?.body?.items ?? []

  return {
    profiles: profiles.flatMap((profile) => {
      const mapped = mapProfile(profile)
      return mapped ? [mapped] : []
    }),
    policyLevels: levels.flatMap((level) => {
      const mapped = mapPolicyLevel(level)
      return mapped ? [mapped] : []
    }),
    policyHealth: policyHealth.map((policy, index) => mapPolicyHealth(policy, index)),
    outcomeFeedback: outcomeFeedback.flatMap((item) => {
      const feedback = mapOutcomeFeedback(item)
      return feedback ? [feedback] : []
    }),
  }
}
