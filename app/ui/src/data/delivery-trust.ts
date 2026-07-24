import type { DeliveryStatusSummary } from "@/data/workboard"

type DeliveryTrustSummary = {
  evidence: string
  assurance: string
  decision: string
  reviewer: string
  decisionActor?: string
  receipt: string
  peerReview?: string
  modelReviewed: boolean
}

const assuranceLabels: Record<string, string> = {
  grounded: "local citation captured",
  deterministic: "locally reproduced",
  peer_reviewed: "second agent affirmed",
  repository_observed: "Submitted commit observed on merged PR/MR",
}

export function trustTierLabel(value: string | undefined): string {
  const normalized = value?.trim()
  if (!normalized) return "Unspecified"
  const labels: Record<string, string> = {
    grounded: "Grounded",
    deterministic: "Deterministic",
    peer_reviewed: "Peer reviewed",
    repository_observed: "Repository observed",
  }
  return labels[normalized] ?? "Unrecognized trust tier"
}

export function summarizeDeliveryTrust(status: DeliveryStatusSummary): DeliveryTrustSummary {
  const assurance = ["Agent-reported"]
  for (const source of status.assuranceSources ?? []) {
    const label = assuranceLabels[source.trim()]
    if (label && !assurance.includes(label)) assurance.push(label)
  }
  for (const criterion of status.criteria) {
    const label = assuranceLabels[criterion.trustTier?.trim() ?? ""]
    if (label && !assurance.includes(label)) assurance.push(label)
  }

  const executor = status.executor?.trim()
  const verdict = status.verdict?.trim()
  const evidenceVerdict = status.evidenceVerdict?.trim() || (executor === "human" ? undefined : verdict)
  const decision = executor === "human"
    ? verdict === "pass" || verdict === "passed"
      ? "Accepted"
      : "Rejected"
    : "Awaiting human acceptance"

  const summary: DeliveryTrustSummary = {
    evidence: evidenceLabel(evidenceVerdict, status.reasonCode),
    assurance: assurance.join("; "),
    decision,
    reviewer: reviewerLabel(status),
    receipt: receiptLabel(status),
    peerReview: status.peerReview?.state
      ? peerReviewLabel(status.peerReview.state)
      : undefined,
    modelReviewed: isModelReviewed(status),
  }
  if (executor === "human") {
    summary.decisionActor = status.actor?.trim()
      ? `Decision by ${status.actor.trim()}`
      : "Decision by human reviewer"
  }
  return summary
}

function receiptLabel(status: DeliveryStatusSummary): string {
  const receipt = status.gitReceipt
  if (!receipt) return "No Git receipt recorded"
  const warnings = receipt.warnings.map((warning) => warning.trim()).filter(Boolean)
  const warningSuffix = warnings.length === 0
    ? ""
    : warnings.length === 1
      ? `; warning: ${warnings[0]}`
      : `; warnings: ${warnings.join(" | ")}`
  const availability = receipt.availability?.trim()
  if (availability && availability !== "available") {
    return `Git receipt unavailable${warningSuffix}`
  }
  const headRevision = receipt.headRevision?.trim()
  return headRevision
    ? `Receipt recorded at commit ${headRevision.slice(0, 12)}${warningSuffix}`
    : `No Git receipt recorded${warningSuffix}`
}

function peerReviewLabel(value: string): string {
  const labels: Record<string, string> = {
    passed: "Passed",
    failed: "Failed",
    stale: "Stale",
    not_run: "Not run",
  }
  return labels[value.trim()] ?? "Unrecognized peer-review state"
}

function isModelReviewed(status: DeliveryStatusSummary): boolean {
  const judgeModel = status.judgeModel?.trim()
  return Boolean(
    judgeModel &&
    judgeModel !== "agent_attested" &&
    judgeModel !== "deterministic_checks" &&
    judgeModel !== "deterministic_policy_guard",
  )
}

function evidenceLabel(verdict: string | undefined, reasonCode: string | undefined): string {
  if (reasonCode?.trim() === "policy_unavailable") return "Policy unavailable"
  if (reasonCode?.trim() === "delivery_review_outdated") return "Review pending for latest completion"
  switch (verdict) {
    case "pass":
    case "passed":
      return "Ready for human review"
    case "fail":
    case "failed":
    case "needs_changes":
      return "Evidence gaps found"
    case "needs_human_review":
      return "Human review required"
    default:
      return "Not reviewed"
  }
}

function reviewerLabel(status: DeliveryStatusSummary): string {
  switch (status.judgeModel?.trim()) {
    case "agent_attested":
      return "Implementing agent"
    case "deterministic_policy_guard":
      return "Deterministic policy guard"
    case "deterministic_checks":
      return "Deterministic checks"
    case "":
    case undefined:
      return status.executor?.trim() === "human"
        ? "Evidence reviewer not recorded"
        : "Reviewer not recorded"
    default:
      return `Platform model (${status.judgeModel?.trim()})`
  }
}
