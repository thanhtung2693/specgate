import {
  BotIcon,
  DatabaseIcon,
  RotateCwIcon,
  SettingsIcon,
} from "lucide-react"
import { lazy, Suspense, useEffect, useMemo, useState } from "react"
import { Link, Navigate, Outlet, useLocation, useParams, useSearchParams } from "react-router-dom"

import { ThemeToggle } from "@/components/theme-toggle"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
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
} from "@/data/workboard"
import {
  userInitials,
  type WorkspaceProfile,
} from "./shared"
import { ArtifactsPage } from "./artifacts"
import { ReviewsPage } from "./reviews"
import { SettingsModal, settingsSectionFromParam, type SettingsSectionId } from "./settings"
import { WorkItemDetail } from "./work/item-detail"

import { WorkItemNotFound, WorkItemRouteLoading, WorkPage } from "./work-page"

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
          reviewer={reviewer}
          onDeliveryDecided={workboard.refresh}
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
