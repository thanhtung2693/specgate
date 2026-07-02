import { docRegistryBase } from "@/data/model-settings"

export type IntegrationProvider = "github" | "gitlab" | "linear"
export type IntegrationStatus = "connected" | "disabled" | "error" | string
export type IntegrationAuthMethod = "pat" | "oauth" | ""

export type IntegrationSummary = {
  id: string
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
    scope: "Repository, pull request, CI, and issue handoff signals.",
    defaultName: "GitHub",
    defaultBaseUrl: "https://github.com",
    tokenPlaceholder: "ghp_...",
  },
  {
    id: "gitlab",
    name: "GitLab",
    scope: "Repository, merge request, CI, and issue handoff signals.",
    defaultName: "GitLab",
    defaultBaseUrl: "https://gitlab.com",
    tokenPlaceholder: "glpat-...",
  },
  {
    id: "linear",
    name: "Linear",
    scope: "Tracker mirror and issue handoff signals.",
    defaultName: "Linear",
    tokenPlaceholder: "lin_api_...",
  },
]

export const linearMcpServerUrlDefault = "https://mcp.linear.app/mcp"

export function integrationsBase(): string | null {
  return docRegistryBase()
}

export function providerDefinition(provider: IntegrationProvider | string): IntegrationProviderDef {
  return integrationProviders.find((entry) => entry.id === provider) ?? integrationProviders[0]
}

export async function listIntegrations(base: string): Promise<IntegrationSummary[]> {
  const response = await fetch(`${base}/integrations`)
  const body = await readJson<IntegrationListBody>(response, "Load integrations failed")
  return normalizeIntegrations(body.items)
}

export async function listIntegrationResources(base: string, integrationId: string): Promise<IntegrationResourceSummary[]> {
  const response = await fetch(`${base}/integrations/${encodeURIComponent(integrationId)}/resources`)
  const body = await readJson<IntegrationResourceListBody>(response, "Load integration resources failed")
  return body.items ?? []
}

export async function listIntegrationWebhookEvents(
  base: string,
  integrationId: string,
  limit = 3,
): Promise<IntegrationWebhookEventSummary[]> {
  const query = new URLSearchParams({ limit: String(limit) })
  const response = await fetch(`${base}/integrations/${encodeURIComponent(integrationId)}/webhook-events?${query.toString()}`)
  const body = await readJson<IntegrationWebhookEventListBody>(response, "Load integration webhook events failed")
  return body.items ?? []
}

export async function listIntegrationRepos(
  base: string,
  integrationId: string,
  search = "",
  limit = 50,
): Promise<IntegrationResourceCandidate[]> {
  const query = new URLSearchParams({ limit: String(limit) })
  if (search.trim()) query.set("search", search.trim())
  const response = await fetch(`${base}/integrations/${encodeURIComponent(integrationId)}/repos?${query.toString()}`)
  const body = await readJson<IntegrationResourceCandidateListBody>(response, "Load integration repos failed")
  return normalizeResourceCandidates(body.items)
}

export async function listLinearTeams(base: string, integrationId: string): Promise<IntegrationResourceCandidate[]> {
  const response = await fetch(`${base}/integrations/${encodeURIComponent(integrationId)}/linear/teams`)
  const body = await readJson<IntegrationResourceCandidateListBody>(response, "Load Linear teams failed")
  return normalizeResourceCandidates(body.items)
}

export async function createIntegration(
  base: string,
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
    body: JSON.stringify(input),
  })
  return readJson<IntegrationSummary>(response, "Create integration failed")
}

export async function createIntegrationResource(
  base: string,
  integrationId: string,
  input: {
    resource_type: string
    external_id?: string
    external_key: string
    display_name?: string
    default_ref?: string
  },
): Promise<IntegrationResourceSummary> {
  const response = await fetch(`${base}/integrations/${encodeURIComponent(integrationId)}/resources`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  })
  return readJson<IntegrationResourceSummary>(response, "Link integration resource failed")
}

export async function setIntegrationApiToken(base: string, id: string, apiToken: string): Promise<void> {
  const response = await fetch(`${base}/integrations/${encodeURIComponent(id)}/api-token`, {
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
    body: JSON.stringify(input),
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
  if (provider === "linear") {
    return JSON.stringify({ mcp_server_url: linearMcpServerUrlDefault, enabled: true })
  }
  return JSON.stringify({ enabled: true })
}

export function parseIntegrationResponse<T>(data: unknown): T {
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

async function readJson<T>(response: Response, label: string): Promise<T> {
  if (!response.ok) {
    const text = await response.text().catch(() => "")
    throw new Error(`${label} (${response.status})${text ? `: ${text.slice(0, 180)}` : ""}`)
  }
  return parseIntegrationResponse<T>(await response.json())
}
