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

export type GovernanceCatalog = {
  profiles: GovernanceProfileSummary[]
  policyLevels: GovernancePolicyLevelSummary[]
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

export async function loadGovernanceCatalog(baseUrl: string, signal?: AbortSignal): Promise<GovernanceCatalog> {
  const base = baseUrl.replace(/\/$/, "")
  const [profilesResponse, levelsResponse] = await Promise.all([
    fetch(`${base}/governance-profiles`, { signal }),
    fetch(`${base}/api/v1/policies/levels`, { signal }),
  ])

  if (!profilesResponse.ok) throw new Error(`governance profiles request failed: ${profilesResponse.status}`)
  if (!levelsResponse.ok) throw new Error(`policy levels request failed: ${levelsResponse.status}`)

  const profilesPayload = (await profilesResponse.json()) as GovernanceProfileResponse
  const levelsPayload = (await levelsResponse.json()) as GovernancePolicyLevelResponse
  const profiles = profilesPayload.items ?? profilesPayload.body?.items ?? []
  const levels = levelsPayload.levels ?? levelsPayload.body?.levels ?? []

  return {
    profiles: profiles.flatMap((profile) => {
      const mapped = mapProfile(profile)
      return mapped ? [mapped] : []
    }),
    policyLevels: levels.flatMap((level) => {
      const mapped = mapPolicyLevel(level)
      return mapped ? [mapped] : []
    }),
  }
}
