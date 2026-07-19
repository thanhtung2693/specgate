import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useNavigate } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { GovernanceAgent } from "@/components/agent/governance-agent"
import { TooltipProvider } from "@/components/ui/tooltip"

import App from "./App"
import { KnowledgePage } from "@/components/layout/knowledge"

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

const langGraphClientMock = vi.hoisted(() => ({
  create: vi.fn(),
  delete: vi.fn(),
  get: vi.fn(),
  getState: vi.fn(),
  search: vi.fn(),
  update: vi.fn(),
}))

const sessionStorageKey = "specgate.ui.session.v2"

function seedLocalSession() {
  localStorage.setItem(
    sessionStorageKey,
    JSON.stringify({
      profile: {
        id: "workspace-main",
        name: "SpecGate Core",
        user: {
          id: "user-local",
          username: "thanhtung",
          name: "Tung Local",
          email: "tung@example.com",
        },
      },
    }),
  )
}

vi.mock("@langchain/langgraph-sdk", () => ({
  Client: vi.fn(function Client() {
    return {
      threads: langGraphClientMock,
    }
  }),
}))

function renderApp(path = "/work") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  )
}

const registryWorkItems = [
  {
    id: "SG-142",
    key: "SG-142",
    work_type: "cleanup",
    title: "Simplify CLI command surface",
    intent_md: "Reduce command sprawl while keeping governed handoff behavior explicit.",
    created_by: "Product",
    created_at: "2026-06-27T05:00:00Z",
    updated_at: "2026-06-27T05:12:00Z",
  },
  {
    id: "SG-136",
    key: "SG-136",
    work_type: "new_feature",
    title: "Workspace and user onboarding review",
    intent_md: "Clarify workspace/user setup, docs, and username-dependent surfaces.",
    created_by: "DX",
    created_at: "2026-06-27T04:00:00Z",
    updated_at: "2026-06-27T05:00:00Z",
  },
  {
    id: "SG-151",
    key: "SG-151",
    work_type: "new_feature",
    title: "Agent skills setup primitives",
    intent_md: "Define small setup skills for project onboarding and plugin refresh.",
    phase: "Ready",
    created_by: "Governance",
    created_at: "2026-06-27T04:20:00Z",
    updated_at: "2026-06-27T05:10:00Z",
  },
  {
    id: "SG-155",
    key: "SG-155",
    work_type: "cleanup",
    title: "Doc Registry migration cleanup",
    intent_md: "Ready for delivery review with Context Pack.",
    phase: "Review",
    created_by: "Backend",
    created_at: "2026-06-27T04:30:00Z",
    updated_at: "2026-06-27T05:20:00Z",
  },
  {
    id: "SG-153",
    key: "SG-153",
    work_type: "new_feature",
    title: "Delivery evidence trust stamp",
    intent_md: "Expose trustworthy evidence state for delivery review and audits.",
    created_by: "Platform",
    created_at: "2026-06-27T03:30:00Z",
    updated_at: "2026-06-27T04:55:00Z",
  },
  {
    id: "SG-147",
    key: "SG-147",
    work_type: "cleanup",
    title: "Pre-release verification sweep",
    intent_md: "Complete pre-release verification evidence.",
    phase: "Review",
    created_by: "QA",
    created_at: "2026-06-27T03:00:00Z",
    updated_at: "2026-06-27T04:45:00Z",
  },
]

// Server-derived delivered phase: explicit human delivery acceptance. Kept out
// of the default fixture so existing queue counts stay stable.
const deliveredChangeRequest = {
  id: "SG-160",
  key: "SG-160",
  work_type: "cleanup",
  title: "Delivered settings polish",
  intent_md: "Shipped through delivery review.",
  phase: "delivered",
  created_by: "Platform",
  created_at: "2026-06-27T02:00:00Z",
  updated_at: "2026-06-27T05:40:00Z",
}

function deliveredRegistryResponse(input: RequestInfo | URL, init?: RequestInit) {
  const url = String(input)
  if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
    return Promise.resolve(
      new Response(JSON.stringify({ items: [...registryWorkItems, deliveredChangeRequest] }), {
        headers: { "Content-Type": "application/json" },
      }),
    )
  }
  return defaultRegistryResponse(input, init)
}

function defaultRegistryResponse(input: RequestInfo | URL, init?: RequestInit) {
  const url = fixtureURL(input)
  if (url.endsWith("/api/v1/workspaces")) {
    return Promise.resolve(
      new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core", is_default: true }] }), {
        headers: { "Content-Type": "application/json" },
      }),
    )
  }
  if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
    return Promise.resolve(new Response(JSON.stringify({ items: registryWorkItems }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/governance/feedback-events?status=received&limit=20")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            {
              id: "feedback-SG-147-browser-smoke",
              event_type: "coding_agent.blocked_ambiguity",
              status: "received",
              reason: "Browser smoke evidence is required before delivery review can pass.",
              change_request_id: "SG-147",
              artifact_id: "artifact-SG-147",
              created_at: "2026-06-27T04:51:00Z",
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/workboard/features")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            {
              id: "SG-155",
              key: "SG-155",
              name: "Doc Registry migration cleanup",
              status: "approved",
              version: 1,
              canonical_artifact_id: "artifact-SG-155",
            },
            {
              id: "SG-151",
              key: "SG-151",
              name: "Agent skills setup primitives",
              status: "approved",
              version: 1,
              canonical_artifact_id: "artifact-SG-151",
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/workboard/features/SG-155")) {
    return Promise.resolve(new Response(JSON.stringify({ id: "SG-155", key: "SG-155", name: "Doc Registry migration cleanup", canonical_artifact_id: "artifact-SG-155" }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/workboard/features/SG-151")) {
    return Promise.resolve(new Response(JSON.stringify({ id: "SG-151", key: "SG-151", name: "Agent skills setup primitives", canonical_artifact_id: "artifact-SG-151" }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/api/v1/work-items/SG-155/context-pack")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          state: "assembled",
          markdown: "# Context Pack\n\nRun `specgate work context SG-155` before implementation.",
          change_request_id: "SG-155",
          feature_id: "SG-155",
          governance_level: "standard",
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/artifacts?limit=50")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            {
              id: "artifact-SG-155",
              feature_id: "SG-155",
              feature_name: "Doc Registry migration cleanup",
              version: "v0.1",
              status: "approved",
              request_type: "change_request",
              impact_level: "low",
              artifact_completeness: "full",
              source_kind: "context_pack",
              created_by: "governance",
              updated_at: "2026-06-27T05:20:00Z",
              expected_gates: ["spec_completeness"],
            },
            {
              id: "artifact-SG-151",
              feature_id: "SG-151",
              feature_name: "Agent skills setup primitives",
              version: "v0.1",
              status: "approved",
              request_type: "change_request",
              impact_level: "low",
              artifact_completeness: "full",
              source_kind: "context_pack",
              created_by: "governance",
              updated_at: "2026-06-27T05:10:00Z",
              expected_gates: ["spec_completeness"],
            },
            {
              id: "artifact-quick-standalone",
              feature_id: "",
              version: "v0.20260703140824",
              status: "approved",
              request_type: "change_request",
              impact_level: "medium",
              artifact_completeness: "full",
              source_kind: "context_pack",
              created_by: "governance",
              updated_at: "2026-06-27T05:22:00Z",
              expected_gates: ["delivery_review"],
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/artifacts?feature_id=SG-155&limit=100")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            {
              id: "artifact-SG-155",
              feature_id: "SG-155",
              feature_name: "Doc Registry migration cleanup",
              version: "v0.1",
              status: "approved",
              request_type: "change_request",
              impact_level: "low",
              artifact_completeness: "full",
              source_kind: "context_pack",
              updated_at: "2026-06-27T05:20:00Z",
            },
            {
              id: "artifact-SG-155-previous",
              feature_id: "SG-155",
              feature_name: "Doc Registry migration cleanup",
              version: "v0.0",
              status: "superseded",
              request_type: "change_request",
              impact_level: "low",
              artifact_completeness: "full",
              source_kind: "context_pack",
              updated_at: "2026-06-27T04:20:00Z",
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/artifacts?feature_id=SG-151&limit=100")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            {
              id: "artifact-SG-151",
              feature_id: "SG-151",
              feature_name: "Agent skills setup primitives",
              version: "v0.1",
              status: "approved",
              request_type: "change_request",
              impact_level: "low",
              artifact_completeness: "full",
              source_kind: "context_pack",
              updated_at: "2026-06-27T05:10:00Z",
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/artifacts/artifact-SG-155/files")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            { path: "migration/spec.md", role: "spec", size_bytes: 3900, updated_at: "2026-06-27T05:20:00Z" },
            { path: "migration/tasks.md", role: "tasks", size_bytes: 1600, updated_at: "2026-06-27T05:20:00Z" },
            { path: "migration/verification.md", role: "verification", size_bytes: 1200, updated_at: "2026-06-27T05:20:00Z" },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/artifacts/artifact-SG-151/files")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            { path: "setup/spec.md", role: "spec", size_bytes: 4200, updated_at: "2026-06-27T05:10:00Z" },
            { path: "setup/tasks.md", role: "tasks", size_bytes: 1800, updated_at: "2026-06-27T05:10:00Z" },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.includes("/artifacts/artifact-SG-155/files/_?path=migration%2Fspec.md")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          content:
            "# Doc Registry migration cleanup\n\nThis spec keeps migration behavior deterministic while reducing noisy expected misses.\n\n```mermaid\nsequenceDiagram\n  participant CI\n  participant DB\n  CI->>DB: Apply migrations\n  DB-->>CI: Schema ready\n```\n",
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.includes("/artifacts/artifact-SG-155-previous/files/_?path=migration%2Fspec.md")) {
    return Promise.resolve(new Response(JSON.stringify({ content: "# Doc Registry migration cleanup\n\nPrevious version reduced expected migration misses.\n" }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.includes("/artifacts/artifact-SG-155/files/_?path=migration%2Ftasks.md")) {
    return Promise.resolve(new Response(JSON.stringify({ content: "# Doc Registry migration cleanup Tasks\n\n- Report completion through the governed delivery loop.\n" }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/features/SG-155/attachments")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [{ id: "attachment-SG-155", kind: "link", title: "Migration rollout note", note: "Reference used by quality-gate review.", audience: "gate", created_by: "governance", created_at: "2026-06-27T05:00:00Z" }],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/governance/feedback-events?artifact_id=artifact-SG-155&limit=20")) {
    return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "feedback-SG-155", artifact_id: "artifact-SG-155", event_type: "coding_agent.completed", status: "received", reason: "Completion evidence is ready for delivery review.", created_at: "2026-06-27T05:00:00Z" }] }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/artifacts/artifact-SG-155/readiness-runs?limit=20")) {
    return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "readiness-SG-155", gate: "spec_completeness", state: "warn", hint: "missing constraints", created_at: "2026-06-27T05:00:00Z" }] }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/events?artifact_id=artifact-SG-155&limit=20")) {
    return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "event-SG-155", artifact_id: "artifact-SG-155", event_type: "artifact.published", payload: { version: "v0.1" }, created_at: "2026-06-27T05:00:00Z" }] }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/api/v1/artifacts/artifact-SG-155/gate-preview")) {
    return Promise.resolve(new Response(JSON.stringify({ preview_tasks: [{ gate_key: "spec_completeness", note: "Expected gate from artifact snapshot." }] }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/api/v1/artifacts/artifact-SG-155/policy")) {
    return Promise.resolve(new Response(JSON.stringify({ governance_level: "standard", title: "Standard governance", summary: "Policy snapshot.", reasons: [], obligations: [] }), { headers: { "Content-Type": "application/json" } }))
  }
  return Promise.resolve(new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } }))
}

function fixtureURL(input: RequestInfo | URL) {
  const request = new URL(String(input), "http://registry.test")
  request.searchParams.delete("workspace_id")
  return request.toString()
}

function emptyRegistryResponse(input: RequestInfo | URL) {
  if (fixtureURL(input).endsWith("/api/v1/workspaces")) {
    return defaultRegistryResponse(input)
  }
  return Promise.resolve(new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } }))
}

async function openSettings(path = "/work") {
  renderApp(path)
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "Settings" }))
  return {
    user,
    settingsDialog: await screen.findByRole("dialog", { name: "Settings" }),
  }
}

describe("SpecGate UI shell", () => {
  beforeEach(() => {
    localStorage.clear()
    seedLocalSession()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "")
    vi.stubGlobal("fetch", vi.fn(defaultRegistryResponse))
    langGraphClientMock.create.mockResolvedValue({
      thread_id: "thread-created",
      metadata: { source: "specgate-ui", surface: "governance-agent", title: "Governance thread" },
      updated_at: "2026-06-30T00:00:00Z",
    })
    langGraphClientMock.delete.mockResolvedValue(undefined)
    langGraphClientMock.get.mockResolvedValue({
      thread_id: "thread-delivery",
      metadata: { source: "specgate-ui", surface: "governance-agent", title: "Delivery review" },
      updated_at: "2026-06-30T00:00:00Z",
    })
    langGraphClientMock.getState.mockResolvedValue({ values: { messages: [] } })
    langGraphClientMock.search.mockResolvedValue([])
    langGraphClientMock.update.mockResolvedValue(undefined)
  })

  afterEach(() => {
    localStorage.clear()
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
    vi.clearAllMocks()
  })

  it("requires Full mode when the Doc Registry is not configured", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
    localStorage.clear()

    renderApp("/work")

    expect(await screen.findByRole("heading", { name: "Web UI requires Full mode" })).toBeInTheDocument()
    expect(screen.getByText("specgate init --mode full")).toBeInTheDocument()
    expect(screen.getByText(/Local mode stays in the CLI and IDE/)).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Set up attribution" })).not.toBeInTheDocument()
    expect(localStorage.getItem(sessionStorageKey)).toBeNull()
  })

  it("renders Work as the primary app surface", async () => {
    renderApp()
    const user = userEvent.setup()

    expect(screen.getByRole("heading", { name: "Work" })).toBeInTheDocument()
    expect(screen.getByText("Governance Board")).toBeInTheDocument()
    expect(screen.getByText("Work queue")).toBeInTheDocument()
    expect(screen.getByRole("table", { name: "Work queue" })).toBeInTheDocument()
    expect(screen.getByLabelText("Search work")).toBeInTheDocument()
    expect(screen.getByText("Action queue")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Board" })).toHaveAttribute("aria-pressed", "false")
    expect(screen.getByRole("button", { name: "List" })).toHaveAttribute("aria-pressed", "true")
    expect(screen.getByText("SpecGate Core")).toBeInTheDocument()

    await waitFor(() => {
      const enabledTrigger = screen
        .getAllByRole("button", { name: "Open governance agent" })
        .find((button) => !button.hasAttribute("disabled"))
      expect(enabledTrigger).toBeTruthy()
    })
    const agentTrigger = screen
      .getAllByRole("button", { name: "Open governance agent" })
      .find((button) => !button.hasAttribute("disabled"))

    await user.click(agentTrigger!)

    expect(await screen.findByRole("heading", { name: "Governance agent" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "View agent threads" })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "View agent threads" }))
    expect(screen.getAllByText("Threads").length).toBeGreaterThan(0)
    expect(screen.getByRole("button", { name: "New agent thread" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Back to chat" })).toBeInTheDocument()
    expect(screen.queryByText(/LangGraph/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Local conversations/)).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Back to chat" }))
    const composer = screen.getByPlaceholderText("Ask about gate failures, blockers, or artifacts. Type / for commands")
    expect(composer).toBeInTheDocument()

    await user.type(composer, "@")
    expect(screen.queryByText("Work items")).not.toBeInTheDocument()

    await user.clear(composer)
    await user.type(composer, "/")
    expect(await screen.findByText("Prepare handoff")).toBeInTheDocument()
    expect(screen.getByText("Evidence summary")).toBeInTheDocument()
  })

  it("requires an active workspace before opening the LangGraph governance agent", async () => {
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "http://agents.test")

    render(
      <TooltipProvider>
        <GovernanceAgent />
      </TooltipProvider>,
    )

    expect(screen.getByRole("heading", { name: "Select a workspace" })).toBeInTheDocument()
    expect(screen.getByText("Choose a workspace before starting a governed conversation.")).toBeInTheDocument()
    expect(screen.queryByPlaceholderText("Ask about gate failures, blockers, or artifacts. Type / for commands")).not.toBeInTheDocument()
    expect(langGraphClientMock.search).not.toHaveBeenCalled()
  })

  it("sends governance-agent messages with Enter", async () => {
    renderApp("/work")
    const user = userEvent.setup()

    const agentTrigger = await screen.findAllByRole("button", { name: "Open governance agent" }).then((buttons) =>
      buttons.find((button) => !button.hasAttribute("disabled")),
    )
    await user.click(agentTrigger!)

    const composer = await screen.findByPlaceholderText("Ask about gate failures, blockers, or artifacts. Type / for commands")
    await user.type(composer, "Check delivery gates{Enter}")

    expect(await screen.findByText(/For this prompt, I would inspect: "Check delivery gates"/)).toBeInTheDocument()
  })

  it("manages governance-agent threads with search, rename, archive, and delete", async () => {
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "http://agents.test")
    const threadFixtures = [
      {
        thread_id: "thread-delivery",
        metadata: { source: "specgate-ui", surface: "governance-agent", workspace_id: "ws-a", title: "Delivery review" },
        updated_at: "2026-06-30T00:00:00Z",
      },
      {
        thread_id: "thread-cleanup",
        metadata: { source: "specgate-ui", surface: "governance-agent", workspace_id: "ws-a", title: "Draft cleanup" },
        updated_at: "2026-06-29T00:00:00Z",
      },
      {
        thread_id: "thread-archived",
        metadata: { source: "specgate-ui", surface: "governance-agent", workspace_id: "ws-a", title: "Archived delivery", archived: true },
        updated_at: "2026-06-28T00:00:00Z",
      },
    ]
    langGraphClientMock.search.mockImplementation(async () => threadFixtures)
    langGraphClientMock.get.mockImplementation(async (threadId: string) => {
      const thread = threadFixtures.find((item) => item.thread_id === threadId)
      if (!thread) throw new Error(`missing thread ${threadId}`)
      return thread
    })
    langGraphClientMock.update.mockImplementation(async (threadId: string, input: { metadata?: Record<string, unknown> }) => {
      const thread = threadFixtures.find((item) => item.thread_id === threadId)
      if (thread && input.metadata) {
        thread.metadata = { ...thread.metadata, ...input.metadata }
      }
    })
    render(
      <TooltipProvider>
        <GovernanceAgent workspaceId="ws-a" />
      </TooltipProvider>,
    )
    const user = userEvent.setup()

    expect(await screen.findByRole("heading", { name: "Governance agent" })).toBeInTheDocument()
    await user.click(await screen.findByRole("button", { name: "View agent threads" }))

    const search = await screen.findByRole("searchbox", { name: "Search agent threads" })
    await user.type(search, "delivery")

    expect(await screen.findByText("Delivery review")).toBeInTheDocument()
    expect(screen.queryByText("Draft cleanup")).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Rename thread Delivery review" }))
    const titleInput = screen.getByRole("textbox", { name: "Thread title" })
    await user.clear(titleInput)
    await user.type(titleInput, "Release gates")
    await user.click(screen.getByRole("button", { name: "Save thread title" }))

    expect(langGraphClientMock.update).toHaveBeenCalledWith(
      "thread-delivery",
      expect.objectContaining({
        metadata: expect.objectContaining({ title: "Release gates" }),
        returnMinimal: true,
      }),
    )

    await user.clear(search)
    await user.click(screen.getByRole("button", { name: /Archive thread (Delivery review|Release gates)/ }))
    expect(langGraphClientMock.update).toHaveBeenCalledWith(
      "thread-delivery",
      expect.objectContaining({
        metadata: expect.objectContaining({ archived: true }),
        returnMinimal: true,
      }),
    )

    await user.click(screen.getByRole("button", { name: "Show archived threads" }))
    expect(await screen.findByText("Archived delivery")).toBeInTheDocument()
    expect(screen.queryByText("Draft cleanup")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Restore thread Archived delivery" }))
    expect(langGraphClientMock.update).toHaveBeenCalledWith(
      "thread-archived",
      expect.objectContaining({
        metadata: expect.objectContaining({ archived: false }),
        returnMinimal: true,
      }),
    )
    expect(await screen.findByText("Archived delivery")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Archive thread Archived delivery" })).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Delete thread Draft cleanup" }))
    await user.click(screen.getByRole("button", { name: "Confirm delete Draft cleanup" }))

    expect(langGraphClientMock.delete).toHaveBeenCalledWith("thread-cleanup")
  })

  it("does not fetch or render legacy governance thread artifact links", async () => {
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "http://agents.test")
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const threadFixtures = [
      {
        thread_id: "thread-delivery",
        metadata: { source: "specgate-ui", surface: "governance-agent", workspace_id: "ws-a", title: "Delivery review" },
        updated_at: "2026-06-30T00:00:00Z",
      },
    ]
    langGraphClientMock.search.mockImplementation(async () => threadFixtures)
    langGraphClientMock.get.mockImplementation(async () => threadFixtures[0])
    langGraphClientMock.getState.mockResolvedValue({ values: { messages: [] } })
    const fetchMock = vi.fn((input: RequestInfo | URL, _init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/governance/threads/thread-delivery/artifacts") {
        return Promise.reject(new Error("legacy thread artifact endpoint should not be called"))
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    render(
      <TooltipProvider>
        <GovernanceAgent workspaceId="ws-a" />
      </TooltipProvider>,
    )
    const user = userEvent.setup()

    await user.click(await screen.findByRole("button", { name: "View agent threads" }))
    await user.click(await screen.findByText("Delivery review"))

    await waitFor(() => expect(screen.getByPlaceholderText("Ask about gate failures, blockers, or artifacts. Type / for commands")).toBeInTheDocument())
    expect(screen.queryByText("Thread artifacts")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/governance/threads/thread-delivery/artifacts", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "")
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("switches work views and filters the list queue", async () => {
    renderApp("/work")
    const user = userEvent.setup()

    expect(screen.getByText("Work queue")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Board" }))
    expect(screen.getByText("Lifecycle lanes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "List" }))

    await user.type(screen.getByLabelText("Search work"), "verification")

    expect(screen.getAllByText("Pre-release verification sweep").length).toBeGreaterThan(0)
    expect(screen.queryByText("Doc Registry migration cleanup")).not.toBeInTheDocument()

    await user.clear(screen.getByLabelText("Search work"))
    await user.click(screen.getByRole("button", { name: /^Blocked\d+$/ }))

    expect(screen.getAllByText("Pre-release verification sweep").length).toBeGreaterThan(0)
    expect(screen.getByText("Delivery evidence trust stamp")).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: /Ready for pickup/ }))
    expect(screen.queryByRole("button", { name: /Gate failures/ })).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: /Action queue/ }))
    expect(screen.getByRole("button", { name: /Reason: All reasons/ })).toBeInTheDocument()
    await user.click(screen.getByRole("link", { name: "Agent skills setup primitives" }))

    expect(screen.getAllByRole("heading", { name: "Agent skills setup primitives" }).length).toBeGreaterThan(0)
  })

  it("fetches governance stats when the disclosure expands and links ledger rows to verification", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/api/v1/stats")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              window_days: 30,
              reviewed_items: 5,
              first_pass: 4,
              gate_catches_pre_build: 3,
              review_catches_post_build: 2,
              review_catches_fixed: 1,
              rework: 2,
              items_with_rework: 1,
              ambiguity_blocks: 1,
              cycle_time_avg_hours: 6.4,
              cycle_time_items: 4,
              ledger: [
                {
                  occurred_at: "2026-06-27T05:00:00Z",
                  change_request_key: "SG-142",
                  kind: "gate_catch",
                  gate: "spec_completeness",
                  detail: "Acceptance criteria are missing measurable outcomes.",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("button", { name: "Governance stats · last 30 days" }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/api/v1/stats?workspace_id=workspace-main&days=30",
        expect.any(Object),
      ),
    )
    const statsRegion = screen.getByRole("region", { name: "Governance stats" })
    expect(await within(statsRegion).findByText("80%")).toBeInTheDocument()
    expect(within(statsRegion).getByText("First-pass yield")).toBeInTheDocument()
    const ledgerLink = within(statsRegion).getByRole("link", { name: "SG-142" })
    expect(ledgerLink).toHaveAttribute("href", "/work/SG-142?tab=verification")
    expect(within(statsRegion).getByText("Gate signals (pre-build)")).toBeInTheDocument()
    expect(within(statsRegion).getByText("Review signals (post-build)")).toBeInTheDocument()
    expect(within(statsRegion).getByText("Signal ledger")).toBeInTheDocument()
    expect(within(statsRegion).getByText("Gate signal")).toBeInTheDocument()
    expect(within(statsRegion).queryByText("Gate Catch")).not.toBeInTheDocument()
    expect(within(statsRegion).getByText("Acceptance criteria are missing measurable outcomes.")).toBeInTheDocument()
  })

  it("shows the not-enough-data line instead of percentages when nothing was reviewed", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/api/v1/stats")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({ window_days: 30, reviewed_items: 0, first_pass: 0, ledger: [] }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("button", { name: "Governance stats · last 30 days" }))

    expect(await screen.findByText("Not enough data yet — run a few governed work items first.")).toBeInTheDocument()
    const statsRegion = screen.getByRole("region", { name: "Governance stats" })
    expect(within(statsRegion).queryByText(/%/)).not.toBeInTheDocument()
    expect(within(statsRegion).queryByText("First-pass yield")).not.toBeInTheDocument()
  })

  it("does not fetch governance stats before the disclosure is expanded", async () => {
    const fetchMock = vi.fn(defaultRegistryResponse)
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")

    expect(await screen.findByRole("button", { name: "Governance stats · last 30 days" })).toBeInTheDocument()
    expect(await screen.findByText("Work queue")).toBeInTheDocument()
    expect(fetchMock.mock.calls.filter(([input]) => String(input).includes("/api/v1/stats"))).toHaveLength(0)
  })

  it("keeps work creation outside the UI workspace", () => {
    renderApp("/work")

    expect(screen.queryByRole("button", { name: "Create work" })).not.toBeInTheDocument()
    expect(screen.queryByRole("dialog", { name: "Create work" })).not.toBeInTheDocument()
  })

  it("exposes the primary navigation structure", async () => {
    renderApp("/reviews")

    expect(screen.getByRole("link", { name: /^Work$/ })).toHaveAttribute("href", "/work")
    expect(screen.getByRole("link", { name: /^Reviews$/ })).toHaveAttribute("href", "/reviews")
    expect(screen.getByRole("link", { name: /^Artifacts$/ })).toHaveAttribute("href", "/artifacts")
    expect(screen.getByRole("button", { name: "Settings" })).toBeInTheDocument()
    expect(await screen.findByText(/items need review/)).toBeInTheDocument()
    expect(screen.getByText("Delivery evidence")).toBeInTheDocument()
    expect(screen.getByLabelText("Search reviews")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /Needs changes/ })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /Gate failed/ })).toBeInTheDocument()
    expect(
      screen.getAllByRole("link", { name: /Inspect review/ }).some((link) => {
        const href = link.getAttribute("href") ?? ""
        return href.startsWith("/work/") && href.includes("tab=verification")
      }),
    ).toBe(true)
    expect(screen.queryByText("Feedback signals")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Ask about review gaps|Ask review summary/ })).not.toBeInTheDocument()
    expect(screen.queryByRole("link", { name: /Skills/ })).not.toBeInTheDocument()
  })

  it("opens the work item Verification tab from the review queue", async () => {
    renderApp("/reviews")
    const user = userEvent.setup()

    const keyLink = await screen.findByRole("link", { name: "SG-155" })
    expect(keyLink).toHaveAttribute("href", "/work/SG-155?tab=verification")

    await user.click(keyLink)

    expect((await screen.findAllByRole("heading", { name: "Doc Registry migration cleanup" })).length).toBeGreaterThan(0)
    expect(screen.getByRole("tab", { name: "Verification", selected: true })).toBeInTheDocument()
  })

  it("orders Reviews as artifact decisions, then delivery evidence", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input))
      if (url.pathname === "/artifacts" && url.searchParams.get("status") === "draft") {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "artifact-draft-review", feature_name: "Draft review", version: "v1", status: "draft", updated_at: "2026-07-11T01:00:00Z" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.pathname === "/artifacts" && url.searchParams.get("status") === "needs_changes") {
        return emptyRegistryResponse(input)
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/reviews?artifact=artifact-draft-review")

    const headings = (await screen.findAllByRole("heading", { level: 2 })).map((heading) => heading.textContent)
    const ordered = ["Artifact decisions", "Delivery evidence"]
    expect(ordered.map((label) => headings.indexOf(label))).toEqual([expect.any(Number), expect.any(Number)])
    expect(headings.indexOf(ordered[0])).toBeLessThan(headings.indexOf(ordered[1]))
    expect(await screen.findByRole("dialog", { name: "Draft review" })).toBeInTheDocument()
    expect(screen.queryByText("Feedback signals")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Ask about review gaps|Ask review summary/ })).not.toBeInTheDocument()
  })

  it("shows a capability placeholder when the chat model has no key", async () => {
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "http://agents.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url === "http://agents.test/governance/chat/health") {
        return Promise.resolve(
          new Response(JSON.stringify({ configured: false, provider: "openai", model: "gpt-5.4-mini" }), {
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    langGraphClientMock.search.mockResolvedValue([])

    renderApp("/work")
    const user = userEvent.setup()

    const agentTrigger = await screen.findAllByRole("button", { name: "Open governance agent" }).then((buttons) =>
      buttons.find((button) => !button.hasAttribute("disabled")),
    )
    await user.click(agentTrigger!)

    expect(await screen.findByText("Chat model not configured")).toBeInTheDocument()
    expect(screen.getByText(/GOVERNANCE_OPS_API_KEY/)).toBeInTheDocument()
    expect(screen.getByText("Point to the CLI command that runs readiness checks")).toBeInTheDocument()
    expect(screen.getByText(/separate from Settings → Models/)).toBeInTheDocument()
    expect(
      screen.queryByPlaceholderText("Ask about gate failures, blockers, or artifacts. Type / for commands"),
    ).not.toBeInTheDocument()
  })

  it("hides review filters when there is nothing to review", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(
          new Response(JSON.stringify({ items: [] }), {
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/reviews")

    expect(await screen.findByText("No delivery evidence needs review.")).toBeInTheDocument()
    expect(screen.queryByLabelText("Search reviews")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "All" })).not.toBeInTheDocument()
  })

  it("does not present unavailable review data as an empty queue", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(new Response("registry unavailable", { status: 503 }))
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/reviews")

    expect(await screen.findByText("Delivery evidence unavailable")).toBeInTheDocument()
    expect(screen.getByText(/Live review data is unavailable/)).toBeInTheDocument()
    expect(screen.queryByText("No delivery evidence needs review.")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Search reviews")).not.toBeInTheDocument()
  })

  it("filters the review queue by review reason", async () => {
    renderApp("/reviews")
    const user = userEvent.setup()

    await user.click(screen.getByRole("button", { name: /Needs changes/ }))

    expect(screen.getByLabelText("2 visible items")).toBeInTheDocument()
    expect(screen.getByText("visible items")).toBeInTheDocument()
    expect(screen.getByText("Pre-release verification sweep")).toBeInTheDocument()
    expect(screen.getByText("Doc Registry migration cleanup")).toBeInTheDocument()

    await user.type(screen.getByLabelText("Search reviews"), "zzzz")

    expect(screen.getByText("No reviews in this view")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Clear filters" }))
    expect(screen.getByText("Doc Registry migration cleanup")).toBeInTheDocument()
  })

  it("opens workspace actions from the sidebar user block", async () => {
    renderApp("/work")
    const user = userEvent.setup()

    expect(screen.queryByRole("button", { name: /SpecGate Core/ })).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Open workspace menu" }))

    expect(screen.queryByRole("menuitem", { name: "Change name" })).not.toBeInTheDocument()
    // Workspace switching appears only when the appliance reports more than one
    // workspace. Logout and client-only identity editing are not UI actions.
    expect(screen.queryByRole("menuitem", { name: "Change workspace" })).not.toBeInTheDocument()
    expect(screen.queryByRole("menuitem", { name: "Logout" })).not.toBeInTheDocument()
  })

  it("does not show local workspace fallback choices when registry workspaces are unavailable", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/workspaces") {
        return Promise.resolve(new Response("registry unavailable", { status: 503 }))
      }
      if (url === "http://registry.test/workboard/change-requests") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")
    const user = userEvent.setup()

    await user.click(screen.getByRole("button", { name: "Open workspace menu" }))

    // With registry workspaces unavailable there is at most the profile's own
    // workspace, so no bundled fallback choices and no switcher entry at all.
    expect(screen.queryByRole("menuitem", { name: "Change workspace" })).not.toBeInTheDocument()
    expect(screen.queryByText("SpecGate Sandbox")).not.toBeInTheDocument()
    expect(screen.queryByText("SpecGate Docs")).not.toBeInTheDocument()
  })

  it("returns to explicit setup when the saved workspace no longer exists", async () => {
    localStorage.setItem(
      sessionStorageKey,
      JSON.stringify({
        profile: {
          id: "d747-stale-workspace",
          slug: "specgate-core",
          name: "SpecGate Core",
          user: {
            id: "stale-user",
            username: "thanhtung",
            name: "Tung Local",
            email: "tung@example.com",
          },
        },
      }),
    )
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/workspaces") {
        return Promise.resolve(
          new Response(JSON.stringify({ items: [{ id: "workspace-other", slug: "other", name: "Other" }] })),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/knowledge")

    expect(await screen.findByRole("heading", { name: "Set up attribution" })).toBeInTheDocument()
    expect(screen.getByRole("alert")).toHaveTextContent("Saved workspace is no longer available")
    expect(fetchMock).not.toHaveBeenCalledWith(
      "http://registry.test/api/v1/identity/bootstrap",
      expect.any(Object),
    )
    expect(localStorage.getItem(sessionStorageKey)).toBeNull()
  })

  it("keeps setup open when a configured Doc Registry is unavailable", async () => {
    localStorage.clear()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/workspaces" || url.endsWith("/api/v1/identity/bootstrap")) {
        return Promise.resolve(new Response("registry unavailable", { status: 503 }))
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")
    const user = userEvent.setup()

    expect(await screen.findByRole("heading", { name: "Set up attribution" })).toBeInTheDocument()
    await user.type(screen.getByLabelText("Display name"), "Offline Dev")
    await user.type(screen.getByLabelText("Username"), "offline")
    await user.click(screen.getByRole("button", { name: "Continue" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("Doc Registry is unavailable")
    expect(screen.getByRole("alert")).toHaveTextContent("specgate doctor")
    expect(screen.getByRole("heading", { name: "Set up attribution" })).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Work" })).not.toBeInTheDocument()
    expect(localStorage.getItem(sessionStorageKey)).toBeNull()
  })

  it("bootstraps submitted attribution through Doc Registry using the first existing workspace", async () => {
    localStorage.clear()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/workspaces") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "ws-core", slug: "core", name: "SpecGate Core" },
                { id: "ws-docs", slug: "docs", name: "SpecGate Docs" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/api/v1/identity/bootstrap" && init?.method === "POST") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              user: {
                id: "user-dev",
                username: "dev",
                display_name: "Dev",
              },
              workspace: { id: "ws-core", slug: "core", name: "SpecGate Core" },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/workboard/change-requests?workspace_id=ws-core") {
        return emptyRegistryResponse(input)
      }
      return Promise.resolve(new Response("{}", { status: 404 }))
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")

    const user = userEvent.setup()
    expect(await screen.findByRole("heading", { name: "Set up attribution" })).toBeInTheDocument()
    await user.type(screen.getByLabelText("Display name"), "Dev")
    await user.type(screen.getByLabelText("Username"), "dev")
    await waitFor(() => expect(screen.getByLabelText("Workspace name")).toHaveValue("SpecGate Core"))
    await user.click(screen.getByRole("button", { name: "Continue" }))
    await screen.findByRole("heading", { name: "Work" })
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/api/v1/identity/bootstrap",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            workspace_name: "SpecGate Core",
            display_name: "Dev",
            username: "dev",
          }),
        }),
      )
    })
    await waitFor(() => {
      expect(screen.getAllByText("SpecGate Core").length).toBeGreaterThan(0)
    })
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/workboard/change-requests?workspace_id=ws-core",
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      )
    })
  })

  it("renders route-backed work item detail", async () => {
    renderApp("/work/SG-155")

    expect(screen.getByRole("heading", { name: "Work" })).toBeInTheDocument()
    expect((await screen.findAllByRole("heading", { name: "Doc Registry migration cleanup" })).length).toBeGreaterThan(0)
    expect(await screen.findByRole("tab", { name: "Handoff" })).toBeInTheDocument()
    expect(screen.getByText("Governance agent context")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Ask about review gaps" })).toBeInTheDocument()
    expect(screen.getByText("Resume in CLI")).toBeInTheDocument()
  })

  it("shows an explicit state for unknown work item routes", async () => {
    renderApp("/work/SG-404")

    expect(await screen.findByRole("heading", { name: "Work item not found" })).toBeInTheDocument()
    expect(screen.getByText("SG-404")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Back to work" })).toHaveAttribute("href", "/work")
    expect(screen.queryByText("Work queue")).not.toBeInTheDocument()
  })

  it("does not show not-found while a registry work item route is loading", () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>(() => {})))

    renderApp("/work/CR-LIVE")

    expect(screen.getByRole("heading", { name: "Loading work item" })).toBeInTheDocument()
    expect(screen.getByText("CR-LIVE")).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Work item not found" })).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not synthesize live gate runs without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/workboard/change-requests")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-gate-run-missing",
                  key: "CR-GATE-RUN-MISSING",
                  work_type: "cleanup",
                  title: "Gate run missing id",
                  intent_md: "Do not render fake gate-run rows.",
                  created_by: "DX",
                  created_at: "2026-06-27T05:00:00Z",
                  updated_at: "2026-06-27T05:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/workboard/change-requests/cr-gate-run-missing/gate-runs?limit=10")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  gate: "delivery_review",
                  state: "fail",
                  hint: "Missing delivery evidence",
                  created_at: "2026-06-27T06:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-GATE-RUN-MISSING")
    const user = userEvent.setup()

    expect(await screen.findByText("Gate run missing id")).toBeInTheDocument()
    await user.click(screen.getByRole("tab", { name: "Verification" }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/workboard/change-requests/cr-gate-run-missing/gate-runs?limit=10&workspace_id=workspace-main",
        expect.any(Object),
      ),
    )
    expect(screen.getByText("No persisted gate runs yet.")).toBeInTheDocument()
    expect(screen.queryByText("Missing delivery evidence")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows live no-fallback errors for work item readback sections", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/workboard/change-requests")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-readback",
                  key: "CR-READBACK",
                  feature_id: "feature-readback",
                  work_type: "cleanup",
                  title: "Work readback outage",
                  intent_md: "Show registry readback failures without sample fallbacks.",
                  created_by: "DX",
                  created_at: "2026-06-27T07:00:00Z",
                  updated_at: "2026-06-27T07:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (
        url.endsWith("/workboard/change-requests/cr-readback/acceptance-criteria") ||
        url.endsWith("/workboard/change-requests/cr-readback/next-actions") ||
        url.endsWith("/workboard/change-requests/cr-readback/gate-runs?limit=10") ||
        url.endsWith("/workboard/change-requests/cr-readback/tracker-links") ||
        url.endsWith("/workboard/features/feature-readback") ||
        url.endsWith("/api/v1/work-items/cr-readback/policy") ||
        url.endsWith("/api/v1/work-items/cr-readback/delivery-status?detail=true")
      ) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "registry unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-READBACK")
    const user = userEvent.setup()

    expect(await screen.findByText("Work readback outage")).toBeInTheDocument()
    expect(await screen.findByText(/Feature context unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/Acceptance criteria unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/Linked issues unavailable/)).toBeInTheDocument()
    expect(screen.queryByText(/No acceptance criteria are recorded for this work item yet/)).not.toBeInTheDocument()
    expect(screen.queryByText("No tracker links recorded.")).not.toBeInTheDocument()
    await user.click(screen.getByRole("tab", { name: "Verification" }))

    expect(await screen.findByText(/Gate next actions unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/Gate run history unavailable/)).toBeInTheDocument()
    expect(await screen.findByText(/Policy explanation unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/no fallback policy guidance is shown/)).toBeInTheDocument()
    expect(screen.getByText(/Delivery review readback unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/no fallback delivery review detail is shown/)).toBeInTheDocument()
    expect(screen.queryByText("No next actions recorded.")).not.toBeInTheDocument()
    expect(screen.queryByText("No persisted gate runs yet.")).not.toBeInTheDocument()
    expect(screen.queryByText(/Current verdict is/)).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not synthesize live tracker links without registry identifiers and URLs", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/workboard/change-requests")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-tracker-missing",
                  key: "CR-TRACKER-MISSING",
                  feature_id: "feature-tracker-missing",
                  work_type: "cleanup",
                  title: "Tracker link missing id",
                  intent_md: "Do not render fake issue links.",
                  created_by: "DX",
                  created_at: "2026-06-27T07:00:00Z",
                  updated_at: "2026-06-27T07:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/workboard/change-requests/cr-tracker-missing/tracker-links")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { state: "opened", tracker_state: "Open" },
                { identifier: "ENG-123", state: "opened", tracker_state: "Open" },
                { url: "https://tracker.test/ENG-124", state: "opened", tracker_state: "Open" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-TRACKER-MISSING")

    expect(await screen.findByText("Tracker link missing id")).toBeInTheDocument()
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/workboard/change-requests/cr-tracker-missing/tracker-links?workspace_id=workspace-main",
        expect.any(Object),
      ),
    )
    expect(screen.getByText("No tracker links recorded.")).toBeInTheDocument()
    expect(screen.queryByText("tracker-1")).not.toBeInTheDocument()
    expect(screen.queryByText("ENG-123")).not.toBeInTheDocument()
    expect(screen.queryByRole("link", { name: /tracker/i })).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows work-item freshness signals as read-only registry context", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/workboard/change-requests")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-live",
                  key: "CR-LIVE",
                  feature_id: "feature-live",
                  work_type: "cleanup",
                  title: "Freshness signal check",
                  intent_md: "Show stale Context Pack and tracker contradiction without fixing it from the browser.",
                  created_by: "DX",
                  created_at: "2026-06-27T05:00:00Z",
                  updated_at: "2026-06-27T05:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/workboard/stale-warnings?change_request_id=cr-live")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  code: "linked_knowledge_newer",
                  severity: "warning",
                  message: "Linked Knowledge changed after the approved source artifact.",
                  feature_id: "feature-live",
                  change_request_id: "cr-live",
                  artifact_id: "artifact-context-pack",
                },
                {
                  code: "tracker_status_conflict",
                  severity: "warning",
                  message: "Tracker says complete but merge evidence is missing.",
                  feature_id: "feature-live",
                  change_request_id: "cr-live",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-LIVE")

    expect((await screen.findAllByText("Freshness signal check")).length).toBeGreaterThan(0)
    expect(await screen.findByText("Freshness signals")).toBeInTheDocument()
    expect(screen.getByText("Linked Knowledge Newer")).toBeInTheDocument()
    expect(screen.getByText("Linked Knowledge changed after the approved source artifact.")).toBeInTheDocument()
    expect(screen.getByText("Tracker Status Conflict")).toBeInTheDocument()
    expect(screen.getByText("Tracker says complete but merge evidence is missing.")).toBeInTheDocument()
    expect(screen.getByText("artifact-context-pack")).toBeInTheDocument()
    expect(screen.getByText("read-only")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Regenerate/i })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Rerun/i })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Resolve/i })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/workboard/stale-warnings?change_request_id=cr-live&workspace_id=workspace-main",
      expect.any(Object),
    )

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("renders the delivery verdict before gate summary and collapses gate detail in the verification tab", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/work-items/SG-155/delivery-status?detail=true")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              change_request_id: "SG-155",
              found: true,
              verdict: "pass",
              hint: "All acceptance criteria are satisfied.",
              reviewed_at: "2026-06-27T05:30:00Z",
              judge_model: "gpt-5.2",
              executor: "platform",
              git_receipt: {
                availability: "available",
                base_revision: "base-1",
                branch: "codex/trust-display",
                changed_files: ["app/ui/src/components/layout/work/item-detail.tsx"],
                diff_digest: "sha256:digest",
                head_revision: "abc123def4567890",
                repository: "specgate",
                warnings: [],
              },
              peer_review: {
                agent_name: "review-agent",
                reviewed_at: "2026-06-27T05:20:00Z",
                state: "stale",
              },
              per_criterion: [
                {
                  criterion_id: "ac-1",
                  text: "Trust provenance stays visible",
                  verdict: "met",
                  trust_tier: "grounded",
                  verification_binding: "app/ui/src/components/layout/work/item-detail.tsx:723",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/workboard/change-requests/SG-155/next-actions")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { gate: "delivery_review", state: "pass", hint: "Delivery review passed." },
                { gate: "spec_completeness", state: "not_applicable", hint: "Not required for quick work." },
                { gate: "design_readiness", state: "not_applicable", hint: "Not required for quick work." },
                { gate: "risk_review", state: "not_applicable", hint: "Not required for quick work." },
                { gate: "release_notes", state: "not_applicable", hint: "Not required for quick work." },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/workboard/change-requests/SG-155/gate-runs?limit=10")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "run-2", gate: "delivery_review", state: "pass", hint: "Latest delivery verdict.", created_at: "2026-06-27T05:30:00Z" },
                { id: "run-1", gate: "delivery_review", state: "pending", hint: "Waiting on delivery evidence.", created_at: "2026-06-27T05:00:00Z" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/SG-155?tab=verification")
    const user = userEvent.setup()

    const deliveryHeading = await screen.findByText("Delivery review")
    expect(await screen.findByLabelText("Delivery evidence available for review")).toBeInTheDocument()
    expect(screen.queryByLabelText("Delivery evidence has gaps")).not.toBeInTheDocument()
    expect(await screen.findAllByText("Ready for human review")).toHaveLength(2)
    expect(screen.getByText("Agent-reported; local citation captured")).toBeInTheDocument()
    expect(screen.getByText("Awaiting human acceptance")).toBeInTheDocument()
    expect(screen.getByText("Receipt recorded at commit abc123def456")).toBeInTheDocument()
    expect(screen.getByText("Platform model (gpt-5.2)")).toBeInTheDocument()
    expect(screen.getByText("Stale")).toBeInTheDocument()
    expect(screen.getByText("Grounded")).toBeInTheDocument()
    expect(screen.getByText("app/ui/src/components/layout/work/item-detail.tsx:723")).toBeInTheDocument()
    expect(screen.getByText(/A model review evaluates submitted evidence/)).toBeInTheDocument()
    expect(screen.queryByText(/Current verdict is/)).not.toBeInTheDocument()
    const gateHeading = screen.getByText("Gate state")
    const gateStateCard = gateHeading.closest(".rounded-lg")
    expect(gateStateCard).not.toBeNull()
    expect(within(gateStateCard as HTMLElement).getByText("Passed")).toBeInTheDocument()
    expect(within(gateStateCard as HTMLElement).queryByText("Failed")).not.toBeInTheDocument()
    expect(deliveryHeading.compareDocumentPosition(gateHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByText("What each gate checked and why, per run.")).toBeInTheDocument()
    expect(screen.queryByText("Readiness and quality outcomes stay in item context.")).not.toBeInTheDocument()

    const gateSummaryToggle = screen.getByRole("button", { name: "5 gates · 1 passed · 4 not applicable" })
    expect(gateSummaryToggle).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByText("Not required for quick work.")).not.toBeInTheDocument()
    await user.click(gateSummaryToggle)
    expect(screen.getAllByText("Not required for quick work.")).toHaveLength(4)
    expect(
      screen.getByText("Checks the package covers its required topics (outcomes, criteria, risks…). Reads every document in the package."),
    ).toBeInTheDocument()
    expect(screen.getAllByText("Judges the delivery evidence against every acceptance criterion.").length).toBeGreaterThan(1)

    expect(screen.getByText("Latest delivery verdict.")).toBeInTheDocument()
    expect(screen.queryByText("Waiting on delivery evidence.")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Show all runs" }))
    expect(screen.getByText("Waiting on delivery evidence.")).toBeInTheDocument()
    expect(screen.getByText("Latest delivery verdict.")).toBeInTheDocument()
  })

  it("renders a human rejection as authority without rewriting passing evidence", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/work-items/SG-155/delivery-status?detail=true")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              change_request_id: "SG-155",
              found: true,
              verdict: "fail",
              evidence_verdict: "pass",
              hint: "Human reviewer requested rework.",
              reviewed_at: "2026-06-27T05:30:00Z",
              executor: "human",
              actor: "lead",
              note: "Restore the rollback test.",
              confidence: 0,
              per_criterion: [],
              checks: [],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/SG-155?tab=verification")

    expect(await screen.findByLabelText("Delivery rejected")).toBeInTheDocument()
    expect(screen.queryByLabelText("Delivery evidence has gaps")).not.toBeInTheDocument()
    expect(screen.getByText("Ready for human review")).toBeInTheDocument()
    expect(screen.getAllByText("Rejected")).toHaveLength(2)
    expect(screen.getByText(/Restore the rollback test/)).toBeInTheDocument()
    expect(screen.getByText("0% reviewer confidence")).toBeInTheDocument()
  })

  it("discloses gate-run evidence with judge origin, confidence, and content", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/workboard/change-requests/SG-155/gate-runs?limit=10")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "run-judged",
                  gate: "canonical_spec",
                  state: "warn",
                  hint: "Judge confidence below threshold",
                  evidence_json: JSON.stringify({
                    evidence_contract_version: "gate-run-v1",
                    gate: "canonical_spec",
                    evaluator: { type: "agent_judge", judge_model: "gpt-5-mini" },
                    verdict: "warn",
                    confidence: 0.85,
                    evidence: "The working spec has not been promoted to canonical.",
                  }),
                  created_at: "2026-06-27T05:30:00Z",
                },
                {
                  id: "run-attested",
                  gate: "delivery_review",
                  state: "pass",
                  hint: "Verdict derived from the coding agent's acceptance-criteria claims.",
                  evidence_json: JSON.stringify({
                    evidence_contract_version: "gate-run-v1",
                    gate: "delivery_review",
                    evaluator: { type: "agent_judge", judge_model: "agent_attested" },
                    verdict: "pass",
                    confidence: 1,
                    evidence: JSON.stringify({
                      criteria: [{ criterion_id: "ac-1", text: "Doc registry CI passes", verdict: "met", why: "coding-agent claim: satisfied" }],
                      checks: [],
                    }),
                  }),
                  created_at: "2026-06-27T05:20:00Z",
                },
                {
                  id: "run-deterministic",
                  gate: "spec_drafted",
                  state: "pass",
                  hint: "artifact-SG-155",
                  evidence_json: JSON.stringify({
                    evidence_contract_version: "gate-run-v1",
                    gate: "spec_drafted",
                    evaluator: { type: "deterministic", judge_model: "deterministic-v1" },
                    verdict: "pass",
                    confidence: 1,
                    evidence: "",
                  }),
                  created_at: "2026-06-27T05:10:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/SG-155?tab=verification")
    const user = userEvent.setup()

    expect(await screen.findByText("Gate run history")).toBeInTheDocument()
    // The deterministic run carries no judgment or evidence content, so it gets no disclosure.
    const whyButtons = await screen.findAllByRole("button", { name: "Why" })
    expect(whyButtons).toHaveLength(2)

    await user.click(whyButtons[0])
    expect(screen.getByText("Evaluated by platform model (gpt-5-mini) · confidence 0.85")).toBeInTheDocument()
    expect(screen.getByText("The working spec has not been promoted to canonical.")).toBeInTheDocument()

    await user.click(whyButtons[1])
    expect(screen.getByText("Agent-attested · confidence 1")).toBeInTheDocument()
    expect(screen.getByText("Doc registry CI passes")).toBeInTheDocument()
    expect(screen.getByText("coding-agent claim: satisfied")).toBeInTheDocument()
  })

  it("derives acceptance criteria state from the delivery verdict when present", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/workboard/change-requests/SG-155/acceptance-criteria")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "ac-1", text: "Doc registry CI passes", done: false, source: "spec" },
                { id: "ac-2", text: "Expected misses are quiet", done: false, source: "spec" },
                { id: "ac-3", text: "Text alone must not establish identity", done: false, source: "spec" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/work-items/SG-155/delivery-status?detail=true")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              change_request_id: "SG-155",
              found: true,
              verdict: "needs_changes",
              per_criterion: [
                { criterion_id: "ac-1", text: "Doc registry CI passes", verdict: "met", why: "CI evidence is green." },
                { criterion_id: "ac-2", text: "Expected misses are quiet", verdict: "unmet", why: "Noise remains in logs." },
                { criterion_id: "different-id", text: "Text alone must not establish identity", verdict: "met", why: "Wrong identity." },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/SG-155")

    expect(await screen.findByText("Doc registry CI passes")).toBeInTheDocument()
    expect(await screen.findByText("1/3")).toBeInTheDocument()
    expect(screen.getByText("Met")).toBeInTheDocument()
    expect(screen.getByText("Unmet")).toBeInTheDocument()
    expect(screen.queryByText("2/3")).not.toBeInTheDocument()
  })

  it("prioritizes a human-review delivery verdict in the work detail header", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/workboard/change-requests/SG-155/acceptance-criteria")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "ac-1", text: "Delivered work leaves the attention list", done: false, source: "spec" },
                { id: "ac-2", text: "Status badge refreshes", done: false, source: "spec" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/work-items/SG-155/delivery-status?detail=true")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              change_request_id: "SG-155",
              found: true,
              verdict: "needs_human_review",
              hint: "One criterion is still unclear.",
              per_criterion: [
                { criterion_id: "ac-1", text: "Delivered work leaves the attention list", verdict: "unclear", why: "Needs proof." },
                { criterion_id: "ac-2", text: "Status badge refreshes", verdict: "met", why: "Covered." },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/SG-155")

    expect(await screen.findByText("Delivered work leaves the attention list")).toBeInTheDocument()
    expect(screen.getByText("Needs review")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /Inspect gaps/ })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /Ask about review gaps/ })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /View handoff/ })).not.toBeInTheDocument()
  })

  it("surfaces delivered work in a dedicated queue chip instead of the action queue", async () => {
    vi.stubGlobal("fetch", vi.fn(deliveredRegistryResponse))
    renderApp("/work")
    const user = userEvent.setup()

    const deliveredChip = await screen.findByRole("button", { name: /^Delivered1$/ })
    const allWorkChip = screen.getByRole("button", { name: /^All work/ })
    expect(deliveredChip.compareDocumentPosition(allWorkChip) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.queryByText("Delivered settings polish")).not.toBeInTheDocument()

    await user.click(deliveredChip)

    expect(screen.getAllByText("Delivered settings polish").length).toBeGreaterThan(0)
    expect(screen.getAllByText("Accepted").length).toBeGreaterThan(0)
    expect(screen.queryByText("Pre-release verification sweep")).not.toBeInTheDocument()
  })

  it("includes acceptance-ready delivery in the Work needs-review queue", async () => {
    const readyForDecision = {
      ...registryWorkItems[0],
      id: "SG-READY",
      key: "SG-READY",
      title: "Acceptance-ready delivery",
      delivery_review: {
        verdict: "pass",
        hint: "Delivery evidence is ready for human review.",
        reviewed_at: "2026-07-19T12:00:00Z",
      },
    }
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(
          new Response(JSON.stringify({ items: [readyForDecision] }), {
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return defaultRegistryResponse(input, init)
    }))
    renderApp("/work")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("button", { name: /^Needs review1$/ }))

    expect(screen.getAllByText("Acceptance-ready delivery").length).toBeGreaterThan(0)
  })

  it("shows a View verdict action and review-summary prompt for delivered work", async () => {
    vi.stubGlobal("fetch", vi.fn(deliveredRegistryResponse))
    renderApp("/work/SG-160")
    const user = userEvent.setup()

    expect((await screen.findAllByRole("heading", { name: "Delivered settings polish" })).length).toBeGreaterThan(0)
    expect(screen.queryByRole("button", { name: "View handoff" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Ask about handoff blockers" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Ask for review summary" })).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "View verdict" }))

    expect(screen.getByRole("tab", { name: "Verification", selected: true })).toBeInTheDocument()
    expect(await screen.findByText("Delivery review")).toBeInTheDocument()
  })

  it("excludes delivered work from the review queue count", async () => {
    vi.stubGlobal("fetch", vi.fn(deliveredRegistryResponse))
    renderApp("/reviews")

    expect(await screen.findByText("2 items need review")).toBeInTheDocument()
    expect(screen.queryByText("Delivered settings polish")).not.toBeInTheDocument()
  })

  it("pluralizes the review count heading for a single item", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(
          new Response(
            JSON.stringify({ items: [registryWorkItems.find((item) => item.id === "SG-147")] }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/reviews")

    expect(await screen.findByText("1 item needs review")).toBeInTheDocument()
    expect(screen.queryByText(/items need review/)).not.toBeInTheDocument()
  })

  it("uses the authoritative delivery review verdict in the review queue", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-review",
                  key: "CR-REVIEW",
                  title: "Needs human review",
                  delivery_review: {
                    verdict: "needs_human_review",
                    hint: "Missing one browser check.",
                    reviewed_at: "2026-07-03T14:10:28Z",
                  },
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/reviews")

    expect(await screen.findByText("1 item needs review")).toBeInTheDocument()
    expect(screen.getByText("Needs review")).toBeInTheDocument()
    expect(screen.getByText("Missing one browser check.")).toBeInTheDocument()
    expect(screen.queryByText("Ready for review")).not.toBeInTheDocument()
  })

  it("keeps acceptance-ready review copy ahead of the Review phase proxy", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{
                id: "cr-ready-review",
                key: "CR-READY-REVIEW",
                title: "Acceptance-ready review",
                phase: "Review",
                delivery_review: {
                  verdict: "pass",
                  hint: "Delivery evidence is ready for human review.",
                  reviewed_at: "2026-07-19T12:00:00Z",
                },
              }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/reviews")

    const row = await screen.findByRole("row", { name: /CR-READY-REVIEW/ })
    expect(within(row).getByText("Ready for human review")).toBeInTheDocument()
    expect(within(row).getByText("Delivery evidence is ready for human review.")).toBeInTheDocument()
    expect(within(row).getByRole("link", { name: "Inspect review outcome" })).toBeInTheDocument()
    expect(within(row).queryByText("A required gate failed.")).not.toBeInTheDocument()
  })

  it("drops the Owner column from the work queue table", async () => {
    renderApp("/work")

    expect(await screen.findByText("Work queue")).toBeInTheDocument()
    expect(await screen.findByRole("columnheader", { name: "Blocker" })).toBeInTheDocument()
    expect(screen.queryByText("Owner")).not.toBeInTheDocument()
  })

  it("labels pickup-ready queue rows without implying delivery passed", async () => {
    renderApp("/work")

    const row = await screen.findByRole("row", { name: /SG-151/ })

    expect(within(row).getByText("Ready for pickup")).toBeInTheDocument()
    expect(within(row).getByText("Ready")).toBeInTheDocument()
    expect(within(row).queryByText("Passed")).not.toBeInTheDocument()
  })

  it("labels pickup-ready work detail without implying delivery passed", async () => {
    renderApp("/work/SG-151")

    expect(await screen.findByRole("heading", { name: "Agent skills setup primitives" })).toBeInTheDocument()
    expect(screen.getByText("Ready for pickup")).toBeInTheDocument()
    expect(screen.queryByText("Passed")).not.toBeInTheDocument()
  })

  it("drops the Owner column from the review queue table", async () => {
    renderApp("/reviews")

    expect(await screen.findByText("Delivery evidence")).toBeInTheDocument()
    expect(await screen.findByText("Pre-release verification sweep")).toBeInTheDocument()
    expect(screen.queryByText("Owner")).not.toBeInTheDocument()
  })

  it("drops Owner and Detail source from the work context panel", async () => {
    renderApp("/work/SG-142")

    expect(await screen.findByText("Work context")).toBeInTheDocument()
    expect(screen.getByText("Blocker")).toBeInTheDocument()
    expect(screen.queryByText("Owner")).not.toBeInTheDocument()
    expect(screen.queryByText("Detail source")).not.toBeInTheDocument()
  })

  it("keeps Work read-only and exposes a copyable CLI resume command", async () => {
    renderApp("/work/SG-155")

    expect(await screen.findByRole("button", { name: "Copy resume command" })).toBeEnabled()
    expect(screen.queryByText("Route decision")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Create quick Context Pack/ })).not.toBeInTheDocument()
  })

  it("refreshes Work explicitly and reports the last successful refresh", async () => {
    renderApp("/work")
    const user = userEvent.setup()

    const refresh = await screen.findByRole("button", { name: "Refresh work" })
    await user.click(refresh)

    expect(await screen.findByText(/Last refreshed/)).toBeInTheDocument()
  })

  it("renders artifact list and selected artifact detail", async () => {
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect(screen.getByRole("heading", { name: "Artifacts" })).toBeInTheDocument()
    expect(screen.getByText("Artifact library")).toBeInTheDocument()
    expect(screen.getByLabelText("Search artifacts")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Current" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "All statuses" })).toBeInTheDocument()
    expect(await screen.findByRole("button", { name: "Approved" })).toBeInTheDocument()
    expect((await screen.findAllByText("Doc Registry migration cleanup")).length).toBeGreaterThan(0)
    expect(screen.queryByText("Documents")).not.toBeInTheDocument()
    expect(screen.queryByRole("dialog", { name: "Document inspector" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Preview spec" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Download summary" })).not.toBeInTheDocument()

    await user.click(await screen.findByRole("button", { name: /Doc Registry migration cleanup/ }))

    const detailDialog = await screen.findByRole("dialog", { name: "Doc Registry migration cleanup" })
    const detailScrollArea = detailDialog.querySelector("[data-slot='scroll-area']")
    expect(detailDialog).toHaveClass("grid")
    expect(detailScrollArea).toHaveClass("min-h-0", "overflow-hidden")
    expect(within(detailDialog).getByText("Documents")).toBeInTheDocument()
    expect(within(detailDialog).getAllByText("migration/spec.md").length).toBeGreaterThan(0)
    expect(within(detailDialog).getByText("Feedback")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Attachments")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Migration rollout note")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Completion evidence is ready for delivery review.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Expected gates")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Readiness history")).toBeInTheDocument()
    expect(within(detailDialog).getAllByText("Spec Completeness").length).toBeGreaterThan(0)
    expect(within(detailDialog).getAllByText("missing constraints").length).toBeGreaterThan(0)
    expect(within(detailDialog).getByText("Audit events")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Artifact Published")).toBeInTheDocument()
    expect(within(detailDialog).queryByRole("link", { name: "Open work" })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: "Ask agent" })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Refresh readiness/i })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Rerun/i })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Save revision/i })).not.toBeInTheDocument()

    await user.click(within(detailDialog).getByRole("button", { name: /migration\/spec\.md/ }))

    const previewDialog = await screen.findByRole("dialog", { name: "Document inspector" })
    expect(within(previewDialog).getByRole("button", { name: "View" })).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Diff" })).toBeInTheDocument()
    expect(within(previewDialog).getByLabelText("Version")).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Markdown" })).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Code" })).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Copy" })).toBeInTheDocument()
    expect(await within(previewDialog).findByText("Mermaid diagram")).toBeInTheDocument()
    expect(await within(previewDialog).findByLabelText("Mermaid diagram viewport")).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Source" })).toBeInTheDocument()
    expect(within(previewDialog).queryByText("sample")).not.toBeInTheDocument()
    await user.click(within(previewDialog).getByRole("button", { name: "Code" }))
    expect(within(previewDialog).getByText(/```mermaid/)).toBeInTheDocument()
    await user.click(within(previewDialog).getByRole("button", { name: "Diff" }))
    expect(await within(previewDialog).findByText("Line diff")).toBeInTheDocument()
    expect(within(previewDialog).queryByLabelText("Compare")).not.toBeInTheDocument()
    expect(within(previewDialog).getByText("Latest v0.1")).toBeInTheDocument()
    expect(within(previewDialog).getByText(/noisy expected misses/)).toBeInTheDocument()
    await user.click(within(previewDialog).getByRole("button", { name: "Close" }))
    await user.click(within(detailDialog).getByRole("button", { name: "Close" }))

    await user.click(screen.getByRole("button", { name: /Agent skills setup primitives/ }))

    const skillsDialog = await screen.findByRole("dialog", { name: "Agent skills setup primitives" })
    expect(within(skillsDialog).getAllByText("setup/spec.md").length).toBeGreaterThan(0)
  })

  it("hides superseded artifacts by default and reveals them with All statuses", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-SG-155",
                  feature_id: "SG-155",
                  feature_name: "Doc Registry migration cleanup",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-27T05:20:00Z",
                },
                {
                  id: "artifact-legacy-pipeline",
                  feature_id: "SG-140",
                  feature_name: "Legacy summary pipeline",
                  version: "v0.3",
                  status: "superseded",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-20T05:20:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Doc Registry migration cleanup")).length).toBeGreaterThan(0)
    expect(screen.queryByRole("button", { name: /Legacy summary pipeline/ })).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "All statuses" }))

    expect(await screen.findByRole("button", { name: /Legacy summary pipeline/ })).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Current" }))

    expect(screen.queryByRole("button", { name: /Legacy summary pipeline/ })).not.toBeInTheDocument()
  })

  it("filters artifacts by search and status", async () => {
    renderApp("/artifacts")
    const user = userEvent.setup()

    await user.type(screen.getByLabelText("Search artifacts"), "skills")

    expect(screen.getAllByText("Agent skills setup primitives").length).toBeGreaterThan(0)
    expect(screen.queryByRole("button", { name: /Doc Registry migration cleanup/ })).not.toBeInTheDocument()

    await user.clear(screen.getByLabelText("Search artifacts"))
    await user.type(screen.getByLabelText("Search artifacts"), "missing artifact")

    expect(screen.getByText("No artifacts match")).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Clear filters" }))
    await user.click(screen.getByRole("button", { name: "Approved" }))

    expect(screen.getAllByText("Doc Registry migration cleanup").length).toBeGreaterThan(0)
  })

  it("restores the selected artifact from the URL query", async () => {
    renderApp("/artifacts?artifact=artifact-SG-151")

    const detailDialog = await screen.findByRole("dialog", { name: "Agent skills setup primitives" })
    expect(within(detailDialog).queryByRole("link", { name: "Open work" })).not.toBeInTheDocument()
    expect((await within(detailDialog).findAllByText("setup/spec.md")).length).toBeGreaterThan(0)
  })

  it("restores the selected artifact from the CLI review URL", async () => {
    renderApp("/artifacts/artifact-SG-151")

    const detailDialog = await screen.findByRole("dialog", { name: "Agent skills setup primitives" })
    expect(within(detailDialog).queryByRole("link", { name: "Open work" })).not.toBeInTheDocument()
    expect((await within(detailDialog).findAllByText("setup/spec.md")).length).toBeGreaterThan(0)
  })

  it("keeps document inspection while omitting the duplicate artifact agent action", async () => {
    renderApp("/artifacts")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("button", { name: /Doc Registry migration cleanup/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Doc Registry migration cleanup" })
    await user.click(within(detailDialog).getByRole("button", { name: /migration\/tasks\.md/ }))
    const previewDialog = await screen.findByRole("dialog", { name: "Document inspector" })
    expect(await within(previewDialog).findByText("Doc Registry migration cleanup Tasks")).toBeInTheDocument()
    expect(within(previewDialog).queryByText("No previewable Markdown content available.")).not.toBeInTheDocument()
    await user.click(within(previewDialog).getByRole("button", { name: "Close" }))

    expect(within(detailDialog).queryByRole("button", { name: "Ask agent" })).not.toBeInTheDocument()
  })

  it("shows an empty artifact document without treating storage as unavailable", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-empty-body",
                  feature_id: "SG-201",
                  feature_name: "Unavailable live document",
                  version: "v0.2",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-empty-body/files")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ path: "live/spec.md", role: "spec", size_bytes: 2048 }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts?feature_id=SG-201&limit=100")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-empty-body",
                  feature_id: "SG-201",
                  feature_name: "Unavailable live document",
                  version: "v0.2",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T10:00:00Z",
                },
                {
                  id: "artifact-empty-body-previous",
                  feature_id: "SG-201",
                  feature_name: "Unavailable live document",
                  version: "v0.1",
                  status: "superseded",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-28T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-empty-body/files/_?path=live%2Fspec.md")) {
        return Promise.resolve(
          new Response(JSON.stringify({ content: "", size_bytes: 0 }), {
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Unavailable live document")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Unavailable live document/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Unavailable live document" })
    await user.click(await within(detailDialog).findByRole("button", { name: /live\/spec\.md/ }))

    const previewDialog = await screen.findByRole("dialog", { name: "Document inspector" })
    expect(within(previewDialog).queryByText(/stored content is unavailable/)).not.toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Copy" })).toBeDisabled()
    expect(within(previewDialog).getByRole("button", { name: "Diff" })).toBeDisabled()
    expect(within(previewDialog).queryByText("sample")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows a live no-fallback error when artifact version history is unavailable", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-version-history-error",
                  feature_id: "SG-206",
                  feature_name: "Version history outage",
                  version: "v0.2",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "medium",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T15:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-version-history-error/files")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ path: "live/spec.md", role: "spec", size_bytes: 2048 }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts?feature_id=SG-206&limit=100")) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "registry unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      if (url.endsWith("/artifacts/artifact-version-history-error/files/_?path=live%2Fspec.md")) {
        return Promise.resolve(
          new Response(JSON.stringify({ content: "# Live Spec\n\nReal registry body." }), {
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Version history outage")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Version history outage/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Version history outage" })
    await user.click(await within(detailDialog).findByRole("button", { name: /live\/spec\.md/ }))

    const previewDialog = await screen.findByRole("dialog", { name: "Document inspector" })
    expect(await within(previewDialog).findByText("Live Spec")).toBeInTheDocument()
    expect(await within(previewDialog).findByText(/Version history unavailable/)).toBeInTheDocument()
    expect(within(previewDialog).getByText(/no fallback version comparison is shown/)).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Diff" })).toBeDisabled()
    expect(within(previewDialog).queryByText(/Latest v0\.2/)).not.toBeInTheDocument()
    expect(within(previewDialog).queryByText("sample")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows a live no-fallback error when artifact documents are unavailable", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-documents-error",
                  feature_id: "SG-205",
                  feature_name: "Document list outage",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T14:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-documents-error/files")) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "registry unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Document list outage")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Document list outage/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Document list outage" })

    expect(await within(detailDialog).findByText(/Artifact documents unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/no fallback document list is shown/)).toBeInTheDocument()
    expect(within(detailDialog).queryByText("No documents are available for this artifact yet.")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not synthesize live artifact rows without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  feature_id: "SG-MISSING-ID",
                  feature_name: "Missing id artifact",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T14:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/artifacts")

    expect(await screen.findByText("No artifacts in this workspace")).toBeInTheDocument()
    expect(screen.getByText("specgate artifact publish --help")).toBeInTheDocument()
    expect(screen.queryByLabelText("Search artifacts")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Current" })).not.toBeInTheDocument()
    expect(screen.queryByText("Missing id artifact")).not.toBeInTheDocument()
    expect(screen.queryByText("artifact-1")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/artifacts/artifact-1/files", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not present an unavailable artifact library as empty", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(new Response("registry unavailable", { status: 503 }))
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/artifacts")

    expect(await screen.findByText("Artifact library unavailable")).toBeInTheDocument()
    expect(screen.getByText(/Check Doc Registry connectivity/)).toBeInTheDocument()
    expect(screen.queryByText("No artifacts in this workspace")).not.toBeInTheDocument()
    expect(screen.queryByText("specgate artifact publish --help")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Search artifacts")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("keeps the artifact library read-only and routes decisions to Reviews", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-reviewable",
                  feature_id: "SG-REVIEW",
                  feature_name: "Reviewable artifact",
                  version: "v0.1",
                  status: "draft",
                  request_type: "change_request",
                  impact_level: "medium",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-07-11T00:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect(await screen.findByRole("button", { name: /Reviewable artifact/ })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Features" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Current" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "All statuses" })).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: /Reviewable artifact/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Reviewable artifact" })

    expect(within(detailDialog).queryByRole("button", { name: "Approve" })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: "Request changes" })).not.toBeInTheDocument()
    expect(within(detailDialog).getByRole("link", { name: "Open in Reviews" })).toHaveAttribute(
      "href",
      "/reviews?artifact=artifact-reviewable",
    )
  })

  it("shows feature lineage inside artifact detail", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-feature-canonical",
                  feature_id: "SG-301",
                  feature_name: "Feature console",
                  version: "v0.3",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "medium",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-30T12:00:00Z",
                },
                {
                  id: "artifact-feature-previous",
                  feature_id: "SG-301",
                  feature_name: "Feature console previous",
                  version: "v0.2",
                  status: "superseded",
                  request_type: "change_request",
                  impact_level: "medium",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T12:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts?feature_id=SG-301&limit=100")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "artifact-feature-canonical", feature_id: "SG-301", feature_name: "Feature console", version: "v0.3", status: "approved", request_type: "change_request", impact_level: "medium", artifact_completeness: "full", source_kind: "context_pack", updated_at: "2026-06-30T12:00:00Z" },
                { id: "artifact-feature-previous", feature_id: "SG-301", feature_name: "Feature console previous", version: "v0.2", status: "superseded", request_type: "change_request", impact_level: "medium", artifact_completeness: "full", source_kind: "context_pack", updated_at: "2026-06-29T12:00:00Z" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("button", { name: /Feature console/ }))
    const artifactDialog = await screen.findByRole("dialog", { name: "Feature console" })

    expect(within(artifactDialog).getByRole("heading", { name: "Feature lineage" })).toBeInTheDocument()
    expect(await within(artifactDialog).findByText("artifact-feature-previous")).toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not fetch or render the removed feature browser", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith("/workboard/features")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "feature-live", key: "SG-401", name: "Live capability", status: "active", version: 1 },
                { id: "feature-old", key: "SG-311", name: "Legacy capability", status: "archived", version: 2 },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    expect(await screen.findByText("Artifact library")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Features" })).not.toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith("/workboard/features"))).toBe(false)
  })

  it("does not synthesize live artifact document rows without registry paths", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-missing-path",
                  feature_id: "SG-MISSING-PATH",
                  feature_name: "Missing path artifact",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T14:45:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-missing-path/files")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ role: "spec", size_bytes: 128, updated_at: "2026-06-29T14:46:00Z" }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Missing path artifact")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Missing path artifact/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Missing path artifact" })

    expect(await within(detailDialog).findByText("No documents are available for this artifact yet.")).toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /document-1\.md/ })).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith(
      "http://registry.test/artifacts/artifact-missing-path/files/_?path=document-1.md",
      expect.any(Object),
    )

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows a live no-fallback error when artifact gate preview is unavailable", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-gate-preview-error",
                  feature_id: "SG-202",
                  feature_name: "Gate preview outage",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T11:00:00Z",
                  expected_gates: ["scope_clear"],
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/artifacts/artifact-gate-preview-error/gate-preview")) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "gate preview unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Gate preview outage")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Gate preview outage/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Gate preview outage" })

    expect(await within(detailDialog).findByText(/Gate preview unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/no fallback gate snapshot is shown/)).toBeInTheDocument()
    expect(within(detailDialog).queryByText("Scope Clear")).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/api/v1/artifacts/artifact-gate-preview-error/gate-preview?workspace_id=workspace-main",
      expect.any(Object),
    )

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows live no-fallback errors for artifact detail evidence sections", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-evidence-errors",
                  feature_id: "SG-203",
                  feature_name: "Artifact evidence outage",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T12:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-evidence-errors/files")) {
        return emptyRegistryResponse(input)
      }
      if (
        url.endsWith("/features/SG-203/attachments") ||
        url.endsWith("/governance/feedback-events?artifact_id=artifact-evidence-errors&limit=20") ||
        url.endsWith("/artifacts/artifact-evidence-errors/readiness-runs?limit=20") ||
        url.endsWith("/events?artifact_id=artifact-evidence-errors&limit=20")
      ) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "registry unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Artifact evidence outage")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Artifact evidence outage/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Artifact evidence outage" })

    expect(await within(detailDialog).findByText(/Reference attachments unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/Artifact feedback unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/Readiness history unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/Audit events unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).queryByText("No reference attachments pinned to this feature.")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("No artifact-linked feedback recorded.")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("No persisted readiness runs recorded.")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("No artifact audit events recorded.")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not synthesize live artifact feedback rows without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-feedback-missing-id",
                  feature_id: "SG-205",
                  feature_name: "Artifact feedback missing id",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T12:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/governance/feedback-events?artifact_id=artifact-feedback-missing-id&limit=20")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  event_type: "delivery.comment_scope_drift",
                  status: "received",
                  reason: "A PR comment asks for work outside the approved artifact.",
                  created_at: "2026-06-30T03:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Artifact feedback missing id")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Artifact feedback missing id/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Artifact feedback missing id" })

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/governance/feedback-events?artifact_id=artifact-feedback-missing-id&limit=20&workspace_id=workspace-main",
        expect.any(Object),
      ),
    )
    expect(within(detailDialog).getByText("No artifact-linked feedback recorded.")).toBeInTheDocument()
    expect(within(detailDialog).queryByText("delivery comment scope drift")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("A PR comment asks for work outside the approved artifact.")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not synthesize live artifact evidence rows without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-evidence-missing-ids",
                  feature_id: "SG-206",
                  feature_name: "Artifact evidence missing ids",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T12:40:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/features/SG-206/attachments")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ title: "Attachment without id", kind: "link", audience: "gate", created_at: "2026-06-30T03:00:00Z" }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-evidence-missing-ids/readiness-runs?limit=20")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ gate: "spec_completeness", state: "fail", hint: "Missing constraints", created_at: "2026-06-30T03:01:00Z" }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/events?artifact_id=artifact-evidence-missing-ids&limit=20")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ event_type: "artifact.approved", payload: { status: "approved" }, created_at: "2026-06-30T03:02:00Z" }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Artifact evidence missing ids")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Artifact evidence missing ids/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Artifact evidence missing ids" })

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/artifacts/artifact-evidence-missing-ids/readiness-runs?limit=20&workspace_id=workspace-main",
        expect.any(Object),
      ),
    )
    expect(within(detailDialog).getByText("No reference attachments pinned to this feature.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("No persisted readiness runs recorded.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("No artifact audit events recorded.")).toBeInTheDocument()
    expect(within(detailDialog).queryByText("Attachment without id")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("Missing constraints")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("Artifact Published")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("revision-1")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows live no-fallback errors for artifact feature and policy readback", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-policy-errors",
                  feature_id: "SG-204",
                  feature_name: "Artifact policy outage",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T13:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/workboard/features") || url.endsWith("/api/v1/artifacts/artifact-policy-errors/policy")) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "registry unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Artifact policy outage")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Artifact policy outage/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Artifact policy outage" })

    expect(await within(detailDialog).findByText(/Feature context unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/Policy snapshot unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/no fallback policy explanation is shown/)).toBeInTheDocument()
    expect(within(detailDialog).queryByText("No policy explanation recorded for this artifact.")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("collapses long artifact document lists", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-many",
                  feature_id: "SG-200",
                  feature_name: "Large artifact bundle",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "now",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-many/files")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: Array.from({ length: 8 }, (_, index) => ({
                path: `doc-${index + 1}.md`,
                role: "reference",
                size_bytes: 100 + index,
              })),
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-many/readiness-runs?limit=20")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "run-live-1",
                  gate: "acceptance_criteria_verifiable",
                  state: "pass",
                  hint: "acceptance criteria are checkable",
                  evidence_json: "Each criterion names an observable outcome.",
                  created_at: "2026-06-28T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/artifacts/artifact-many/gate-preview")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              artifact_id: "artifact-many",
              preview_tasks: [
                {
                  gate_key: "scope_clear",
                  gate_version: "v1",
                  executor: "ide_agent",
                },
                {
                  gate_key: "acceptance_criteria_verifiable",
                  gate_version: "v1",
                  executor: "ide_agent",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/events?artifact_id=artifact-many&limit=20")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "event-live-1",
                  artifact_id: "artifact-many",
                  event_type: "artifact.approved",
                  payload: { version: "v0.1", status: "approved" },
                  created_at: "2026-06-28T12:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/artifacts/artifact-many/policy")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              governance_level: "standard",
              title: "Standard governance",
              summary: "Context Pack handoff, evidence, and delivery review are required.",
              reasons: ["Persisted artifact snapshot"],
              obligations: ["Keep implementation inside the approved artifact scope."],
              policy_lineage: [
                {
                  key: "builtin/standard",
                  version: "1",
                  digest: "sha256:artifactpolicyabcdef",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/workspaces")) {
        return defaultRegistryResponse(input)
      }
      return Promise.resolve(new Response(JSON.stringify({ content: "# Document" }), { headers: { "Content-Type": "application/json" } }))
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Large artifact bundle")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Large artifact bundle/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Large artifact bundle" })
    expect(await within(detailDialog).findByRole("button", { name: /doc-3\.md/ })).toBeInTheDocument()
    expect(await within(detailDialog).findByText("Readiness history")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Each run records what the checker read and why it decided.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Scope Clear")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Checks the change is bounded with explicit non-goals. Reads the spec.")).toBeInTheDocument()
    expect(within(detailDialog).queryByText(/preview\s*[-—]\s*not persisted/)).not.toBeInTheDocument()
    expect(within(detailDialog).getByText("Expected by this artifact's policy. Not run yet.")).toBeInTheDocument()
    expect(within(detailDialog).getAllByText("ide_agent").length).toBeGreaterThan(0)
    expect(within(detailDialog).getAllByText("Acceptance Criteria Verifiable").length).toBeGreaterThan(0)
    expect(within(detailDialog).getByText("Latest persisted readiness run.")).toBeInTheDocument()
    expect(within(detailDialog).getAllByText("acceptance criteria are checkable").length).toBeGreaterThan(0)
    expect(within(detailDialog).queryByText("Each criterion names an observable outcome.")).not.toBeInTheDocument()
    await user.click(within(detailDialog).getByRole("button", { name: "Why" }))
    expect(within(detailDialog).getByText("Each criterion names an observable outcome.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Audit events")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Artifact Approved")).toBeInTheDocument()
    expect(within(detailDialog).getByText("version: v0.1 / status: approved")).toBeInTheDocument()
    expect(within(detailDialog).getAllByText("Governance policy").length).toBeGreaterThan(0)
    expect(within(detailDialog).getByText("Standard governance")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Context Pack handoff, evidence, and delivery review are required.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Persisted artifact snapshot")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Keep implementation inside the approved artifact scope.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("builtin/standard")).toBeInTheDocument()
    expect(within(detailDialog).getByRole("button", { name: /doc-4\.md/ })).toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /doc-5\.md/ })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Refresh readiness/i })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Apply revision/i })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Accept exception|Resolve policy|Switch policy/i })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/artifacts/artifact-many/readiness-runs?limit=20&workspace_id=workspace-main",
      expect.any(Object),
    )
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/api/v1/artifacts/artifact-many/gate-preview?workspace_id=workspace-main",
      expect.any(Object),
    )
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/events?artifact_id=artifact-many&limit=20&workspace_id=workspace-main",
      expect.any(Object),
    )
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/api/v1/artifacts/artifact-many/policy?workspace_id=workspace-main",
      expect.any(Object),
    )

    await user.click(within(detailDialog).getByRole("button", { name: "Show 4 more documents" }))

    expect(within(detailDialog).getByRole("button", { name: /doc-8\.md/ })).toBeInTheDocument()
    expect(within(detailDialog).getByRole("button", { name: "Show fewer documents" })).toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("supports Context Pack copy and markdown download from work detail", async () => {
    let downloadedBlob: Blob | undefined
    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockImplementation((blob) => {
      if (blob instanceof Blob) downloadedBlob = blob
      return "blob:context-pack"
    })
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined)
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined)
    renderApp("/work/SG-155")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Handoff" }))
    expect(screen.getAllByRole("button", { name: "Copy resume command" })).toHaveLength(1)
    await user.click(screen.getByRole("button", { name: "Copy handoff" }))
    expect(await screen.findByRole("button", { name: "Handoff copied" })).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Download .md" }))
    expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob))
    expect(click).toHaveBeenCalled()
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:context-pack")
    await expect(downloadedBlob?.text()).resolves.toContain("specgate work context SG-155")

    createObjectURL.mockRestore()
    revokeObjectURL.mockRestore()
    click.mockRestore()
  })

  it("hands a Ready Context Pack to one selected Linear team and reads back the linked issue", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let handedOff = false
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core" }] })))
      if (url.endsWith("/workboard/change-requests")) return Promise.resolve(new Response(JSON.stringify({ items: [{
        id: "cr-linear", key: "CR-LINEAR", phase: "Ready", work_type: "cleanup", title: "Linear handoff", intent_md: "Hand off an approved Context Pack.",
        created_by: "DX", created_at: "2026-07-21T09:00:00Z", updated_at: "2026-07-21T09:00:00Z",
      }] })))
      if (url.endsWith("/api/v1/work-items/cr-linear/context-pack")) return Promise.resolve(new Response(JSON.stringify({ state: "assembled", markdown: "# Ready handoff" })))
      if (url.endsWith("/workboard/change-requests/cr-linear/tracker-links")) return Promise.resolve(new Response(JSON.stringify({ items: handedOff ? [{ identifier: "ENG-42", url: "https://linear.app/acme/issue/ENG-42", state: "opened" }] : [] })))
      if (url.endsWith("/integrations")) return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "int-linear", provider: "linear", name: "Linear", status: "connected", has_oauth_token: true }] })))
      if (url.endsWith("/integrations/int-linear/resources")) return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "team-platform", resource_type: "team", external_key: "platform", display_name: "Platform" }] })))
      if (url.endsWith("/workboard/change-requests/cr-linear/linear-handoff") && init?.method === "POST") {
        handedOff = true
        return Promise.resolve(new Response(JSON.stringify({ created: true, link: { identifier: "ENG-42", url: "https://linear.app/acme/issue/ENG-42", state: "opened" } })))
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-LINEAR")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Handoff" }))
    expect(await screen.findByRole("button", { name: "Copy handoff" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Hand off to Linear" })).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => fixtureURL(input).endsWith("/integrations"))).toBe(false)

    await user.click(screen.getByRole("button", { name: "Hand off to Linear" }))
    const dialog = await screen.findByRole("dialog", { name: "Hand off to Linear" })
    expect(await within(dialog).findByText("Platform")).toBeInTheDocument()
    await user.click(within(dialog).getByRole("button", { name: "Hand off to Linear" }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/workboard/change-requests/cr-linear/linear-handoff?workspace_id=workspace-main",
      expect.objectContaining({ method: "POST" }),
    ))
    expect(await screen.findByText("ENG-42")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Hand off to Linear" })).not.toBeInTheDocument()
  })

  it("renders actionable open, exact, and stale repository observations", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core" }] })))
      if (url.endsWith("/workboard/change-requests")) return Promise.resolve(new Response(JSON.stringify({ items: [{
        id: "cr-repository", key: "CR-REPOSITORY", phase: "Review", work_type: "cleanup", title: "Repository observations", intent_md: "Read persisted delivery links.",
        created_by: "DX", created_at: "2026-07-21T09:00:00Z", updated_at: "2026-07-21T09:00:00Z",
      }] })))
      if (url.endsWith("/api/v1/work-items/cr-repository/delivery-status?detail=true")) return Promise.resolve(new Response(JSON.stringify({ found: true, verdict: "pass", git_receipt: { head_revision: "submitted-head" } })))
      if (url.endsWith("/workboard/change-requests/cr-repository/delivery-links")) return Promise.resolve(new Response(JSON.stringify({ items: [
        { external_key: "#12", title: "Exact", url: "https://github.test/pull/12", state: "merged", head_sha: "submitted-head", merge_commit_sha: "merge-12", updated_at: "2026-07-21T09:00:00Z" },
        { external_key: "#13", title: "Stale", url: "https://github.test/pull/13", state: "merged", head_sha: "old-head", merge_commit_sha: "merge-13", updated_at: "2026-07-21T09:00:00Z" },
        { external_key: "#14", title: "Open", url: "https://github.test/pull/14", state: "opened", head_sha: "submitted-head", updated_at: "2026-07-21T09:00:00Z" },
      ] })))
      return emptyRegistryResponse(input)
    }))
    renderApp("/work/CR-REPOSITORY")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Verification" }))
    expect(await screen.findByText("Submitted commit observed on merged PR/MR.")).toBeInTheDocument()
    expect(screen.getByText("PR/MR merged, but it does not match the latest submitted commit. Update or resubmit, then merge the matching head.")).toBeInTheDocument()
    expect(screen.getByText("PR/MR linked; merge it before repository observation can corroborate delivery.")).toBeInTheDocument()
  })

  it("previews and copies the registry-derived Context Pack in work detail", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let downloadedBlob: Blob | undefined
    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockImplementation((blob) => {
      if (blob instanceof Blob) downloadedBlob = blob
      return "blob:canonical-context-pack"
    })
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined)
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined)
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/workboard/change-requests")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-live-pack",
                  key: "CR-LIVE-PACK",
                  feature_id: "feature-pack",
                  work_type: "cleanup",
                  title: "Registry-derived Context Pack preview",
                  intent_md: "Show the exact handoff material that CLI and IDE agents read.",
                  created_by: "DX",
                  created_at: "2026-06-27T05:00:00Z",
                  updated_at: "2026-06-27T05:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/work-items/cr-live-pack/context-pack")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              state: "assembled",
              markdown:
                "# Registry-derived Context Pack\n\n## Execution Brief\n\nUse the registry-derived pack, not a browser summary.\n\n## Resume\n\n```bash\nspecgate work context cr-live-pack\n```",
              change_request_id: "cr-live-pack",
              feature_id: "feature-pack",
              source_artifact_id: "artifact-pack",
              warnings: [{ code: "linked_knowledge_newer", message: "Linked knowledge changed after handoff." }],
              knowledge_provenance: [{ document_id: "knowledge-1", title: "Domain terms", version: "v1.0" }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-LIVE-PACK")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Handoff" }))

    expect(await screen.findByText("Registry-derived Context Pack")).toBeInTheDocument()
    expect(screen.getByText("Use the registry-derived pack, not a browser summary.")).toBeInTheDocument()
    expect(screen.getByText("Linked knowledge changed after handoff.")).toBeInTheDocument()
    expect(screen.getByText("Domain terms")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Copy handoff" }))
    expect(await screen.findByRole("button", { name: "Handoff copied" })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Download .md" }))
    await expect(downloadedBlob?.text()).resolves.toContain("# Registry-derived Context Pack")
    await expect(downloadedBlob?.text()).resolves.toContain("specgate work context cr-live-pack")
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/work-items/cr-live-pack/context-pack?workspace_id=workspace-main", expect.any(Object))

    createObjectURL.mockRestore()
    revokeObjectURL.mockRestore()
    click.mockRestore()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("disables live handoff markdown actions when Context Pack readback fails", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/workboard/change-requests")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-live-pack-error",
                  key: "CR-LIVE-PACK-ERROR",
                  feature_id: "feature-pack-error",
                  work_type: "cleanup",
                  title: "Context Pack outage",
                  intent_md: "Do not hand off browser fallback markdown when registry readback fails.",
                  created_by: "DX",
                  created_at: "2026-06-27T06:00:00Z",
                  updated_at: "2026-06-27T06:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/work-items/cr-live-pack-error/context-pack")) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "context pack unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-LIVE-PACK-ERROR")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Handoff" }))

    expect(await screen.findByText(/^Context Pack unavailable\./)).toBeInTheDocument()
    expect(screen.getByText(/no fallback handoff markdown is copied or downloaded/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Copy handoff" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Download .md" })).toBeDisabled()
    expect(screen.queryByText("Context Pack preview")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("disables live handoff markdown actions when Context Pack readback has no markdown", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/workboard/change-requests")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-live-pack-pending",
                  key: "CR-LIVE-PACK-PENDING",
                  feature_id: "feature-pack-pending",
                  work_type: "cleanup",
                  title: "Context Pack unavailable",
                  intent_md: "Do not hand off browser fallback markdown when Context Pack readback has no markdown.",
                  created_by: "DX",
                  created_at: "2026-06-27T07:00:00Z",
                  updated_at: "2026-06-27T07:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/work-items/cr-live-pack-pending/context-pack")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              state: "pending",
              markdown: "",
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-LIVE-PACK-PENDING")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Handoff" }))

    expect(await screen.findByText(/^Context Pack unavailable\./)).toBeInTheDocument()
    expect(screen.getByText(/no fallback handoff markdown is copied or downloaded/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Copy handoff" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Download .md" })).toBeDisabled()
    expect(screen.queryByText("Context Pack preview")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("moves keyboard users past the persistent navigation without changing routes", async () => {
    renderApp("/reviews")
    const user = userEvent.setup()

    await user.click(screen.getByRole("button", { name: "Skip to main content" }))

    expect(document.activeElement).toHaveAttribute("id", "main-content")
    expect(screen.getByRole("heading", { name: "Reviews" })).toBeInTheDocument()
  })

  it("opens a settings section page when a settings query arrives while Settings is already open", async () => {
    let navigateToGovernanceSettings: (() => void) | undefined
    function DeepLinkProbe() {
      const navigate = useNavigate()
      navigateToGovernanceSettings = () => navigate("/work?settings=governance")
      return null
    }

    render(
      <MemoryRouter initialEntries={["/work"]}>
        <App />
        <DeepLinkProbe />
      </MemoryRouter>,
    )
    const user = userEvent.setup()

    await user.click(screen.getByRole("button", { name: "Settings" }))
    const settingsDialog = await screen.findByRole("dialog", { name: "Settings" })
    expect(within(settingsDialog).getByText("General settings")).toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: "Back to settings sections" })).not.toBeInTheDocument()

    await act(async () => {
      navigateToGovernanceSettings?.()
    })

    expect(within(settingsDialog).getByRole("button", { name: "Back to settings sections" })).toBeInTheDocument()
    expect(await within(settingsDialog).findByText("Team rubric Skills")).toBeInTheDocument()
    expect(await within(settingsDialog).findByText("No team rubric Skills found.")).toBeInTheDocument()
  })

  it("keeps implementation tools and resources out of browser Settings", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/skills?workspace_id=workspace-main") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "skill-readiness",
                  name: "checking-spec-readiness",
                  description: "Review artifacts for minimum executable contract coverage.",
                  prompt: "Check goal, scope, non-goals, acceptance criteria, constraints, risks, and verification.",
                  created_at: "2026-06-20T10:00:00Z",
                  updated_at: "2026-06-27T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { settingsDialog } = await openSettings()

    expect(within(settingsDialog).queryByText("Team rubric Skills")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Agent tool catalog")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Resources")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("specgate://skills")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("search_knowledge")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Execute tool|Run tool|Rotate token/i })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText(/API key/i)).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/skills", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("manages registry team rubric Skills without plugin mutation controls", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/skills?workspace_id=workspace-main") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "skill-readiness",
                  name: "checking-spec-readiness",
                  description: "Review artifacts for minimum executable contract coverage.",
                  prompt: "Check goal, scope, non-goals, acceptance criteria, constraints, risks, and verification.",
                  created_at: "2026-06-20T10:00:00Z",
                  updated_at: "2026-06-27T10:00:00Z",
                },
                {
                  id: "skill-delivery",
                  name: "completing-delivery",
                  description: "Report implementation evidence against approved Context Pack criteria.",
                  prompt: "Collect evidence and submit delivery feedback.",
                  created_at: "2026-06-20T10:00:00Z",
                  updated_at: "2026-06-28T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/api/v1/skills/skill-readiness?workspace_id=workspace-main") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              body: {
                id: "skill-readiness",
                name: "checking-spec-readiness",
                description: "Review artifacts for minimum executable contract coverage.",
                prompt: "# Checking spec readiness\n\nCheck goal, scope, non-goals, acceptance criteria, constraints, risks, and verification.",
                created_at: "2026-06-20T10:00:00Z",
                updated_at: "2026-06-27T10:00:00Z",
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/skills" && init?.method === "POST") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              body: {
                id: "skill-created",
                name: "delivery-evidence",
                description: "Use when reviewing delivery evidence.",
                prompt: "# Delivery evidence\n\nCheck changed files, tests, and docs.",
                created_at: "2026-06-30T10:00:00Z",
                updated_at: "2026-06-30T10:00:00Z",
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/skills/skill-readiness?workspace_id=workspace-main" && init?.method === "PUT") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              body: {
                id: "skill-readiness",
                name: "checking-spec-readiness",
                description: "Use when evaluating readiness evidence.",
                prompt: "# Checking spec readiness\n\nCheck approved evidence only.",
                created_at: "2026-06-20T10:00:00Z",
                updated_at: "2026-06-30T10:05:00Z",
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/skills/skill-readiness?workspace_id=workspace-main" && init?.method === "DELETE") {
        return Promise.resolve(new Response(JSON.stringify({ body: { ok: true } }), { headers: { "Content-Type": "application/json" } }))
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    expect(within(settingsDialog).queryByText("Team rubric Skills")).not.toBeInTheDocument()
    await user.click(within(settingsDialog).getByRole("button", { name: "GovernanceSkills and policy tiers" }))

    expect(await within(settingsDialog).findByText("Team rubric Skills")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("checking-spec-readiness")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText(/^Registry$/)).not.toBeInTheDocument()
    const skillDescription = within(settingsDialog).getByText("Review artifacts for minimum executable contract coverage.")
    expect(skillDescription).toHaveClass("break-words")
    expect(within(settingsDialog).getByText("completing-delivery")).toBeInTheDocument()
    expect(within(settingsDialog).getByRole("button", { name: "Add Skill" })).toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Install skill/i })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/skills?workspace_id=workspace-main", expect.any(Object))

    await user.click(within(settingsDialog).getByRole("button", { name: "Add Skill" }))
    const addDialog = await screen.findByRole("dialog", { name: "Add Skill" })
    await user.type(within(addDialog).getByLabelText("Skill name"), "delivery-evidence")
    await user.type(within(addDialog).getByLabelText("Description"), "Use when reviewing delivery evidence.")
    await user.type(within(addDialog).getByLabelText("Prompt"), "# Delivery evidence\n\nCheck changed files, tests, and docs.")
    await user.click(within(addDialog).getByRole("button", { name: "Create Skill" }))
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add Skill" })).not.toBeInTheDocument())
    expect(within(settingsDialog).getByText("delivery-evidence")).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/skills",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          name: "delivery-evidence",
          description: "Use when reviewing delivery evidence.",
          prompt: "# Delivery evidence\n\nCheck changed files, tests, and docs.",
          workspace_id: "workspace-main",
        }),
      }),
    )

    await user.click(within(settingsDialog).getByRole("button", { name: "Manage checking-spec-readiness Skill" }))
    const detailDialog = await screen.findByRole("dialog", { name: "checking-spec-readiness" })
    expect(within(detailDialog).getByText("Check goal, scope, non-goals, acceptance criteria, constraints, risks, and verification.")).toBeInTheDocument()
    await user.click(within(detailDialog).getByRole("button", { name: "Edit Skill" }))
    expect(screen.queryByRole("dialog", { name: "Edit Skill" })).not.toBeInTheDocument()
    await user.clear(within(detailDialog).getByLabelText("Description"))
    await user.type(within(detailDialog).getByLabelText("Description"), "Use when evaluating readiness evidence.")
    await user.clear(within(detailDialog).getByLabelText("Prompt"))
    await user.type(within(detailDialog).getByLabelText("Prompt"), "# Checking spec readiness\n\nCheck approved evidence only.")
    await user.click(within(detailDialog).getByRole("button", { name: "Save Skill" }))
    await waitFor(() => expect(within(detailDialog).queryByLabelText("Prompt")).not.toBeInTheDocument())
    expect(screen.getByText("Check approved evidence only.")).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/skills/skill-readiness?workspace_id=workspace-main",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          name: "checking-spec-readiness",
          description: "Use when evaluating readiness evidence.",
          prompt: "# Checking spec readiness\n\nCheck approved evidence only.",
        }),
      }),
    )

    await user.click(within(detailDialog).getByRole("button", { name: "Delete Skill" }))
    const deleteDialog = await screen.findByRole("dialog", { name: "Delete checking-spec-readiness Skill?" })
    await user.click(within(deleteDialog).getByRole("button", { name: "Delete Skill" }))
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "checking-spec-readiness" })).not.toBeInTheDocument())
    expect(within(settingsDialog).queryByText("checking-spec-readiness")).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/skills/skill-readiness?workspace_id=workspace-main", expect.objectContaining({ method: "DELETE" }))
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/skills/skill-readiness?workspace_id=workspace-main", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not render live registry Skills without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/policies/levels") {
        return Promise.resolve(new Response(JSON.stringify({ levels: [] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url === "http://registry.test/api/v1/skills?workspace_id=workspace-main") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  name: "missing-id-skill",
                  description: "This Skill should not be editable without a registry id.",
                  prompt: "No durable id.",
                  updated_at: "2026-06-30T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: "GovernanceSkills and policy tiers" }))

    expect(await within(settingsDialog).findByText("Team rubric Skills")).toBeInTheDocument()
    expect(await within(settingsDialog).findByText("No team rubric Skills found.")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("missing-id-skill")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: "Manage missing-id-skill Skill" })).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/skills/missing-id-skill?workspace_id=workspace-main", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("retries team rubric Skills without reloading policy reference", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let skillRequests = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/skills?workspace_id=workspace-main") {
        skillRequests += 1
        if (skillRequests === 1) return Promise.resolve(new Response("{}", { status: 503 }))
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "skill-readiness",
                  name: "checking-spec-readiness",
                  description: "Review artifacts for executable handoff coverage.",
                  prompt: "Check goal, scope, non-goals, acceptance criteria, constraints, risks, and verification.",
                  updated_at: "2026-06-27T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: "GovernanceSkills and policy tiers" }))

    await within(settingsDialog).findByText("Team rubric Skills unavailable.")
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/governance-profiles", expect.any(Object))
    await user.click(within(settingsDialog).getByRole("button", { name: "Retry Skills" }))
    expect(await within(settingsDialog).findByText("checking-spec-readiness")).toBeInTheDocument()
    expect(skillRequests).toBe(2)

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("lazy-loads independent policy reference sections and expands compact row details", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/policies/levels") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              levels: [
                {
                  governance_level: "enhanced",
                  display_name: "Enhanced",
                  approval_policy: "human_required",
                  evidence_policy: "corroborated_required",
                  required_roles: ["PM", "QA"],
                  required_topics: ["risk"],
                  required_evidence: ["test_report"],
                  enabled_gates: ["readiness", "delivery_review"],
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /GovernanceSkills and policy tiers/ }))

    expect(within(settingsDialog).queryByRole("heading", { name: "Governance" })).not.toBeInTheDocument()
    expect(within(settingsDialog).getByText("Policy reference")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Live")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Governance health")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/governance-profiles", expect.any(Object))
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/policies/levels", expect.any(Object))

    await user.click(within(settingsDialog).getByText("Policy reference"))
    expect(await within(settingsDialog).findByText("1 tier")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Enhanced")).toBeInTheDocument()
    expect(within(settingsDialog).getAllByText("human_required").length).toBeGreaterThan(0)
    expect(within(settingsDialog).getAllByText("corroborated_required").length).toBeGreaterThan(0)
    const enhancedRow = within(settingsDialog).getByText("Enhanced").closest("details")
    expect(enhancedRow).not.toBeNull()
    expect(within(enhancedRow!).getByText("2 gates · 1 evidence · 2 roles · 1 topic")).toBeVisible()
    expect(within(enhancedRow!).getByText("delivery_review")).not.toBeVisible()
    await user.click(within(enhancedRow!).getByText("Enhanced"))
    expect(within(enhancedRow!).getByText("delivery_review")).toBeVisible()
    expect(within(enhancedRow!).getByText("PM")).toBeVisible()
    expect(within(enhancedRow!).getByText("risk")).toBeVisible()
    expect(within(enhancedRow!).getByText("test_report")).toBeVisible()
    expect(within(enhancedRow!).getByText("enhanced")).toBeVisible()
    expect(within(settingsDialog).queryByRole("button", { name: /Import profile|Activate policy|Accept exception|Resolve policy|Record feedback/i })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/policies/levels", expect.any(Object))
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/policy-health", expect.any(Object))
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/outcome-feedback", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows automatic policy tiers without fetching removed profile catalogs", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let levelRequests = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/policies/levels")) {
        levelRequests += 1
        return Promise.resolve(new Response(JSON.stringify({ levels: [{ governance_level: "standard", display_name: "Standard" }] })))
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /GovernanceSkills and policy tiers/ }))
    await user.click(within(settingsDialog).getByText("Policy reference"))

    expect(await within(settingsDialog).findByText("Standard")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Profiles unavailable.")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Recovered profile")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Profiles")).not.toBeInTheDocument()
    expect(levelRequests).toBe(1)
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/governance-profiles?workspace_id=workspace-main", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("keeps registry diagnostics out of browser Settings", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { settingsDialog } = await openSettings()

    expect(within(settingsDialog).queryByRole("button", { name: /Diagnostics|Registry and CLI/ })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("heading", { name: "Diagnostics" })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Registry build")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/meta", expect.any(Object))
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/status", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("lets mobile Settings sections return to the section list", async () => {
    const { user, settingsDialog } = await openSettings()

    expect(within(settingsDialog).queryByRole("button", { name: "Back to settings sections" })).not.toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))

    expect(within(settingsDialog).getByRole("button", { name: "Back to settings sections" })).toBeInTheDocument()
    expect(within(settingsDialog).getByRole("heading", { name: "Integrations" })).toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Back to settings sections" }))

    expect(within(settingsDialog).queryByRole("button", { name: "Back to settings sections" })).not.toBeInTheDocument()
    expect(within(settingsDialog).getByRole("button", { name: /Models/ })).toBeInTheDocument()
    expect(within(settingsDialog).getByRole("button", { name: /General/ })).toBeInTheDocument()
  })

  it("does not expose Settings as a standalone route", () => {
    renderApp("/settings")

    expect(screen.getByRole("heading", { name: "Work" })).toBeInTheDocument()
    expect(screen.queryByRole("dialog", { name: "Settings" })).not.toBeInTheDocument()
  })

  it("opens Settings Integrations from the OAuth return query", async () => {
    renderApp("/work?settings=integrations")

    const settingsDialog = await screen.findByRole("dialog", { name: "Settings" })
    expect(await within(settingsDialog).findByRole("heading", { name: "Integrations" })).toBeInTheDocument()
    expect(within(settingsDialog).getByRole("button", { name: /Integrations/ })).toHaveAttribute("aria-current", "page")
  })

  it("shows current workspace members in Settings", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (
        url ===
        "http://registry.test/api/v1/workspaces/workspace-main/members?current_user_id=user-local&current_username=thanhtung"
      ) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              workspace: { id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" },
              members: [
                {
                  workspace_id: "workspace-main",
                  user_id: "user-owner",
                  username: "ada",
                  display_name: "Ada Lovelace",
                  email: "ada@example.com",
                  role: "owner",
                },
                {
                  workspace_id: "workspace-main",
                  user_id: "user-local",
                  username: "thanhtung",
                  display_name: "Tung Local",
                  email: "tung@example.com",
                  role: "member",
                  current: true,
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Workspace/ }))

    expect(await within(settingsDialog).findByRole("heading", { name: "Team members" })).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Ada Lovelace")).toBeInTheDocument()
    expect(within(settingsDialog).getByText(/@ada/)).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Tung Local")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("You")).toBeInTheDocument()
    await user.hover(within(settingsDialog).getByText("Ada Lovelace"))
    expect(await screen.findByRole("tooltip", { name: "Ada Lovelace member details" })).toBeInTheDocument()
    expect(screen.getByText("Workspace member")).toBeInTheDocument()
    expect(screen.getByText("ada@example.com")).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/api/v1/workspaces/workspace-main/members?current_user_id=user-local&current_username=thanhtung",
      expect.any(Object),
    )
  })

  it("preserves Settings return query when redirecting the retired Settings route", async () => {
    renderApp("/settings?settings=integrations")

    const settingsDialog = await screen.findByRole("dialog", { name: "Settings" })
    expect(await within(settingsDialog).findByRole("heading", { name: "Integrations" })).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Work" })).toBeInTheDocument()
  })

  it("adds an integration through Doc Registry", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let createdBody: Record<string, string> | null = null
    let tokenBody: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations" && init?.method === "POST") {
        createdBody = JSON.parse(String(init.body)) as Record<string, string>
        return Promise.resolve({
          ok: true,
          json: async () => ({
            id: "int-github",
            provider: "github",
            name: createdBody?.name,
            status: "connected",
            base_url: createdBody?.base_url,
            config_json: createdBody?.config_json,
            auth_method: "pat",
            has_api_token: false,
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-github/api-token?workspace_id=workspace-main" && init?.method === "PUT") {
        tokenBody = JSON.parse(String(init.body)) as Record<string, string>
        return Promise.resolve({ ok: true, status: 204, text: async () => "" } as Response)
      }
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    expect(await within(settingsDialog).findByText("No integrations connected yet. Add GitHub, GitLab, or Linear when you need provider signals.")).toBeInTheDocument()
    await user.click(within(settingsDialog).getByRole("button", { name: "Add integration" }))

    const addDialog = screen.getByRole("dialog", { name: "Add integration" })
    const nameInput = within(addDialog).getByPlaceholderText("GitHub")
    await user.clear(nameInput)
    await user.type(nameInput, "GitHub workspace")
    expect(within(addDialog).queryByText("Base URL")).not.toBeInTheDocument()
    await user.click(within(addDialog).getByRole("button", { name: /API token/ }))
    await user.type(within(addDialog).getByPlaceholderText("ghp_..."), "github-test-token")
    await user.click(within(addDialog).getByRole("button", { name: "Add integration" }))

    await waitFor(() => expect(createdBody).not.toBeNull())
    expect(createdBody).toMatchObject({
      provider: "github",
      name: "GitHub workspace",
      config_json: JSON.stringify({ enabled: true }),
    })
    expect(createdBody).not.toHaveProperty("base_url")
    expect(tokenBody).toEqual({ api_token: "github-test-token" })
    expect(await within(settingsDialog).findByText("GitHub workspace")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("API token stored. Link resources here; reprovisioning, disconnect, and webhook-secret management stay with backend/admin flows.")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("No linked resources yet. Use Link resource to register a repository or team; webhook management stays in backend/admin flows.")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Loading linked resources...")).not.toBeInTheDocument()
  })

  it("does not render live integrations without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                provider: "github",
                name: "Missing id integration",
                status: "connected",
                auth_method: "pat",
                has_api_token: true,
              },
            ],
          }),
        } as Response)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))

    expect(await within(settingsDialog).findByText("No integrations connected yet. Add GitHub, GitLab, or Linear when you need provider signals.")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Missing id integration")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/integrations/undefined/resources")
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/integrations/undefined/webhook-events?limit=3")

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows linked integration resources as read-only health", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                id: "int-gitlab",
                provider: "gitlab",
                name: "GitLab delivery",
                status: "connected",
                auth_method: "pat",
                has_api_token: true,
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-gitlab/resources?workspace_id=workspace-main") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                id: "res-web",
                resource_type: "project",
                external_key: "specgate/web",
                display_name: "SpecGate Web",
                default_ref: "main",
                has_webhook_secret: true,
                config_json: JSON.stringify({
                  webhook_status: "connected",
                  provider_webhook_id: "hook-42",
                }),
              },
              {
                id: "res-secret",
                resource_type: "project",
                external_key: "specgate/legacy",
                display_name: "Legacy Repo",
                has_webhook_secret: true,
                config_json: "{not-json",
              },
              {
                id: "res-error",
                resource_type: "repo",
                external_key: "specgate/broken",
                display_name: "Broken Hook",
                config_json: JSON.stringify({
                  webhook_status: "error",
                  webhook_last_error: "permission denied",
                }),
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-gitlab/webhook-events?workspace_id=workspace-main&limit=3") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                id: "webhook-1",
                event_type: "tracker_issue",
                status: "processed",
                correlation_id: "CR-7D0AD9AF",
                received_at: "2026-06-30T06:01:00Z",
              },
              {
                id: "webhook-2",
                event_type: "comment",
                status: "failed",
                error: "signature mismatch",
                received_at: "2026-06-30T06:00:00Z",
              },
            ],
          }),
        } as Response)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    expect(await within(settingsDialog).findByText("GitLab delivery")).toBeInTheDocument()

    // Details are lazy: nothing is fetched per integration until it is expanded.
    expect(within(settingsDialog).queryByText("Linked resources")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/integrations/int-gitlab/resources?workspace_id=workspace-main")
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/integrations/int-gitlab/webhook-events?workspace_id=workspace-main&limit=3")

    await user.click(within(settingsDialog).getByRole("button", { name: "Show details" }))
    expect(await within(settingsDialog).findByText("Linked resources")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("SpecGate Web")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Ref main")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Webhook connected")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Provider hook hook-42")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Legacy Repo")).toBeInTheDocument()
    expect(within(settingsDialog).getAllByText("Webhook connected")).toHaveLength(1)
    expect(within(settingsDialog).getAllByText("Webhook secret stored").length).toBeGreaterThan(0)
    expect(within(settingsDialog).getByText("Broken Hook")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Webhook error")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("permission denied")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Recent webhooks")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Tracker issue")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("processed")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("CR-7D0AD9AF")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Comment")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("failed")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("signature mismatch")).toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Reprovision/ })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Delete resource/ })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Record webhook/ })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/integrations/int-gitlab/resources?workspace_id=workspace-main")
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/integrations/int-gitlab/webhook-events?workspace_id=workspace-main&limit=3")
  })

  it("links a repository resource through Doc Registry without admin controls", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let linkedBody: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                id: "int-github",
                provider: "github",
                name: "GitHub delivery",
                status: "connected",
                auth_method: "pat",
                has_api_token: true,
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-github/resources?workspace_id=workspace-main") {
        if (init?.method === "POST") {
          linkedBody = JSON.parse(String(init.body)) as Record<string, string>
          return Promise.resolve({
            ok: true,
            json: async () => ({
              id: "res-api",
              resource_type: linkedBody?.resource_type,
              external_id: linkedBody?.external_id,
              external_key: linkedBody?.external_key,
              display_name: linkedBody?.display_name,
              default_ref: linkedBody?.default_ref,
              has_webhook_secret: true,
              config_json: JSON.stringify({ webhook_status: "connected", provider_webhook_id: "hook-api" }),
            }),
          } as Response)
        }
        return emptyRegistryResponse(input)
      }
      if (url === "http://registry.test/integrations/int-github/repos?workspace_id=workspace-main&limit=50") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                external_id: "987",
                external_key: "specgate/api",
                display_name: "SpecGate API",
                default_ref: "main",
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-github/webhook-events?workspace_id=workspace-main&limit=3") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    expect(await within(settingsDialog).findByText("GitHub delivery")).toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Link resource" }))
    const linkDialog = await screen.findByRole("dialog", { name: "Link resource" })
    expect(await within(linkDialog).findByText("SpecGate API")).toBeInTheDocument()
    await user.click(within(linkDialog).getByRole("button", { name: "Link resource" }))

    await waitFor(() => expect(linkedBody).not.toBeNull())
    expect(linkedBody).toMatchObject({
      resource_type: "repo",
      external_id: "987",
      external_key: "specgate/api",
      display_name: "SpecGate API",
      default_ref: "main",
    })
    expect(await within(settingsDialog).findByText("Webhook connected")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Provider hook hook-api")).toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Reprovision/ })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Delete resource/ })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Rotate secret/ })).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not link integration resource candidates without registry keys", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let linkedBody: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                id: "int-github",
                provider: "github",
                name: "GitHub delivery",
                status: "connected",
                auth_method: "pat",
                has_api_token: true,
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-github/repos?workspace_id=workspace-main&limit=50") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                external_id: "987",
                display_name: "SpecGate API",
                default_ref: "main",
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-github/resources?workspace_id=workspace-main") {
        if (init?.method === "POST") {
          linkedBody = JSON.parse(String(init.body)) as Record<string, string>
        }
        return emptyRegistryResponse(input)
      }
      if (url === "http://registry.test/integrations/int-github/webhook-events?workspace_id=workspace-main&limit=3") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    expect(await within(settingsDialog).findByText("GitHub delivery")).toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Link resource" }))
    const linkDialog = await screen.findByRole("dialog", { name: "Link resource" })
    expect(await within(linkDialog).findByText("No repositories returned for this integration credential.")).toBeInTheDocument()
    expect(within(linkDialog).queryByText("SpecGate API")).not.toBeInTheDocument()
    expect(within(linkDialog).getByRole("button", { name: "Link resource" })).toBeDisabled()
    expect(linkedBody).toBeNull()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("only asks for integration Base URL for self-hosted API-token setup", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let createdBody: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations" && init?.method === "POST") {
        createdBody = JSON.parse(String(init.body)) as Record<string, string>
        return Promise.resolve({
          ok: true,
          json: async () => ({
            id: "int-gitlab",
            provider: "gitlab",
            name: createdBody?.name,
            status: "connected",
            base_url: createdBody?.base_url,
            config_json: createdBody?.config_json,
            auth_method: "pat",
            has_api_token: false,
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-gitlab/api-token?workspace_id=workspace-main" && init?.method === "PUT") {
        return Promise.resolve({ ok: true, status: 204, text: async () => "" } as Response)
      }
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    await user.click(await within(settingsDialog).findByRole("button", { name: "Add integration" }))

    const addDialog = screen.getByRole("dialog", { name: "Add integration" })
    await user.click(within(addDialog).getByRole("button", { name: /GitLab/ }))
    expect(within(addDialog).queryByText("Base URL")).not.toBeInTheDocument()

    await user.click(within(addDialog).getByRole("button", { name: /API token/ }))
    await user.click(within(addDialog).getByRole("checkbox", { name: /Self-hosted GitLab/ }))
    const baseUrlInput = within(addDialog).getByLabelText("Base URL")
    await user.clear(baseUrlInput)
    await user.type(baseUrlInput, "https://gitlab.example.com")
    await user.type(within(addDialog).getByPlaceholderText("glpat-..."), "gitlab-test-token")
    await user.click(within(addDialog).getByRole("button", { name: "Add integration" }))

    await waitFor(() => expect(createdBody).not.toBeNull())
    expect(createdBody).toMatchObject({
      provider: "gitlab",
      name: "GitLab",
      base_url: "https://gitlab.example.com",
    })
  })

  it("starts hosted integration OAuth without Base URL or API token", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let oauthBody: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations/oauth/begin" && init?.method === "POST") {
        oauthBody = JSON.parse(String(init.body)) as Record<string, string>
        return Promise.resolve({ ok: true, json: async () => ({ authorize_url: "#oauth-provider" }) } as Response)
      }
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    await user.click(await within(settingsDialog).findByRole("button", { name: "Add integration" }))

    const addDialog = screen.getByRole("dialog", { name: "Add integration" })
    await user.click(within(addDialog).getByRole("button", { name: /OAuth/ }))
    expect(within(addDialog).queryByText("Base URL")).not.toBeInTheDocument()
    expect(within(addDialog).queryByLabelText("API token")).not.toBeInTheDocument()
    await user.click(within(addDialog).getByRole("button", { name: "Add integration" }))

    await waitFor(() => expect(oauthBody).not.toBeNull())
    expect(oauthBody).toMatchObject({
      provider: "github",
      name: "GitHub",
      config_json: JSON.stringify({ enabled: true }),
    })
    expect(oauthBody).not.toHaveProperty("base_url")
    expect(oauthBody).toHaveProperty("redirect_target")
    expect(oauthBody!.redirect_target).toContain("settings=integrations")
    expect(window.location.hash).toBe("#oauth-provider")
    window.history.replaceState(null, "", "/")
  })

  it("shows an integration OAuth error when Doc Registry omits the authorize URL", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations/oauth/begin" && init?.method === "POST") {
        return Promise.resolve({ ok: true, json: async () => ({}) } as Response)
      }
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    await user.click(await within(settingsDialog).findByRole("button", { name: "Add integration" }))

    const addDialog = screen.getByRole("dialog", { name: "Add integration" })
    await user.click(within(addDialog).getByRole("button", { name: /OAuth/ }))
    await user.click(within(addDialog).getByRole("button", { name: "Add integration" }))

    expect(await within(addDialog).findByText(/provider authorization URL was not returned/i)).toBeInTheDocument()
    expect(window.location.hash).not.toBe("#oauth-provider")
  })

  it("loads and saves OpenRouter governance model settings", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let savedSettings: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("openrouter.ai")) {
        if (url.includes("output_modalities=embeddings")) {
          return Promise.resolve(
            new Response(JSON.stringify({
              data: [
                { id: "mistralai/mistral-embed-2312", name: "Mistral Embed", architecture: { output_modalities: ["embeddings"] } },
                { id: "someorg/chat-only", name: "Chat Only", architecture: { output_modalities: ["text"] } },
              ],
            })),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify({
            data: [
              { id: "deepseek/deepseek-v4-flash", name: "DeepSeek V4 Flash", architecture: { output_modalities: ["text"] } },
              { id: "anthropic/claude-sonnet-4.6", name: "Claude Sonnet 4.6", architecture: { output_modalities: ["text"] } },
              { id: "someorg/image-gen", name: "Image Gen", architecture: { output_modalities: ["image", "text"] } },
            ],
          })),
        )
      }
      if (url === "http://registry.test/settings" && init?.method === "PUT") {
        savedSettings = JSON.parse(String(init.body)).settings as Record<string, string>
        return Promise.resolve({ ok: true, json: async () => ({ settings: savedSettings }) } as Response)
      }
      if (url === "http://registry.test/settings") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            settings: {
              "governance.model_provider": "openrouter",
              "governance.model": "deepseek/deepseek-v4-flash",
              "governance.default_thinking_level": "medium",
              "embedding.model_provider": "openrouter",
              "embedding.model": "mistralai/mistral-embed-2312",
              "openrouter.api_key": "***",
            },
          }),
        } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /Models/ }))
    expect(await within(settingsDialog).findByText("DeepSeek V4 Flash")).toBeInTheDocument()
    expect(await within(settingsDialog).findByText("Claude Sonnet 4.6")).toBeInTheDocument()
    expect(await within(settingsDialog).findByText("Mistral Embed")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Chat Only")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Image Gen")).not.toBeInTheDocument()
    expect(within(settingsDialog).getByRole("button", { name: "Save" })).toBeDisabled()

    const search = within(settingsDialog).getByPlaceholderText("Search models or type a model ID")
    await user.type(search, "z-ai/glm-5.1")
    await user.click(await within(settingsDialog).findByRole("button", { name: /Use z-ai\/glm-5\.1/ }))
    expect(within(settingsDialog).getByRole("button", { name: "Save" })).toBeEnabled()
    await user.click(within(settingsDialog).getByRole("button", { name: "Save" }))

    await waitFor(() => expect(savedSettings).not.toBeNull())
    expect(savedSettings!["governance.model_provider"]).toBe("openrouter")
    expect(savedSettings!["governance.model"]).toBe("z-ai/glm-5.1")
    expect(savedSettings!["governance.default_thinking_level"]).toBe("medium")
    expect(savedSettings!["embedding.model_provider"]).toBe("openrouter")
    expect(savedSettings!["embedding.model"]).toBe("mistralai/mistral-embed-2312")
    expect(savedSettings!["openrouter.api_key"]).toBe("***")
  })

  it("loads and saves General governance defaults, auto-archive, and the retention sweeper toggle", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let savedSettings: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/settings" && init?.method === "PUT") {
        savedSettings = JSON.parse(String(init.body)).settings as Record<string, string>
        return Promise.resolve({ ok: true, json: async () => ({ settings: savedSettings }) } as Response)
      }
      if (url === "http://registry.test/settings") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            settings: {
              "governance.auto_archive_on_delivery_pass": "false",
              "governance.feature_freshness_sla_days": "7",
              "governance.artifact_stale_days": "5",
              "governancefiles.ttl_days": "90",
              "governance.gate_confidence_threshold": "0.7",
              "retention.artifact_sweep_enabled": "false",
            },
          }),
        } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /General/ }))
    expect(await within(settingsDialog).findByRole("heading", { name: "General settings" })).toBeInTheDocument()
    expect(within(settingsDialog).queryByLabelText("Auto-refresh Feature overviews")).not.toBeInTheDocument()
    expect(within(settingsDialog).getByLabelText("Auto-archive after human delivery acceptance")).not.toBeChecked()
    expect(within(settingsDialog).getByLabelText("Artifact retention sweeper")).not.toBeChecked()
    expect(within(settingsDialog).queryByLabelText("Feature freshness SLA days")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByLabelText("Artifact stale after days")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Operational boundaries")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Governance file retention")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByLabelText(/Governance file retention days/i)).not.toBeInTheDocument()

    await user.click(within(settingsDialog).getByLabelText("Auto-archive after human delivery acceptance"))
    await user.click(within(settingsDialog).getByLabelText("Artifact retention sweeper"))
    expect(within(settingsDialog).getByRole("button", { name: "Save" })).toBeEnabled()
    await user.click(within(settingsDialog).getByRole("button", { name: "Save" }))

    await waitFor(() => expect(savedSettings).not.toBeNull())
    expect(savedSettings).toEqual({
      "governance.auto_archive_on_delivery_pass": "true",
      "retention.artifact_sweep_enabled": "true",
    })
    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("runs workspace cleanup from General settings behind an explicit confirm", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let cleanupCalls = 0
    let cleanupURL = ""
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/workspaces") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }),
        } as Response)
      }
      if (url.startsWith("http://registry.test/maintenance/cleanup") && init?.method === "POST") {
        cleanupCalls++
        cleanupURL = url
        return Promise.resolve({
          ok: true,
          json: async () => ({
            expired_artifacts_deleted: 2,
            referenced_skipped: 1,
            demo_features_deleted: 3,
            demo_change_requests_deleted: 5,
            demo_artifacts_deleted: 12,
            archived_change_requests_deleted: 4,
          }),
        } as Response)
      }
      if (url === "http://registry.test/settings") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ settings: { "retention.artifact_sweep_enabled": "false" } }),
        } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /General/ }))
    expect(await within(settingsDialog).findByRole("heading", { name: "General settings" })).toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Clean up workspace" }))
    expect(cleanupCalls).toBe(0)
    expect(within(settingsDialog).getByText(/cannot be undone/i)).toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Cancel cleanup" }))
    expect(cleanupCalls).toBe(0)

    await user.click(within(settingsDialog).getByRole("button", { name: "Clean up workspace" }))
    await user.click(within(settingsDialog).getByRole("button", { name: "Yes, delete clutter" }))

    await waitFor(() => expect(cleanupCalls).toBe(1))
    expect(cleanupURL).toBe("http://registry.test/maintenance/cleanup?workspace_id=workspace-main")
    expect(await within(settingsDialog).findByText(/Cleanup complete/)).toBeInTheDocument()
    expect(within(settingsDialog).getByText(/2 expired artifacts/)).toBeInTheDocument()
    expect(within(settingsDialog).getByText(/4 archived work items/)).toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows saved custom model slugs even when they are missing from the catalog", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            data: [
              { id: "deepseek/deepseek-v4-flash", name: "DeepSeek V4 Flash", architecture: { output_modalities: ["text"] } },
              { id: "mistralai/mistral-embed-2312", name: "Mistral Embed", architecture: { output_modalities: ["embeddings"] } },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/settings") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            settings: {
              "governance.model_provider": "openrouter",
              "governance.model": "vendor/private-governance",
              "governance.default_thinking_level": "medium",
              "embedding.model_provider": "openrouter",
              "embedding.model": "vendor/private-embedding",
              "openrouter.api_key": "***",
            },
          }),
        } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /Models/ }))

    expect(await within(settingsDialog).findByRole("button", { name: /vendor\/private-governance/ })).toHaveTextContent(
      "Selected",
    )
    expect(await within(settingsDialog).findByRole("button", { name: /vendor\/private-embedding/ })).toHaveTextContent(
      "Selected",
    )
  })

  it("accepts an explicit provider model ID outside the built-in suggestions", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/settings") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            settings: {
              "governance.model_provider": "openai",
              "governance.model": "gpt-5.4-mini",
            },
          }),
        } as Response)
      }
      return emptyRegistryResponse(input)
    }))

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Models/ }))
    const modelSearch = await within(settingsDialog).findByPlaceholderText("Search models or type a model ID")
    await user.type(modelSearch, "future-provider-model")

    expect(within(settingsDialog).getByRole("button", { name: /Use future-provider-model/ })).toBeInTheDocument()
  })

  it("groups integrations by repository and work-tracking roles without an experimental label", async () => {
    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))

    expect(await within(settingsDialog).findByRole("heading", { name: "Repositories" })).toBeInTheDocument()
    expect(within(settingsDialog).getByRole("heading", { name: "Work tracking" })).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Experimental")).not.toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Add integration" }))
    const addDialog = screen.getByRole("dialog", { name: "Add integration" })
    expect(within(addDialog).getByText("Marked pull requests and exact submitted commits.")).toBeInTheDocument()
    expect(within(addDialog).getByText("Marked merge requests and exact submitted commits.")).toBeInTheDocument()
    expect(within(addDialog).getByText("Optional handoff of approved work to a Linear team.")).toBeInTheDocument()
    expect(within(addDialog).getByRole("button", { name: /OAuth/ })).toHaveClass("border-primary/70")
  })

  it("redirects unknown sections back to Work", () => {
    renderApp("/not-a-section")

    expect(screen.getByRole("heading", { name: "Work" })).toBeInTheDocument()
  })
})
