// Work-item detail view extracted from app-shell.tsx: overview, handoff,
// verification, and activity tabs for a selected work item.

import { CopyIcon, ShieldCheckIcon } from "lucide-react"
import { useState } from "react"
import { useSearchParams } from "react-router-dom"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useWorkItemDetail } from "@/data/workboard"
import type { WorkItem } from "@/data/workspace"
import { cn } from "@/lib/utils"
import { isDeliveredWorkItem, stateText, statusTone, toneClass } from "../shared"
import { copyText, PolicyExplanationSection } from "../shared-ui"
import { workItemStatusBadge } from "../work-page"

import {
  AcceptanceCriteriaSummary,
  ActivityList,
  ContextPackDetail,
  ContextSummary,
  DeliverySummary,
  FeatureOverview,
  FreshnessSignalsSummary,
  GateSummary,
  RepositoryObservationSummary,
} from "./item-detail-sections"

export function WorkItemDetail({
  item,
  workspaceId,
  hydrateDetail,
  refreshGeneration,
  reviewer,
  onDeliveryDecided,
}: {
  item: WorkItem
  workspaceId: string
  hydrateDetail: boolean
  refreshGeneration: number
  reviewer: string
  onDeliveryDecided: () => void
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
              <DeliverySummary item={item} detail={detail} reviewer={reviewer} workspaceId={workspaceId} onDecided={onDeliveryDecided} />
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
