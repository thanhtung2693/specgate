// Document-viewer cluster extracted from artifacts.tsx: the artifact document
// list, line-diff/code views, and the document inspector dialog.

import { CodeIcon, CopyIcon, FileTextIcon } from "lucide-react"
import { useEffect, useMemo, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  useArtifactDocumentContent,
  useArtifactVersions,
  type ArtifactDocumentSummary,
  type ArtifactFilesView,
  type ArtifactSummary,
} from "@/data/artifacts"
import { formatRelativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { readableKey } from "../shared"
import { ActionTooltip, copyText, MarkdownText } from "../shared-ui"

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  const kb = bytes / 1024
  if (kb < 1024) return `${kb.toFixed(1)} KB`
  return `${(kb / 1024).toFixed(1)} MB`
}

export function ArtifactDocumentList({
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

export function ArtifactDocumentPreview({
  artifact,
  document,
  workspaceId,
  onOpenChange,
}: {
  artifact: ArtifactSummary
  document: ArtifactDocumentSummary | undefined
  workspaceId: string
  onOpenChange: (open: boolean) => void
}) {
  const versions = useArtifactVersions(artifact.featureId, artifact, workspaceId, document !== undefined)
  const [mode, setMode] = useState<"view" | "diff">("view")
  const [viewMode, setViewMode] = useState<"markdown" | "code">("markdown")
  const [copiedDocument, setCopiedDocument] = useState(false)
  const [viewArtifactId, setViewArtifactId] = useState(artifact.id)
  const viewArtifact = versions.items.find((version) => version.id === viewArtifactId) ?? artifact
  const latestArtifact = versions.items[0] ?? artifact
  const content = useArtifactDocumentContent(viewArtifact.id, document?.path, workspaceId, document !== undefined)
  const latestContent = useArtifactDocumentContent(latestArtifact.id, document?.path, workspaceId, mode === "diff")
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
