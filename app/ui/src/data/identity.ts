export type IdentityWorkspace = {
  id: string
  slug: string
  name: string
}

export type IdentityUser = {
  id: string
  username: string
  name: string
  email?: string
}

export type IdentitySelection = {
  workspace: IdentityWorkspace
  user: IdentityUser
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

type ListResponse<T> = {
  body?: {
    items?: T[]
  }
  items?: T[]
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
