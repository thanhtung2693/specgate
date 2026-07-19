export type GovernanceModelProvider = "openai" | "google_genai" | "anthropic" | "openrouter"
type GovernanceModelEnabled = "true" | "false"
export type EmbeddingModelProvider = "" | "openai" | "google_genai" | "openrouter"
export type GovernanceThinkingLevel = "low" | "medium" | "high"

export type ModelSettingsData = {
  "governance.model_enabled": GovernanceModelEnabled
  "governance.model_provider": GovernanceModelProvider
  "governance.model": string
  "governance.default_thinking_level": GovernanceThinkingLevel
  "embedding.model_provider": EmbeddingModelProvider
  "embedding.model": string
  "openai.api_key": string
  "google.api_key": string
  "anthropic.api_key": string
  "openrouter.api_key": string
}

export type GovernanceModelOption = {
  id: string
  name: string
  provider: GovernanceModelProvider
}

export type EmbeddingModelOption = {
  id: string
  name: string
  provider: Exclude<EmbeddingModelProvider, "">
}

export type OpenRouterModel = {
  id: string
  name: string
}

type RawOpenRouterModel = {
  id?: unknown
  name?: unknown
  architecture?: { output_modalities?: unknown } | null
}

type SettingsBody = {
  settings?: Record<string, string>
}

const governanceProviders: Array<{ id: GovernanceModelProvider; label: string; key: keyof ModelSettingsData }> = [
  { id: "openrouter", label: "OpenRouter", key: "openrouter.api_key" },
  { id: "openai", label: "OpenAI", key: "openai.api_key" },
  { id: "anthropic", label: "Anthropic", key: "anthropic.api_key" },
  { id: "google_genai", label: "Google", key: "google.api_key" },
]

export const embeddingProviders: Array<{ id: EmbeddingModelProvider; label: string; key?: keyof ModelSettingsData }> = [
  { id: "", label: "Disabled" },
  { id: "openrouter", label: "OpenRouter", key: "openrouter.api_key" },
  { id: "openai", label: "OpenAI", key: "openai.api_key" },
  { id: "google_genai", label: "Google", key: "google.api_key" },
]

export const governanceThinkingLevels: Array<{ id: GovernanceThinkingLevel; label: string }> = [
  { id: "low", label: "Low" },
  { id: "medium", label: "Medium" },
  { id: "high", label: "High" },
]

const governanceModelOptions: GovernanceModelOption[] = [
  { id: "deepseek/deepseek-v4-flash", name: "DeepSeek V4 Flash", provider: "openrouter" },
  { id: "z-ai/glm-5.1", name: "GLM-5.1", provider: "openrouter" },
  { id: "gpt-5.4-mini", name: "GPT-5.4 Mini", provider: "openai" },
  { id: "gpt-5.4", name: "GPT-5.4", provider: "openai" },
  { id: "gpt-5.5", name: "GPT-5.5", provider: "openai" },
  { id: "claude-sonnet-4-6", name: "Claude Sonnet 4.6", provider: "anthropic" },
  { id: "claude-opus-4-7", name: "Claude Opus 4.7", provider: "anthropic" },
  { id: "gemini-3.1-flash-lite", name: "Gemini 3.1 Flash Lite", provider: "google_genai" },
  { id: "gemini-3.1-pro", name: "Gemini 3.1 Pro", provider: "google_genai" },
]

const embeddingModelOptions: EmbeddingModelOption[] = [
  { id: "openrouter", name: "OpenRouter default embedding", provider: "openrouter" },
  { id: "openai/text-embedding-3-large", name: "OpenAI 3-large via OpenRouter", provider: "openrouter" },
  { id: "mistralai/mistral-embed-2312", name: "Mistral Embed via OpenRouter", provider: "openrouter" },
  { id: "text-embedding-3-small", name: "OpenAI 3-small", provider: "openai" },
  { id: "text-embedding-3-large", name: "OpenAI 3-large", provider: "openai" },
  { id: "gemini-embedding-001", name: "Gemini Embedding", provider: "google_genai" },
]

export const defaultModelSettings: ModelSettingsData = {
  "governance.model_enabled": "true",
  "governance.model_provider": "openrouter",
  "governance.model": "deepseek/deepseek-v4-flash",
  "governance.default_thinking_level": "low",
  "embedding.model_provider": "",
  "embedding.model": "",
  "openai.api_key": "",
  "google.api_key": "",
  "anthropic.api_key": "",
  "openrouter.api_key": "",
}

export const openRouterModelsUrl = "https://openrouter.ai/api/v1/models?sort=most-popular&output_modalities=text"
export const openRouterEmbeddingModelsUrl = "https://openrouter.ai/api/v1/models?sort=most-popular&output_modalities=embeddings"
const openRouterCatalogMaxBytes = 2 << 20
const openRouterCatalogTimeoutMs = 15_000

export function docRegistryBase(): string | null {
  const base = import.meta.env.VITE_DOC_REGISTRY_URL?.trim()
  return base ? base.replace(/\/$/, "") : null
}

export function providerLabel(provider: GovernanceModelProvider): string {
  return governanceProviders.find((entry) => entry.id === provider)?.label ?? provider
}

export function embeddingProviderLabel(provider: EmbeddingModelProvider): string {
  return embeddingProviders.find((entry) => entry.id === provider)?.label ?? provider
}

export function providerKey(provider: GovernanceModelProvider): keyof ModelSettingsData {
  return governanceProviders.find((entry) => entry.id === provider)?.key ?? "openai.api_key"
}

export function embeddingProviderKey(provider: EmbeddingModelProvider): keyof ModelSettingsData | null {
  return embeddingProviders.find((entry) => entry.id === provider)?.key ?? null
}

export function modelsForProvider(provider: GovernanceModelProvider): GovernanceModelOption[] {
  return governanceModelOptions.filter((model) => model.provider === provider)
}

export function embeddingModelsForProvider(provider: EmbeddingModelProvider): EmbeddingModelOption[] {
  if (!provider) return []
  return embeddingModelOptions.filter((model) => model.provider === provider)
}

export function parseSettingsBody(data: unknown): SettingsBody {
  if (data && typeof data === "object" && "body" in data) {
    return ((data as { body?: SettingsBody }).body ?? {}) as SettingsBody
  }
  if (data && typeof data === "object" && "Body" in data) {
    return ((data as { Body?: SettingsBody }).Body ?? {}) as SettingsBody
  }
  return (data ?? {}) as SettingsBody
}

export function normalizeModelSettings(settings?: Record<string, string>): ModelSettingsData {
  const provider = normalizeProvider(settings?.["governance.model_provider"])
  const level = normalizeThinkingLevel(settings?.["governance.default_thinking_level"])
  const embeddingProvider = normalizeEmbeddingProvider(settings?.["embedding.model_provider"])
  const providerModels = modelsForProvider(provider)
  const embeddingProviderModels = embeddingModelsForProvider(embeddingProvider)
  const model = settings?.["governance.model"] || providerModels[0]?.id || defaultModelSettings["governance.model"]
  const embeddingModel =
    settings?.["embedding.model"] ||
    embeddingProviderModels[0]?.id ||
    defaultModelSettings["embedding.model"]

  return {
    ...defaultModelSettings,
    "governance.model_enabled": settings?.["governance.model_enabled"] === "false" ? "false" : "true",
    "governance.model_provider": provider,
    "governance.model": model,
    "governance.default_thinking_level": level,
    "embedding.model_provider": embeddingProvider,
    "embedding.model": embeddingProvider ? embeddingModel : "",
    "openai.api_key": settings?.["openai.api_key"] ?? defaultModelSettings["openai.api_key"],
    "google.api_key": settings?.["google.api_key"] ?? defaultModelSettings["google.api_key"],
    "anthropic.api_key": settings?.["anthropic.api_key"] ?? defaultModelSettings["anthropic.api_key"],
    "openrouter.api_key": settings?.["openrouter.api_key"] ?? defaultModelSettings["openrouter.api_key"],
  }
}

export async function loadModelSettings(base: string): Promise<ModelSettingsData> {
  const response = await fetch(`${base}/settings`, { method: "GET" })
  if (!response.ok) throw new Error(`GET /settings failed (${response.status})`)
  return normalizeModelSettings(parseSettingsBody(await response.json()).settings)
}

export async function saveModelSettings(base: string, settings: ModelSettingsData): Promise<ModelSettingsData> {
  const response = await fetch(`${base}/settings`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ settings }),
  })
  if (!response.ok) {
    const text = await response.text().catch(() => "")
    throw new Error(`PUT /settings failed (${response.status})${text ? `: ${text.slice(0, 160)}` : ""}`)
  }
  return normalizeModelSettings(parseSettingsBody(await response.json()).settings)
}

export async function fetchOpenRouterModels(signal?: AbortSignal): Promise<OpenRouterModel[]> {
  const body = await fetchOpenRouterCatalog(openRouterModelsUrl, "models", signal)
  const data = Array.isArray(body.data) ? body.data : []
  return data
    .filter(isTextModel)
    .map((model) => ({ id: String(model.id ?? ""), name: String(model.name ?? model.id ?? "") }))
    .filter((model) => model.id !== "")
}

export async function fetchOpenRouterEmbeddingModels(signal?: AbortSignal): Promise<OpenRouterModel[]> {
  const body = await fetchOpenRouterCatalog(openRouterEmbeddingModelsUrl, "embedding models", signal)
  const data = Array.isArray(body.data) ? body.data : []
  return data
    .filter(isEmbeddingModel)
    .map((model) => ({ id: String(model.id ?? ""), name: String(model.name ?? model.id ?? "") }))
    .filter((model) => model.id !== "")
}

async function fetchOpenRouterCatalog(
  url: string,
  label: string,
  signal?: AbortSignal,
): Promise<{ data?: RawOpenRouterModel[] }> {
  const timeoutSignal = AbortSignal.timeout(openRouterCatalogTimeoutMs)
  const requestSignal = signal ? AbortSignal.any([signal, timeoutSignal]) : timeoutSignal
  const response = await fetch(url, { signal: requestSignal })
  if (!response.ok) throw new Error(`OpenRouter ${label} request failed (${response.status})`)
  const declaredLength = Number(response.headers.get("content-length") ?? "0")
  if (Number.isFinite(declaredLength) && declaredLength > openRouterCatalogMaxBytes) {
    throw new Error("OpenRouter catalog response exceeds the 2 MiB limit")
  }
  if (!response.body) return {}

  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let total = 0
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    total += value.byteLength
    if (total > openRouterCatalogMaxBytes) {
      await reader.cancel()
      throw new Error("OpenRouter catalog response exceeds the 2 MiB limit")
    }
    chunks.push(value)
  }
  const bytes = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    bytes.set(chunk, offset)
    offset += chunk.byteLength
  }
  return JSON.parse(new TextDecoder().decode(bytes)) as { data?: RawOpenRouterModel[] }
}

function normalizeProvider(value: string | undefined): GovernanceModelProvider {
  if (value === "openai" || value === "google_genai" || value === "anthropic" || value === "openrouter") return value
  return defaultModelSettings["governance.model_provider"]
}

function normalizeEmbeddingProvider(value: string | undefined): EmbeddingModelProvider {
  if (value === "openai" || value === "google_genai" || value === "openrouter") return value
  return defaultModelSettings["embedding.model_provider"]
}

function normalizeThinkingLevel(value: string | undefined): GovernanceThinkingLevel {
  if (value === "low" || value === "medium" || value === "high") return value
  return defaultModelSettings["governance.default_thinking_level"]
}

function isTextModel(model: RawOpenRouterModel): boolean {
  const output = model.architecture?.output_modalities
  if (!Array.isArray(output)) return false
  return output.length > 0 && output.every((modality) => modality === "text")
}

function isEmbeddingModel(model: RawOpenRouterModel): boolean {
  const output = model.architecture?.output_modalities
  return Array.isArray(output) && output.includes("embeddings")
}
