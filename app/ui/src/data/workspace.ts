export type Lane = {
  title: string
  count: number
  tone: "neutral" | "accent" | "success" | "warning"
  items: WorkItem[]
}

export type Signal = {
  label: string
  value: string
  detail: string
  tone: "neutral" | "success" | "warning" | "danger"
}

export type WorkItem = {
  registryId?: string
  featureId?: string
  key: string
  title: string
  route: "quick" | "full"
  owner: string
  agent: string
  lifecycle: string
  status: string
  gate: "pass" | "pending" | "fail"
  delivery: "not_started" | "ready" | "needs_changes" | "passed"
  blocker: string
  age: string
  updated: string
  contextPackId?: string
  skills: string[]
  summary: string
  acceptance: string[]
  activity: string[]
}
