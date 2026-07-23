import {
  ArrowLeftIcon,
  ChevronRightIcon,
  KanbanSquareIcon,
  ListIcon,
  RotateCwIcon,
  SearchIcon,
} from "lucide-react"
import { useMemo, useState } from "react"
import { Link } from "react-router-dom"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { type WorkboardData, type WorkboardView } from "@/data/workboard"
import type { Lane, WorkItem } from "@/data/workspace"
import { useGovernanceStats } from "@/data/stats"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import {
  deliveryText,
  gateText,
  isDeliveredWorkItem,
  readableKey,
  stateText,
  statusTone,
  toneClass,
  type Tone,
} from "./shared"
import { copyText } from "./shared-ui"
import { isReviewItem } from "./reviews"

function governanceSignalKindLabel(kind: string) {
  return (
    {
      gate_catch: "Gate signal",
      review_catch: "Review signal",
      ambiguity_block: "Blocked-ambiguity report",
    }[kind] ?? readableKey(kind)
  )
}

function GovernanceStatsContent({ workspaceId }: { workspaceId?: string }) {
  const stats = useGovernanceStats(workspaceId)

  if (stats.status === "loading") {
    return <p className="text-xs text-muted-foreground">Loading governance stats…</p>
  }
  if (stats.status === "unconfigured") {
    return <p className="text-xs text-muted-foreground">Stats need a configured Doc Registry (VITE_DOC_REGISTRY_URL).</p>
  }
  if (stats.status === "workspace_required") {
    return <p className="text-xs text-muted-foreground">Select a workspace to view governance stats.</p>
  }
  if (stats.status === "error") {
    return <p className="text-xs text-muted-foreground">Stats unavailable. Check Doc Registry connectivity.</p>
  }

  const item = stats.item
  if (!item || item.reviewedItems === 0) {
    return <p className="text-xs text-muted-foreground">Not enough data yet — run a few governed work items first.</p>
  }

  const yieldPercent = Math.round((item.firstPass / item.reviewedItems) * 100)
  const metrics = [
    {
      label: "First-pass yield",
      value: `${yieldPercent}%`,
      detail: `${item.firstPass}/${item.reviewedItems} reviewed`,
      tone: yieldPercent >= 80 ? "success" : yieldPercent >= 50 ? "warning" : "danger",
    },
    { label: "Gate signals (pre-build)", value: `${item.gateCatchesPreBuild}`, detail: "before handoff", tone: "neutral" },
    {
      label: "Review signals (post-build)",
      value: `${item.reviewCatchesPostBuild}`,
      detail: `${item.reviewCatchesFixed} later passed review`,
      tone: "neutral",
    },
    { label: "Rework runs", value: `${item.rework}`, detail: `${item.itemsWithRework} items`, tone: "neutral" },
    { label: "Ambiguity reports", value: `${item.ambiguityBlocks}`, detail: "agent blocked", tone: "neutral" },
    {
      label: "Avg cycle time",
      value: item.cycleTimeItems > 0 ? `${item.cycleTimeAvgHours.toFixed(1)}h` : "—",
      detail: `${item.cycleTimeItems} items`,
      tone: "neutral",
    },
  ]

  return (
    <div className="grid gap-3">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {metrics.map((metric) => (
          <div key={metric.label} className="rounded-lg border bg-card px-4 py-3">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-medium text-muted-foreground">{metric.label}</span>
              <Badge variant="outline" className={cn("border text-[0.65rem]", toneClass(metric.tone))}>
                {metric.detail}
              </Badge>
            </div>
            <div className="mt-1.5 font-mono text-xl font-semibold tracking-tight tabular-nums">{metric.value}</div>
          </div>
        ))}
      </div>
      {item.ledger.length > 0 ? (
        <div className="rounded-lg border bg-card">
          <div className="border-b px-4 py-2 text-xs font-medium text-muted-foreground">Signal ledger</div>
          <ul className="divide-y">
            {item.ledger.map((entry, index) => (
              <li
                key={`${entry.changeRequestKey}-${entry.occurredAt}-${index}`}
                className="flex items-center gap-2 px-4 py-2 text-xs"
              >
                <span className="w-16 shrink-0 text-muted-foreground">{formatRelativeTime(entry.occurredAt)}</span>
                {!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(entry.changeRequestKey) ? (
                  <Link
                    to={`/work/${entry.changeRequestKey}?tab=verification`}
                    className="shrink-0 font-mono font-semibold hover:underline"
                  >
                    {entry.changeRequestKey}
                  </Link>
                ) : (
                  // Server falls back to the raw CR id when the key is empty;
                  // ids do not resolve as work routes, so render text only.
                  <span className="shrink-0 font-mono font-semibold">{entry.changeRequestKey}</span>
                )}
                <Badge variant="secondary" className="shrink-0 text-[0.65rem]">
                  {governanceSignalKindLabel(entry.kind)}
                </Badge>
                <span className="min-w-0 truncate text-muted-foreground">{entry.detail}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">No governance signals recorded in this window.</p>
      )}
    </div>
  )
}

// Read-only governance-value readout (GET /api/v1/stats). Collapsed by
// default; the content mounts only after expansion so the board never pays
// for stats on load.
function GovernanceStatsSection({ workspaceId }: { workspaceId?: string }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <section aria-label="Governance stats" className="grid gap-3">
      <button
        type="button"
        aria-expanded={expanded}
        className="flex w-fit items-center gap-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        onClick={() => setExpanded((current) => !current)}
      >
        <ChevronRightIcon className={cn("size-3.5 transition-transform", expanded && "rotate-90")} />
        Governance stats · last 30 days
      </button>
      {expanded ? <GovernanceStatsContent workspaceId={workspaceId} /> : null}
    </section>
  )
}

const queueViews = [
  { id: "needs_attention", label: "Action queue" },
  { id: "ready_for_pickup", label: "Ready for pickup" },
  { id: "needs_review", label: "Needs review" },
  { id: "blocked", label: "Blocked" },
  { id: "delivered", label: "Delivered" },
  { id: "all_work", label: "All work" },
] as const

type QueueViewId = (typeof queueViews)[number]["id"]
type WorkDisplayMode = "board" | "list"

const attentionReasonLabels = ["Gate failures", "Delivery failures", "Pending approvals", "Agent handoffs"] as const
const reasonFilterViews = new Set<QueueViewId>(["needs_attention", "blocked"])

function queueViewMatches(item: WorkItem, view: QueueViewId) {
  if (view === "delivered") return isDeliveredWorkItem(item)
  if (view === "needs_attention")
    return !isDeliveredWorkItem(item) && (item.blocker !== "none" || item.delivery !== "not_started" || item.gate !== "pass")
  if (view === "ready_for_pickup") {
    return !isDeliveredWorkItem(item) && item.gate === "pass" && item.delivery === "ready"
  }
  if (view === "needs_review") {
    return isReviewItem(item)
  }
  if (view === "blocked") return item.blocker !== "none" || item.gate === "fail"
  return true
}

function attentionReasonMatches(item: WorkItem, reason: string) {
  if (reason === "All reasons") return true
  if (reason === "Gate failures") return item.gate === "fail"
  if (reason === "Delivery failures") return item.delivery === "needs_changes"
  if (reason === "Pending approvals") return item.gate === "pending"
  if (reason === "Agent handoffs") return item.delivery === "ready"
  return true
}

function workSearchMatches(item: WorkItem, query: string) {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return true
  return [
    item.key,
    item.title,
    item.createdBy,
    item.agent,
    item.lifecycle,
    item.status,
    item.blocker,
    item.skills.join(" "),
  ]
    .join(" ")
    .toLowerCase()
    .includes(normalized)
}

export function workItemStatusBadge(item: WorkItem): { label: string; tone: Tone } {
  if (isDeliveredWorkItem(item) || item.delivery === "accepted") {
    return { label: "Accepted", tone: "success" }
  }
  if (item.deliveryVerdict) {
    if (item.deliveryVerdict === "pass") {
      return { label: "Ready for human review", tone: "success" }
    }
    return { label: stateText(item.deliveryVerdict), tone: statusTone("state", item.deliveryVerdict) }
  }
  if (item.delivery === "passed") {
    return { label: "Ready for human review", tone: "success" }
  }
  if (item.delivery === "needs_changes") {
    return { label: deliveryText(item.delivery), tone: "warning" }
  }
  if (item.delivery === "ready") {
    return { label: "Ready for pickup", tone: "success" }
  }
  return { label: gateText(item.gate), tone: statusTone("gate", item.gate) }
}

function QueueViewControls({
  workItems,
  activeView,
  activeReason,
  displayMode,
  query,
  onViewChange,
  onReasonChange,
  onDisplayModeChange,
  onQueryChange,
}: {
  workItems: WorkItem[]
  activeView: QueueViewId
  activeReason: string
  displayMode: WorkDisplayMode
  query: string
  onViewChange: (view: QueueViewId) => void
  onReasonChange: (reason: string) => void
  onDisplayModeChange: (mode: WorkDisplayMode) => void
  onQueryChange: (query: string) => void
}) {
  const reasonBaseItems = workItems.filter((item) => queueViewMatches(item, activeView))
  const reasonOptions = [
    { label: "All reasons", count: reasonBaseItems.length },
    ...attentionReasonLabels.map((label) => ({
      label,
      count: reasonBaseItems.filter((item) => attentionReasonMatches(item, label)).length,
    })),
  ]

  return (
    <div className="grid gap-3 border-t pt-4">
      <div className="grid gap-2 sm:grid-cols-[minmax(220px,1fr)_auto] lg:justify-end">
        <Input
          className="h-10 min-w-0 rounded-md text-sm"
          value={query}
          onChange={(event) => onQueryChange(event.target.value)}
          placeholder="Search work"
          aria-label="Search work"
        />
        <div className="flex h-10 justify-self-start rounded-lg border bg-background/60 p-1 sm:justify-self-end">
          <Button
            type="button"
            variant={displayMode === "board" ? "secondary" : "ghost"}
            size="sm"
            className="h-8 rounded-md"
            aria-pressed={displayMode === "board"}
            onClick={() => onDisplayModeChange("board")}
          >
            <KanbanSquareIcon data-icon="inline-start" />
            Board
          </Button>
          <Button
            type="button"
            variant={displayMode === "list" ? "secondary" : "ghost"}
            size="sm"
            className="h-8 rounded-md"
            aria-pressed={displayMode === "list"}
            onClick={() => onDisplayModeChange("list")}
          >
            <ListIcon data-icon="inline-start" />
            List
          </Button>
        </div>
      </div>
      <div className="flex flex-wrap gap-2">
        {queueViews.map((view) => (
          <Button
            key={view.id}
            type="button"
            variant={activeView === view.id ? "secondary" : "ghost"}
            size="sm"
            className="rounded-md"
            onClick={() => onViewChange(view.id)}
          >
            {view.label}
            <Badge variant="secondary" className="ml-1 font-mono">
              {workItems.filter((item) => queueViewMatches(item, view.id)).length}
            </Badge>
          </Button>
        ))}
      </div>
      {reasonFilterViews.has(activeView) ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button type="button" variant="outline" size="sm" className="h-8 w-fit rounded-md text-xs">
              Reason: {activeReason}
              <Badge variant="outline" className="ml-1 font-mono">
                {reasonOptions.find((reason) => reason.label === activeReason)?.count ?? 0}
              </Badge>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            <DropdownMenuLabel>Action reason</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {reasonOptions.map((reason) => (
              <DropdownMenuItem key={reason.label} onSelect={() => onReasonChange(reason.label)}>
                <span>{reason.label}</span>
                <Badge variant="outline" className="ml-auto font-mono">
                  {reason.count}
                </Badge>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
    </div>
  )
}

function WorkQueueTable({
  workItems,
  totalItems,
  filtersActive,
  source,
  status,
  onClearFilters,
}: {
  workItems: WorkItem[]
  totalItems: number
  filtersActive: boolean
  source: WorkboardView["source"]
  status: WorkboardView["status"]
  onClearFilters: () => void
}) {
  const [copiedCommand, setCopiedCommand] = useState("")
  // Three empty cases: the workspace has nothing at all, the default
  // action queue is clear (items exist but none need a human), or active
  // filters hide everything.
  const isWorkspaceEmpty = source === "registry" && !filtersActive && totalItems === 0
  const isAttentionClear = source === "registry" && !filtersActive && totalItems > 0
  const isLoading = status === "loading"
  const emptyTitle = isLoading
    ? "Loading work items"
    : isWorkspaceEmpty
      ? "No work items in this workspace"
      : isAttentionClear
        ? "Nothing needs action"
        : "No work items match"
  const emptyDescription = isLoading
    ? "Reading the active workspace from Doc Registry."
    : isWorkspaceEmpty
      ? "Create or pick up governed work from the CLI or IDE agent, then refresh this board."
      : isAttentionClear
        ? "Everything is in flight or delivered — check the other queue views above."
        : "Clear search or queue filters to inspect the full queue."

  return (
    <section className="overflow-hidden rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-3 py-2.5">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-sm font-semibold">Work queue</h2>
          <Badge variant="outline" className="font-mono">
            {workItems.length}
          </Badge>
          <p className="text-xs text-muted-foreground">visible for review and CLI handoff</p>
        </div>
      </div>
      <div>
        <table className="w-full table-fixed text-left text-xs" aria-label="Work queue">
          <caption className="sr-only">Work items visible in the current queue</caption>
          <colgroup className="hidden sm:table-column-group">
            <col className="w-[33%]" />
            <col className="w-[15%]" />
            <col className="w-[13%]" />
            <col className="w-[27%]" />
            <col className="w-[12%]" />
          </colgroup>
          <thead className="hidden border-b bg-muted/40 text-xs text-muted-foreground sm:table-header-group">
            <tr>
              <th scope="col" className="px-3 py-2 font-medium">Item</th>
              <th scope="col" className="px-3 py-2 font-medium">Status</th>
              <th scope="col" className="px-3 py-2 font-medium">Review</th>
              <th scope="col" className="px-3 py-2 font-medium">Blocker</th>
              <th scope="col" className="px-3 py-2 font-medium">Updated</th>
            </tr>
          </thead>
          <tbody className="grid sm:table-row-group">
            {workItems.length === 0 ? (
              <tr className="block w-full sm:table-row">
                <td colSpan={5} className="block w-full px-4 py-8 text-center sm:table-cell">
                  <h3 className="text-sm font-semibold">{emptyTitle}</h3>
                  <p className="mt-1 text-sm text-muted-foreground">{emptyDescription}</p>
                  {filtersActive ? (
                    <Button type="button" variant="outline" size="sm" className="mt-3 rounded-md" onClick={onClearFilters}>
                      Clear filters
                    </Button>
                  ) : isWorkspaceEmpty ? (
                    <div className="mx-auto mt-4 grid w-full max-w-md gap-2 text-left">
                      <h4 className="text-center text-sm font-semibold">Continue in your IDE</h4>
                      {["specgate plugins install", "specgate doctor", "specgate artifact publish --help", "specgate work create-quick --help"].map((command) => (
                        <Button key={command} type="button" variant="outline" size="sm" className="h-auto min-h-11 justify-between gap-3 whitespace-normal rounded-md py-2 text-left font-mono text-xs break-all" onClick={() => void copyText(command).then((copied) => copied && setCopiedCommand(command))}>
                          {command}<span className="font-sans text-muted-foreground">{copiedCommand === command ? "Copied" : "Copy"}</span>
                        </Button>
                      ))}
                    </div>
                  ) : null}
                </td>
              </tr>
            ) : (
              workItems.map((item) => {
                const statusBadge = workItemStatusBadge(item)
                return (
                  <tr
                    key={item.key}
                    className="grid gap-2 border-b px-3 py-3 transition-colors last:border-b-0 hover:bg-muted/35 sm:table-row sm:px-0 sm:py-0"
                  >
                    <td className="min-w-0 sm:px-3 sm:py-2">
                      <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <Link to={`/work/${item.key}`} className="whitespace-nowrap font-mono font-semibold text-foreground hover:underline">
                          {item.key}
                        </Link>
                        <Badge variant="secondary" className="shrink-0 text-[0.68rem]">{item.route}</Badge>
                        <Badge variant="outline" className={cn("shrink-0 border text-[0.68rem]", toneClass(statusBadge.tone))}>
                          {statusBadge.label}
                        </Badge>
                      </div>
                      <div className="mt-1 flex min-w-0 items-center gap-2">
                        <Link to={`/work/${item.key}`} className="block min-w-0 max-w-[360px] truncate text-muted-foreground hover:text-foreground">
                          {item.title}
                        </Link>
                      </div>
                    </td>
                    <td className="flex justify-between gap-3 truncate sm:table-cell sm:px-3 sm:py-2"><span className="font-medium sm:hidden">Status</span>{item.status}</td>
                    <td className="flex justify-between gap-3 truncate sm:table-cell sm:px-3 sm:py-2"><span className="font-medium sm:hidden">Review</span>{deliveryText(item.delivery)}</td>
                    <td className="grid gap-1 leading-5 sm:table-cell sm:px-3 sm:py-2" title={item.blocker}><span className="font-medium sm:hidden">Blocker</span>{item.blocker}</td>
                    <td className="flex justify-between gap-3 truncate text-muted-foreground sm:table-cell sm:px-3 sm:py-2"><span className="font-medium text-foreground sm:hidden">Updated</span>{formatDateTime(item.updated)}</td>
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}

export function WorkItemNotFound({ itemKey, source }: { itemKey: string; source: WorkboardView["source"] }) {
  const sourceLabel = source === "registry" ? "current workspace" : "local workspace"

  return (
    <section className="rounded-lg border bg-card p-5">
      <div className="flex max-w-2xl items-start gap-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-md border bg-background text-muted-foreground">
          <SearchIcon className="size-4" />
        </span>
        <div className="min-w-0">
          <h2 className="text-lg font-semibold">Work item not found</h2>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">
            <span className="font-mono text-foreground">{itemKey}</span> is not in the {sourceLabel}. It may belong to
            another workspace or have been archived.
          </p>
          <Button asChild variant="outline" size="sm" className="mt-4 rounded-md">
            <Link to="/work">
              <ArrowLeftIcon data-icon="inline-start" />
              Back to work
            </Link>
          </Button>
        </div>
      </div>
    </section>
  )
}

export function WorkItemRouteLoading({ itemKey }: { itemKey: string }) {
  return (
    <section className="rounded-lg border bg-card p-5">
      <div className="flex max-w-2xl items-start gap-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-md border bg-background text-muted-foreground">
          <RotateCwIcon className="size-4 animate-spin" />
        </span>
        <div className="min-w-0">
          <h2 className="text-lg font-semibold">Loading work item</h2>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">
            Looking for <span className="font-mono text-foreground">{itemKey}</span> in the current workspace.
          </p>
        </div>
      </div>
    </section>
  )
}

function ActiveLanes({ lanes }: { lanes: Lane[] }) {
  return (
    <section className="grid gap-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Lifecycle lanes</h2>
          <p className="text-xs text-muted-foreground">Compact lifecycle summary for the visible queue.</p>
        </div>
      </div>
      <div className="mt-4 grid gap-3 md:grid-cols-2 lg:grid-cols-4">
        {lanes.map((lane) => (
          <div key={lane.title} className="rounded-lg border bg-background/70">
            <div className="flex items-center justify-between border-b px-3 py-2.5">
              <h3 className="text-sm font-medium">{lane.title}</h3>
              <Badge variant="outline" className={cn("border", toneClass(lane.tone))}>
                {lane.items.length}
              </Badge>
            </div>
            <div className="grid gap-2 p-2">
              {lane.items.length > 0 ? (
                lane.items.map((item) => {
                  const statusBadge = workItemStatusBadge(item)
                  return (
                    <Link
                      key={item.key}
                      to={`/work/${item.key}`}
                      className="block rounded-md border bg-card p-3 transition-colors hover:bg-muted/35"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="font-mono text-xs font-semibold text-foreground">{item.key}</span>
                        <span className="text-xs text-muted-foreground">{formatDateTime(item.updated)}</span>
                      </div>
                      <div className="mt-2 line-clamp-2 text-sm font-medium leading-5">{item.title}</div>
                      <div className="mt-2 flex flex-wrap items-center gap-1.5">
                        <Badge variant="outline" className={cn("border text-[0.68rem]", toneClass(statusBadge.tone))}>
                          {statusBadge.label}
                        </Badge>
                        <Badge variant="secondary" className="text-[0.68rem]">
                          {deliveryText(item.delivery)}
                        </Badge>
                      </div>
                      <div className="mt-2 flex items-center justify-between gap-3 text-xs text-muted-foreground">
                        <span>{item.createdBy}</span>
                        <span className="truncate">{item.status}</span>
                      </div>
                      {item.blocker !== "none" ? (
                        <div className="mt-2 line-clamp-2 text-xs leading-5 text-muted-foreground">{item.blocker}</div>
                      ) : null}
                    </Link>
                  )
                })
              ) : (
                <div className="px-2 py-6 text-xs leading-5 text-muted-foreground">
                  No visible work is waiting in this lane.
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

export function WorkPage({ workboard, workspaceId }: { workboard: WorkboardData; workspaceId?: string }) {
  const [activeQueueView, setActiveQueueView] = useState<QueueViewId>("needs_attention")
  const [activeAttentionReason, setActiveAttentionReason] = useState("All reasons")
  const [displayMode, setDisplayMode] = useState<WorkDisplayMode>("list")
  const [workQuery, setWorkQuery] = useState("")
  const visibleWorkItems = useMemo(
    () =>
      workboard.workItems.filter(
        (item) =>
          queueViewMatches(item, activeQueueView) &&
          (!reasonFilterViews.has(activeQueueView) || attentionReasonMatches(item, activeAttentionReason)) &&
          workSearchMatches(item, workQuery),
      ),
    [activeAttentionReason, activeQueueView, workQuery, workboard.workItems],
  )
  const filtersActive =
    activeQueueView !== "needs_attention" ||
    activeAttentionReason !== "All reasons" ||
    workQuery.trim().length > 0

  function clearWorkFilters() {
    setActiveQueueView("needs_attention")
    setActiveAttentionReason("All reasons")
    setWorkQuery("")
  }

  function updateQueueView(view: QueueViewId) {
    setActiveQueueView(view)
    if (!reasonFilterViews.has(view)) setActiveAttentionReason("All reasons")
  }

  const visibleLanes = workboard.lanes.map((lane) => ({
    ...lane,
    items: lane.items.filter((laneItem) => visibleWorkItems.some((item) => item.key === laneItem.key)),
  }))

  return (
    <div className="grid gap-4">
      <section className="grid gap-3.5">
        <div>
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h2 className="text-sm font-semibold">Governance Board</h2>
              <p className="text-xs text-muted-foreground">Review handoffs, evidence, verification failures, and blocked governance work.</p>
            </div>
            <Button variant="outline" size="sm" className="rounded-md" aria-label="Refresh work" disabled={workboard.refreshing} onClick={workboard.refresh}>
              <RotateCwIcon data-icon="inline-start" className={cn(workboard.refreshing && "animate-spin")} />
              {workboard.refreshing ? "Refreshing…" : "Refresh"}
            </Button>
          </div>
          <div aria-live="polite" className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            {workboard.lastRefreshedAt ? <span>Last refreshed {formatDateTime(workboard.lastRefreshedAt)}</span> : null}
            {workboard.refreshError ? <><span>{workboard.refreshError}</span><Button variant="link" size="sm" className="h-auto p-0 text-xs" onClick={workboard.refresh}>Retry</Button></> : null}
          </div>
        </div>
        <GovernanceStatsSection workspaceId={workspaceId} />
        {workboard.status === "loading" || workboard.workItems.length > 0 ? (
          <QueueViewControls
            workItems={workboard.workItems}
            activeView={activeQueueView}
            activeReason={activeAttentionReason}
            displayMode={displayMode}
            query={workQuery}
            onViewChange={updateQueueView}
            onReasonChange={setActiveAttentionReason}
            onDisplayModeChange={setDisplayMode}
            onQueryChange={setWorkQuery}
          />
        ) : null}
      </section>
      {displayMode === "board" ? (
        <ActiveLanes lanes={visibleLanes} />
      ) : (
        <WorkQueueTable
          workItems={visibleWorkItems}
          totalItems={workboard.workItems.length}
          filtersActive={filtersActive}
          source={workboard.source}
          status={workboard.status}
          onClearFilters={clearWorkFilters}
        />
      )}
    </div>
  )
}
