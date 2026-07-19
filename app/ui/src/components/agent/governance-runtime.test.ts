import { describe, expect, it, vi } from "vitest"

import {
  createLangGraphThreadListAdapter,
  getGovernanceRuntimeConfig,
  readLangGraphErrorMessage,
  withGovernanceWorkspaceRunConfig,
} from "@/components/agent/governance-runtime"

describe("governance runtime config", () => {
  it("uses the local runtime when LangGraph is not configured", () => {
    const env = { VITE_LANGGRAPH_API_URL: "" } as unknown as ImportMetaEnv

    expect(getGovernanceRuntimeConfig(env)).toEqual({
      mode: "local",
      assistantId: "governance",
    })
  })

  it("uses the LangGraph runtime when the API URL is configured", () => {
    const env = { VITE_LANGGRAPH_API_URL: " http://127.0.0.1:2024 " } as unknown as ImportMetaEnv

    expect(getGovernanceRuntimeConfig(env)).toEqual({
      mode: "langgraph",
      apiUrl: "http://127.0.0.1:2024",
      assistantId: "governance",
    })
  })

  it("allows the Vite dev proxy path for the LangGraph runtime", () => {
    const env = { VITE_LANGGRAPH_API_URL: " /api/agents " } as unknown as ImportMetaEnv

    expect(getGovernanceRuntimeConfig(env)).toEqual({
      mode: "langgraph",
      apiUrl: "http://localhost:5173/api/agents",
      assistantId: "governance",
    })
  })

  it("turns provider rate limits into a readable chat error", () => {
    expect(readLangGraphErrorMessage({ message: "Provider returned error", status_code: 429 })).toBe(
      "The model provider is rate-limited. Wait a moment or configure a provider key in Models.",
    )
  })
})

describe("LangGraph governance thread list adapter", () => {
  it("maps archived thread metadata into the assistant-ui thread status", async () => {
    const client = {
      threads: {
        search: vi.fn().mockResolvedValue([
          {
            thread_id: "thread-active",
            metadata: { source: "specgate-ui", surface: "governance-agent", workspace_id: "ws-a", title: "Active thread" },
            updated_at: "2026-06-30T00:00:00Z",
          },
          {
            thread_id: "thread-archived",
            metadata: { source: "specgate-ui", surface: "governance-agent", workspace_id: "ws-a", title: "Done", archived: true },
            updated_at: "2026-06-29T00:00:00Z",
          },
        ]),
      },
    }

    const adapter = createLangGraphThreadListAdapter(client as never, "http://agents.test", "ws-a")
    const result = await adapter.list()

    expect(result.threads).toEqual([
      expect.objectContaining({ remoteId: "thread-active", status: "regular", title: "Active thread" }),
      expect.objectContaining({ remoteId: "thread-archived", status: "archived", title: "Done" }),
    ])
    expect(client.threads.search).toHaveBeenCalledWith(expect.objectContaining({
      metadata: { source: "specgate-ui", surface: "governance-agent", workspace_id: "ws-a" },
    }))
  })

  it("creates threads pinned to the active workspace", async () => {
    const create = vi.fn().mockResolvedValue({ thread_id: "thread-a" })
    const client = { threads: { create } }

    const adapter = createLangGraphThreadListAdapter(client as never, "http://agents.test", "ws-a")
    await expect(adapter.initialize("draft-a")).resolves.toEqual({ remoteId: "thread-a", externalId: "thread-a" })

    expect(create).toHaveBeenCalledWith({
      metadata: expect.objectContaining({ workspace_id: "ws-a" }),
    })
  })

  it("treats missing LangGraph thread search as an empty thread list", async () => {
    const client = {
      threads: {
        search: vi.fn().mockRejectedValue(Object.assign(new Error('HTTP 404: {"detail":"Not Found"}'), { status: 404 })),
      },
    }

    const adapter = createLangGraphThreadListAdapter(client as never, "http://agents.test", "ws-a")
    await expect(adapter.list()).resolves.toEqual({ threads: [] })
  })

  it("persists archive and unarchive as LangGraph thread metadata", async () => {
    const get = vi.fn().mockResolvedValue({
      thread_id: "thread-delivery",
      metadata: { source: "specgate-ui", surface: "governance-agent", workspace_id: "ws-a", title: "Delivery review" },
      updated_at: "2026-06-30T00:00:00Z",
    })
    const update = vi.fn().mockResolvedValue(undefined)
    const client = { threads: { get, update } }

    const adapter = createLangGraphThreadListAdapter(client as never, "http://agents.test", "ws-a")
    await adapter.archive("thread-delivery")
    await adapter.unarchive("thread-delivery")

    expect(update).toHaveBeenNthCalledWith(1, "thread-delivery", {
      metadata: expect.objectContaining({ title: "Delivery review", archived: true }),
      returnMinimal: true,
    })
    expect(update).toHaveBeenNthCalledWith(2, "thread-delivery", {
      metadata: expect.objectContaining({ title: "Delivery review", archived: false }),
      returnMinimal: true,
    })
  })

  it("rejects thread operations when a thread belongs to another workspace", async () => {
    const get = vi.fn().mockResolvedValue({
      thread_id: "thread-b",
      metadata: { source: "specgate-ui", surface: "governance-agent", workspace_id: "ws-b" },
      updated_at: "2026-06-30T00:00:00Z",
    })
    const client = { threads: { get } }
    const adapter = createLangGraphThreadListAdapter(client as never, "http://agents.test", "ws-a")

    await expect(adapter.fetch("thread-b")).rejects.toThrow("workspace mismatch")
  })

  it("rejects title generation before calling the title route for another workspace", async () => {
    const get = vi.fn().mockResolvedValue({
      thread_id: "thread-b",
      metadata: { workspace_id: "ws-b" },
      updated_at: "2026-06-30T00:00:00Z",
    })
    const client = { threads: { get, update: vi.fn() } }
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)

    const adapter = createLangGraphThreadListAdapter(client as never, "http://agents.test", "ws-a")
    await expect(adapter.generateTitle("thread-b", [])).rejects.toThrow("workspace mismatch")
    expect(fetchMock).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})

describe("governance run workspace context", () => {
  it("overrides model-provided workspace values with the active workspace", () => {
    expect(
      withGovernanceWorkspaceRunConfig(
        { configurable: { workspace_id: "ws-b", model_name: "gpt-5.4-mini" } },
        "ws-a",
      ),
    ).toEqual({
      configurable: { workspace_id: "ws-a", thread_workspace_id: "ws-a", model_name: "gpt-5.4-mini" },
    })
  })
})
