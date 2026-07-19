// Settings domain: general/model/governance/integrations panels and
// the Settings modal. Extracted from app-shell.

import { ArrowLeftIcon, KeyRoundIcon, PlugIcon, ShieldCheckIcon, SlidersHorizontalIcon, SettingsIcon, UsersIcon } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"
import { ActionTooltip } from "./shared-ui"
import { type SettingsSaveStatus } from "./settings/shared"
import { GeneralSettingsPanel } from "./settings/general-settings-panel"
import { GovernanceSettingsPanel } from "./settings/governance-catalog-panel"
import { IntegrationsSettingsPanel } from "./settings/integrations-panel"
import { ModelSettingsPanel } from "./settings/model-settings-panel"
import { WorkspaceSettingsPanel } from "./settings/workspace-settings-panel"
import { type IdentityWorkspace } from "@/data/identity"
import { type WorkspaceProfile } from "./shared"

const settingsSections = [
  {
    id: "general",
    label: "General",
    description: "Governance defaults",
    icon: SlidersHorizontalIcon,
  },
  {
    id: "models",
    label: "Models",
    description: "Server-side and embedding",
    icon: KeyRoundIcon,
  },
  {
    id: "workspace",
    label: "Workspace",
    description: "Team and identity",
    icon: UsersIcon,
  },
  {
    id: "governance",
    label: "Governance",
    description: "Skills and policy tiers",
    icon: ShieldCheckIcon,
  },
  {
    id: "integrations",
    label: "Integrations",
    description: "Repositories and work tracking",
    icon: PlugIcon,
  },
] as const

export type SettingsSectionId = (typeof settingsSections)[number]["id"]
const settingsSectionIds = new Set<string>(settingsSections.map((section) => section.id))
// Opening Settings on the default section keeps the mobile section list first.
const defaultSettingsSectionId: SettingsSectionId = settingsSections[0].id

export function settingsSectionFromParam(value: string | null): SettingsSectionId | null {
  return value && settingsSectionIds.has(value) ? (value as SettingsSectionId) : null
}

function SettingsPanelContent({
  activeSection,
  profile,
  workspaceOptions,
  workspaceId,
  onGeneralSaveStatusChange,
  onModelSaveStatusChange,
}: {
  activeSection: SettingsSectionId
  profile: WorkspaceProfile
  workspaceOptions: IdentityWorkspace[]
  workspaceId?: string
  onGeneralSaveStatusChange: (status: SettingsSaveStatus) => void
  onModelSaveStatusChange: (status: SettingsSaveStatus) => void
}) {
  if (activeSection === "general") return <GeneralSettingsPanel workspaceId={workspaceId} onSaveStatusChange={onGeneralSaveStatusChange} />
  if (activeSection === "models") return <ModelSettingsPanel onSaveStatusChange={onModelSaveStatusChange} />
  if (activeSection === "workspace") return <WorkspaceSettingsPanel profile={profile} workspaceOptions={workspaceOptions} />
  if (activeSection === "governance") return <GovernanceSettingsPanel workspaceId={workspaceId} />
  return <IntegrationsSettingsPanel workspaceId={workspaceId} />
}

export function SettingsModal({
  open,
  onOpenChange,
  activeSection,
  onActiveSectionChange,
  profile,
  workspaceOptions,
  workspaceId,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  activeSection: SettingsSectionId
  onActiveSectionChange: (section: SettingsSectionId) => void
  profile: WorkspaceProfile
  workspaceOptions: IdentityWorkspace[]
  workspaceId?: string
}) {
  const [generalSaveStatus, setGeneralSaveStatus] = useState<SettingsSaveStatus>({ canSave: false, saving: false })
  const [modelSaveStatus, setModelSaveStatus] = useState<SettingsSaveStatus>({ canSave: false, saving: false })
  const [mobileSectionOpen, setMobileSectionOpen] = useState(false)
  const initialMobileSection = useRef<SettingsSectionId | null>(null)
  const activeSectionMeta = settingsSections.find((section) => section.id === activeSection) ?? settingsSections[0]
  const handleGeneralSaveStatusChange = useCallback((status: SettingsSaveStatus) => {
    setGeneralSaveStatus((current) =>
      current.canSave === status.canSave && current.saving === status.saving ? current : status,
    )
  }, [])
  const handleModelSaveStatusChange = useCallback((status: SettingsSaveStatus) => {
    setModelSaveStatus((current) =>
      current.canSave === status.canSave && current.saving === status.saving ? current : status,
    )
  }, [])

  useEffect(() => {
    if (!open) {
      initialMobileSection.current = null
      setMobileSectionOpen(false)
      return
    }
    if (initialMobileSection.current === null) {
      initialMobileSection.current = activeSection
      setMobileSectionOpen(activeSection !== defaultSettingsSectionId)
      return
    }
    if (initialMobileSection.current !== activeSection) {
      initialMobileSection.current = activeSection
      if (activeSection !== defaultSettingsSectionId) setMobileSectionOpen(true)
    }
  }, [activeSection, open])

  return (
    <Dialog modal={false} open={open} onOpenChange={onOpenChange}>
      <DialogContent className="h-[min(760px,calc(100vh-2rem))] grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0 sm:max-w-[980px]">
        <DialogHeader className="border-b px-5 py-4">
          <div className="flex items-center gap-3">
            {mobileSectionOpen ? (
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                className="md:hidden"
                aria-label="Back to settings sections"
                onClick={() => setMobileSectionOpen(false)}
              >
                <ArrowLeftIcon />
              </Button>
            ) : null}
            <span className={cn("size-9 items-center justify-center rounded-lg border bg-card", mobileSectionOpen ? "hidden md:flex" : "flex")}>
              <SettingsIcon />
            </span>
            <div>
              <DialogTitle className={cn(mobileSectionOpen && "sr-only md:not-sr-only")}>Settings</DialogTitle>
              {mobileSectionOpen ? (
                <span aria-hidden="true" className="text-sm font-semibold md:hidden">
                  {activeSectionMeta.label}
                </span>
              ) : null}
            </div>
          </div>
        </DialogHeader>
        <div className="grid min-h-0 min-w-0 md:grid-cols-[240px_minmax(0,1fr)]">
          <nav
            className={cn(
              "min-h-0 border-b p-2 md:block md:border-r md:border-b-0",
              mobileSectionOpen ? "hidden" : "block",
            )}
            aria-label="Settings sections"
          >
            <div className="grid gap-1">
              {settingsSections.map((section) => {
                const Icon = section.icon
                const isActive = activeSection === section.id

                return (
                  <Button
                    key={section.id}
                    type="button"
                    variant={isActive ? "secondary" : "ghost"}
                    className="h-auto justify-start rounded-md px-3 py-2 text-left"
                    aria-current={isActive ? "page" : undefined}
                    onClick={() => {
                      onActiveSectionChange(section.id)
                      setMobileSectionOpen(true)
                    }}
                  >
                    <Icon data-icon="inline-start" />
                    <span className="grid min-w-0">
                      <span className="truncate">{section.label}</span>
                      <span className="truncate text-xs font-normal text-muted-foreground">{section.description}</span>
                    </span>
                  </Button>
                )
              })}
            </div>
          </nav>
          <ScrollArea className={cn("h-full min-h-0 min-w-0 overflow-hidden md:block", mobileSectionOpen ? "block" : "hidden")}>
            <div className="min-w-0 p-4 md:p-5">
              <SettingsPanelContent
                activeSection={activeSection}
                profile={profile}
                workspaceOptions={workspaceOptions}
                workspaceId={workspaceId}
                onGeneralSaveStatusChange={handleGeneralSaveStatusChange}
                onModelSaveStatusChange={handleModelSaveStatusChange}
              />
            </div>
          </ScrollArea>
        </div>
        <DialogFooter className="m-0 rounded-none bg-popover px-5 py-3">
          <Button className="rounded-md" onClick={() => onOpenChange(false)}>
            Close
          </Button>
          {activeSection === "general" ? (
            <ActionTooltip content="Save changed General settings to Doc Registry.">
              <span className="inline-flex">
                <Button type="submit" form="general-settings-form" className="rounded-md" disabled={!generalSaveStatus.canSave}>
                  {generalSaveStatus.saving ? "Saving" : "Save"}
                </Button>
              </span>
            </ActionTooltip>
          ) : null}
          {activeSection === "models" ? (
            <ActionTooltip content="Save changed model settings to Doc Registry.">
              <span className="inline-flex">
                <Button type="submit" form="model-settings-form" className="rounded-md" disabled={!modelSaveStatus.canSave}>
                  {modelSaveStatus.saving ? "Saving" : "Save"}
                </Button>
              </span>
            </ActionTooltip>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
