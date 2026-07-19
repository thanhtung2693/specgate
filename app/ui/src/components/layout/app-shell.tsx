import {
  ArrowLeftIcon,
  BotIcon,
  ChevronRightIcon,
  DatabaseIcon,
  KanbanSquareIcon,
  ListIcon,
  RotateCwIcon,
  SearchIcon,
  SettingsIcon,
} from "lucide-react"
import { lazy, Suspense, useEffect, useMemo, useState } from "react"
import { Link, Navigate, Outlet, useLocation, useParams, useSearchParams } from "react-router-dom"

import { ThemeToggle } from "@/components/theme-toggle"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar"
import { TooltipProvider } from "@/components/ui/tooltip"
import { sections } from "@/data/navigation"
import { KnowledgePage } from "@/components/layout/knowledge"
import {
  bootstrapIdentity,
  listIdentityWorkspaces,
  type IdentitySelection,
  type IdentityWorkspace,
} from "@/data/identity"
import {
  docRegistryBase,
} from "@/data/model-settings"
import {
  useWorkboardData,
  type WorkboardData,
  type WorkboardView,
} from "@/data/workboard"
import {
  type Lane,
  type WorkItem,
} from "@/data/workspace"
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
  userInitials,
  type Tone,
  type WorkspaceProfile,
} from "./shared"
import { ActionTooltip, copyText, openGovernanceAgentModal, runGovernanceAgentPrompt } from "./shared-ui"
import { ArtifactsPage } from "./artifacts"
import { isReviewItem, ReviewsPage } from "./reviews"
import { SettingsModal, settingsSectionFromParam, type SettingsSectionId } from "./settings"
import { WorkItemDetail } from "./work/item-detail"

const GovernanceAgent = lazy(() =>
  import("@/components/agent/governance-agent").then((module) => ({
    default: module.GovernanceAssistantModal,
  })),
)

const validSectionIds = new Set(sections.map((section) => section.id))

type AccountDialog = "workspace" | null

const sessionStorageKey = "specgate.ui.session.v2"

function profileFromSelection(selection: IdentitySelection): WorkspaceProfile {
  return {
    id: selection.workspace.id,
    slug: selection.workspace.slug,
    name: selection.workspace.name,
    user: {
      id: selection.user.id,
      name: selection.user.name,
      username: selection.user.username,
      email: selection.user.email ?? "",
    },
  }
}

function readLocalSession(): WorkspaceProfile | null {
  try {
    const payload = JSON.parse(localStorage.getItem(sessionStorageKey) ?? "null") as { profile?: WorkspaceProfile } | null
    if (!payload?.profile?.name || !payload.profile.user?.username) return null
    return {
      ...payload.profile,
      user: {
        ...payload.profile.user,
        email: payload.profile.user.email ?? "",
      },
    }
  } catch {
    return null
  }
}

function writeLocalSession(profile: WorkspaceProfile | null) {
  if (!profile) {
    localStorage.removeItem(sessionStorageKey)
    return
  }
  localStorage.setItem(sessionStorageKey, JSON.stringify({ profile }))
}

function AppSidebar({
  activeSection,
  profile,
  canSwitchWorkspace,
  onAccountAction,
  onOpenSettings,
}: {
  activeSection: string
  profile: WorkspaceProfile
  canSwitchWorkspace: boolean
  onAccountAction: (dialog: AccountDialog) => void
  onOpenSettings: () => void
}) {
  const displayName = profile.user.name
  const workspaceName = profile.name
  const initials = userInitials(profile.user.name || profile.user.username)

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild size="lg" isActive={activeSection === "work"}>
              <Link to="/work" aria-label="SpecGate work">
                <img className="size-8 rounded-lg" src="/logo.svg" alt="" />
                <span className="grid min-w-0 text-left">
                  <span className="truncate text-sm font-semibold">SpecGate (Experimental)</span>
                  <span className="truncate text-xs text-muted-foreground">Governed delivery</span>
                </span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Workflow</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {sections.map((section) => {
                const Icon = section.icon
                return (
                  <SidebarMenuItem key={section.id}>
                    <SidebarMenuButton asChild isActive={section.id === activeSection} tooltip={section.label}>
                      <Link to={section.path}>
                        <Icon />
                        <span>{section.label}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                )
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <DropdownMenu>
            <SidebarMenuItem className="group/user-row flex items-center rounded-lg transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground">
              <DropdownMenuTrigger asChild>
                <button
                  type="button"
                  className="flex h-12 min-w-0 flex-1 items-center gap-3 rounded-l-lg px-2.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                  aria-label="Open workspace menu"
                >
                  <Avatar className="size-8">
                    <AvatarFallback>{initials}</AvatarFallback>
                  </Avatar>
                  <span className="grid min-w-0 flex-1 text-left group-data-[collapsible=icon]:hidden">
                    <span className="truncate text-sm leading-5">{displayName}</span>
                    <span className="truncate text-xs leading-4 text-muted-foreground">{workspaceName}</span>
                  </span>
                </button>
              </DropdownMenuTrigger>
              <Button
                variant="ghost"
                size="icon-sm"
                className="mr-1 shrink-0 rounded-md hover:bg-sidebar-accent-foreground/10 group-data-[collapsible=icon]:hidden"
                aria-label="Settings"
                onClick={onOpenSettings}
              >
                <SettingsIcon />
              </Button>
            </SidebarMenuItem>
            <DropdownMenuContent side="top" align="start" className="w-64">
              <DropdownMenuLabel>
                <span className="block text-sm text-foreground">{displayName}</span>
                <span className="block text-xs font-normal">{`@${profile.user.username}`}</span>
              </DropdownMenuLabel>
              {canSwitchWorkspace ? (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onSelect={() => onAccountAction("workspace")}>
                    <RotateCwIcon />
                    Change workspace
                  </DropdownMenuItem>
                </>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
        </SidebarMenu>
      </SidebarFooter>
    </Sidebar>
  )
}

function Header({ activeSection }: { activeSection: (typeof sections)[number] }) {
  return (
    <header className="flex h-14 shrink-0 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur">
      <SidebarTrigger aria-label="Toggle sidebar" />
      <div className="min-w-0 flex-1">
        <h1 className="truncate text-base font-semibold">{activeSection.label}</h1>
        <p className="hidden truncate text-xs text-muted-foreground sm:block">{activeSection.description}</p>
      </div>
      <ThemeToggle />
    </header>
  )
}

function FloatingGovernanceAgent({ workspaceId }: { workspaceId?: string }) {
  return (
    <div className="fixed right-4 bottom-4 z-40 md:right-5 md:bottom-5">
      <Suspense
        fallback={
          <Button
            variant="outline"
            size="icon-lg"
            className="rounded-lg border bg-card shadow-sm"
            aria-label="Open governance agent"
            disabled
          >
            <BotIcon />
          </Button>
        }
      >
        <GovernanceAgent workspaceId={workspaceId} />
      </Suspense>
    </div>
  )
}

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

type ContextualAgentPrompt = {
  label: string
  prompt: string
}

function WorkItemNotFound({ itemKey, source }: { itemKey: string; source: WorkboardView["source"] }) {
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

function WorkItemRouteLoading({ itemKey }: { itemKey: string }) {
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

export function ContextualGovernancePanel({
  contextLabel,
  prompts,
}: {
  contextLabel: string
  prompts: ContextualAgentPrompt[]
}) {
  return (
    <section className="min-w-0 overflow-hidden rounded-lg border bg-card/85 p-4">
      <div className="flex items-start gap-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-md border bg-background">
          <BotIcon className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold">Governance agent context</h3>
          <p className="mt-1 truncate text-xs text-muted-foreground">{contextLabel}</p>
        </div>
      </div>
      <div className="mt-4 grid gap-2">
        {prompts.map((item) => (
          <ActionTooltip key={item.label} content="Opens governance agent. Does not change workflow state.">
            <Button
              variant="outline"
              size="sm"
              className="min-w-0 justify-start rounded-md"
              onClick={() => runGovernanceAgentPrompt(item.prompt)}
            >
              <BotIcon data-icon="inline-start" />
              <span className="min-w-0 truncate">{item.label}</span>
            </Button>
          </ActionTooltip>
        ))}
      </div>
      <ActionTooltip content="Open governance chat without sending a prompt.">
        <Button variant="secondary" size="sm" className="mt-3 w-full min-w-0 rounded-md" onClick={openGovernanceAgentModal}>
          <BotIcon data-icon="inline-start" />
          Open agent
        </Button>
      </ActionTooltip>
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

function WorkPage({ workboard, workspaceId }: { workboard: WorkboardData; workspaceId?: string }) {
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

function AccountActionDialog({
  action,
  profile,
  workspaceOptions,
  onOpenChange,
  onWorkspaceChange,
}: {
  action: AccountDialog
  profile: WorkspaceProfile
  workspaceOptions: IdentityWorkspace[]
  onOpenChange: (action: AccountDialog) => void
  onWorkspaceChange: (workspace: IdentityWorkspace) => void
}) {
  const [draftWorkspaceId, setDraftWorkspaceId] = useState(profile.id ?? profile.name)

  useEffect(() => {
    if (action === "workspace") {
      setDraftWorkspaceId(profile.id ?? profile.name)
    }
  }, [action, profile.id, profile.name])

  const selectedWorkspace =
    workspaceOptions.find((choice) => choice.id === draftWorkspaceId || choice.name === draftWorkspaceId) ??
    workspaceOptions.find((choice) => choice.name === profile.name) ??
    { id: profile.id ?? profile.name, slug: profile.slug ?? profile.name, name: profile.name }

  return (
    <Dialog open={action !== null} onOpenChange={(open) => !open && onOpenChange(null)}>
      <DialogContent className="sm:max-w-[440px]">
        {action === "workspace" ? (
          <>
            <DialogHeader>
              <DialogTitle>Change workspace</DialogTitle>
              <DialogDescription>Switch the active workspace for this browser.</DialogDescription>
            </DialogHeader>
            <div className="grid gap-2">
              {workspaceOptions.map((choice) => (
                <Button
                  key={choice.id}
                  type="button"
                  variant={choice.id === selectedWorkspace.id ? "secondary" : "outline"}
                  className="h-auto justify-start rounded-md px-3 py-2"
                  onClick={() => setDraftWorkspaceId(choice.id)}
                >
                  <DatabaseIcon data-icon="inline-start" />
                  {choice.name}
                </Button>
              ))}
            </div>
            <DialogFooter>
              <Button variant="outline" className="rounded-md" onClick={() => onOpenChange(null)}>
                Cancel
              </Button>
              <Button
                className="rounded-md"
                onClick={() => {
                  onWorkspaceChange(selectedWorkspace)
                  onOpenChange(null)
                }}
              >
                Switch workspace
              </Button>
            </DialogFooter>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}


function SectionContent({
  activeSection,
  workItemKey,
  workboard,
  workspaceId,
  knowledgeWorkspaceId,
  reviewer,
}: {
  activeSection: (typeof sections)[number]
  workItemKey?: string
  workboard: WorkboardData
  workspaceId?: string
  knowledgeWorkspaceId?: string
  reviewer: string
}) {
  if (activeSection.id === "work") {
    if (workItemKey && workboard.status === "loading") {
      return <WorkItemRouteLoading itemKey={workItemKey} />
    }
    const item = workboard.workItems.find((candidate) => candidate.key === workItemKey)
    if (workItemKey && item) {
      return (
        <WorkItemDetail
          item={item}
          workspaceId={workspaceId ?? ""}
          hydrateDetail={workboard.source === "registry"}
          refreshGeneration={workboard.refreshGeneration}
        />
      )
    }
    if (workItemKey) {
      return <WorkItemNotFound itemKey={workItemKey} source={workboard.source} />
    }
    return <WorkPage workboard={workboard} workspaceId={workspaceId} />
  }
  if (activeSection.id === "reviews") {
    return <ReviewsPage workboard={workboard} reviewer={reviewer} workspaceId={workspaceId ?? ""} />
  }
  if (activeSection.id === "artifacts") {
    return <ArtifactsPage reviewer={reviewer} workspaceId={workspaceId ?? ""} workItems={workboard.workItems} routeArtifactId={workItemKey} />
  }
  if (activeSection.id === "knowledge") {
    return <KnowledgePage key={knowledgeWorkspaceId ?? "unresolved"} workspaceId={knowledgeWorkspaceId} uploader={reviewer} />
  }
  return <WorkPage workboard={workboard} workspaceId={workspaceId} />
}

export function AppShell() {
  const params = useParams()
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const registryBase = useMemo(() => docRegistryBase(), [])
  const [profile, setProfile] = useState<WorkspaceProfile | null>(() => readLocalSession())
  const [setupDisplayName, setSetupDisplayName] = useState("")
  const [setupUsername, setSetupUsername] = useState("")
  const [setupEmail, setSetupEmail] = useState("")
  const [setupWorkspaceName, setSetupWorkspaceName] = useState("My workspace")
  const [setupBusy, setSetupBusy] = useState(false)
  const [setupError, setSetupError] = useState("")
  const registryWorkspaceId = profile?.id
  const workboard = useWorkboardData(registryWorkspaceId)
  const [workspaceOptions, setWorkspaceOptions] = useState<IdentityWorkspace[]>([])
  const [validatedKnowledgeWorkspaceId, setValidatedKnowledgeWorkspaceId] = useState<string>()
  const [accountAction, setAccountAction] = useState<AccountDialog>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [activeSettingsSection, setActiveSettingsSection] = useState<SettingsSectionId>("general")
  const activeId = params.section ?? "work"
  const workItemKey = params.itemKey

  useEffect(() => {
    if (!validSectionIds.has(activeId)) return
    const requestedSection = settingsSectionFromParam(searchParams.get("settings"))
    if (!requestedSection) return
    setActiveSettingsSection(requestedSection)
    setSettingsOpen(true)
    const next = new URLSearchParams(searchParams)
    next.delete("settings")
    setSearchParams(next, { replace: true })
  }, [activeId, searchParams, setSearchParams])

  useEffect(() => {
    if (!registryBase) {
      setWorkspaceOptions([])
      setValidatedKnowledgeWorkspaceId(undefined)
      return
    }

    setValidatedKnowledgeWorkspaceId(undefined)
    const controller = new AbortController()
    let active = true
    void listIdentityWorkspaces(registryBase, controller.signal)
      .then((items) => {
        if (!active) return
        setWorkspaceOptions(items)
        if (!profile?.id) return

        if (items.some((workspace) => workspace.id === profile.id)) {
          setValidatedKnowledgeWorkspaceId(profile.id)
          return
        }
        setSetupDisplayName(profile.user.name)
        setSetupUsername(profile.user.username)
        setSetupEmail(profile.user.email)
        setSetupWorkspaceName(profile.name)
        setSetupError("Saved workspace is no longer available. Set up attribution for this appliance.")
        writeLocalSession(null)
        setProfile(null)
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setValidatedKnowledgeWorkspaceId(undefined)
        setWorkspaceOptions(
          profile
            ? [{ id: profile.id ?? profile.name, slug: profile.slug ?? profile.name, name: profile.name }]
            : [],
        )
      })
    return () => {
      active = false
      controller.abort()
    }
  }, [profile, registryBase])

  useEffect(() => {
    if (!profile && workspaceOptions.length > 0 && setupWorkspaceName === "My workspace") {
      setSetupWorkspaceName(workspaceOptions[0].name)
    }
  }, [profile, setupWorkspaceName, workspaceOptions])

  async function completeSetup() {
    if (!registryBase) return
    const displayName = setupDisplayName.trim()
    const username = setupUsername.trim()
    const workspaceName = setupWorkspaceName.trim()
    if (!displayName || !username || !workspaceName) {
      setSetupError("Display name, username, and workspace name are required.")
      return
    }
    setSetupBusy(true)
    setSetupError("")
    let next: WorkspaceProfile
    try {
      next = profileFromSelection(await bootstrapIdentity(registryBase, {
        workspaceName,
        displayName,
        username,
        email: setupEmail.trim() || undefined,
      }))
    } catch {
      setSetupError("Doc Registry is unavailable. Run specgate doctor, then try again.")
      setSetupBusy(false)
      return
    }
    writeLocalSession(next)
    setProfile(next)
    setSetupBusy(false)
}
  if (!validSectionIds.has(activeId)) {
    return <Navigate to={`/work${location.search}`} replace />
  }

  if (!registryBase) {
    return (
      <main className="grid min-h-svh place-items-center bg-background p-4">
        <section className="grid w-full max-w-md gap-4 rounded-xl border bg-card p-5">
          <div>
            <h1 className="text-xl font-semibold">Web UI requires Full mode</h1>
            <p className="mt-2 text-sm text-muted-foreground">
              Local mode stays in the CLI and IDE. Start the Full appliance to use the browser UI.
            </p>
          </div>
          <code className="rounded-md border bg-background px-3 py-2 text-sm">specgate init --mode full</code>
          <p className="text-xs text-muted-foreground">Then run <code>specgate doctor</code> and open the URL it reports.</p>
        </section>
      </main>
    )
  }

  if (!profile) {
    return (
      <main className="grid min-h-svh place-items-center bg-background p-4">
        <form
          className="grid w-full max-w-md gap-4 rounded-xl border bg-card p-5"
          onSubmit={(event) => {
            event.preventDefault()
            void completeSetup()
          }}
        >
          <div>
            <h1 className="text-xl font-semibold">Set up attribution</h1>
            <p className="mt-2 text-sm text-muted-foreground">Used for attribution and audit history. This does not control access.</p>
          </div>
          <label className="grid gap-1.5 text-sm">Display name<Input required value={setupDisplayName} onChange={(event) => setSetupDisplayName(event.target.value)} /></label>
          <label className="grid gap-1.5 text-sm">Username<Input required value={setupUsername} onChange={(event) => setSetupUsername(event.target.value)} /></label>
          <label className="grid gap-1.5 text-sm">Email (optional)<Input type="email" value={setupEmail} onChange={(event) => setSetupEmail(event.target.value)} /></label>
          <label className="grid gap-1.5 text-sm">Workspace name<Input required value={setupWorkspaceName} onChange={(event) => setSetupWorkspaceName(event.target.value)} /></label>
          {setupError ? <p role="alert" className="text-sm text-destructive">{setupError}</p> : null}
          <Button type="submit" className="rounded-md" disabled={setupBusy}>{setupBusy ? "Setting up…" : "Continue"}</Button>
        </form>
      </main>
    )
  }

  const activeSection = sections.find((section) => section.id === activeId) ?? sections[0]

  return (
    <TooltipProvider>
      <button
        type="button"
        className="sr-only fixed left-4 top-4 z-50 rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground shadow-sm focus:not-sr-only"
        onClick={() => {
          document.getElementById("main-content")?.focus()
        }}
      >
        Skip to main content
      </button>
      <SidebarProvider>
        <AppSidebar
          activeSection={activeSection.id}
          profile={profile}
          canSwitchWorkspace={workspaceOptions.length > 1}
          onAccountAction={setAccountAction}
          onOpenSettings={() => {
            setActiveSettingsSection("general")
            setSettingsOpen(true)
          }}
        />
        <SidebarInset>
          <Header activeSection={activeSection} />
          <div id="main-content" tabIndex={-1} className="min-h-0 flex-1 overflow-hidden bg-background">
            <div className="grid h-full min-h-0 gap-5 p-4 md:p-5">
              <ScrollArea className="min-h-0">
                <div className="mx-auto flex max-w-[1560px] flex-col gap-5 pb-24">
                  <SectionContent
                    activeSection={activeSection}
                    workItemKey={workItemKey}
                    workboard={workboard}
                    workspaceId={registryWorkspaceId}
                    knowledgeWorkspaceId={validatedKnowledgeWorkspaceId}
                    reviewer={profile.user.username}
                  />
                </div>
              </ScrollArea>
            </div>
          </div>
          <SettingsModal
            open={settingsOpen}
            onOpenChange={setSettingsOpen}
            activeSection={activeSettingsSection}
            onActiveSectionChange={setActiveSettingsSection}
            profile={profile}
            workspaceOptions={workspaceOptions}
            workspaceId={registryWorkspaceId}
          />
          <AccountActionDialog
            action={accountAction}
            profile={profile}
            workspaceOptions={workspaceOptions}
            onOpenChange={setAccountAction}
            onWorkspaceChange={(nextWorkspace) => {
              setProfile((current) => {
                if (!current) return current
                const next = {
                  ...current,
                  id: nextWorkspace.id,
                  slug: nextWorkspace.slug,
                  name: nextWorkspace.name,
                }
                writeLocalSession(next)
                return next
              })
            }}
          />
          <FloatingGovernanceAgent workspaceId={registryWorkspaceId} />
          <Outlet />
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}
