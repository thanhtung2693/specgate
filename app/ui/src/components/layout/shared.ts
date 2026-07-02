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

export function looksLikeWorkItemKey(value: string | undefined) {
  return /^SG-\d+$/i.test(value ?? "")
}
