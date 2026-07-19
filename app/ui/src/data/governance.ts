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

export async function loadGovernancePolicyLevels(baseUrl: string, signal?: AbortSignal): Promise<GovernancePolicyLevelSummary[]> {
  const base = baseUrl.replace(/\/$/, "")
  const levelsResponse = await fetch(`${base}/api/v1/policies/levels`, { signal })
  if (!levelsResponse.ok) throw new Error(`policy levels request failed: ${levelsResponse.status}`)
  const levelsPayload = (await levelsResponse.json()) as GovernancePolicyLevelResponse
  const levels = levelsPayload.levels ?? levelsPayload.body?.levels ?? []
  return levels.flatMap((level) => {
    const mapped = mapPolicyLevel(level)
    return mapped ? [mapped] : []
  })
}
