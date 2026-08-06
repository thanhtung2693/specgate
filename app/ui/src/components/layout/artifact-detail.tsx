// Artifacts domain: artifact document list, detail dialog, preview, and the
// Artifacts page. Extracted from app-shell.
import { CheckIcon, ClockIcon, ExternalLinkIcon, MessageSquareTextIcon, PaperclipIcon, ShieldCheckIcon, XIcon } from "lucide-react"
import { useEffect, useMemo, useState, type ReactNode } from "react"
import { Link } from "react-router"

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
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  gatePreviewNotRunNote,
  updateArtifactStatus,
  useArtifactAttachments,
  useArtifactEvents,
  useArtifactFiles,
  useArtifactFeature,
  useArtifactFeedback,
  useArtifactGatePreview,
  useArtifactPolicy,
  useArtifactReadinessRuns,
  useArtifactVersions,
  type ArtifactAttachmentSummary,
  type ArtifactDocumentSummary,
  type ArtifactEventSummary,
  type ArtifactFeatureSummary,
  type ArtifactFeedbackSummary,
  type ArtifactGatePreviewView,
  type ArtifactPolicyView,
  type ArtifactReadinessRunSummary,
  type ArtifactSummary,
} from "@/data/artifacts"
import { formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { gateChecks, gateText, readableKey, stateText, statusTone, toneClass } from "./shared"
import { ActionTooltip, GateEvidenceWhy, PolicyExplanationSection } from "./shared-ui"
import { ArtifactDocumentList, ArtifactDocumentPreview } from "./artifacts/document-viewer"

export function artifactStatusText(status: string) {
  return {
    approved: "Approved",
    needs_changes: "Needs changes",
    draft: "Draft",
  }[status] ?? readableKey(status)
}

function statusNote(status: "ready" | "loading" | "error", loading: string, error: string) {
  if (status === "ready") {
    return null
  }
  return <p className="text-sm text-muted-foreground">{status === "loading" ? loading : error}</p>
}

function ArtifactCompactSection({
  title,
  count,
  action,
  children,
}: {
  title: string
  count?: number
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="border-t pt-3">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-semibold">{title}</h3>
        {count !== undefined ? (
          <Badge variant="outline" className="h-5 px-1.5 font-mono text-[10px]">
            {count}
          </Badge>
        ) : null}
        {action ? <div className="ml-auto">{action}</div> : null}
      </div>
      {children}
    </section>
  )
}

function ArtifactAttachmentsSection({
  attachments,
  status,
}: {
  attachments: ArtifactAttachmentSummary[]
  status: "ready" | "loading" | "error"
}) {
  const note = statusNote(status, "Loading attachments...", "Reference attachments unavailable. Check Doc Registry connectivity; no fallback attachments are shown in live mode.")
  if (note) {
    return note
  }

  if (attachments.length === 0) {
    return <p className="text-sm text-muted-foreground">No reference attachments pinned to this feature.</p>
  }

  return (
    <div className="grid gap-1">
      {attachments.slice(0, 4).map((attachment) => (
        <div key={attachment.id} className="grid min-h-9 grid-cols-[minmax(0,1fr)_6rem_7rem] items-center gap-2 rounded-md px-2 text-xs hover:bg-muted/35">
          <span className="flex min-w-0 items-center gap-2">
            <PaperclipIcon className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0">
              {attachment.url ? (
                <a className="block truncate text-foreground underline-offset-4 hover:underline" href={attachment.url} target="_blank" rel="noreferrer">
                  {attachment.title}
                </a>
              ) : (
                <span className="block truncate text-foreground">{attachment.title}</span>
              )}
              {attachment.note ? <span className="block truncate text-muted-foreground">{attachment.note}</span> : null}
            </span>
          </span>
          <span className="truncate text-muted-foreground">{readableKey(attachment.kind)}</span>
          <span className="truncate text-right text-muted-foreground">{readableKey(attachment.audience)}</span>
        </div>
      ))}
      {attachments.length > 4 ? (
        <p className="px-2 text-xs text-muted-foreground">Showing latest 4 of {attachments.length} attachments.</p>
      ) : null}
    </div>
  )
}

function ArtifactFeedbackSection({
  feedback,
  status,
}: {
  feedback: ArtifactFeedbackSummary[]
  status: "ready" | "loading" | "error"
}) {
  const note = statusNote(status, "Loading feedback...", "Artifact feedback unavailable. Check Doc Registry connectivity; no fallback feedback is shown in live mode.")
  if (note) {
    return note
  }

  if (feedback.length === 0) {
    return <p className="text-sm text-muted-foreground">No artifact-linked feedback recorded.</p>
  }

  return (
    <div className="grid gap-1">
      {feedback.slice(0, 4).map((event) => (
        <div key={event.id} className="grid min-h-9 grid-cols-[minmax(0,1fr)_6.5rem_8rem] items-center gap-2 rounded-md px-2 text-xs hover:bg-muted/35">
          <span className="flex min-w-0 items-center gap-2">
            <MessageSquareTextIcon className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0">
              <span className="block truncate text-foreground">{readableKey(event.type)}</span>
              <span className="block truncate text-muted-foreground">{event.reason}</span>
            </span>
          </span>
          <Badge variant="outline" className="h-5 justify-self-start px-1.5 text-[10px]">
            {readableKey(event.status)}
          </Badge>
          <span className="truncate text-right text-muted-foreground">{formatDateTime(event.createdAt)}</span>
        </div>
      ))}
      {feedback.length > 4 ? (
        <p className="px-2 text-xs text-muted-foreground">Showing latest 4 of {feedback.length} feedback events.</p>
      ) : null}
    </div>
  )
}

function ArtifactReadinessRunsSection({
  runs,
  status,
}: {
  runs: ArtifactReadinessRunSummary[]
  status: "ready" | "loading" | "error"
}) {
  const note = statusNote(status, "Loading readiness history...", "Readiness history unavailable. Check Doc Registry connectivity; no fallback readiness runs are shown in live mode.")
  if (note) {
    return note
  }

  if (runs.length === 0) {
    return <p className="text-sm text-muted-foreground">No persisted readiness runs recorded.</p>
  }

  return (
    <div className="grid gap-1">
      <p className="px-2 text-xs text-muted-foreground">Each run records what the checker read and why it decided.</p>
      {runs.slice(0, 5).map((run) => (
        <div key={run.id} className="rounded-md px-2 pb-1 hover:bg-muted/35">
          <div className="grid min-h-9 grid-cols-[minmax(0,1fr)_7rem_8rem] items-center gap-2 text-xs">
            <span className="flex min-w-0 items-center gap-2">
              <ShieldCheckIcon className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="min-w-0">
                <span className="block truncate text-foreground">{gateText(run.gate)}</span>
                <span className="block truncate text-muted-foreground">{run.hint}</span>
              </span>
            </span>
            <Badge variant="outline" className={cn("h-5 justify-self-start border px-1.5 text-[10px]", toneClass(statusTone("state",run.state)))}>
              {stateText(run.state)}
            </Badge>
            <span className="truncate text-right text-muted-foreground">{formatDateTime(run.createdAt)}</span>
          </div>
          <GateEvidenceWhy evidence={run.evidence} executor={run.executor} />
        </div>
      ))}
      {runs.length > 5 ? (
        <p className="px-2 text-xs text-muted-foreground">Showing latest 5 of {runs.length} readiness runs.</p>
      ) : null}
    </div>
  )
}

function ArtifactEventsSection({
  events,
  status,
}: {
  events: ArtifactEventSummary[]
  status: "ready" | "loading" | "error"
}) {
  const note = statusNote(status, "Loading audit events...", "Audit events unavailable. Check Doc Registry connectivity; no fallback audit events are shown in live mode.")
  if (note) {
    return note
  }

  if (events.length === 0) {
    return <p className="text-sm text-muted-foreground">No artifact audit events recorded.</p>
  }

  return (
    <div className="grid gap-1">
      {events.slice(0, 5).map((event) => (
        <div key={event.id} className="grid min-h-9 grid-cols-[minmax(0,1fr)_8rem] items-center gap-2 rounded-md px-2 text-xs hover:bg-muted/35">
          <span className="flex min-w-0 items-center gap-2">
            <ClockIcon className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0">
              <span className="block truncate text-foreground">{readableKey(event.type)}</span>
              <span className="block truncate text-muted-foreground">{event.payloadSummary}</span>
            </span>
          </span>
          <span className="truncate text-right text-muted-foreground">{formatDateTime(event.createdAt)}</span>
        </div>
      ))}
      {events.length > 5 ? (
        <p className="px-2 text-xs text-muted-foreground">Showing latest 5 of {events.length} audit events.</p>
      ) : null}
    </div>
  )
}

function ArtifactFeatureContext({
  feature,
  status,
}: {
  feature: ArtifactFeatureSummary | undefined
  status: "ready" | "loading" | "error"
}) {
  if (status === "loading") {
    return (
      <section className="sg-card p-4">
        <h3 className="text-sm font-semibold">Feature context</h3>
        <p className="mt-2 text-sm text-muted-foreground">Loading linked feature...</p>
      </section>
    )
  }

  if (status === "error") {
    return (
      <section className="sg-card p-4">
        <h3 className="text-sm font-semibold">Feature context</h3>
        <p className="mt-2 text-sm text-muted-foreground">Feature context unavailable. Check Doc Registry connectivity; no fallback feature summary is shown in live mode.</p>
      </section>
    )
  }

  if (!feature) return null

  return (
    <section className="sg-card p-4">
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
            <span className="font-mono">{feature.canonicalArtifactId}</span>
          ) : null}
        </div>
      </div>
      <p className="mt-3 text-sm text-muted-foreground">Feature identity and canonical artifact recorded by Doc Registry.</p>
    </section>
  )
}

function ArtifactPolicySection({ policy }: { policy: ArtifactPolicyView }) {
  if (policy.status === "error") {
    return <p className="text-sm text-muted-foreground">Policy snapshot unavailable. Check Doc Registry connectivity; no fallback policy explanation is shown in live mode.</p>
  }

  if (policy.status === "ready" && !policy.item) {
    return <p className="text-sm text-muted-foreground">No policy explanation recorded for this artifact.</p>
  }

  return <PolicyExplanationSection policy={policy.item} status={policy.status} context="artifact" />
}

function ArtifactGatePreviewSection({
  preview,
  readinessRuns = [],
}: {
  preview: ArtifactGatePreviewView
  readinessRuns: ArtifactReadinessRunSummary[]
}) {
  const latestRunByGate = useMemo(() => {
    const runsByGate = new Map<string, ArtifactReadinessRunSummary>()
    for (const run of readinessRuns) {
      if (!runsByGate.has(run.gate)) runsByGate.set(run.gate, run)
    }
    return runsByGate
  }, [readinessRuns])

  if (preview.status === "loading") {
    return <p className="mt-3 text-sm text-muted-foreground">Loading gate preview...</p>
  }

  if (preview.status === "error") {
    return (
      <p className="mt-3 text-sm text-muted-foreground">
        Gate preview unavailable. Check Doc Registry connectivity; no fallback gate snapshot is shown in live mode.
      </p>
    )
  }

  if (preview.items.length === 0) {
    return <p className="mt-3 text-sm text-muted-foreground">No gate snapshot recorded.</p>
  }

  return (
    <div className="mt-3 grid gap-2">
      {preview.items.map((gate) => {
        const latestRun = latestRunByGate.get(gate.gateKey)
        const note = latestRun && gate.note === gatePreviewNotRunNote ? "Latest persisted readiness run." : gate.note
        return (
          <div key={`${gate.gateKey}:${gate.gateVersion ?? ""}:${gate.executor ?? ""}`} className="sg-inset p-3">
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="min-w-0">
                <p className="font-medium">{gateText(gate.gateKey)}</p>
                {gateChecks(gate.gateKey) ? (
                  <p className="mt-1 text-xs text-muted-foreground">{gateChecks(gate.gateKey)}</p>
                ) : null}
                <p className="mt-1 text-xs text-muted-foreground">{note}</p>
                {latestRun ? (
                  <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <Badge variant="outline" className={cn("h-5 border px-1.5 text-[10px]", toneClass(statusTone("state",latestRun.state)))}>
                      {stateText(latestRun.state)}
                    </Badge>
                    <span className="min-w-0 truncate">{latestRun.hint}</span>
                    <span className="font-mono">{formatDateTime(latestRun.createdAt)}</span>
                  </div>
                ) : null}
              </div>
              <div className="flex flex-wrap gap-1.5">
                {gate.gateVersion ? (
                  <Badge variant="outline" className="rounded-md font-mono text-[0.68rem]">
                    {gate.gateVersion}
                  </Badge>
                ) : null}
                {gate.executor ? (
                  <Badge variant="outline" className="rounded-md font-mono text-[0.68rem]">
                    {gate.executor}
                  </Badge>
                ) : null}
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}

export function ArtifactDetailDialog({
  artifact,
  open,
  reviewer,
  workspaceId,
  mode,
  onOpenChange,
  onDecided,
}: {
  artifact: ArtifactSummary | undefined
  open: boolean
  reviewer: string
  workspaceId: string
  mode: "library" | "review"
  onOpenChange: (open: boolean) => void
  onDecided: () => void
}) {
  const files = useArtifactFiles(artifact?.id, workspaceId, artifact !== undefined)
  const attachments = useArtifactAttachments(artifact?.featureId, artifact?.id, workspaceId, artifact !== undefined)
  const feedback = useArtifactFeedback(artifact?.id, workspaceId, artifact !== undefined)
  const readinessRuns = useArtifactReadinessRuns(artifact?.id, workspaceId, artifact !== undefined)
  const events = useArtifactEvents(artifact?.id, workspaceId, artifact !== undefined)
  const feature = useArtifactFeature(artifact, workspaceId, artifact !== undefined)
  const policy = useArtifactPolicy(artifact?.id, workspaceId, artifact !== undefined)
  const gatePreview = useArtifactGatePreview(artifact?.id, workspaceId, artifact !== undefined)
  const versions = useArtifactVersions(artifact?.featureId, artifact, workspaceId, artifact !== undefined)
  const [previewDocument, setPreviewDocument] = useState<ArtifactDocumentSummary | undefined>(undefined)
  const [pendingDecision, setPendingDecision] = useState<"approved" | "needs_changes" | undefined>(undefined)
  const [decisionNote, setDecisionNote] = useState("")
  const [decisionBusy, setDecisionBusy] = useState(false)
  const [decisionError, setDecisionError] = useState("")
  const decidable = mode === "review" && (artifact?.status === "draft" || artifact?.status === "needs_changes")
  const reviewable = artifact?.status === "draft" || artifact?.status === "needs_changes"

  async function confirmDecision() {
    if (!artifact || !pendingDecision) return
    setDecisionBusy(true)
    setDecisionError("")
    try {
      await updateArtifactStatus(artifact.id, pendingDecision, { approvedBy: reviewer, note: decisionNote.trim() || undefined }, workspaceId)
      setPendingDecision(undefined)
      setDecisionNote("")
      onDecided()
      onOpenChange(false)
    } catch {
      setDecisionError("Could not record the decision. Check Doc Registry connectivity, then retry.")
    } finally {
      setDecisionBusy(false)
    }
  }

  useEffect(() => {
    if (previewDocument && !files.items.some((document) => document.path === previewDocument.path)) {
      setPreviewDocument(undefined)
    }
  }, [files.items, previewDocument])

  useEffect(() => {
    if (!open) setPreviewDocument(undefined)
  }, [open])

  if (!artifact) {
    return null
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="grid h-[min(840px,calc(100svh-2rem))] grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-4xl">
        <DialogHeader className="border-b p-4 pr-12">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="flex flex-wrap gap-2">
                <Badge variant="secondary" className="font-mono">{artifact.version}</Badge>
                <Badge variant="outline" className={cn("border", toneClass(statusTone("artifact",artifact.status)))}>
                  {artifactStatusText(artifact.status)}
                </Badge>
              </div>
              <DialogTitle className="mt-3 text-lg">{artifact.featureName}</DialogTitle>
              <DialogDescription className="truncate font-mono text-xs">{artifact.id}</DialogDescription>
            </div>
            <div className="flex flex-wrap gap-2">
              {decidable ? (
                <>
                  <ActionTooltip content="Approve this artifact version. Durable status transition recorded in Doc Registry.">
                    <Button
                      size="sm"
                      className="rounded-md bg-success/90 text-background hover:bg-success"
                      disabled={decisionBusy}
                      onClick={() => setPendingDecision("approved")}
                    >
                      <CheckIcon data-icon="inline-start" />
                      Approve
                    </Button>
                  </ActionTooltip>
                  <ActionTooltip content="Send this artifact back for changes. Durable status transition recorded in Doc Registry.">
                    <Button
                      variant="outline"
                      size="sm"
                      className="rounded-md text-destructive hover:bg-destructive/10"
                      disabled={decisionBusy}
                      onClick={() => setPendingDecision("needs_changes")}
                    >
                      <XIcon data-icon="inline-start" />
                      Request changes
                    </Button>
                  </ActionTooltip>
                </>
              ) : null}
              {mode === "library" && reviewable ? (
                <Button asChild variant="outline" size="sm" className="rounded-md">
                  <Link to={`/reviews?artifact=${encodeURIComponent(artifact.id)}`}>
                    <ExternalLinkIcon data-icon="inline-start" />
                    Open in Reviews
                  </Link>
                </Button>
              ) : null}
            </div>
          </div>
        </DialogHeader>
        <ScrollArea className="min-h-0 overflow-hidden">
          <div className="grid min-w-0 gap-4 p-4">
            <div className="grid gap-3 text-sm sm:grid-cols-3">
              {artifact.featureId ? (
                <div>
                  <span className="text-xs text-muted-foreground">Feature</span>
                  <p className="mt-1 font-mono text-xs">{artifact.featureId}</p>
                </div>
              ) : (
                <div>
                  <span className="text-xs text-muted-foreground">Scope</span>
                  <p className="mt-1 text-xs">Standalone quick-path artifact</p>
                </div>
              )}
              <div>
                <span className="text-xs text-muted-foreground">Request type</span>
                <p className="mt-1">{readableKey(artifact.requestType)}</p>
              </div>
              <div>
                <span className="text-xs text-muted-foreground">Impact</span>
                <p className="mt-1">{readableKey(artifact.impactLevel)}</p>
              </div>
              <div>
                <span className="text-xs text-muted-foreground">Completeness</span>
                <p className="mt-1">{readableKey(artifact.completeness)}</p>
              </div>
              <div>
                <span className="text-xs text-muted-foreground">Source</span>
                <p className="mt-1">{readableKey(artifact.sourceKind)}</p>
              </div>
              <div>
                <span className="text-xs text-muted-foreground">Updated</span>
                <p className="mt-1">{formatDateTime(artifact.updatedAt)}</p>
              </div>
            </div>
            <ArtifactFeatureContext feature={feature.item} status={feature.status} />
            <ArtifactCompactSection title="Documents" count={files.items.length}>
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <p className="text-xs text-muted-foreground">Click a file to inspect Markdown, source, or version diff.</p>
              </div>
              <ArtifactDocumentList
                documents={files.items}
                status={files.status}
                fallbackUpdatedAt={artifact.updatedAt}
                selectedPath={previewDocument?.path}
                onPreview={setPreviewDocument}
              />
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Feedback" count={feedback.items.length}>
              <ArtifactFeedbackSection feedback={feedback.items} status={feedback.status} />
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Attachments" count={attachments.items.length}>
              <ArtifactAttachmentsSection attachments={attachments.items} status={attachments.status} />
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Expected gates" count={gatePreview.items.length}>
              <ArtifactGatePreviewSection preview={gatePreview} readinessRuns={readinessRuns.items} />
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Governance policy" count={policy.item ? 1 : 0}>
              <ArtifactPolicySection policy={policy} />
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Feature lineage" count={versions.items.length}>
              {versions.status === "error" ? (
                <p className="text-sm text-muted-foreground">Version history unavailable. Check Doc Registry connectivity; no fallback versions are shown.</p>
              ) : (
                <div className="grid gap-2">
                  {versions.items.map((version) => (
                    <div key={version.id} className="flex flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2 text-xs">
                      <span className="min-w-0 truncate font-mono">{version.id}</span>
                      <span className="flex items-center gap-2">
                        <Badge variant="outline" className="font-mono">{version.version}</Badge>
                        <Badge variant="outline">{artifactStatusText(version.status)}</Badge>
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Readiness history" count={readinessRuns.items.length}>
              <ArtifactReadinessRunsSection runs={readinessRuns.items} status={readinessRuns.status} />
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Audit events" count={events.items.length}>
              <ArtifactEventsSection events={events.items} status={events.status} />
            </ArtifactCompactSection>
          </div>
        </ScrollArea>
      <ArtifactDocumentPreview
        artifact={artifact}
        document={previewDocument}
        workspaceId={workspaceId}
          onOpenChange={(nextOpen) => {
            if (!nextOpen) setPreviewDocument(undefined)
          }}
        />
        <Dialog
          open={pendingDecision !== undefined}
          onOpenChange={(nextOpen) => {
            if (!nextOpen) {
              setPendingDecision(undefined)
              setDecisionNote("")
              setDecisionError("")
            }
          }}
        >
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>{pendingDecision === "approved" ? "Approve artifact" : "Request changes"}</DialogTitle>
              <DialogDescription>
                {pendingDecision === "approved"
                  ? `Approve ${artifact.version} of ${artifact.featureName} as ${reviewer}. Approval unblocks handoff for governed work.`
                  : `Send ${artifact.version} of ${artifact.featureName} back for changes as ${reviewer}.`}
              </DialogDescription>
            </DialogHeader>
            <label className="grid gap-1.5">
              <span className="text-xs font-medium text-muted-foreground">Note (optional)</span>
              <Input
                value={decisionNote}
                onChange={(event) => setDecisionNote(event.target.value)}
                placeholder={pendingDecision === "approved" ? "Why this is ready" : "What must change"}
              />
            </label>
            {decisionError ? (
              <p role="alert" className="text-sm text-destructive">{decisionError}</p>
            ) : null}
            <DialogFooter>
              <Button variant="outline" className="rounded-md" onClick={() => setPendingDecision(undefined)}>
                Cancel
              </Button>
              <Button
                variant={pendingDecision === "approved" ? "default" : "destructive"}
                className="rounded-md"
                disabled={decisionBusy}
                onClick={() => void confirmDecision()}
              >
                {decisionBusy ? "Recording…" : pendingDecision === "approved" ? "Approve" : "Request changes"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </DialogContent>
    </Dialog>
  )
}
