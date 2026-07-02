import {
  AssistantModalPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  ThreadListItemPrimitive,
  ThreadListPrimitive,
  unstable_useSlashCommandAdapter,
  type MessageState,
  useAui,
  useAuiState,
  useThreadListItemRuntime,
} from "@assistant-ui/react"
import { useEffect, useState, type FormEvent, type ReactNode } from "react"
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  ArrowLeftIcon,
  ArrowUpIcon,
  BotIcon,
  CheckCircle2Icon,
  CheckIcon,
  GitPullRequestArrowIcon,
  PencilIcon,
  MessageSquareTextIcon,
  PackageCheckIcon,
  PlusIcon,
  SearchIcon,
  SquareIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"

import { ComposerTriggerPopover } from "@/components/agent/composer-trigger-popover"
import {
  GOVERNANCE_AGENT_PROMPT_EVENT,
  takePendingGovernanceAgentPrompts,
} from "@/components/agent/governance-agent-events"
import { getGovernanceRuntimeConfig, GovernanceRuntimeProvider, useGovernanceRuntimeStatus } from "@/components/agent/governance-runtime"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

function formatThreadAge(value?: Date) {
  if (!value) {
    return "now"
  }

  const diffMs = Math.max(0, Date.now() - value.getTime())
  const minutes = Math.max(1, Math.floor(diffMs / 60_000))
  if (minutes < 60) {
    return `${minutes}m`
  }

  const hours = Math.floor(minutes / 60)
  if (hours < 24) {
    return `${hours}h`
  }

  const days = Math.floor(hours / 24)
  if (days < 30) {
    return `${days}d`
  }

  const months = Math.floor(days / 30)
  if (months < 12) {
    return `${months}mo`
  }

  return `${Math.floor(months / 12)}y`
}

export function hasVisibleMessageContent(message: MessageState) {
  if (message.role === "user") return true
  if (message.role !== "assistant") return false

  return message.content.some((part) => {
    if (part.type === "text") return part.text.trim().length > 0
    if (part.type === "reasoning") return false
    return true
  })
}

function Message() {
  return (
    <MessagePrimitive.Root className="grid gap-1.5">
      <MessagePrimitive.If user>
        <div className="ml-auto max-w-[82%] rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground">
          <MessagePrimitive.Content />
        </div>
      </MessagePrimitive.If>
      <MessagePrimitive.If assistant>
        <div className="mr-auto max-w-[92%] px-1 py-1 text-sm leading-6 text-foreground">
          <MessagePrimitive.Content />
        </div>
      </MessagePrimitive.If>
    </MessagePrimitive.Root>
  )
}

function ActionTooltip({ content, children }: { content: string; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent>{content}</TooltipContent>
    </Tooltip>
  )
}

function ThinkingMessage() {
  return (
    <ThreadPrimitive.If running>
      <div className="flex items-center gap-2 px-1 py-2 text-sm" aria-live="polite">
        <span className="sg-thinking-shimmer bg-[linear-gradient(110deg,var(--muted-foreground),var(--foreground),var(--muted-foreground))] bg-[length:220%_100%] bg-clip-text text-transparent">
          Thinking…
        </span>
      </div>
    </ThreadPrimitive.If>
  )
}

function Composer() {
  const aui = useAui()
  const { clearStreamError } = useGovernanceRuntimeStatus()
  const commands = unstable_useSlashCommandAdapter({
    removeOnExecute: true,
    iconMap: {
      evidence: CheckCircle2Icon,
      gates: PackageCheckIcon,
      handoff: GitPullRequestArrowIcon,
    },
    commands: [
      {
        id: "handoff",
        label: "Prepare handoff",
        description: "Draft the next handoff question from selected context",
        icon: "handoff",
        execute: () =>
          aui.thread().append({
            role: "user",
            content: [{ type: "text", text: "Prepare the governed handoff for this work." }],
          }),
      },
      {
        id: "gates",
        label: "Review gates",
        description: "Check readiness and delivery review expectations",
        icon: "gates",
        execute: () =>
          aui.thread().append({
            role: "user",
            content: [{ type: "text", text: "Review the expected gates and any missing evidence." }],
          }),
      },
      {
        id: "evidence",
        label: "Evidence summary",
        description: "Create a concise delivery evidence outline",
        icon: "evidence",
        execute: () =>
          aui.thread().append({
            role: "user",
            content: [{ type: "text", text: "Create the delivery evidence summary for this change." }],
          }),
      },
    ],
  })

  return (
    <ComposerPrimitive.Unstable_TriggerPopoverRoot>
      <ComposerPrimitive.Root className="relative rounded-lg border bg-background p-2 shadow-xs">
        <ComposerTriggerPopover char="/" {...commands} />
        <div className="flex items-end gap-2">
          <ComposerPrimitive.Input
            aria-label="Message governance agent"
            className="max-h-32 min-h-24 flex-1 resize-none bg-transparent px-2 py-1 text-sm outline-none placeholder:text-muted-foreground"
            placeholder="Ask about gate failures, blockers, or artifacts. Type / for commands"
            submitMode="enter"
          />
          <ThreadPrimitive.If running={false}>
            <Tooltip>
              <TooltipTrigger asChild>
                <ComposerPrimitive.Send asChild>
                  <Button aria-label="Send message" size="icon" onClick={clearStreamError}>
                    <ArrowUpIcon />
                  </Button>
                </ComposerPrimitive.Send>
              </TooltipTrigger>
              <TooltipContent>Send message</TooltipContent>
            </Tooltip>
          </ThreadPrimitive.If>
          <ThreadPrimitive.If running>
            <Tooltip>
              <TooltipTrigger asChild>
                <ComposerPrimitive.Cancel asChild>
                  <Button aria-label="Stop response" size="icon" variant="secondary">
                    <SquareIcon />
                  </Button>
                </ComposerPrimitive.Cancel>
              </TooltipTrigger>
              <TooltipContent>Stop the current response</TooltipContent>
            </Tooltip>
          </ThreadPrimitive.If>
        </div>
      </ComposerPrimitive.Root>
    </ComposerPrimitive.Unstable_TriggerPopoverRoot>
  )
}

function StreamErrorNotice() {
  const { clearStreamError, streamError } = useGovernanceRuntimeStatus()

  if (!streamError) return null

  return (
    <div className="mx-3 mb-3 flex items-start gap-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning">
      <p className="min-w-0 flex-1 leading-5">{streamError}</p>
      <Button
        variant="ghost"
        size="icon-sm"
        className="size-6 text-warning hover:bg-warning/15 hover:text-warning"
        aria-label="Dismiss agent error"
        onClick={clearStreamError}
      >
        <XIcon />
      </Button>
    </div>
  )
}

type GovernanceThreadListItemView = {
  title?: string | undefined
  lastMessageAt?: Date | undefined
  status?: string | undefined
}

function getThreadTitle(threadListItem: GovernanceThreadListItemView) {
  return threadListItem.title?.trim() || "Governance thread"
}

function GovernanceThreadListItem({
  onThreadSelect,
  onThreadRestore,
  query,
  threadListItem,
}: {
  onThreadSelect: () => void
  onThreadRestore?: () => void
  query: string
  threadListItem: GovernanceThreadListItemView
}) {
  const aui = useAui()
  const itemRuntime = useThreadListItemRuntime()
  const [deleteRequested, setDeleteRequested] = useState(false)
  const [isRenaming, setIsRenaming] = useState(false)
  const [renameValue, setRenameValue] = useState(getThreadTitle(threadListItem))
  const title = getThreadTitle(threadListItem)
  const matchesQuery = !query || title.toLowerCase().includes(query)
  const isArchived = threadListItem.status === "archived"

  if (!matchesQuery) return null

  async function handleRename(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextTitle = renameValue.trim()
    if (!nextTitle) return
    if (nextTitle !== title) {
      await itemRuntime.rename(nextTitle)
    }
    setIsRenaming(false)
  }

  async function handleRestoreThread() {
    await itemRuntime.unarchive()
    await aui.threads().reload()
    onThreadRestore?.()
  }

  return (
    <ThreadListItemPrimitive.Root className="group flex items-center gap-1 rounded-md border bg-card/70 p-1 transition-colors data-active:bg-muted/60">
      {isRenaming ? (
        <form className="flex min-w-0 flex-1 items-center gap-1" onSubmit={handleRename}>
          <input
            aria-label="Thread title"
            className="h-8 min-w-0 flex-1 rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            value={renameValue}
            onChange={(event) => setRenameValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                setRenameValue(title)
                setIsRenaming(false)
              }
            }}
            autoFocus
          />
          <ActionTooltip content="Save title">
            <Button type="submit" size="icon-sm" aria-label="Save thread title">
              <CheckIcon />
            </Button>
          </ActionTooltip>
          <ActionTooltip content="Cancel rename">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Cancel rename ${title}`}
              onClick={() => {
                setRenameValue(title)
                setIsRenaming(false)
              }}
            >
              <XIcon />
            </Button>
          </ActionTooltip>
        </form>
      ) : (
        <>
          {isArchived ? (
            <div className="min-w-0 flex-1 rounded px-2 py-1.5">
              <span className="block truncate text-xs font-medium">
                <ThreadListItemPrimitive.Title fallback="Governance thread" />
              </span>
              <span className="mt-0.5 block text-[11px] text-muted-foreground">
                Archived {formatThreadAge(threadListItem.lastMessageAt)} ago
              </span>
            </div>
          ) : (
            <ThreadListItemPrimitive.Trigger
              className="min-w-0 flex-1 rounded px-2 py-1.5 text-left transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
              onClick={onThreadSelect}
            >
              <span className="block truncate text-xs font-medium">
                <ThreadListItemPrimitive.Title fallback="Governance thread" />
              </span>
              <span className="mt-0.5 block text-[11px] text-muted-foreground">
                {formatThreadAge(threadListItem.lastMessageAt)}
              </span>
            </ThreadListItemPrimitive.Trigger>
          )}
          {deleteRequested ? (
            <div className="flex shrink-0 items-center gap-1">
              <ThreadListItemPrimitive.Delete asChild>
                <Button size="icon-sm" variant="destructive" aria-label={`Confirm delete ${title}`}>
                  <CheckIcon />
                </Button>
              </ThreadListItemPrimitive.Delete>
              <ActionTooltip content="Cancel delete">
                <Button
                  size="icon-sm"
                  variant="ghost"
                  aria-label={`Cancel delete ${title}`}
                  onClick={() => setDeleteRequested(false)}
                >
                  <XIcon />
                </Button>
              </ActionTooltip>
            </div>
          ) : (
            <div className="flex shrink-0 items-center gap-1">
              {isArchived ? (
                <ActionTooltip content="Restore thread to active history">
                  <Button type="button" variant="ghost" size="icon-sm" aria-label={`Restore thread ${title}`} onClick={handleRestoreThread}>
                    <ArchiveRestoreIcon />
                  </Button>
                </ActionTooltip>
              ) : (
                <>
                  <ActionTooltip content="Rename thread">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Rename thread ${title}`}
                      onClick={() => {
                        setRenameValue(title)
                        setIsRenaming(true)
                      }}
                    >
                      <PencilIcon />
                    </Button>
                  </ActionTooltip>
                  <ActionTooltip content="Archive thread">
                    <ThreadListItemPrimitive.Archive asChild>
                      <Button type="button" variant="ghost" size="icon-sm" aria-label={`Archive thread ${title}`}>
                        <ArchiveIcon />
                      </Button>
                    </ThreadListItemPrimitive.Archive>
                  </ActionTooltip>
                </>
              )}
              <ActionTooltip content="Delete thread">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Delete thread ${title}`}
                  onClick={() => setDeleteRequested(true)}
                >
                  <Trash2Icon />
                </Button>
              </ActionTooltip>
            </div>
          )}
        </>
      )}
    </ThreadListItemPrimitive.Root>
  )
}

function GovernanceThreadList({ onThreadSelect }: { onThreadSelect: () => void }) {
  const { archivedThreadIds, isLoading, mainThreadId, newThreadId, threadIds } = useAuiState((state) => state.threads)
  const [query, setQuery] = useState("")
  const [showArchived, setShowArchived] = useState(false)
  const isDraftThread = mainThreadId === newThreadId
  const activeCount = threadIds.length
  const archivedCount = archivedThreadIds.length
  const visibleCount = showArchived ? archivedCount : activeCount
  const hasVisibleThreads = visibleCount > 0
  const normalizedQuery = query.trim().toLowerCase()

  useEffect(() => {
    if (showArchived && archivedCount === 0) {
      setShowArchived(false)
    }
  }, [archivedCount, showArchived])

  return (
    <ThreadListPrimitive.Root className="flex min-h-0 flex-1 flex-col bg-background/70">
      <div className="flex items-center justify-between gap-3 px-4 py-3">
        <div>
          <h3 className="text-sm font-semibold">Threads</h3>
          <p className="text-xs text-muted-foreground">Recent governance conversations</p>
        </div>
        <ThreadListPrimitive.New
          aria-label="New agent thread"
          className="inline-flex h-7 items-center gap-1 rounded-md border bg-card px-2 text-xs font-medium transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:opacity-50"
          onClick={onThreadSelect}
        >
          <PlusIcon className="size-3.5" />
          New thread
        </ThreadListPrimitive.New>
      </div>
      <div className="px-4 pb-3">
        <label className="relative block">
          <SearchIcon className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <input
            type="search"
            aria-label="Search agent threads"
            className="h-8 w-full rounded-md border bg-background pl-8 pr-2 text-xs outline-none placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
            placeholder="Search threads"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </label>
        {archivedCount > 0 ? (
          <div className="mt-2 grid grid-cols-2 gap-1 rounded-md border bg-card/70 p-1">
            <Button
              type="button"
              variant={showArchived ? "ghost" : "secondary"}
              size="sm"
              className="h-7 rounded text-xs"
              aria-label="Show active threads"
              aria-pressed={!showArchived}
              onClick={() => setShowArchived(false)}
            >
              Active {activeCount}
            </Button>
            <Button
              type="button"
              variant={showArchived ? "secondary" : "ghost"}
              size="sm"
              className="h-7 rounded text-xs"
              aria-label="Show archived threads"
              aria-pressed={showArchived}
              onClick={() => setShowArchived(true)}
            >
              Archived {archivedCount}
            </Button>
          </div>
        ) : null}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-3 pb-3">
        <div className="grid gap-1">
        {isLoading ? (
          <p className="rounded-md border bg-card/70 px-3 py-2 text-xs text-muted-foreground">Loading threads...</p>
        ) : null}
        {!isLoading && !hasVisibleThreads ? (
          <div className="rounded-md border bg-card/70 px-3 py-2">
            <p className="text-xs font-medium">
              {showArchived ? "No archived threads" : isDraftThread ? "Current draft" : "No saved threads yet"}
            </p>
            <p className="mt-0.5 text-[11px] leading-4 text-muted-foreground">
              {showArchived
                ? "Archived governance conversations will appear here."
                : "Send a message to make this conversation appear in the thread list."}
            </p>
          </div>
        ) : null}
        <ThreadListPrimitive.Items archived={showArchived}>
          {({ threadListItem }) => (
            <GovernanceThreadListItem
              onThreadSelect={onThreadSelect}
              onThreadRestore={() => setShowArchived(false)}
              query={normalizedQuery}
              threadListItem={threadListItem}
            />
          )}
        </ThreadListPrimitive.Items>
        <ThreadListPrimitive.LoadMore className="h-7 rounded-md border bg-card px-2 text-xs font-medium transition-colors hover:bg-muted disabled:hidden">
          Load more
        </ThreadListPrimitive.LoadMore>
        {!showArchived && archivedCount > 0 ? (
          <p className="px-1 pt-1 text-[11px] text-muted-foreground">{archivedCount} archived threads available.</p>
        ) : null}
        </div>
      </div>
    </ThreadListPrimitive.Root>
  )
}

type ChatModelHealth = {
  status: "loading" | "ready"
  configured: boolean
}

// Probes GET /governance/chat/health on the agents service so an unconfigured
// chat model shows a capability placeholder instead of a dead composer.
// Fails open: probe errors (older service, network) keep the chat usable.
function useChatModelHealth(): ChatModelHealth {
  const [health, setHealth] = useState<ChatModelHealth>(() => {
    const config = getGovernanceRuntimeConfig(import.meta.env)
    return config.mode === "langgraph" ? { status: "loading", configured: true } : { status: "ready", configured: true }
  })

  useEffect(() => {
    const config = getGovernanceRuntimeConfig(import.meta.env)
    if (config.mode !== "langgraph") return

    const controller = new AbortController()
    void fetch(`${config.apiUrl}/governance/chat/health`, { signal: controller.signal })
      .then((response) => (response.ok ? (response.json() as Promise<{ configured?: boolean }>) : Promise.reject(new Error(`chat health ${response.status}`))))
      .then((payload) => setHealth({ status: "ready", configured: payload?.configured !== false }))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setHealth({ status: "ready", configured: true })
      })
    return () => controller.abort()
  }, [])

  return health
}

const chatCapabilities = [
  "Explain why a gate failed and what unblocks it",
  "Summarize blockers and handoff readiness for a work item",
  "Read and compare artifact documents",
  "Run artifact readiness checks on demand",
]

function ChatModelPlaceholder() {
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4" data-testid="chat-model-placeholder">
      <div>
        <h3 className="text-sm font-semibold">Chat model not configured</h3>
        <p className="mt-1 text-xs text-muted-foreground">
          The governance agent can answer questions once its chat model has an API key.
        </p>
      </div>
      <ul className="grid gap-1.5 text-xs text-muted-foreground">
        {chatCapabilities.map((capability) => (
          <li key={capability} className="flex items-start gap-2">
            <BotIcon className="mt-0.5 size-3.5 shrink-0" />
            <span>{capability}</span>
          </li>
        ))}
      </ul>
      <div className="rounded-md border bg-background/70 p-3 text-xs text-muted-foreground">
        <p className="font-medium text-foreground">Add the key</p>
        <p className="mt-1">
          Set <code className="font-mono">GOVERNANCE_OPS_API_KEY</code> (and optionally{" "}
          <code className="font-mono">GOVERNANCE_OPS_MODEL</code> /{" "}
          <code className="font-mono">GOVERNANCE_OPS_MODEL_PROVIDER</code>) on the agents service, then restart it.
        </p>
        <p className="mt-2">
          This is separate from Settings → Models, which configures the server-side model for gates, classification,
          and delivery review.
        </p>
      </div>
    </div>
  )
}

function GovernanceAgentPanel() {
  const [view, setView] = useState<"chat" | "threads">("chat")
  const chatModel = useChatModelHealth()

  return (
    <section
      className="flex h-[min(640px,calc(100vh-5rem))] min-h-0 flex-col rounded-lg border bg-card text-card-foreground shadow-xs"
      aria-label="Governance agent"
    >
      <header className="flex items-center justify-between border-b px-4 py-3">
        <div className="flex items-center gap-2">
          {view === "threads" ? (
            <ActionTooltip content="Back to chat">
              <Button variant="ghost" size="icon-sm" aria-label="Back to chat" onClick={() => setView("chat")}>
                <ArrowLeftIcon />
              </Button>
            </ActionTooltip>
          ) : (
            <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
              <BotIcon />
            </span>
          )}
          <div>
            <h2 className="text-sm font-semibold">{view === "threads" ? "Threads" : "Governance agent"}</h2>
            <p className="text-xs text-muted-foreground">
              {view === "threads" ? "Pick up a prior conversation" : "Context, gates, and delivery review"}
            </p>
          </div>
        </div>
        {view === "chat" ? (
          <ActionTooltip content="View governance threads">
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="View agent threads"
              onClick={() => setView("threads")}
            >
              <MessageSquareTextIcon />
            </Button>
          </ActionTooltip>
        ) : null}
      </header>

      {view === "threads" ? <GovernanceThreadList onThreadSelect={() => setView("chat")} /> : null}

      {view === "chat" && chatModel.status === "ready" && !chatModel.configured ? <ChatModelPlaceholder /> : null}

      {chatModel.status === "ready" && !chatModel.configured ? null : (
      <ThreadPrimitive.Root className={cn("min-h-0 flex-1 flex-col", view === "threads" ? "hidden" : "flex")}>
        <ThreadPrimitive.Viewport className="min-h-0 flex-1">
          <ScrollArea className="h-full">
            <div className="flex min-h-[320px] flex-col gap-4 p-4">
              <ThreadPrimitive.Empty>
                <Empty className="min-h-[280px] border-0">
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <BotIcon />
                    </EmptyMedia>
                    <EmptyTitle>Governance path</EmptyTitle>
                    <EmptyDescription>Readiness, Context Packs, evidence, and review outcomes.</EmptyDescription>
                  </EmptyHeader>
                </Empty>
              </ThreadPrimitive.Empty>
              <ThreadPrimitive.Messages>
                {({ message }) => (hasVisibleMessageContent(message) ? <Message /> : null)}
              </ThreadPrimitive.Messages>
              <ThinkingMessage />
            </div>
          </ScrollArea>
        </ThreadPrimitive.Viewport>
        <div className="border-t p-3">
          <StreamErrorNotice />
          <Composer />
        </div>
      </ThreadPrimitive.Root>
      )}
    </section>
  )
}

function GovernanceAgentPromptBridge() {
  const aui = useAui()

  useEffect(() => {
    function appendPendingPrompts() {
      for (const prompt of takePendingGovernanceAgentPrompts()) {
        const text = prompt.trim()
        if (!text) continue
        aui.thread().append({
          role: "user",
          content: [{ type: "text", text }],
        })
      }
    }

    function handlePrompt(_event: Event) {
      appendPendingPrompts()
    }

    appendPendingPrompts()
    window.addEventListener(GOVERNANCE_AGENT_PROMPT_EVENT, handlePrompt)
    return () => window.removeEventListener(GOVERNANCE_AGENT_PROMPT_EVENT, handlePrompt)
  }, [aui])

  return null
}

export function GovernanceAgent() {
  return (
    <GovernanceRuntimeProvider>
      <GovernanceAgentPanel />
    </GovernanceRuntimeProvider>
  )
}

export function GovernanceAssistantModal() {
  return (
    <GovernanceRuntimeProvider>
      <AssistantModalPrimitive.Root unstable_openOnRunStart>
        <GovernanceAgentPromptBridge />
        <Tooltip>
          <TooltipTrigger asChild>
            <AssistantModalPrimitive.Trigger asChild>
              <Button
                variant="outline"
                size="icon-lg"
                className="rounded-lg border bg-card text-card-foreground shadow-sm hover:bg-accent"
                aria-label="Open governance agent"
              >
                <BotIcon />
              </Button>
            </AssistantModalPrimitive.Trigger>
          </TooltipTrigger>
          <TooltipContent>Open governance agent</TooltipContent>
        </Tooltip>
        <AssistantModalPrimitive.Content
          align="end"
          sideOffset={10}
          className="z-50 w-[min(calc(100vw-1rem),460px)] outline-none"
        >
          <GovernanceAgentPanel />
        </AssistantModalPrimitive.Content>
      </AssistantModalPrimitive.Root>
    </GovernanceRuntimeProvider>
  )
}
