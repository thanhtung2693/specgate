type RegistrySkillDTO = {
  id?: string
  workspace_id?: string
  name?: string
  description?: string
  prompt?: string
  created_at?: string
  updated_at?: string
}

type RegistrySkillListBody = {
  items?: RegistrySkillDTO[]
}

type RegistrySkillDetailBody = {
  body?: RegistrySkillDTO
}

export type RegistrySkillInput = {
  name: string
  description: string
  prompt: string
}

export type RegistrySkillSummary = {
  id: string
  name: string
  description: string
  updatedAt?: string
}

export type RegistrySkillDetail = RegistrySkillSummary & {
  prompt: string
  createdAt?: string
}

function mapRegistrySkill(skill: RegistrySkillDTO): RegistrySkillSummary | null {
  const id = skill.id?.trim()
  if (!id) return null

  return {
    id,
    name: skill.name?.trim() || id,
    description: skill.description || "No skill description recorded.",
    updatedAt: skill.updated_at || skill.created_at,
  }
}

function mapRegistrySkillDetail(skill: RegistrySkillDTO): RegistrySkillDetail {
  const summary = mapRegistrySkill(skill)
  if (!summary) throw new Error("skill detail response missing registry id")

  return {
    ...summary,
    prompt: skill.prompt || "No Skill prompt recorded.",
    createdAt: skill.created_at,
  }
}

function requiredWorkspaceId(workspaceId: string): string {
  const workspace = workspaceId.trim()
  if (!workspace) throw new Error("workspaceId is required")
  return workspace
}

export async function listRegistrySkills(baseUrl: string, workspaceId: string, signal?: AbortSignal): Promise<RegistrySkillSummary[]> {
  const query = new URLSearchParams({ workspace_id: requiredWorkspaceId(workspaceId) })
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/api/v1/skills?${query}`, { signal })
  if (!response.ok) {
    throw new Error(`skills request failed: ${response.status}`)
  }

  const payload = (await response.json()) as RegistrySkillListBody
  return (payload.items ?? []).flatMap((skill) => {
    const mapped = mapRegistrySkill(skill)
    return mapped ? [mapped] : []
  })
}

export async function getRegistrySkill(baseUrl: string, id: string, workspaceId: string, signal?: AbortSignal): Promise<RegistrySkillDetail> {
  const query = new URLSearchParams({ workspace_id: requiredWorkspaceId(workspaceId) })
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/api/v1/skills/${encodeURIComponent(id)}?${query}`, { signal })
  if (!response.ok) {
    throw new Error(`skill detail request failed: ${response.status}`)
  }

  const payload = (await response.json()) as RegistrySkillDetailBody & RegistrySkillDTO
  return mapRegistrySkillDetail(payload.body ?? payload)
}

async function submitRegistrySkill(
  baseUrl: string,
  path: string,
  method: "POST" | "PUT",
  input: RegistrySkillInput,
  workspaceId: string,
): Promise<RegistrySkillDetail> {
  const workspace = requiredWorkspaceId(workspaceId)
  const query = method === "PUT" ? `?${new URLSearchParams({ workspace_id: workspace })}` : ""
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}${path}${query}`, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(method === "POST" ? { ...input, workspace_id: workspace } : input),
  })
  if (!response.ok) {
    throw new Error(`skill ${method.toLowerCase()} request failed: ${response.status}`)
  }

  const payload = (await response.json()) as RegistrySkillDetailBody & RegistrySkillDTO
  return mapRegistrySkillDetail(payload.body ?? payload)
}

export async function createRegistrySkill(baseUrl: string, workspaceId: string, input: RegistrySkillInput): Promise<RegistrySkillDetail> {
  return submitRegistrySkill(baseUrl, "/skills", "POST", input, workspaceId)
}

export async function updateRegistrySkill(baseUrl: string, id: string, workspaceId: string, input: RegistrySkillInput): Promise<RegistrySkillDetail> {
  return submitRegistrySkill(baseUrl, `/skills/${encodeURIComponent(id)}`, "PUT", input, workspaceId)
}

export async function deleteRegistrySkill(baseUrl: string, id: string, workspaceId: string): Promise<void> {
  const query = new URLSearchParams({ workspace_id: requiredWorkspaceId(workspaceId) })
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/skills/${encodeURIComponent(id)}?${query}`, {
    method: "DELETE",
  })
  if (!response.ok) {
    throw new Error(`skill delete request failed: ${response.status}`)
  }
}
