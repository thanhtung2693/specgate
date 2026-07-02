// Settings domain: general/model/governance/plugins/integrations panels and
// the Settings modal. Extracted from app-shell.
import { ArrowLeftIcon, CopyIcon, DatabaseIcon, EyeIcon, KeyRoundIcon, PlugIcon, PlusIcon, ShieldCheckIcon, SlidersHorizontalIcon, SettingsIcon } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react"

import { GitHubIcon, GitLabIcon, LinearIcon, ProviderBrandIcon } from "@/components/brand-icons"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import {
  defaultGeneralSettings,
  editableGeneralSettings,
  loadGeneralSettings,
  saveGeneralSettings,
  type GeneralSettingsData,
} from "@/data/general-settings"
import {
  beginPendingIntegrationOAuth,
  createIntegration,
  createIntegrationResource,
  integrationConfigJson,
  integrationProviders,
  integrationsBase,
  listIntegrationRepos,
  listIntegrationResources,
  listIntegrationWebhookEvents,
  listIntegrations,
  listLinearTeams,
  oauthRedirectTarget,
  providerDefinition,
  setIntegrationApiToken,
  type IntegrationProvider,
  type IntegrationResourceCandidate,
  type IntegrationResourceSummary,
  type IntegrationSummary,
  type IntegrationWebhookEventSummary,
} from "@/data/integrations"
import {
  loadGovernanceCatalog,
  type GovernanceCatalog,
  type GovernanceOutcomeFeedbackSummary,
  type GovernancePolicyHealthSummary,
  type GovernancePolicyLevelSummary,
  type GovernanceProfileSummary,
} from "@/data/governance"
import {
  docRegistryBase,
  embeddingModelsForProvider,
  embeddingProviderKey,
  embeddingProviderLabel,
  embeddingProviders,
  fetchOpenRouterEmbeddingModels,
  fetchOpenRouterModels,
  defaultModelSettings,
  governanceThinkingLevels,
  loadModelSettings,
  modelsForProvider,
  providerKey,
  providerLabel,
  saveModelSettings,
  type EmbeddingModelOption,
  type EmbeddingModelProvider,
  type GovernanceModelOption,
  type GovernanceModelProvider,
  type ModelSettingsData,
  type OpenRouterModel,
} from "@/data/model-settings"
import {
  createRegistrySkill,
  deleteRegistrySkill,
  getRegistrySkill,
  listRegistrySkills,
  updateRegistrySkill,
  type RegistrySkillDetail,
  type RegistrySkillInput,
  type RegistrySkillSummary,
} from "@/data/skills"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { readableKey, statusTone, toneClass, type WorkspaceProfile } from "./shared"
import { ActionTooltip, copyText, MarkdownText } from "./shared-ui"

const pluginCliCommands = [
  { label: "Install IDE plugins", command: "specgate plugins install" },
  { label: "Verify plugin health", command: "specgate plugins doctor" },
  { label: "Update SpecGate", command: "specgate update" },
]

function PluginsSettingsPanel() {
  const [copiedCommand, setCopiedCommand] = useState<string | null>(null)

  function copyPluginCommand(command: string) {
    void copyText(command).then((didCopy) => {
      if (!didCopy) return
      setCopiedCommand(command)
      window.setTimeout(() => setCopiedCommand((current) => (current === command ? null : current)), 1600)
    })
  }

  return (
    <section className="grid min-w-0 gap-4">
      <div>
        <h2 className="text-sm font-semibold">IDE plugins</h2>
        <p className="mt-2 text-sm break-words text-muted-foreground">
          IDE plugins are installed and updated via the CLI; the browser cannot inspect local IDE files.
        </p>
      </div>
      <div className="grid min-w-0 gap-2">
        {pluginCliCommands.map((entry) => (
          <div key={entry.command} className="flex min-w-0 items-center gap-2 rounded-md border bg-card/70 px-2 py-1.5">
            <span className="w-36 shrink-0 text-xs text-muted-foreground">{entry.label}</span>
            <code className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground">{entry.command}</code>
            <ActionTooltip content="Copy plugin CLI command.">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 shrink-0 rounded-md px-2"
                onClick={() => copyPluginCommand(entry.command)}
              >
                <CopyIcon data-icon="inline-start" />
                {copiedCommand === entry.command ? "Copied" : "Copy"}
              </Button>
            </ActionTooltip>
          </div>
        ))}
      </div>
    </section>
  )
}

function TeamSkillsPanel() {
  const [registrySkills, setRegistrySkills] = useState<RegistrySkillSummary[]>([])
  const [registrySkillsStatus, setRegistrySkillsStatus] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [selectedSkill, setSelectedSkill] = useState<RegistrySkillSummary | null>(null)
  const [skillForm, setSkillForm] = useState<{ mode: "create" } | null>(null)
  const [skillToDelete, setSkillToDelete] = useState<RegistrySkillSummary | null>(null)
  const [skillDetails, setSkillDetails] = useState<Record<string, RegistrySkillDetail>>({})
  const [deleteStatus, setDeleteStatus] = useState<"idle" | "deleting" | "error">("idle")
  const registryBase = docRegistryBase()

  useEffect(() => {
    if (!registryBase) {
      setRegistrySkills([])
      setRegistrySkillsStatus("idle")
      return
    }

    const controller = new AbortController()
    setRegistrySkillsStatus("loading")
    void listRegistrySkills(registryBase, controller.signal)
      .then((skills) => {
        setRegistrySkills(skills)
        setRegistrySkillsStatus("ready")
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setRegistrySkills([])
        setRegistrySkillsStatus("error")
      })

    return () => controller.abort()
  }, [registryBase])

  async function saveSkill(input: RegistrySkillInput, skillId?: string) {
    if (!registryBase) throw new Error("Doc Registry is not configured")
    const saved = skillId
      ? await updateRegistrySkill(registryBase, skillId, input)
      : await createRegistrySkill(registryBase, input)
    const summary: RegistrySkillSummary = {
      id: saved.id,
      name: saved.name,
      description: saved.description,
      updatedAt: saved.updatedAt,
    }
    setRegistrySkills((current) => [summary, ...current.filter((skill) => skill.id !== saved.id)])
    setSkillDetails((current) => ({ ...current, [saved.id]: saved }))
    if (selectedSkill?.id === saved.id || skillId) {
      setSelectedSkill(summary)
    }
  }

  async function confirmDeleteSkill() {
    if (!registryBase || !skillToDelete) return
    setDeleteStatus("deleting")
    try {
      await deleteRegistrySkill(registryBase, skillToDelete.id)
      setRegistrySkills((current) => current.filter((skill) => skill.id !== skillToDelete.id))
      setSkillDetails((current) => {
        const next = { ...current }
        delete next[skillToDelete.id]
        return next
      })
      if (selectedSkill?.id === skillToDelete.id) setSelectedSkill(null)
      setSkillToDelete(null)
      setDeleteStatus("idle")
    } catch {
      setDeleteStatus("error")
    }
  }

  return (
    <section className="grid min-w-0 gap-5">
      <div className="grid min-w-0 gap-3">
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 className="text-sm font-semibold">Team rubric Skills</h2>
            <p className="mt-2 text-sm break-words text-muted-foreground">
              Team Skills are governance rubrics that can appear in gates and Context Packs. IDE plugin installation stays CLI-managed.
            </p>
          </div>
          {registrySkillsStatus === "ready" ? (
            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" variant="outline" size="sm" className="rounded-md" onClick={() => setSkillForm({ mode: "create" })}>
                <PlusIcon data-icon="inline-start" />
                Add Skill
              </Button>
            </div>
          ) : null}
        </div>
        {!registryBase ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">
            Configure VITE_DOC_REGISTRY_URL to view team rubric Skills from Doc Registry.
          </p>
        ) : registrySkillsStatus === "loading" ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">Loading team rubric Skills...</p>
        ) : registrySkillsStatus === "error" ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">Team rubric Skills unavailable.</p>
        ) : registrySkills.length === 0 ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">No team rubric Skills found.</p>
        ) : (
          <div className="grid min-w-0 gap-2">
            {registrySkills.map((skill) => (
              <div key={skill.id} className="min-w-0 overflow-hidden rounded-md border bg-background/70 p-3">
                <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="min-w-0 truncate font-mono text-xs font-medium text-foreground">{skill.name}</span>
                    <ActionTooltip content="Inspect and manage this team rubric Skill.">
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        className="shrink-0 rounded-md"
                        aria-label={`Inspect ${skill.name} Skill`}
                        onClick={() => setSelectedSkill(skill)}
                      >
                        <EyeIcon />
                      </Button>
                    </ActionTooltip>
                  </div>
                  {skill.updatedAt ? (
                    <span className="text-xs text-muted-foreground" title={formatDateTime(skill.updatedAt)}>
                      {formatRelativeTime(skill.updatedAt)}
                    </span>
                  ) : null}
                </div>
                <p className="mt-2 text-sm leading-5 break-words text-muted-foreground">{skill.description}</p>
              </div>
            ))}
          </div>
        )}
      </div>
      <RegistrySkillDetailDialog
        baseUrl={registryBase}
        skill={selectedSkill}
        overrideDetail={selectedSkill ? skillDetails[selectedSkill.id] : undefined}
        open={selectedSkill !== null}
        onLoaded={(detail) => setSkillDetails((current) => ({ ...current, [detail.id]: detail }))}
        onSave={(detail, input) => saveSkill(input, detail.id)}
        onDelete={(skill) => setSkillToDelete(skill)}
        onOpenChange={(open) => {
          if (!open) setSelectedSkill(null)
        }}
      />
      <RegistrySkillFormDialog
        state={skillForm}
        onOpenChange={(open) => {
          if (!open) setSkillForm(null)
        }}
        onSubmit={saveSkill}
      />
      <Dialog
        open={skillToDelete !== null}
        onOpenChange={(open) => {
          if (!open) {
            setSkillToDelete(null)
            setDeleteStatus("idle")
          }
        }}
      >
        <DialogContent className="sm:max-w-[440px]">
          <DialogHeader>
            <DialogTitle>Delete {skillToDelete?.name ?? "Skill"} Skill?</DialogTitle>
            <DialogDescription>
              Delete this team rubric from Doc Registry. Existing gate snapshots keep their recorded Skill names, but future lookups will not resolve this prompt.
            </DialogDescription>
          </DialogHeader>
          {deleteStatus === "error" ? <p className="text-sm text-destructive">Failed to delete Skill.</p> : null}
          <DialogFooter className="pb-[calc(0.75rem+env(safe-area-inset-bottom))]">
            <Button type="button" variant="outline" className="rounded-md" disabled={deleteStatus === "deleting"} onClick={() => setSkillToDelete(null)}>
              Cancel
            </Button>
            <Button type="button" variant="destructive" className="rounded-md" disabled={deleteStatus === "deleting"} onClick={() => void confirmDeleteSkill()}>
              {deleteStatus === "deleting" ? "Deleting" : "Delete Skill"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}

function RegistrySkillDetailDialog({
  baseUrl,
  skill,
  overrideDetail,
  open,
  onLoaded,
  onSave,
  onDelete,
  onOpenChange,
}: {
  baseUrl: string | null
  skill: RegistrySkillSummary | null
  overrideDetail?: RegistrySkillDetail
  open: boolean
  onLoaded: (detail: RegistrySkillDetail) => void
  onSave: (detail: RegistrySkillDetail, input: RegistrySkillInput) => Promise<void>
  onDelete: (skill: RegistrySkillSummary) => void
  onOpenChange: (open: boolean) => void
}) {
  const [detail, setDetail] = useState<RegistrySkillDetail | null>(null)
  const [status, setStatus] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [editing, setEditing] = useState(false)
  const [description, setDescription] = useState("")
  const [prompt, setPrompt] = useState("")
  const [saveStatus, setSaveStatus] = useState<"idle" | "saving" | "error">("idle")

  useEffect(() => {
    if (!open || !skill || !baseUrl) {
      setDetail(null)
      setStatus("idle")
      setEditing(false)
      setSaveStatus("idle")
      return
    }

    if (overrideDetail) {
      setDetail(overrideDetail)
      setStatus("ready")
      return
    }

    const controller = new AbortController()
    setDetail(null)
    setStatus("loading")
    void getRegistrySkill(baseUrl, skill.id, controller.signal)
      .then((loaded) => {
        setDetail(loaded)
        onLoaded(loaded)
        setStatus("ready")
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setDetail(null)
        setStatus("error")
      })

    return () => controller.abort()
  }, [baseUrl, onLoaded, open, overrideDetail, skill])

  useEffect(() => {
    if (!detail) return
    setDescription(detail.description)
    setPrompt(detail.prompt)
    setSaveStatus("idle")
  }, [detail])

  const display = detail ?? skill
  const canSave = Boolean(detail) && prompt.trim().length > 0 && saveStatus !== "saving"

  async function saveInlineEdit() {
    if (!detail || !canSave) return
    setSaveStatus("saving")
    try {
      await onSave(detail, {
        name: detail.name,
        description: description.trim(),
        prompt,
      })
      const next = { ...detail, description: description.trim(), prompt }
      setDetail(next)
      setEditing(false)
      setSaveStatus("idle")
    } catch {
      setSaveStatus("error")
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[calc(100svh-3rem)] flex-col overflow-hidden p-0 sm:max-w-3xl">
        <DialogHeader className="shrink-0 border-b px-5 py-4">
          <DialogTitle>{display?.name ?? "Skill detail"}</DialogTitle>
          <DialogDescription>
            Rubric used by governance gates and Context Packs.
          </DialogDescription>
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto">
          <div className="grid gap-4 p-5">
            {status === "loading" ? (
              <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">Loading Skill prompt...</p>
            ) : status === "error" ? (
              <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">Skill detail unavailable.</p>
            ) : detail ? (
              <>
                <div className="rounded-md border bg-background/70 p-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="font-mono text-xs font-medium text-foreground">{detail.id}</span>
                    {detail.updatedAt ? (
                      <span className="text-xs text-muted-foreground" title={formatDateTime(detail.updatedAt)}>
                        Updated {formatRelativeTime(detail.updatedAt)}
                      </span>
                    ) : null}
                  </div>
                  {editing ? (
                    <label className="mt-3 grid gap-2">
                      <span className="text-xs font-medium text-muted-foreground">Description</span>
                      <Textarea
                        value={description}
                        onChange={(event) => setDescription(event.target.value)}
                        className="h-24 resize-y overflow-auto"
                        aria-label="Description"
                      />
                    </label>
                  ) : (
                    <p className="mt-2 text-sm leading-6 text-muted-foreground">{detail.description}</p>
                  )}
                </div>
                <section className="grid gap-2">
                  <h3 className="text-sm font-semibold">Prompt</h3>
                  {editing ? (
                    <Textarea
                      value={prompt}
                      onChange={(event) => setPrompt(event.target.value)}
                      className="h-72 max-h-[40svh] resize-y overflow-auto font-mono text-xs"
                      aria-label="Prompt"
                    />
                  ) : (
                    <div className="max-h-[420px] overflow-auto rounded-md border bg-card/55 p-3 text-sm leading-6">
                      <MarkdownText content={detail.prompt} compact />
                    </div>
                  )}
                </section>
                {saveStatus === "error" ? <p className="text-sm text-destructive">Failed to save Skill.</p> : null}
              </>
            ) : null}
          </div>
        </div>
        <DialogFooter className="shrink-0 border-t px-5 pt-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))]">
          {detail ? (
            editing ? (
              <>
                <Button type="button" variant="outline" size="sm" className="rounded-md" disabled={saveStatus === "saving"} onClick={() => setEditing(false)}>
                  Cancel
                </Button>
                <Button type="button" size="sm" className="rounded-md" disabled={!canSave} onClick={() => void saveInlineEdit()}>
                  {saveStatus === "saving" ? "Saving" : "Save Skill"}
                </Button>
              </>
            ) : (
              <>
                <Button type="button" variant="outline" size="sm" className="rounded-md" onClick={() => setEditing(true)}>
                  Edit Skill
                </Button>
                <Button type="button" variant="destructive" size="sm" className="rounded-md" onClick={() => onDelete(detail)}>
                  Delete Skill
                </Button>
              </>
            )
          ) : null}
          {!editing ? (
            <DialogClose asChild>
              <Button type="button" variant="default" size="sm" className="rounded-md">
                Close
              </Button>
            </DialogClose>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function RegistrySkillFormDialog({
  state,
  onOpenChange,
  onSubmit,
}: {
  state: { mode: "create" } | null
  onOpenChange: (open: boolean) => void
  onSubmit: (input: RegistrySkillInput) => Promise<void>
}) {
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [prompt, setPrompt] = useState("")
  const [status, setStatus] = useState<"idle" | "saving" | "error">("idle")
  const open = state !== null
  const canSave = name.trim().length > 0 && prompt.trim().length > 0 && status !== "saving"

  useEffect(() => {
    if (!state) {
      setName("")
      setDescription("")
      setPrompt("")
      setStatus("idle")
      return
    }
    setName("")
    setDescription("")
    setPrompt("")
    setStatus("idle")
  }, [state])

  async function submit() {
    if (!canSave) return
    setStatus("saving")
    try {
      await onSubmit({ name: name.trim(), description: description.trim(), prompt })
      setStatus("idle")
      onOpenChange(false)
    } catch {
      setStatus("error")
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(760px,calc(100svh-2rem))] flex-col overflow-hidden p-0 sm:max-w-2xl">
        <DialogHeader className="shrink-0 border-b px-5 py-4">
          <DialogTitle>Add Skill</DialogTitle>
          <DialogDescription>
            Manage the registry rubric prompt used by governance gates and Context Packs.
          </DialogDescription>
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto">
          <form
            id="registry-skill-form"
            className="grid gap-4 p-5"
            onSubmit={(event) => {
              event.preventDefault()
              void submit()
            }}
          >
            <label className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Skill name</span>
              <Input value={name} onChange={(event) => setName(event.target.value)} autoComplete="off" />
            </label>
            <label className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Description</span>
              <Textarea value={description} onChange={(event) => setDescription(event.target.value)} className="h-24 resize-y overflow-auto" />
            </label>
            <label className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Prompt</span>
              <Textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} className="h-56 resize-y overflow-auto font-mono text-xs" />
            </label>
            {status === "error" ? <p className="text-sm text-destructive">Failed to save Skill.</p> : null}
          </form>
        </div>
        <DialogFooter className="shrink-0 border-t px-5 pt-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))]">
          <Button type="button" variant="outline" className="rounded-md" disabled={status === "saving"} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" form="registry-skill-form" className="rounded-md" disabled={!canSave}>
            {status === "saving" ? "Saving" : "Create Skill"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function GovernanceSettingsPanel() {
  return (
    <section className="grid gap-6">
      <div>
        <h2 className="text-sm font-semibold">Governance</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Team rubric Skills are editable here. The policy catalog below is read-only context; policy edits stay in CLI and backend flows.
        </p>
      </div>
      <TeamSkillsPanel />
      <GovernanceCatalogSection />
    </section>
  )
}

function GovernanceCatalogSection() {
  const [catalog, setCatalog] = useState<GovernanceCatalog | null>(null)
  const [status, setStatus] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const registryBase = docRegistryBase()

  useEffect(() => {
    if (!registryBase) {
      setCatalog(null)
      setStatus("idle")
      return
    }

    const controller = new AbortController()
    setStatus("loading")
    void loadGovernanceCatalog(registryBase, controller.signal)
      .then((nextCatalog) => {
        setCatalog(nextCatalog)
        setStatus("ready")
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setCatalog(null)
        setStatus("error")
      })

    return () => controller.abort()
  }, [registryBase])

  return (
    <section className="grid gap-4 border-t pt-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">Policy catalog</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Read-only policy tiers, profiles, and outcome health for the CLI and IDE-agent delivery loop.
          </p>
        </div>
        {status === "ready" ? (
          <Badge variant="outline" className={cn("border", toneClass("success"))}>
            Live
          </Badge>
        ) : null}
      </div>
      {!registryBase ? (
        <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">
          Configure VITE_DOC_REGISTRY_URL to view governance policy and profile context.
        </p>
      ) : status === "loading" ? (
        <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">Loading governance catalog...</p>
      ) : status === "error" ? (
        <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">Governance catalog unavailable.</p>
      ) : catalog ? (
        <div className="grid gap-5">
          <GovernanceHealthPanel health={catalog.policyHealth} feedback={catalog.outcomeFeedback} />
          <GovernancePolicyLevels levels={catalog.policyLevels} />
          <GovernanceProfiles profiles={catalog.profiles} />
        </div>
      ) : null}
    </section>
  )
}

function GovernanceHealthPanel({
  health,
  feedback,
}: {
  health: GovernancePolicyHealthSummary[]
  feedback: GovernanceOutcomeFeedbackSummary[]
}) {
  const totals = health.reduce(
    (acc, item) => ({
      feedback: acc.feedback + item.totalFeedback,
      overrides: acc.overrides + item.overrideCount,
      rejected: acc.rejected + item.rejectedEvidenceCount,
      rollbacks: acc.rollbacks + item.postMergeRollbackCount,
      escaped: acc.escaped + item.escapedDefectCount,
    }),
    { feedback: 0, overrides: 0, rejected: 0, rollbacks: 0, escaped: 0 },
  )

  return (
    <section className="grid gap-3">
      <div>
        <h3 className="text-sm font-semibold">Governance health</h3>
        <p className="mt-1 text-xs text-muted-foreground">
          Outcome signals recorded by CLI, MCP, agents, and reviewers.
        </p>
      </div>
      {health.length === 0 && feedback.length === 0 ? (
        <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">
          No outcome feedback recorded yet, or the outcome store is not configured.
        </p>
      ) : (
        <div className="grid gap-3">
          <div className="grid gap-2 md:grid-cols-5">
            <GovernanceHealthMetric label="Signals" value={totals.feedback} />
            <GovernanceHealthMetric label="Overrides" value={totals.overrides} />
            <GovernanceHealthMetric label="Rejected evidence" value={totals.rejected} />
            <GovernanceHealthMetric label="Rollbacks" value={totals.rollbacks} />
            <GovernanceHealthMetric label="Escaped defects" value={totals.escaped} />
          </div>
          {health.length > 0 ? (
            <div className="grid gap-2">
              {health.slice(0, 5).map((policy) => (
                <GovernancePolicyHealthRow key={policy.policyId} policy={policy} />
              ))}
              {health.length > 5 ? (
                <p className="px-1 text-xs text-muted-foreground">Showing 5 of {health.length} policy health rows.</p>
              ) : null}
            </div>
          ) : null}
          {feedback.length > 0 ? (
            <div className="grid gap-2">
              <p className="text-xs font-semibold text-muted-foreground">Outcome feedback</p>
              {feedback.slice(0, 5).map((item) => (
                <GovernanceOutcomeFeedbackRow key={item.id} item={item} />
              ))}
            </div>
          ) : null}
        </div>
      )}
    </section>
  )
}

function GovernanceHealthMetric({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-md border bg-background/70 p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 font-mono text-lg font-semibold">{value}</p>
    </div>
  )
}

function GovernancePolicyHealthRow({ policy }: { policy: GovernancePolicyHealthSummary }) {
  return (
    <div className="rounded-md border bg-background/70 p-3">
      <div className="min-w-0">
        <p className="font-medium">{policy.policyId}</p>
        <p className="mt-1 text-xs text-muted-foreground">
          {policy.totalFeedback} signals · {policy.overrideCount} overrides · {policy.escapedDefectCount} escaped defects
        </p>
      </div>
      {policy.gateBreakdown.length > 0 ? (
        <div className="mt-3 flex flex-wrap gap-1.5">
          {policy.gateBreakdown.slice(0, 6).map((gate) => (
            <span key={gate.gateKey} className="rounded-md border bg-card/70 px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
              {gate.gateKey}: {gate.overrideCount}
            </span>
          ))}
          {policy.gateBreakdown.length > 6 ? <span className="px-1.5 py-0.5 text-[11px]">+{policy.gateBreakdown.length - 6}</span> : null}
        </div>
      ) : null}
    </div>
  )
}

function GovernanceOutcomeFeedbackRow({ item }: { item: GovernanceOutcomeFeedbackSummary }) {
  return (
    <div className="rounded-md border bg-background/70 p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="font-medium">{readableKey(item.type)}</p>
          <p className="mt-1 truncate font-mono text-xs text-muted-foreground">{item.workItemId}</p>
        </div>
        {item.recordedAt ? (
          <Badge variant="outline" className="rounded-md border text-[0.7rem]">
            {formatDateTime(item.recordedAt)}
          </Badge>
        ) : null}
      </div>
      <div className="mt-2 flex flex-wrap gap-1.5">
        {item.policyId ? (
          <Badge variant="outline" className="rounded-md font-mono text-[0.68rem]">
            {item.policyId}
          </Badge>
        ) : null}
        {item.gateKey ? (
          <Badge variant="outline" className="rounded-md font-mono text-[0.68rem]">
            {item.gateKey}
          </Badge>
        ) : null}
        {item.actor ? (
          <Badge variant="outline" className="rounded-md text-[0.68rem]">
            {item.actor}
          </Badge>
        ) : null}
      </div>
      {item.reason ? <p className="mt-2 text-xs leading-5 text-muted-foreground">{item.reason}</p> : null}
    </div>
  )
}

function GovernancePolicyLevels({ levels }: { levels: GovernancePolicyLevelSummary[] }) {
  return (
    <section className="grid gap-3">
      <div>
        <h3 className="text-sm font-semibold">Policy tiers</h3>
        <p className="mt-1 text-xs text-muted-foreground">Built-in execution projections used by governed artifact and work-item checks.</p>
      </div>
      {levels.length === 0 ? (
        <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">No policy tiers advertised yet.</p>
      ) : (
        <div className="grid gap-2">
          {levels.map((level) => (
            <GovernanceCatalogRow
              key={level.level}
              title={level.displayName}
              subtitle={level.level}
              approvalPolicy={level.approvalPolicy}
              evidencePolicy={level.evidencePolicy}
              enabledGates={level.enabledGates}
              requiredEvidence={level.requiredEvidence}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function GovernanceProfiles({ profiles }: { profiles: GovernanceProfileSummary[] }) {
  return (
    <section className="grid gap-3">
      <div>
        <h3 className="text-sm font-semibold">Profiles</h3>
        <p className="mt-1 text-xs text-muted-foreground">Resolved built-in and imported profiles with effective approval and evidence policies.</p>
      </div>
      {profiles.length === 0 ? (
        <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">No governance profiles available.</p>
      ) : (
        <div className="grid gap-2">
          {profiles.map((profile) => (
            <GovernanceCatalogRow
              key={profile.id}
              title={profile.displayName}
              subtitle={`${profile.id} · ${profile.source} · v${profile.version}`}
              approvalPolicy={profile.approvalPolicy}
              evidencePolicy={profile.evidencePolicy}
              enabledGates={profile.enabledGates}
              requiredEvidence={profile.requiredEvidence}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function GovernanceCatalogRow({
  title,
  subtitle,
  approvalPolicy,
  evidencePolicy,
  enabledGates,
  requiredEvidence,
}: {
  title: string
  subtitle: string
  approvalPolicy: string
  evidencePolicy: string
  enabledGates: string[]
  requiredEvidence: string[]
}) {
  return (
    <div className="rounded-md border bg-background/70 p-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="font-medium">{title}</p>
          <p className="mt-1 truncate font-mono text-xs text-muted-foreground">{subtitle}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge variant="outline" className="rounded-md font-mono text-[0.7rem]">
            {approvalPolicy}
          </Badge>
          <Badge variant="outline" className="rounded-md font-mono text-[0.7rem]">
            {evidencePolicy}
          </Badge>
        </div>
      </div>
      <div className="mt-3 grid gap-2 text-xs text-muted-foreground md:grid-cols-2">
        <GovernanceTokenList label="Gates" items={enabledGates} />
        <GovernanceTokenList label="Evidence" items={requiredEvidence} />
      </div>
    </div>
  )
}

function GovernanceTokenList({ label, items }: { label: string; items: string[] }) {
  return (
    <div className="min-w-0">
      <p className="font-medium text-foreground/80">{label}</p>
      {items.length === 0 ? (
        <p className="mt-1">None recorded.</p>
      ) : (
        <div className="mt-1 flex flex-wrap gap-1.5">
          {items.slice(0, 6).map((item) => (
            <span key={item} className="rounded-md border bg-card/70 px-1.5 py-0.5 font-mono text-[11px]">
              {item}
            </span>
          ))}
          {items.length > 6 ? <span className="px-1.5 py-0.5 text-[11px]">+{items.length - 6}</span> : null}
        </div>
      )}
    </div>
  )
}

type SettingsSaveStatus = {
  canSave: boolean
  saving: boolean
}

function isValidDays(value: string): boolean {
  const n = Number(value)
  return Number.isInteger(n) && n >= 1 && n <= 3650
}

function sameGeneralSettings(left: GeneralSettingsData, right: GeneralSettingsData): boolean {
  const leftEditable = editableGeneralSettings(left)
  const rightEditable = editableGeneralSettings(right)
  return (Object.keys(leftEditable) as Array<keyof ReturnType<typeof editableGeneralSettings>>).every(
    (key) => leftEditable[key] === rightEditable[key],
  )
}

function GeneralSettingsPanel({ onSaveStatusChange }: { onSaveStatusChange: (status: SettingsSaveStatus) => void }) {
  const base = useMemo(() => docRegistryBase(), [])
  const [settings, setSettings] = useState<GeneralSettingsData>(() => ({ ...defaultGeneralSettings }))
  const [savedSettings, setSavedSettings] = useState<GeneralSettingsData>(() => ({ ...defaultGeneralSettings }))
  const [loading, setLoading] = useState(Boolean(base))
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const dirty = !sameGeneralSettings(settings, savedSettings)
  const valid = isValidDays(settings["governance.feature_freshness_sla_days"]) && isValidDays(settings["governance.artifact_stale_days"])

  useEffect(() => {
    onSaveStatusChange({ canSave: Boolean(base) && dirty && valid && !loading && !saving, saving })
  }, [base, dirty, loading, onSaveStatusChange, saving, valid])

  useEffect(() => {
    if (!base) {
      setLoading(false)
      return
    }

    let cancelled = false
    setLoading(true)
    setError(null)
    loadGeneralSettings(base)
      .then((loaded) => {
        if (!cancelled) {
          setSettings(loaded)
          setSavedSettings(loaded)
        }
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "Failed to load general settings")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [base])

  async function saveSettings() {
    if (!base || !dirty || !valid) return
    setSaving(true)
    setMessage(null)
    setError(null)
    try {
      const saved = await saveGeneralSettings(base, editableGeneralSettings(settings))
      setSettings(saved)
      setSavedSettings(saved)
      setMessage("General settings saved.")
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save general settings")
    } finally {
      setSaving(false)
    }
  }

  return (
    <form
      id="general-settings-form"
      className="grid gap-5"
      onSubmit={(event: FormEvent<HTMLFormElement>) => {
        event.preventDefault()
        void saveSettings()
      }}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">General settings</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            User-facing governance defaults for freshness signals and Feature overview maintenance.
          </p>
        </div>
      </div>

      {!base ? (
        <div className="rounded-lg border border-dashed bg-card/60 p-3 text-sm text-muted-foreground">
          Set <code>VITE_DOC_REGISTRY_URL</code> to load and save General settings.
        </div>
      ) : null}
      {error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-300">
          {error}
        </div>
      ) : null}
      {message ? (
        <div className="rounded-lg border border-green-500/30 bg-green-500/10 p-3 text-sm text-green-700 dark:text-green-300">
          {message}
        </div>
      ) : null}

      <div className="grid gap-3">
        <div className="rounded-lg border bg-background/70 p-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <label htmlFor="auto-feature-summary" className="text-sm font-semibold">
                Auto-refresh Feature overviews
              </label>
              <p className="mt-1 text-sm text-muted-foreground">
                Let the governance service refresh a Feature overview when the canonical artifact changes.
              </p>
            </div>
            <Switch
              id="auto-feature-summary"
              checked={settings["governance.auto_feature_summary"] === "true"}
              disabled={loading}
              onCheckedChange={(checked) => {
                setMessage(null)
                setSettings((current) => ({ ...current, "governance.auto_feature_summary": checked ? "true" : "false" }))
              }}
            />
          </div>
        </div>

        <div className="rounded-lg border bg-background/70 p-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <label htmlFor="auto-archive-delivery-pass" className="text-sm font-semibold">
                Auto-archive after passed delivery review
              </label>
              <p className="mt-1 text-sm text-muted-foreground">
                Hide future completed work from the default board when Doc Registry persists a passing delivery review.
              </p>
            </div>
            <Switch
              id="auto-archive-delivery-pass"
              checked={settings["governance.auto_archive_on_delivery_pass"] === "true"}
              disabled={loading}
              onCheckedChange={(checked) => {
                setMessage(null)
                setSettings((current) => ({
                  ...current,
                  "governance.auto_archive_on_delivery_pass": checked ? "true" : "false",
                }))
              }}
            />
          </div>
        </div>

        <div className="grid gap-3 md:grid-cols-2">
          <label className="grid gap-2 rounded-lg border bg-background/70 p-4">
            <span className="text-sm font-semibold">Feature freshness SLA days</span>
            <Input
              type="number"
              min={1}
              max={3650}
              aria-label="Feature freshness SLA days"
              value={settings["governance.feature_freshness_sla_days"]}
              disabled={loading}
              aria-invalid={!isValidDays(settings["governance.feature_freshness_sla_days"])}
              onChange={(event) => {
                setMessage(null)
                setSettings((current) => ({ ...current, "governance.feature_freshness_sla_days": event.target.value }))
              }}
            />
            <span className="text-xs text-muted-foreground">Escalates stale Feature overview attention after this many days.</span>
          </label>
          <label className="grid gap-2 rounded-lg border bg-background/70 p-4">
            <span className="text-sm font-semibold">Artifact stale after days</span>
            <Input
              type="number"
              min={1}
              max={3650}
              aria-label="Artifact stale after days"
              value={settings["governance.artifact_stale_days"]}
              disabled={loading}
              aria-invalid={!isValidDays(settings["governance.artifact_stale_days"])}
              onChange={(event) => {
                setMessage(null)
                setSettings((current) => ({ ...current, "governance.artifact_stale_days": event.target.value }))
              }}
            />
            <span className="text-xs text-muted-foreground">Flags artifact freshness signals without changing artifact state.</span>
          </label>
        </div>
      </div>
    </form>
  )
}

function ModelSettingsPanel({ onSaveStatusChange }: { onSaveStatusChange: (status: SettingsSaveStatus) => void }) {
  const base = useMemo(() => docRegistryBase(), [])
  const [settings, setSettings] = useState<ModelSettingsData>(() => ({ ...defaultModelSettings }))
  const [savedSettings, setSavedSettings] = useState<ModelSettingsData>(() => ({ ...defaultModelSettings }))
  const [loading, setLoading] = useState(Boolean(base))
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [embeddingQuery, setEmbeddingQuery] = useState("")
  const [openRouterModels, setOpenRouterModels] = useState<OpenRouterModel[]>([])
  const [openRouterEmbeddingModels, setOpenRouterEmbeddingModels] = useState<OpenRouterModel[]>([])
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [embeddingCatalogLoading, setEmbeddingCatalogLoading] = useState(false)
  const [catalogError, setCatalogError] = useState<string | null>(null)
  const [embeddingCatalogError, setEmbeddingCatalogError] = useState<string | null>(null)

  const modelProvider = settings["governance.model_provider"]
  const embeddingProvider = settings["embedding.model_provider"]
  const apiKeyName = providerKey(modelProvider)
  const embeddingApiKeyName = embeddingProviderKey(embeddingProvider)
  const usesOpenRouter = modelProvider === "openrouter"
  const embeddingUsesOpenRouter = embeddingProvider === "openrouter"
  const staticOptions = useMemo(() => modelsForProvider(modelProvider), [modelProvider])
  const staticEmbeddingOptions = useMemo(() => embeddingModelsForProvider(embeddingProvider), [embeddingProvider])
  const openRouterOptions = openRouterModels.length > 0 ? openRouterModels : staticOptions
  const openRouterEmbeddingOptions =
    openRouterEmbeddingModels.length > 0 ? openRouterEmbeddingModels : staticEmbeddingOptions
  const modelOptions: Array<GovernanceModelOption | OpenRouterModel> = usesOpenRouter ? openRouterOptions : staticOptions
  const embeddingModelOptions: Array<EmbeddingModelOption | OpenRouterModel> = embeddingUsesOpenRouter
    ? openRouterEmbeddingOptions
    : staticEmbeddingOptions
  const selectedGovernanceModel = settings["governance.model"]
  const selectedEmbeddingModel = settings["embedding.model"]
  const visibleModelOptions = useMemo(() => {
    const hasSelected = modelOptions.some((model) => model.id === selectedGovernanceModel)
    if (!selectedGovernanceModel || hasSelected) return modelOptions
    return [{ id: selectedGovernanceModel, name: selectedGovernanceModel }, ...modelOptions]
  }, [modelOptions, selectedGovernanceModel])
  const visibleEmbeddingModelOptions = useMemo(() => {
    const hasSelected = embeddingModelOptions.some((model) => model.id === selectedEmbeddingModel)
    if (!selectedEmbeddingModel || hasSelected) return embeddingModelOptions
    return [{ id: selectedEmbeddingModel, name: selectedEmbeddingModel }, ...embeddingModelOptions]
  }, [embeddingModelOptions, selectedEmbeddingModel])
  const filteredModelOptions = useMemo(() => {
    const needle = query.trim().toLowerCase()
    const options = visibleModelOptions.slice(0, needle ? visibleModelOptions.length : 8)
    if (!needle) return options.slice(0, 8)
    return options
      .filter((model) => `${model.name} ${model.id}`.toLowerCase().includes(needle))
      .slice(0, 8)
  }, [query, visibleModelOptions])
  const filteredEmbeddingModelOptions = useMemo(() => {
    const needle = embeddingQuery.trim().toLowerCase()
    const options = visibleEmbeddingModelOptions.slice(0, needle ? visibleEmbeddingModelOptions.length : 8)
    if (!needle) return options.slice(0, 8)
    return options
      .filter((model) => `${model.name} ${model.id}`.toLowerCase().includes(needle))
      .slice(0, 8)
  }, [embeddingQuery, visibleEmbeddingModelOptions])
  const customSlug = query.trim()
  const customEmbeddingSlug = embeddingQuery.trim()
  const canUseCustomSlug = usesOpenRouter && customSlug.includes("/") && !visibleModelOptions.some((model) => model.id === customSlug)
  const canUseCustomEmbeddingSlug =
    embeddingUsesOpenRouter &&
    customEmbeddingSlug.includes("/") &&
    !visibleEmbeddingModelOptions.some((model) => model.id === customEmbeddingSlug)
  const dirty = !sameModelSettings(settings, savedSettings)

  useEffect(() => {
    onSaveStatusChange({ canSave: Boolean(base) && dirty && !loading && !saving, saving })
  }, [base, dirty, loading, onSaveStatusChange, saving])

  useEffect(() => {
    if (!base) {
      setLoading(false)
      return
    }

    let cancelled = false
    setLoading(true)
    setError(null)
    loadModelSettings(base)
      .then((loaded) => {
        if (!cancelled) {
          setSettings(loaded)
          setSavedSettings(loaded)
        }
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "Failed to load model settings")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [base])

  useEffect(() => {
    if (!usesOpenRouter || openRouterModels.length > 0) return

    const controller = new AbortController()
    setCatalogLoading(true)
    setCatalogError(null)
    fetchOpenRouterModels(controller.signal)
      .then(setOpenRouterModels)
      .catch((reason: unknown) => {
        if (!controller.signal.aborted) {
          setCatalogError(reason instanceof Error ? reason.message : "OpenRouter catalog unavailable")
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setCatalogLoading(false)
      })

    return () => controller.abort()
  }, [usesOpenRouter, openRouterModels.length])

  useEffect(() => {
    if (!embeddingUsesOpenRouter || openRouterEmbeddingModels.length > 0) return

    const controller = new AbortController()
    setEmbeddingCatalogLoading(true)
    setEmbeddingCatalogError(null)
    fetchOpenRouterEmbeddingModels(controller.signal)
      .then(setOpenRouterEmbeddingModels)
      .catch((reason: unknown) => {
        if (!controller.signal.aborted) {
          setEmbeddingCatalogError(reason instanceof Error ? reason.message : "OpenRouter embedding catalog unavailable")
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setEmbeddingCatalogLoading(false)
      })

    return () => controller.abort()
  }, [embeddingUsesOpenRouter, openRouterEmbeddingModels.length])

  function selectProvider(provider: GovernanceModelProvider) {
    setQuery("")
    setMessage(null)
    setSettings((current) => {
      const firstModel = modelsForProvider(provider)[0]?.id ?? current["governance.model"]
      return {
        ...current,
        "governance.model_provider": provider,
        "governance.model": firstModel,
      }
    })
  }

  function selectEmbeddingProvider(provider: EmbeddingModelProvider) {
    setEmbeddingQuery("")
    setMessage(null)
    setSettings((current) => {
      const firstModel = embeddingModelsForProvider(provider)[0]?.id ?? ""
      return {
        ...current,
        "embedding.model_provider": provider,
        "embedding.model": provider ? firstModel : "",
      }
    })
  }

  async function saveSettings() {
    if (!base || !dirty) return
    setSaving(true)
    setMessage(null)
    setError(null)
    try {
      const saved = await saveModelSettings(base, settings)
      setSettings(saved)
      setSavedSettings(saved)
      setMessage("Model settings saved.")
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save model settings")
    } finally {
      setSaving(false)
    }
  }

  return (
    <form
      id="model-settings-form"
      className="grid gap-5"
      onSubmit={(event: FormEvent<HTMLFormElement>) => {
        event.preventDefault()
        void saveSettings()
      }}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Model settings</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            Server-side model for governance operations — readiness gates, route
            classification, summaries, and delivery review. The governance chat agent
            uses a separately configured (environment) model.
          </p>
        </div>
      </div>

      {!base ? (
        <div className="rounded-lg border border-dashed bg-card/60 p-3 text-sm text-muted-foreground">
          Set <code>VITE_DOC_REGISTRY_URL</code> to load and save model settings.
        </div>
      ) : null}
      {error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-300">
          {error}
        </div>
      ) : null}
      {message ? (
        <div className="rounded-lg border border-green-500/30 bg-green-500/10 p-3 text-sm text-green-700 dark:text-green-300">
          {message}
        </div>
      ) : null}

      <div className="grid gap-5">
        <div className="rounded-lg border bg-background/70 p-4">
          <div className="flex items-center gap-2">
            <KeyRoundIcon className="size-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">Server-side model</h3>
          </div>
          <div className="mt-4 grid gap-3">
            <div className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Provider</span>
              <div className="flex flex-wrap gap-2">
                {(["openrouter", "openai", "anthropic", "google_genai"] as GovernanceModelProvider[]).map((provider) => (
                  <Button
                    key={provider}
                    type="button"
                    variant={modelProvider === provider ? "secondary" : "outline"}
                    size="sm"
                    className="rounded-md"
                    disabled={loading}
                    onClick={() => selectProvider(provider)}
                  >
                    <ProviderBrandIcon provider={provider} className="size-4" />
                    {providerLabel(provider)}
                  </Button>
                ))}
              </div>
            </div>
            <label className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">{providerLabel(modelProvider)} API key</span>
              <Input
                type="password"
                value={settings[apiKeyName]}
                placeholder="Leave *** to keep existing secret"
                disabled={loading}
                onChange={(event) => setSettings((current) => ({ ...current, [apiKeyName]: event.target.value }))}
              />
            </label>
            <div className="grid gap-2">
              <div className="flex items-center justify-between gap-3">
                <span className="text-xs font-medium text-muted-foreground">Model</span>
                {usesOpenRouter ? (
                  <span className="text-xs text-muted-foreground">
                    {catalogLoading ? "Loading OpenRouter catalog" : catalogError ? "Using seeded suggestions" : "OpenRouter catalog"}
                  </span>
                ) : null}
              </div>
              <Input
                value={query}
                placeholder={usesOpenRouter ? "Search models or type vendor/model" : "Search models"}
                disabled={loading}
                onChange={(event) => setQuery(event.target.value)}
              />
              <div className="grid overflow-hidden rounded-lg border">
                {canUseCustomSlug ? (
                  <button
                    type="button"
                    className="flex items-center justify-between gap-3 border-b px-3 py-2 text-left text-sm hover:bg-muted/60"
                    onClick={() => {
                      setSettings((current) => ({ ...current, "governance.model": customSlug }))
                      setQuery("")
                    }}
                  >
                    <span>Use {customSlug}</span>
                    <span className="font-mono text-xs text-muted-foreground">custom</span>
                  </button>
                ) : null}
                {filteredModelOptions.map((model) => (
                  <button
                    key={model.id}
                    type="button"
                    className={cn(
                      "flex items-center justify-between gap-3 border-b px-3 py-2 text-left text-sm last:border-b-0 hover:bg-muted/60",
                      settings["governance.model"] === model.id && "bg-muted/45",
                    )}
                    onClick={() => {
                      setSettings((current) => ({ ...current, "governance.model": model.id }))
                      setQuery("")
                    }}
                  >
                    <span className="min-w-0">
                      <span className="block truncate font-medium">{model.name}</span>
                      <span className="block truncate font-mono text-xs text-muted-foreground">{model.id}</span>
                    </span>
                    {settings["governance.model"] === model.id ? (
                      <Badge variant="outline" className={cn("shrink-0 border", toneClass("success"))}>
                        Selected
                      </Badge>
                    ) : null}
                  </button>
                ))}
                {filteredModelOptions.length === 0 && !canUseCustomSlug ? (
                  <div className="px-3 py-4 text-sm text-muted-foreground">No model found.</div>
                ) : null}
              </div>
              {catalogError ? <p className="text-xs text-muted-foreground">{catalogError}</p> : null}
            </div>
            <div className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Reasoning effort</span>
              <div className="flex flex-wrap gap-2">
                {governanceThinkingLevels.map((level) => (
                  <Button
                    key={level.id}
                    type="button"
                    variant={settings["governance.default_thinking_level"] === level.id ? "secondary" : "outline"}
                    size="sm"
                    className="rounded-md"
                    disabled={loading}
                    onClick={() => setSettings((current) => ({ ...current, "governance.default_thinking_level": level.id }))}
                  >
                    {level.label}
                  </Button>
                ))}
              </div>
            </div>
          </div>
        </div>

        <div className="rounded-lg border bg-background/70 p-4">
          <div className="flex items-center gap-2">
            <DatabaseIcon className="size-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">Embedding model</h3>
          </div>
          <p className="mt-3 text-sm text-muted-foreground">
            Optional model for knowledge indexing and semantic search. Leave disabled if you do not use knowledge upload/search.
          </p>
          <div className="mt-4 grid gap-3">
            <div className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Provider</span>
              <div className="flex flex-wrap gap-2">
                {embeddingProviders.map((provider) => (
                  <Button
                    key={provider.id || "disabled"}
                    type="button"
                    variant={embeddingProvider === provider.id ? "secondary" : "outline"}
                    size="sm"
                    className="rounded-md"
                    disabled={loading}
                    onClick={() => selectEmbeddingProvider(provider.id)}
                  >
                    {provider.id ? <ProviderBrandIcon provider={provider.id} className="size-4" /> : null}
                    {provider.label}
                  </Button>
                ))}
              </div>
            </div>
            {embeddingProvider && embeddingApiKeyName ? (
              <>
                {embeddingProvider === modelProvider ? (
                  <p className="rounded-md border bg-card/70 px-3 py-2 text-xs text-muted-foreground">
                    Uses the {embeddingProviderLabel(embeddingProvider)} API key from the server-side model section.
                  </p>
                ) : (
                  <label className="grid gap-2">
                    <span className="text-xs font-medium text-muted-foreground">
                      {embeddingProviderLabel(embeddingProvider)} API key
                    </span>
                    <Input
                      type="password"
                      value={settings[embeddingApiKeyName]}
                      placeholder="Leave *** to keep existing secret"
                      disabled={loading}
                      onChange={(event) =>
                        setSettings((current) => ({ ...current, [embeddingApiKeyName]: event.target.value }))
                      }
                    />
                  </label>
                )}
                <div className="grid gap-2">
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-xs font-medium text-muted-foreground">Model</span>
                    {embeddingUsesOpenRouter ? (
                      <span className="text-xs text-muted-foreground">
                        {embeddingCatalogLoading
                          ? "Loading OpenRouter catalog"
                          : embeddingCatalogError
                            ? "Using seeded suggestions"
                            : "OpenRouter catalog"}
                      </span>
                    ) : null}
                  </div>
                  <Input
                    value={embeddingQuery}
                    placeholder={embeddingUsesOpenRouter ? "Search embedding models or type vendor/model" : "Search embedding models"}
                    disabled={loading}
                    onChange={(event) => setEmbeddingQuery(event.target.value)}
                  />
                  <div className="grid overflow-hidden rounded-lg border">
                    {canUseCustomEmbeddingSlug ? (
                      <button
                        type="button"
                        className="flex items-center justify-between gap-3 border-b px-3 py-2 text-left text-sm hover:bg-muted/60"
                        onClick={() => {
                          setSettings((current) => ({ ...current, "embedding.model": customEmbeddingSlug }))
                          setEmbeddingQuery("")
                        }}
                      >
                        <span>Use {customEmbeddingSlug}</span>
                        <span className="font-mono text-xs text-muted-foreground">custom</span>
                      </button>
                    ) : null}
                    {filteredEmbeddingModelOptions.map((model) => (
                      <button
                        key={model.id}
                        type="button"
                        className={cn(
                          "flex items-center justify-between gap-3 border-b px-3 py-2 text-left text-sm last:border-b-0 hover:bg-muted/60",
                          settings["embedding.model"] === model.id && "bg-muted/45",
                        )}
                        onClick={() => {
                          setSettings((current) => ({ ...current, "embedding.model": model.id }))
                          setEmbeddingQuery("")
                        }}
                      >
                        <span className="min-w-0">
                          <span className="block truncate font-medium">{model.name}</span>
                          <span className="block truncate font-mono text-xs text-muted-foreground">{model.id}</span>
                        </span>
                        {settings["embedding.model"] === model.id ? (
                          <Badge variant="outline" className={cn("shrink-0 border", toneClass("success"))}>
                            Selected
                          </Badge>
                        ) : null}
                      </button>
                    ))}
                    {filteredEmbeddingModelOptions.length === 0 && !canUseCustomEmbeddingSlug ? (
                      <div className="px-3 py-4 text-sm text-muted-foreground">No model found.</div>
                    ) : null}
                  </div>
                  {embeddingCatalogError ? <p className="text-xs text-muted-foreground">{embeddingCatalogError}</p> : null}
                </div>
              </>
            ) : null}
          </div>
        </div>
      </div>
    </form>
  )
}

function sameModelSettings(left: ModelSettingsData, right: ModelSettingsData): boolean {
  return (Object.keys(defaultModelSettings) as Array<keyof ModelSettingsData>).every((key) => left[key] === right[key])
}

function IntegrationBrandIcon({ provider }: { provider: string }) {
  const className = "size-5"
  if (provider === "github") return <GitHubIcon className={className} />
  if (provider === "gitlab") return <GitLabIcon className={className} />
  if (provider === "linear") return <LinearIcon className={className} />
  return <PlugIcon className={className} />
}

function IntegrationsSettingsPanel() {
  const base = useMemo(() => integrationsBase(), [])
  const [items, setItems] = useState<IntegrationSummary[] | null>(null)
  const [resourcesByIntegration, setResourcesByIntegration] = useState<Record<string, IntegrationResourceState>>({})
  const [webhooksByIntegration, setWebhooksByIntegration] = useState<Record<string, IntegrationWebhookState>>({})
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(Boolean(base))
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [linkingIntegration, setLinkingIntegration] = useState<IntegrationSummary | null>(null)
  const displayedItems = items ?? []

  // Integration details are lazy: the list view fetches only /integrations,
  // and per-integration resources/webhook events load on first expansion.
  const reloadIntegrations = useCallback(() => {
    if (!base) return
    setLoading(true)
    setError(null)
    setResourcesByIntegration({})
    setWebhooksByIntegration({})
    setExpandedIds(new Set())
    listIntegrations(base)
      .then(setItems)
      .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Failed to load integrations"))
      .finally(() => setLoading(false))
  }, [base])

  useEffect(() => {
    reloadIntegrations()
  }, [reloadIntegrations])

  const loadWebhookEvents = useCallback(
    (integrationId: string) => {
      if (!base) return
      void listIntegrationWebhookEvents(base, integrationId, 3)
        .then((events) => setWebhooksByIntegration((current) => ({ ...current, [integrationId]: { items: events } })))
        .catch((reason: unknown) => {
          const message = reason instanceof Error ? reason.message : "Failed to load webhook events"
          setWebhooksByIntegration((current) => ({ ...current, [integrationId]: { items: [], error: message } }))
        })
    },
    [base],
  )

  const loadIntegrationDetails = useCallback(
    (integrationId: string) => {
      if (!base) return
      void listIntegrationResources(base, integrationId)
        .then((resources) => setResourcesByIntegration((current) => ({ ...current, [integrationId]: { items: resources } })))
        .catch((reason: unknown) => {
          const message = reason instanceof Error ? reason.message : "Failed to load resources"
          setResourcesByIntegration((current) => ({ ...current, [integrationId]: { items: [], error: message } }))
        })
      loadWebhookEvents(integrationId)
    },
    [base, loadWebhookEvents],
  )

  function toggleIntegrationDetails(integrationId: string) {
    const expanding = !expandedIds.has(integrationId)
    setExpandedIds((current) => {
      const next = new Set(current)
      if (next.has(integrationId)) next.delete(integrationId)
      else next.add(integrationId)
      return next
    })
    if (expanding && !resourcesByIntegration[integrationId] && !webhooksByIntegration[integrationId]) {
      loadIntegrationDetails(integrationId)
    }
  }

  function handleCreated(integration: IntegrationSummary) {
    setItems((current) => [integration, ...(current ?? [])])
    setResourcesByIntegration((current) => ({ ...current, [integration.id]: { items: [] } }))
    setWebhooksByIntegration((current) => ({ ...current, [integration.id]: { items: [] } }))
    setExpandedIds((current) => new Set(current).add(integration.id))
    setAddOpen(false)
  }

  function handleResourceLinked(integrationId: string, resource: IntegrationResourceSummary) {
    setResourcesByIntegration((current) => {
      const existing = current[integrationId]?.items ?? []
      return {
        ...current,
        [integrationId]: { items: [resource, ...existing.filter((item) => item.id !== resource.id)] },
      }
    })
    if (!webhooksByIntegration[integrationId]) loadWebhookEvents(integrationId)
    setExpandedIds((current) => new Set(current).add(integrationId))
    setLinkingIntegration(null)
  }

  return (
    <section className="grid gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-sm font-semibold">Integrations</h2>
            <Badge variant="outline" className="border">
              Experimental
            </Badge>
          </div>
          <p className="mt-2 text-sm text-muted-foreground">
            Repository and tracker adapters feed evidence, links, and work item mirrors.
          </p>
        </div>
        <ActionTooltip content={!base ? "Set VITE_DOC_REGISTRY_URL before adding integrations." : "Connect GitHub, GitLab, or Linear for delivery signals."}>
          <span className="inline-flex">
            <Button type="button" size="sm" className="rounded-md" disabled={!base} onClick={() => setAddOpen(true)}>
              <PlusIcon data-icon="inline-start" />
              Add integration
            </Button>
          </span>
        </ActionTooltip>
      </div>
      {!base ? (
        <div className="rounded-lg border border-dashed bg-card/60 p-3 text-sm text-muted-foreground">
          Set <code>VITE_DOC_REGISTRY_URL</code> to add integrations and view connected provider status.
        </div>
      ) : null}
      {error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-300">
          {error}
        </div>
      ) : null}
      <div className="grid gap-2">
        {loading ? <div className="rounded-lg border bg-background/70 p-3 text-sm text-muted-foreground">Loading integrations...</div> : null}
        {!loading && displayedItems.length === 0 ? (
          <div className="rounded-lg border border-dashed bg-card/60 p-3 text-sm text-muted-foreground">
            No integrations connected yet. Add GitHub, GitLab, or Linear when you need provider signals.
          </div>
        ) : null}
        {!loading ? displayedItems.map((integration) => (
          <div
            key={integration.id}
            className="grid gap-3 rounded-lg border bg-background/70 p-3 md:grid-cols-[minmax(0,1fr)_auto]"
          >
            <div className="flex min-w-0 items-center gap-3">
              <div className="flex size-9 items-center justify-center rounded-md border bg-card">
                <IntegrationBrandIcon provider={integrationProvider(integration)} />
              </div>
              <div className="min-w-0">
                <h3 className="text-sm font-medium">{integrationName(integration)}</h3>
                <p className="mt-1 text-xs text-muted-foreground">{integrationScope(integration)}</p>
              </div>
            </div>
            <div className="flex flex-wrap items-center justify-start gap-2 md:justify-end">
              <Badge
                variant="outline"
                className={cn("border", toneClass(statusTone("integration", integration.status)))}
              >
                {integration.status}
              </Badge>
              {base ? (
                <>
                  <ActionTooltip content={canLinkIntegrationResource(integration) ? "Link a repository or tracker team through Doc Registry." : "Store an API token or finish OAuth before linking resources."}>
                    <span className="inline-flex">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="rounded-md"
                        disabled={!canLinkIntegrationResource(integration)}
                        onClick={() => setLinkingIntegration(integration)}
                      >
                        <PlusIcon data-icon="inline-start" />
                        Link resource
                      </Button>
                    </span>
                  </ActionTooltip>
                  <ActionTooltip content="Load linked resources and recent webhook deliveries for this integration.">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="rounded-md"
                      aria-expanded={expandedIds.has(integration.id)}
                      onClick={() => toggleIntegrationDetails(integration.id)}
                    >
                      {expandedIds.has(integration.id) ? "Hide details" : "Show details"}
                    </Button>
                  </ActionTooltip>
                </>
              ) : null}
            </div>
            <p className="border-t pt-2 text-xs leading-5 text-muted-foreground md:col-span-2">
              {integrationDetail(integration, Boolean(base))}
            </p>
            {base && expandedIds.has(integration.id) ? (
              <div className="grid gap-3 md:col-span-2">
                <IntegrationResourcesSummary state={resourcesByIntegration[integration.id]} />
                <IntegrationWebhookEventsSummary state={webhooksByIntegration[integration.id]} />
              </div>
            ) : null}
          </div>
        )) : null}
      </div>
      <AddIntegrationDialog open={addOpen} onOpenChange={setAddOpen} base={base} onCreated={handleCreated} />
      <LinkIntegrationResourceDialog
        integration={linkingIntegration}
        base={base}
        onOpenChange={(open) => {
          if (!open) setLinkingIntegration(null)
        }}
        onLinked={handleResourceLinked}
      />
    </section>
  )
}

type IntegrationResourceState = {
  items: IntegrationResourceSummary[]
  error?: string
}

type IntegrationWebhookState = {
  items: IntegrationWebhookEventSummary[]
  error?: string
}

function IntegrationResourcesSummary({ className, state }: { className?: string; state?: IntegrationResourceState }) {
  if (!state) {
    return (
      <div className={cn("border-t pt-2 text-xs text-muted-foreground", className)}>
        Loading linked resources...
      </div>
    )
  }
  if (state.error) {
    return (
      <div className={cn("border-t pt-2 text-xs text-amber-700 dark:text-amber-300", className)}>
        Resource list unavailable: {state.error}
      </div>
    )
  }
  if (state.items.length === 0) {
    return (
      <div className={cn("border-t pt-2 text-xs text-muted-foreground", className)}>
        No linked resources yet. Use Link resource to register a repository or team; webhook management stays in backend/admin flows.
      </div>
    )
  }

  return (
    <div className={cn("grid gap-2 border-t pt-2", className)}>
      <div className="text-xs font-medium text-muted-foreground">Linked resources</div>
      <div className="grid gap-2">
        {state.items.map((resource) => {
          const webhook = integrationResourceWebhook(resource)
          return (
            <div key={resource.id} className="grid gap-2 rounded-md border bg-card/50 p-2 sm:grid-cols-[minmax(0,1fr)_auto]">
              <div className="min-w-0">
                <div className="truncate text-xs font-medium">{integrationResourceName(resource)}</div>
                <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  <span>{resource.resource_type || "resource"}</span>
                  {resource.default_ref ? <span>Ref {resource.default_ref}</span> : null}
                  {resource.has_webhook_secret ? <span>Webhook secret stored</span> : null}
                </div>
              </div>
              <Badge variant="outline" className={cn("w-fit border", toneClass(webhook.tone))}>
                {webhook.label}
              </Badge>
              {webhook.detail ? (
                <p className="text-xs text-muted-foreground sm:col-span-2">{webhook.detail}</p>
              ) : null}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function IntegrationWebhookEventsSummary({ state }: { state?: IntegrationWebhookState }) {
  if (!state) {
    return <div className="border-t pt-2 text-xs text-muted-foreground">Loading webhook events...</div>
  }
  if (state.error) {
    return <div className="border-t pt-2 text-xs text-amber-700 dark:text-amber-300">Webhook events unavailable: {state.error}</div>
  }
  if (state.items.length === 0) {
    return <div className="border-t pt-2 text-xs text-muted-foreground">No webhook deliveries recorded yet.</div>
  }

  return (
    <div className="grid gap-2 border-t pt-2">
      <div className="text-xs font-medium text-muted-foreground">Recent webhooks</div>
      <div className="grid gap-2">
        {state.items.map((event) => (
          <div key={event.id} className="grid gap-2 rounded-md border bg-card/50 p-2 sm:grid-cols-[minmax(0,1fr)_auto]">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-xs font-medium">{webhookEventLabel(event.event_type || "webhook")}</span>
                {event.correlation_id ? (
                  <span className="font-mono text-[0.68rem] text-muted-foreground">{event.correlation_id}</span>
                ) : null}
              </div>
              <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                {event.received_at ? <span>{formatDateTime(event.received_at)}</span> : null}
                {event.resource_id ? <span>Resource {event.resource_id}</span> : null}
              </div>
            </div>
            <Badge variant="outline" className={cn("w-fit border", toneClass(statusTone("webhookEvent", event.status)))}>
              {event.status || "recorded"}
            </Badge>
            {event.error ? <p className="text-xs text-muted-foreground sm:col-span-2">{event.error}</p> : null}
          </div>
        ))}
      </div>
    </div>
  )
}

function webhookEventLabel(eventType: string): string {
  const words = eventType.split(/[_-]+/).filter(Boolean)
  if (words.length === 0) return "Webhook"
  return [words[0][0]?.toUpperCase() + words[0].slice(1), ...words.slice(1)].join(" ")
}

function integrationResourceName(resource: IntegrationResourceSummary): string {
  return resource.display_name || resource.external_key || resource.external_id || resource.id
}

function integrationResourceWebhook(resource: IntegrationResourceSummary): { label: string; detail?: string; tone: "neutral" | "success" | "warning" | "danger" } {
  const config = parseJsonObject(resource.config_json)
  const status = stringFromRecord(config, "webhook_status")
  const lastError = stringFromRecord(config, "webhook_last_error")
  const providerWebhookID = stringFromRecord(config, "provider_webhook_id")

  if (status === "connected") {
    return {
      label: "Webhook connected",
      detail: providerWebhookID ? `Provider hook ${providerWebhookID}` : undefined,
      tone: "success",
    }
  }
  if (status === "error") {
    return {
      label: "Webhook error",
      detail: lastError || undefined,
      tone: "danger",
    }
  }
  if (status) {
    return {
      label: `Webhook ${status}`,
      detail: lastError || undefined,
      tone: "warning",
    }
  }
  if (resource.has_webhook_secret) {
    return { label: "Webhook secret stored", tone: "warning" }
  }
  return { label: "No webhook", tone: "neutral" }
}

function parseJsonObject(value?: string): Record<string, unknown> | null {
  if (!value) return null
  try {
    const parsed = JSON.parse(value) as unknown
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : null
  } catch {
    return null
  }
}

function stringFromRecord(record: Record<string, unknown> | null, key: string): string {
  const value = record?.[key]
  return typeof value === "string" ? value.trim() : ""
}

function integrationScope(integration: IntegrationSummary): string {
  return providerDefinition(integration.provider).scope
}

function integrationProvider(integration: IntegrationSummary): string {
  return integration.provider
}

function integrationName(integration: IntegrationSummary): string {
  return integration.name
}

function integrationDetail(integration: IntegrationSummary, live: boolean): string {
  const auth =
    integration.auth_method === "oauth" && integration.has_oauth_token
      ? "OAuth connected"
      : integration.has_api_token
        ? "API token stored"
        : "No outbound token recorded"
  if (integration.last_error) return `${auth}. Last error: ${integration.last_error}`
  return live ? `${auth}. Link resources here; reprovisioning, disconnect, and webhook-secret management stay with backend/admin flows.` : auth
}

function canLinkIntegrationResource(integration: IntegrationSummary): boolean {
  return Boolean(integration.has_api_token || integration.has_oauth_token)
}

function integrationResourceType(provider: string): string {
  return provider === "linear" ? "team" : "project"
}

function LinkIntegrationResourceDialog({
  integration,
  base,
  onOpenChange,
  onLinked,
}: {
  integration: IntegrationSummary | null
  base: string | null
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
    if (!base || !integration) return
    setLoading(true)
    setError(null)
    const loader =
      provider === "linear"
        ? listLinearTeams(base, integration.id)
        : listIntegrationRepos(base, integration.id, search, 50)
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
  }, [base, integration, provider])

  useEffect(() => {
    if (!open) {
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
    if (!base || !integration || !selected) return
    setSaving(true)
    setError(null)
    try {
      const linked = await createIntegrationResource(base, integration.id, {
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
          <div className="overflow-hidden rounded-lg border bg-background/70">
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

function AddIntegrationDialog({
  open,
  onOpenChange,
  base,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  base: string | null
  onCreated: (integration: IntegrationSummary) => void
}) {
  const [provider, setProvider] = useState<IntegrationProvider>("github")
  const [name, setName] = useState(providerDefinition("github").defaultName)
  const [baseUrl, setBaseUrl] = useState(providerDefinition("github").defaultBaseUrl ?? "")
  const [authMethod, setAuthMethod] = useState<"pat" | "oauth">("pat")
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
    setName(next.defaultName)
    setBaseUrl(next.defaultBaseUrl ?? "")
    setAuthMethod("pat")
    setSelfHosted(false)
    setApiToken("")
    setError(null)
  }, [open, provider])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!base || !ready) return
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
        const result = await beginPendingIntegrationOAuth(base, {
          ...payload,
          redirect_target: oauthRedirectTarget(),
        })
        window.location.assign(result.authorize_url)
        return
      }
      const created = await createIntegration(base, payload)
      await setIntegrationApiToken(base, created.id, apiToken.trim())
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
                    "grid gap-2 rounded-lg border bg-background/70 p-3 text-left transition-colors hover:bg-muted/40",
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
                    "rounded-lg border bg-background/70 px-3 py-2 text-left transition-colors hover:bg-muted/40",
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
            <label className="flex items-start gap-3 rounded-md border bg-card/70 px-3 py-2">
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
            <p className="rounded-md border bg-card/70 px-3 py-2 text-xs leading-5 text-muted-foreground">
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
    id: "governance",
    label: "Governance",
    description: "Skills, policy, and profiles",
    icon: ShieldCheckIcon,
  },
  {
    id: "plugins",
    label: "Plugins",
    description: "CLI-managed IDE plugins",
    icon: PlugIcon,
  },
  {
    id: "integrations",
    label: "Integrations (Experimental)",
    description: "Tracker and repository",
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
  onGeneralSaveStatusChange,
  onModelSaveStatusChange,
}: {
  activeSection: SettingsSectionId
  onGeneralSaveStatusChange: (status: SettingsSaveStatus) => void
  onModelSaveStatusChange: (status: SettingsSaveStatus) => void
}) {
  if (activeSection === "general") return <GeneralSettingsPanel onSaveStatusChange={onGeneralSaveStatusChange} />
  if (activeSection === "models") return <ModelSettingsPanel onSaveStatusChange={onModelSaveStatusChange} />
  if (activeSection === "governance") return <GovernanceSettingsPanel />
  if (activeSection === "integrations") return <IntegrationsSettingsPanel />
  return <PluginsSettingsPanel />
}

export function SettingsModal({
  open,
  onOpenChange,
  activeSection,
  onActiveSectionChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  // Accepted for app-shell compatibility; no settings panel reads it anymore.
  profile?: WorkspaceProfile
  activeSection: SettingsSectionId
  onActiveSectionChange: (section: SettingsSectionId) => void
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
