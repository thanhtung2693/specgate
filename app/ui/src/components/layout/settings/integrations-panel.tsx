// Settings panel extracted from settings.tsx. See app/ui/AGENTS.md.

import { PlugIcon, PlusIcon } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react"
import { GitHubIcon, GitLabIcon, LinearIcon } from "@/components/brand-icons"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { beginPendingIntegrationOAuth, createIntegration, createIntegrationResource, integrationConfigJson, integrationProviders, integrationsBase, listIntegrationRepos, listIntegrationResources, listIntegrationWebhookEvents, listIntegrations, listLinearTeams, oauthRedirectTarget, providerDefinition, setIntegrationApiToken, type IntegrationProvider, type IntegrationResourceCandidate, type IntegrationResourceSummary, type IntegrationSummary, type IntegrationWebhookEventSummary } from "@/data/integrations"
import { formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { statusTone, toneClass } from "../shared"
import { ActionTooltip } from "../shared-ui"

function IntegrationBrandIcon({ provider }: { provider: string }) {
  const className = "size-5"
  if (provider === "github") return <GitHubIcon className={className} />
  if (provider === "gitlab") return <GitLabIcon className={className} />
  if (provider === "linear") return <LinearIcon className={className} />
  return <PlugIcon className={className} />
}

export function IntegrationsSettingsPanel({ workspaceId }: { workspaceId?: string }) {
  const base = useMemo(() => integrationsBase(), [])
  const selectedWorkspace = workspaceId?.trim() ?? ""
  const [items, setItems] = useState<IntegrationSummary[] | null>(null)
  const [resourcesByIntegration, setResourcesByIntegration] = useState<Record<string, IntegrationResourceState>>({})
  const [webhooksByIntegration, setWebhooksByIntegration] = useState<Record<string, IntegrationWebhookState>>({})
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(Boolean(base && selectedWorkspace))
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [linkingIntegration, setLinkingIntegration] = useState<IntegrationSummary | null>(null)
  const requestGeneration = useRef(0)
  const displayedItems = items ?? []

  // Integration details are lazy: the list view fetches only /integrations,
  // and per-integration resources/webhook events load on first expansion.
  const reloadIntegrations = useCallback(() => {
    if (!base || !selectedWorkspace) {
      requestGeneration.current += 1
      setItems([])
      setResourcesByIntegration({})
      setWebhooksByIntegration({})
      setExpandedIds(new Set())
      setLinkingIntegration(null)
      setAddOpen(false)
      setLoading(false)
      return
    }
    const generation = ++requestGeneration.current
    setLoading(true)
    setError(null)
    setItems([])
    setResourcesByIntegration({})
    setWebhooksByIntegration({})
    setExpandedIds(new Set())
    setLinkingIntegration(null)
    setAddOpen(false)
    listIntegrations(base, selectedWorkspace)
      .then((nextItems) => {
        if (generation === requestGeneration.current) setItems(nextItems)
      })
      .catch((reason: unknown) => {
        if (generation === requestGeneration.current) setError(reason instanceof Error ? reason.message : "Failed to load integrations")
      })
      .finally(() => {
        if (generation === requestGeneration.current) setLoading(false)
      })
  }, [base, selectedWorkspace])

  useEffect(() => {
    reloadIntegrations()
  }, [reloadIntegrations])

  const loadWebhookEvents = useCallback(
    (integrationId: string) => {
      if (!base || !selectedWorkspace) return
      const generation = requestGeneration.current
      void listIntegrationWebhookEvents(base, selectedWorkspace, integrationId, 3)
        .then((events) => {
          if (generation === requestGeneration.current) setWebhooksByIntegration((current) => ({ ...current, [integrationId]: { items: events } }))
        })
        .catch((reason: unknown) => {
          if (generation !== requestGeneration.current) return
          const message = reason instanceof Error ? reason.message : "Failed to load webhook events"
          setWebhooksByIntegration((current) => ({ ...current, [integrationId]: { items: [], error: message } }))
        })
    },
    [base, selectedWorkspace],
  )

  const loadIntegrationDetails = useCallback(
    (integrationId: string) => {
      if (!base || !selectedWorkspace) return
      const generation = requestGeneration.current
      void listIntegrationResources(base, selectedWorkspace, integrationId)
        .then((resources) => {
          if (generation === requestGeneration.current) setResourcesByIntegration((current) => ({ ...current, [integrationId]: { items: resources } }))
        })
        .catch((reason: unknown) => {
          if (generation !== requestGeneration.current) return
          const message = reason instanceof Error ? reason.message : "Failed to load resources"
          setResourcesByIntegration((current) => ({ ...current, [integrationId]: { items: [], error: message } }))
        })
      loadWebhookEvents(integrationId)
    },
    [base, loadWebhookEvents, selectedWorkspace],
  )

  function toggleIntegrationDetails(integrationId: string) {
    const expanding = !expandedIds.has(integrationId)
    setExpandedIds((current) => {
      const next = new Set(current)
      if (next.has(integrationId)) next.delete(integrationId)
      else next.add(integrationId)
      return next
    })
    if (expanding && !resourcesByIntegration[integrationId] && !webhooksByIntegration[integrationId]) {
      loadIntegrationDetails(integrationId)
    }
  }

  function handleCreated(integration: IntegrationSummary) {
    setItems((current) => [integration, ...(current ?? [])])
    setResourcesByIntegration((current) => ({ ...current, [integration.id]: { items: [] } }))
    setWebhooksByIntegration((current) => ({ ...current, [integration.id]: { items: [] } }))
    setExpandedIds((current) => new Set(current).add(integration.id))
    setAddOpen(false)
  }

  function handleResourceLinked(integrationId: string, resource: IntegrationResourceSummary) {
    setResourcesByIntegration((current) => {
      const existing = current[integrationId]?.items ?? []
      return {
        ...current,
        [integrationId]: { items: [resource, ...existing.filter((item) => item.id !== resource.id)] },
      }
    })
    if (!webhooksByIntegration[integrationId]) loadWebhookEvents(integrationId)
    setExpandedIds((current) => new Set(current).add(integrationId))
    setLinkingIntegration(null)
  }

  return (
    <section className="grid gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Integrations</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            Repository observations and optional work-tracking handoffs are backed by connected providers.
          </p>
        </div>
        <ActionTooltip content={!base ? "Set VITE_DOC_REGISTRY_URL before adding integrations." : !selectedWorkspace ? "Select a workspace before adding integrations." : "Connect GitHub, GitLab, or Linear for delivery signals."}>
          <span className="inline-flex">
            <Button type="button" size="sm" className="rounded-md" disabled={!base || !selectedWorkspace} onClick={() => setAddOpen(true)}>
              <PlusIcon data-icon="inline-start" />
              Add integration
            </Button>
          </span>
        </ActionTooltip>
      </div>
      {!base ? (
        <div className="rounded-lg border border-dashed bg-card/60 p-3 text-sm text-muted-foreground">
          Set <code>VITE_DOC_REGISTRY_URL</code> to add integrations and view connected provider status.
        </div>
      ) : null}
      {base && !selectedWorkspace ? (
        <div className="rounded-lg border border-dashed bg-card/60 p-3 text-sm text-muted-foreground">
          Select a workspace to add integrations and view connected provider status.
        </div>
      ) : null}
      {error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-300">
          {error}
        </div>
      ) : null}
      <div className="grid gap-5">
        {loading ? <div className="rounded-lg border bg-background/70 p-3 text-sm text-muted-foreground">Loading integrations...</div> : null}
        {!loading && displayedItems.length === 0 ? (
          <div className="rounded-lg border border-dashed bg-card/60 p-3 text-sm text-muted-foreground">
            No integrations connected yet. Add GitHub, GitLab, or Linear when you need provider signals.
          </div>
        ) : null}
        {!loading ? ([
          ["repositories", "Repositories"],
          ["work_tracking", "Work tracking"],
        ] as const).map(([category, label]) => {
          const categoryItems = displayedItems.filter((integration) => providerDefinition(integration.provider).category === category)
          return (
            <section key={category} className="grid gap-2" aria-labelledby={`integration-${category}`}>
              <h3 id={`integration-${category}`} className="text-sm font-semibold">{label}</h3>
              {categoryItems.length === 0 ? <p className="text-sm text-muted-foreground">No {label.toLowerCase()} connected.</p> : null}
              {categoryItems.map((integration) => (
                <div
                  key={integration.id}
                  className="grid gap-3 rounded-lg border bg-background/70 p-3 md:grid-cols-[minmax(0,1fr)_auto]"
                >
                  <div className="flex min-w-0 items-center gap-3">
                    <div className="flex size-9 items-center justify-center rounded-md border bg-card">
                      <IntegrationBrandIcon provider={integrationProvider(integration)} />
                    </div>
                    <div className="min-w-0">
                      <h4 className="text-sm font-medium">{integrationName(integration)}</h4>
                      <p className="mt-1 text-xs text-muted-foreground">{integrationScope(integration)}</p>
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center justify-start gap-2 md:justify-end">
                    <Badge variant="outline" className={cn("border", toneClass(statusTone("integration", integration.status)))}>
                      {integration.status}
                    </Badge>
                    {base ? (
                      <>
                        <ActionTooltip content={canLinkIntegrationResource(integration) ? "Link a repository or tracker team through Doc Registry." : "Store an API token or finish OAuth before linking resources."}>
                          <span className="inline-flex">
                            <Button type="button" variant="outline" size="sm" className="rounded-md" disabled={!canLinkIntegrationResource(integration)} onClick={() => setLinkingIntegration(integration)}>
                              <PlusIcon data-icon="inline-start" />
                              Link resource
                            </Button>
                          </span>
                        </ActionTooltip>
                        <ActionTooltip content="Load linked resources and recent webhook deliveries for this integration.">
                          <Button type="button" variant="ghost" size="sm" className="rounded-md" aria-expanded={expandedIds.has(integration.id)} onClick={() => toggleIntegrationDetails(integration.id)}>
                            {expandedIds.has(integration.id) ? "Hide details" : "Show details"}
                          </Button>
                        </ActionTooltip>
                      </>
                    ) : null}
                  </div>
                  <p className="border-t pt-2 text-xs leading-5 text-muted-foreground md:col-span-2">{integrationDetail(integration, Boolean(base))}</p>
                  {base && expandedIds.has(integration.id) ? (
                    <div className="grid gap-3 md:col-span-2">
                      <IntegrationResourcesSummary state={resourcesByIntegration[integration.id]} />
                      <IntegrationWebhookEventsSummary state={webhooksByIntegration[integration.id]} />
                    </div>
                  ) : null}
                </div>
              ))}
            </section>
          )
        }) : null}
      </div>
      <AddIntegrationDialog open={addOpen} onOpenChange={setAddOpen} base={base} workspaceId={selectedWorkspace} onCreated={handleCreated} />
      <LinkIntegrationResourceDialog
        integration={linkingIntegration}
        base={base}
        workspaceId={selectedWorkspace}
        onOpenChange={(open) => {
          if (!open) setLinkingIntegration(null)
        }}
        onLinked={handleResourceLinked}
      />
    </section>
  )
}

type IntegrationResourceState = {
  items: IntegrationResourceSummary[]
  error?: string
}

type IntegrationWebhookState = {
  items: IntegrationWebhookEventSummary[]
  error?: string
}

function IntegrationResourcesSummary({ className, state }: { className?: string; state?: IntegrationResourceState }) {
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

function IntegrationWebhookEventsSummary({ state }: { state?: IntegrationWebhookState }) {
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

function integrationScope(integration: IntegrationSummary): string {
  return providerDefinition(integration.provider).scope
}

function integrationProvider(integration: IntegrationSummary): string {
  return integration.provider
}

function integrationName(integration: IntegrationSummary): string {
  return integration.name
}

function integrationDetail(integration: IntegrationSummary, live: boolean): string {
  const auth =
    integration.auth_method === "oauth" && integration.has_oauth_token
      ? "OAuth connected"
      : integration.has_api_token
        ? "API token stored"
        : "No outbound token recorded"
  if (integration.last_error) return `${auth}. Last error: ${integration.last_error}`
  return live ? `${auth}. Link resources here; reprovisioning, disconnect, and webhook-secret management stay with backend/admin flows.` : auth
}

function canLinkIntegrationResource(integration: IntegrationSummary): boolean {
  return Boolean(integration.has_api_token || integration.has_oauth_token)
}

function integrationResourceType(provider: string): string {
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

function LinkIntegrationResourceDialog({
  integration,
  base,
  workspaceId,
  onOpenChange,
  onLinked,
}: {
  integration: IntegrationSummary | null
  base: string | null
  workspaceId: string
  onOpenChange: (open: boolean) => void
  onLinked: (integrationId: string, resource: IntegrationResourceSummary) => void
}) {
  const [query, setQuery] = useState("")
  const [candidates, setCandidates] = useState<IntegrationResourceCandidate[]>([])
  const [selectedKey, setSelectedKey] = useState("")
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const open = integration !== null
  const provider = integration ? integrationProvider(integration) : "github"
  const selected = candidates.find((candidate) => candidate.external_key === selectedKey)
  const candidateLabel = provider === "linear" ? "teams" : "repositories"

  const loadCandidates = useCallback((search = "") => {
    if (!base || !workspaceId || !integration) return
    setLoading(true)
    setError(null)
    const loader =
      provider === "linear"
        ? listLinearTeams(base, workspaceId, integration.id)
        : listIntegrationRepos(base, workspaceId, integration.id, search, 50)
    loader
      .then((items) => {
        setCandidates(items)
        setSelectedKey(items[0]?.external_key || "")
      })
      .catch((reason: unknown) => {
        setCandidates([])
        setSelectedKey("")
        setError(reason instanceof Error ? reason.message : "Resource candidates unavailable")
      })
      .finally(() => setLoading(false))
  }, [base, integration, provider, workspaceId])

  useEffect(() => {
    if (!open) {
      setQuery("")
      setCandidates([])
      setSelectedKey("")
      setLoading(false)
      setSaving(false)
      setError(null)
      return
    }
    loadCandidates("")
  }, [loadCandidates, open])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!base || !workspaceId || !integration || !selected) return
    setSaving(true)
    setError(null)
    try {
      const linked = await createIntegrationResource(base, workspaceId, integration.id, {
        resource_type: integrationResourceType(provider),
        external_id: selected.external_id,
        external_key: selected.external_key,
        display_name: selected.display_name,
        default_ref: provider === "linear" ? undefined : selected.default_ref,
      })
      onLinked(integration.id, linked)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Link resource failed")
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(720px,calc(100svh-2rem))] flex-col overflow-hidden sm:max-w-2xl">
        <DialogHeader className="shrink-0">
          <DialogTitle>Link resource</DialogTitle>
          <DialogDescription>
            {integration ? `${integrationName(integration)} ${provider === "linear" ? "team" : "repository"} selection` : "Select a provider resource"}
          </DialogDescription>
        </DialogHeader>
        <form id="link-integration-resource-form" className="grid min-h-0 flex-1 gap-4 overflow-y-auto pr-1" onSubmit={submit}>
          {error ? (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-300">
              {error}
            </div>
          ) : null}
          {provider !== "linear" ? (
            <div className="grid gap-2">
              <label htmlFor="integration-resource-search" className="text-xs font-medium text-muted-foreground">
                Search repositories
              </label>
              <div className="flex gap-2">
                <Input
                  id="integration-resource-search"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="owner/repository"
                />
                <Button type="button" variant="outline" className="rounded-md" disabled={loading} onClick={() => loadCandidates(query)}>
                  Search
                </Button>
              </div>
            </div>
          ) : null}
          <div className="overflow-hidden rounded-lg border bg-background/70">
            {loading ? <div className="p-3 text-sm text-muted-foreground">Loading {candidateLabel}...</div> : null}
            {!loading && candidates.length === 0 ? (
              <div className="p-3 text-sm text-muted-foreground">
                No {candidateLabel} returned for this integration credential.
              </div>
            ) : null}
            {!loading ? candidates.map((candidate) => {
              const selectedCandidate = selectedKey === candidate.external_key
              return (
                <button
                  key={`${candidate.external_id ?? ""}:${candidate.external_key}`}
                  type="button"
                  className={cn(
                    "grid w-full gap-1 border-b px-3 py-2.5 text-left last:border-b-0 hover:bg-muted/45",
                    selectedCandidate && "bg-muted/60",
                  )}
                  aria-pressed={selectedCandidate}
                  onClick={() => setSelectedKey(candidate.external_key)}
                >
                  <span className="flex min-w-0 flex-wrap items-center gap-2">
                    <span className="min-w-0 truncate text-sm font-medium">{candidate.display_name || candidate.external_key}</span>
                    {candidate.default_ref ? (
                      <Badge variant="outline" className="border font-mono text-[0.65rem]">
                        {candidate.default_ref}
                      </Badge>
                    ) : null}
                    {selectedCandidate ? (
                      <Badge variant="outline" className={cn("border", toneClass("success"))}>
                        Selected
                      </Badge>
                    ) : null}
                  </span>
                  <span className="min-w-0 truncate font-mono text-xs text-muted-foreground">{candidate.external_key}</span>
                </button>
              )
            }) : null}
          </div>
          <p className="text-xs leading-5 text-muted-foreground">
            Linking stores the resource in Doc Registry. Provider webhook provisioning, when supported by the backend credential, is performed by the registry service and reflected in the linked resource status.
          </p>
        </form>
        <DialogFooter className="shrink-0">
          <Button type="button" variant="outline" className="rounded-md" disabled={saving} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" form="link-integration-resource-form" className="rounded-md" disabled={!selected || saving}>
            {saving ? "Linking" : "Link resource"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function AddIntegrationDialog({
  open,
  onOpenChange,
  base,
  workspaceId,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  base: string | null
  workspaceId: string
  onCreated: (integration: IntegrationSummary) => void
}) {
  const [provider, setProvider] = useState<IntegrationProvider>("github")
  const [name, setName] = useState(providerDefinition("github").defaultName)
  const [baseUrl, setBaseUrl] = useState(providerDefinition("github").defaultBaseUrl ?? "")
  const [authMethod, setAuthMethod] = useState<"pat" | "oauth">("oauth")
  const [selfHosted, setSelfHosted] = useState(false)
  const [apiToken, setApiToken] = useState("")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const selectedProvider = providerDefinition(provider)
  const ready = Boolean(base && name.trim() && (authMethod === "oauth" || apiToken.trim()))
  const supportsSelfHosted = provider === "github" || provider === "gitlab"
  const showBaseUrl = authMethod === "pat" && supportsSelfHosted && selfHosted

  useEffect(() => {
    if (!open) return
    const next = providerDefinition(provider)
    setName(next.defaultName)
    setBaseUrl(next.defaultBaseUrl ?? "")
    setAuthMethod("oauth")
    setSelfHosted(false)
    setApiToken("")
    setError(null)
  }, [open, provider])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!base || !workspaceId || !ready) return
    setSaving(true)
    setError(null)
    const payload = {
      provider,
      name: name.trim(),
      base_url: showBaseUrl ? baseUrl.trim() || selectedProvider.defaultBaseUrl : undefined,
      config_json: integrationConfigJson(provider),
    }
    try {
      if (authMethod === "oauth") {
        const result = await beginPendingIntegrationOAuth(base, workspaceId, {
          ...payload,
          redirect_target: oauthRedirectTarget(),
        })
        window.location.assign(result.authorize_url)
        return
      }
      const created = await createIntegration(base, workspaceId, payload)
      await setIntegrationApiToken(base, workspaceId, created.id, apiToken.trim())
      onCreated({ ...created, has_api_token: true, auth_method: "pat" })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Add integration failed")
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(760px,calc(100svh-2rem))] flex-col overflow-hidden sm:max-w-2xl">
        <DialogHeader className="shrink-0">
          <DialogTitle>Add integration</DialogTitle>
          <DialogDescription>
            Connect one provider for delivery evidence, tracker status, and handoff signals.
          </DialogDescription>
        </DialogHeader>
        <form id="add-integration-form" className="grid min-h-0 flex-1 gap-4 overflow-y-auto pr-1" onSubmit={submit}>
          {error ? (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-300">
              {error}
            </div>
          ) : null}
          <div className="grid gap-2">
            <span className="text-xs font-medium text-muted-foreground">Provider</span>
            <div className="grid gap-2 sm:grid-cols-3">
              {integrationProviders.map((entry) => (
                <button
                  key={entry.id}
                  type="button"
                  className={cn(
                    "grid gap-2 rounded-lg border bg-background/70 p-3 text-left transition-colors hover:bg-muted/40",
                    provider === entry.id && "border-primary/70 bg-primary/5 ring-1 ring-primary/30",
                  )}
                  onClick={() => setProvider(entry.id)}
                >
                  <span className="flex items-center gap-2 text-sm font-medium">
                    <IntegrationBrandIcon provider={entry.id} />
                    {entry.name}
                  </span>
                  <span className="text-xs leading-5 text-muted-foreground">{entry.scope}</span>
                </button>
              ))}
            </div>
          </div>
          <label className="grid gap-2">
            <span className="text-xs font-medium text-muted-foreground">Name</span>
            <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={selectedProvider.defaultName} />
          </label>
          <div className="grid gap-2">
            <span className="text-xs font-medium text-muted-foreground">Authentication</span>
            <div className="grid gap-2 sm:grid-cols-2">
              {(["pat", "oauth"] as const).map((method) => (
                <button
                  key={method}
                  type="button"
                  className={cn(
                    "rounded-lg border bg-background/70 px-3 py-2 text-left transition-colors hover:bg-muted/40",
                    authMethod === method && "border-primary/70 bg-primary/5 ring-1 ring-primary/30",
                  )}
                  onClick={() => setAuthMethod(method)}
                >
                  <span className="block text-sm font-medium">{method === "pat" ? "API token" : "OAuth"}</span>
                  <span className="text-xs text-muted-foreground">
                    {method === "pat" ? "Store encrypted write-only token." : `Redirect to ${selectedProvider.name}.`}
                  </span>
                </button>
              ))}
            </div>
          </div>
          {authMethod === "pat" && supportsSelfHosted ? (
            <label className="flex items-start gap-3 rounded-md border bg-card/70 px-3 py-2">
              <input
                type="checkbox"
                className="mt-1 size-4 accent-primary"
                checked={selfHosted}
                onChange={(event) => setSelfHosted(event.target.checked)}
              />
              <span className="grid gap-1 text-sm">
                <span className="font-medium">Self-hosted {selectedProvider.name}</span>
                <span className="text-xs leading-5 text-muted-foreground">
                  Leave off for {selectedProvider.defaultBaseUrl}. Turn on only for Enterprise or self-managed installs.
                </span>
              </span>
            </label>
          ) : null}
          {showBaseUrl ? (
            <label className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Base URL</span>
              <Input
                type="url"
                value={baseUrl}
                onChange={(event) => setBaseUrl(event.target.value)}
                placeholder={selectedProvider.defaultBaseUrl}
              />
            </label>
          ) : null}
          {authMethod === "pat" ? (
            <label className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">API token</span>
              <Input
                type="password"
                value={apiToken}
                onChange={(event) => setApiToken(event.target.value)}
                placeholder={selectedProvider.tokenPlaceholder}
                autoComplete="off"
                spellCheck={false}
              />
              <span className="text-xs text-muted-foreground">Sent once to Doc Registry and stored encrypted. The UI never reads it back.</span>
            </label>
          ) : (
            <p className="rounded-md border bg-card/70 px-3 py-2 text-xs leading-5 text-muted-foreground">
              OAuth uses the hosted {selectedProvider.name} flow and returns to Settings when authorization succeeds.
            </p>
          )}
        </form>
        <DialogFooter className="shrink-0">
          <Button type="button" variant="outline" className="rounded-md" disabled={saving} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" form="add-integration-form" className="rounded-md" disabled={!ready || saving}>
            {saving ? "Adding" : "Add integration"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
