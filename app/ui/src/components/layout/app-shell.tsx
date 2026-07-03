import {
  AlertTriangleIcon,
  ArrowLeftIcon,
  BotIcon,
  CheckCircle2Icon,
  ChevronRightIcon,
  CircleDotIcon,
  ClockIcon,
  CopyIcon,
  DatabaseIcon,
  ExternalLinkIcon,
  FileTextIcon,
  KanbanSquareIcon,
  ListIcon,
  RotateCwIcon,
  SearchIcon,
  ShieldCheckIcon,
  SettingsIcon,
  UserRoundIcon,
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
  DropdownMenuGroup,
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TooltipProvider } from "@/components/ui/tooltip"
import { sections } from "@/data/navigation"
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
  classifyWorkItemRoute,
  contextPackUriForWorkItem,
  createQuickContextPack,
  updateWorkItemRoute,
  useWorkboardData,
  useWorkItemDetail,
  type AcceptanceCriterionSummary,
  type CreateWorkResult,
  type ContextPackSummary,
  type DeliveryStatusSummary,
  type GateRunSummary,
  type NextActionSummary,
  type RouteClassification,
  type StaleWarningSummary,
  type TrackerLinkSummary,
  type WorkboardView,
  type WorkItemDetailData,
} from "@/data/workboard"
import {
  type Lane,
  type Signal,
  type WorkItem,
} from "@/data/workspace"
import { useGovernanceStats } from "@/data/stats"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import {
  deliveryText,
  gateChecks,
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
import { ActionTooltip, copyText, GateEvidenceWhy, MarkdownText, openGovernanceAgentModal, PolicyExplanationSection, runGovernanceAgentPrompt } from "./shared-ui"
import { ArtifactsPage } from "./artifacts"
import { ReviewsPage } from "./reviews"
import { SettingsModal, settingsSectionFromParam, type SettingsSectionId } from "./settings"

const GovernanceAgent = lazy(() =>
  import("@/components/agent/governance-agent").then((module) => ({
    default: module.GovernanceAssistantModal,
  })),
)

const validSectionIds = new Set(sections.map((section) => section.id))

type AccountDialog = "name" | "workspace" | null

const sessionStorageKey = "specgate.ui.session.v1"
const routeDescriptions: Record<WorkItem["route"], string> = {
  quick: "Small, low-risk change with a lightweight spec and direct handoff.",
  full: "Larger or ambiguous change that needs fuller shaping before handoff.",
}

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

function defaultLocalProfile(): WorkspaceProfile {
  return {
    id: "local-workspace",
    slug: "local",
    name: "Local Workspace",
    user: { id: "dev", username: "dev", name: "Dev", email: "" },
  }
}

function mergeLocalWorkItems(workboard: WorkboardView, localItems: WorkItem[]): WorkboardView {
  if (localItems.length === 0) return workboard

  const lanesByTitle = new Map<string, Lane>()
  for (const lane of workboard.lanes) {
    const items = lane.items.filter(
      (item) => !localItems.some((localItem) => localItem.key === item.key || localItem.registryId === item.registryId),
    )
    lanesByTitle.set(lane.title, {
      ...lane,
      count: items.length,
      items,
    })
  }
  for (const item of localItems) {
    const lane = lanesByTitle.get(item.lifecycle) ?? {
      title: item.lifecycle,
      count: 0,
      tone: "neutral",
      items: [],
    } satisfies Lane
    lane.items = [item, ...lane.items]
    lane.count = lane.items.length
    lanesByTitle.set(item.lifecycle, lane)
  }

  return {
    ...workboard,
    workItems: [
      ...localItems,
      ...workboard.workItems.filter(
        (item) => !localItems.some((localItem) => localItem.key === item.key || localItem.registryId === item.registryId),
      ),
    ],
    lanes: [...lanesByTitle.values()],
    contextPacks: [
      ...localItems
        .filter((item) => item.contextPackId)
        .map((item) => ({
          id: item.contextPackId!,
          title: item.title,
          uri: contextPackUriForWorkItem(item),
          evidence: "Context Pack artifact linked in Doc Registry",
        })),
      ...workboard.contextPacks.filter(
        (pack) => !localItems.some((item) => item.contextPackId === pack.id),
      ),
    ],
  }
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
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuItem onSelect={() => onAccountAction("name")}>
                  <UserRoundIcon />
                  Change name
                </DropdownMenuItem>
                {canSwitchWorkspace ? (
                  <DropdownMenuItem onSelect={() => onAccountAction("workspace")}>
                    <RotateCwIcon />
                    Change workspace
                  </DropdownMenuItem>
                ) : null}
              </DropdownMenuGroup>
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

function FloatingGovernanceAgent() {
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
        <GovernanceAgent />
      </Suspense>
    </div>
  )
}

function SignalStrip({ signals, onSignalSelect }: { signals: Signal[]; onSignalSelect: (signal: Signal) => void }) {
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {signals.map((signal) => (
        <button
          key={signal.label}
          type="button"
          className="rounded-lg border bg-card px-4 py-3 text-left transition-colors hover:bg-muted/35 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
          onClick={() => onSignalSelect(signal)}
        >
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-medium text-muted-foreground">{signal.label}</span>
            <Badge variant="outline" className={cn("border text-[0.65rem]", toneClass(signal.tone))}>
              {signal.detail}
            </Badge>
          </div>
          <div className="mt-1.5 font-mono text-xl font-semibold tracking-tight tabular-nums">{signal.value}</div>
        </button>
      ))}
    </div>
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
    { label: "Gate catches (pre-build)", value: `${item.gateCatchesPreBuild}`, detail: "before handoff", tone: "neutral" },
    { label: "Review catches (post-build)", value: `${item.reviewCatchesPostBuild}`, detail: `${item.reviewCatchesFixed} fixed`, tone: "neutral" },
    { label: "Rework runs", value: `${item.rework}`, detail: `${item.itemsWithRework} items`, tone: "neutral" },
    { label: "Ambiguity blocks", value: `${item.ambiguityBlocks}`, detail: "agent stops", tone: "neutral" },
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
          <div className="border-b px-4 py-2 text-xs font-medium text-muted-foreground">Catch ledger</div>
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
                  {readableKey(entry.kind)}
                </Badge>
                <span className="min-w-0 truncate text-muted-foreground">{entry.detail}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">No catch events recorded in this window.</p>
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
  { id: "needs_attention", label: "Needs attention" },
  { id: "ready_for_pickup", label: "Ready for pickup" },
  { id: "in_implementation", label: "In implementation" },
  { id: "needs_review", label: "Needs review" },
  { id: "blocked", label: "Blocked" },
  { id: "delivered", label: "Delivered" },
  { id: "all_work", label: "All work" },
] as const

type QueueViewId = (typeof queueViews)[number]["id"]
type WorkDisplayMode = "board" | "list"

const attentionReasonLabels = ["Gate failures", "Delivery failures", "Pending approvals", "Agent handoffs"] as const
const reasonFilterViews = new Set<QueueViewId>(["needs_attention", "blocked"])
const selectedRouteClass =
  "border-primary/55 bg-primary/14 text-foreground shadow-[inset_0_0_0_1px_color-mix(in_oklch,var(--primary),transparent_55%)] dark:bg-primary/24"
const routeLabels: Record<WorkItem["route"], string> = {
  quick: "Quick",
  full: "Full",
}

function queueViewMatches(item: WorkItem, view: QueueViewId) {
  if (view === "delivered") return isDeliveredWorkItem(item)
  if (view === "needs_attention")
    return !isDeliveredWorkItem(item) && (item.blocker !== "none" || item.delivery !== "not_started" || item.gate !== "pass")
  if (view === "ready_for_pickup") {
    return !isDeliveredWorkItem(item) && item.gate === "pass" && item.delivery === "ready" && Boolean(item.contextPackId)
  }
  if (view === "in_implementation") return item.lifecycle === "Implementation" || item.status.includes("implement")
  if (view === "needs_review") {
    return !isDeliveredWorkItem(item) && (item.delivery !== "not_started" || item.gate === "fail")
  }
  if (view === "blocked") return item.blocker !== "none" || item.gate === "fail"
  return true
}

function attentionReasonMatches(item: WorkItem, reason: string) {
  if (reason === "All reasons") return true
  if (reason === "Gate failures") return item.gate === "fail"
  if (reason === "Delivery failures") return item.delivery === "needs_changes"
  if (reason === "Pending approvals") return item.gate === "pending" || item.status.includes("approval")
  if (reason === "Agent handoffs") return Boolean(item.contextPackId)
  return true
}

function workSearchMatches(item: WorkItem, query: string) {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return true
  return [
    item.key,
    item.title,
    item.owner,
    item.agent,
    item.lifecycle,
    item.status,
    item.blocker,
    item.contextPackId ?? "",
    item.skills.join(" "),
  ]
    .join(" ")
    .toLowerCase()
    .includes(normalized)
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
            <DropdownMenuLabel>Attention reason</DropdownMenuLabel>
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
  // Three empty cases: the workspace has nothing at all, the default
  // attention view is clear (items exist but none need a human), or active
  // filters hide everything.
  const isWorkspaceEmpty = source === "registry" && !filtersActive && totalItems === 0
  const isAttentionClear = source === "registry" && !filtersActive && totalItems > 0
  const isLoading = status === "loading"
  const emptyTitle = isLoading
    ? "Loading work items"
    : isWorkspaceEmpty
      ? "No work items in this workspace"
      : isAttentionClear
        ? "Nothing needs attention"
        : "No work items match"
  const emptyDescription = isLoading
    ? "Reading the active workspace from Doc Registry."
    : isWorkspaceEmpty
      ? "Publish or pick up governed work from the CLI or IDE agent, then refresh this board."
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
      <div className="overflow-x-auto">
        <table className="w-full min-w-[680px] table-fixed text-left text-xs md:min-w-full">
          <colgroup>
            <col className="w-[33%]" />
            <col className="w-[15%]" />
            <col className="w-[13%]" />
            <col className="w-[27%]" />
            <col className="w-[12%]" />
          </colgroup>
          <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
            <tr>
              <th className="px-3 py-2 font-medium">Item</th>
              <th className="px-3 py-2 font-medium">Status</th>
              <th className="px-3 py-2 font-medium">Review</th>
              <th className="px-3 py-2 font-medium">Blocker</th>
              <th className="px-3 py-2 font-medium">Updated</th>
            </tr>
          </thead>
          <tbody>
            {workItems.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center">
                  <h3 className="text-sm font-semibold">{emptyTitle}</h3>
                  <p className="mt-1 text-sm text-muted-foreground">{emptyDescription}</p>
                  {filtersActive ? (
                    <Button type="button" variant="outline" size="sm" className="mt-3 rounded-md" onClick={onClearFilters}>
                      Clear filters
                    </Button>
                  ) : isWorkspaceEmpty ? (
                    <div className="mt-3 inline-flex rounded-md border bg-background px-3 py-2 font-mono text-xs text-muted-foreground">
                      specgate work list
                    </div>
                  ) : null}
                </td>
              </tr>
            ) : (
              workItems.map((item) => (
              <tr
                key={item.key}
                className="border-b transition-colors last:border-b-0 hover:bg-muted/35"
              >
                <td className="px-3 py-2">
                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                    <Link to={`/work/${item.key}`} className="whitespace-nowrap font-mono font-semibold text-foreground hover:underline">
                      {item.key}
                    </Link>
                    <Badge variant="secondary" className="shrink-0 text-[0.68rem]">{item.route}</Badge>
                    <Badge variant="outline" className={cn("shrink-0 border text-[0.68rem]", toneClass(statusTone("gate", item.gate)))}>
                      {gateText(item.gate)}
                    </Badge>
                  </div>
                  <div className="mt-1 flex min-w-0 items-center gap-2">
                    <Link to={`/work/${item.key}`} className="block min-w-0 max-w-[360px] truncate text-muted-foreground hover:text-foreground">
                      {item.title}
                    </Link>
                  </div>
                </td>
                <td className="truncate px-3 py-2">{item.status}</td>
                <td className="truncate px-3 py-2">{deliveryText(item.delivery)}</td>
                <td className="px-3 py-2 leading-5" title={item.blocker}>{item.blocker}</td>
                <td className="truncate px-3 py-2 text-muted-foreground">{formatDateTime(item.updated)}</td>
              </tr>
              ))
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

function ContextualGovernancePanel({
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
                lane.items.map((item) => (
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
                      <Badge variant="outline" className={cn("border text-[0.68rem]", toneClass(statusTone("gate", item.gate)))}>
                        {gateText(item.gate)}
                      </Badge>
                      <Badge variant="secondary" className="text-[0.68rem]">
                        {deliveryText(item.delivery)}
                      </Badge>
                    </div>
                    <div className="mt-2 flex items-center justify-between gap-3 text-xs text-muted-foreground">
                      <span>{item.owner}</span>
                      <span className="truncate">{item.status}</span>
                    </div>
                    {item.blocker !== "none" ? (
                      <div className="mt-2 line-clamp-2 text-xs leading-5 text-muted-foreground">{item.blocker}</div>
                    ) : null}
                  </Link>
                ))
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

function WorkPage({ workboard, workspaceId }: { workboard: WorkboardView; workspaceId?: string }) {
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

  function selectSignal(signal: Signal) {
    if (signal.label === "Ready for handoff") {
      updateQueueView("ready_for_pickup")
      setActiveAttentionReason("All reasons")
      return
    }
    if (signal.label === "Open gate failures") {
      updateQueueView("needs_attention")
      setActiveAttentionReason("Gate failures")
      return
    }
    if (signal.label === "Blocked by ambiguity") {
      updateQueueView("blocked")
      setActiveAttentionReason("All reasons")
    }
  }

  const visibleLanes = workboard.lanes.map((lane) => ({
    ...lane,
    items: lane.items.filter((laneItem) => visibleWorkItems.some((item) => item.key === laneItem.key)),
  }))

  return (
    <div className="grid gap-4">
      <section className="grid gap-3.5">
        <div>
          <h2 className="text-sm font-semibold">Governance Board</h2>
          <p className="text-xs text-muted-foreground">
            Review specs, handoffs, evidence, verification failures, and blocked governance work.
          </p>
        </div>
        <SignalStrip signals={workboard.signals} onSignalSelect={selectSignal} />
        <GovernanceStatsSection workspaceId={workspaceId} />
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

function WorkItemDetail({
  item,
  contextPacks,
  hydrateDetail,
  onCreateQuickContextPack,
  onUpdateWorkItemRoute,
}: {
  item: WorkItem
  contextPacks: ContextPackSummary[]
  hydrateDetail: boolean
  onCreateQuickContextPack: (item: WorkItem) => Promise<CreateWorkResult>
  onUpdateWorkItemRoute: (item: WorkItem, route: WorkItem["route"]) => Promise<CreateWorkResult>
}) {
  const detail = useWorkItemDetail(item, hydrateDetail)
  const pack = contextPacks.find((contextPack) => contextPack.id === item.contextPackId)
  const [searchParams] = useSearchParams()
  const [activeTab, setActiveTab] = useState(() => {
    const requested = searchParams.get("tab")
    return requested && ["overview", "handoff", "verification", "activity"].includes(requested) ? requested : "overview"
  })
  const needsRouteConfirmation = item.blocker.toLowerCase().includes("route confirmation")
  const delivered = isDeliveredWorkItem(item)
  const nextActionTab = delivered
    ? "verification"
    : needsRouteConfirmation
      ? "overview"
      : item.delivery === "needs_changes"
        ? "verification"
        : "handoff"
  const nextActionLabel = delivered
    ? "View verdict"
    : needsRouteConfirmation
      ? "Confirm route"
      : item.delivery === "needs_changes"
        ? "Inspect gaps"
        : pack
          ? "View handoff"
          : "Prepare handoff"
  const agentPrompt = delivered
    ? {
        label: "Ask for review summary",
        prompt: `Summarize the delivery review outcome for ${item.key}.`,
      }
    : item.delivery === "needs_changes" || item.gate === "fail"
    ? {
        label: "Ask about review gaps",
        prompt: `For ${item.key}, summarize the missing evidence, failed gates, or acceptance criteria that block delivery review.`,
      }
    : {
        label: "Ask about handoff blockers",
        prompt: pack
          ? `For ${item.key}, inspect ${pack.uri} and summarize any remaining handoff risk before an IDE coding agent picks it up.`
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
              <Badge variant="outline" className={cn("border", toneClass(statusTone("gate", item.gate)))}>
                {gateText(item.gate)}
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
              <ContextSummary item={item} detail={detail} onUpdateWorkItemRoute={onUpdateWorkItemRoute} />
            </div>
          </TabsContent>
          <TabsContent value="handoff" className="mt-4">
            <ContextPackDetail item={item} pack={pack} detail={detail} onCreateQuickContextPack={onCreateQuickContextPack} />
          </TabsContent>
          <TabsContent value="verification" className="mt-4">
            {/* Verdict first: Reviews deep-links here to read the delivery outcome. */}
            <div className="grid gap-3">
              <DeliverySummary item={item} detail={detail} />
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
          <h3 className="text-sm font-semibold">Resume in CLI</h3>
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

  const summary = (feature.summaryMd || feature.summary || "").trim()

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
          {feature.summarySourceVersion ? <span>summary {feature.summarySourceVersion}</span> : null}
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
      {summary ? (
        <div className="mt-3 max-h-52 overflow-y-auto rounded-md border bg-card/50 p-3">
          <MarkdownText content={summary} />
        </div>
      ) : (
        <p className="mt-3 text-sm text-muted-foreground">
          No feature summary is recorded yet. Summary drafting stays owned by the governance agent/backend flow.
        </p>
      )}
    </section>
  )
}

// When a delivery verdict carries per-criterion results, the verdict owns the
// criterion check state; the registry `done` flag is only the fallback.
function acceptanceCriterionVerdict(
  criterion: AcceptanceCriterionSummary,
  deliveryStatus: DeliveryStatusSummary | undefined,
): string | undefined {
  const criteria = deliveryStatus?.criteria ?? []
  if (criteria.length === 0) return undefined
  const match = criteria.find((row) => row.id === criterion.id) ?? criteria.find((row) => row.text === criterion.text)
  return match?.verdict
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

function RouteDecisionPanel({
  item,
  onUpdateWorkItemRoute,
}: {
  item: WorkItem
  onUpdateWorkItemRoute: (item: WorkItem, route: WorkItem["route"]) => Promise<CreateWorkResult>
}) {
  const [selectedRoute, setSelectedRoute] = useState<WorkItem["route"]>(item.route)
  const [classification, setClassification] = useState<RouteClassification | null>(null)
  const [isClassifying, setIsClassifying] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [message, setMessage] = useState("")
  const changed = selectedRoute !== item.route
  const needsRouteConfirmation = item.blocker.toLowerCase().includes("route confirmation")

  useEffect(() => {
    setSelectedRoute(item.route)
    setClassification(null)
    setMessage("")
  }, [item.key, item.route])

  async function classifyRoute() {
    setIsClassifying(true)
    setMessage("")
    try {
      const result = await classifyWorkItemRoute(item)
      if (!result) {
        setMessage("Route classifier needs the agents service.")
        return
      }
      setClassification(result)
      setSelectedRoute(result.route)
    } catch {
      setMessage("Could not classify the route. You can still choose manually.")
    } finally {
      setIsClassifying(false)
    }
  }

  async function saveRoute() {
    setIsSaving(true)
    setMessage("")
    try {
      const result = await onUpdateWorkItemRoute(item, selectedRoute)
      setMessage(result.persisted ? "Route saved." : "Route change returned without registry persistence.")
    } catch (reason) {
      setMessage(reason instanceof Error ? reason.message : "Could not save the route. Check the registry service, then try again.")
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">Route decision</h3>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">
            Confirm the amount of planning before handoff. Quick can create a direct Context Pack; full stays in artifact shaping.
          </p>
        </div>
        {needsRouteConfirmation ? (
          <Badge variant="outline" className="border-warning/45 bg-warning/12 text-warning">
            needs confirmation
          </Badge>
        ) : null}
      </div>
      <div className="mt-4 grid gap-2 sm:grid-cols-2">
        {(["quick", "full"] as const).map((route) => (
          <button
            key={route}
            type="button"
            className={cn(
              "rounded-md border p-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
              selectedRoute === route ? selectedRouteClass : "bg-card/70 hover:bg-muted/45",
            )}
            aria-pressed={selectedRoute === route}
            onClick={() => setSelectedRoute(route)}
          >
            <span className="block text-sm font-medium">{routeLabels[route]}</span>
            <span className="mt-1 block text-xs leading-5 text-muted-foreground">{routeDescriptions[route]}</span>
          </button>
        ))}
      </div>
      {classification ? (
        <div className="mt-3 rounded-md border bg-card/70 p-3 text-sm">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">Suggested {routeLabels[classification.route]}</span>
            <Badge variant="secondary" className="font-mono">
              {Math.round(classification.confidence * 100)}%
            </Badge>
          </div>
          <p className="mt-2 leading-5 text-muted-foreground">{classification.rationale}</p>
        </div>
      ) : null}
      {message ? <p className="mt-3 text-sm text-muted-foreground">{message}</p> : null}
      <div className="mt-4 flex flex-wrap justify-end gap-2">
        <ActionTooltip content="Ask the agents service to suggest quick or full. This does not save the route.">
          <span className="inline-flex">
            <Button type="button" variant="outline" size="sm" className="rounded-md" disabled={isClassifying || isSaving} onClick={classifyRoute}>
              <RotateCwIcon data-icon="inline-start" />
              {isClassifying ? "Checking route..." : "Check route"}
            </Button>
          </span>
        </ActionTooltip>
        <ActionTooltip content="Persist this route on the work item through Doc Registry.">
          <span className="inline-flex">
            <Button type="button" size="sm" className="rounded-md" disabled={isSaving || (!changed && !needsRouteConfirmation)} onClick={saveRoute}>
              <ShieldCheckIcon data-icon="inline-start" />
              {isSaving ? "Saving route..." : `Confirm ${routeLabels[selectedRoute].toLowerCase()}`}
            </Button>
          </span>
        </ActionTooltip>
      </div>
    </section>
  )
}

function ContextSummary({
  item,
  detail,
  onUpdateWorkItemRoute,
}: {
  item: WorkItem
  detail: WorkItemDetailData
  onUpdateWorkItemRoute: (item: WorkItem, route: WorkItem["route"]) => Promise<CreateWorkResult>
}) {
  const canChangeRoute = !item.contextPackId

  return (
    <section className="grid gap-3">
      {canChangeRoute ? <RouteDecisionPanel item={item} onUpdateWorkItemRoute={onUpdateWorkItemRoute} /> : null}
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
              key={`${link.identifier}-${link.lane ?? "full"}`}
              href={link.url}
              className="flex flex-wrap items-center justify-between gap-3 rounded-md border bg-card/70 p-3 text-sm transition-colors hover:bg-accent"
              target="_blank"
              rel="noreferrer"
            >
              <span className="flex min-w-0 items-center gap-2">
                <ExternalLinkIcon className="size-4 shrink-0 text-muted-foreground" />
                <span className="font-mono text-xs">{link.identifier}</span>
                {link.lane ? <Badge variant="secondary">{link.lane}</Badge> : null}
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

function buildContextPackMarkdown(item: WorkItem, pack: ContextPackSummary, detail: WorkItemDetailData) {
  const acceptance =
    detail.readback.acceptance === "error"
      ? "- Acceptance criteria unavailable from Doc Registry."
      : detail.acceptanceCriteria.length > 0
      ? detail.acceptanceCriteria
          .map((criterion) => `- [${criterion.done ? "x" : " "}] ${criterion.text} (${criterion.source})`)
          .join("\n")
      : "- No acceptance criteria recorded."
  const gates =
    detail.readback.gateRuns === "error"
      ? "- Gate run history unavailable from Doc Registry."
      : detail.gateRuns.length > 0
      ? detail.gateRuns.map((run) => `- ${run.gate}: ${run.state}${run.hint ? ` - ${run.hint}` : ""}`).join("\n")
      : "- No persisted gate runs recorded."
  const links =
    detail.readback.trackerLinks === "error"
      ? "- Tracker links unavailable from Doc Registry."
      : detail.trackerLinks.length > 0
      ? detail.trackerLinks.map((link) => `- ${link.identifier}: ${link.url}`).join("\n")
      : "- No tracker links recorded."

  return `# ${item.key} ${item.title}

Context Pack: ${pack.uri}
Route: ${item.route}
Owner: ${item.owner}
Gate: ${item.gate}
Delivery: ${deliveryText(item.delivery)}

## Summary

${item.summary}

## Acceptance Criteria

${acceptance}

## Gate Runs

${gates}

## Tracker Links

${links}

## Resume

\`\`\`bash
specgate work context ${item.key}
\`\`\`
`
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
  pack,
  detail,
  onCreateQuickContextPack,
}: {
  item: WorkItem
  pack: ContextPackSummary | undefined
  detail: WorkItemDetailData
  onCreateQuickContextPack: (item: WorkItem) => Promise<CreateWorkResult>
}) {
  const [copied, setCopied] = useState<"uri" | "handoff" | null>(null)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [isCreating, setIsCreating] = useState(false)
  const [error, setError] = useState("")
  const canCreateQuickPack = item.route === "quick"

  async function submitQuickContextPack() {
    setIsCreating(true)
    setError("")
    try {
      await onCreateQuickContextPack(item)
      setConfirmOpen(false)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not create the Context Pack. Check that the agents service is running, then try again.")
    } finally {
      setIsCreating(false)
    }
  }

  if (!pack) {
    return (
      <section className="rounded-lg border bg-background/70 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold">Handoff / Context Pack</h3>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
              {canCreateQuickPack
                ? "Creates an approved quick-mode Context Pack for this work item. It does not start implementation or send anything to a tracker."
                : "No Context Pack built yet. Full-route work needs an approved source artifact before handoff."}
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            {canCreateQuickPack ? (
              <ActionTooltip content="Create the backend Context Pack. This does not start implementation.">
                <Button type="button" size="sm" className="rounded-md" onClick={() => setConfirmOpen(true)}>
                  <ShieldCheckIcon data-icon="inline-start" />
                  Create quick Context Pack
                </Button>
              </ActionTooltip>
            ) : null}
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
        <Dialog open={confirmOpen} onOpenChange={(open) => !isCreating && setConfirmOpen(open)}>
          <DialogContent className="sm:max-w-[440px]">
            <DialogHeader>
              <DialogTitle>Create quick Context Pack?</DialogTitle>
              <DialogDescription>
                This will create the implementation handoff artifact for this approved low-risk work item and attach it to the
                work item. Implementation and tracker handoff stay separate.
              </DialogDescription>
            </DialogHeader>
            {error ? <p className="text-sm text-destructive">{error}</p> : null}
            <DialogFooter>
              <Button type="button" variant="outline" disabled={isCreating} onClick={() => setConfirmOpen(false)}>
                Cancel
              </Button>
              <Button type="button" disabled={isCreating} onClick={submitQuickContextPack}>
                {isCreating ? "Creating Context Pack..." : "Create Context Pack"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </section>
    )
  }

  const canonicalPack = detail.contextPack
  const canonicalPackUnavailable = detail.readback.contextPack === "error"
  const handoffMarkdownAvailable = !canonicalPackUnavailable
  const markdown = canonicalPack?.markdown ?? (handoffMarkdownAvailable ? buildContextPackMarkdown(item, pack, detail) : "")
  const contextPackUri = canonicalPack?.uri || pack.uri
  const filename = `${item.key.toLowerCase()}-context-pack.md`

  return (
    <section className="grid gap-3">
      <div className="rounded-lg border bg-background/70 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <h3 className="text-sm font-semibold">Handoff / Context Pack</h3>
            <p className="mt-2 break-all font-mono text-xs text-muted-foreground">{contextPackUri}</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <ActionTooltip content="Copy the specgate:// Context Pack URI.">
              <Button
                variant="outline"
                size="sm"
                className="rounded-md"
                onClick={() => {
                  void copyText(contextPackUri).then((didCopy) => {
                    if (didCopy) setCopied("uri")
                  })
                }}
              >
                <CopyIcon data-icon="inline-start" />
                {copied === "uri" ? "URI copied" : "Copy URI"}
              </Button>
            </ActionTooltip>
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
        <p className="mt-4 text-sm text-muted-foreground">{pack.evidence}</p>
        {canonicalPackUnavailable ? (
          <p className="mt-3 rounded-md border bg-card/55 p-3 text-sm text-muted-foreground">
            Canonical Context Pack unavailable. Check Doc Registry connectivity; no fallback handoff markdown is copied or downloaded in live mode.
          </p>
        ) : null}
      </div>
      {canonicalPack ? (
        <div className="rounded-lg border bg-background/70 p-4">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h4 className="text-sm font-semibold">Canonical preview</h4>
              <p className="mt-1 text-sm text-muted-foreground">
                Registry-assembled handoff material for CLI and IDE agents.
              </p>
            </div>
            <Badge variant="outline" className={cn("border", toneClass("success"))}>
              {canonicalPack.state}
            </Badge>
          </div>
          <div className="mt-3 max-h-[420px] overflow-auto rounded-md border bg-card/55 p-3">
            <MarkdownText content={canonicalPack.markdown} compact />
          </div>
          {canonicalPack.warnings.length > 0 ? (
            <div className="mt-3 grid gap-2">
              <h5 className="text-xs font-semibold text-muted-foreground">Warnings</h5>
              {canonicalPack.warnings.map((warning) => (
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
          {canonicalPack.knowledgeProvenance.length > 0 ? (
            <div className="mt-3 grid gap-2">
              <h5 className="text-xs font-semibold text-muted-foreground">Knowledge provenance</h5>
              {canonicalPack.knowledgeProvenance.map((row) => (
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
              <GateEvidenceWhy evidence={run.evidence} />
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
  return (
    <section className="grid gap-3">
      <div className="rounded-lg border bg-background/70 p-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold">Gate state</h3>
            <p className="mt-1 text-xs text-muted-foreground">What each gate checked and why, per run.</p>
          </div>
          <Badge variant="outline" className={cn("border", toneClass(statusTone("gate", item.gate)))}>
            {gateText(item.gate)}
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


function DeliverySummary({ item, detail }: { item: WorkItem; detail: WorkItemDetailData }) {
  const deliveryRun = detail.gateRuns.find((run) => run.gate === "delivery_review")
  const deliveryStatus = detail.deliveryStatus
  const state = deliveryStatus?.verdict ?? deliveryRun?.state ?? item.delivery
  const failed = state === "fail" || item.delivery === "needs_changes"
  const verdictLabel = deliveryStatus?.found === false
    ? "No delivery review has run yet."
    : deliveryStatus?.verdict
      ? stateText(deliveryStatus.verdict)
      : deliveryRun
        ? stateText(deliveryRun.state)
        : deliveryText(item.delivery)

  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-2">
          {failed ? (
            <AlertTriangleIcon className="size-4 text-warning" />
          ) : (
            <CheckCircle2Icon className="size-4 text-success" />
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
          <p className="mt-3 text-sm text-muted-foreground">
            Current verdict is <span className="font-medium text-foreground">{verdictLabel}</span>
          </p>
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
        No persisted delivery review result is available yet. Triggering review remains a CLI, MCP, or agents-service workflow.
      </p>
    )
  }

  return (
    <div className="mt-3 grid gap-3">
      <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
        {status.reviewedAt ? <Badge variant="secondary" className="rounded-full">{formatDateTime(status.reviewedAt)}</Badge> : null}
        {typeof status.confidence === "number" ? (
          <Badge variant="secondary" className="rounded-full">{Math.round(status.confidence * 100)}% confidence</Badge>
        ) : null}
      </div>
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

function AccountActionDialog({
  action,
  profile,
  workspaceOptions,
  onOpenChange,
  onRename,
  onWorkspaceChange,
}: {
  action: AccountDialog
  profile: WorkspaceProfile
  workspaceOptions: IdentityWorkspace[]
  onOpenChange: (action: AccountDialog) => void
  onRename: (name: string) => void
  onWorkspaceChange: (workspace: IdentityWorkspace) => void
}) {
  const [draftName, setDraftName] = useState(profile.user.name)
  const [draftWorkspaceId, setDraftWorkspaceId] = useState(profile.id ?? profile.name)

  useEffect(() => {
    if (action === "name") {
      setDraftName(profile.user.name)
    }
    if (action === "workspace") {
      setDraftWorkspaceId(profile.id ?? profile.name)
    }
  }, [action, profile.id, profile.name, profile.user.name])

  const selectedWorkspace =
    workspaceOptions.find((choice) => choice.id === draftWorkspaceId || choice.name === draftWorkspaceId) ??
    workspaceOptions.find((choice) => choice.name === profile.name) ??
    { id: profile.id ?? profile.name, slug: profile.slug ?? profile.name, name: profile.name }

  return (
    <Dialog open={action !== null} onOpenChange={(open) => !open && onOpenChange(null)}>
      <DialogContent className="sm:max-w-[440px]">
        {action === "name" ? (
          <>
            <DialogHeader>
              <DialogTitle>Change name</DialogTitle>
              <DialogDescription>Update the local display name shown in SpecGate.</DialogDescription>
            </DialogHeader>
            <label className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Display name</span>
              <Input value={draftName} onChange={(event) => setDraftName(event.target.value)} />
            </label>
            <DialogFooter>
              <Button variant="outline" className="rounded-md" onClick={() => onOpenChange(null)}>
                Cancel
              </Button>
              <Button
                className="rounded-md"
                onClick={() => {
                  onRename(draftName.trim() || profile.user.name)
                  onOpenChange(null)
                }}
              >
                Save name
              </Button>
            </DialogFooter>
          </>
        ) : null}

        {action === "workspace" ? (
          <>
            <DialogHeader>
              <DialogTitle>Change workspace</DialogTitle>
              <DialogDescription>Switch the active local workspace for this UI session.</DialogDescription>
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
  reviewer,
  onCreateQuickContextPack,
  onUpdateWorkItemRoute,
}: {
  activeSection: (typeof sections)[number]
  workItemKey?: string
  workboard: WorkboardView
  workspaceId?: string
  reviewer: string
  onCreateQuickContextPack: (item: WorkItem) => Promise<CreateWorkResult>
  onUpdateWorkItemRoute: (item: WorkItem, route: WorkItem["route"]) => Promise<CreateWorkResult>
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
          contextPacks={workboard.contextPacks}
          hydrateDetail={workboard.source === "registry"}
          onCreateQuickContextPack={onCreateQuickContextPack}
          onUpdateWorkItemRoute={onUpdateWorkItemRoute}
        />
      )
    }
    if (workItemKey) {
      return <WorkItemNotFound itemKey={workItemKey} source={workboard.source} />
    }
    return <WorkPage workboard={workboard} workspaceId={workspaceId} />
  }
  if (activeSection.id === "reviews") {
    return <ReviewsPage workboard={workboard} reviewer={reviewer} />
  }
  if (activeSection.id === "artifacts") {
    return <ArtifactsPage reviewer={reviewer} />
  }
  return <WorkPage workboard={workboard} workspaceId={workspaceId} />
}

export function AppShell() {
  const params = useParams()
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const registryBase = useMemo(() => docRegistryBase(), [])
  const [profile, setProfile] = useState<WorkspaceProfile | null>(() => readLocalSession())
  const registryWorkspaceId = registryBase && profile?.id?.startsWith("local-") ? undefined : profile?.id
  const workboard = useWorkboardData(registryWorkspaceId)
  const [localWorkItems, setLocalWorkItems] = useState<WorkItem[]>([])
  const [workspaceOptions, setWorkspaceOptions] = useState<IdentityWorkspace[]>([])
  const [accountAction, setAccountAction] = useState<AccountDialog>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [activeSettingsSection, setActiveSettingsSection] = useState<SettingsSectionId>("general")
  const activeId = params.section ?? "work"
  const workItemKey = params.itemKey
  const sessionWorkboard = useMemo(() => mergeLocalWorkItems(workboard, localWorkItems), [localWorkItems, workboard])

  useEffect(() => {
    setLocalWorkItems([])
  }, [profile?.id])

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
      return
    }

    const controller = new AbortController()
    void listIdentityWorkspaces(registryBase, controller.signal)
      .then((items) => {
        if (items.length > 0) setWorkspaceOptions(items)
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setWorkspaceOptions(
          profile
            ? [{ id: profile.id ?? profile.name, slug: profile.slug ?? profile.name, name: profile.name }]
            : [],
        )
      })
    return () => controller.abort()
  }, [profile, registryBase])

  // Local attribution only (no auth): bootstrap a session silently instead of
  // gating the app behind a form. Users can rename via the account menu.
  useEffect(() => {
    if (profile) return
    let cancelled = false

    function adopt(next: WorkspaceProfile) {
      if (cancelled) return
      writeLocalSession(next)
      setProfile(next)
    }

    if (!registryBase) {
      adopt(defaultLocalProfile())
      return
    }

    void (async () => {
      let workspaceName = "SpecGate"
      try {
        const items = await listIdentityWorkspaces(registryBase)
        if (items.length > 0) workspaceName = items[0].name
      } catch {
        // fall through to the default workspace name
      }
      try {
        const selection = await bootstrapIdentity(registryBase, {
          workspaceName,
          displayName: "Dev",
          username: "dev",
        })
        adopt(profileFromSelection(selection))
      } catch {
        adopt(defaultLocalProfile())
      }
    })()

    return () => {
      cancelled = true
    }
  }, [profile, registryBase])

  if (!validSectionIds.has(activeId)) {
    return <Navigate to={`/work${location.search}`} replace />
  }

  if (!profile) {
    return (
      <main className="grid min-h-svh place-items-center bg-background text-sm text-muted-foreground">
        Starting session…
      </main>
    )
  }

  const activeSection = sections.find((section) => section.id === activeId) ?? sections[0]

  function addLocalWorkItem(item: WorkItem) {
    setLocalWorkItems((current) => [
      item,
      ...current.filter((candidate) => candidate.key !== item.key && candidate.registryId !== item.registryId),
    ])
  }

  async function createWorkItemQuickContextPack(item: WorkItem): Promise<CreateWorkResult> {
    const result = await createQuickContextPack(item)
    if (!result) {
      throw new Error("agents service is not configured")
    }
    addLocalWorkItem(result.item)
    return result
  }

  async function updateWorkItemRouteChoice(item: WorkItem, route: WorkItem["route"]): Promise<CreateWorkResult> {
    const registryResult = await updateWorkItemRoute(item, route)
    if (!registryResult) {
      throw new Error("Route changes require the Doc Registry service.")
    }
    addLocalWorkItem(registryResult.item)
    return registryResult
  }

  return (
    <TooltipProvider>
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
          <main className="min-h-0 flex-1 overflow-hidden bg-background">
            <div className="grid h-full min-h-0 gap-5 p-4 md:p-5">
              <ScrollArea className="min-h-0">
                <div className="mx-auto flex max-w-[1560px] flex-col gap-5 pb-8">
                  <SectionContent
                    activeSection={activeSection}
                    workItemKey={workItemKey}
                    workboard={sessionWorkboard}
                    workspaceId={registryWorkspaceId}
                    reviewer={profile.user.username}
                    onCreateQuickContextPack={createWorkItemQuickContextPack}
                    onUpdateWorkItemRoute={updateWorkItemRouteChoice}
                  />
                </div>
              </ScrollArea>
            </div>
          </main>
          <SettingsModal
            open={settingsOpen}
            onOpenChange={setSettingsOpen}
            profile={profile}
            activeSection={activeSettingsSection}
            onActiveSectionChange={setActiveSettingsSection}
          />
          <AccountActionDialog
            action={accountAction}
            profile={profile}
            workspaceOptions={workspaceOptions}
            onOpenChange={setAccountAction}
            onRename={(name) => {
              setProfile((current) => {
                const next = current ? { ...current, user: { ...current.user, name } } : null
                writeLocalSession(next)
                return next
              })
            }}
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
          <FloatingGovernanceAgent />
          <Outlet />
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}
