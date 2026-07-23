import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { KnowledgePage } from "@/components/layout/knowledge"
import { defaultRegistryResponse, renderApp, seedLocalSession, sessionStorageKey } from "./app-test-support"

describe("Knowledge workspace", () => {
  const knowledgeDocument = (overrides: Record<string, unknown> = {}) => ({
    document_id: "doc-1", version: "v2", is_latest: true, title: "Architecture guide",
    document_type: "design_reference", authority_level: "high", source_kind: "upload",
    original_filename: "architecture.md", mime_type: "text/markdown", status: "indexed",
    tags: ["architecture", "approved"], notes: "Canonical decisions", linked_feature_id: "FEAT-1",
    linked_request_id: "WORK-1", created_at: "2026-07-01T00:00:00Z",
    updated_at: "2026-07-02T00:00:00Z", ...overrides,
  })

  function seedKnowledgeWorkspace() {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "/api/doc-registry")
    seedLocalSession()
    const session = JSON.parse(localStorage.getItem(sessionStorageKey) ?? "{}")
    session.profile.id = "workspace-main"
    localStorage.setItem(sessionStorageKey, JSON.stringify(session))
  }

  it("drops stale workspace responses and closes workspace-bound detail after a keyed switch", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const user = userEvent.setup()
    let resolveWorkspaceA!: (response: Response) => void
    const fetchMock = vi.fn((input: RequestInfo | URL, _init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/documents?") && (url.includes("workspace_id=workspace-a&") || url.includes("workspace_id=workspace-a-detail&"))) {
        return new Promise<Response>((resolve) => { resolveWorkspaceA = resolve })
      }
      if (url.includes("workspace_id=workspace-b")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [knowledgeDocument({ document_id: "doc-b", title: "Workspace B guide" })], embeddings_enabled: true })))
      }
      if (url.includes("/documents/doc-a")) {
        const document = knowledgeDocument({ document_id: "doc-a", title: "Workspace A guide" })
        return Promise.resolve(new Response(JSON.stringify({ document, history: [document] })))
      }
      return Promise.resolve(new Response(JSON.stringify({ items: [] })))
    })
    vi.stubGlobal("fetch", fetchMock)

    const view = render(
      <MemoryRouter>
        <KnowledgePage key="workspace-a" workspaceId="workspace-a" uploader="thanhtung" />
      </MemoryRouter>,
    )
    expect(screen.getByRole("button", { name: "Upload document" })).toBeDisabled()
    view.rerender(
      <MemoryRouter>
        <KnowledgePage key="workspace-b" workspaceId="workspace-b" uploader="thanhtung" />
      </MemoryRouter>,
    )
    expect(await screen.findByText("Workspace B guide")).toBeInTheDocument()
    await act(async () => {
      resolveWorkspaceA(new Response(JSON.stringify({ items: [knowledgeDocument({ document_id: "doc-a", title: "Workspace A guide" })], embeddings_enabled: true })))
      await Promise.resolve()
    })
    expect(screen.queryByText("Workspace A guide")).not.toBeInTheDocument()

    view.rerender(
      <MemoryRouter>
        <KnowledgePage key="workspace-a-detail" workspaceId="workspace-a-detail" uploader="thanhtung" />
      </MemoryRouter>,
    )
    await act(async () => {
      resolveWorkspaceA(new Response(JSON.stringify({ items: [knowledgeDocument({ document_id: "doc-a", title: "Workspace A guide" })], embeddings_enabled: true })))
      await Promise.resolve()
    })
    await user.click(await screen.findByRole("button", { name: "Workspace A guide" }))
    expect(await screen.findByRole("dialog", { name: "Workspace A guide" })).toBeInTheDocument()
    view.rerender(
      <MemoryRouter>
        <KnowledgePage key="workspace-b-detail" workspaceId="workspace-b" uploader="thanhtung" />
      </MemoryRouter>,
    )
    expect(screen.queryByRole("dialog", { name: "Workspace A guide" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Retry ingestion|Delete exact version|Curate links/ })).not.toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => /doc-a\/(retry|links)/.test(String(input)))).toBe(false)
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === "DELETE")).toBe(false)
  })

  it("renders loading, empty, and API error states and retries the list", async () => {
    seedKnowledgeWorkspace()
    let resolveFirst!: (response: Response) => void
    let listCalls = 0
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      if (!String(input).includes("/documents?")) return defaultRegistryResponse(input)
      listCalls += 1
      if (listCalls === 1) return new Promise<Response>((resolve) => { resolveFirst = resolve })
      if (listCalls === 2) return Promise.resolve(new Response("unavailable", { status: 503 }))
      return Promise.resolve(new Response(JSON.stringify({ items: [], total: 0, embeddings_enabled: true })))
    }))

    renderApp("/knowledge")
    expect(await screen.findByText("Loading Knowledge…")).toBeInTheDocument()
    resolveFirst(new Response("unavailable", { status: 503 }))
    expect(await screen.findByRole("alert")).toHaveTextContent("Doc Registry Knowledge is unavailable.")
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByText("Doc Registry Knowledge is unavailable.")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByText("No Knowledge documents match this view.")).toBeInTheDocument()
    expect(listCalls).toBe(3)
  })

  it("filters documents by search, type, and ingestion status on the client", async () => {
    const user = userEvent.setup()
    seedKnowledgeWorkspace()
    const documents = [
      knowledgeDocument(),
      knowledgeDocument({ document_id: "doc-2", title: "QA notes", document_type: "qa_finding", status: "failed" }),
    ]
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => String(input).includes("/documents?")
      ? Promise.resolve(new Response(JSON.stringify({ items: documents, total: 2, embeddings_enabled: true })))
      : defaultRegistryResponse(input)))
    renderApp("/knowledge")
    await waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/doc-registry/api/v1/workspaces", expect.any(Object)))
    expect(await screen.findByText("Architecture guide")).toBeInTheDocument()
    await user.type(screen.getByRole("textbox", { name: "Search knowledge" }), "doc-2")
    expect(screen.queryByText("Architecture guide")).not.toBeInTheDocument()
    expect(screen.getByText("QA notes")).toBeInTheDocument()
    await user.clear(screen.getByRole("textbox", { name: "Search knowledge" }))
    await user.selectOptions(screen.getByRole("combobox", { name: "Document type" }), "design_reference")
    expect(screen.getByText("Architecture guide")).toBeInTheDocument()
    expect(screen.queryByText("QA notes")).not.toBeInTheDocument()
    await user.selectOptions(screen.getByRole("combobox", { name: "Document type" }), "")
    await user.selectOptions(screen.getByRole("combobox", { name: "Ingestion status" }), "failed")
    expect(screen.getByText("QA notes")).toBeInTheDocument()
    expect(screen.queryByText("Architecture guide")).not.toBeInTheDocument()
  })

  it("shows complete detail metadata and navigates exact versions from history", async () => {
    const user = userEvent.setup()
    seedKnowledgeWorkspace()
    const latest = knowledgeDocument()
    const previous = knowledgeDocument({ version: "v1", is_latest: false, status: "failed", error_message: "Parse failed" })
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/documents?")) return Promise.resolve(new Response(JSON.stringify({ items: [latest], total: 1, embeddings_enabled: true })))
      if (url.includes("version=v1")) return Promise.resolve(new Response(JSON.stringify({ document: previous, history: [latest, previous], extracted_preview: "Older preview" })))
      if (url.includes("/documents/doc-1")) return Promise.resolve(new Response(JSON.stringify({ document: latest, history: [latest, previous], extracted_preview: "Current preview" })))
      return defaultRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/knowledge")
    await user.click(await screen.findByRole("button", { name: "Architecture guide" }))
    const dialog = await screen.findByRole("dialog", { name: "Architecture guide" })
    for (const value of ["Design reference", "High", "architecture, approved", "Canonical decisions", "FEAT-1", "WORK-1", "architecture.md", "text/markdown", "Current preview"]) {
      expect(within(dialog).getByText(value)).toBeInTheDocument()
    }
    await user.click(within(dialog).getByRole("button", { name: "v1 · Failed" }))
    expect(await within(dialog).findByText("Older preview")).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith("/api/doc-registry/documents/doc-1?workspace_id=workspace-main&version=v1", { signal: undefined })
  })

  it("stops polling when the selected version reaches failed", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    seedKnowledgeWorkspace()
    const pending = knowledgeDocument({ status: "parsing" })
    let detailCalls = 0
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/documents?")) return Promise.resolve(new Response(JSON.stringify({ items: [pending], total: 1, embeddings_enabled: true })))
      if (url.includes("/documents/doc-1")) { detailCalls += 1; const document = detailCalls === 1 ? pending : knowledgeDocument({ status: "failed", error_message: "Embedding failed" }); return Promise.resolve(new Response(JSON.stringify({ document, history: [document] }))) }
      return defaultRegistryResponse(input)
    }))
    renderApp("/knowledge")
    await act(async () => { await Promise.resolve() })
    fireEvent.click(await screen.findByRole("button", { name: "Architecture guide" }))
    await act(async () => { await Promise.resolve() })
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(await screen.findByText("Status: Failed")).toBeInTheDocument()
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(detailCalls).toBe(2)
    vi.useRealTimers()
  })

  it("cleans up polling on unmount and bounds non-terminal polling to twelve attempts", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    seedKnowledgeWorkspace()
    const pending = knowledgeDocument({ status: "parsing" })
    let detailCalls = 0
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/documents?")) return Promise.resolve(new Response(JSON.stringify({ items: [pending], total: 1, embeddings_enabled: true })))
      if (url.includes("/documents/doc-1")) { detailCalls += 1; return Promise.resolve(new Response(JSON.stringify({ document: pending, history: [pending] }))) }
      return defaultRegistryResponse(input)
    }))
    const first = renderApp("/knowledge")
    await act(async () => { await Promise.resolve() })
    fireEvent.click(await screen.findByRole("button", { name: "Architecture guide" }))
    await act(async () => { await Promise.resolve() })
    first.unmount()
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(detailCalls).toBe(1)

    renderApp("/knowledge")
    await act(async () => { await Promise.resolve() })
    fireEvent.click(await screen.findByRole("button", { name: "Architecture guide" }))
    await act(async () => { await Promise.resolve() })
    await act(async () => { await vi.advanceTimersByTimeAsync(20000) })
    expect(detailCalls).toBe(14)
    vi.useRealTimers()
  })

  it("prevents duplicate upload submission while the first request is busy", async () => {
    seedKnowledgeWorkspace()
    let resolveUpload!: (response: Response) => void
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/documents?")) return Promise.resolve(new Response(JSON.stringify({ items: [], total: 0, embeddings_enabled: true })))
      if (url.endsWith("/documents/upload")) return new Promise<Response>((resolve) => { resolveUpload = resolve })
      return defaultRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/knowledge")
    await screen.findByText("No Knowledge documents match this view.")
    fireEvent.click(await screen.findByRole("button", { name: "Upload document" }))
    const form = screen.getByRole("button", { name: "Upload" }).closest("form")!
    const fileInput = within(form).getByLabelText("Document file")
    fireEvent.change(fileInput, { target: { files: [new File(["doc"], "doc.md", { type: "text/markdown" })] } })
    fireEvent.change(within(form).getByLabelText("Document title"), { target: { value: "Guide" } })
    fireEvent.change(within(form).getByLabelText("Upload document type"), { target: { value: "supporting_doc" } })
    fireEvent.change(within(form).getByLabelText("Authority"), { target: { value: "reference" } })
    fireEvent.submit(form)
    fireEvent.submit(form)
    expect(fetchMock.mock.calls.filter(([input]) => String(input).endsWith("/documents/upload"))).toHaveLength(1)
    await act(async () => {
      resolveUpload(new Response(JSON.stringify(knowledgeDocument({ title: "Guide" })), { status: 202 }))
      await Promise.resolve()
    })
  })

  it("prevents duplicate retry clicks while the first request is busy", async () => {
    seedKnowledgeWorkspace()
    const failed = knowledgeDocument({ status: "failed", error_message: "Embedding failed" })
    let resolveRetry!: (response: Response) => void
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/documents?")) return Promise.resolve(new Response(JSON.stringify({ items: [failed], total: 1, embeddings_enabled: true })))
      if (url.includes("/retry?workspace_id=workspace-main&version=v2")) return new Promise<Response>((resolve) => { resolveRetry = resolve })
      if (url.includes("/documents/doc-1")) return Promise.resolve(new Response(JSON.stringify({ document: failed, history: [failed] })))
      return defaultRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/knowledge")
    fireEvent.click(await screen.findByRole("button", { name: "Architecture guide" }))
    const retry = await screen.findByRole("button", { name: "Retry ingestion" })
    act(() => {
      retry.click()
      retry.click()
    })
    expect(fetchMock.mock.calls.filter(([input]) => String(input).includes("/retry?workspace_id=workspace-main&version=v2"))).toHaveLength(1)
    await act(async () => {
      resolveRetry(new Response(JSON.stringify(knowledgeDocument({ status: "uploaded" })), { status: 202 }))
      await Promise.resolve()
    })
  })

  it("prevents duplicate delete clicks while the first request is busy", async () => {
    seedKnowledgeWorkspace()
    const document = knowledgeDocument()
    let resolveDelete!: (response: Response) => void
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/documents?")) return Promise.resolve(new Response(JSON.stringify({ items: [document], total: 1, embeddings_enabled: true })))
      if (init?.method === "DELETE") return new Promise<Response>((resolve) => { resolveDelete = resolve })
      if (url.includes("/documents/doc-1")) return Promise.resolve(new Response(JSON.stringify({ document, history: [document] })))
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    vi.spyOn(window, "confirm").mockReturnValue(true)
    renderApp("/knowledge")
    fireEvent.click(await screen.findByRole("button", { name: "Architecture guide" }))
    const remove = await screen.findByRole("button", { name: "Delete exact version" })
    act(() => {
      remove.click()
      remove.click()
    })
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === "DELETE")).toHaveLength(1)
    await act(async () => {
      resolveDelete(new Response(JSON.stringify({ deleted: true })))
      await Promise.resolve()
    })
  })

  it("renders top-level Knowledge navigation and registry documents", async () => {
    seedLocalSession()
    const session = JSON.parse(localStorage.getItem(sessionStorageKey) ?? "{}")
    session.profile.id = "workspace-main"
    localStorage.setItem(sessionStorageKey, JSON.stringify(session))
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/documents?")) return Promise.resolve(new Response(JSON.stringify({ items: [{ document_id: "doc-1", version: "v1", is_latest: true, title: "Architecture guide", document_type: "design_reference", authority_level: "high", source_kind: "upload", status: "indexed", created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-02T00:00:00Z" }], total: 1, embeddings_enabled: true })))
      return defaultRegistryResponse(input)
    }))
    renderApp("/knowledge")
    expect(await screen.findByRole("link", { name: "Knowledge" })).toBeInTheDocument()
    expect(await screen.findByText("Architecture guide")).toBeInTheDocument()
    expect(screen.getAllByRole("heading", { level: 1, name: "Knowledge" })).toHaveLength(1)
    expect(screen.getByRole("heading", { level: 2, name: "Knowledge library" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Upload document" })).toBeInTheDocument()
    expect(screen.getByRole("table", { name: "Knowledge documents" })).toBeInTheDocument()
  })

  it("disables upload and links to Models when embeddings are unavailable", async () => {
    seedLocalSession()
    const session = JSON.parse(localStorage.getItem(sessionStorageKey) ?? "{}")
    session.profile.id = "workspace-main"
    localStorage.setItem(sessionStorageKey, JSON.stringify(session))
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => String(input).includes("/documents?")
      ? Promise.resolve(new Response(JSON.stringify({ items: [], total: 0, embeddings_enabled: false })))
      : defaultRegistryResponse(input)))
    renderApp("/knowledge")
    expect(await screen.findByText(/Embeddings are disabled\./)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Upload document" })).toBeDisabled()
    expect(screen.getByRole("link", { name: "Settings → Models" })).toHaveAttribute("href", "/knowledge?settings=models")
  })

  it("manages exact document versions through direct registry mutations", async () => {
    const user = userEvent.setup()
    seedLocalSession()
    const session = JSON.parse(localStorage.getItem(sessionStorageKey) ?? "{}")
    session.profile.id = "workspace-main"
    localStorage.setItem(sessionStorageKey, JSON.stringify(session))
    const document = { document_id: "doc-1", version: "v1", is_latest: true, title: "Architecture guide", document_type: "design_reference", authority_level: "high", source_kind: "upload", status: "failed", error_message: "Embedding failed", created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-02T00:00:00Z" }
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/documents?")) return Promise.resolve(new Response(JSON.stringify({ items: [document], total: 1, embeddings_enabled: true })))
      if (url.includes("/documents/doc-1") && !init?.method) return Promise.resolve(new Response(JSON.stringify({ document, history: [document], extracted_preview: "Preview" })))
      if (url.endsWith("/documents/upload")) return Promise.resolve(new Response(JSON.stringify({ ...document, version: "v2", parent_version: "v1", status: "uploaded" }), { status: 202 }))
      if (url.endsWith("/documents/doc-1/links")) return Promise.resolve(new Response(JSON.stringify({ ...document, version: "v2", parent_version: "v1", status: "uploaded" }), { status: 202 }))
      if (url.includes("/retry?workspace_id=workspace-main&version=v1")) return Promise.resolve(new Response(JSON.stringify({ ...document, status: "uploaded" }), { status: 202 }))
      if (init?.method === "DELETE") return Promise.resolve(new Response(JSON.stringify({ deleted: true })))
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    vi.spyOn(window, "confirm").mockReturnValue(true)
    renderApp("/knowledge")
    await user.click(await screen.findByRole("button", { name: "Architecture guide" }))
    await user.click(screen.getByRole("button", { name: "Upload new file version" }))
    expect(screen.getByText("doc-1 · parent v1")).toBeInTheDocument()
    await user.type(screen.getByRole("textbox", { name: "New version" }), "v2")
    const fileInput = globalThis.document.querySelector<HTMLInputElement>('input[name="file"]')!
    await user.upload(fileInput, new File(["updated"], "architecture.md", { type: "text/markdown" }))
    fireEvent.submit(fileInput.form!)
    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith("/documents/upload"))
      expect(call).toBeTruthy()
      const body = call![1]!.body as FormData
      expect(body.get("document_id")).toBe("doc-1")
      expect(body.get("parent_version")).toBe("v1")
      expect(body.get("new_version")).toBe("v2")
    })
    await user.click(screen.getByRole("button", { name: "Curate links" }))
    await user.type(screen.getByRole("textbox", { name: "Feature link" }), "FEAT-1")
    await user.click(screen.getByLabelText("Clear work/request link"))
    await user.type(screen.getByRole("textbox", { name: "Curation notes" }), "Align links")
    await user.click(screen.getByRole("button", { name: "Create curated version" }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/doc-registry/documents/doc-1/links", expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ workspace_id: "workspace-main", version: "v1", linked_feature_id: "FEAT-1", clear_feature_link: false, clear_request_link: true, uploaded_by: "thanhtung", notes: "Align links" }),
    })))
    await user.click(screen.getByRole("button", { name: "Retry ingestion" }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/doc-registry/documents/doc-1/retry?workspace_id=workspace-main&version=v1", { method: "POST" }))
    await user.click(screen.getByRole("button", { name: "Delete exact version" }))
    expect(window.confirm).toHaveBeenCalledWith("Delete doc-1 exact version v1?")
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/doc-registry/documents/doc-1?workspace_id=workspace-main&version=v1", { method: "DELETE" }))
  })

  it("polls a selected non-terminal version until indexed, then refreshes the list", async () => {
    const user = userEvent.setup()
    seedLocalSession()
    const session = JSON.parse(localStorage.getItem(sessionStorageKey) ?? "{}")
    session.profile.id = "workspace-main"
    localStorage.setItem(sessionStorageKey, JSON.stringify(session))
    const pending = { document_id: "doc-poll", version: "v1", is_latest: true, title: "Polling guide", document_type: "supporting_doc", authority_level: "reference", source_kind: "upload", status: "parsing", created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-02T00:00:00Z" }
    let detailCalls = 0
    let listCalls = 0
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("/documents?")) { listCalls += 1; return Promise.resolve(new Response(JSON.stringify({ items: [pending], total: 1, embeddings_enabled: true }))) }
      if (url.includes("/documents/doc-poll")) { detailCalls += 1; const current = detailCalls === 1 ? pending : { ...pending, status: "indexed" }; return Promise.resolve(new Response(JSON.stringify({ document: current, history: [current] }))) }
      return defaultRegistryResponse(input)
    }))
    renderApp("/knowledge")
    await user.click(await screen.findByRole("button", { name: "Polling guide" }))
    expect(await screen.findByText("Status: Parsing")).toBeInTheDocument()
    expect(await screen.findByText("Status: Indexed", {}, { timeout: 2500 })).toBeInTheDocument()
    await waitFor(() => expect(listCalls).toBeGreaterThan(1))
  })
})
