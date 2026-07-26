// Repository observation and delivery-review controls.

import { AlertTriangleIcon, CheckCircle2Icon, ExternalLinkIcon, ShieldCheckIcon } from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { summarizeDeliveryTrust, trustTierLabel } from "@/data/delivery-trust"
import { recordDeliveryDecision, repositoryObservation, type DeliveryLinkSummary, type DeliveryStatusSummary, type WorkItemDetailData } from "@/data/workboard"
import { type WorkItem } from "@/data/workspace"
import { formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { deliveryText, stateText, statusTone, toneClass, type Tone } from "../shared"
import { MarkdownText } from "../shared-ui"

export function RepositoryObservationSummary({ detail }: { detail: WorkItemDetailData }) {
  if (detail.readback.deliveryLinks === "error") {
    return (
      <section className="rounded-lg border bg-background/70 p-4">
        <h3 className="text-sm font-semibold">Repository observation</h3>
        <p className="mt-2 text-sm text-muted-foreground">Repository links unavailable. Check Doc Registry connectivity.</p>
      </section>
    )
  }
  if (detail.deliveryLinks.length === 0) return null

  const latestCompletionHead = detail.deliveryStatus?.gitReceipt?.headRevision
  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <h3 className="text-sm font-semibold">Repository observation</h3>
      <div className="mt-3 grid gap-2">
        {detail.deliveryLinks.map((link) => (
          <RepositoryObservationLink key={`${link.externalKey}-${link.url}`} link={link} latestCompletionHead={latestCompletionHead} />
        ))}
      </div>
    </section>
  )
}

function RepositoryObservationLink({ link, latestCompletionHead }: { link: DeliveryLinkSummary; latestCompletionHead?: string }) {
  const observation = repositoryObservation(link, latestCompletionHead)
  const copy = observation === "open"
    ? "PR/MR linked; merge it before repository observation can corroborate delivery."
    : observation === "exact"
      ? "Submitted commit observed on merged PR/MR."
      : observation === "stale"
        ? "PR/MR merged, but it does not match the latest submitted commit. Update or resubmit, then merge the matching head."
        : observation === "missing-receipt"
          ? "PR/MR merged; submit a delivery receipt so SpecGate can compare the exact head."
          : "PR/MR closed without merge; open or link a replacement."
  const tone: Tone = observation === "exact" ? "success" : observation === "open" || observation === "missing-receipt" ? "warning" : "danger"

  return (
    <div className="rounded-md border bg-card/70 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <a href={link.url} target="_blank" rel="noreferrer" className="inline-flex min-w-0 items-center gap-2 text-sm font-medium hover:underline">
          <ExternalLinkIcon className="size-4 shrink-0" />
          <span className="truncate">{link.title}</span>
        </a>
        <Badge variant="outline" className={cn("border", toneClass(tone))}>{stateText(link.state)}</Badge>
      </div>
      <p className="mt-2 text-sm leading-6 text-muted-foreground">{copy}</p>
      <p className="mt-2 break-all font-mono text-[11px] text-muted-foreground">{link.url}</p>
    </div>
  )
}

export function DeliverySummary({
  item,
  detail,
  reviewer,
  workspaceId,
  onDecided,
}: {
  item: WorkItem
  detail: WorkItemDetailData
  reviewer: string
  workspaceId: string
  onDecided: () => void
}) {
  const [pendingDecision, setPendingDecision] = useState<"approve" | "reject">()
  const [decisionNote, setDecisionNote] = useState("")
  const [decisionBusy, setDecisionBusy] = useState(false)
  const [decisionError, setDecisionError] = useState("")
  const deliveryRun = detail.gateRuns.find((run) => run.gate === "delivery_review")
  const deliveryStatus = detail.deliveryStatus
  const evidenceState = deliveryStatus?.evidenceVerdict ??
    (deliveryStatus?.executor === "human" ? undefined : deliveryStatus?.verdict) ??
    deliveryRun?.state ??
    item.delivery
  const needsReview = deliveryStatus?.reasonCode === "policy_unavailable" ||
    deliveryStatus?.reasonCode === "delivery_review_outdated" ||
    evidenceState === "fail" ||
    evidenceState === "needs_human_review" ||
    evidenceState === "needs_changes" ||
    (!deliveryStatus?.found && item.delivery === "needs_changes")
  const trust = deliveryStatus?.found ? summarizeDeliveryTrust(deliveryStatus) : undefined
  const accepted = trust?.decision === "Accepted"
  const rejected = trust?.decision === "Rejected"
  const decisionGateRunId = deliveryStatus?.gateRunId
  const decisionCompletionId = deliveryStatus?.completionFeedbackEventId
  const canDecide = deliveryStatus?.found &&
    Boolean(decisionGateRunId) &&
    Boolean(decisionCompletionId) &&
    deliveryStatus.executor !== "human" &&
    deliveryStatus.reasonCode !== "delivery_review_outdated"
  const completion = decisionCompletionId
    ? deliveryStatus?.gitReceipt?.headRevision
      ? `Completion ${decisionCompletionId} at commit ${deliveryStatus.gitReceipt.headRevision}`
      : deliveryStatus?.reviewedAt
        ? `Completion ${decisionCompletionId}, reviewed ${formatDateTime(deliveryStatus.reviewedAt)}`
        : `Completion ${decisionCompletionId}`
    : "Latest reviewed completion"

  async function confirmDecision() {
    if (!pendingDecision || !decisionGateRunId || !decisionCompletionId) return
    const note = decisionNote.trim()
    if (pendingDecision === "reject" && !note) {
      setDecisionError("Describe what must change before resubmission.")
      return
    }
    setDecisionBusy(true)
    setDecisionError("")
    try {
      await recordDeliveryDecision(
        item.registryId || item.key,
        pendingDecision,
        reviewer,
        workspaceId,
        decisionGateRunId,
        decisionCompletionId,
        note || undefined,
      )
      setPendingDecision(undefined)
      setDecisionNote("")
      onDecided()
    } catch (error) {
      setDecisionError(error instanceof Error ? error.message : "Delivery decision failed.")
    } finally {
      setDecisionBusy(false)
    }
  }

  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-2">
          {accepted ? (
            <CheckCircle2Icon aria-label="Delivery accepted" className="size-4 text-success" />
          ) : rejected ? (
            <AlertTriangleIcon aria-label="Delivery rejected" className="size-4 text-warning" />
          ) : needsReview ? (
            <AlertTriangleIcon aria-label="Delivery evidence has gaps" className="size-4 text-warning" />
          ) : (
            <ShieldCheckIcon aria-label="Delivery evidence available for review" className="size-4 text-primary" />
          )}
          <h3 className="text-sm font-semibold">Delivery review</h3>
        </div>
        {canDecide ? (
          <div className="flex flex-wrap gap-2">
            <Button size="sm" className="rounded-md" onClick={() => setPendingDecision("approve")}>
              Accept delivery
            </Button>
            <Button variant="outline" size="sm" className="rounded-md text-destructive" onClick={() => setPendingDecision("reject")}>
              Request changes
            </Button>
          </div>
        ) : (
          <Badge variant="outline" className="rounded-full">read-only</Badge>
        )}
      </div>
      {detail.readback.delivery === "error" ? (
        <p className="mt-3 rounded-md border bg-card/70 p-3 text-sm leading-6 text-muted-foreground">
          Delivery review readback unavailable. Check Doc Registry connectivity; no fallback delivery review detail is shown in live mode.
        </p>
      ) : (
        <>
          {!deliveryStatus && (
            <p className="mt-3 text-sm text-muted-foreground">
              Latest gate state: <span className="font-medium text-foreground">
                {deliveryRun ? stateText(deliveryRun.state) : deliveryText(item.delivery)}
              </span>
            </p>
          )}
          {deliveryStatus?.hint ? (
            <p className="mt-3 text-sm leading-6 text-muted-foreground">{deliveryStatus.hint}</p>
          ) : deliveryRun?.hint ? (
            <p className="mt-3 text-sm leading-6 text-muted-foreground">{deliveryRun.hint}</p>
          ) : null}
          {deliveryStatus ? <DeliveryStatusDetails status={deliveryStatus} /> : null}
        </>
      )}
      <Dialog
        open={pendingDecision !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setPendingDecision(undefined)
            setDecisionNote("")
            setDecisionError("")
          }
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{pendingDecision === "approve" ? "Accept delivery" : "Request changes"}</DialogTitle>
            <DialogDescription>
              {item.key} · {item.title} as {reviewer}. {completion}. Doc Registry validates latest-review binding and reviewer identity.
            </DialogDescription>
          </DialogHeader>
          <label className="grid gap-1.5">
            <span className="text-xs font-medium text-muted-foreground">
              {pendingDecision === "reject" ? "Required changes" : "Note (optional)"}
            </span>
            <textarea
              className="min-h-24 rounded-md border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
              value={decisionNote}
              onChange={(event) => setDecisionNote(event.target.value)}
              placeholder={pendingDecision === "reject" ? "Describe specific evidence or behavior that must change" : "Why this delivery is acceptable"}
            />
          </label>
          {decisionError ? <p role="alert" className="text-sm text-destructive">{decisionError}</p> : null}
          <DialogFooter>
            <Button
              variant="outline"
              className="rounded-md"
              disabled={decisionBusy}
              onClick={() => {
                setPendingDecision(undefined)
                setDecisionNote("")
                setDecisionError("")
              }}
            >
              Cancel
            </Button>
            <Button
              variant={pendingDecision === "approve" ? "default" : "destructive"}
              className="rounded-md"
              disabled={decisionBusy}
              onClick={() => void confirmDecision()}
            >
              {decisionBusy ? "Recording…" : pendingDecision === "approve" ? "Accept delivery" : "Request changes"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}

function DeliveryStatusDetails({ status }: { status: DeliveryStatusSummary }) {
  if (!status.found) {
    return (
      <p className="mt-3 rounded-md border bg-card/70 p-3 text-sm leading-6 text-muted-foreground">
        No persisted delivery review result is available yet. Triggering review remains a CLI or agents-service workflow.
      </p>
    )
  }

  const trust = summarizeDeliveryTrust(status)

  return (
    <div className="mt-3 grid gap-3">
      <div className="grid gap-2 sm:grid-cols-2">
        <DeliveryTrustFact label="Evidence" value={trust.evidence} />
        <DeliveryTrustFact label="Assurance" value={trust.assurance} />
        <DeliveryTrustFact label="Decision" value={trust.decision} />
        <DeliveryTrustFact label="Receipt" value={trust.receipt} />
      </div>
      <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
        <Badge variant="secondary" className="rounded-full">{trust.reviewer}</Badge>
        {trust.decisionActor ? <Badge variant="secondary" className="rounded-full">{trust.decisionActor}</Badge> : null}
        {trust.peerReview ? <Badge variant="secondary" className="rounded-full">{trust.peerReview}</Badge> : null}
        {status.reviewedAt ? <Badge variant="secondary" className="rounded-full">{formatDateTime(status.reviewedAt)}</Badge> : null}
        {typeof status.confidence === "number" ? (
          <Badge variant="secondary" className="rounded-full">{Math.round(status.confidence * 100)}% reviewer confidence</Badge>
        ) : null}
      </div>
      {status.executor?.trim() === "human" && status.verdict?.trim() !== "pass" && status.note ? (
        <p className="rounded-md border border-warning/40 bg-warning/10 p-3 text-sm leading-6">
          <span className="font-semibold">Requested changes:</span> {status.note}
        </p>
      ) : null}
      {trust.modelReviewed ? (
        <p className="rounded-md border bg-card/70 p-3 text-xs leading-5 text-muted-foreground">
          A model review evaluates submitted evidence; it does not verify the code, replace CI, or make the human acceptance decision.
        </p>
      ) : null}
      {status.outstandingMd ? (
        <div className="max-h-64 overflow-y-auto rounded-md border bg-card/70 p-3">
          <h4 className="mb-2 text-xs font-semibold text-muted-foreground">Outstanding review feedback</h4>
          <MarkdownText content={status.outstandingMd} />
        </div>
      ) : null}
      {status.criteria.length > 0 ? (
        <div>
          <h4 className="text-xs font-semibold text-muted-foreground">Criteria verdicts</h4>
          <div className="mt-2 grid gap-2">
            {status.criteria.map((criterion) => (
              <div key={criterion.id} className="rounded-md border bg-card/70 p-3">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div className="min-w-0">
                    <p className="font-mono text-[11px] text-muted-foreground">{criterion.id}</p>
                    <p className="mt-1 text-sm leading-5">{criterion.text}</p>
                  </div>
                  <Badge variant="outline" className={cn("border", toneClass(statusTone("state",criterion.verdict)))}>
                    {stateText(criterion.verdict)}
                  </Badge>
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-2">
                  <Badge variant="secondary" className="rounded-full">{trustTierLabel(criterion.trustTier)}</Badge>
                  {criterion.verificationBinding ? (
                    <span className="break-all font-mono text-[11px] text-muted-foreground">{criterion.verificationBinding}</span>
                  ) : null}
                </div>
                {criterion.why ? <p className="mt-2 text-xs leading-5 text-muted-foreground">{criterion.why}</p> : null}
              </div>
            ))}
          </div>
        </div>
      ) : null}
      {status.checks.length > 0 ? (
        <div>
          <h4 className="text-xs font-semibold text-muted-foreground">Automated checks</h4>
          <div className="mt-2 grid gap-2">
            {status.checks.map((check) => (
              <div key={`${check.name}-${check.status}-${check.detail ?? ""}`} className="grid gap-2 rounded-md border bg-card/70 p-3 text-sm sm:grid-cols-[minmax(0,1fr)_auto]">
                <div className="min-w-0">
                  <p className="truncate font-medium">{check.name}</p>
                  {check.detail ? <p className="mt-1 text-xs leading-5 text-muted-foreground">{check.detail}</p> : null}
                </div>
                <Badge variant="outline" className={cn("w-fit border", toneClass(statusTone("state",check.status)))}>
                  {stateText(check.status)}
                </Badge>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}

function DeliveryTrustFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border bg-card/70 p-3">
      <p className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-1 text-sm font-medium leading-5">{value}</p>
    </div>
  )
}
