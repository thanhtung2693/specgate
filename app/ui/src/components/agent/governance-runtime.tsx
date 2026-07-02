import { AssistantRuntimeProvider, type ChatModelAdapter, type RemoteThreadListAdapter, useLocalRuntime } from "@assistant-ui/react"
import { unstable_createLangGraphStream, useLangGraphRuntime, type LangChainMessage } from "@assistant-ui/react-langgraph"
import { Client } from "@langchain/langgraph-sdk"
import { createAssistantStream } from "assistant-stream"
import { createContext, useContext, useMemo, useState, type ReactNode } from "react"

type LocalGovernanceRuntimeConfig = {
  mode: "local"
  assistantId: string
}

type LangGraphGovernanceRuntimeConfig = {
  mode: "langgraph"
  apiUrl: string
  assistantId: string
}

type GovernanceRuntimeConfig = LocalGovernanceRuntimeConfig | LangGraphGovernanceRuntimeConfig

type LangGraphThreadValues = {
  messages?: LangChainMessage[]
}

type GovernanceRuntimeProviderProps = {
  children: ReactNode
}

type GovernanceRuntimeStatus = {
  streamError: string | null
  clearStreamError: () => void
}

const governanceThreadMetadata = {
  source: "specgate-ui",
  surface: "governance-agent",
} as const

const GovernanceRuntimeStatusContext = createContext<GovernanceRuntimeStatus>({
  streamError: null,
  clearStreamError: () => {},
})

type TitleMessage = Parameters<RemoteThreadListAdapter["generateTitle"]>[1][number]

export function getGovernanceRuntimeConfig(env: ImportMetaEnv): GovernanceRuntimeConfig {
  const apiUrl = normalizeApiUrl(env.VITE_LANGGRAPH_API_URL?.trim() ?? "")

  if (!apiUrl) {
    return {
      mode: "local",
      assistantId: "governance",
    }
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

function readLastUserText(messages: Parameters<ChatModelAdapter["run"]>[0]["messages"]) {
  const lastUser = [...messages].reverse().find((message) => message.role === "user")
  const parts = lastUser?.content ?? []
  return parts
    .map((part) => (part.type === "text" ? part.text : ""))
    .join(" ")
    .trim()
}

function readFirstUserText(messages: readonly TitleMessage[]) {
  const userMessage = messages.find((message) => message.role === "user")
  return (
    userMessage?.content
      .map((part) => (part.type === "text" ? part.text : ""))
      .join(" ")
      .trim() ?? ""
  )
}

function fallbackThreadTitle(messages: readonly TitleMessage[]) {
  const text = readFirstUserText(messages).replace(/\s+/g, " ").trim()
  if (!text) {
    return "Governance thread"
  }
  const shortened = text.split(" ").slice(0, 8).join(" ")
  return shortened.length > 56 ? `${shortened.slice(0, 53).trim()}...` : shortened
}

function joinApiPath(baseUrl: string, path: string) {
  return `${baseUrl.replace(/\/+$/, "")}/${path.replace(/^\/+/, "")}`
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
    return "The model provider is rate-limited. Wait a moment or configure a provider key in Models."
  }

  return message || "The governance agent could not finish this response."
}

function createGovernanceAdapter(): ChatModelAdapter {
  return {
    async run({ messages }) {
      const question = readLastUserText(messages)
      const answer = question
        ? [
            "I would start by keeping this as a small governed slice.",
            "",
            "1. Confirm the work item has a clear route and acceptance criteria.",
            "2. Build or refresh the Context Pack from the approved artifact.",
            "3. Run the narrowest verification that can fail for the touched surface.",
            "4. Report delivery evidence with checks, criteria, and remaining risk.",
            "",
            `For this prompt, I would inspect: "${question}".`,
          ].join("\n")
        : "Ask me about a gate, Context Pack, artifact version, or delivery review."

      return {
        content: [{ type: "text", text: answer }],
      }
    },
  }
}

function LocalGovernanceRuntimeProvider({ children }: GovernanceRuntimeProviderProps) {
  const adapter = useMemo(createGovernanceAdapter, [])
  const runtime = useLocalRuntime(adapter)
  const status = useMemo<GovernanceRuntimeStatus>(() => ({ streamError: null, clearStreamError: () => {} }), [])

  return (
    <GovernanceRuntimeStatusContext.Provider value={status}>
      <AssistantRuntimeProvider runtime={runtime}>{children}</AssistantRuntimeProvider>
    </GovernanceRuntimeStatusContext.Provider>
  )
}

function readThreadMetadata(thread: { metadata?: Record<string, unknown> | null }) {
  return thread.metadata ?? {}
}

function readThreadTitle(thread: { metadata?: Record<string, unknown> | null }) {
  const metadata = readThreadMetadata(thread)
  return typeof metadata.title === "string" && metadata.title.trim() ? metadata.title : "Governance thread"
}

function readThreadStatus(thread: { metadata?: Record<string, unknown> | null }) {
  return readThreadMetadata(thread).archived === true ? "archived" : "regular"
}

export function createLangGraphThreadListAdapter(client: Client, apiUrl: string): RemoteThreadListAdapter {
  return {
    list: async () => {
      const threads = await client.threads.search({
        metadata: governanceThreadMetadata,
        limit: 20,
        sortBy: "updated_at",
        sortOrder: "desc",
      })

      return {
        threads: threads.map((thread) => ({
          status: readThreadStatus(thread),
          remoteId: thread.thread_id,
          externalId: thread.thread_id,
          title: readThreadTitle(thread),
          lastMessageAt: new Date(thread.updated_at),
          custom: thread.metadata ?? undefined,
        })),
      }
    },
    initialize: async () => {
      const thread = await client.threads.create({
        metadata: {
          ...governanceThreadMetadata,
          title: "Governance thread",
        },
      })
      return { remoteId: thread.thread_id, externalId: thread.thread_id }
    },
    fetch: async (threadId) => {
      const thread = await client.threads.get(threadId)
      return {
        status: readThreadStatus(thread),
        remoteId: thread.thread_id,
        externalId: thread.thread_id,
        title: readThreadTitle(thread),
        lastMessageAt: new Date(thread.updated_at),
        custom: thread.metadata ?? undefined,
      }
    },
    rename: async (threadId, title) => {
      const thread = await client.threads.get(threadId)
      await client.threads.update(threadId, {
        metadata: {
          ...(thread.metadata ?? {}),
          title,
        },
        returnMinimal: true,
      })
    },
    archive: async (threadId) => {
      const thread = await client.threads.get(threadId)
      await client.threads.update(threadId, {
        metadata: {
          ...readThreadMetadata(thread),
          archived: true,
        },
        returnMinimal: true,
      })
    },
    unarchive: async (threadId) => {
      const thread = await client.threads.get(threadId)
      await client.threads.update(threadId, {
        metadata: {
          ...readThreadMetadata(thread),
          archived: false,
        },
        returnMinimal: true,
      })
    },
    delete: async (threadId) => {
      await client.threads.delete(threadId)
    },
    generateTitle: async (threadId, messages) => {
      const requestText = readFirstUserText(messages)
      let title = ""

      try {
        const res = await fetch(joinApiPath(apiUrl, `/governance/threads/${encodeURIComponent(threadId)}/title`), {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ request_text: requestText, request_type: "governance-chat" }),
        })

        if (res.ok) {
          const body = (await res.json()) as { title?: unknown }
          title = typeof body.title === "string" ? body.title.trim() : ""
        }
      } catch {
        title = ""
      }

      title ||= fallbackThreadTitle(messages)
      try {
        const thread = await client.threads.get(threadId)
        await client.threads.update(threadId, {
          metadata: {
            ...(thread.metadata ?? {}),
            title,
          },
          returnMinimal: true,
        })
      } catch {
        // The generated title can still render for the current session.
      }

      return createAssistantStream((controller) => {
        controller.appendText(title)
      })
    },
  }
}

function LangGraphGovernanceRuntimeProvider({
  children,
  config,
}: GovernanceRuntimeProviderProps & { config: LangGraphGovernanceRuntimeConfig }) {
  const client = useMemo(() => new Client({ apiUrl: config.apiUrl, apiKey: null }), [config.apiUrl])
  const threadListAdapter = useMemo(() => createLangGraphThreadListAdapter(client, config.apiUrl), [client, config.apiUrl])
  const [streamError, setStreamError] = useState<string | null>(null)
  const status = useMemo<GovernanceRuntimeStatus>(
    () => ({
      streamError,
      clearStreamError: () => setStreamError(null),
    }),
    [streamError],
  )
  const stream = useMemo(
    () =>
      unstable_createLangGraphStream({
        client,
        assistantId: config.assistantId,
        streamMode: ["messages", "updates"],
      }),
    [client, config.assistantId],
  )
  const runtime = useLangGraphRuntime({
    stream,
    eventHandlers: {
      onError: (error) => setStreamError(readLangGraphErrorMessage(error)),
    },
    load: async (threadId, loadConfig) => {
      const state = await client.threads.getState<LangGraphThreadValues>(threadId, undefined, {
        signal: loadConfig?.signal,
      })

      return {
        messages: state.values.messages ?? [],
        interrupts: [],
      }
    },
    unstable_threadListAdapter: threadListAdapter,
    unstable_enableMessageQueue: true,
    unstable_allowCancellation: true,
  })

  return (
    <GovernanceRuntimeStatusContext.Provider value={status}>
      <AssistantRuntimeProvider runtime={runtime}>{children}</AssistantRuntimeProvider>
    </GovernanceRuntimeStatusContext.Provider>
  )
}

export function GovernanceRuntimeProvider({ children }: GovernanceRuntimeProviderProps) {
  const config = getGovernanceRuntimeConfig(import.meta.env)

  if (config.mode === "langgraph") {
    return <LangGraphGovernanceRuntimeProvider config={config}>{children}</LangGraphGovernanceRuntimeProvider>
  }

  return <LocalGovernanceRuntimeProvider>{children}</LocalGovernanceRuntimeProvider>
}

export function useGovernanceRuntimeStatus() {
  return useContext(GovernanceRuntimeStatusContext)
}
