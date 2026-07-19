import { docRegistryBase } from "@/data/model-settings"

export type IntegrationProvider = "github" | "gitlab" | "linear"
type IntegrationStatus = "connected" | "disabled" | "error" | string
type IntegrationAuthMethod = "pat" | "oauth" | ""

export type IntegrationSummary = {
  id: string
  workspace_id?: string
  provider: IntegrationProvider | string
  name: string
  status: IntegrationStatus
  base_url?: string
  config_json?: string
  auth_method?: IntegrationAuthMethod
  has_api_token?: boolean
  has_oauth_token?: boolean
  last_error?: string
  updated_at?: string
}

export type IntegrationResourceSummary = {
  id: string
  workspace_id?: string
  integration_id?: string
  resource_type: string
  external_id?: string
  external_key: string
  display_name?: string
  default_ref?: string
  config_json?: string
  has_webhook_secret?: boolean
  updated_at?: string
}

export type IntegrationWebhookEventSummary = {
  id: string
  integration_id?: string
  resource_id?: string
  provider?: string
  event_type: string
  external_event_id?: string
  correlation_id?: string
  status: string
  error?: string
  received_at?: string
  processed_at?: string
  created_at?: string
  updated_at?: string
}

export type IntegrationResourceCandidate = {
  external_id?: string
  external_key: string
  display_name: string
  default_ref?: string
}

export type IntegrationProviderDef = {
  id: IntegrationProvider
  name: string
  category: "repositories" | "work_tracking"
  scope: string
  defaultName: string
  defaultBaseUrl?: string
  tokenPlaceholder: string
}

type IntegrationListBody = {
  items?: Array<Partial<IntegrationSummary>>
}

type IntegrationResourceListBody = {
  items?: IntegrationResourceSummary[]
}

type IntegrationWebhookEventListBody = {
  items?: IntegrationWebhookEventSummary[]
}

type IntegrationResourceCandidateListBody = {
  items?: Array<Partial<IntegrationResourceCandidate>>
}

type OAuthBeginBody = {
  authorize_url: string
}

export const integrationProviders: IntegrationProviderDef[] = [
  {
    id: "github",
    name: "GitHub",
    category: "repositories",
    scope: "Marked pull requests and exact submitted commits.",
    defaultName: "GitHub",
    defaultBaseUrl: "https://github.com",
    tokenPlaceholder: "ghp_...",
  },
  {
    id: "gitlab",
    name: "GitLab",
    category: "repositories",
    scope: "Marked merge requests and exact submitted commits.",
    defaultName: "GitLab",
    defaultBaseUrl: "https://gitlab.com",
    tokenPlaceholder: "glpat-...",
  },
  {
    id: "linear",
    name: "Linear",
    category: "work_tracking",
    scope: "Optional handoff of approved work to a Linear team.",
    defaultName: "Linear",
    tokenPlaceholder: "lin_api_...",
  },
]

export function integrationsBase(): string | null {
  return docRegistryBase()
}

export function providerDefinition(provider: IntegrationProvider | string): IntegrationProviderDef {
  return integrationProviders.find((entry) => entry.id === provider) ?? integrationProviders[0]
}

export async function listIntegrations(base: string, workspaceId: string): Promise<IntegrationSummary[]> {
  const query = workspaceQuery(workspaceId)
  const response = await fetch(`${base}/integrations?${query}`)
  const body = await readJson<IntegrationListBody>(response, "Load integrations failed")
  return normalizeIntegrations(body.items)
}

export async function listIntegrationResources(base: string, workspaceId: string, integrationId: string): Promise<IntegrationResourceSummary[]> {
  const query = workspaceQuery(workspaceId)
  const response = await fetch(`${base}/integrations/${encodeURIComponent(integrationId)}/resources?${query}`)
  const body = await readJson<IntegrationResourceListBody>(response, "Load integration resources failed")
  return body.items ?? []
}

export async function listIntegrationWebhookEvents(
  base: string,
  workspaceId: string,
  integrationId: string,
  limit = 3,
): Promise<IntegrationWebhookEventSummary[]> {
  const query = workspaceQuery(workspaceId, { limit: String(limit) })
  const response = await fetch(`${base}/integrations/${encodeURIComponent(integrationId)}/webhook-events?${query.toString()}`)
  const body = await readJson<IntegrationWebhookEventListBody>(response, "Load integration webhook events failed")
  return body.items ?? []
}

export async function listIntegrationRepos(
  base: string,
  workspaceId: string,
  integrationId: string,
  search = "",
  limit = 50,
): Promise<IntegrationResourceCandidate[]> {
  const query = workspaceQuery(workspaceId, { limit: String(limit) })
  if (search.trim()) query.set("search", search.trim())
  const response = await fetch(`${base}/integrations/${encodeURIComponent(integrationId)}/repos?${query.toString()}`)
  const body = await readJson<IntegrationResourceCandidateListBody>(response, "Load integration repos failed")
  return normalizeResourceCandidates(body.items)
}

export async function listLinearTeams(base: string, workspaceId: string, integrationId: string): Promise<IntegrationResourceCandidate[]> {
  const query = workspaceQuery(workspaceId)
  const response = await fetch(`${base}/integrations/${encodeURIComponent(integrationId)}/linear/teams?${query}`)
  const body = await readJson<IntegrationResourceCandidateListBody>(response, "Load Linear teams failed")
  return normalizeResourceCandidates(body.items)
}

export async function createIntegration(
  base: string,
  workspaceId: string,
  input: {
    provider: IntegrationProvider
    name: string
    base_url?: string
    config_json?: string
  },
): Promise<IntegrationSummary> {
  const response = await fetch(`${base}/integrations`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ...input, workspace_id: requiredWorkspaceId(workspaceId) }),
  })
  return readJson<IntegrationSummary>(response, "Create integration failed")
}

export async function createIntegrationResource(
  base: string,
  workspaceId: string,
  integrationId: string,
  input: {
    resource_type: string
    external_id?: string
    external_key: string
    display_name?: string
    default_ref?: string
  },
): Promise<IntegrationResourceSummary> {
  const query = workspaceQuery(workspaceId)
  const response = await fetch(`${base}/integrations/${encodeURIComponent(integrationId)}/resources?${query}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ...input, workspace_id: requiredWorkspaceId(workspaceId) }),
  })
  return readJson<IntegrationResourceSummary>(response, "Link integration resource failed")
}

export async function handoffToLinear(
  base: string,
  workspaceId: string,
  changeRequestId: string,
  integrationId: string,
  resourceId: string,
): Promise<void> {
  const response = await fetch(
    `${base}/workboard/change-requests/${encodeURIComponent(changeRequestId)}/linear-handoff?${workspaceQuery(workspaceId)}`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ integration_id: integrationId, resource_id: resourceId }),
    },
  )
  await readJson<unknown>(response, "Hand off to Linear failed")
}

export async function setIntegrationApiToken(base: string, workspaceId: string, id: string, apiToken: string): Promise<void> {
  const query = workspaceQuery(workspaceId)
  const response = await fetch(`${base}/integrations/${encodeURIComponent(id)}/api-token?${query}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ api_token: apiToken }),
  })
  if (!response.ok && response.status !== 204) {
    const text = await response.text().catch(() => "")
    throw new Error(`Set API token failed (${response.status})${text ? `: ${text.slice(0, 180)}` : ""}`)
  }
}

export async function beginPendingIntegrationOAuth(
  base: string,
  workspaceId: string,
  input: {
    provider: IntegrationProvider
    name: string
    base_url?: string
    config_json?: string
    redirect_target?: string
  },
): Promise<OAuthBeginBody> {
  const response = await fetch(`${base}/integrations/oauth/begin`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ...input, workspace_id: requiredWorkspaceId(workspaceId) }),
  })
  const body = await readJson<OAuthBeginBody>(response, "Start OAuth failed")
  if (!body.authorize_url?.trim()) {
    throw new Error("Start OAuth failed: provider authorization URL was not returned")
  }
  return body
}

export function oauthRedirectTarget(): string {
  const params = new URLSearchParams(window.location.search)
  params.set("settings", "integrations")
  return `${window.location.pathname}?${params.toString()}`
}

export function integrationConfigJson(provider: IntegrationProvider): string {
  void provider
  return JSON.stringify({ enabled: true })
}

function parseIntegrationResponse<T>(data: unknown): T {
  if (data && typeof data === "object" && "body" in data) {
    return (data as { body?: T }).body as T
  }
  if (data && typeof data === "object" && "Body" in data) {
    return (data as { Body?: T }).Body as T
  }
  return data as T
}

function normalizeResourceCandidates(items: Array<Partial<IntegrationResourceCandidate>> | undefined): IntegrationResourceCandidate[] {
  return (items ?? []).flatMap((item) => {
    const externalKey = item.external_key?.trim()
    if (!externalKey) return []
    const displayName = item.display_name?.trim() || externalKey
    const externalId = item.external_id?.trim()
    const defaultRef = item.default_ref?.trim()
    return [
      {
        external_key: externalKey,
        display_name: displayName,
        ...(externalId ? { external_id: externalId } : {}),
        ...(defaultRef ? { default_ref: defaultRef } : {}),
      },
    ]
  })
}

function normalizeIntegrations(items: Array<Partial<IntegrationSummary>> | undefined): IntegrationSummary[] {
  return (items ?? []).flatMap((item) => {
    const id = item.id?.trim()
    if (!id) return []
    const provider = item.provider?.trim() || "github"
    return [
      {
        id,
        workspace_id: item.workspace_id,
        provider,
        name: item.name?.trim() || providerDefinition(provider).name,
        status: item.status || "disabled",
        base_url: item.base_url,
        config_json: item.config_json,
        auth_method: item.auth_method,
        has_api_token: item.has_api_token,
        has_oauth_token: item.has_oauth_token,
        last_error: item.last_error,
        updated_at: item.updated_at,
      },
    ]
  })
}

function requiredWorkspaceId(workspaceId: string): string {
  const value = workspaceId.trim()
  if (!value) throw new Error("workspaceId is required")
  return value
}

function workspaceQuery(workspaceId: string, extras: Record<string, string> = {}): URLSearchParams {
  return new URLSearchParams({ workspace_id: requiredWorkspaceId(workspaceId), ...extras })
}

async function readJson<T>(response: Response, label: string): Promise<T> {
  if (!response.ok) {
    const text = await response.text().catch(() => "")
    throw new Error(`${label} (${response.status})${text ? `: ${text.slice(0, 180)}` : ""}`)
  }
  return parseIntegrationResponse<T>(await response.json())
}
