import { afterEach, describe, expect, it, vi } from "vitest"

import { curateKnowledgeLinks, deleteKnowledgeVersion, getKnowledgeDocument, listKnowledgeDocuments, retryKnowledgeDocument, uploadKnowledgeDocument } from "@/data/knowledge"

afterEach(() => vi.unstubAllGlobals())

const dto = { document_id: "doc-1", version: "v2", workspace_id: "ws-1", parent_version: "v1", is_latest: true, title: "Architecture", document_type: "design_reference", authority_level: "high", source_kind: "upload", status: "indexed", tags: ["platform"], created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-02T00:00:00Z" }

describe("knowledge adapter", () => {
  it("maps list and detail response envelopes", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ items: [dto], total: 1, embeddings_enabled: false }))).mockResolvedValueOnce(new Response(JSON.stringify({ document: dto, history: [dto], extracted_preview: "Preview" })))
    vi.stubGlobal("fetch", fetchMock)
    await expect(listKnowledgeDocuments("/registry", "ws-1")).resolves.toMatchObject({ total: 1, embeddingsEnabled: false, items: [{ documentId: "doc-1", parentVersion: "v1", documentType: "design_reference" }] })
    await expect(getKnowledgeDocument("/registry", "ws-1", "doc-1", "v2")).resolves.toMatchObject({ document: { documentId: "doc-1" }, extractedPreview: "Preview" })
    expect(fetchMock).toHaveBeenNthCalledWith(1, "/registry/documents?workspace_id=ws-1&limit=100", { signal: undefined })
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/registry/documents/doc-1?workspace_id=ws-1&version=v2", { signal: undefined })
  })

  it("uploads multipart metadata including new-version fields", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(dto), { status: 202 }))
    vi.stubGlobal("fetch", fetchMock)
    const file = new File(["body"], "architecture.md", { type: "text/markdown" })
    await uploadKnowledgeDocument("/registry", { workspaceId: "ws-1", file, title: "Architecture", documentType: "design_reference", authorityLevel: "high", uploadedBy: "tung", tags: ["platform", "governance"], notes: "Reviewed", documentId: "doc-1", parentVersion: "v1", newVersion: "v2" })
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(fetchMock.mock.calls[0][0]).toBe("/registry/documents/upload")
    expect(init.method).toBe("POST")
    const form = init.body as FormData
    expect(Object.fromEntries(form.entries())).toMatchObject({ workspace_id: "ws-1", title: "Architecture", document_type: "design_reference", authority_level: "high", uploaded_by: "tung", document_id: "doc-1", parent_version: "v1", new_version: "v2", notes: "Reviewed" })
    expect(form.getAll("tags")).toEqual(["platform", "governance"])
  })

  it("curates links and targets exact versions for retry and delete", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(dto), { status: 202 })).mockResolvedValueOnce(new Response(JSON.stringify(dto), { status: 202 })).mockResolvedValueOnce(new Response(JSON.stringify({ deleted: true })))
    vi.stubGlobal("fetch", fetchMock)
    await curateKnowledgeLinks("/registry", "doc-1", { workspaceId: "ws-1", version: "v2", linkedFeatureId: "feat-1", clearRequestLink: true, uploadedBy: "tung" })
    await retryKnowledgeDocument("/registry", "ws-1", "doc-1", "v2")
    await deleteKnowledgeVersion("/registry", "ws-1", "doc-1", "v2")
    expect(fetchMock).toHaveBeenNthCalledWith(1, "/registry/documents/doc-1/links", expect.objectContaining({ method: "POST", body: JSON.stringify({ workspace_id: "ws-1", version: "v2", linked_feature_id: "feat-1", clear_request_link: true, uploaded_by: "tung" }) }))
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/registry/documents/doc-1/retry?workspace_id=ws-1&version=v2", { method: "POST" })
    expect(fetchMock).toHaveBeenNthCalledWith(3, "/registry/documents/doc-1?workspace_id=ws-1&version=v2", { method: "DELETE" })
  })

  it("propagates HTTP failures", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("no", { status: 503 })))
    await expect(listKnowledgeDocuments("/registry", "ws-1")).rejects.toThrow("knowledge list request failed: 503")
  })

  it("rejects an empty workspace before any Knowledge request", async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)

    await expect(listKnowledgeDocuments("/registry", " ")).rejects.toThrow("workspaceId is required")
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
