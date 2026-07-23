import { AssistantRuntimeProvider } from "@assistant-ui/react"
import { unstable_createLangGraphStream, useLangGraphRuntime, type LangChainMessage } from "@assistant-ui/react-langgraph"
import { Client } from "@langchain/langgraph-sdk"
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react"

type UnavailableGovernanceRuntimeConfig = {
  mode: "unavailable"
}

type LangGraphGovernanceRuntimeConfig = {
  mode: "langgraph"
  apiUrl: string
  assistantId: string
}

type GovernanceRuntimeConfig = UnavailableGovernanceRuntimeConfig | LangGraphGovernanceRuntimeConfig

type LangGraphThreadValues = {
  messages?: LangChainMessage[]
}

type GovernanceRuntimeProviderProps = {
  children: ReactNode
  workspaceId?: string
}

type GovernanceRuntimeStatus = {
  streamError: string | null
  clearStreamError: () => void
}

const governanceThreadMetadata = {
  source: "specgate-ui",
  surface: "governance-agent",
} as const

function scopedThreadMetadata(workspaceId: string) {
  return { ...governanceThreadMetadata, workspace_id: workspaceId }
}

const GovernanceRuntimeStatusContext = createContext<GovernanceRuntimeStatus>({
  streamError: null,
  clearStreamError: () => {},
})

export function getGovernanceRuntimeConfig(env: ImportMetaEnv): GovernanceRuntimeConfig {
  const apiUrl = normalizeApiUrl(env.VITE_LANGGRAPH_API_URL?.trim() ?? "")

  if (!apiUrl) {
    return { mode: "unavailable" }
  }

  return {
    mode: "langgraph",
    apiUrl,
    assistantId: "governance",
  }
}

function normalizeApiUrl(apiUrl: string) {
  if (!apiUrl.startsWith("/")) return apiUrl

  const origin = globalThis.location?.origin
  return origin ? new URL(apiUrl, origin).toString().replace(/\/$/, "") : apiUrl
}

function joinApiPath(baseUrl: string, path: string) {
  return `${baseUrl.replace(/\/+$/, "")}/${path.replace(/^\/+/, "")}`
}

export function withGovernanceWorkspaceRunConfig(runConfig: unknown, workspaceId: string) {
  const scopedWorkspaceId = workspaceId.trim()
  if (!scopedWorkspaceId) throw new Error("workspace_id is required for governance runs")

  const current = runConfig && typeof runConfig === "object" ? (runConfig as Record<string, unknown>) : {}
  const configurable =
    current.configurable && typeof current.configurable === "object"
      ? (current.configurable as Record<string, unknown>)
      : {}

  return {
    ...current,
    configurable: {
      ...configurable,
      workspace_id: scopedWorkspaceId,
      thread_workspace_id: scopedWorkspaceId,
    },
  }
}

export function readLangGraphErrorMessage(value: unknown) {
  if (!value || typeof value !== "object") {
    return "The governance agent could not finish this response."
  }

  const record = value as Record<string, unknown>
  const message = typeof record.message === "string" ? record.message.trim() : ""
  const statusCode =
    typeof record.status_code === "number"
      ? record.status_code
      : typeof record.status === "number"
        ? record.status
        : undefined

  if (statusCode === 429) {
    return "The model provider is rate-limited. Wait a moment and try again."
  }

  return message || "The governance agent could not finish this response."
}

function assertGovernanceThreadWorkspace(thread: { metadata?: Record<string, unknown> | null }, workspaceId: string) {
  const threadWorkspaceId = String(thread.metadata?.workspace_id ?? "").trim()
  if (!workspaceId || threadWorkspaceId !== workspaceId) {
    throw new Error("governance thread workspace mismatch")
  }
}

export async function createGovernanceThread(client: Client, workspaceId = "") {
  const scopedWorkspaceId = workspaceId.trim()
  if (!scopedWorkspaceId) throw new Error("workspace_id is required for governance threads")
  const thread = await client.threads.create({
    metadata: {
      ...scopedThreadMetadata(scopedWorkspaceId),
      title: "Governance thread",
    },
  })
  return {
    externalId: thread.thread_id,
  }
}

function LangGraphGovernanceRuntimeProvider({
  children,
  config,
  workspaceId = "",
}: GovernanceRuntimeProviderProps & { config: LangGraphGovernanceRuntimeConfig }) {
  const client = useMemo(() => new Client({ apiUrl: config.apiUrl, apiKey: null }), [config.apiUrl])
  const [streamError, setStreamError] = useState<string | null>(null)
  const status = useMemo<GovernanceRuntimeStatus>(
    () => ({
      streamError,
      clearStreamError: () => setStreamError(null),
    }),
    [streamError],
  )
  const stream = useMemo(
    () => {
      const baseStream = unstable_createLangGraphStream({
        client,
        assistantId: config.assistantId,
        streamMode: ["messages", "updates"],
      })
      return async (...args: Parameters<typeof baseStream>) => {
        const [messages, streamConfig] = args
        return baseStream(messages, {
          ...streamConfig,
          runConfig: withGovernanceWorkspaceRunConfig(streamConfig.runConfig, workspaceId),
        })
      }
    },
    [client, config.assistantId, workspaceId],
  )
  const runtime = useLangGraphRuntime({
    stream,
    create: () => createGovernanceThread(client, workspaceId),
    eventHandlers: {
      onError: (error) => setStreamError(readLangGraphErrorMessage(error)),
    },
    load: async (threadId, loadConfig) => {
      const thread = await client.threads.get(threadId)
      assertGovernanceThreadWorkspace(thread, workspaceId.trim())
      const state = await client.threads.getState<LangGraphThreadValues>(threadId, undefined, {
        signal: loadConfig?.signal,
      })

      return {
        messages: state.values.messages ?? [],
        interrupts: [],
      }
    },
    unstable_enableMessageQueue: true,
    unstable_allowCancellation: true,
  })

  return (
    <GovernanceRuntimeStatusContext.Provider value={status}>
      <AssistantRuntimeProvider runtime={runtime}>{children}</AssistantRuntimeProvider>
    </GovernanceRuntimeStatusContext.Provider>
  )
}

export function GovernanceRuntimeProvider({ children, workspaceId = "" }: GovernanceRuntimeProviderProps) {
  const config = getGovernanceRuntimeConfig(import.meta.env)

  if (config.mode === "unavailable") return null

  return (
    <LangGraphGovernanceRuntimeProvider config={config} workspaceId={workspaceId}>
      {children}
    </LangGraphGovernanceRuntimeProvider>
  )
}

export function useGovernanceRuntimeStatus() {
  return useContext(GovernanceRuntimeStatusContext)
}

export function useGovernanceChatAvailability() {
  const config = getGovernanceRuntimeConfig(import.meta.env)
  const apiUrl = config.mode === "langgraph" ? config.apiUrl : ""
  const [available, setAvailable] = useState<boolean | undefined>(
    config.mode === "unavailable" ? false : undefined,
  )

  useEffect(() => {
    if (config.mode === "unavailable") return
    const controller = new AbortController()
    void fetch(joinApiPath(apiUrl, "/governance/chat/health"), { signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw new Error(`chat health ${response.status}`)
        return response.json() as Promise<{ configured?: boolean }>
      })
      .then((health) => setAvailable(health.configured === true))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setAvailable(false)
      })
    return () => controller.abort()
  }, [apiUrl, config.mode])

  return available
}
