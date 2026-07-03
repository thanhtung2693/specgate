// Cross-cutting pure presentation helpers shared across the layout domain
// modules (work board/detail, reviews, artifacts, settings). Keep this a leaf:
// it must not import from app-shell or any domain module.
import type { WorkItem } from "@/data/workspace"

export type WorkspaceProfile = {
  id?: string
  slug?: string
  name: string
  user: {
    id?: string
    name: string
    username: string
    email: string
  }
}

export function userInitials(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join("")
}

export function toneClass(tone: string) {
  return {
    accent: "border-primary/20 bg-[#eef0ff] text-primary dark:bg-primary/10 dark:text-primary",
    success: "border-success/20 bg-[#edf3ec] text-[#346538] dark:bg-success/10 dark:text-success",
    warning: "border-warning/20 bg-[#fbf3db] text-[#956400] dark:bg-warning/10 dark:text-warning",
    danger: "border-destructive/20 bg-[#fdebec] text-[#9f2f2d] dark:bg-destructive/10 dark:text-destructive",
    neutral: "border-border bg-secondary text-secondary-foreground dark:bg-[#23252a]",
  }[tone]
}

export type Tone = "neutral" | "success" | "warning" | "danger"

// Domain-keyed status→tone lookup. Each domain preserves its own mapping
// (e.g. "pending" is warning for gate runs but neutral for artifacts); do not
// merge rows across domains.
const statusToneByDomain = {
  gate: { pass: "success", pending: "warning", fail: "danger" },
  state: {
    pass: "success",
    not_applicable: "success",
    fail: "danger",
    warn: "warning",
    needs_human_review: "warning",
    pending: "warning",
    met: "success",
    unmet: "danger",
    unclear: "warning",
    covered: "success",
    partial: "warning",
    missing: "danger",
  },
  severity: {
    warning: "warning",
    warn: "warning",
    error: "danger",
    danger: "danger",
    critical: "danger",
    blocking: "danger",
  },
  artifact: {
    approved: "success",
    needs_changes: "warning",
  },
  plugin: { installed: "success", "needs refresh": "warning" },
  webhookEvent: { processed: "success", failed: "danger", pending: "warning" },
  integration: { connected: "success", error: "danger" },
  policy: { enhanced: "warning", light: "success" },
  feedbackSignal: {
    received: "warning",
    pending: "warning",
    accepted: "success",
    processed: "success",
  },
} satisfies Record<string, Record<string, Tone>>

export type StatusToneDomain = keyof typeof statusToneByDomain

export function statusTone(domain: StatusToneDomain, value: string): Tone {
  const key = domain === "severity" ? value.toLowerCase() : value
  return (statusToneByDomain[domain] as Record<string, Tone>)[key] ?? "neutral"
}

export function reviewTone(item: WorkItem): Tone {
  if (item.delivery === "needs_changes" || item.gate === "fail") return "warning"
  if (item.delivery === "ready" || item.delivery === "passed") return "success"
  return "neutral"
}

export function readableKey(value: string) {
  return value
    .split(/[_.-]+/)
    .filter(Boolean)
    .map((part) => part[0]?.toUpperCase() + part.slice(1))
    .join(" ")
}

export function gateText(gate: string) {
  return {
    pass: "Passed",
    pending: "Waiting",
    fail: "Failed",
  }[gate] ?? readableKey(gate)
}

export function stateText(state: string) {
  return {
    pass: "Passed",
    fail: "Failed",
    warn: "Warning",
    pending: "Waiting",
    not_applicable: "Not applicable",
    needs_human_review: "Needs review",
  }[state] ?? readableKey(state)
}

export function deliveryText(delivery: WorkItem["delivery"]) {
  return {
    not_started: "Not started",
    ready: "Ready",
    needs_changes: "Needs changes",
    passed: "Passed",
  }[delivery]
}

// Shared gate catalog: one plain sentence per gate covering what it verifies
// and what it reads, so any surface that lists a gate can explain it. Display
// names still come from gateText(); the catalog `name` is for surfaces that
// otherwise show only the raw key (e.g. Settings governance lists).
export type GateCatalogEntry = {
  name: string
  checks: string
}

export const gateCatalog: Record<string, GateCatalogEntry> = {
  spec_completeness: {
    name: "Spec completeness",
    checks: "Checks the package covers its required topics (outcomes, criteria, risks…). Reads every document in the package.",
  },
  scope_clear: {
    name: "Scope clarity",
    checks: "Checks the change is bounded with explicit non-goals. Reads the spec.",
  },
  acceptance_criteria_verifiable: {
    name: "Verifiable acceptance criteria",
    checks: "Checks every acceptance criterion is an observable, testable outcome. Reads the spec and verification documents.",
  },
  acceptance_criteria_edge_cases: {
    name: "Edge-case coverage",
    checks: "Checks criteria cover failure and edge paths, not just the happy path. Reads the spec and verification documents.",
  },
  success_metric_measurable: {
    name: "Measurable success metric",
    checks: "Checks the success metric can actually be measured. Reads the spec.",
  },
  rollback_plan_present: {
    name: "Rollback plan",
    checks: "Checks a rollback or recovery path is stated. Reads the spec and reference documents.",
  },
  implementation_plan_traceable: {
    name: "Traceable implementation plan",
    checks: "Checks plan tasks trace to criteria and scope. Reads the spec, plan, and verification documents.",
  },
  required_roles: {
    name: "Required document roles",
    checks: "Checks the required document roles are present in the package. Structural, no model.",
  },
  // Deterministic workflow gates: sentences follow the registry's NextActions
  // hint text (workboard_repository.go), not invented behavior.
  spec_drafted: {
    name: "Spec drafted",
    checks: "Checks a working spec is attached to the work item. Structural, no model.",
  },
  spec_approved: {
    name: "Spec approved",
    checks: "Checks the attached working spec has reached approved status. Structural, no model.",
  },
  canonical_spec: {
    name: "Canonical spec",
    checks: "Checks the working spec is promoted as the feature's canonical document. Structural, no model.",
  },
  no_conflicts: {
    name: "No conflicts",
    checks: "Checks impacted services have no blocking conflicts with other features. Structural, no model.",
  },
  knowledge_fresh: {
    name: "Knowledge freshness",
    checks: "Checks linked knowledge is not newer than the canonical spec. Structural, no model.",
  },
  delivery_pack: {
    name: "Delivery Context Pack",
    checks: "Checks an approved Context Pack exists for handoff.",
  },
  delivery_review: {
    name: "Delivery review",
    checks: "Judges the delivery evidence against every acceptance criterion.",
  },
}

export function gateChecks(gate: string): string | undefined {
  return gateCatalog[gate]?.checks
}

// Parsed view of a gate/readiness run's evidence_json. Parsing is defensive:
// evidence may be a plain quote string, a gate-run-v1 envelope with evaluator
// and confidence, per-topic completeness detail ({topics: [...]}), or delivery
// review detail ({criteria: [...], checks: [...]}). Anything unrecognized is
// dropped rather than rendered as raw JSON.
export type GateEvidenceRow = {
  label: string
  state?: string
  why?: string
}

export type GateEvidenceDetails = {
  evaluator?: "platform_model" | "agent"
  judgeModel?: string
  confidence?: number
  quote?: string
  rows: GateEvidenceRow[]
}

function evidenceRecord(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined
}

function evidenceString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined
}

function evidenceRowsFromTopics(topics: unknown): GateEvidenceRow[] {
  if (!Array.isArray(topics)) return []
  return topics.flatMap((entry) => {
    const row = evidenceRecord(entry)
    const label = evidenceString(row?.topic)
    if (!row || !label) return []
    const why = [evidenceString(row.why), evidenceString(row.where)].filter(Boolean).join(" — ")
    return [{ label, state: evidenceString(row.status), why: why || undefined }]
  })
}

function evidenceRowsFromCriteria(criteria: unknown, checks: unknown): GateEvidenceRow[] {
  const rows: GateEvidenceRow[] = []
  if (Array.isArray(criteria)) {
    for (const entry of criteria) {
      const row = evidenceRecord(entry)
      const label = evidenceString(row?.text) ?? evidenceString(row?.criterion_id)
      if (!row || !label) continue
      rows.push({ label, state: evidenceString(row.verdict), why: evidenceString(row.why) })
    }
  }
  if (Array.isArray(checks)) {
    for (const entry of checks) {
      const row = evidenceRecord(entry)
      const label = evidenceString(row?.name)
      if (!row || !label) continue
      rows.push({ label, state: evidenceString(row.status), why: evidenceString(row.detail) })
    }
  }
  return rows
}

// applyRunExecutor merges the run's first-class executor column into parsed
// evidence details: an ide_agent run is agent-attested even when its evidence
// carries no envelope. Platform (or absent) executors change nothing — the
// envelope, when present, is the richer source.
export function applyRunExecutor(
  details: GateEvidenceDetails | undefined,
  executor: string | undefined,
): GateEvidenceDetails | undefined {
  if (executor?.trim() !== "ide_agent") return details
  if (!details) return { rows: [], evaluator: "agent" }
  return { ...details, evaluator: "agent" }
}

export function parseGateEvidence(evidence: string | undefined): GateEvidenceDetails | undefined {
  const raw = evidence?.trim()
  if (!raw || raw === "{}") return undefined

  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    // Not JSON: readiness evidence can be a plain supporting quote.
    return { quote: raw, rows: [] }
  }

  if (typeof parsed === "string") {
    const quote = parsed.trim()
    return quote ? { quote, rows: [] } : undefined
  }

  const record = evidenceRecord(parsed)
  if (!record) return undefined

  const details: GateEvidenceDetails = { rows: [] }

  const evaluator = evidenceRecord(record.evaluator)
  const judgeModel = evidenceString(evaluator?.judge_model) ?? evidenceString(record.judge_model)
  const evaluatorType = evidenceString(evaluator?.type)
  const executor = evidenceString(record.executor) ?? evidenceString(evaluator?.executor)
  const isDeterministic = evaluatorType === "deterministic" || judgeModel === "deterministic-v1"

  if (judgeModel === "agent_attested" || judgeModel === "ide_agent" || executor === "ide_agent") {
    details.evaluator = "agent"
  } else if (!isDeterministic && (evaluatorType === "agent_judge" || judgeModel)) {
    details.evaluator = "platform_model"
    if (judgeModel && judgeModel !== "none") details.judgeModel = judgeModel
  }
  // Deterministic confidence is derived from the state, not a judgment — omit it.
  if (!isDeterministic && typeof record.confidence === "number") {
    details.confidence = record.confidence
  }

  // gate-run-v1 envelope: the inner `evidence` string may itself be a quote or
  // nested JSON detail (topics / criteria).
  const inner = record.evidence
  if (typeof inner === "string" && inner.trim()) {
    const nested = parseGateEvidence(inner)
    if (nested) {
      details.quote = nested.quote
      details.rows = nested.rows
      if (!details.evaluator && nested.evaluator) details.evaluator = nested.evaluator
    }
  }

  details.rows = [
    ...details.rows,
    ...evidenceRowsFromTopics(record.topics),
    ...evidenceRowsFromCriteria(record.criteria, record.checks),
  ]
  if (!details.quote) details.quote = evidenceString(record.summary)

  // ide_agent gate results carry free-form findings; keep string findings only.
  if (Array.isArray(record.findings)) {
    for (const finding of record.findings) {
      const text = evidenceString(finding)
      if (text) details.rows.push({ label: text })
    }
  }

  const hasContent =
    details.evaluator !== undefined ||
    details.confidence !== undefined ||
    details.quote !== undefined ||
    details.rows.length > 0
  return hasContent ? details : undefined
}

export function looksLikeWorkItemKey(value: string | undefined) {
  return /^SG-\d+$/i.test(value ?? "")
}

// Server-derived board phase for change requests whose latest delivery review
// passed. The phase arrives through `lifecycle`; older servers never send it.
export function isDeliveredWorkItem(item: Pick<WorkItem, "lifecycle">) {
  return item.lifecycle.trim().toLowerCase() === "delivered"
}
