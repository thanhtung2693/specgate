// Settings panel extracted from settings.tsx. See app/ui/AGENTS.md.

import { DatabaseIcon, KeyRoundIcon } from "lucide-react"
import { useEffect, useMemo, useState, type FormEvent } from "react"
import { ProviderBrandIcon } from "@/components/brand-icons"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { docRegistryBase, embeddingModelsForProvider, embeddingProviderKey, embeddingProviderLabel, embeddingProviders, fetchOpenRouterEmbeddingModels, fetchOpenRouterModels, defaultModelSettings, governanceThinkingLevels, loadModelSettings, modelsForProvider, providerKey, providerLabel, saveModelSettings, type EmbeddingModelOption, type EmbeddingModelProvider, type GovernanceModelOption, type GovernanceModelProvider, type ModelSettingsData, type OpenRouterModel } from "@/data/model-settings"
import { cn } from "@/lib/utils"
import { toneClass } from "../shared"
import { type SettingsSaveStatus } from "./shared"

export function ModelSettingsPanel({ onSaveStatusChange }: { onSaveStatusChange: (status: SettingsSaveStatus) => void }) {
  const base = useMemo(() => docRegistryBase(), [])
  const [settings, setSettings] = useState<ModelSettingsData>(() => ({ ...defaultModelSettings }))
  const [savedSettings, setSavedSettings] = useState<ModelSettingsData>(() => ({ ...defaultModelSettings }))
  const [loading, setLoading] = useState(Boolean(base))
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [embeddingQuery, setEmbeddingQuery] = useState("")
  const [openRouterModels, setOpenRouterModels] = useState<OpenRouterModel[]>([])
  const [openRouterEmbeddingModels, setOpenRouterEmbeddingModels] = useState<OpenRouterModel[]>([])
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [embeddingCatalogLoading, setEmbeddingCatalogLoading] = useState(false)
  const [catalogError, setCatalogError] = useState<string | null>(null)
  const [embeddingCatalogError, setEmbeddingCatalogError] = useState<string | null>(null)

  const modelProvider = settings["governance.model_provider"]
  const modelEnabled = settings["governance.model_enabled"] === "true"
  const embeddingProvider = settings["embedding.model_provider"]
  const apiKeyName = providerKey(modelProvider)
  const embeddingApiKeyName = embeddingProviderKey(embeddingProvider)
  const usesOpenRouter = modelProvider === "openrouter"
  const embeddingUsesOpenRouter = embeddingProvider === "openrouter"
  const staticOptions = useMemo(() => modelsForProvider(modelProvider), [modelProvider])
  const staticEmbeddingOptions = useMemo(() => embeddingModelsForProvider(embeddingProvider), [embeddingProvider])
  const openRouterOptions = openRouterModels.length > 0 ? openRouterModels : staticOptions
  const openRouterEmbeddingOptions =
    openRouterEmbeddingModels.length > 0 ? openRouterEmbeddingModels : staticEmbeddingOptions
  const modelOptions: Array<GovernanceModelOption | OpenRouterModel> = usesOpenRouter ? openRouterOptions : staticOptions
  const embeddingModelOptions: Array<EmbeddingModelOption | OpenRouterModel> = embeddingUsesOpenRouter
    ? openRouterEmbeddingOptions
    : staticEmbeddingOptions
  const selectedGovernanceModel = settings["governance.model"]
  const selectedEmbeddingModel = settings["embedding.model"]
  const visibleModelOptions = useMemo(() => {
    const hasSelected = modelOptions.some((model) => model.id === selectedGovernanceModel)
    if (!selectedGovernanceModel || hasSelected) return modelOptions
    return [{ id: selectedGovernanceModel, name: selectedGovernanceModel }, ...modelOptions]
  }, [modelOptions, selectedGovernanceModel])
  const visibleEmbeddingModelOptions = useMemo(() => {
    const hasSelected = embeddingModelOptions.some((model) => model.id === selectedEmbeddingModel)
    if (!selectedEmbeddingModel || hasSelected) return embeddingModelOptions
    return [{ id: selectedEmbeddingModel, name: selectedEmbeddingModel }, ...embeddingModelOptions]
  }, [embeddingModelOptions, selectedEmbeddingModel])
  const filteredModelOptions = useMemo(() => {
    const needle = query.trim().toLowerCase()
    const options = visibleModelOptions.slice(0, needle ? visibleModelOptions.length : 8)
    if (!needle) return options.slice(0, 8)
    return options
      .filter((model) => `${model.name} ${model.id}`.toLowerCase().includes(needle))
      .slice(0, 8)
  }, [query, visibleModelOptions])
  const filteredEmbeddingModelOptions = useMemo(() => {
    const needle = embeddingQuery.trim().toLowerCase()
    const options = visibleEmbeddingModelOptions.slice(0, needle ? visibleEmbeddingModelOptions.length : 8)
    if (!needle) return options.slice(0, 8)
    return options
      .filter((model) => `${model.name} ${model.id}`.toLowerCase().includes(needle))
      .slice(0, 8)
  }, [embeddingQuery, visibleEmbeddingModelOptions])
  const customSlug = query.trim()
  const customEmbeddingSlug = embeddingQuery.trim()
  const canUseCustomSlug = customSlug !== "" && !visibleModelOptions.some((model) => model.id === customSlug)
  const canUseCustomEmbeddingSlug =
    customEmbeddingSlug !== "" &&
    !visibleEmbeddingModelOptions.some((model) => model.id === customEmbeddingSlug)
  const dirty = !sameModelSettings(settings, savedSettings)

  useEffect(() => {
    onSaveStatusChange({ canSave: Boolean(base) && dirty && !loading && !saving, saving })
  }, [base, dirty, loading, onSaveStatusChange, saving])

  useEffect(() => {
    if (!base) {
      setLoading(false)
      return
    }

    let cancelled = false
    setLoading(true)
    setError(null)
    loadModelSettings(base)
      .then((loaded) => {
        if (!cancelled) {
          setSettings(loaded)
          setSavedSettings(loaded)
        }
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "Failed to load model settings")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [base])

  useEffect(() => {
    if (!usesOpenRouter || openRouterModels.length > 0) return

    const controller = new AbortController()
    setCatalogLoading(true)
    setCatalogError(null)
    fetchOpenRouterModels(controller.signal)
      .then(setOpenRouterModels)
      .catch((reason: unknown) => {
        if (!controller.signal.aborted) {
          setCatalogError(reason instanceof Error ? reason.message : "OpenRouter catalog unavailable")
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setCatalogLoading(false)
      })

    return () => controller.abort()
  }, [usesOpenRouter, openRouterModels.length])

  useEffect(() => {
    if (!embeddingUsesOpenRouter || openRouterEmbeddingModels.length > 0) return

    const controller = new AbortController()
    setEmbeddingCatalogLoading(true)
    setEmbeddingCatalogError(null)
    fetchOpenRouterEmbeddingModels(controller.signal)
      .then(setOpenRouterEmbeddingModels)
      .catch((reason: unknown) => {
        if (!controller.signal.aborted) {
          setEmbeddingCatalogError(reason instanceof Error ? reason.message : "OpenRouter embedding catalog unavailable")
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setEmbeddingCatalogLoading(false)
      })

    return () => controller.abort()
  }, [embeddingUsesOpenRouter, openRouterEmbeddingModels.length])

  function selectProvider(provider: GovernanceModelProvider) {
    setQuery("")
    setMessage(null)
    setSettings((current) => {
      const firstModel = modelsForProvider(provider)[0]?.id ?? current["governance.model"]
      return {
        ...current,
        "governance.model_provider": provider,
        "governance.model": firstModel,
      }
    })
  }

  function selectEmbeddingProvider(provider: EmbeddingModelProvider) {
    setEmbeddingQuery("")
    setMessage(null)
    setSettings((current) => {
      const firstModel = embeddingModelsForProvider(provider)[0]?.id ?? ""
      return {
        ...current,
        "embedding.model_provider": provider,
        "embedding.model": provider ? firstModel : "",
      }
    })
  }

  async function saveSettings() {
    if (!base || !dirty) return
    setSaving(true)
    setMessage(null)
    setError(null)
    try {
      const saved = await saveModelSettings(base, settings)
      setSettings(saved)
      setSavedSettings(saved)
      setMessage("Model settings saved.")
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save model settings")
    } finally {
      setSaving(false)
    }
  }

  return (
    <form
      id="model-settings-form"
      className="grid gap-5"
      onSubmit={(event: FormEvent<HTMLFormElement>) => {
        event.preventDefault()
        void saveSettings()
      }}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Model settings</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            Server-side model for governance operations — readiness gates, quick-work
            acceptance criteria, and delivery review. The governance chat agent
            uses a separately configured (environment) model.
          </p>
          <p className="mt-2 text-xs text-muted-foreground">
            Model-less mode keeps the saved provider and key, uses IDE-agent checks, and requires peer or human review for agent-attested delivery.
          </p>
        </div>
      </div>

      {!base ? (
        <div className="rounded-lg border border-dashed bg-card/60 p-3 text-sm text-muted-foreground">
          Set <code>VITE_DOC_REGISTRY_URL</code> to load and save model settings.
        </div>
      ) : null}
      {error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-300">
          {error}
        </div>
      ) : null}
      {message ? (
        <div className="rounded-lg border border-green-500/30 bg-green-500/10 p-3 text-sm text-green-700 dark:text-green-300">
          {message}
        </div>
      ) : null}

      <div className="grid gap-5">
        <div className="rounded-lg border bg-background/70 p-4">
          <div className="flex items-center gap-2">
            <KeyRoundIcon className="size-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">Server-side model</h3>
          </div>
          <div className="mt-4 grid gap-3">
            <div className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Provider</span>
              <div className="flex flex-wrap gap-2">
                <Button type="button" variant={!modelEnabled ? "secondary" : "outline"} size="sm" className="rounded-md" disabled={loading} onClick={() => setSettings((current) => ({ ...current, "governance.model_enabled": "false" }))}>
                  Model-less
                </Button>
                {!modelEnabled ? <Button type="button" variant="outline" size="sm" className="rounded-md" disabled={loading} onClick={() => setSettings((current) => ({ ...current, "governance.model_enabled": "true" }))}>Use saved model</Button> : null}
                {(["openrouter", "openai", "anthropic", "google_genai"] as GovernanceModelProvider[]).map((provider) => (
                  <Button
                    key={provider}
                    type="button"
                    variant={modelEnabled && modelProvider === provider ? "secondary" : "outline"}
                    size="sm"
                    className="rounded-md"
                    disabled={loading}
                    onClick={() => {
                      selectProvider(provider)
                      setSettings((current) => ({ ...current, "governance.model_enabled": "true" }))
                    }}
                  >
                    <ProviderBrandIcon provider={provider} className="size-4" />
                    {providerLabel(provider)}
                  </Button>
                ))}
              </div>
            </div>
            {modelEnabled ? <>
            <label className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">{providerLabel(modelProvider)} API key</span>
              <Input
                type="password"
                value={settings[apiKeyName]}
                placeholder="Leave *** to keep existing secret"
                disabled={loading}
                onChange={(event) => setSettings((current) => ({ ...current, [apiKeyName]: event.target.value }))}
              />
            </label>
            <div className="grid gap-2">
              <div className="flex items-center justify-between gap-3">
                <span className="text-xs font-medium text-muted-foreground">Model</span>
                {usesOpenRouter ? (
                  <span className="text-xs text-muted-foreground">
                    {catalogLoading ? "Loading OpenRouter catalog" : catalogError ? "Using seeded suggestions" : "OpenRouter catalog"}
                  </span>
                ) : null}
              </div>
              <Input
                value={query}
                placeholder="Search models or type a model ID"
                disabled={loading}
                onChange={(event) => setQuery(event.target.value)}
              />
              <div className="grid overflow-hidden rounded-lg border">
                {canUseCustomSlug ? (
                  <button
                    type="button"
                    className="flex items-center justify-between gap-3 border-b px-3 py-2 text-left text-sm hover:bg-muted/60"
                    onClick={() => {
                      setSettings((current) => ({ ...current, "governance.model": customSlug }))
                      setQuery("")
                    }}
                  >
                    <span>Use {customSlug}</span>
                    <span className="font-mono text-xs text-muted-foreground">custom</span>
                  </button>
                ) : null}
                {filteredModelOptions.map((model) => (
                  <button
                    key={model.id}
                    type="button"
                    className={cn(
                      "flex items-center justify-between gap-3 border-b px-3 py-2 text-left text-sm last:border-b-0 hover:bg-muted/60",
                      settings["governance.model"] === model.id && "bg-muted/45",
                    )}
                    onClick={() => {
                      setSettings((current) => ({ ...current, "governance.model": model.id }))
                      setQuery("")
                    }}
                  >
                    <span className="min-w-0">
                      <span className="block truncate font-medium">{model.name}</span>
                      <span className="block truncate font-mono text-xs text-muted-foreground">{model.id}</span>
                    </span>
                    {settings["governance.model"] === model.id ? (
                      <Badge variant="outline" className={cn("shrink-0 border", toneClass("success"))}>
                        Selected
                      </Badge>
                    ) : null}
                  </button>
                ))}
                {filteredModelOptions.length === 0 && !canUseCustomSlug ? (
                  <div className="px-3 py-4 text-sm text-muted-foreground">No model found.</div>
                ) : null}
              </div>
              {catalogError ? <p className="text-xs text-muted-foreground">{catalogError}</p> : null}
            </div>
            </> : null}
            {modelEnabled ? (
            <div className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Reasoning effort</span>
              <div className="flex flex-wrap gap-2">
                {governanceThinkingLevels.map((level) => (
                  <Button
                    key={level.id}
                    type="button"
                    variant={settings["governance.default_thinking_level"] === level.id ? "secondary" : "outline"}
                    size="sm"
                    className="rounded-md"
                    disabled={loading}
                    onClick={() => setSettings((current) => ({ ...current, "governance.default_thinking_level": level.id }))}
                  >
                    {level.label}
                  </Button>
                ))}
              </div>
            </div>
            ) : null}
          </div>
        </div>

        <div className="rounded-lg border bg-background/70 p-4">
          <div className="flex items-center gap-2">
            <DatabaseIcon className="size-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">Embedding model</h3>
          </div>
          <p className="mt-3 text-sm text-muted-foreground">
            Optional model for knowledge indexing and semantic search. Leave disabled if you do not use knowledge upload/search.
          </p>
          <div className="mt-4 grid gap-3">
            <div className="grid gap-2">
              <span className="text-xs font-medium text-muted-foreground">Provider</span>
              <div className="flex flex-wrap gap-2">
                {embeddingProviders.map((provider) => (
                  <Button
                    key={provider.id || "disabled"}
                    type="button"
                    variant={embeddingProvider === provider.id ? "secondary" : "outline"}
                    size="sm"
                    className="rounded-md"
                    disabled={loading}
                    onClick={() => selectEmbeddingProvider(provider.id)}
                  >
                    {provider.id ? <ProviderBrandIcon provider={provider.id} className="size-4" /> : null}
                    {provider.label}
                  </Button>
                ))}
              </div>
            </div>
            {embeddingProvider && embeddingApiKeyName ? (
              <>
                {embeddingProvider === modelProvider ? (
                  <p className="rounded-md border bg-card/70 px-3 py-2 text-xs text-muted-foreground">
                    Uses the {embeddingProviderLabel(embeddingProvider)} API key from the server-side model section.
                  </p>
                ) : (
                  <label className="grid gap-2">
                    <span className="text-xs font-medium text-muted-foreground">
                      {embeddingProviderLabel(embeddingProvider)} API key
                    </span>
                    <Input
                      type="password"
                      value={settings[embeddingApiKeyName]}
                      placeholder="Leave *** to keep existing secret"
                      disabled={loading}
                      onChange={(event) =>
                        setSettings((current) => ({ ...current, [embeddingApiKeyName]: event.target.value }))
                      }
                    />
                  </label>
                )}
                <div className="grid gap-2">
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-xs font-medium text-muted-foreground">Model</span>
                    {embeddingUsesOpenRouter ? (
                      <span className="text-xs text-muted-foreground">
                        {embeddingCatalogLoading
                          ? "Loading OpenRouter catalog"
                          : embeddingCatalogError
                            ? "Using seeded suggestions"
                            : "OpenRouter catalog"}
                      </span>
                    ) : null}
                  </div>
                  <Input
                    value={embeddingQuery}
                    placeholder={embeddingUsesOpenRouter ? "Search embedding models or type vendor/model" : "Search embedding models"}
                    disabled={loading}
                    onChange={(event) => setEmbeddingQuery(event.target.value)}
                  />
                  <div className="grid overflow-hidden rounded-lg border">
                    {canUseCustomEmbeddingSlug ? (
                      <button
                        type="button"
                        className="flex items-center justify-between gap-3 border-b px-3 py-2 text-left text-sm hover:bg-muted/60"
                        onClick={() => {
                          setSettings((current) => ({ ...current, "embedding.model": customEmbeddingSlug }))
                          setEmbeddingQuery("")
                        }}
                      >
                        <span>Use {customEmbeddingSlug}</span>
                        <span className="font-mono text-xs text-muted-foreground">custom</span>
                      </button>
                    ) : null}
                    {filteredEmbeddingModelOptions.map((model) => (
                      <button
                        key={model.id}
                        type="button"
                        className={cn(
                          "flex items-center justify-between gap-3 border-b px-3 py-2 text-left text-sm last:border-b-0 hover:bg-muted/60",
                          settings["embedding.model"] === model.id && "bg-muted/45",
                        )}
                        onClick={() => {
                          setSettings((current) => ({ ...current, "embedding.model": model.id }))
                          setEmbeddingQuery("")
                        }}
                      >
                        <span className="min-w-0">
                          <span className="block truncate font-medium">{model.name}</span>
                          <span className="block truncate font-mono text-xs text-muted-foreground">{model.id}</span>
                        </span>
                        {settings["embedding.model"] === model.id ? (
                          <Badge variant="outline" className={cn("shrink-0 border", toneClass("success"))}>
                            Selected
                          </Badge>
                        ) : null}
                      </button>
                    ))}
                    {filteredEmbeddingModelOptions.length === 0 && !canUseCustomEmbeddingSlug ? (
                      <div className="px-3 py-4 text-sm text-muted-foreground">No model found.</div>
                    ) : null}
                  </div>
                  {embeddingCatalogError ? <p className="text-xs text-muted-foreground">{embeddingCatalogError}</p> : null}
                </div>
              </>
            ) : null}
          </div>
        </div>
      </div>
    </form>
  )
}

function sameModelSettings(left: ModelSettingsData, right: ModelSettingsData): boolean {
  return (Object.keys(defaultModelSettings) as Array<keyof ModelSettingsData>).every((key) => left[key] === right[key])
}
