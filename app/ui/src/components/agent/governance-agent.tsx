import {
  AssistantModalPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  unstable_useSlashCommandAdapter,
  type MessageState,
  useAui,
} from "@assistant-ui/react"
import {
  ArrowUpIcon,
  BotIcon,
  FileTextIcon,
  SearchIcon,
  ShieldCheckIcon,
  SquareIcon,
  XIcon,
} from "lucide-react"

import { ComposerTriggerPopover } from "@/components/agent/composer-trigger-popover"
import {
  GovernanceRuntimeProvider,
  useGovernanceChatAvailability,
  useGovernanceRuntimeStatus,
} from "@/components/agent/governance-runtime"
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
      artifact: FileTextIcon,
      knowledge: SearchIcon,
      readiness: ShieldCheckIcon,
    },
    commands: [
      {
        id: "artifact",
        label: "Artifact summary",
        description: "Read one exact artifact and its documents",
        icon: "artifact",
        execute: () =>
          aui.thread().append({
            role: "user",
            content: [{ type: "text", text: "Ask me for an exact artifact ID, then summarize its metadata and documents." }],
          }),
      },
      {
        id: "readiness",
        label: "Readiness results",
        description: "Explain stored readiness results for one artifact",
        icon: "readiness",
        execute: () =>
          aui.thread().append({
            role: "user",
            content: [{ type: "text", text: "Ask me for an exact artifact ID, then explain its stored readiness results." }],
          }),
      },
      {
        id: "knowledge",
        label: "Knowledge search",
        description: "Search active-workspace Governance Knowledge",
        icon: "knowledge",
        execute: () =>
          aui.thread().append({
            role: "user",
            content: [{ type: "text", text: "Ask what governance topic I want to search for in this workspace's Governance Knowledge." }],
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
            placeholder="Ask about artifacts, readiness results, or Governance Knowledge. Type / for prompts"
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
              <TooltipContent>Stop current response</TooltipContent>
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

function GovernanceWorkspaceRequired() {
  return (
    <section
      className="flex h-[min(640px,calc(100vh-5rem))] min-h-0 flex-col rounded-lg border bg-card text-card-foreground shadow-xs"
      aria-label="Governance agent"
    >
      <header className="flex items-center gap-2 border-b px-4 py-3">
        <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
          <BotIcon />
        </span>
        <div>
          <h2 className="text-sm font-semibold">Select a workspace</h2>
          <p className="text-xs text-muted-foreground">Governance conversations are workspace-bound.</p>
        </div>
      </header>
      <div className="flex flex-1 items-center p-5">
        <p className="text-sm leading-6 text-muted-foreground">Choose a workspace before starting a governed conversation.</p>
      </div>
    </section>
  )
}

function GovernanceAgentPanel({ workspaceId }: GovernanceAgentProps) {
  if (!workspaceId?.trim()) return <GovernanceWorkspaceRequired />

  return (
    <section
      className="flex h-[min(640px,calc(100vh-5rem))] min-h-0 flex-col rounded-lg border bg-card text-card-foreground shadow-xs"
      aria-label="Governance agent"
    >
      <header className="flex items-center gap-2 border-b px-4 py-3">
        <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
          <BotIcon />
        </span>
        <div>
          <h2 className="text-sm font-semibold">Governance agent</h2>
          <p className="text-xs text-muted-foreground">Artifact and readiness advisor</p>
        </div>
      </header>
      <ThreadPrimitive.Root className="flex min-h-0 flex-1 flex-col">
        <ThreadPrimitive.Viewport className="min-h-0 flex-1">
          <ScrollArea className="h-full">
            <div className="flex min-h-[320px] flex-col gap-4 p-4">
              <ThreadPrimitive.Empty>
                <Empty className="min-h-[280px] border-0">
                  <EmptyHeader>
                    <EmptyMedia variant="icon"><BotIcon /></EmptyMedia>
                    <EmptyTitle>Governance reference</EmptyTitle>
                    <EmptyDescription>Artifacts, stored readiness, and Governance Knowledge.</EmptyDescription>
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
    </section>
  )
}

type GovernanceAgentProps = {
  workspaceId?: string
}

export function GovernanceAgent({ workspaceId }: GovernanceAgentProps = {}) {
  const available = useGovernanceChatAvailability()
  if (!available) return null

  return (
    <GovernanceRuntimeProvider key={workspaceId ?? "no-workspace"} workspaceId={workspaceId}>
      <GovernanceAgentPanel workspaceId={workspaceId} />
    </GovernanceRuntimeProvider>
  )
}

export function GovernanceAssistantModal({ workspaceId }: GovernanceAgentProps = {}) {
  const available = useGovernanceChatAvailability()
  if (!available) return null

  return (
    <GovernanceRuntimeProvider key={workspaceId ?? "no-workspace"} workspaceId={workspaceId}>
      <AssistantModalPrimitive.Root>
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
          <GovernanceAgentPanel workspaceId={workspaceId} />
        </AssistantModalPrimitive.Content>
      </AssistantModalPrimitive.Root>
    </GovernanceRuntimeProvider>
  )
}
