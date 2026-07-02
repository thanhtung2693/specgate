import { describe, expect, it, vi } from "vitest"

import {
  createLangGraphThreadListAdapter,
  getGovernanceRuntimeConfig,
  readLangGraphErrorMessage,
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
            metadata: { source: "specgate-ui", surface: "governance-agent", title: "Active thread" },
            updated_at: "2026-06-30T00:00:00Z",
          },
          {
            thread_id: "thread-archived",
            metadata: { source: "specgate-ui", surface: "governance-agent", title: "Done", archived: true },
            updated_at: "2026-06-29T00:00:00Z",
          },
        ]),
      },
    }

    const adapter = createLangGraphThreadListAdapter(client as never, "http://agents.test")
    const result = await adapter.list()

    expect(result.threads).toEqual([
      expect.objectContaining({ remoteId: "thread-active", status: "regular", title: "Active thread" }),
      expect.objectContaining({ remoteId: "thread-archived", status: "archived", title: "Done" }),
    ])
  })

  it("persists archive and unarchive as LangGraph thread metadata", async () => {
    const get = vi.fn().mockResolvedValue({
      thread_id: "thread-delivery",
      metadata: { source: "specgate-ui", surface: "governance-agent", title: "Delivery review" },
      updated_at: "2026-06-30T00:00:00Z",
    })
    const update = vi.fn().mockResolvedValue(undefined)
    const client = { threads: { get, update } }

    const adapter = createLangGraphThreadListAdapter(client as never, "http://agents.test")
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
})
