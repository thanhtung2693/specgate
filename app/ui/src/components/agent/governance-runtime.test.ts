import { describe, expect, it, vi } from "vitest"

import {
  createGovernanceThread,
  getGovernanceRuntimeConfig,
  readLangGraphErrorMessage,
  withGovernanceWorkspaceRunConfig,
} from "@/components/agent/governance-runtime"

describe("governance runtime config", () => {
  it("marks governance chat unavailable when LangGraph is not configured", () => {
    const env = { VITE_LANGGRAPH_API_URL: "" } as unknown as ImportMetaEnv

    expect(getGovernanceRuntimeConfig(env)).toEqual({
      mode: "unavailable",
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
      "The model provider is rate-limited. Wait a moment and try again.",
    )
  })
})

describe("LangGraph governance thread creation", () => {
  it("creates threads pinned to the active workspace", async () => {
    const create = vi.fn().mockResolvedValue({ thread_id: "thread-a" })
    const client = { threads: { create } }

    await expect(createGovernanceThread(client as never, "ws-a")).resolves.toEqual({ externalId: "thread-a" })

    expect(create).toHaveBeenCalledWith({
      metadata: expect.objectContaining({ workspace_id: "ws-a" }),
    })
  })

  it("requires a workspace before creating a thread", async () => {
    const client = { threads: { create: vi.fn() } }
    await expect(createGovernanceThread(client as never, " ")).rejects.toThrow("workspace_id is required")
    expect(client.threads.create).not.toHaveBeenCalled()
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
