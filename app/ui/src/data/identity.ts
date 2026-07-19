export type IdentityWorkspace = {
  id: string
  slug: string
  name: string
}

type IdentityUser = {
  id: string
  username: string
  name: string
  email?: string
}

export type IdentitySelection = {
  workspace: IdentityWorkspace
  user: IdentityUser
}

export type WorkspaceMember = {
  workspaceId: string
  userId: string
  username: string
  displayName: string
  email?: string
  role: string
  current: boolean
}

export type BootstrapIdentityInput = {
  workspaceName: string
  displayName: string
  username: string
  email?: string
}

type WorkspaceDTO = {
  id?: string
  slug?: string
  name?: string
}

type UserDTO = {
  id?: string
  username?: string
  display_name?: string
  name?: string
  email?: string
}

type SelectionDTO = {
  body?: SelectionDTO
  workspace?: WorkspaceDTO
  user?: UserDTO
}

type WorkspaceMemberDTO = {
  workspace_id?: string
  user_id?: string
  username?: string
  display_name?: string
  email?: string
  role?: string
  current?: boolean
}

type ListResponse<T> = {
  body?: {
    items?: T[]
  }
  items?: T[]
}

type WorkspaceMembersResponse = {
  body?: WorkspaceMembersResponse
  members?: WorkspaceMemberDTO[]
}

function normalizeBaseUrl(baseUrl: string) {
  return baseUrl.replace(/\/$/, "")
}

function mapWorkspace(dto: WorkspaceDTO): IdentityWorkspace {
  return {
    id: dto.id?.trim() || dto.slug?.trim() || dto.name?.trim() || "local",
    slug: dto.slug?.trim() || dto.id?.trim() || "local",
    name: dto.name?.trim() || dto.slug?.trim() || "Local workspace",
  }
}

function mapRegistryWorkspace(dto: WorkspaceDTO): IdentityWorkspace | null {
  const id = dto.id?.trim()
  if (!id) {
    return null
  }

  return {
    id,
    slug: dto.slug?.trim() || id,
    name: dto.name?.trim() || dto.slug?.trim() || id,
  }
}

function mapWorkspaceMember(dto: WorkspaceMemberDTO): WorkspaceMember | null {
  const userId = dto.user_id?.trim()
  const username = dto.username?.trim()
  if (!userId || !username) return null

  return {
    workspaceId: dto.workspace_id?.trim() || "",
    userId,
    username,
    displayName: dto.display_name?.trim() || username,
    email: dto.email?.trim() || undefined,
    role: dto.role?.trim() || "member",
    current: Boolean(dto.current),
  }
}

function mapUser(dto: UserDTO): IdentityUser {
  const username = dto.username?.trim() || dto.email?.split("@")[0]?.trim() || "local-user"
  return {
    id: dto.id?.trim() || username,
    username,
    name: dto.display_name?.trim() || dto.name?.trim() || username,
    email: dto.email?.trim() || undefined,
  }
}

function unwrapSelection(dto: SelectionDTO): SelectionDTO {
  return dto.body ? unwrapSelection(dto.body) : dto
}

function unwrapWorkspaceMembers(dto: WorkspaceMembersResponse): WorkspaceMembersResponse {
  return dto.body ? unwrapWorkspaceMembers(dto.body) : dto
}

export async function bootstrapIdentity(baseUrl: string, input: BootstrapIdentityInput): Promise<IdentitySelection> {
  const response = await fetch(`${normalizeBaseUrl(baseUrl)}/api/v1/identity/bootstrap`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      workspace_name: input.workspaceName,
      display_name: input.displayName,
      username: input.username,
      email: input.email,
    }),
  })
  if (!response.ok) {
    throw new Error(`identity bootstrap failed: ${response.status}`)
  }

  const dto = unwrapSelection((await response.json()) as SelectionDTO)
  return {
    workspace: mapWorkspace(dto.workspace ?? {}),
    user: mapUser(dto.user ?? {}),
  }
}

export async function listIdentityWorkspaces(baseUrl: string, signal?: AbortSignal): Promise<IdentityWorkspace[]> {
  const response = await fetch(`${normalizeBaseUrl(baseUrl)}/api/v1/workspaces`, { signal })
  if (!response.ok) {
    throw new Error(`workspaces request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListResponse<WorkspaceDTO>
  return (payload.body?.items ?? payload.items ?? []).flatMap((item) => {
    const workspace = mapRegistryWorkspace(item)
    return workspace ? [workspace] : []
  })
}

export async function listWorkspaceMembers(
  baseUrl: string,
  workspaceId: string,
  currentUser?: { userId?: string; username?: string },
  signal?: AbortSignal,
): Promise<WorkspaceMember[]> {
  const params = new URLSearchParams()
  const userId = currentUser?.userId?.trim()
  const username = currentUser?.username?.trim()
  if (userId) params.set("current_user_id", userId)
  if (username) params.set("current_username", username)
  const query = params.toString()
  const response = await fetch(
    `${normalizeBaseUrl(baseUrl)}/api/v1/workspaces/${encodeURIComponent(workspaceId)}/members${query ? `?${query}` : ""}`,
    { signal },
  )
  if (!response.ok) {
    throw new Error(`workspace members request failed: ${response.status}`)
  }

  const payload = unwrapWorkspaceMembers((await response.json()) as WorkspaceMembersResponse)
  return (payload.members ?? []).flatMap((item) => {
    const member = mapWorkspaceMember(item)
    return member ? [member] : []
  })
}
