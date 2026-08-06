// Work-item overview, context, and gate sections.

import { CheckCircle2Icon, ChevronRightIcon, CircleDotIcon, ClockIcon, ExternalLinkIcon } from "lucide-react"
import { useState } from "react"
import { Link } from "react-router"
import { Badge } from "@/components/ui/badge"
import { type AcceptanceCriterionSummary, type DeliveryStatusSummary, type GateRunSummary, type NextActionSummary, type StaleWarningSummary, type TrackerLinkSummary, type WorkItemDetailData } from "@/data/workboard"
import { type WorkItem } from "@/data/workspace"
import { formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import {
  routeText, gateChecks, gateText, readableKey, stateText, statusTone, toneClass, type Tone } from "../shared"
import { GateEvidenceWhy } from "../shared-ui"

export function FreshnessSignalsSummary({ warnings }: { warnings: StaleWarningSummary[] }) {
  if (warnings.length === 0) return null

  return (
    <section className="sg-card p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">Freshness signals</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Registry attention context for stale handoffs, tracker contradictions, and external delivery signals.
          </p>
        </div>
        <Badge variant="outline" className="rounded-full">
          read-only
        </Badge>
      </div>
      <div className="mt-3 grid gap-2">
        {warnings.map((warning) => (
          <div key={warning.id} className="sg-card p-3">
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="min-w-0">
                <p className="text-sm font-medium">{readableKey(warning.code)}</p>
                <p className="mt-1 text-sm leading-6 text-muted-foreground">{warning.message}</p>
              </div>
              <Badge variant="outline" className={cn("shrink-0 border", toneClass(statusTone("severity", warning.severity)))}>
                {readableKey(warning.severity)}
              </Badge>
            </div>
            <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted-foreground">
              {warning.changeRequestId ? <Badge variant="secondary" className="font-mono">{warning.changeRequestId}</Badge> : null}
              {warning.featureId ? <Badge variant="secondary" className="font-mono">{warning.featureId}</Badge> : null}
              {warning.artifactId ? (
                <Badge variant="outline" className="font-mono" asChild>
                  <Link to={`/artifacts?artifact=${encodeURIComponent(warning.artifactId)}`}>{warning.artifactId}</Link>
                </Badge>
              ) : null}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

export function FeatureOverview({ detail }: { detail: WorkItemDetailData }) {
  const feature = detail.feature

  if (detail.readback.feature === "error") {
    return (
      <section className="sg-card p-4">
        <h3 className="text-sm font-semibold">Feature context</h3>
        <p className="mt-2 text-sm text-muted-foreground">Feature context unavailable. Check Doc Registry connectivity; no fallback feature summary is shown in live mode.</p>
      </section>
    )
  }

  if (!feature) return null

  return (
    <section className="sg-card p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold">Feature context</h3>
            <Badge variant="outline" className="h-6 rounded-full font-mono text-[0.7rem]">
              {feature.key}
            </Badge>
            <Badge variant="secondary" className="h-6 rounded-full text-[0.7rem]">
              {stateText(feature.status)}
            </Badge>
          </div>
          <p className="mt-1 truncate text-sm text-muted-foreground">{feature.name}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          {typeof feature.version === "number" ? <span>v{feature.version}</span> : null}
          {feature.canonicalArtifactId ? (
            <Link
              to={`/artifacts?artifact=${encodeURIComponent(feature.canonicalArtifactId)}`}
              className="text-foreground underline-offset-4 hover:underline"
            >
              canonical artifact
            </Link>
          ) : null}
        </div>
      </div>
    </section>
  )
}

// Delivery results bind to the immutable criterion ID. Text is display content
// and may change or repeat, so it must never establish identity.
function acceptanceCriterionVerdict(
  criterion: AcceptanceCriterionSummary,
  deliveryStatus: DeliveryStatusSummary | undefined,
): string | undefined {
  const criteria = deliveryStatus?.criteria ?? []
  if (criteria.length === 0) return undefined
  return criteria.find((row) => row.id === criterion.id)?.verdict
}

export function acceptanceCriterionDone(criterion: AcceptanceCriterionSummary, deliveryStatus: DeliveryStatusSummary | undefined) {
  const verdict = acceptanceCriterionVerdict(criterion, deliveryStatus)
  return verdict ? verdict === "met" : criterion.done
}

// Mirrors the CLI's `Enforcement:` line so a reviewer reads the same sentence
// wherever they accept from. Absence of a badge must not be what tells someone a
// criterion is unenforced: the shortfall is stated.
function criteriaEnforcementText(criteria: AcceptanceCriterionSummary[]) {
  if (criteria.length === 0) return undefined
  const bound = criteria.filter((criterion) => Boolean(criterion.verificationBinding)).length
  if (bound === criteria.length) return `All ${criteria.length} criteria are bound to a check.`
  if (bound === 0) {
    return `None of the ${criteria.length} criteria are bound to a check, so review reports the agent's claims.`
  }
  return `${bound} of ${criteria.length} criteria are bound to a check; the rest are reviewed as the agent's claim.`
}

export function AcceptanceCriteriaSummary({ detail }: { detail: WorkItemDetailData }) {
  const enforcement = criteriaEnforcementText(detail.acceptanceCriteria)
  return (
    <section className="sg-card p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-sm font-semibold">Acceptance criteria</h3>
        <Badge variant="outline" className="font-mono">
          {detail.acceptanceCriteria.filter((criterion) => acceptanceCriterionDone(criterion, detail.deliveryStatus)).length}/{detail.acceptanceCriteria.length}
        </Badge>
      </div>
      <div className="mt-3 grid gap-2">
        {detail.readback.acceptance === "error" ? (
          <p className="sg-inset p-3 text-sm leading-5 text-muted-foreground">
            Acceptance criteria unavailable. Check Doc Registry connectivity; no fallback acceptance criteria are shown in live mode.
          </p>
        ) : detail.acceptanceCriteria.length === 0 ? (
          <p className="sg-inset p-3 text-sm leading-5 text-muted-foreground">
            No acceptance criteria are recorded for this work item yet. Shape or update the governed artifact from the CLI or IDE workflow, then refresh the registry view.
          </p>
        ) : (
          detail.acceptanceCriteria.map((criterion) => {
            const verdict = acceptanceCriterionVerdict(criterion, detail.deliveryStatus)
            const done = verdict ? verdict === "met" : criterion.done
            return (
              <div
                key={criterion.id}
                // Wraps rather than squeezing: with a verdict, a check name, and a
                // source badge, a non-wrapping row leaves the criterion text a few
                // characters wide on a phone.
                className="flex flex-wrap items-start justify-between gap-x-3 gap-y-2 sg-inset p-3"
              >
                <div className="flex min-w-[12rem] flex-1 items-start gap-2 text-sm">
                  {done ? (
                    <CheckCircle2Icon className="mt-0.5 size-4 shrink-0 text-success" />
                  ) : (
                    <CircleDotIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  )}
                  <span className="leading-5">{criterion.text}</span>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  {verdict ? (
                    <Badge variant="outline" className={cn("border text-[11px]", toneClass(statusTone("state", verdict)))}>
                      {stateText(verdict)}
                    </Badge>
                  ) : null}
                  {criterion.verificationBinding ? (
                    <Badge variant="outline" className="font-mono text-[11px]">
                      check: {criterion.verificationBinding}
                    </Badge>
                  ) : null}
                  {/* A human-approved criterion is the expected case and needs no
                      badge. The one worth surfacing is a criterion no human is
                      recorded as having written. */}
                  {criterion.source === "human" ? null : (
                    <Badge variant="outline" className="text-[11px]">
                      agent-drafted
                    </Badge>
                  )}
                </div>
              </div>
            )
          })
        )}
      </div>
      {enforcement && detail.readback.acceptance !== "error" ? (
        <p className="mt-3 text-xs leading-5 text-muted-foreground">{enforcement}</p>
      ) : null}
    </section>
  )
}

export function ContextSummary({
  item,
  detail,
}: {
  item: WorkItem
  detail: WorkItemDetailData
}) {
  return (
    <section className="grid gap-3">
      <div className="sg-card p-4">
        <h3 className="text-sm font-semibold">Work context</h3>
        <div className="mt-3 grid gap-3 text-sm sm:grid-cols-2">
          <div>
            <span className="text-xs text-muted-foreground">Built from</span>
            <p className="mt-1">{routeText(item.route)}</p>
          </div>
          <div>
            <span className="text-xs text-muted-foreground">Waiting on</span>
            <p className="mt-1">{item.blocker}</p>
          </div>
        </div>
      </div>
      <TrackerLinks trackerLinks={detail.trackerLinks} status={detail.readback.trackerLinks} />
    </section>
  )
}

function TrackerLinks({
  trackerLinks,
  status,
}: {
  trackerLinks: TrackerLinkSummary[]
  status: "ready" | "loading" | "error"
}) {
  return (
    <section className="sg-card p-4">
      <h3 className="text-sm font-semibold">Linked issues</h3>
      <div className="mt-3 grid gap-2">
        {status === "error" ? (
          <p className="text-sm text-muted-foreground">Linked issues unavailable. Check Doc Registry connectivity; no fallback tracker links are shown in live mode.</p>
        ) : trackerLinks.length === 0 ? (
          <p className="text-sm text-muted-foreground">No tracker links recorded.</p>
        ) : (
          trackerLinks.map((link) => (
            <a
              key={`${link.identifier}-${link.url}`}
              href={link.url}
              className="flex flex-wrap items-center justify-between gap-3 sg-inset p-3 text-sm transition-colors hover:bg-accent"
              target="_blank"
              rel="noreferrer"
            >
              <span className="flex min-w-0 items-center gap-2">
                <ExternalLinkIcon className="size-4 shrink-0 text-muted-foreground" />
                <span className="font-mono text-xs">{link.identifier}</span>
              </span>
              <span className="flex items-center gap-2">
                {link.trackerState ? <span className="text-xs text-muted-foreground">{link.trackerState}</span> : null}
              <Badge variant="outline" className={cn("border", toneClass(link.state === "opened" ? "success" : "neutral"))}>
                  {stateText(link.state)}
                </Badge>
              </span>
            </a>
          ))
        )}
      </div>
    </section>
  )
}

// Worst-state ordering for the collapsed gate summary line and its tone.
const gateStateOrder = ["fail", "needs_human_review", "warn", "pending", "pass", "not_applicable"]
const toneRank: Record<Tone, number> = { success: 0, neutral: 1, warning: 2, danger: 3 }

function gateStateSummary(nextActions: NextActionSummary[]): { label: string; tone: Tone } {
  const counts = new Map<string, number>()
  for (const action of nextActions) {
    counts.set(action.state, (counts.get(action.state) ?? 0) + 1)
  }
  const rank = (state: string) => {
    const index = gateStateOrder.indexOf(state)
    return index === -1 ? gateStateOrder.length : index
  }
  const states = [...counts.keys()].sort((a, b) => rank(a) - rank(b))
  const label = [
    `${nextActions.length} ${nextActions.length === 1 ? "gate" : "gates"}`,
    ...states.map((state) => `${counts.get(state)} ${stateText(state).toLowerCase()}`),
  ].join(" · ")
  const tone = states.reduce<Tone>((worst, state) => {
    const candidate = statusTone("state", state)
    return toneRank[candidate] > toneRank[worst] ? candidate : worst
  }, "success")
  return { label, tone }
}

function GateActionRows({
  nextActions,
  status,
}: {
  nextActions: NextActionSummary[]
  status: "ready" | "loading" | "error"
}) {
  const [expanded, setExpanded] = useState(false)
  const summary = gateStateSummary(nextActions)

  return (
    <div className="mt-4 grid gap-2">
      {status === "error" ? (
        <p className="text-sm text-muted-foreground">Gate next actions unavailable. Check Doc Registry connectivity; no fallback next actions are shown in live mode.</p>
      ) : nextActions.length === 0 ? (
        <p className="text-sm text-muted-foreground">No next actions recorded.</p>
      ) : (
        <>
          <button
            type="button"
            aria-expanded={expanded}
            className="flex w-fit items-center gap-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            onClick={() => setExpanded((current) => !current)}
          >
            <ChevronRightIcon className={cn("size-3.5 transition-transform", expanded && "rotate-90")} />
            <Badge variant="outline" className={cn("border", toneClass(summary.tone))}>
              {summary.label}
            </Badge>
          </button>
          {expanded
            ? nextActions.map((action) => (
                <div
                  key={`${action.gate}-${action.state}`}
                  className={cn("sg-inset p-3", action.state === "not_applicable" && "opacity-60")}
                >
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span className="text-xs font-medium">{gateText(action.gate)}</span>
                    <Badge variant="outline" className={cn("border", toneClass(statusTone("state",action.state)))}>
                      {stateText(action.state)}
                    </Badge>
                  </div>
                  {gateChecks(action.gate) ? (
                    <p className="mt-1 text-xs text-muted-foreground">{gateChecks(action.gate)}</p>
                  ) : null}
                  <p className="mt-2 text-sm leading-5 text-muted-foreground">{action.hint}</p>
                </div>
              ))
            : null}
        </>
      )}
    </div>
  )
}

// Registry gate-run history repeats identical rows per refresh; default to the
// latest run per gate and keep the chronological list behind Show all runs.
function latestGateRuns(gateRuns: GateRunSummary[]): GateRunSummary[] {
  const latestByGate = new Map<string, GateRunSummary>()
  for (const run of gateRuns) {
    const current = latestByGate.get(run.gate)
    if (!current) {
      latestByGate.set(run.gate, run)
      continue
    }
    if (run.createdAt && current.createdAt && new Date(run.createdAt).getTime() > new Date(current.createdAt).getTime()) {
      latestByGate.set(run.gate, run)
    }
  }
  return [...latestByGate.values()]
}

function GateRunRows({
  gateRuns,
  status,
}: {
  gateRuns: GateRunSummary[]
  status: "ready" | "loading" | "error"
}) {
  const [showAllRuns, setShowAllRuns] = useState(false)
  const latestRuns = latestGateRuns(gateRuns)
  const visibleRuns = showAllRuns ? gateRuns : latestRuns

  return (
    <div className="mt-4 grid gap-2">
      {status === "error" ? (
        <p className="text-sm text-muted-foreground">Gate run history unavailable. Check Doc Registry connectivity; no fallback gate runs are shown in live mode.</p>
      ) : gateRuns.length === 0 ? (
        <p className="text-sm text-muted-foreground">Nothing checked yet.</p>
      ) : (
        <>
          {visibleRuns.map((run) => (
            <div key={run.id} className="sg-card p-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2">
                  <ClockIcon className="size-4 text-muted-foreground" />
                  <span className="text-xs font-medium">{gateText(run.gate)}</span>
                </div>
                <Badge variant="outline" className={cn("border", toneClass(statusTone("state",run.state)))}>
                  {stateText(run.state)}
                </Badge>
              </div>
              {gateChecks(run.gate) ? (
                <p className="mt-1 text-xs text-muted-foreground">{gateChecks(run.gate)}</p>
              ) : null}
              <p className="mt-2 text-sm leading-5 text-muted-foreground">{run.hint}</p>
              <GateEvidenceWhy evidence={run.evidence} executor={run.executor} />
              {run.createdAt ? <p className="mt-2 font-mono text-[11px] text-muted-foreground">{formatDateTime(run.createdAt)}</p> : null}
            </div>
          ))}
          {gateRuns.length > latestRuns.length ? (
            <button
              type="button"
              aria-expanded={showAllRuns}
              className="flex w-fit items-center gap-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
              onClick={() => setShowAllRuns((current) => !current)}
            >
              <ChevronRightIcon className={cn("size-3.5 transition-transform", showAllRuns && "rotate-90")} />
              {showAllRuns ? "Show latest run per gate" : "Show all runs"}
            </button>
          ) : null}
        </>
      )}
    </div>
  )
}

export function GateSummary({ item, detail }: { item: WorkItem; detail: WorkItemDetailData }) {
  const states = detail.nextActions.map((action) => action.state.trim())
  const gateState: WorkItem["gate"] = states.length === 0
    ? item.gate
    : states.includes("fail")
      ? "fail"
      : states.some((state) => !["pass", "not_applicable"].includes(state))
        ? "pending"
        : "pass"
  return (
    <section className="grid gap-3">
      <div data-slot="gate-state-card" className="sg-card p-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold">Readiness checks</h3>
            <p className="mt-1 text-xs text-muted-foreground">What was checked before this work was approved, and why.</p>
          </div>
          <Badge variant="outline" className={cn("border", toneClass(statusTone("gate", gateState)))}>
            {gateText(gateState)}
          </Badge>
        </div>
        <GateActionRows nextActions={detail.nextActions} status={detail.readback.nextActions} />
      </div>
      <div className="sg-card p-4">
        <h3 className="text-sm font-semibold">Gate run history</h3>
        <GateRunRows gateRuns={detail.gateRuns} status={detail.readback.gateRuns} />
      </div>
    </section>
  )
}
