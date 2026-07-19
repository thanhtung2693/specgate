// Work-item detail view extracted from app-shell.tsx: overview, handoff,
// verification, and activity tabs for a selected work item.

import { AlertTriangleIcon, BotIcon, CheckCircle2Icon, ChevronRightIcon, CircleDotIcon, ClockIcon, CopyIcon, ExternalLinkIcon, FileTextIcon, ShieldCheckIcon } from "lucide-react"
import { useState } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { summarizeDeliveryTrust, trustTierLabel } from "@/data/delivery-trust"
import { handoffToLinear, integrationsBase, listIntegrationResources, listIntegrations, type IntegrationResourceSummary, type IntegrationSummary } from "@/data/integrations"
import { repositoryObservation, useWorkItemDetail, type AcceptanceCriterionSummary, type DeliveryLinkSummary, type DeliveryStatusSummary, type GateRunSummary, type NextActionSummary, type StaleWarningSummary, type TrackerLinkSummary, type WorkItemDetailData } from "@/data/workboard"
import { type WorkItem } from "@/data/workspace"
import { formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { deliveryText, gateChecks, gateText, isDeliveredWorkItem, readableKey, stateText, statusTone, toneClass, type Tone } from "../shared"
import { ActionTooltip, copyText, GateEvidenceWhy, MarkdownText, PolicyExplanationSection, runGovernanceAgentPrompt } from "../shared-ui"
import { ContextualGovernancePanel, workItemStatusBadge } from "../app-shell"

export function WorkItemDetail({
  item,
  workspaceId,
  hydrateDetail,
  refreshGeneration,
}: {
  item: WorkItem
  workspaceId: string
  hydrateDetail: boolean
  refreshGeneration: number
}) {
  const [handoffRefreshGeneration, setHandoffRefreshGeneration] = useState(0)
  const detail = useWorkItemDetail(item, workspaceId, hydrateDetail, refreshGeneration + handoffRefreshGeneration)
  const [searchParams] = useSearchParams()
  const [activeTab, setActiveTab] = useState(() => {
    const requested = searchParams.get("tab")
    return requested && ["overview", "handoff", "verification", "activity"].includes(requested) ? requested : "overview"
  })
  const [resumeCopied, setResumeCopied] = useState(false)
  const deliveryVerdict = detail.deliveryStatus?.found === false ? undefined : detail.deliveryStatus?.verdict?.trim()
  const deliveryNeedsReview = deliveryVerdict === "fail" || deliveryVerdict === "needs_human_review" || deliveryVerdict === "needs_changes"
  const deliveryPassed = deliveryVerdict === "pass"
  const humanDecision = detail.deliveryStatus?.executor?.trim() === "human"
  const headerBadge = humanDecision && deliveryVerdict
    ? {
        label: deliveryPassed ? "Accepted" : "Rejected",
        tone: statusTone("state", deliveryVerdict),
      }
    : deliveryVerdict
      ? {
          label: deliveryPassed ? "Ready for human review" : stateText(deliveryVerdict),
          tone: statusTone("state", deliveryVerdict),
        }
      : workItemStatusBadge(item)
  const delivered = isDeliveredWorkItem(item)
  const nextActionTab = delivered || deliveryPassed
    ? "verification"
    : deliveryNeedsReview || item.delivery === "needs_changes"
        ? "verification"
        : "handoff"
  const nextActionLabel = delivered || deliveryPassed
    ? "View verdict"
    : deliveryNeedsReview || item.delivery === "needs_changes"
        ? "Inspect gaps"
        : detail.contextPack
          ? "View handoff"
          : "Prepare handoff"
  const agentPrompt = delivered || deliveryPassed
    ? {
        label: "Ask for review summary",
        prompt: `Summarize the delivery review outcome for ${item.key}.`,
      }
    : deliveryNeedsReview || item.delivery === "needs_changes" || item.gate === "fail"
    ? {
        label: "Ask about review gaps",
        prompt: `For ${item.key}, summarize the missing evidence, failed gates, or acceptance criteria that block delivery review.`,
      }
    : {
        label: "Ask about handoff blockers",
        prompt: detail.contextPack
          ? `For ${item.key}, inspect the Context Pack and summarize any remaining handoff risk before an IDE coding agent picks it up.`
          : `For ${item.key}, explain what blocks a governed coding-agent handoff. Check scope, acceptance criteria, route, gate state, and whether a Context Pack or approved source artifact is available.`,
      }

  return (
    <div className="grid min-w-0 gap-5 2xl:grid-cols-[minmax(0,1fr)_minmax(0,320px)]">
      <div className="min-w-0 rounded-lg border bg-card p-5">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <div className="flex flex-wrap gap-2">
              <Badge variant="secondary" className="font-mono">{item.key}</Badge>
              <Badge variant="outline">{item.route}</Badge>
              <Badge variant="outline" className={cn("border", toneClass(headerBadge.tone))}>
                {headerBadge.label}
              </Badge>
            </div>
            <h2 className="mt-3 text-2xl font-semibold tracking-tight">{item.title}</h2>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">{item.summary}</p>
          </div>
          <Button className="rounded-md bg-foreground text-background hover:bg-foreground/85" onClick={() => setActiveTab(nextActionTab)}>
            <ShieldCheckIcon data-icon="inline-start" />
            {nextActionLabel}
          </Button>
        </div>
        <Tabs value={activeTab} onValueChange={setActiveTab} className="mt-5">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="handoff">Handoff</TabsTrigger>
            <TabsTrigger value="verification">Verification</TabsTrigger>
            <TabsTrigger value="activity">Activity</TabsTrigger>
          </TabsList>
          <TabsContent value="overview" className="mt-4">
            <div className="grid gap-3">
              <FeatureOverview detail={detail} />
              <FreshnessSignalsSummary warnings={detail.staleWarnings} />
              <AcceptanceCriteriaSummary detail={detail} />
              <ContextSummary item={item} detail={detail} />
            </div>
          </TabsContent>
          <TabsContent value="handoff" className="mt-4">
            <ContextPackDetail item={item} detail={detail} workspaceId={workspaceId} onHandoffSuccess={() => setHandoffRefreshGeneration((generation) => generation + 1)} />
          </TabsContent>
          <TabsContent value="verification" className="mt-4">
            {/* Verdict first: Reviews deep-links here to read the delivery outcome. */}
            <div className="grid gap-3">
              <DeliverySummary item={item} detail={detail} />
              <RepositoryObservationSummary detail={detail} />
              <GateSummary item={item} detail={detail} />
              <PolicyExplanationSection policy={detail.policy} status={detail.readback.policy} context="work" />
            </div>
          </TabsContent>
          <TabsContent value="activity" className="mt-4">
            <ActivityList item={item} />
          </TabsContent>
        </Tabs>
      </div>
      <aside className="grid min-w-0 content-start gap-4">
        <ContextualGovernancePanel contextLabel={`${item.key} · ${item.title}`} prompts={[agentPrompt]} />
        <section className="min-w-0 overflow-hidden rounded-lg border bg-card/85 p-4">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-sm font-semibold">Resume in CLI</h3>
            <Button
              variant="outline"
              size="sm"
              className="rounded-md"
              aria-label="Copy resume command"
              onClick={() => {
                void copyText(`specgate work context ${item.key}`).then((didCopy) => setResumeCopied(didCopy))
              }}
            >
              <CopyIcon data-icon="inline-start" />
              {resumeCopied ? "Copied" : "Copy"}
            </Button>
          </div>
          <p className="mt-2 max-w-full overflow-x-auto whitespace-nowrap rounded-md border bg-background/70 px-2 py-1.5 font-mono text-xs text-muted-foreground">
            specgate work context {item.key}
          </p>
        </section>
      </aside>
    </div>
  )
}

function FreshnessSignalsSummary({ warnings }: { warnings: StaleWarningSummary[] }) {
  if (warnings.length === 0) return null

  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">Freshness signals</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Registry attention context for stale handoffs, tracker contradictions, and external delivery signals.
          </p>
        </div>
        <Badge variant="outline" className="rounded-full">
          read-only
        </Badge>
      </div>
      <div className="mt-3 grid gap-2">
        {warnings.map((warning) => (
          <div key={warning.id} className="rounded-md border bg-card/70 p-3">
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="min-w-0">
                <p className="text-sm font-medium">{readableKey(warning.code)}</p>
                <p className="mt-1 text-sm leading-6 text-muted-foreground">{warning.message}</p>
              </div>
              <Badge variant="outline" className={cn("shrink-0 border", toneClass(statusTone("severity", warning.severity)))}>
                {readableKey(warning.severity)}
              </Badge>
            </div>
            <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted-foreground">
              {warning.changeRequestId ? <Badge variant="secondary" className="font-mono">{warning.changeRequestId}</Badge> : null}
              {warning.featureId ? <Badge variant="secondary" className="font-mono">{warning.featureId}</Badge> : null}
              {warning.artifactId ? (
                <Badge variant="outline" className="font-mono" asChild>
                  <Link to={`/artifacts?artifact=${encodeURIComponent(warning.artifactId)}`}>{warning.artifactId}</Link>
                </Badge>
              ) : null}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

function FeatureOverview({ detail }: { detail: WorkItemDetailData }) {
  const feature = detail.feature

  if (detail.readback.feature === "error") {
    return (
      <section className="rounded-lg border bg-background/70 p-4">
        <h3 className="text-sm font-semibold">Feature context</h3>
        <p className="mt-2 text-sm text-muted-foreground">Feature context unavailable. Check Doc Registry connectivity; no fallback feature summary is shown in live mode.</p>
      </section>
    )
  }

  if (!feature) return null

  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold">Feature context</h3>
            <Badge variant="outline" className="h-6 rounded-full font-mono text-[0.7rem]">
              {feature.key}
            </Badge>
            <Badge variant="secondary" className="h-6 rounded-full text-[0.7rem]">
              {stateText(feature.status)}
            </Badge>
          </div>
          <p className="mt-1 truncate text-sm text-muted-foreground">{feature.name}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {typeof feature.version === "number" ? <span>v{feature.version}</span> : null}
          {feature.canonicalArtifactId ? (
            <Link
              to={`/artifacts?artifact=${encodeURIComponent(feature.canonicalArtifactId)}`}
              className="text-foreground underline-offset-4 hover:underline"
            >
              canonical artifact
            </Link>
          ) : null}
        </div>
      </div>
    </section>
  )
}

// Delivery results bind to the immutable criterion ID. Text is display content
// and may change or repeat, so it must never establish identity.
function acceptanceCriterionVerdict(
  criterion: AcceptanceCriterionSummary,
  deliveryStatus: DeliveryStatusSummary | undefined,
): string | undefined {
  const criteria = deliveryStatus?.criteria ?? []
  if (criteria.length === 0) return undefined
  return criteria.find((row) => row.id === criterion.id)?.verdict
}

function acceptanceCriterionDone(criterion: AcceptanceCriterionSummary, deliveryStatus: DeliveryStatusSummary | undefined) {
  const verdict = acceptanceCriterionVerdict(criterion, deliveryStatus)
  return verdict ? verdict === "met" : criterion.done
}

function AcceptanceCriteriaSummary({ detail }: { detail: WorkItemDetailData }) {
  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-sm font-semibold">Acceptance criteria</h3>
        <Badge variant="outline" className="font-mono">
          {detail.acceptanceCriteria.filter((criterion) => acceptanceCriterionDone(criterion, detail.deliveryStatus)).length}/{detail.acceptanceCriteria.length}
        </Badge>
      </div>
      <div className="mt-3 grid gap-2">
        {detail.readback.acceptance === "error" ? (
          <p className="rounded-md border bg-card/55 p-3 text-sm leading-5 text-muted-foreground">
            Acceptance criteria unavailable. Check Doc Registry connectivity; no fallback acceptance criteria are shown in live mode.
          </p>
        ) : detail.acceptanceCriteria.length === 0 ? (
          <p className="rounded-md border bg-card/55 p-3 text-sm leading-5 text-muted-foreground">
            No acceptance criteria are recorded for this work item yet. Shape or update the governed artifact from the CLI or IDE workflow, then refresh the registry view.
          </p>
        ) : (
          detail.acceptanceCriteria.map((criterion) => {
            const verdict = acceptanceCriterionVerdict(criterion, detail.deliveryStatus)
            const done = verdict ? verdict === "met" : criterion.done
            return (
              <div key={criterion.id} className="flex items-start justify-between gap-3 rounded-md border bg-card/70 p-3">
                <div className="flex min-w-0 items-start gap-2 text-sm">
                  {done ? (
                    <CheckCircle2Icon className="mt-0.5 size-4 shrink-0 text-success" />
                  ) : (
                    <CircleDotIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  )}
                  <span className="leading-5">{criterion.text}</span>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  {verdict ? (
                    <Badge variant="outline" className={cn("border text-[11px]", toneClass(statusTone("state", verdict)))}>
                      {stateText(verdict)}
                    </Badge>
                  ) : null}
                  <Badge variant="outline" className="text-[11px]">
                    {criterion.source}
                  </Badge>
                </div>
              </div>
            )
          })
        )}
      </div>
    </section>
  )
}

function ContextSummary({
  item,
  detail,
}: {
  item: WorkItem
  detail: WorkItemDetailData
}) {
  return (
    <section className="grid gap-3">
      <div className="rounded-lg border bg-background/70 p-4">
        <h3 className="text-sm font-semibold">Work context</h3>
        <div className="mt-3 grid gap-3 text-sm sm:grid-cols-2">
          <div>
            <span className="text-xs text-muted-foreground">Route</span>
            <p className="mt-1">{item.route}</p>
          </div>
          <div>
            <span className="text-xs text-muted-foreground">Blocker</span>
            <p className="mt-1">{item.blocker}</p>
          </div>
        </div>
      </div>
      <TrackerLinks trackerLinks={detail.trackerLinks} status={detail.readback.trackerLinks} />
    </section>
  )
}

function TrackerLinks({
  trackerLinks,
  status,
}: {
  trackerLinks: TrackerLinkSummary[]
  status: "ready" | "loading" | "error"
}) {
  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <h3 className="text-sm font-semibold">Linked issues</h3>
      <div className="mt-3 grid gap-2">
        {status === "error" ? (
          <p className="text-sm text-muted-foreground">Linked issues unavailable. Check Doc Registry connectivity; no fallback tracker links are shown in live mode.</p>
        ) : trackerLinks.length === 0 ? (
          <p className="text-sm text-muted-foreground">No tracker links recorded.</p>
        ) : (
          trackerLinks.map((link) => (
            <a
              key={`${link.identifier}-${link.url}`}
              href={link.url}
              className="flex flex-wrap items-center justify-between gap-3 rounded-md border bg-card/70 p-3 text-sm transition-colors hover:bg-accent"
              target="_blank"
              rel="noreferrer"
            >
              <span className="flex min-w-0 items-center gap-2">
                <ExternalLinkIcon className="size-4 shrink-0 text-muted-foreground" />
                <span className="font-mono text-xs">{link.identifier}</span>
              </span>
              <span className="flex items-center gap-2">
                {link.trackerState ? <span className="text-xs text-muted-foreground">{link.trackerState}</span> : null}
              <Badge variant="outline" className={cn("border", toneClass(link.state === "opened" ? "success" : "neutral"))}>
                  {stateText(link.state)}
                </Badge>
              </span>
            </a>
          ))
        )}
      </div>
    </section>
  )
}

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

function ContextPackDetail({
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
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold">Handoff / Context Pack</h3>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
              No Context Pack is available yet. Continue preparation in your IDE or CLI, then return here to inspect the handoff.
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <ActionTooltip content="Opens governance agent. Does not create a Context Pack or change workflow state.">
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="rounded-md"
                onClick={() =>
                  runGovernanceAgentPrompt(
                    `For ${item.key} (${item.title}), explain what blocks a governed coding-agent handoff. Check scope, acceptance criteria, route, gate state, and whether a Context Pack or approved source artifact is available.`,
                  )
                }
              >
                <BotIcon data-icon="inline-start" />
                Ask what blocks handoff
              </Button>
            </ActionTooltip>
          </div>
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

// Worst-state ordering for the collapsed gate summary line and its tone.
const gateStateOrder = ["fail", "needs_human_review", "warn", "pending", "pass", "not_applicable"]
const toneRank: Record<Tone, number> = { success: 0, neutral: 1, warning: 2, danger: 3 }

function gateStateSummary(nextActions: NextActionSummary[]): { label: string; tone: Tone } {
  const counts = new Map<string, number>()
  for (const action of nextActions) {
    counts.set(action.state, (counts.get(action.state) ?? 0) + 1)
  }
  const rank = (state: string) => {
    const index = gateStateOrder.indexOf(state)
    return index === -1 ? gateStateOrder.length : index
  }
  const states = [...counts.keys()].sort((a, b) => rank(a) - rank(b))
  const label = [
    `${nextActions.length} ${nextActions.length === 1 ? "gate" : "gates"}`,
    ...states.map((state) => `${counts.get(state)} ${stateText(state).toLowerCase()}`),
  ].join(" · ")
  const tone = states.reduce<Tone>((worst, state) => {
    const candidate = statusTone("state", state)
    return toneRank[candidate] > toneRank[worst] ? candidate : worst
  }, "success")
  return { label, tone }
}

function GateActionRows({
  nextActions,
  status,
}: {
  nextActions: NextActionSummary[]
  status: "ready" | "loading" | "error"
}) {
  const [expanded, setExpanded] = useState(false)
  const summary = gateStateSummary(nextActions)

  return (
    <div className="mt-4 grid gap-2">
      {status === "error" ? (
        <p className="text-sm text-muted-foreground">Gate next actions unavailable. Check Doc Registry connectivity; no fallback next actions are shown in live mode.</p>
      ) : nextActions.length === 0 ? (
        <p className="text-sm text-muted-foreground">No next actions recorded.</p>
      ) : (
        <>
          <button
            type="button"
            aria-expanded={expanded}
            className="flex w-fit items-center gap-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            onClick={() => setExpanded((current) => !current)}
          >
            <ChevronRightIcon className={cn("size-3.5 transition-transform", expanded && "rotate-90")} />
            <Badge variant="outline" className={cn("border", toneClass(summary.tone))}>
              {summary.label}
            </Badge>
          </button>
          {expanded
            ? nextActions.map((action) => (
                <div
                  key={`${action.gate}-${action.state}`}
                  className={cn("rounded-md border bg-card/70 p-3", action.state === "not_applicable" && "opacity-60")}
                >
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span className="text-xs font-medium">{gateText(action.gate)}</span>
                    <Badge variant="outline" className={cn("border", toneClass(statusTone("state",action.state)))}>
                      {stateText(action.state)}
                    </Badge>
                  </div>
                  {gateChecks(action.gate) ? (
                    <p className="mt-1 text-xs text-muted-foreground">{gateChecks(action.gate)}</p>
                  ) : null}
                  <p className="mt-2 text-sm leading-5 text-muted-foreground">{action.hint}</p>
                </div>
              ))
            : null}
        </>
      )}
    </div>
  )
}

// Registry gate-run history repeats identical rows per refresh; default to the
// latest run per gate and keep the chronological list behind Show all runs.
function latestGateRuns(gateRuns: GateRunSummary[]): GateRunSummary[] {
  const latestByGate = new Map<string, GateRunSummary>()
  for (const run of gateRuns) {
    const current = latestByGate.get(run.gate)
    if (!current) {
      latestByGate.set(run.gate, run)
      continue
    }
    if (run.createdAt && current.createdAt && new Date(run.createdAt).getTime() > new Date(current.createdAt).getTime()) {
      latestByGate.set(run.gate, run)
    }
  }
  return [...latestByGate.values()]
}

function GateRunRows({
  gateRuns,
  status,
}: {
  gateRuns: GateRunSummary[]
  status: "ready" | "loading" | "error"
}) {
  const [showAllRuns, setShowAllRuns] = useState(false)
  const latestRuns = latestGateRuns(gateRuns)
  const visibleRuns = showAllRuns ? gateRuns : latestRuns

  return (
    <div className="mt-4 grid gap-2">
      {status === "error" ? (
        <p className="text-sm text-muted-foreground">Gate run history unavailable. Check Doc Registry connectivity; no fallback gate runs are shown in live mode.</p>
      ) : gateRuns.length === 0 ? (
        <p className="text-sm text-muted-foreground">No persisted gate runs yet.</p>
      ) : (
        <>
          {visibleRuns.map((run) => (
            <div key={run.id} className="rounded-md border bg-card/70 p-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2">
                  <ClockIcon className="size-4 text-muted-foreground" />
                  <span className="text-xs font-medium">{gateText(run.gate)}</span>
                </div>
                <Badge variant="outline" className={cn("border", toneClass(statusTone("state",run.state)))}>
                  {stateText(run.state)}
                </Badge>
              </div>
              {gateChecks(run.gate) ? (
                <p className="mt-1 text-xs text-muted-foreground">{gateChecks(run.gate)}</p>
              ) : null}
              <p className="mt-2 text-sm leading-5 text-muted-foreground">{run.hint}</p>
              <GateEvidenceWhy evidence={run.evidence} executor={run.executor} />
              {run.createdAt ? <p className="mt-2 font-mono text-[11px] text-muted-foreground">{formatDateTime(run.createdAt)}</p> : null}
            </div>
          ))}
          {gateRuns.length > latestRuns.length ? (
            <button
              type="button"
              aria-expanded={showAllRuns}
              className="flex w-fit items-center gap-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
              onClick={() => setShowAllRuns((current) => !current)}
            >
              <ChevronRightIcon className={cn("size-3.5 transition-transform", showAllRuns && "rotate-90")} />
              {showAllRuns ? "Show latest run per gate" : "Show all runs"}
            </button>
          ) : null}
        </>
      )}
    </div>
  )
}

function GateSummary({ item, detail }: { item: WorkItem; detail: WorkItemDetailData }) {
  const states = detail.nextActions.map((action) => action.state.trim())
  const gateState: WorkItem["gate"] = states.length === 0
    ? item.gate
    : states.includes("fail")
      ? "fail"
      : states.some((state) => !["pass", "not_applicable"].includes(state))
        ? "pending"
        : "pass"
  return (
    <section className="grid gap-3">
      <div className="rounded-lg border bg-background/70 p-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold">Gate state</h3>
            <p className="mt-1 text-xs text-muted-foreground">What each gate checked and why, per run.</p>
          </div>
          <Badge variant="outline" className={cn("border", toneClass(statusTone("gate", gateState)))}>
            {gateText(gateState)}
          </Badge>
        </div>
        <GateActionRows nextActions={detail.nextActions} status={detail.readback.nextActions} />
      </div>
      <div className="rounded-lg border bg-background/70 p-4">
        <h3 className="text-sm font-semibold">Gate run history</h3>
        <GateRunRows gateRuns={detail.gateRuns} status={detail.readback.gateRuns} />
      </div>
    </section>
  )
}


function RepositoryObservationSummary({ detail }: { detail: WorkItemDetailData }) {
  if (detail.readback.deliveryLinks === "error") {
    return (
      <section className="rounded-lg border bg-background/70 p-4">
        <h3 className="text-sm font-semibold">Repository observation</h3>
        <p className="mt-2 text-sm text-muted-foreground">Repository links unavailable. Check Doc Registry connectivity.</p>
      </section>
    )
  }
  if (detail.deliveryLinks.length === 0) return null

  const latestCompletionHead = detail.deliveryStatus?.gitReceipt?.headRevision
  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <h3 className="text-sm font-semibold">Repository observation</h3>
      <div className="mt-3 grid gap-2">
        {detail.deliveryLinks.map((link) => (
          <RepositoryObservationLink key={`${link.externalKey}-${link.url}`} link={link} latestCompletionHead={latestCompletionHead} />
        ))}
      </div>
    </section>
  )
}

function RepositoryObservationLink({ link, latestCompletionHead }: { link: DeliveryLinkSummary; latestCompletionHead?: string }) {
  const observation = repositoryObservation(link, latestCompletionHead)
  const copy = observation === "open"
    ? "PR/MR linked; merge it before repository observation can corroborate delivery."
    : observation === "exact"
      ? "Submitted commit observed on merged PR/MR."
      : observation === "stale"
        ? "PR/MR merged, but it does not match the latest submitted commit. Update or resubmit, then merge the matching head."
        : observation === "missing-receipt"
          ? "PR/MR merged; submit a delivery receipt so SpecGate can compare the exact head."
          : "PR/MR closed without merge; open or link a replacement."
  const tone: Tone = observation === "exact" ? "success" : observation === "open" || observation === "missing-receipt" ? "warning" : "danger"

  return (
    <div className="rounded-md border bg-card/70 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <a href={link.url} target="_blank" rel="noreferrer" className="inline-flex min-w-0 items-center gap-2 text-sm font-medium hover:underline">
          <ExternalLinkIcon className="size-4 shrink-0" />
          <span className="truncate">{link.title}</span>
        </a>
        <Badge variant="outline" className={cn("border", toneClass(tone))}>{stateText(link.state)}</Badge>
      </div>
      <p className="mt-2 text-sm leading-6 text-muted-foreground">{copy}</p>
      <p className="mt-2 break-all font-mono text-[11px] text-muted-foreground">{link.url}</p>
    </div>
  )
}

function DeliverySummary({ item, detail }: { item: WorkItem; detail: WorkItemDetailData }) {
  const deliveryRun = detail.gateRuns.find((run) => run.gate === "delivery_review")
  const deliveryStatus = detail.deliveryStatus
  const evidenceState = deliveryStatus?.evidenceVerdict ??
    (deliveryStatus?.executor === "human" ? undefined : deliveryStatus?.verdict) ??
    deliveryRun?.state ??
    item.delivery
  const needsReview = deliveryStatus?.reasonCode === "policy_unavailable" ||
    deliveryStatus?.reasonCode === "delivery_review_outdated" ||
    evidenceState === "fail" ||
    evidenceState === "needs_human_review" ||
    evidenceState === "needs_changes" ||
    (!deliveryStatus?.found && item.delivery === "needs_changes")
  const trust = deliveryStatus?.found ? summarizeDeliveryTrust(deliveryStatus) : undefined
  const accepted = trust?.decision === "Accepted"
  const rejected = trust?.decision === "Rejected"

  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-2">
          {accepted ? (
            <CheckCircle2Icon aria-label="Delivery accepted" className="size-4 text-success" />
          ) : rejected ? (
            <AlertTriangleIcon aria-label="Delivery rejected" className="size-4 text-warning" />
          ) : needsReview ? (
            <AlertTriangleIcon aria-label="Delivery evidence has gaps" className="size-4 text-warning" />
          ) : (
            <ShieldCheckIcon aria-label="Delivery evidence available for review" className="size-4 text-primary" />
          )}
          <h3 className="text-sm font-semibold">Delivery review</h3>
        </div>
        <Badge variant="outline" className="rounded-full">
          read-only
        </Badge>
      </div>
      {detail.readback.delivery === "error" ? (
        <p className="mt-3 rounded-md border bg-card/70 p-3 text-sm leading-6 text-muted-foreground">
          Delivery review readback unavailable. Check Doc Registry connectivity; no fallback delivery review detail is shown in live mode.
        </p>
      ) : (
        <>
          {!deliveryStatus && (
            <p className="mt-3 text-sm text-muted-foreground">
              Latest gate state: <span className="font-medium text-foreground">
                {deliveryRun ? stateText(deliveryRun.state) : deliveryText(item.delivery)}
              </span>
            </p>
          )}
          {deliveryStatus?.hint ? (
            <p className="mt-3 text-sm leading-6 text-muted-foreground">{deliveryStatus.hint}</p>
          ) : deliveryRun?.hint ? (
            <p className="mt-3 text-sm leading-6 text-muted-foreground">{deliveryRun.hint}</p>
          ) : null}
          {deliveryStatus ? <DeliveryStatusDetails status={deliveryStatus} /> : null}
        </>
      )}
    </section>
  )
}

function DeliveryStatusDetails({ status }: { status: DeliveryStatusSummary }) {
  if (!status.found) {
    return (
      <p className="mt-3 rounded-md border bg-card/70 p-3 text-sm leading-6 text-muted-foreground">
        No persisted delivery review result is available yet. Triggering review remains a CLI or agents-service workflow.
      </p>
    )
  }

  const trust = summarizeDeliveryTrust(status)

  return (
    <div className="mt-3 grid gap-3">
      <div className="grid gap-2 sm:grid-cols-2">
        <DeliveryTrustFact label="Evidence" value={trust.evidence} />
        <DeliveryTrustFact label="Assurance" value={trust.assurance} />
        <DeliveryTrustFact label="Decision" value={trust.decision} />
        <DeliveryTrustFact label="Receipt" value={trust.receipt} />
      </div>
      <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
        <Badge variant="secondary" className="rounded-full">{trust.reviewer}</Badge>
        {trust.decisionActor ? <Badge variant="secondary" className="rounded-full">{trust.decisionActor}</Badge> : null}
        {trust.peerReview ? <Badge variant="secondary" className="rounded-full">{trust.peerReview}</Badge> : null}
        {status.reviewedAt ? <Badge variant="secondary" className="rounded-full">{formatDateTime(status.reviewedAt)}</Badge> : null}
        {typeof status.confidence === "number" ? (
          <Badge variant="secondary" className="rounded-full">{Math.round(status.confidence * 100)}% reviewer confidence</Badge>
        ) : null}
      </div>
      {status.executor?.trim() === "human" && status.verdict?.trim() !== "pass" && status.note ? (
        <p className="rounded-md border border-warning/40 bg-warning/10 p-3 text-sm leading-6">
          <span className="font-semibold">Requested changes:</span> {status.note}
        </p>
      ) : null}
      {trust.modelReviewed ? (
        <p className="rounded-md border bg-card/70 p-3 text-xs leading-5 text-muted-foreground">
          A model review evaluates submitted evidence; it does not verify the code, replace CI, or make the human acceptance decision.
        </p>
      ) : null}
      {status.outstandingMd ? (
        <div className="max-h-64 overflow-y-auto rounded-md border bg-card/70 p-3">
          <h4 className="mb-2 text-xs font-semibold text-muted-foreground">Outstanding review feedback</h4>
          <MarkdownText content={status.outstandingMd} />
        </div>
      ) : null}
      {status.criteria.length > 0 ? (
        <div>
          <h4 className="text-xs font-semibold text-muted-foreground">Criteria verdicts</h4>
          <div className="mt-2 grid gap-2">
            {status.criteria.map((criterion) => (
              <div key={criterion.id} className="rounded-md border bg-card/70 p-3">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div className="min-w-0">
                    <p className="font-mono text-[11px] text-muted-foreground">{criterion.id}</p>
                    <p className="mt-1 text-sm leading-5">{criterion.text}</p>
                  </div>
                  <Badge variant="outline" className={cn("border", toneClass(statusTone("state",criterion.verdict)))}>
                    {stateText(criterion.verdict)}
                  </Badge>
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-2">
                  <Badge variant="secondary" className="rounded-full">{trustTierLabel(criterion.trustTier)}</Badge>
                  {criterion.verificationBinding ? (
                    <span className="break-all font-mono text-[11px] text-muted-foreground">{criterion.verificationBinding}</span>
                  ) : null}
                </div>
                {criterion.why ? <p className="mt-2 text-xs leading-5 text-muted-foreground">{criterion.why}</p> : null}
              </div>
            ))}
          </div>
        </div>
      ) : null}
      {status.checks.length > 0 ? (
        <div>
          <h4 className="text-xs font-semibold text-muted-foreground">Automated checks</h4>
          <div className="mt-2 grid gap-2">
            {status.checks.map((check) => (
              <div key={`${check.name}-${check.status}-${check.detail ?? ""}`} className="grid gap-2 rounded-md border bg-card/70 p-3 text-sm sm:grid-cols-[minmax(0,1fr)_auto]">
                <div className="min-w-0">
                  <p className="truncate font-medium">{check.name}</p>
                  {check.detail ? <p className="mt-1 text-xs leading-5 text-muted-foreground">{check.detail}</p> : null}
                </div>
                <Badge variant="outline" className={cn("w-fit border", toneClass(statusTone("state",check.status)))}>
                  {stateText(check.status)}
                </Badge>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}

function DeliveryTrustFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border bg-card/70 p-3">
      <p className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-1 text-sm font-medium leading-5">{value}</p>
    </div>
  )
}

function ActivityList({ item }: { item: WorkItem }) {
  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <h3 className="text-sm font-semibold">Activity</h3>
      <div className="mt-3 grid gap-2">
        {item.activity.map((activity) => (
          <div key={activity} className="flex items-center gap-2 text-sm text-muted-foreground">
            <ClockIcon className="size-4" />
            {activity}
          </div>
        ))}
      </div>
    </section>
  )
}
