import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react"
import { Link } from "react-router-dom"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import {
  curateKnowledgeLinks,
  deleteKnowledgeVersion,
  getKnowledgeDocument,
  listKnowledgeDocuments,
  retryKnowledgeDocument,
  uploadKnowledgeDocument,
  type KnowledgeDocument,
  type KnowledgeDocumentDetail,
} from "@/data/knowledge"
import { docRegistryBase } from "@/data/model-settings"
import { formatDateTime, formatRelativeTime } from "@/lib/format"

const documentTypes = ["product_brief", "srs", "design_reference", "supporting_doc", "existing_artifact", "qa_finding", "policy_doc"]
const statuses = ["uploaded", "parsing", "chunked", "embedded", "indexed", "failed"]
const terminalStatuses = new Set(["indexed", "failed"])

function knowledgeLabel(value: string) {
  const normalized = value.replaceAll("_", " ").trim()
  return normalized ? normalized[0].toUpperCase() + normalized.slice(1) : "Unknown"
}

type UploadTarget = KnowledgeDocument | null | undefined

export function KnowledgePage({ workspaceId, uploader }: { workspaceId?: string; uploader: string }) {
  const registry = useMemo(() => docRegistryBase() ?? "/api/doc-registry", [])
  const [items, setItems] = useState<KnowledgeDocument[]>([])
  const [embeddingsEnabled, setEmbeddingsEnabled] = useState(false)
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">("loading")
  const [loadError, setLoadError] = useState("")
  const [query, setQuery] = useState("")
  const [typeFilter, setTypeFilter] = useState("")
  const [statusFilter, setStatusFilter] = useState("")
  const [selected, setSelected] = useState<KnowledgeDocumentDetail>()
  const [uploadTarget, setUploadTarget] = useState<UploadTarget>(undefined)
  const [curateOpen, setCurateOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const listControllerRef = useRef<AbortController | undefined>(undefined)
  const listGenerationRef = useRef(0)
  const [mutationError, setMutationError] = useState("")
  const pollingDocumentId = selected?.document.documentId
  const pollingVersion = selected?.document.version
  const pollingStatus = selected?.document.status

  const load = useCallback(async () => {
    listControllerRef.current?.abort()
    const controller = new AbortController()
    listControllerRef.current = controller
    const generation = ++listGenerationRef.current
    if (!workspaceId) {
      setEmbeddingsEnabled(false)
      setLoadState("error")
      setLoadError("Knowledge needs a configured Doc Registry workspace.")
      return
    }
    setLoadState("loading")
    setEmbeddingsEnabled(false)
    try {
      const result = await listKnowledgeDocuments(registry, workspaceId, controller.signal)
      if (generation !== listGenerationRef.current) return
      setItems(result.items)
      setEmbeddingsEnabled(result.embeddingsEnabled)
      setLoadState("ready")
    } catch (error: unknown) {
      if (error instanceof DOMException && error.name === "AbortError") return
      if (generation !== listGenerationRef.current) return
      setLoadError("Doc Registry Knowledge is unavailable.")
      setLoadState("error")
    }
  }, [registry, workspaceId])

  const openDocument = useCallback(async (document: KnowledgeDocument, version = document.version) => {
    try {
      if (!workspaceId) return
      setSelected(await getKnowledgeDocument(registry, workspaceId, document.documentId, version))
    } catch {
      setMutationError("Unable to load document detail.")
    }
  }, [registry, workspaceId])

  useEffect(() => {
    void load()
    return () => {
      listGenerationRef.current += 1
      listControllerRef.current?.abort()
    }
  }, [load])

  useEffect(() => {
    if (!pollingDocumentId || !pollingVersion || !pollingStatus || terminalStatuses.has(pollingStatus)) return
    let attempts = 0
    const timer = window.setInterval(() => {
      attempts += 1
      if (attempts > 12) {
        window.clearInterval(timer)
        return
      }
      if (!workspaceId) return
      void getKnowledgeDocument(registry, workspaceId, pollingDocumentId, pollingVersion).then((next) => {
        setSelected(next)
        if (terminalStatuses.has(next.document.status)) {
          window.clearInterval(timer)
          void load()
        }
      }).catch(() => window.clearInterval(timer))
    }, 1000)
    return () => window.clearInterval(timer)
  }, [load, pollingDocumentId, pollingStatus, pollingVersion, registry, workspaceId])

  const filteredItems = items.filter((document) => {
    const matchesQuery = !query || `${document.title} ${document.documentId}`.toLowerCase().includes(query.toLowerCase())
    return matchesQuery && (!typeFilter || document.documentType === typeFilter) && (!statusFilter || document.status === statusFilter)
  })

  async function submitUpload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!workspaceId || busyRef.current) return
    const form = new FormData(event.currentTarget)
    const file = form.get("file")
    if (!(file instanceof File)) return
    busyRef.current = true
    setBusy(true)
    setMutationError("")
    try {
      const document = await uploadKnowledgeDocument(registry, {
        workspaceId,
        uploadedBy: uploader,
        file,
        title: String(form.get("title")),
        documentType: String(form.get("document_type")),
        authorityLevel: String(form.get("authority_level")),
        tags: String(form.get("tags") || "").split(",").map((tag) => tag.trim()).filter(Boolean),
        notes: String(form.get("notes") || ""),
        linkedFeatureId: String(form.get("linked_feature_id") || ""),
        linkedRequestId: String(form.get("linked_request_id") || ""),
        documentId: uploadTarget?.documentId,
        parentVersion: uploadTarget?.version,
        newVersion: String(form.get("new_version") || ""),
      })
      setUploadTarget(undefined)
      await openDocument(document)
      await load()
    } catch {
      setMutationError("Upload failed.")
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }

  async function submitCuration(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selected || busyRef.current) return
    const form = new FormData(event.currentTarget)
    const feature = String(form.get("linked_feature_id") || "").trim()
    const request = String(form.get("linked_request_id") || "").trim()
    busyRef.current = true
    setBusy(true)
    setMutationError("")
    try {
      if (!workspaceId) return
      const document = await curateKnowledgeLinks(registry, selected.document.documentId, {
        workspaceId,
        version: selected.document.version,
        linkedFeatureId: feature || undefined,
        linkedRequestId: request || undefined,
        clearFeatureLink: form.get("clear_feature_link") === "on",
        clearRequestLink: form.get("clear_request_link") === "on",
        uploadedBy: uploader,
        notes: String(form.get("notes") || ""),
      })
      setCurateOpen(false)
      await openDocument(document)
      await load()
    } catch {
      setMutationError("Link curation failed.")
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }

  async function retrySelectedDocument() {
    if (!selected || busyRef.current) return
    busyRef.current = true
    setBusy(true)
    setMutationError("")
    try {
      if (!workspaceId) return
      const document = await retryKnowledgeDocument(registry, workspaceId, selected.document.documentId, selected.document.version)
      await openDocument(document)
    } catch {
      setMutationError("Retry failed.")
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }

  async function deleteSelectedVersion() {
    if (!selected || busyRef.current) return
    if (!window.confirm(`Delete ${selected.document.documentId} exact version ${selected.document.version}?`)) return
    busyRef.current = true
    setBusy(true)
    setMutationError("")
    try {
      if (!workspaceId) return
      await deleteKnowledgeVersion(registry, workspaceId, selected.document.documentId, selected.document.version)
      setSelected(undefined)
      await load()
    } catch {
      setMutationError("Delete failed.")
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }

  return (
    <section className="grid gap-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold">Knowledge library</h2>
          <p className="text-sm text-muted-foreground">Versioned source material for governance context. Source frameworks remain the authors.</p>
        </div>
        <Button aria-label="Upload document" disabled={loadState !== "ready" || !embeddingsEnabled || !workspaceId} onClick={() => setUploadTarget(null)}>Upload document</Button>
      </header>

      {loadState === "ready" && !embeddingsEnabled ? <p role="alert" className="rounded-md border p-3 text-sm">Embeddings are disabled. Configure a local embedding provider in <Link className="underline" to="/knowledge?settings=models">Settings → Models</Link>.</p> : null}

      <div className="grid gap-2 sm:grid-cols-3">
        <Input aria-label="Search knowledge" placeholder="Search title or ID" value={query} onChange={(event) => setQuery(event.target.value)} />
        <select aria-label="Document type" className="rounded-md border bg-background px-3" value={typeFilter} onChange={(event) => setTypeFilter(event.target.value)}><option value="">All types</option>{documentTypes.map((value) => <option key={value} value={value}>{knowledgeLabel(value)}</option>)}</select>
        <select aria-label="Ingestion status" className="rounded-md border bg-background px-3" value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}><option value="">All statuses</option>{statuses.map((value) => <option key={value} value={value}>{knowledgeLabel(value)}</option>)}</select>
      </div>

      {loadState === "loading" ? <p>Loading Knowledge…</p> : null}
      {loadState === "error" ? <div><p role="alert">{loadError}</p><Button variant="outline" onClick={() => void load()}>Retry</Button></div> : null}
      {loadState === "ready" && filteredItems.length === 0 ? <p>No Knowledge documents match this view.</p> : null}
      {loadState === "ready" && filteredItems.length > 0 ? (
        <div className="overflow-x-auto rounded-lg border">
          <table className="min-w-0 w-full text-sm md:min-w-[760px]" aria-label="Knowledge documents">
            <caption className="sr-only">Versioned Knowledge documents in the active workspace</caption>
            <thead className="hidden md:table-header-group"><tr className="border-b text-left"><th scope="col" className="p-3">Document</th><th scope="col" className="px-3 py-2">Version</th><th scope="col" className="px-3 py-2">Status</th><th scope="col" className="px-3 py-2">Type</th><th scope="col" className="px-3 py-2">Authority</th><th scope="col" className="px-3 py-2">Links</th><th scope="col" className="px-3 py-2">Updated</th></tr></thead>
            <tbody className="block md:table-row-group">{filteredItems.map((document) => (
              <tr key={`${document.documentId}-${document.version}`} className="block border-b p-3 last:border-b-0 md:table-row md:p-0">
                <td className="block px-3 py-1.5 md:table-cell md:p-3"><span className="mr-2 text-xs font-medium uppercase tracking-wide text-muted-foreground md:hidden">Document</span><button type="button" className="font-medium underline underline-offset-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50" onClick={() => void openDocument(document)}>{document.title}</button><div className="text-xs text-muted-foreground">{document.documentId}</div></td>
                <td className="block px-3 py-1.5 md:table-cell md:p-3"><span className="mr-2 text-xs font-medium uppercase tracking-wide text-muted-foreground md:hidden">Version</span>{document.version}</td>
                <td className="block px-3 py-1.5 md:table-cell md:p-3"><span className="mr-2 text-xs font-medium uppercase tracking-wide text-muted-foreground md:hidden">Status</span>{knowledgeLabel(document.status)}</td>
                <td className="block px-3 py-1.5 md:table-cell md:p-3"><span className="mr-2 text-xs font-medium uppercase tracking-wide text-muted-foreground md:hidden">Type</span>{knowledgeLabel(document.documentType)}</td>
                <td className="block px-3 py-1.5 md:table-cell md:p-3"><span className="mr-2 text-xs font-medium uppercase tracking-wide text-muted-foreground md:hidden">Authority</span>{knowledgeLabel(document.authorityLevel)}</td>
                <td className="block px-3 py-1.5 md:table-cell md:p-3"><span className="mr-2 text-xs font-medium uppercase tracking-wide text-muted-foreground md:hidden">Links</span>{[document.linkedFeatureId, document.linkedRequestId].filter(Boolean).join(" · ") || "—"}</td>
                <td className="block px-3 py-1.5 text-muted-foreground md:table-cell md:p-3" title={formatDateTime(document.updatedAt)}><span className="mr-2 text-xs font-medium uppercase tracking-wide text-muted-foreground md:hidden">Updated</span>{formatRelativeTime(document.updatedAt)}</td>
              </tr>
            ))}</tbody>
          </table>
        </div>
      ) : null}

      <Dialog open={uploadTarget !== undefined} onOpenChange={(open) => !open && setUploadTarget(undefined)}>
        <DialogContent>
          <form className="grid gap-3" onSubmit={submitUpload}>
            <DialogHeader>
              <DialogTitle>{uploadTarget ? "Upload new file version" : "Upload Knowledge document"}</DialogTitle>
              <DialogDescription>
                {uploadTarget ? `${uploadTarget.documentId} · parent ${uploadTarget.version}` : "Creates an immutable document version."}
              </DialogDescription>
            </DialogHeader>
            {uploadTarget ? <label className="grid gap-1.5 text-xs font-medium"><span>New version</span><Input name="new_version" aria-label="New version" placeholder="v2" required /></label> : null}
            <label className="grid gap-1.5 text-xs font-medium"><span>Document file</span><Input name="file" aria-label="Document file" type="file" required /></label>
            <label className="grid gap-1.5 text-xs font-medium"><span>Title</span><Input name="title" aria-label="Document title" placeholder="Title" defaultValue={uploadTarget?.title} required /></label>
            <label className="grid gap-1.5 text-xs font-medium"><span>Document type</span><select name="document_type" aria-label="Upload document type" required defaultValue={uploadTarget?.documentType ?? ""} className="rounded-md border bg-background p-2 font-normal">
              <option value="">Choose a type</option>
              {documentTypes.map((value) => <option key={value} value={value}>{knowledgeLabel(value)}</option>)}
            </select></label>
            <label className="grid gap-1.5 text-xs font-medium"><span>Authority</span><select name="authority_level" aria-label="Authority" required defaultValue={uploadTarget?.authorityLevel ?? ""} className="rounded-md border bg-background p-2 font-normal">
              <option value="">Choose an authority level</option>
              {["source_of_truth", "high", "reference", "low"].map((value) => <option key={value} value={value}>{knowledgeLabel(value)}</option>)}
            </select></label>
            <label className="grid gap-1.5 text-xs font-medium"><span>Tags <span className="font-normal text-muted-foreground">(optional)</span></span><Input name="tags" aria-label="Tags" placeholder="design, policy" defaultValue={uploadTarget?.tags.join(", ")} /></label>
            <label className="grid gap-1.5 text-xs font-medium"><span>Notes <span className="font-normal text-muted-foreground">(optional)</span></span><Input name="notes" aria-label="Upload notes" placeholder="Why this document matters" defaultValue={uploadTarget?.notes} /></label>
            <label className="grid gap-1.5 text-xs font-medium"><span>Feature link <span className="font-normal text-muted-foreground">(optional)</span></span><Input name="linked_feature_id" aria-label="Upload feature link" placeholder="Feature ID" defaultValue={uploadTarget?.linkedFeatureId} /></label>
            <label className="grid gap-1.5 text-xs font-medium"><span>Work/request link <span className="font-normal text-muted-foreground">(optional)</span></span><Input name="linked_request_id" aria-label="Upload work/request link" placeholder="Work or request ID" defaultValue={uploadTarget?.linkedRequestId} /></label>
            {mutationError ? <p role="alert">{mutationError}</p> : null}
            <DialogFooter>
              <Button disabled={busy} type="submit">{busy ? "Uploading…" : "Upload"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(selected)} onOpenChange={(open) => !open && setSelected(undefined)}>
        <DialogContent className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-2xl">
          {selected ? (
            <>
              <DialogHeader>
                <DialogTitle>{selected.document.title}</DialogTitle>
                <DialogDescription>{selected.document.documentId} · exact version {selected.document.version}</DialogDescription>
              </DialogHeader>
              <div className="grid gap-3">
                <dl className="grid grid-cols-2 gap-2 text-sm">
                  <dt>Type</dt><dd>{knowledgeLabel(selected.document.documentType)}</dd>
                  <dt>Authority</dt><dd>{knowledgeLabel(selected.document.authorityLevel)}</dd>
                  <dt>Tags</dt><dd>{selected.document.tags.join(", ") || "—"}</dd>
                  <dt>Notes</dt><dd>{selected.document.notes || "—"}</dd>
                  <dt>Feature link</dt><dd>{selected.document.linkedFeatureId || "—"}</dd>
                  <dt>Work/request link</dt><dd>{selected.document.linkedRequestId || "—"}</dd>
                  <dt>Version</dt><dd>{selected.document.version}</dd>
                  <dt>Created</dt><dd>{formatDateTime(selected.document.createdAt)}</dd>
                  <dt>Updated</dt><dd>{formatDateTime(selected.document.updatedAt)}</dd>
                  <dt>Source</dt><dd>{selected.document.originalFilename || selected.document.sourceUri || selected.document.sourceKind}</dd>
                  <dt>MIME type</dt><dd>{selected.document.mimeType || "—"}</dd>
                </dl>
                <p>Status: {knowledgeLabel(selected.document.status)}</p>
                {selected.document.errorMessage ? <p role="alert">{selected.document.errorMessage}</p> : null}
                <pre className="max-h-48 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3">
                  {selected.extractedPreview || "No extracted preview."}
                </pre>
                <h3 className="font-medium">Version history</h3>
                {selected.history.map((version) => (
                  <button type="button" key={version.version} className="text-left underline underline-offset-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50" onClick={() => void openDocument(version)}>
                    {version.version} · {knowledgeLabel(version.status)}
                  </button>
                ))}
              </div>
              <DialogFooter>
                <Button variant="outline" disabled={busy} onClick={() => setUploadTarget(selected.document)}>Upload new file version</Button>
                <Button variant="outline" disabled={busy} onClick={() => setCurateOpen(true)}>Curate links</Button>
                <Button variant="outline" disabled={selected.document.status !== "failed" || busy} onClick={() => void retrySelectedDocument()}>Retry ingestion</Button>
                <Button variant="destructive" disabled={busy} onClick={() => void deleteSelectedVersion()}>Delete exact version</Button>
              </DialogFooter>
              {mutationError ? <p role="alert">{mutationError}</p> : null}
            </>
          ) : null}
        </DialogContent>
      </Dialog>

      <Dialog open={curateOpen} onOpenChange={setCurateOpen}>
        <DialogContent>
          <form className="grid gap-3" onSubmit={submitCuration}>
            <DialogHeader>
              <DialogTitle>Curate document links</DialogTitle>
              <DialogDescription>
                {selected?.document.documentId} · source version {selected?.document.version}. This creates a new immutable version.
              </DialogDescription>
            </DialogHeader>
            <label className="grid gap-1.5 text-xs font-medium"><span>Feature link</span><Input name="linked_feature_id" aria-label="Feature link" defaultValue={selected?.document.linkedFeatureId} /></label>
            <label className="flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" name="clear_feature_link" />Clear feature link</label>
            <label className="grid gap-1.5 text-xs font-medium"><span>Work/request link</span><Input name="linked_request_id" aria-label="Work/request link" defaultValue={selected?.document.linkedRequestId} /></label>
            <label className="flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" name="clear_request_link" />Clear work/request link</label>
            <label className="grid gap-1.5 text-xs font-medium"><span>Notes <span className="font-normal text-muted-foreground">(optional)</span></span><Input name="notes" aria-label="Curation notes" placeholder="What changed" /></label>
            {mutationError ? <p role="alert">{mutationError}</p> : null}
            <DialogFooter>
              <Button type="submit" disabled={busy}>{busy ? "Saving…" : "Create curated version"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </section>
  )
}
