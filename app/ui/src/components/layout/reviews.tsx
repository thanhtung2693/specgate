// Reviews domain: review queue helpers, artifact-decision/feedback dialogs, and the
// Reviews page. Extracted from app-shell.
import { EyeIcon, SearchIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { Link, useSearchParams } from "react-router-dom"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { type ArtifactSummary } from "@/data/artifacts"
import { useArtifactDecisionQueue } from "@/data/reviews"
import { type WorkboardView } from "@/data/workboard"
import { type WorkItem } from "@/data/workspace"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { isDeliveredWorkItem, readableKey, reviewTone, toneClass } from "./shared"
import { ArtifactDetailDialog } from "./artifacts"

type ReviewFilter = "all" | "ready" | "needs_changes" | "gate_failed"

export function isReviewItem(item: WorkItem) {
  return !isDeliveredWorkItem(item) &&
    (item.delivery === "passed" || item.deliveryVerdict === "pass" || item.delivery === "needs_changes" || item.gate === "fail")
}

function reviewItems(workItems: WorkItem[]) {
  return workItems.filter(isReviewItem)
}

function reviewFilterCount(workItems: WorkItem[], filter: ReviewFilter) {
  return reviewItems(workItems).filter((item) => reviewFilterMatches(item, filter)).length
}

function reviewFilterMatches(item: WorkItem, filter: ReviewFilter) {
  if (filter === "all") return true
  if (filter === "ready") return item.delivery === "passed" || item.deliveryVerdict === "pass"
  if (filter === "needs_changes") return item.delivery === "needs_changes" || Boolean(item.deliveryVerdict && item.deliveryVerdict !== "pass")
  return item.gate === "fail"
}

function reviewReason(item: WorkItem) {
  if (item.deliveryVerdict && item.deliveryVerdict !== "pass") {
    return item.deliveryHint || "Delivery review needs attention."
  }
  if (item.delivery === "needs_changes") {
    return item.blocker !== "none" ? item.blocker : "Delivery review needs changes."
  }
  if (item.gate === "fail") return "A required gate failed."
  if (item.delivery === "passed") return "Delivery evidence is ready for human review."
  return item.blocker !== "none" ? item.blocker : "Needs human review."
}

function reviewLabel(item: WorkItem) {
  if (item.deliveryVerdict === "needs_human_review") return "Needs review"
  if (item.deliveryVerdict === "fail") return "Review failed"
  if (item.deliveryVerdict === "needs_changes") return "Needs changes"
  if (item.deliveryVerdict === "pass") return "Ready for human review"
  if (item.delivery === "needs_changes") return "Needs changes"
  if (item.gate === "fail") return "Gate failed"
  if (item.delivery === "passed") return "Ready for human review"
  return "Review"
}

function reviewActionLabel(item: WorkItem) {
  if ((item.deliveryVerdict && item.deliveryVerdict !== "pass") || item.delivery === "needs_changes" || item.gate === "fail") return "Inspect review gaps"
  if (item.delivery === "passed") return "Inspect review outcome"
  return "Inspect review"
}

function reviewSearchMatches(item: WorkItem, search: string) {
  const query = search.trim().toLowerCase()
  if (!query) return true
  return [
    item.key,
    item.title,
    item.agent,
    item.route,
    reviewLabel(item),
    reviewReason(item),
  ].some((value) => value.toLowerCase().includes(query))
}

function ArtifactDecisionQueueSection({
  artifacts,
  status,
  onInspect,
}: {
  artifacts: ArtifactSummary[]
  status: "ready" | "loading" | "error"
  onInspect: (artifact: ArtifactSummary) => void
}) {
  return (
    <section className="overflow-hidden rounded-lg border bg-card">
      <div className="flex items-center gap-2 border-b px-3 py-2.5">
        <h2 className="text-sm font-semibold">Artifact decisions</h2>
        <Badge variant="outline" className="font-mono">{artifacts.length}</Badge>
      </div>
      {status === "loading" ? <p className="p-4 text-sm text-muted-foreground">Loading artifact decisions…</p> : null}
      {status === "error" ? <p className="p-4 text-sm text-muted-foreground">Artifact decisions unavailable. Check Doc Registry connectivity, then retry.</p> : null}
      {status === "ready" && artifacts.length === 0 ? <p className="p-4 text-sm text-muted-foreground">No artifacts need a decision.</p> : null}
      {artifacts.length > 0 ? (
        <div className="grid divide-y">
          {artifacts.map((artifact) => (
            <div key={artifact.id} className="grid gap-2 px-3 py-3 text-sm sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate font-medium">{artifact.featureName}</span>
                  <Badge variant="outline">{readableKey(artifact.status)}</Badge>
                  <Badge variant="secondary" className="font-mono">{artifact.version}</Badge>
                </div>
                <p className="mt-1 truncate font-mono text-xs text-muted-foreground">{artifact.id}</p>
              </div>
              <Button variant="outline" size="sm" className="min-h-11 rounded-md sm:min-h-8" onClick={() => onInspect(artifact)}>
                Inspect artifact
              </Button>
            </div>
          ))}
        </div>
      ) : null}
    </section>
  )
}

function reviewTargetPath(item: WorkItem) {
  return `/work/${item.key}?tab=verification`
}

export function ReviewsPage({ workboard, reviewer, workspaceId }: { workboard: WorkboardView; reviewer: string; workspaceId: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [activeFilter, setActiveFilter] = useState<ReviewFilter>("all")
  const [reviewSearch, setReviewSearch] = useState("")
  const [selectedArtifact, setSelectedArtifact] = useState<ArtifactSummary | undefined>(undefined)
  const artifactDecisions = useArtifactDecisionQueue(workspaceId)

  useEffect(() => {
    const artifactId = searchParams.get("artifact")
    if (artifactId) setSelectedArtifact(artifactDecisions.items.find((artifact) => artifact.id === artifactId))
  }, [artifactDecisions.items, searchParams])

  const workItems = workboard.workItems
  const itemsNeedingReview = reviewItems(workItems)
  const visibleItems = itemsNeedingReview.filter(
    (item) => reviewFilterMatches(item, activeFilter) && reviewSearchMatches(item, reviewSearch),
  )
  const reviewCount = itemsNeedingReview.length
  const filterOptions: Array<{ id: ReviewFilter; label: string }> = [
    { id: "all", label: "All" },
    { id: "ready", label: "Ready for decision" },
    { id: "needs_changes", label: "Needs changes" },
    { id: "gate_failed", label: "Gate failed" },
  ]
  return (
    <div className="grid gap-4">
      <section className="grid gap-3.5">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">
              {reviewCount === 1 ? "1 item needs review" : `${reviewCount} items need review`}
            </h2>
            <p className="mt-1 text-xs text-muted-foreground">Inspect delivery evidence ready for a human decision, failed gates, and evidence gaps.</p>
          </div>
        </div>
        {workboard.status === "error" ? (
          <p className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-muted-foreground">
            Live review data is unavailable. Check Doc Registry connectivity, then reload this page; no fallback review rows are shown in live mode.
          </p>
        ) : null}
        {workboard.status === "loading" || reviewCount > 0 ? (
          <>
            <div className="grid gap-3">
              <div className="grid gap-2">
                <label htmlFor="review-search" className="text-xs font-medium text-muted-foreground">
                  Search reviews
                </label>
                <div className="relative">
                  <SearchIcon className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    id="review-search"
                    aria-label="Search reviews"
                    value={reviewSearch}
                    onChange={(event) => setReviewSearch(event.target.value)}
                    placeholder="Search by work item, reason, or Context Pack"
                    className="pl-8"
                  />
                </div>
              </div>
            </div>
            <div className="flex flex-wrap gap-2 border-y py-3">
              {filterOptions.map((filter) => {
                const selected = activeFilter === filter.id
                return (
                  <Button
                    key={filter.id}
                    type="button"
                    variant={selected ? "secondary" : "outline"}
                    size="sm"
                    className={cn("rounded-md", selected ? "bg-primary/14 ring-1 ring-primary/25" : "")}
                    onClick={() => setActiveFilter(filter.id)}
                  >
                    {filter.label}
                    <Badge variant={selected ? "secondary" : "outline"} className="ml-1 font-mono">
                      {reviewFilterCount(workItems, filter.id)}
                    </Badge>
                  </Button>
                )
              })}
            </div>
          </>
        ) : null}
      </section>

      <ArtifactDecisionQueueSection
        artifacts={artifactDecisions.items}
        status={artifactDecisions.status}
        onInspect={setSelectedArtifact}
      />

      <section className="overflow-hidden rounded-lg border bg-card">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-3 py-2.5">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-sm font-semibold">Delivery evidence</h2>
            <Badge variant="outline" className="font-mono" aria-label={`${visibleItems.length} visible items`}>
              {visibleItems.length}
            </Badge>
            <p className="text-xs text-muted-foreground">visible items</p>
          </div>
          {activeFilter !== "all" || reviewSearch ? (
            <Button
              variant="ghost"
              size="sm"
              className="rounded-md"
              onClick={() => {
                setActiveFilter("all")
                setReviewSearch("")
              }}
            >
              Clear filters
            </Button>
          ) : null}
        </div>
        {visibleItems.length === 0 ? (
          <div className="p-6 text-center">
            <h3 className="text-sm font-semibold">
              {workboard.status === "error"
                ? "Delivery evidence unavailable"
                : reviewCount === 0
                  ? "No delivery evidence needs review."
                  : "No reviews in this view"}
            </h3>
            <p className="mx-auto mt-2 max-w-md text-sm text-muted-foreground">
              {workboard.status === "error"
                ? "Check Doc Registry connectivity, then reload this page."
                : reviewCount === 0
                ? "Open Work to inspect the full queue."
                : "Change the filter or open Work to inspect the full queue."}
            </p>
            <Button variant="outline" size="sm" className="mt-4 rounded-md" asChild>
              <Link to="/work">Open Work</Link>
            </Button>
          </div>
        ) : (
          <div>
            <table className="w-full table-fixed text-left text-xs" aria-label="Delivery evidence reviews">
              <caption className="sr-only">Work items with delivery evidence requiring review</caption>
              <colgroup>
                <col className="w-[40%]" />
                <col className="w-[33%]" />
                <col className="w-[15%]" />
                <col className="w-[12%]" />
              </colgroup>
              <thead className="hidden border-b bg-muted/40 text-xs text-muted-foreground sm:table-header-group">
                <tr>
                  <th scope="col" className="px-3 py-2 font-medium">Item</th>
                  <th scope="col" className="px-3 py-2 font-medium">Reason</th>
                  <th scope="col" className="px-3 py-2 font-medium">Updated</th>
                  <th scope="col" className="px-3 py-2 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="grid sm:table-row-group">
                {visibleItems.map((item) => (
                  <tr
                    key={item.key}
                    className="grid gap-2 border-b px-3 py-3 align-middle transition-colors last:border-b-0 hover:bg-muted/35 sm:table-row sm:px-0 sm:py-0"
                  >
                    <td className="min-w-0 sm:px-3 sm:py-2">
                      <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <Link
                          to={reviewTargetPath(item)}
                          className="whitespace-nowrap font-mono font-semibold text-foreground hover:underline"
                          onClick={(event) => event.stopPropagation()}
                        >
                          {item.key}
                        </Link>
                        <Badge variant="outline" className={cn("shrink-0 border text-[0.68rem]", toneClass(reviewTone(item)))}>
                          {reviewLabel(item)}
                        </Badge>
                        <Badge variant="secondary" className="shrink-0 text-[0.68rem]">{item.route}</Badge>
                      </div>
                      <div className="mt-1 flex min-w-0 items-center gap-2">
                        <span className="block min-w-0 max-w-[460px] truncate text-left text-muted-foreground">
                          {item.title}
                        </span>
                      </div>
                    </td>
                    <td className="grid gap-1 leading-5 text-muted-foreground sm:table-cell sm:px-3 sm:py-2" title={reviewReason(item)}>
                      <span className="font-medium text-foreground sm:hidden">Reason</span>
                      <span className="line-clamp-2">{reviewReason(item)}</span>
                    </td>
                    <td className="flex justify-between gap-3 truncate text-muted-foreground sm:table-cell sm:px-3 sm:py-2" title={formatDateTime(item.updated)}>
                      <span className="font-medium text-foreground sm:hidden">Updated</span>
                      {formatRelativeTime(item.updated)}
                    </td>
                    <td className="sm:px-3 sm:py-2">
                      <div className="flex justify-start gap-2 sm:justify-end">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button size="icon-sm" className="rounded-md bg-foreground text-background hover:bg-foreground/85" asChild>
                              <Link
                                to={reviewTargetPath(item)}
                                aria-label={reviewActionLabel(item)}
                                onClick={(event) => event.stopPropagation()}
                              >
                                <EyeIcon />
                              </Link>
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>{reviewActionLabel(item)}</TooltipContent>
                        </Tooltip>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <ArtifactDetailDialog
        artifact={selectedArtifact}
        open={selectedArtifact !== undefined}
        reviewer={reviewer}
        workspaceId={workspaceId}
        mode="review"
        onOpenChange={(open) => {
          if (!open) {
            setSelectedArtifact(undefined)
            const next = new URLSearchParams(searchParams)
            next.delete("artifact")
            setSearchParams(next, { replace: true })
          }
        }}
        onDecided={artifactDecisions.refresh}
      />
    </div>
  )
}
