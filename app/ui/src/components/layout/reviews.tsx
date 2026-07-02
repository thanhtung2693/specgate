// Reviews domain: review queue helpers, proposal/feedback dialogs, and the
// Reviews page. Extracted from app-shell.
import { BotIcon, CheckIcon, ExternalLinkIcon, EyeIcon, FileTextIcon, SearchIcon, XIcon } from "lucide-react"
import { useMemo, useState } from "react"
import { Link, useNavigate } from "react-router-dom"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import {
  approveArtifactProposal,
  rejectArtifactProposal,
  useArtifactProposalDiff,
  useArtifactProposals,
  useGovernanceFeedbackSignals,
  type ArtifactProposalSummary,
  type GovernanceFeedbackSignal,
} from "@/data/reviews"
import { type WorkboardView } from "@/data/workboard"
import { type WorkItem } from "@/data/workspace"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { readableKey, reviewTone, statusTone, toneClass } from "./shared"
import { ActionTooltip, MarkdownText, runGovernanceAgentPrompt } from "./shared-ui"

type ReviewFilter = "all" | "needs_changes" | "ready" | "gate_failed"

function reviewItems(workItems: WorkItem[]) {
  return workItems.filter((item) => item.delivery !== "not_started" || item.gate === "fail")
}

function reviewFilterCount(workItems: WorkItem[], filter: ReviewFilter) {
  return reviewItems(workItems).filter((item) => reviewFilterMatches(item, filter)).length
}

function reviewFilterMatches(item: WorkItem, filter: ReviewFilter) {
  if (filter === "all") return true
  if (filter === "needs_changes") return item.delivery === "needs_changes"
  if (filter === "ready") return (item.delivery === "ready" || item.delivery === "passed") && item.gate !== "fail"
  return item.gate === "fail"
}

function reviewReason(item: WorkItem) {
  if (item.delivery === "needs_changes") {
    return item.blocker !== "none" ? item.blocker : "Delivery review needs changes."
  }
  if (item.gate === "fail") return "A required gate failed."
  if (item.delivery === "ready") return item.contextPackId ? "Ready for delivery review with Context Pack." : "Ready for delivery review."
  if (item.delivery === "passed") return "Delivery review passed."
  return item.blocker !== "none" ? item.blocker : "Needs human review."
}

function reviewLabel(item: WorkItem) {
  if (item.delivery === "needs_changes") return "Needs changes"
  if (item.gate === "fail") return "Gate failed"
  if (item.delivery === "ready") return "Ready for review"
  if (item.delivery === "passed") return "Passed"
  return "Review"
}

function reviewActionLabel(item: WorkItem) {
  if (item.delivery === "needs_changes" || item.gate === "fail") return "Inspect review gaps"
  if (item.delivery === "passed") return "Inspect review outcome"
  return "Inspect review"
}

function reviewSearchMatches(item: WorkItem, search: string) {
  const query = search.trim().toLowerCase()
  if (!query) return true
  return [
    item.key,
    item.title,
    item.owner,
    item.agent,
    item.route,
    reviewLabel(item),
    reviewReason(item),
    item.contextPackId ?? "",
  ].some((value) => value.toLowerCase().includes(query))
}

function proposalSearchMatches(proposal: ArtifactProposalSummary, search: string) {
  const query = search.trim().toLowerCase()
  if (!query) return true
  return [
    proposal.id,
    proposal.baseArtifactId,
    proposal.baseVersion,
    proposal.diffSummary,
    proposal.sourceKind,
    proposal.sourceId,
  ].some((value) => value.toLowerCase().includes(query))
}

function feedbackSignalSearchMatches(signal: GovernanceFeedbackSignal, search: string) {
  const query = search.trim().toLowerCase()
  if (!query) return true
  return [
    signal.id,
    signal.eventType,
    signal.status,
    signal.reason,
    signal.changeRequestId ?? "",
    signal.artifactId ?? "",
    signal.featureId ?? "",
    signal.sourceLabel,
  ].some((value) => value.toLowerCase().includes(query))
}

function proposalSourceLabel(proposal: ArtifactProposalSummary) {
  if (proposal.sourceKind === "feedback_event") return "Feedback"
  if (proposal.sourceKind === "gate") return "Gate"
  if (proposal.sourceKind === "coding_agent_update") return "Coding agent"
  return readableKey(proposal.sourceKind)
}

function ArtifactProposalDiffDialog({
  proposal,
  open,
  onOpenChange,
}: {
  proposal: ArtifactProposalSummary | undefined
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const diff = useArtifactProposalDiff(proposal, open)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[86vh] overflow-hidden p-0 sm:max-w-5xl">
        <DialogHeader className="border-b px-5 py-4">
          <div className="flex flex-wrap items-start justify-between gap-3 pr-8">
            <div>
              <DialogTitle>Proposal diff</DialogTitle>
              <DialogDescription className="mt-1 font-mono text-xs">
                {proposal?.id ?? "No proposal selected"}
              </DialogDescription>
            </div>
            {proposal ? (
              <div className="flex flex-wrap gap-2">
                <Badge variant="outline" className="border">{proposalSourceLabel(proposal)}</Badge>
                <Badge variant="secondary" className="font-mono">{proposal.baseVersion}</Badge>
              </div>
            ) : null}
          </div>
        </DialogHeader>
        <div className="grid max-h-[calc(86vh-5.5rem)] gap-4 overflow-y-auto px-5 py-4">
          {diff.status === "loading" ? (
            <p className="text-sm text-muted-foreground">Loading proposal diff...</p>
          ) : null}
          {diff.status === "error" ? (
            <p className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-muted-foreground">
              Live proposal diff is unavailable. Check Doc Registry and retry; no fallback diff is shown in live mode.
            </p>
          ) : null}
          <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
            <div>
              <h3 className="text-sm font-semibold">{diff.summary}</h3>
              <p className="mt-1 text-xs text-muted-foreground">
                Inspect the change, then decide from the proposal queue: approve saves a draft revision, reject discards.
              </p>
            </div>
            <Badge variant="outline" className="w-fit font-mono">{diff.files.length} files</Badge>
          </div>
          {diff.files.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {diff.files.map((file) => (
                <Badge key={file.key} variant="outline" className="border font-mono text-[0.68rem]">
                  {file.key}
                </Badge>
              ))}
            </div>
          ) : null}
          <UnifiedMarkdownDiff unifiedDiff={diff.unifiedDiff} />
        </div>
      </DialogContent>
    </Dialog>
  )
}

type UnifiedDiffLine = {
  type: "file" | "hunk" | "added" | "removed" | "context"
  text: string
}

function parseUnifiedDiff(unifiedDiff: string): UnifiedDiffLine[] {
  return unifiedDiff
    .replaceAll("\r\n", "\n")
    .split("\n")
    .filter((line, index, lines) => line.length > 0 || index < lines.length - 1)
    .map((line) => {
      if (line.startsWith("--- ") || line.startsWith("+++ ")) {
        return { type: "file", text: line }
      }
      if (line.startsWith("@@")) {
        return { type: "hunk", text: line }
      }
      if (line.startsWith("+")) {
        return { type: "added", text: line.slice(1) }
      }
      if (line.startsWith("-")) {
        return { type: "removed", text: line.slice(1) }
      }
      if (line.startsWith(" ")) {
        return { type: "context", text: line.slice(1) }
      }
      return { type: "context", text: line }
    })
}

function UnifiedMarkdownDiff({ unifiedDiff }: { unifiedDiff: string }) {
  const lines = useMemo(() => parseUnifiedDiff(unifiedDiff), [unifiedDiff])

  if (lines.length === 0) {
    return (
      <div className="rounded-lg border bg-background/70 p-3 text-sm text-muted-foreground">
        No unified diff returned for this proposal.
      </div>
    )
  }

  return (
    <div className="max-h-[52vh] overflow-auto rounded-lg border bg-background/70 py-2 text-sm">
      {lines.map((line, index) => (
        <UnifiedMarkdownDiffLine key={`${line.type}-${index}-${line.text}`} line={line} />
      ))}
    </div>
  )
}

function UnifiedMarkdownDiffLine({ line }: { line: UnifiedDiffLine }) {
  if (line.type === "file" || line.type === "hunk") {
    return (
      <div className="border-y border-border/50 bg-muted/40 px-3 py-1 font-mono text-xs leading-5 text-muted-foreground first:border-t-0">
        {line.text}
      </div>
    )
  }

  return (
    <div
      className={cn(
        "grid grid-cols-[2rem_minmax(0,1fr)] gap-2 px-3 py-1",
        line.type === "added" && "bg-success/12 text-success",
        line.type === "removed" && "bg-destructive/12 text-destructive",
        line.type === "context" && "text-muted-foreground",
      )}
    >
      <span className="select-none text-right font-mono text-xs leading-6 opacity-70">
        {line.type === "added" ? "+" : line.type === "removed" ? "-" : " "}
      </span>
      <div className="min-w-0 overflow-hidden">
        {line.text.trim().length > 0 ? (
          <MarkdownText content={line.text} compact />
        ) : (
          <span className="block min-h-6 font-mono text-xs leading-6"> </span>
        )}
      </div>
    </div>
  )
}


function ArtifactProposalQueueSection({
  proposals,
  status,
  query,
  busyId,
  decisionError,
  onInspect,
  onApprove,
  onReject,
}: {
  proposals: ArtifactProposalSummary[]
  status: "ready" | "loading" | "error"
  query: string
  busyId: string | null
  decisionError: string
  onInspect: (proposal: ArtifactProposalSummary) => void
  onApprove: (proposal: ArtifactProposalSummary) => void
  onReject: (proposal: ArtifactProposalSummary) => void
}) {
  const visibleProposals = proposals.filter((proposal) => proposalSearchMatches(proposal, query))

  return (
    <section className="overflow-hidden rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-3 py-2.5">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-sm font-semibold">Artifact proposals</h2>
          <Badge variant="outline" className="font-mono" aria-label={`${visibleProposals.length} visible proposals`}>
            {visibleProposals.length}
          </Badge>
          <p className="text-xs text-muted-foreground">pending artifact updates</p>
        </div>
        {status === "loading" ? <span className="text-xs text-muted-foreground">Loading...</span> : null}
      </div>
      {status === "error" ? (
        <p className="border-b border-warning/30 bg-warning/10 px-3 py-2 text-xs text-muted-foreground">
          Live artifact proposals are unavailable. Check Doc Registry connectivity, then refresh.
        </p>
      ) : null}
      {decisionError ? (
        <p role="alert" className="border-b border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-muted-foreground">
          {decisionError}
        </p>
      ) : null}
      {visibleProposals.length === 0 ? (
        <div className="p-6 text-center">
          <h3 className="text-sm font-semibold">No proposals in this view</h3>
          <p className="mx-auto mt-2 max-w-md text-sm text-muted-foreground">
            Artifact-update proposals appear here when feedback or gates draft a reviewed update. Use CLI or IDE agents to submit governed feedback first.
          </p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] table-fixed text-left text-xs md:min-w-full">
            <colgroup>
              <col className="w-[29%]" />
              <col className="w-[14%]" />
              <col className="w-[33%]" />
              <col className="w-[13%]" />
              <col className="w-[11%]" />
            </colgroup>
            <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th scope="col" className="px-3 py-2 font-medium">Proposal</th>
                <th scope="col" className="px-3 py-2 font-medium">Source</th>
                <th scope="col" className="px-3 py-2 font-medium">Diff</th>
                <th scope="col" className="px-3 py-2 font-medium">Updated</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {visibleProposals.map((proposal) => (
                <tr key={proposal.id} className="border-b align-middle transition-colors last:border-b-0 hover:bg-muted/35">
                  <td className="px-3 py-2">
                    <div className="min-w-0">
                      <div className="truncate font-mono font-semibold">{proposal.baseArtifactId}</div>
                      <div className="mt-1 flex min-w-0 items-center gap-2">
                        <Badge variant="secondary" className="shrink-0 font-mono text-[0.68rem]">{proposal.baseVersion}</Badge>
                        <span className="truncate font-mono text-[0.68rem] text-muted-foreground">{proposal.id}</span>
                      </div>
                    </div>
                  </td>
                  <td className="px-3 py-2">
                    <div className="truncate">{proposalSourceLabel(proposal)}</div>
                    <div className="truncate font-mono text-[0.68rem] text-muted-foreground">{proposal.sourceId}</div>
                  </td>
                  <td className="px-3 py-2 leading-5 text-muted-foreground" title={proposal.diffSummary}>
                    <span className="line-clamp-2">{proposal.diffSummary}</span>
                  </td>
                  <td className="truncate px-3 py-2 text-muted-foreground">{formatRelativeTime(proposal.updatedAt)}</td>
                  <td className="px-3 py-2">
                    <div className="flex justify-end gap-1.5">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            className="rounded-md"
                            aria-label="Inspect proposal diff"
                            onClick={() => onInspect(proposal)}
                          >
                            <FileTextIcon />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>Inspect proposal diff</TooltipContent>
                      </Tooltip>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            type="button"
                            size="icon-sm"
                            className="rounded-md bg-success/90 text-background hover:bg-success"
                            aria-label={`Approve proposal ${proposal.id}`}
                            disabled={busyId !== null}
                            onClick={() => onApprove(proposal)}
                          >
                            <CheckIcon />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>Approve: save as a draft revision</TooltipContent>
                      </Tooltip>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            className="rounded-md text-destructive hover:bg-destructive/10"
                            aria-label={`Reject proposal ${proposal.id}`}
                            disabled={busyId !== null}
                            onClick={() => onReject(proposal)}
                          >
                            <XIcon />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>Reject: discard the proposal</TooltipContent>
                      </Tooltip>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function FeedbackSignalsSection({
  signals,
  status,
  query,
}: {
  signals: GovernanceFeedbackSignal[]
  status: "ready" | "loading" | "error"
  query: string
}) {
  const visibleSignals = signals.filter((signal) => feedbackSignalSearchMatches(signal, query))

  return (
    <section className="overflow-hidden rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-3 py-2.5">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-sm font-semibold">Feedback signals</h2>
          <Badge variant="outline" className="font-mono" aria-label={`${visibleSignals.length} visible feedback signals`}>
            {visibleSignals.length}
          </Badge>
          <p className="text-xs text-muted-foreground">received from agents and integrations</p>
        </div>
        {status === "loading" ? <span className="text-xs text-muted-foreground">Loading...</span> : null}
      </div>
      {status === "error" ? (
        <p className="border-b border-warning/30 bg-warning/10 px-3 py-2 text-xs text-muted-foreground">
          Live feedback signals are unavailable. Check Doc Registry connectivity, then refresh.
        </p>
      ) : null}
      {visibleSignals.length === 0 ? (
        <div className="p-6 text-center">
          <h3 className="text-sm font-semibold">No feedback signals in this view</h3>
          <p className="mx-auto mt-2 max-w-md text-sm text-muted-foreground">
            Feedback appears here when CLI, IDE agents, or delivery integrations report review signals. Use the CLI or IDE agent to submit a delivery signal.
          </p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[820px] table-fixed text-left text-xs md:min-w-full">
            <colgroup>
              <col className="w-[28%]" />
              <col className="w-[34%]" />
              <col className="w-[18%]" />
              <col className="w-[12%]" />
              <col className="w-[8%]" />
            </colgroup>
            <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th scope="col" className="px-3 py-2 font-medium">Signal</th>
                <th scope="col" className="px-3 py-2 font-medium">Reason</th>
                <th scope="col" className="px-3 py-2 font-medium">Links</th>
                <th scope="col" className="px-3 py-2 font-medium">Received</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Open</th>
              </tr>
            </thead>
            <tbody>
              {visibleSignals.map((signal) => (
                <tr key={signal.id} className="border-b align-middle transition-colors last:border-b-0 hover:bg-muted/35">
                  <td className="px-3 py-2">
                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                      <Badge variant="outline" className={cn("shrink-0 border text-[0.68rem]", toneClass(statusTone("feedbackSignal", signal.status)))}>
                        {readableKey(signal.status)}
                      </Badge>
                      <span className="truncate font-mono font-semibold">{signal.eventType}</span>
                    </div>
                    <div className="mt-1 flex min-w-0 items-center gap-2">
                      <span className="truncate text-muted-foreground">{signal.sourceLabel}</span>
                      <span className="truncate font-mono text-[0.68rem] text-muted-foreground">{signal.id}</span>
                    </div>
                  </td>
                  <td className="px-3 py-2 leading-5 text-muted-foreground" title={signal.reason}>
                    <span className="line-clamp-2">{signal.reason}</span>
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex min-w-0 flex-wrap gap-1.5">
                      {signal.changeRequestId ? (
                        <Badge variant="secondary" className="max-w-full truncate font-mono text-[0.68rem]">
                          {signal.changeRequestId}
                        </Badge>
                      ) : null}
                      {signal.artifactId ? (
                        <Badge variant="outline" className="max-w-full truncate border font-mono text-[0.68rem]">
                          {signal.artifactId}
                        </Badge>
                      ) : null}
                      {!signal.changeRequestId && !signal.artifactId && signal.featureId ? (
                        <Badge variant="outline" className="max-w-full truncate border font-mono text-[0.68rem]">
                          {signal.featureId}
                        </Badge>
                      ) : null}
                    </div>
                  </td>
                  <td className="truncate px-3 py-2 text-muted-foreground" title={formatDateTime(signal.createdAt)}>
                    {formatRelativeTime(signal.createdAt)}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex justify-end gap-1.5">
                      {signal.changeRequestId ? (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button variant="outline" size="icon-sm" className="rounded-md" asChild>
                              <Link to={`/work/${encodeURIComponent(signal.changeRequestId)}`} aria-label="Open feedback work item">
                                <FileTextIcon />
                              </Link>
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>Open work item</TooltipContent>
                        </Tooltip>
                      ) : null}
                      {signal.artifactId ? (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button variant="outline" size="icon-sm" className="rounded-md" asChild>
                              <Link to={`/artifacts?artifact=${encodeURIComponent(signal.artifactId)}`} aria-label="Open feedback artifact">
                                <ExternalLinkIcon />
                              </Link>
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>Open artifact</TooltipContent>
                        </Tooltip>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function reviewTargetPath(item: WorkItem) {
  return `/work/${item.key}?tab=verification`
}

export function ReviewsPage({ workboard, reviewer }: { workboard: WorkboardView; reviewer: string }) {
  const navigate = useNavigate()
  const [activeFilter, setActiveFilter] = useState<ReviewFilter>("all")
  const [reviewSearch, setReviewSearch] = useState("")
  const [selectedProposal, setSelectedProposal] = useState<ArtifactProposalSummary | undefined>(undefined)
  const [decisionBusyId, setDecisionBusyId] = useState<string | null>(null)
  const [decisionError, setDecisionError] = useState("")
  const [pendingReject, setPendingReject] = useState<ArtifactProposalSummary | undefined>(undefined)
  const artifactProposals = useArtifactProposals()
  const feedbackInbox = useGovernanceFeedbackSignals()

  async function decideProposal(proposal: ArtifactProposalSummary, decision: "approve" | "reject") {
    setDecisionBusyId(proposal.id)
    setDecisionError("")
    try {
      if (decision === "approve") {
        await approveArtifactProposal(proposal.id, reviewer)
      } else {
        await rejectArtifactProposal(proposal.id)
      }
      artifactProposals.refresh()
    } catch {
      setDecisionError(
        decision === "approve"
          ? `Could not approve proposal ${proposal.id}. Check Doc Registry connectivity, then retry.`
          : `Could not reject proposal ${proposal.id}. Check Doc Registry connectivity, then retry.`,
      )
    } finally {
      setDecisionBusyId(null)
    }
  }
  const workItems = workboard.workItems
  const itemsNeedingReview = reviewItems(workItems)
  const visibleItems = itemsNeedingReview.filter(
    (item) => reviewFilterMatches(item, activeFilter) && reviewSearchMatches(item, reviewSearch),
  )
  const reviewCount = itemsNeedingReview.length
  const filterOptions: Array<{ id: ReviewFilter; label: string }> = [
    { id: "all", label: "All" },
    { id: "needs_changes", label: "Needs changes" },
    { id: "ready", label: "Ready" },
    { id: "gate_failed", label: "Gate failed" },
  ]
  const reviewPrompts = [
    {
      label: "Ask about review gaps",
      prompt: "Identify review gaps that should block delivery, including missing evidence, failed gates, or unresolved acceptance criteria.",
    },
    {
      label: "Ask review summary",
      prompt: `Summarize the ${reviewCount} visible review items by urgency, delivery verdict, and next human action.`,
    },
  ]

  return (
    <div className="grid gap-4">
      <section className="grid gap-3.5">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">{reviewCount} items need review</h2>
            <p className="mt-1 text-xs text-muted-foreground">Triage delivery verdicts, failed gates, and evidence gaps.</p>
          </div>
        </div>
        {workboard.status === "error" ? (
          <p className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-muted-foreground">
            Live review data is unavailable. Check Doc Registry connectivity, then refresh; no fallback review rows are shown in live mode.
          </p>
        ) : null}
        <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
          <div className="grid gap-2">
            <label htmlFor="review-search" className="text-xs font-medium text-muted-foreground">
              Search reviews
            </label>
            <div className="relative">
              <SearchIcon className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                id="review-search"
                aria-label="Search reviews"
                value={reviewSearch}
                onChange={(event) => setReviewSearch(event.target.value)}
                placeholder="Search by work item, owner, reason, or Context Pack"
                className="pl-8"
              />
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            {reviewPrompts.map((item) => (
              <ActionTooltip key={item.label} content="Opens governance agent. Does not change review state.">
                <Button
                  variant="outline"
                  size="sm"
                  className="rounded-md"
                  onClick={() => runGovernanceAgentPrompt(item.prompt)}
                >
                  <BotIcon data-icon="inline-start" />
                  {item.label}
                </Button>
              </ActionTooltip>
            ))}
          </div>
        </div>
        <div className="flex flex-wrap gap-2 border-y py-3">
          {filterOptions.map((filter) => {
            const selected = activeFilter === filter.id
            return (
              <Button
                key={filter.id}
                type="button"
                variant={selected ? "secondary" : "outline"}
                size="sm"
                className={cn("rounded-md", selected ? "bg-primary/14 ring-1 ring-primary/25" : "")}
                onClick={() => setActiveFilter(filter.id)}
              >
                {filter.label}
                <Badge variant={selected ? "secondary" : "outline"} className="ml-1 font-mono">
                  {reviewFilterCount(workItems, filter.id)}
                </Badge>
              </Button>
            )
          })}
        </div>
      </section>

      <section className="overflow-hidden rounded-lg border bg-card">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-3 py-2.5">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-sm font-semibold">Review queue</h2>
            <Badge variant="outline" className="font-mono" aria-label={`${visibleItems.length} visible items`}>
              {visibleItems.length}
            </Badge>
            <p className="text-xs text-muted-foreground">visible items</p>
          </div>
          {activeFilter !== "all" || reviewSearch ? (
            <Button
              variant="ghost"
              size="sm"
              className="rounded-md"
              onClick={() => {
                setActiveFilter("all")
                setReviewSearch("")
              }}
            >
              Clear filters
            </Button>
          ) : null}
        </div>
        {visibleItems.length === 0 ? (
          <div className="p-6 text-center">
            <h3 className="text-sm font-semibold">No reviews in this view</h3>
            <p className="mx-auto mt-2 max-w-md text-sm text-muted-foreground">
              Change the filter or open Work to inspect the full queue.
            </p>
            <Button variant="outline" size="sm" className="mt-4 rounded-md" asChild>
              <Link to="/work">Open Work</Link>
            </Button>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[720px] table-fixed text-left text-xs md:min-w-full">
              <colgroup>
                <col className="w-[34%]" />
                <col className="w-[30%]" />
                <col className="w-[13%]" />
                <col className="w-[13%]" />
                <col className="w-[10%]" />
              </colgroup>
              <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  <th scope="col" className="px-3 py-2 font-medium">Item</th>
                  <th scope="col" className="px-3 py-2 font-medium">Reason</th>
                  <th scope="col" className="px-3 py-2 font-medium">Owner</th>
                  <th scope="col" className="px-3 py-2 font-medium">Updated</th>
                  <th scope="col" className="px-3 py-2 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {visibleItems.map((item) => (
                  <tr
                    key={item.key}
                    className="cursor-pointer border-b align-middle transition-colors last:border-b-0 hover:bg-muted/35"
                    onClick={() => navigate(reviewTargetPath(item))}
                  >
                    <td className="px-3 py-2">
                      <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <Link
                          to={reviewTargetPath(item)}
                          className="whitespace-nowrap font-mono font-semibold text-foreground hover:underline"
                          onClick={(event) => event.stopPropagation()}
                        >
                          {item.key}
                        </Link>
                        <Badge variant="outline" className={cn("shrink-0 border text-[0.68rem]", toneClass(reviewTone(item)))}>
                          {reviewLabel(item)}
                        </Badge>
                        <Badge variant="secondary" className="shrink-0 text-[0.68rem]">{item.route}</Badge>
                        {item.contextPackId ? (
                          <Badge
                            variant="outline"
                            className={cn("shrink-0 border text-[0.68rem]", toneClass("success"))}
                            title="Context Pack available"
                          >
                            Pack
                          </Badge>
                        ) : (
                          <Badge
                            variant="outline"
                            className={cn("shrink-0 border text-[0.68rem]", toneClass("warning"))}
                            title="Context Pack missing"
                          >
                            No pack
                          </Badge>
                        )}
                      </div>
                      <div className="mt-1 flex min-w-0 items-center gap-2">
                        <span className="block min-w-0 max-w-[460px] truncate text-left text-muted-foreground">
                          {item.title}
                        </span>
                      </div>
                    </td>
                    <td className="px-3 py-2 leading-5 text-muted-foreground" title={reviewReason(item)}>
                      <span className="line-clamp-2">{reviewReason(item)}</span>
                    </td>
                    <td className="truncate px-3 py-2">{item.owner}</td>
                    <td className="truncate px-3 py-2 text-muted-foreground" title={formatDateTime(item.updated)}>
                      {formatRelativeTime(item.updated)}
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex justify-end gap-1.5">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button size="icon-sm" className="rounded-md bg-foreground text-background hover:bg-foreground/85" asChild>
                              <Link
                                to={reviewTargetPath(item)}
                                aria-label={reviewActionLabel(item)}
                                onClick={(event) => event.stopPropagation()}
                              >
                                <EyeIcon />
                              </Link>
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>{reviewActionLabel(item)}</TooltipContent>
                        </Tooltip>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <ArtifactProposalQueueSection
        proposals={artifactProposals.items}
        status={artifactProposals.status}
        query={reviewSearch}
        busyId={decisionBusyId}
        decisionError={decisionError}
        onInspect={setSelectedProposal}
        onApprove={(proposal) => void decideProposal(proposal, "approve")}
        onReject={setPendingReject}
      />
      <Dialog open={pendingReject !== undefined} onOpenChange={(open) => { if (!open) setPendingReject(undefined) }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Reject proposal</DialogTitle>
            <DialogDescription>
              Discard proposal{" "}
              <span className="font-mono">{pendingReject?.id}</span> for{" "}
              <span className="font-mono">{pendingReject?.baseArtifactId}</span>? The proposed changes are deleted and
              cannot be recovered.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" className="rounded-md" onClick={() => setPendingReject(undefined)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              className="rounded-md"
              disabled={decisionBusyId !== null}
              onClick={() => {
                if (pendingReject) void decideProposal(pendingReject, "reject")
                setPendingReject(undefined)
              }}
            >
              Reject proposal
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <FeedbackSignalsSection
        signals={feedbackInbox.items}
        status={feedbackInbox.status}
        query={reviewSearch}
      />
      <ArtifactProposalDiffDialog
        proposal={selectedProposal}
        open={selectedProposal !== undefined}
        onOpenChange={(open) => {
          if (!open) setSelectedProposal(undefined)
        }}
      />
    </div>
  )
}
