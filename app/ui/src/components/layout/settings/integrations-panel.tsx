// Integration settings list and lazy detail loading.

import { PlusIcon } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { integrationsBase, listIntegrationResources, listIntegrationWebhookEvents, listIntegrations, providerDefinition, type IntegrationResourceSummary, type IntegrationSummary } from "@/data/integrations"
import { cn } from "@/lib/utils"
import { statusTone, toneClass } from "../shared"
import { ActionTooltip } from "../shared-ui"
import { AddIntegrationDialog, LinkIntegrationResourceDialog } from "./integration-dialogs"
import { IntegrationBrandIcon, IntegrationResourcesSummary, IntegrationWebhookEventsSummary, canLinkIntegrationResource, integrationDetail, integrationName, integrationProvider, integrationScope, type IntegrationResourceState, type IntegrationWebhookState } from "./integration-shared"

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
    // oxlint-disable-next-line react/set-state-in-effect -- Reset stale request state before loading data for the newly selected endpoint or workspace.
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
        {loading ? <div className="sg-inset p-3 text-sm text-muted-foreground">Loading integrations...</div> : null}
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
                  className="grid gap-3 sg-inset p-3 md:grid-cols-[minmax(0,1fr)_auto]"
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
