// Shared integration labels and read-only summaries.

import { PlugIcon } from "lucide-react"
import { GitHubIcon, GitLabIcon, LinearIcon } from "@/components/brand-icons"
import { Badge } from "@/components/ui/badge"
import { providerDefinition, type IntegrationResourceSummary, type IntegrationSummary, type IntegrationWebhookEventSummary } from "@/data/integrations"
import { formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { statusTone, toneClass } from "../shared"

export function IntegrationBrandIcon({ provider }: { provider: string }) {
  const className = "size-5"
  if (provider === "github") return <GitHubIcon className={className} />
  if (provider === "gitlab") return <GitLabIcon className={className} />
  if (provider === "linear") return <LinearIcon className={className} />
  return <PlugIcon className={className} />
}

export type IntegrationResourceState = {
  items: IntegrationResourceSummary[]
  error?: string
}

export type IntegrationWebhookState = {
  items: IntegrationWebhookEventSummary[]
  error?: string
}

export function IntegrationResourcesSummary({ className, state }: { className?: string; state?: IntegrationResourceState }) {
  if (!state) {
    return (
      <div className={cn("border-t pt-2 text-xs text-muted-foreground", className)}>
        Loading linked resources...
      </div>
    )
  }
  if (state.error) {
    return (
      <div className={cn("border-t pt-2 text-xs text-amber-700 dark:text-amber-300", className)}>
        Resource list unavailable: {state.error}
      </div>
    )
  }
  if (state.items.length === 0) {
    return (
      <div className={cn("border-t pt-2 text-xs text-muted-foreground", className)}>
        No linked resources yet. Use Link resource to register a repository or team; webhook management stays in backend/admin flows.
      </div>
    )
  }

  return (
    <div className={cn("grid gap-2 border-t pt-2", className)}>
      <div className="text-xs font-medium text-muted-foreground">Linked resources</div>
      <div className="grid gap-2">
        {state.items.map((resource) => {
          const webhook = integrationResourceWebhook(resource)
          return (
            <div key={resource.id} className="grid gap-2 rounded-md border bg-card/50 p-2 sm:grid-cols-[minmax(0,1fr)_auto]">
              <div className="min-w-0">
                <div className="truncate text-xs font-medium">{integrationResourceName(resource)}</div>
                <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  <span>{resource.resource_type || "resource"}</span>
                  {resource.default_ref ? <span>Ref {resource.default_ref}</span> : null}
                  {resource.has_webhook_secret ? <span>Webhook secret stored</span> : null}
                </div>
              </div>
              <Badge variant="outline" className={cn("w-fit border", toneClass(webhook.tone))}>
                {webhook.label}
              </Badge>
              {webhook.detail ? (
                <p className="text-xs text-muted-foreground sm:col-span-2">{webhook.detail}</p>
              ) : null}
            </div>
          )
        })}
      </div>
    </div>
  )
}

export function IntegrationWebhookEventsSummary({ state }: { state?: IntegrationWebhookState }) {
  if (!state) {
    return <div className="border-t pt-2 text-xs text-muted-foreground">Loading webhook events...</div>
  }
  if (state.error) {
    return <div className="border-t pt-2 text-xs text-amber-700 dark:text-amber-300">Webhook events unavailable: {state.error}</div>
  }
  if (state.items.length === 0) {
    return <div className="border-t pt-2 text-xs text-muted-foreground">No webhook deliveries recorded yet.</div>
  }

  return (
    <div className="grid gap-2 border-t pt-2">
      <div className="text-xs font-medium text-muted-foreground">Recent webhooks</div>
      <div className="grid gap-2">
        {state.items.map((event) => (
          <div key={event.id} className="grid gap-2 rounded-md border bg-card/50 p-2 sm:grid-cols-[minmax(0,1fr)_auto]">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-xs font-medium">{webhookEventLabel(event.event_type || "webhook")}</span>
                {event.correlation_id ? (
                  <span className="font-mono text-[0.68rem] text-muted-foreground">{event.correlation_id}</span>
                ) : null}
              </div>
              <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                {event.received_at ? <span>{formatDateTime(event.received_at)}</span> : null}
                {event.resource_id ? <span>Resource {event.resource_id}</span> : null}
              </div>
            </div>
            <Badge variant="outline" className={cn("w-fit border", toneClass(statusTone("webhookEvent", event.status)))}>
              {event.status || "recorded"}
            </Badge>
            {event.error ? <p className="text-xs text-muted-foreground sm:col-span-2">{event.error}</p> : null}
          </div>
        ))}
      </div>
    </div>
  )
}

function webhookEventLabel(eventType: string): string {
  const words = eventType.split(/[_-]+/).filter(Boolean)
  if (words.length === 0) return "Webhook"
  return [words[0][0]?.toUpperCase() + words[0].slice(1), ...words.slice(1)].join(" ")
}

function integrationResourceName(resource: IntegrationResourceSummary): string {
  return resource.display_name || resource.external_key || resource.external_id || resource.id
}

function integrationResourceWebhook(resource: IntegrationResourceSummary): { label: string; detail?: string; tone: "neutral" | "success" | "warning" | "danger" } {
  const config = parseJsonObject(resource.config_json)
  const status = stringFromRecord(config, "webhook_status")
  const lastError = stringFromRecord(config, "webhook_last_error")
  const providerWebhookID = stringFromRecord(config, "provider_webhook_id")

  if (status === "connected") {
    return {
      label: "Webhook connected",
      detail: providerWebhookID ? `Provider hook ${providerWebhookID}` : undefined,
      tone: "success",
    }
  }
  if (status === "error") {
    return {
      label: "Webhook error",
      detail: lastError || undefined,
      tone: "danger",
    }
  }
  if (status) {
    return {
      label: `Webhook ${status}`,
      detail: lastError || undefined,
      tone: "warning",
    }
  }
  if (resource.has_webhook_secret) {
    return { label: "Webhook secret stored", tone: "warning" }
  }
  return { label: "No webhook", tone: "neutral" }
}

function parseJsonObject(value?: string): Record<string, unknown> | null {
  if (!value) return null
  try {
    const parsed = JSON.parse(value) as unknown
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : null
  } catch {
    return null
  }
}

function stringFromRecord(record: Record<string, unknown> | null, key: string): string {
  const value = record?.[key]
  return typeof value === "string" ? value.trim() : ""
}

export function integrationScope(integration: IntegrationSummary): string {
  return providerDefinition(integration.provider).scope
}

export function integrationProvider(integration: IntegrationSummary): string {
  return integration.provider
}

export function integrationName(integration: IntegrationSummary): string {
  return integration.name
}

export function integrationDetail(integration: IntegrationSummary, live: boolean): string {
  const auth =
    integration.auth_method === "oauth" && integration.has_oauth_token
      ? "OAuth connected"
      : integration.has_api_token
        ? "API token stored"
        : "No outbound token recorded"
  if (integration.last_error) return `${auth}. Last error: ${integration.last_error}`
  return live ? `${auth}. Link resources here; reprovisioning, disconnect, and webhook-secret management stay with backend/admin flows.` : auth
}

export function canLinkIntegrationResource(integration: IntegrationSummary): boolean {
  return Boolean(integration.has_api_token || integration.has_oauth_token)
}

export function integrationResourceType(provider: string): string {
  switch (provider) {
    case "github":
      return "repo"
    case "gitlab":
      return "project"
    case "linear":
      return "team"
    default:
      throw new Error(`Unsupported integration provider: ${provider}`)
  }
}
