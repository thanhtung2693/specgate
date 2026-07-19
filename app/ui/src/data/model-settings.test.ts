import { afterEach, describe, expect, it, vi } from "vitest"

import {
  fetchOpenRouterEmbeddingModels,
  fetchOpenRouterModels,
  normalizeModelSettings,
  openRouterEmbeddingModelsUrl,
  openRouterModelsUrl,
  saveModelSettings,
} from "@/data/model-settings"

describe("model settings helpers", () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("normalizes invalid provider values to safe defaults", () => {
    expect(
      normalizeModelSettings({
        "governance.model_provider": "unknown",
        "governance.default_thinking_level": "max",
        "embedding.model_provider": "anthropic",
        "embedding.model": "ignored",
        "openrouter.api_key": "***",
      }),
    ).toMatchObject({
      "governance.model_provider": "openrouter",
      "governance.model": "deepseek/deepseek-v4-flash",
      "governance.default_thinking_level": "low",
      "embedding.model_provider": "",
      "embedding.model": "",
      "openrouter.api_key": "***",
    })
  })

  it("keeps selected governance and embedding model settings", () => {
    expect(
      normalizeModelSettings({
        "governance.model_provider": "google_genai",
        "governance.model": "gemini-3.1-pro",
        "governance.default_thinking_level": "high",
        "embedding.model_provider": "openai",
        "embedding.model": "text-embedding-3-large",
        "openai.api_key": "***",
      }),
    ).toMatchObject({
      "governance.model_provider": "google_genai",
      "governance.model": "gemini-3.1-pro",
      "governance.default_thinking_level": "high",
      "embedding.model_provider": "openai",
      "embedding.model": "text-embedding-3-large",
      "openai.api_key": "***",
    })
  })

  it("preserves a configured model while model-less mode is enabled", () => {
    expect(
      normalizeModelSettings({
        "governance.model_enabled": "false",
        "governance.model_provider": "google_genai",
        "governance.model": "gemini-3.1-flash-lite",
      }),
    ).toMatchObject({
      "governance.model_enabled": "false",
      "governance.model_provider": "google_genai",
      "governance.model": "gemini-3.1-flash-lite",
    })
  })

  it("loads only text-only OpenRouter governance models", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          data: [
            { id: "deepseek/deepseek-v4", name: "DeepSeek", architecture: { output_modalities: ["text"] } },
            { id: "image/model", name: "Image Model", architecture: { output_modalities: ["text", "image"] } },
            { id: "audio/model", name: "Audio Model", architecture: { output_modalities: ["audio"] } },
            { id: "video/model", name: "Video Model", architecture: { output_modalities: ["text", "video"] } },
            { name: "Missing ID", architecture: { output_modalities: ["text"] } },
          ],
        }),
      ),
    )

    await expect(fetchOpenRouterModels()).resolves.toEqual([{ id: "deepseek/deepseek-v4", name: "DeepSeek" }])
    expect(fetchMock).toHaveBeenCalledWith(openRouterModelsUrl, { signal: expect.any(AbortSignal) })
  })

  it("loads only OpenRouter embedding models", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          data: [
            { id: "mistralai/mistral-embed", name: "Mistral Embed", architecture: { output_modalities: ["embeddings"] } },
            { id: "chat/model", name: "Chat Model", architecture: { output_modalities: ["text"] } },
          ],
        }),
      ),
    )

    await expect(fetchOpenRouterEmbeddingModels()).resolves.toEqual([
      { id: "mistralai/mistral-embed", name: "Mistral Embed" },
    ])
  })

  it("saves settings through the Doc Registry settings endpoint", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          settings: {
            "governance.model_provider": "openrouter",
            "governance.model": "anthropic/claude-sonnet-4.6",
            "governance.default_thinking_level": "low",
            "embedding.model_provider": "openrouter",
            "embedding.model": "openai/text-embedding-3-large",
            "openrouter.api_key": "***",
          },
        }),
      ),
    )

    const saved = await saveModelSettings("http://registry.test", {
      ...normalizeModelSettings(),
      "governance.model": "anthropic/claude-sonnet-4.6",
      "governance.default_thinking_level": "low",
      "embedding.model_provider": "openrouter",
      "embedding.model": "openai/text-embedding-3-large",
      "openrouter.api_key": "***",
    })

    expect(saved).toMatchObject({
      "governance.model": "anthropic/claude-sonnet-4.6",
      "embedding.model": "openai/text-embedding-3-large",
      "openrouter.api_key": "***",
    })
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/settings",
      expect.objectContaining({
        method: "PUT",
        headers: { "Content-Type": "application/json" },
      }),
    )
  })

  it("uses the embedding catalog URL for embedding model lookup", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(new Response(JSON.stringify({ data: [] })))

    await fetchOpenRouterEmbeddingModels()

    expect(fetchMock).toHaveBeenCalledWith(openRouterEmbeddingModelsUrl, { signal: expect.any(AbortSignal) })
  })

  it("rejects an oversized OpenRouter catalog before reading its body", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
      new Response("{}", { headers: { "Content-Length": String((2 << 20) + 1) } }),
    )

    await expect(fetchOpenRouterModels()).rejects.toThrow("2 MiB")
  })
})
