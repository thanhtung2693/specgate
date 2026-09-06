// Integration creation and resource-link dialogs.

import { useCallback, useEffect, useState, type FormEvent } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { beginPendingIntegrationOAuth, createIntegration, createIntegrationResource, integrationConfigJson, integrationProviders, listIntegrationRepos, listLinearTeams, oauthRedirectTarget, providerDefinition, setIntegrationApiToken, type IntegrationProvider, type IntegrationResourceCandidate, type IntegrationResourceSummary, type IntegrationSummary } from "@/data/integrations"
import { cn } from "@/lib/utils"
import { toneClass } from "../shared"
import { IntegrationBrandIcon, integrationName, integrationProvider, integrationResourceType } from "./integration-shared"

export function LinkIntegrationResourceDialog({
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
      // oxlint-disable-next-line react/set-state-in-effect -- Reset the externally controlled dialog draft; never retain credentials across reopen.
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
          <div className="overflow-hidden sg-inset">
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

export function AddIntegrationDialog({
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
    // oxlint-disable-next-line react/set-state-in-effect -- Reset the externally controlled dialog draft; never retain credentials across reopen.
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
                    "grid gap-2 sg-inset p-3 text-left transition-colors hover:bg-muted/40",
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
                    "sg-inset px-3 py-2 text-left transition-colors hover:bg-muted/40",
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
            <label className="flex items-start gap-3 sg-inset px-3 py-2">
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
            <p className="sg-inset px-3 py-2 text-xs leading-5 text-muted-foreground">
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
