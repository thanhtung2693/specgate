// Artifacts domain: artifact document list, detail dialog, preview, and the
// Artifacts page. Extracted from app-shell.
import { BotIcon, CheckIcon, CodeIcon, ClockIcon, CopyIcon, ExternalLinkIcon, FileTextIcon, GitPullRequestArrowIcon, MessageSquareTextIcon, PaperclipIcon, ShieldCheckIcon, XIcon } from "lucide-react"
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react"
import { Link, useSearchParams } from "react-router-dom"

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
  updateArtifactStatus,
  useArtifactData,
  useFeatureList,
  useArtifactAttachments,
  useArtifactDocumentContent,
  useArtifactEvents,
  useArtifactFiles,
  useArtifactFeature,
  useArtifactFeedback,
  useArtifactGatePreview,
  useArtifactPolicy,
  useArtifactReadinessRuns,
  useArtifactRevisions,
  useArtifactVersions,
  type ArtifactAttachmentSummary,
  type ArtifactDocumentSummary,
  type ArtifactEventSummary,
  type ArtifactFeatureSummary,
  type ArtifactFilesView,
  type ArtifactFeedbackSummary,
  type ArtifactGatePreviewView,
  type ArtifactPolicyView,
  type ArtifactReadinessRunSummary,
  type ArtifactRevisionSummary,
  type ArtifactSummary,
} from "@/data/artifacts"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { gateText, looksLikeWorkItemKey, readableKey, stateText, statusTone, toneClass } from "./shared"
import { ActionTooltip, copyText, MarkdownText, PolicyExplanationSection, runGovernanceAgentPrompt } from "./shared-ui"

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  const kb = bytes / 1024
  if (kb < 1024) return `${kb.toFixed(1)} KB`
  return `${(kb / 1024).toFixed(1)} MB`
}

function artifactStatusText(status: string) {
  return {
    approved: "Approved",
    needs_changes: "Needs changes",
    draft: "Draft",
  }[status] ?? readableKey(status)
}

function ArtifactDocumentList({
  documents,
  status,
  fallbackUpdatedAt,
  selectedPath,
  onPreview,
}: {
  documents: ArtifactDocumentSummary[]
  status: ArtifactFilesView["status"]
  fallbackUpdatedAt?: string
  selectedPath?: string
  onPreview: (document: ArtifactDocumentSummary) => void
}) {
  const collapsedCount = 4
  const [expanded, setExpanded] = useState(false)
  const hasHiddenDocuments = documents.length > collapsedCount
  const visibleDocuments = expanded || !hasHiddenDocuments ? documents : documents.slice(0, collapsedCount)
  const hiddenCount = Math.max(0, documents.length - collapsedCount)

  if (status === "loading") {
    return <p className="text-sm text-muted-foreground">Loading documents...</p>
  }

  if (status === "error") {
    return <p className="text-sm text-muted-foreground">Artifact documents unavailable. Check Doc Registry connectivity; no fallback document list is shown in live mode.</p>
  }

  return (
    <div className="grid gap-2">
      {documents.length === 0 ? (
        <p className="text-sm text-muted-foreground">No documents are available for this artifact yet.</p>
      ) : (
        <>
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
            {visibleDocuments.map((document) => (
              <button
                type="button"
                key={`${document.role}-${document.path}`}
                className={cn(
                  "grid min-h-20 content-between gap-2 rounded-md border bg-card/70 p-2.5 text-left transition-colors hover:bg-accent focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none",
                  selectedPath === document.path && "border-ring bg-accent",
                )}
                onClick={() => onPreview(document)}
              >
                <span className="flex min-w-0 items-start gap-2">
                  <FileTextIcon className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                  <span className="min-w-0">
                    <span className="block truncate font-mono text-[11px] leading-4">{document.path}</span>
                    <span className="block truncate text-[11px] leading-4 text-muted-foreground">{readableKey(document.role)}</span>
                  </span>
                </span>
                <span className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] text-muted-foreground">
                    {formatRelativeTime(document.updatedAt ?? fallbackUpdatedAt)}
                  </span>
                  <Badge variant="outline" className="h-5 w-fit px-1.5 text-[10px]">
                    {formatBytes(document.sizeBytes)}
                  </Badge>
                </span>
              </button>
            ))}
          </div>
          {hasHiddenDocuments ? (
            <Button variant="outline" size="sm" className="mt-1 h-8 rounded-md text-xs" onClick={() => setExpanded((value) => !value)}>
              {expanded ? "Show fewer documents" : `Show ${hiddenCount} more documents`}
            </Button>
          ) : null}
        </>
      )}
    </div>
  )
}

type DiffLine = {
  type: "same" | "added" | "removed"
  text: string
}

function buildLineDiff(before: string, after: string): DiffLine[] {
  const beforeLines = before.replaceAll("\\n", "\n").split("\n")
  const afterLines = after.replaceAll("\\n", "\n").split("\n")
  const matrix = Array.from({ length: beforeLines.length + 1 }, () => Array(afterLines.length + 1).fill(0) as number[])

  for (let beforeIndex = beforeLines.length - 1; beforeIndex >= 0; beforeIndex -= 1) {
    for (let afterIndex = afterLines.length - 1; afterIndex >= 0; afterIndex -= 1) {
      matrix[beforeIndex][afterIndex] =
        beforeLines[beforeIndex] === afterLines[afterIndex]
          ? matrix[beforeIndex + 1][afterIndex + 1] + 1
          : Math.max(matrix[beforeIndex + 1][afterIndex], matrix[beforeIndex][afterIndex + 1])
    }
  }

  const diff: DiffLine[] = []
  let beforeIndex = 0
  let afterIndex = 0

  while (beforeIndex < beforeLines.length && afterIndex < afterLines.length) {
    if (beforeLines[beforeIndex] === afterLines[afterIndex]) {
      diff.push({ type: "same", text: beforeLines[beforeIndex] })
      beforeIndex += 1
      afterIndex += 1
    } else if (matrix[beforeIndex + 1][afterIndex] >= matrix[beforeIndex][afterIndex + 1]) {
      diff.push({ type: "removed", text: beforeLines[beforeIndex] })
      beforeIndex += 1
    } else {
      diff.push({ type: "added", text: afterLines[afterIndex] })
      afterIndex += 1
    }
  }

  while (beforeIndex < beforeLines.length) {
    diff.push({ type: "removed", text: beforeLines[beforeIndex] })
    beforeIndex += 1
  }

  while (afterIndex < afterLines.length) {
    diff.push({ type: "added", text: afterLines[afterIndex] })
    afterIndex += 1
  }

  return diff
}

function DocumentDiffView({ before, after }: { before: string; after: string }) {
  const diff = useMemo(() => buildLineDiff(before, after), [before, after])

  return (
    <div className="overflow-hidden rounded-lg border bg-background/70">
      <div className="border-b px-3 py-2 text-xs text-muted-foreground">
        Line diff
      </div>
      <pre className="max-h-[min(58vh,560px)] overflow-auto py-2 font-mono text-xs leading-5">
        {diff.map((line, index) => (
          <div
            key={`${line.type}-${index}-${line.text}`}
            className={cn(
              "grid grid-cols-[2rem_minmax(0,1fr)] gap-2 px-3",
              line.type === "added" && "bg-success/12 text-success",
              line.type === "removed" && "bg-destructive/12 text-destructive",
              line.type === "same" && "text-muted-foreground",
            )}
          >
            <span className="select-none text-right opacity-70">
              {line.type === "added" ? "+" : line.type === "removed" ? "-" : " "}
            </span>
            <span className="min-w-0 whitespace-pre-wrap break-words">{line.text || " "}</span>
          </div>
        ))}
      </pre>
    </div>
  )
}

function DocumentCodeView({ content }: { content: string }) {
  return (
    <pre className="max-h-[min(58vh,560px)] overflow-auto rounded-lg border bg-background/70 p-4 font-mono text-xs leading-5 text-muted-foreground">
      {content || " "}
    </pre>
  )
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
  if (status === "loading") {
    return <p className="text-sm text-muted-foreground">Loading attachments...</p>
  }

  if (status === "error") {
    return <p className="text-sm text-muted-foreground">Reference attachments unavailable. Check Doc Registry connectivity; no fallback attachments are shown in live mode.</p>
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
  if (status === "loading") {
    return <p className="text-sm text-muted-foreground">Loading feedback...</p>
  }

  if (status === "error") {
    return <p className="text-sm text-muted-foreground">Artifact feedback unavailable. Check Doc Registry connectivity; no fallback feedback is shown in live mode.</p>
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

function ArtifactDocumentPreview({
  artifact,
  document,
  onOpenChange,
}: {
  artifact: ArtifactSummary
  document: ArtifactDocumentSummary | undefined
  onOpenChange: (open: boolean) => void
}) {
  const versions = useArtifactVersions(artifact.featureId, artifact, document !== undefined)
  const [mode, setMode] = useState<"view" | "diff">("view")
  const [viewMode, setViewMode] = useState<"markdown" | "code">("markdown")
  const [copiedDocument, setCopiedDocument] = useState(false)
  const [viewArtifactId, setViewArtifactId] = useState(artifact.id)
  const viewArtifact = versions.items.find((version) => version.id === viewArtifactId) ?? artifact
  const latestArtifact = versions.items[0] ?? artifact
  const content = useArtifactDocumentContent(viewArtifact.id, document?.path, document !== undefined)
  const latestContent = useArtifactDocumentContent(latestArtifact.id, document?.path, mode === "diff")
  const previewContent = content.content.replaceAll("\\n", "\n")
  const hasPreviewContent = content.status === "ready" && previewContent.trim().length > 0
  const canDiff = versions.status === "ready" && versions.items.length > 1 && hasPreviewContent

  useEffect(() => {
    setViewArtifactId(artifact.id)
    setMode("view")
    setViewMode("markdown")
    setCopiedDocument(false)
  }, [artifact.id, document?.path])

  useEffect(() => {
    setCopiedDocument(false)
  }, [previewContent, viewMode])

  useEffect(() => {
    if (mode === "diff" && !canDiff) setMode("view")
  }, [canDiff, mode])

  if (!document) {
    return null
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[calc(100vh-2rem)] max-h-[800px] min-h-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl">
        <DialogHeader className="border-b p-4 pr-12">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div className="min-w-0">
              <DialogTitle>Document inspector</DialogTitle>
              <DialogDescription className="truncate font-mono text-xs">{document.path}</DialogDescription>
            </div>
            <div className="flex flex-wrap items-center gap-2 self-start">
              <div className="flex h-9 rounded-md border bg-background p-0.5">
                <Button
                  type="button"
                  variant={mode === "view" ? "secondary" : "ghost"}
                  size="sm"
                  className="h-8 rounded-sm px-3 text-xs"
                  onClick={() => setMode("view")}
                >
                  View
                </Button>
                <ActionTooltip content="Compare the selected version against the latest version.">
                  <span className="inline-flex">
                    <Button
                      type="button"
                      variant={mode === "diff" ? "secondary" : "ghost"}
                      size="sm"
                      className="h-8 rounded-sm px-3 text-xs"
                      disabled={!canDiff}
                      onClick={() => setMode("diff")}
                    >
                      Diff
                    </Button>
                  </span>
                </ActionTooltip>
              </div>
              <label className="flex h-9 items-center gap-2 rounded-md border bg-background px-2 text-xs text-muted-foreground">
                <span>Version</span>
                <select
                  className="h-7 rounded-sm border-0 bg-transparent px-1 text-sm text-foreground outline-none focus-visible:ring-0"
                  value={viewArtifactId}
                  onChange={(event) => setViewArtifactId(event.target.value)}
                >
                  {versions.items.map((version) => (
                    <option key={version.id} value={version.id}>
                      {version.version}
                    </option>
                  ))}
                </select>
              </label>
              {mode === "diff" ? (
                <Badge variant="outline" className="h-9 rounded-md px-2 text-xs font-normal text-muted-foreground">
                  Latest {latestArtifact.version}
                </Badge>
              ) : null}
            </div>
          </div>
        </DialogHeader>
        <ScrollArea className="min-h-0 flex-1">
          <div className="grid gap-4 p-4">
            {versions.status === "error" ? (
              <p className="text-xs text-muted-foreground">
                Version history unavailable. Check Doc Registry connectivity; no fallback version comparison is shown in live mode.
              </p>
            ) : null}
            {content.status === "loading" || (mode === "diff" && latestContent.status === "loading") ? (
              <p className="text-sm text-muted-foreground">Loading document...</p>
            ) : null}
            {mode === "diff" ? (
              <DocumentDiffView before={previewContent} after={latestContent.content.replaceAll("\\n", "\n")} />
            ) : null}
            {mode === "view" ? (
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex h-8 rounded-md border bg-background p-0.5">
                  <Button
                    type="button"
                    variant={viewMode === "markdown" ? "secondary" : "ghost"}
                    size="sm"
                    className="h-7 rounded-sm px-3 text-xs"
                    onClick={() => setViewMode("markdown")}
                  >
                    Markdown
                  </Button>
                  <ActionTooltip content="View the raw document source.">
                    <Button
                      type="button"
                      variant={viewMode === "code" ? "secondary" : "ghost"}
                      size="sm"
                      className="h-7 rounded-sm px-3 text-xs"
                      onClick={() => setViewMode("code")}
                    >
                      <CodeIcon data-icon="inline-start" />
                      Code
                    </Button>
                  </ActionTooltip>
                </div>
                <ActionTooltip content="Copy the current document body.">
                  <span className="inline-flex">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-8 rounded-md text-xs"
                      onClick={() => {
                        void copyText(previewContent).then((didCopy) => {
                          if (didCopy) setCopiedDocument(true)
                        })
                      }}
                      disabled={!hasPreviewContent}
                    >
                      <CopyIcon data-icon="inline-start" />
                      {copiedDocument ? "Copied" : "Copy"}
                    </Button>
                  </span>
                </ActionTooltip>
              </div>
            ) : null}
            {mode === "view" && previewContent.trim().length === 0 && content.status !== "loading" ? (
              <div className="rounded-lg border bg-background/70 p-4">
                <h4 className="text-sm font-semibold">Preview unavailable</h4>
                <p className="mt-2 text-sm text-muted-foreground">
                  {content.unavailableReason ?? "This document has no Markdown body available yet."}
                </p>
              </div>
            ) : null}
            {mode === "view" && viewMode === "markdown" && previewContent.trim().length > 0 ? <MarkdownText content={previewContent} /> : null}
            {mode === "view" && viewMode === "code" ? <DocumentCodeView content={previewContent} /> : null}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}

function ArtifactReadinessRunsSection({
  runs,
  status,
}: {
  runs: ArtifactReadinessRunSummary[]
  status: "ready" | "loading" | "error"
}) {
  if (status === "loading") {
    return <p className="text-sm text-muted-foreground">Loading readiness history...</p>
  }

  if (status === "error") {
    return <p className="text-sm text-muted-foreground">Readiness history unavailable. Check Doc Registry connectivity; no fallback readiness runs are shown in live mode.</p>
  }

  if (runs.length === 0) {
    return <p className="text-sm text-muted-foreground">No persisted readiness runs recorded.</p>
  }

  return (
    <div className="grid gap-1">
      {runs.slice(0, 5).map((run) => (
        <div key={run.id} className="grid min-h-9 grid-cols-[minmax(0,1fr)_7rem_8rem] items-center gap-2 rounded-md px-2 text-xs hover:bg-muted/35">
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
  if (status === "loading") {
    return <p className="text-sm text-muted-foreground">Loading audit events...</p>
  }

  if (status === "error") {
    return <p className="text-sm text-muted-foreground">Audit events unavailable. Check Doc Registry connectivity; no fallback audit events are shown in live mode.</p>
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

function ArtifactRevisionsSection({
  revisions,
  status,
}: {
  revisions: ArtifactRevisionSummary[]
  status: "ready" | "loading" | "error"
}) {
  if (status === "loading") {
    return <p className="text-sm text-muted-foreground">Loading saved revisions...</p>
  }

  if (status === "error") {
    return <p className="text-sm text-muted-foreground">Saved revisions unavailable. Check Doc Registry connectivity; no fallback revisions are shown in live mode.</p>
  }

  if (revisions.length === 0) {
    return <p className="text-sm text-muted-foreground">No saved draft revisions recorded.</p>
  }

  return (
    <div className="grid gap-1">
      {revisions.slice(0, 5).map((revision) => (
        <div key={revision.id} className="grid min-h-9 grid-cols-[minmax(0,1fr)_7rem_8rem] items-center gap-2 rounded-md px-2 text-xs hover:bg-muted/35">
          <span className="flex min-w-0 items-center gap-2">
            <GitPullRequestArrowIcon className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="min-w-0">
              <span className="block truncate font-mono text-foreground">{revision.id}</span>
              <span className="block truncate text-muted-foreground">
                {revision.materializedArtifactId ? `draft ${revision.materializedArtifactId}` : `base ${revision.baseArtifactId}`}
              </span>
            </span>
          </span>
          <Badge variant="outline" className={cn("h-5 justify-self-start border px-1.5 text-[10px]", toneClass(statusTone("state",revision.state)))}>
            {stateText(revision.state)}
          </Badge>
          <span className="truncate text-right text-muted-foreground">{formatDateTime(revision.createdAt)}</span>
        </div>
      ))}
      {revisions.length > 5 ? (
        <p className="px-2 text-xs text-muted-foreground">Showing latest 5 of {revisions.length} saved revisions.</p>
      ) : null}
    </div>
  )
}

function ArtifactFeatureContext({
  feature,
  status,
  onOpenFeature,
}: {
  feature: ArtifactFeatureSummary | undefined
  status: "ready" | "loading" | "error"
  onOpenFeature: (feature: ArtifactFeatureSummary) => void
}) {
  if (status === "loading") {
    return (
      <section className="rounded-lg border bg-background/70 p-4">
        <h3 className="text-sm font-semibold">Feature context</h3>
        <p className="mt-2 text-sm text-muted-foreground">Loading linked feature...</p>
      </section>
    )
  }

  if (status === "error") {
    return (
      <section className="rounded-lg border bg-background/70 p-4">
        <h3 className="text-sm font-semibold">Feature context</h3>
        <p className="mt-2 text-sm text-muted-foreground">Feature context unavailable. Check Doc Registry connectivity; no fallback feature summary is shown in live mode.</p>
      </section>
    )
  }

  if (!feature) return null

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
            <span className="font-mono">{feature.canonicalArtifactId}</span>
          ) : null}
        </div>
      </div>
      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-md border bg-card/50 px-3 py-2">
        <p className="text-sm text-muted-foreground">
          Feature summary, canonical artifact, and source version live in Feature detail.
        </p>
        <Button type="button" variant="outline" size="sm" className="h-8 rounded-md text-xs" onClick={() => onOpenFeature(feature)}>
          <ExternalLinkIcon data-icon="inline-start" />
          Open feature detail
        </Button>
      </div>
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

function ArtifactGatePreviewSection({ preview }: { preview: ArtifactGatePreviewView }) {
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
      {preview.items.map((gate) => (
        <div key={`${gate.gateKey}:${gate.gateVersion ?? ""}:${gate.executor ?? ""}`} className="rounded-md border bg-background/70 p-3">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <p className="font-medium">{gateText(gate.gateKey)}</p>
              <p className="mt-1 text-xs text-muted-foreground">{gate.note}</p>
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
      ))}
    </div>
  )
}

function ArtifactDetailDialog({
  artifact,
  open,
  reviewer,
  onOpenChange,
  onOpenFeature,
  onDecided,
}: {
  artifact: ArtifactSummary | undefined
  open: boolean
  reviewer: string
  onOpenChange: (open: boolean) => void
  onOpenFeature: (feature: ArtifactFeatureSummary) => void
  onDecided: () => void
}) {
  const files = useArtifactFiles(artifact?.id, artifact !== undefined)
  const attachments = useArtifactAttachments(artifact?.featureId, artifact?.id, artifact !== undefined)
  const feedback = useArtifactFeedback(artifact?.id, artifact !== undefined)
  const readinessRuns = useArtifactReadinessRuns(artifact?.id, artifact !== undefined)
  const events = useArtifactEvents(artifact?.id, artifact !== undefined)
  const revisions = useArtifactRevisions(artifact?.id, artifact !== undefined)
  const feature = useArtifactFeature(artifact, artifact !== undefined)
  const policy = useArtifactPolicy(artifact?.id, artifact !== undefined)
  const gatePreview = useArtifactGatePreview(artifact?.id, artifact?.expectedGates, artifact !== undefined)
  const [previewDocument, setPreviewDocument] = useState<ArtifactDocumentSummary | undefined>(undefined)
  const canOpenWork = looksLikeWorkItemKey(artifact?.featureId)
  const [pendingDecision, setPendingDecision] = useState<"approved" | "needs_changes" | undefined>(undefined)
  const [decisionNote, setDecisionNote] = useState("")
  const [decisionBusy, setDecisionBusy] = useState(false)
  const [decisionError, setDecisionError] = useState("")
  const decidable = artifact?.status === "draft" || artifact?.status === "needs_changes"

  async function confirmDecision() {
    if (!artifact || !pendingDecision) return
    setDecisionBusy(true)
    setDecisionError("")
    try {
      await updateArtifactStatus(artifact.id, pendingDecision, { approvedBy: reviewer, note: decisionNote.trim() || undefined })
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
              {canOpenWork ? (
                <ActionTooltip content="Open the matching SpecGate work item.">
                  <Button asChild variant="outline" size="sm" className="rounded-md">
                    <Link to={`/work/${artifact.featureId ?? ""}`}>
                      <ExternalLinkIcon data-icon="inline-start" />
                      Open work
                    </Link>
                  </Button>
                </ActionTooltip>
              ) : null}
              <ActionTooltip content="Opens governance agent with this artifact context. Does not change artifact state.">
                <Button
                  variant="outline"
                  size="sm"
                  className="rounded-md"
                  onClick={() =>
                    runGovernanceAgentPrompt(
                      `Review artifact ${artifact.id} for ${artifact.featureName}. Summarize its status, expected gates, documents, and the next useful action.`,
                    )
                  }
                >
                  <BotIcon data-icon="inline-start" />
                  Ask agent
                </Button>
              </ActionTooltip>
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
            <ArtifactFeatureContext feature={feature.item} status={feature.status} onOpenFeature={onOpenFeature} />
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
              <ArtifactGatePreviewSection preview={gatePreview} />
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Governance policy" count={policy.item ? 1 : 0}>
              <ArtifactPolicySection policy={policy} />
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Readiness history" count={readinessRuns.items.length}>
              <ArtifactReadinessRunsSection runs={readinessRuns.items} status={readinessRuns.status} />
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Saved revisions" count={revisions.items.length}>
              <ArtifactRevisionsSection revisions={revisions.items} status={revisions.status} />
            </ArtifactCompactSection>
            <ArtifactCompactSection title="Audit events" count={events.items.length}>
              <ArtifactEventsSection events={events.items} status={events.status} />
            </ArtifactCompactSection>
          </div>
        </ScrollArea>
        <ArtifactDocumentPreview
          artifact={artifact}
          document={previewDocument}
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

function FeatureLookupTable({
  features,
  status,
  filtered,
  onOpenFeature,
}: {
  features: ArtifactFeatureSummary[]
  status: "ready" | "loading" | "error"
  filtered: boolean
  onOpenFeature: (feature: ArtifactFeatureSummary) => void
}) {
  if (status === "loading") {
    return <div className="px-4 py-8 text-center text-sm text-muted-foreground">Loading features…</div>
  }
  if (status === "error") {
    return (
      <div className="px-4 py-8 text-center text-sm text-muted-foreground">
        Feature list unavailable. Check Doc Registry connectivity, then refresh.
      </div>
    )
  }
  if (features.length === 0) {
    return (
      <div className="px-4 py-8 text-center">
        <h3 className="text-sm font-semibold">{filtered ? "No features match" : "No features yet"}</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          {filtered
            ? "Clear the search to see all features."
            : "Publish a governed artifact from the CLI or IDE agent to create the first feature."}
        </p>
      </div>
    )
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[640px] table-fixed text-left text-xs md:min-w-full">
        <colgroup>
          <col className="w-[42%]" />
          <col className="w-[34%]" />
          <col className="w-[14%]" />
          <col className="w-[10%]" />
        </colgroup>
        <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
          <tr>
            <th className="px-3 py-2 font-medium">Feature key</th>
            <th className="px-3 py-2 font-medium">Name</th>
            <th className="px-3 py-2 font-medium">Status</th>
            <th className="px-3 py-2 font-medium">Version</th>
          </tr>
        </thead>
        <tbody>
          {features.map((feature) => (
            <tr key={feature.id} className="border-b transition-colors last:border-b-0 hover:bg-muted/35">
              <td className="px-3 py-2">
                <button
                  type="button"
                  className="group flex min-w-0 items-center gap-2 text-left font-mono text-foreground focus-visible:rounded-sm focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
                  aria-label={`Open feature ${feature.key}`}
                  onClick={() => onOpenFeature(feature)}
                >
                  <ExternalLinkIcon className="size-3.5 shrink-0 text-muted-foreground transition-colors group-hover:text-foreground" />
                  <span className="truncate" title={feature.key}>{feature.key}</span>
                </button>
              </td>
              <td className="truncate px-3 py-2">{feature.name}</td>
              <td className="px-3 py-2">
                <Badge variant="outline" className={cn("border text-[0.68rem]", toneClass(statusTone("artifact",feature.status)))}>
                  {artifactStatusText(feature.status)}
                </Badge>
              </td>
              <td className="px-3 py-2 font-mono tabular-nums text-muted-foreground">
                {feature.version != null ? `v${feature.version}` : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function FeatureDetailDialog({
  feature,
  open,
  onOpenChange,
  onOpenCanonicalArtifact,
  relatedArtifacts,
}: {
  feature: ArtifactFeatureSummary | undefined
  open: boolean
  onOpenChange: (open: boolean) => void
  onOpenCanonicalArtifact: (artifactId: string) => void
  relatedArtifacts: ArtifactSummary[]
}) {
  if (!feature) return null

  const summary = (feature.summaryMd || feature.summary || "").trim()
  const canOpenWork = looksLikeWorkItemKey(feature.key)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="grid h-[min(760px,calc(100svh-2rem))] grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-3xl">
        <DialogHeader className="border-b p-4 pr-12">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="secondary" className="font-mono">{feature.key}</Badge>
                <Badge variant="outline" className={cn("border", toneClass(statusTone("artifact",feature.status)))}>
                  {artifactStatusText(feature.status)}
                </Badge>
              </div>
              <DialogTitle className="mt-3 text-lg">{feature.name}</DialogTitle>
              <DialogDescription className="truncate font-mono text-xs">{feature.id}</DialogDescription>
            </div>
            <div className="flex flex-wrap gap-2">
              {feature.canonicalArtifactId ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="rounded-md"
                  onClick={() => onOpenCanonicalArtifact(feature.canonicalArtifactId as string)}
                >
                  <FileTextIcon data-icon="inline-start" />
                  Open canonical artifact
                </Button>
              ) : null}
              {canOpenWork ? (
                <Button asChild variant="outline" size="sm" className="rounded-md">
                  <Link to={`/work/${feature.key}`}>
                    <ExternalLinkIcon data-icon="inline-start" />
                    Open work
                  </Link>
                </Button>
              ) : null}
            </div>
          </div>
        </DialogHeader>
        <ScrollArea className="min-h-0 overflow-hidden">
          <div className="grid min-w-0 gap-4 p-4">
            <div className="grid gap-3 text-sm sm:grid-cols-3">
              <div className="rounded-md border bg-background/70 p-3">
                <span className="text-xs text-muted-foreground">Version</span>
                <p className="mt-1 font-mono text-xs">{feature.version != null ? `v${feature.version}` : "Not recorded"}</p>
              </div>
              <div className="rounded-md border bg-background/70 p-3">
                <span className="text-xs text-muted-foreground">Summary source</span>
                <p className="mt-1 font-mono text-xs">{feature.summarySourceVersion ? `summary ${feature.summarySourceVersion}` : "No summary source"}</p>
              </div>
              <div className="rounded-md border bg-background/70 p-3">
                <span className="text-xs text-muted-foreground">Canonical artifact</span>
                <p className="mt-1 truncate font-mono text-xs">{feature.canonicalArtifactId ?? "Not selected"}</p>
              </div>
            </div>
            <section className="rounded-lg border bg-background/70 p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-semibold">Feature summary</h3>
                {feature.summarySourceVersion ? (
                  <Badge variant="outline" className="rounded-md font-mono text-[0.68rem]">
                    summary {feature.summarySourceVersion}
                  </Badge>
                ) : null}
              </div>
              {summary ? (
                <div className="mt-3 max-h-[42svh] overflow-y-auto rounded-md border bg-card/50 p-3">
                  <MarkdownText content={summary} />
                </div>
              ) : (
                <p className="mt-3 text-sm text-muted-foreground">
                  No feature summary is recorded yet. Summary drafting stays owned by the governance agent/backend flow.
                </p>
              )}
            </section>
            <section className="rounded-lg border bg-background/70 p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-semibold">Related artifacts</h3>
                <Badge variant="outline" className="rounded-md font-mono text-[0.68rem]">
                  {relatedArtifacts.length}
                </Badge>
              </div>
              {relatedArtifacts.length === 0 ? (
                <p className="mt-3 text-sm text-muted-foreground">
                  No artifact rows for this feature are loaded in the current library.
                </p>
              ) : (
                <div className="mt-3 grid gap-2">
                  {relatedArtifacts.map((artifact) => {
                    const canonical = feature.canonicalArtifactId === artifact.id
                    return (
                      <div
                        key={artifact.id}
                        className="grid gap-2 rounded-md border bg-card/50 p-3 text-sm sm:grid-cols-[minmax(0,1fr)_auto]"
                      >
                        <div className="min-w-0">
                          <div className="flex min-w-0 flex-wrap items-center gap-2">
                            <p className="truncate font-medium">{artifact.featureName}</p>
                            {canonical ? (
                              <Badge variant="secondary" className="rounded-md text-[0.68rem]">
                                Canonical
                              </Badge>
                            ) : null}
                            <Badge variant="outline" className={cn("border text-[0.68rem]", toneClass(statusTone("artifact",artifact.status)))}>
                              {artifactStatusText(artifact.status)}
                            </Badge>
                          </div>
                          <div className="mt-1 flex flex-wrap gap-2 text-xs text-muted-foreground">
                            <span className="font-mono">{artifact.id}</span>
                            <span className="font-mono">{artifact.version}</span>
                            <span>{formatDateTime(artifact.updatedAt)}</span>
                          </div>
                        </div>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-8 justify-self-start rounded-md text-xs sm:justify-self-end"
                          aria-label={`Open artifact ${artifact.id}`}
                          onClick={() => onOpenCanonicalArtifact(artifact.id)}
                        >
                          <FileTextIcon data-icon="inline-start" />
                          Open artifact
                        </Button>
                      </div>
                    )
                  })}
                </div>
              )}
            </section>
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}

export function ArtifactsPage({ reviewer }: { reviewer: string }) {
  const artifacts = useArtifactData()
  const [searchParams, setSearchParams] = useSearchParams()
  const artifactParam = searchParams.get("artifact") ?? undefined
  const featureParam = searchParams.get("feature") ?? undefined
  const [selectedId, setSelectedId] = useState<string | undefined>(() => artifactParam)
  const [selectedFeatureId, setSelectedFeatureId] = useState<string | undefined>(() => featureParam)
  const [selectedFeatureFallback, setSelectedFeatureFallback] = useState<ArtifactFeatureSummary | undefined>(undefined)
  const [artifactQuery, setArtifactQuery] = useState("")
  const [artifactStatusFilter, setArtifactStatusFilter] = useState("all")
  const [libraryView, setLibraryView] = useState<"artifacts" | "features">("artifacts")
  const features = useFeatureList()
  const filteredFeatures = useMemo(() => {
    const query = artifactQuery.trim().toLowerCase()
    if (!query) return features.items
    return features.items.filter((feature) =>
      [feature.key, feature.name, feature.status].join(" ").toLowerCase().includes(query),
    )
  }, [artifactQuery, features.items])
  const artifactStatuses = useMemo(
    () => Array.from(new Set(artifacts.items.map((artifact) => artifact.status))).filter(Boolean),
    [artifacts.items],
  )
  const filteredArtifacts = useMemo(() => {
    const query = artifactQuery.trim().toLowerCase()
    return artifacts.items.filter((artifact) => {
      const matchesStatus = artifactStatusFilter === "all" || artifact.status === artifactStatusFilter
      const searchable = [
        artifact.featureName,
        artifact.featureId,
        artifact.id,
        artifact.version,
        artifactStatusText(artifact.status),
        readableKey(artifact.requestType),
      ]
        .join(" ")
        .toLowerCase()
      return matchesStatus && (!query || searchable.includes(query))
    })
  }, [artifactQuery, artifactStatusFilter, artifacts.items])
  const selectedArtifact = artifacts.items.find((artifact) => artifact.id === selectedId)
  const selectedFeature =
    features.items.find((feature) => feature.id === selectedFeatureId || feature.key === selectedFeatureId) ??
    (selectedFeatureFallback && (selectedFeatureFallback.id === selectedFeatureId || selectedFeatureFallback.key === selectedFeatureId)
      ? selectedFeatureFallback
      : undefined)
  const selectedFeatureArtifacts = useMemo(() => {
    if (!selectedFeature) return []
    const featureRefs = new Set([selectedFeature.id, selectedFeature.key].filter(Boolean))
    return artifacts.items
      .filter((artifact) => artifact.id === selectedFeature.canonicalArtifactId || (artifact.featureId ? featureRefs.has(artifact.featureId) : false))
      .sort((a, b) => {
        if (a.id === selectedFeature.canonicalArtifactId) return -1
        if (b.id === selectedFeature.canonicalArtifactId) return 1
        const versionOrder = b.version.localeCompare(a.version, undefined, { numeric: true })
        if (versionOrder !== 0) return versionOrder
        return b.updatedAt.localeCompare(a.updatedAt)
      })
  }, [artifacts.items, selectedFeature])
  const filtersActive = artifactQuery.trim().length > 0 || artifactStatusFilter !== "all"
  const liveArtifactEmpty = artifacts.source === "registry" && !filtersActive
  const artifactEmptyTitle =
    artifacts.status === "loading" ? "Loading artifacts" : liveArtifactEmpty ? "No artifacts in this workspace" : "No artifacts match"
  const artifactEmptyDescription =
    artifacts.status === "loading"
      ? "Reading artifact bundles from Doc Registry."
      : liveArtifactEmpty
        ? "Publish a governed artifact from the CLI or IDE agent, then refresh this library."
        : "Clear search or status filters to inspect the full library."

  const updateArtifactQuery = useCallback((id: string | undefined, nextFeatureId?: string | null) => {
    const next = new URLSearchParams(searchParams)
    if (id) {
      next.set("artifact", id)
    } else {
      next.delete("artifact")
    }
    if (nextFeatureId === null) {
      next.delete("feature")
    } else if (nextFeatureId) {
      next.set("feature", nextFeatureId)
    }
    setSearchParams(next)
  }, [searchParams, setSearchParams])

  const updateFeatureQuery = useCallback((id: string | undefined, nextArtifactId?: string | null) => {
    const next = new URLSearchParams(searchParams)
    if (id) {
      next.set("feature", id)
    } else {
      next.delete("feature")
    }
    if (nextArtifactId === null) {
      next.delete("artifact")
    } else if (nextArtifactId) {
      next.set("artifact", nextArtifactId)
    }
    setSearchParams(next)
  }, [searchParams, setSearchParams])

  function selectArtifact(id: string) {
    setSelectedFeatureId(undefined)
    setSelectedId(id)
    updateArtifactQuery(id, null)
  }

  function openFeatureDetail(feature: ArtifactFeatureSummary) {
    setSelectedId(undefined)
    setSelectedFeatureFallback(feature)
    setSelectedFeatureId(feature.id)
    updateFeatureQuery(feature.id, null)
  }

  function openCanonicalArtifact(id: string) {
    setSelectedFeatureId(undefined)
    setSelectedId(id)
    setLibraryView("artifacts")
    updateArtifactQuery(id, null)
  }

  useEffect(() => {
    setSelectedId(artifactParam)
  }, [artifactParam])

  useEffect(() => {
    setSelectedFeatureId(featureParam)
  }, [featureParam])

  function clearArtifactFilters() {
    setArtifactQuery("")
    setArtifactStatusFilter("all")
  }

  return (
    <div className="grid gap-4">
      <section className="min-w-0 overflow-hidden rounded-lg border bg-card">
        <div className="grid gap-2 border-b px-3 py-2.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-sm font-semibold">Artifact library</h2>
              <Badge variant="outline" className="font-mono">
                {libraryView === "artifacts" ? filteredArtifacts.length : filteredFeatures.length}
              </Badge>
              <p className="text-xs text-muted-foreground">
                {libraryView === "artifacts" ? "versioned bundles" : "features · look up a key to link an artifact"}
              </p>
            </div>
            <div className="flex gap-1.5">
              <Button
                type="button"
                variant={libraryView === "artifacts" ? "secondary" : "outline"}
                size="sm"
                className="h-8 rounded-md text-xs"
                onClick={() => setLibraryView("artifacts")}
              >
                Artifacts
              </Button>
              <Button
                type="button"
                variant={libraryView === "features" ? "secondary" : "outline"}
                size="sm"
                className="h-8 rounded-md text-xs"
                onClick={() => setLibraryView("features")}
              >
                Features
              </Button>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Input
              className="h-8 min-w-[220px] flex-1 rounded-md text-sm"
              value={artifactQuery}
              onChange={(event) => setArtifactQuery(event.target.value)}
              placeholder={libraryView === "artifacts" ? "Search artifacts" : "Search features by key or name"}
              aria-label={libraryView === "artifacts" ? "Search artifacts" : "Search features"}
            />
            {libraryView === "artifacts" ? (
              <div className="flex flex-wrap gap-1.5">
                <Button
                  type="button"
                  variant={artifactStatusFilter === "all" ? "secondary" : "outline"}
                  size="sm"
                  className="h-8 rounded-md text-xs"
                  onClick={() => setArtifactStatusFilter("all")}
                >
                  All
                </Button>
                {artifactStatuses.map((status) => (
                  <Button
                    type="button"
                    key={status}
                    variant={artifactStatusFilter === status ? "secondary" : "outline"}
                    size="sm"
                    className="h-8 rounded-md text-xs"
                    onClick={() => setArtifactStatusFilter(status)}
                  >
                    {artifactStatusText(status)}
                  </Button>
                ))}
              </div>
            ) : null}
          </div>
        </div>
        {libraryView === "artifacts" ? (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] table-fixed text-left text-xs md:min-w-full">
            <colgroup>
              <col className="w-[38%]" />
              <col className="w-[12%]" />
              <col className="w-[14%]" />
              <col className="w-[18%]" />
              <col className="w-[18%]" />
            </colgroup>
            <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-medium">Artifact</th>
                <th className="px-3 py-2 font-medium">Version</th>
                <th className="px-3 py-2 font-medium">Impact</th>
                <th className="px-3 py-2 font-medium">Type</th>
                <th className="px-3 py-2 font-medium">Updated</th>
              </tr>
            </thead>
            <tbody>
              {filteredArtifacts.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center">
                    <h3 className="text-sm font-semibold">{artifactEmptyTitle}</h3>
                    <p className="mt-1 text-sm text-muted-foreground">{artifactEmptyDescription}</p>
                    {filtersActive ? (
                      <Button type="button" variant="outline" size="sm" className="mt-3 rounded-md" onClick={clearArtifactFilters}>
                        Clear filters
                      </Button>
                    ) : liveArtifactEmpty ? (
                      <div className="mt-3 inline-flex rounded-md border bg-background px-3 py-2 font-mono text-xs text-muted-foreground">
                        specgate artifact list
                      </div>
                    ) : null}
                  </td>
                </tr>
              ) : (
                filteredArtifacts.map((artifact) => (
                <tr
                  key={artifact.id}
                  className={cn(
                    "border-b transition-colors last:border-b-0 hover:bg-muted/35",
                    artifact.id === selectedId && "bg-muted/45",
                  )}
                >
                  <td className="px-3 py-2">
                    <button type="button" className="min-w-0 text-left" onClick={() => selectArtifact(artifact.id)}>
                      <span className="flex min-w-0 flex-wrap items-center gap-2">
                        <span className="truncate font-medium">{artifact.featureName}</span>
                        <Badge variant="outline" className={cn("shrink-0 border text-[0.68rem]", toneClass(statusTone("artifact",artifact.status)))}>
                          {artifactStatusText(artifact.status)}
                        </Badge>
                      </span>
                      <span className="mt-1 block font-mono text-xs text-muted-foreground">{artifact.featureId ?? "standalone quick-path"}</span>
                    </button>
                  </td>
                  <td className="truncate px-3 py-2 font-mono text-muted-foreground">{artifact.version}</td>
                  <td className="truncate px-3 py-2">{readableKey(artifact.impactLevel)}</td>
                  <td className="truncate px-3 py-2">{readableKey(artifact.requestType)}</td>
                  <td className="truncate px-3 py-2 text-muted-foreground">{formatDateTime(artifact.updatedAt)}</td>
                </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
        ) : (
          <FeatureLookupTable
            features={filteredFeatures}
            status={features.status}
            filtered={artifactQuery.trim().length > 0}
            onOpenFeature={openFeatureDetail}
          />
        )}
      </section>
      <ArtifactDetailDialog
        artifact={selectedArtifact}
        open={selectedArtifact !== undefined}
        reviewer={reviewer}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedId(undefined)
            updateArtifactQuery(undefined)
          }
        }}
        onOpenFeature={openFeatureDetail}
        onDecided={artifacts.refresh}
      />
      <FeatureDetailDialog
        feature={selectedFeature}
        open={selectedFeature !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedFeatureId(undefined)
            updateFeatureQuery(undefined)
          }
        }}
        onOpenCanonicalArtifact={openCanonicalArtifact}
        relatedArtifacts={selectedFeatureArtifacts}
      />
    </div>
  )
}
