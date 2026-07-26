// Context Pack preview and Linear handoff controls.

import { CopyIcon, ExternalLinkIcon, FileTextIcon } from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { handoffToLinear, integrationsBase, listIntegrationResources, listIntegrations, type IntegrationResourceSummary, type IntegrationSummary } from "@/data/integrations"
import { type WorkItemDetailData } from "@/data/workboard"
import { type WorkItem } from "@/data/workspace"
import { cn } from "@/lib/utils"
import { readableKey, toneClass } from "../shared"
import { ActionTooltip, copyText, MarkdownText } from "../shared-ui"
import { acceptanceCriterionDone } from "./item-detail-sections"

function downloadMarkdown(filename: string, markdown: string) {
  const url = URL.createObjectURL(new Blob([markdown], { type: "text/markdown;charset=utf-8" }))
  const anchor = document.createElement("a")
  anchor.href = url
  anchor.download = filename
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}

export function ContextPackDetail({
  item,
  detail,
  workspaceId,
  onHandoffSuccess,
}: {
  item: WorkItem
  detail: WorkItemDetailData
  workspaceId: string
  onHandoffSuccess: () => void
}) {
  const [copied, setCopied] = useState<"uri" | "handoff" | null>(null)
  const contextPack = detail.contextPack
  const contextPackUnavailable = detail.readback.contextPack === "error" || !contextPack?.markdown

  if (!contextPack && !contextPackUnavailable) {
    return (
      <section className="rounded-lg border bg-background/70 p-4">
        <div>
          <h3 className="text-sm font-semibold">Handoff / Context Pack</h3>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
            No Context Pack is available yet. Continue preparation in your IDE or CLI, then return here to inspect the handoff.
          </p>
        </div>
      </section>
    )
  }

  const handoffMarkdownAvailable = !contextPackUnavailable
  const markdown = contextPack?.markdown ?? ""
  const filename = `${item.key.toLowerCase()}-context-pack.md`

  return (
    <section className="grid gap-3">
      <div className="rounded-lg border bg-background/70 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h3 className="text-sm font-semibold">Handoff / Context Pack</h3>
          <div className="flex flex-wrap gap-2">
            <ActionTooltip content="Copy Markdown handoff for CLI or IDE agent.">
              <Button
                variant="outline"
                size="sm"
                className="rounded-md"
                disabled={!handoffMarkdownAvailable}
                onClick={() => {
                  void copyText(markdown).then((didCopy) => {
                    if (didCopy) setCopied("handoff")
                  })
                }}
              >
                <CopyIcon data-icon="inline-start" />
                {copied === "handoff" ? "Handoff copied" : "Copy handoff"}
              </Button>
            </ActionTooltip>
            <ActionTooltip content="Download the same Markdown handoff as a file.">
              <Button
                variant="outline"
                size="sm"
                className="rounded-md"
                disabled={!handoffMarkdownAvailable}
                onClick={() => downloadMarkdown(filename, markdown)}
              >
                <FileTextIcon data-icon="inline-start" />
                Download .md
              </Button>
            </ActionTooltip>
          </div>
        </div>
        {contextPackUnavailable ? (
          <p className="mt-3 rounded-md border bg-card/55 p-3 text-sm text-muted-foreground">
            Context Pack unavailable. Check Doc Registry connectivity; no fallback handoff markdown is copied or downloaded in live mode.
          </p>
        ) : null}
        {contextPack && !contextPackUnavailable ? (
          <LinearHandoffControl item={item} detail={detail} workspaceId={workspaceId} onSuccess={onHandoffSuccess} />
        ) : null}
      </div>
      {contextPack && !contextPackUnavailable ? (
        <div className="rounded-lg border bg-background/70 p-4">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h4 className="text-sm font-semibold">Context Pack preview</h4>
              <p className="mt-1 text-sm text-muted-foreground">
                Registry-derived handoff material for CLI and IDE agents.
              </p>
            </div>
            <Badge variant="outline" className={cn("border", toneClass("success"))}>
              {contextPack.state}
            </Badge>
          </div>
          <div className="mt-3 max-h-[420px] overflow-auto rounded-md border bg-card/55 p-3">
            <MarkdownText content={contextPack.markdown} compact />
          </div>
          {contextPack.warnings.length > 0 ? (
            <div className="mt-3 grid gap-2">
              <h5 className="text-xs font-semibold text-muted-foreground">Warnings</h5>
              {contextPack.warnings.map((warning) => (
                <div key={warning.id} className="rounded-md border bg-card/55 p-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="outline" className={cn("border text-[0.68rem]", toneClass("warning"))}>
                      {readableKey(warning.code)}
                    </Badge>
                    {warning.artifactId ? <span className="font-mono text-[0.68rem] text-muted-foreground">{warning.artifactId}</span> : null}
                  </div>
                  <p className="mt-1 text-xs leading-5 text-muted-foreground">{warning.message}</p>
                </div>
              ))}
            </div>
          ) : null}
          {contextPack.knowledgeProvenance.length > 0 ? (
            <div className="mt-3 grid gap-2">
              <h5 className="text-xs font-semibold text-muted-foreground">Knowledge provenance</h5>
              {contextPack.knowledgeProvenance.map((row) => (
                <div key={row.id} className="grid gap-1 rounded-md border bg-card/55 p-2 sm:grid-cols-[minmax(0,1fr)_auto]">
                  <div className="min-w-0">
                    <p className="truncate text-xs font-medium">{row.title}</p>
                    <p className="mt-1 truncate font-mono text-[0.68rem] text-muted-foreground">{row.id}</p>
                  </div>
                  <div className="flex flex-wrap gap-1 sm:justify-end">
                    {row.version ? <Badge variant="secondary" className="text-[0.65rem]">{row.version}</Badge> : null}
                    {row.freshness ? <Badge variant="outline" className="border text-[0.65rem]">{readableKey(row.freshness)}</Badge> : null}
                  </div>
                </div>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
      <div className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-lg border bg-card/70 p-3">
          <span className="text-xs text-muted-foreground">Route</span>
          <p className="mt-1 text-sm font-medium">{item.route}</p>
        </div>
        <div className="rounded-lg border bg-card/70 p-3">
          <span className="text-xs text-muted-foreground">Acceptance</span>
          <p className="mt-1 text-sm font-medium">
            {detail.acceptanceCriteria.filter((criterion) => acceptanceCriterionDone(criterion, detail.deliveryStatus)).length}/{detail.acceptanceCriteria.length}
          </p>
        </div>
        <div className="rounded-lg border bg-card/70 p-3">
          <span className="text-xs text-muted-foreground">Detail source</span>
          <p className="mt-1 text-sm font-medium">{detail.source}</p>
        </div>
      </div>
    </section>
  )
}

type LinearDestination = {
  integration: IntegrationSummary
  resource: IntegrationResourceSummary
}

function LinearHandoffControl({
  item,
  detail,
  workspaceId,
  onSuccess,
}: {
  item: WorkItem
  detail: WorkItemDetailData
  workspaceId: string
  onSuccess: () => void
}) {
  const [open, setOpen] = useState(false)
  const [destinations, setDestinations] = useState<LinearDestination[]>([])
  const [selectedDestination, setSelectedDestination] = useState("")
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const base = integrationsBase()
  const ready = item.lifecycle.trim().toLowerCase() === "ready"
  const hasTrackerLink = detail.readback.trackerLinks === "ready" && detail.trackerLinks.length > 0
  const selected = destinations.find(({ resource }) => resource.id === selectedDestination)

  async function openSelector() {
    if (!base || !workspaceId || !ready || hasTrackerLink) return
    setOpen(true)
    setLoading(true)
    setError(null)
    try {
      const integrations = (await listIntegrations(base, workspaceId)).filter(
        (integration) => integration.provider === "linear" && integration.status === "connected",
      )
      const resources = await Promise.all(
        integrations.map(async (integration) => ({
          integration,
          resources: await listIntegrationResources(base, workspaceId, integration.id),
        })),
      )
      const nextDestinations = resources.flatMap(({ integration, resources: integrationResources }) =>
        integrationResources
          .filter((resource) => resource.resource_type === "team")
          .map((resource) => ({ integration, resource })),
      )
      setDestinations(nextDestinations)
      setSelectedDestination(nextDestinations[0]?.resource.id ?? "")
    } catch (reason) {
      setDestinations([])
      setSelectedDestination("")
      setError(reason instanceof Error ? reason.message : "Linear destinations unavailable")
    } finally {
      setLoading(false)
    }
  }

  async function handoff() {
    if (!base || !workspaceId || !selected) return
    setSaving(true)
    setError(null)
    try {
      await handoffToLinear(base, workspaceId, item.registryId || item.key, selected.integration.id, selected.resource.id)
      setOpen(false)
      onSuccess()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Linear handoff failed")
    } finally {
      setSaving(false)
    }
  }

  if (!ready || !base || !workspaceId || detail.readback.trackerLinks !== "ready") return null
  if (hasTrackerLink) {
    return (
      <div className="mt-4 rounded-md border bg-card/55 p-3">
        <p className="text-xs font-medium text-muted-foreground">Linked Linear issue</p>
        {detail.trackerLinks.map((link) => (
          <a key={`${link.identifier}-${link.url}`} href={link.url} target="_blank" rel="noreferrer" className="mt-1 inline-flex items-center gap-2 text-sm font-medium hover:underline">
            <ExternalLinkIcon className="size-4" />
            {link.identifier}
          </a>
        ))}
      </div>
    )
  }

  return (
    <>
      <div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-md border bg-card/55 p-3">
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground">
          Use this approved Context Pack with your IDE agent, or hand it off to a connected Linear team.
        </p>
        <Button type="button" variant="outline" size="sm" className="rounded-md" onClick={() => void openSelector()}>
          Hand off to Linear
        </Button>
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Hand off to Linear</DialogTitle>
            <DialogDescription>Select the connected Linear team that should receive this Ready work item.</DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            {loading ? <p className="text-sm text-muted-foreground">Loading connected Linear teams...</p> : null}
            {error ? <p className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">{error}</p> : null}
            {!loading && !error && destinations.length === 0 ? <p className="text-sm text-muted-foreground">No connected Linear teams are available.</p> : null}
            {!loading ? destinations.map(({ integration, resource }) => {
              const selectedTeam = resource.id === selectedDestination
              return (
                <button
                  key={resource.id}
                  type="button"
                  className={cn("grid gap-1 rounded-md border p-3 text-left", selectedTeam && "border-primary bg-primary/5")}
                  aria-pressed={selectedTeam}
                  onClick={() => setSelectedDestination(resource.id)}
                >
                  <span className="text-sm font-medium">{resource.display_name || resource.external_key}</span>
                  <span className="font-mono text-xs text-muted-foreground">{integration.name} · {resource.external_key}</span>
                </button>
              )
            }) : null}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" className="rounded-md" disabled={saving} onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="button" className="rounded-md" disabled={!selected || saving} onClick={() => void handoff()}>
              {saving ? "Handing off" : "Hand off to Linear"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
