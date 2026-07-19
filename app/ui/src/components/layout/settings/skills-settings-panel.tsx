// Settings panel extracted from settings.tsx. See app/ui/AGENTS.md.

import { PlusIcon } from "lucide-react"
import { useCallback, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { docRegistryBase } from "@/data/model-settings"
import { createRegistrySkill, deleteRegistrySkill, getRegistrySkill, listRegistrySkills, updateRegistrySkill, type RegistrySkillDetail, type RegistrySkillInput, type RegistrySkillSummary } from "@/data/skills"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { MarkdownText } from "../shared-ui"

export function TeamSkillsPanel({ workspaceId }: { workspaceId?: string }) {
  const [registrySkills, setRegistrySkills] = useState<RegistrySkillSummary[]>([])
  const [registrySkillsStatus, setRegistrySkillsStatus] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [selectedSkill, setSelectedSkill] = useState<RegistrySkillSummary | null>(null)
  const [skillForm, setSkillForm] = useState<{ mode: "create" } | null>(null)
  const [skillToDelete, setSkillToDelete] = useState<RegistrySkillSummary | null>(null)
  const [skillDetails, setSkillDetails] = useState<Record<string, RegistrySkillDetail>>({})
  const [deleteStatus, setDeleteStatus] = useState<"idle" | "deleting" | "error">("idle")
  const registryBase = docRegistryBase()

  const loadSkills = useCallback((signal?: AbortSignal) => {
    const selectedWorkspace = workspaceId?.trim()
    if (!registryBase || !selectedWorkspace) {
      setRegistrySkillsStatus("idle")
      return
    }
    setRegistrySkillsStatus("loading")
    void listRegistrySkills(registryBase, selectedWorkspace, signal)
      .then((skills) => {
        setRegistrySkills(skills)
        setRegistrySkillsStatus("ready")
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setRegistrySkills([])
        setRegistrySkillsStatus("error")
      })
  }, [registryBase, workspaceId])

  useEffect(() => {
    if (!registryBase) {
      setRegistrySkills([])
      setRegistrySkillsStatus("idle")
      return
    }

    const controller = new AbortController()
    setRegistrySkills([])
    setSkillDetails({})
    setSelectedSkill(null)
    setSkillForm(null)
    setSkillToDelete(null)
    loadSkills(controller.signal)

    return () => controller.abort()
  }, [loadSkills, registryBase, workspaceId])

  async function saveSkill(input: RegistrySkillInput, skillId?: string) {
    const selectedWorkspace = workspaceId?.trim()
    if (!registryBase || !selectedWorkspace) throw new Error("Select a workspace before editing Skills")
    const saved = skillId
      ? await updateRegistrySkill(registryBase, skillId, selectedWorkspace, input)
      : await createRegistrySkill(registryBase, selectedWorkspace, input)
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
      const selectedWorkspace = workspaceId?.trim()
      if (!selectedWorkspace) throw new Error("Select a workspace before deleting Skills")
      await deleteRegistrySkill(registryBase, skillToDelete.id, selectedWorkspace)
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
              Team rubric Skills are governance rubrics that can appear in gates and Context Packs. IDE plugin installation stays CLI-managed.
            </p>
            <p className="mt-1 text-xs break-words text-muted-foreground">
              Edits affect later resolutions and lookups; existing snapshots retain what they recorded.
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
        ) : !workspaceId?.trim() ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">Select a workspace to view team rubric Skills.</p>
        ) : registrySkillsStatus === "loading" ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">Loading team rubric Skills...</p>
        ) : registrySkillsStatus === "error" ? (
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border bg-background/70 p-3">
            <p className="text-sm text-muted-foreground">Team rubric Skills unavailable.</p>
            <Button type="button" variant="outline" size="sm" className="rounded-md" onClick={() => loadSkills()}>
              Retry Skills
            </Button>
          </div>
        ) : registrySkills.length === 0 ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">No team rubric Skills found.</p>
        ) : (
          <div className="grid min-w-0 gap-2">
            {registrySkills.map((skill) => (
              <div key={skill.id} className="min-w-0 overflow-hidden rounded-md border bg-background/70 p-3">
                <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="min-w-0 truncate font-mono text-xs font-medium text-foreground">{skill.name}</span>
                  </div>
                  <div className="flex items-center gap-2">
                    {skill.updatedAt ? (
                      <span className="text-xs text-muted-foreground" title={formatDateTime(skill.updatedAt)}>
                        {formatRelativeTime(skill.updatedAt)}
                      </span>
                    ) : null}
                    <Button type="button" variant="outline" size="sm" className="rounded-md" onClick={() => setSelectedSkill(skill)}>
                      <span className="sr-only">Manage {skill.name} Skill</span>
                      <span aria-hidden="true">Manage</span>
                    </Button>
                  </div>
                </div>
                <p className="mt-2 text-sm leading-5 break-words text-muted-foreground">{skill.description}</p>
              </div>
            ))}
          </div>
        )}
      </div>
      <RegistrySkillDetailDialog
        baseUrl={registryBase}
        workspaceId={workspaceId}
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
              Delete this team rubric from Doc Registry. Existing gate snapshots keep their recorded Skill names, but later lookups will not resolve this prompt.
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
  workspaceId,
  skill,
  overrideDetail,
  open,
  onLoaded,
  onSave,
  onDelete,
  onOpenChange,
}: {
  baseUrl: string | null
  workspaceId?: string
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

    const selectedWorkspace = workspaceId?.trim()
    if (!selectedWorkspace) {
      setDetail(null)
      setStatus("idle")
      return
    }
    const controller = new AbortController()
    setDetail(null)
    setStatus("loading")
    void getRegistrySkill(baseUrl, skill.id, selectedWorkspace, controller.signal)
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
  }, [baseUrl, onLoaded, open, overrideDetail, skill, workspaceId])

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
