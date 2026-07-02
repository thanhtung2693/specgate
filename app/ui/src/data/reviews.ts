import { useCallback, useEffect, useMemo, useState } from "react"

import { docRegistryBase } from "@/data/model-settings"

type ArtifactEditProposalDTO = {
  id?: string
  base_artifact_id?: string
  base_version?: string
  state?: string
  last_diff_summary?: string
  source_kind?: string
  source_id?: string
  updated_at?: string
}

type ArtifactEditProposalListResponse = {
  items?: ArtifactEditProposalDTO[]
}

type ArtifactEditDiffFileDTO = {
  key?: string
  status?: string
  modified?: boolean
}

type ArtifactEditDiffResponse = {
  summary?: string
  files?: ArtifactEditDiffFileDTO[]
  unified_diff?: string
}

type GovernanceFeedbackEventDTO = {
  id?: string
  integration_id?: string
  resource_id?: string
  webhook_event_id?: string
  delivery_link_id?: string
  feature_id?: string
  change_request_id?: string
  artifact_id?: string
  event_type?: string
  payload_json?: string
  status?: string
  reason?: string
  created_at?: string
  updated_at?: string
}

type GovernanceFeedbackEventListResponse = {
  items?: GovernanceFeedbackEventDTO[]
}

export type ArtifactProposalSummary = {
  id: string
  baseArtifactId: string
  baseVersion: string
  state: string
  diffSummary: string
  sourceKind: string
  sourceId: string
  updatedAt?: string
}

export type ArtifactProposalDiff = {
  summary: string
  files: Array<{
    key: string
    status: string
    modified: boolean
  }>
  unifiedDiff: string
  status: "idle" | "loading" | "ready" | "error"
  source: "registry"
}

export type ArtifactProposalQueue = {
  items: ArtifactProposalSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type GovernanceFeedbackSignal = {
  id: string
  eventType: string
  status: string
  reason: string
  changeRequestId?: string
  artifactId?: string
  featureId?: string
  sourceLabel: string
  createdAt?: string
}

export type GovernanceFeedbackInbox = {
  items: GovernanceFeedbackSignal[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

function mapProposal(item: ArtifactEditProposalDTO): ArtifactProposalSummary | null {
  const id = item.id?.trim()
  if (!id) return null

  return {
    id,
    baseArtifactId: item.base_artifact_id || "unknown-artifact",
    baseVersion: item.base_version || "unknown",
    state: item.state || "active",
    diffSummary: item.last_diff_summary || "Diff not summarized yet",
    sourceKind: item.source_kind || "unknown",
    sourceId: item.source_id || "unknown",
    updatedAt: item.updated_at,
  }
}

function feedbackSourceLabel(item: GovernanceFeedbackEventDTO) {
  if (item.integration_id || item.resource_id || item.webhook_event_id || item.delivery_link_id) {
    return "Integration"
  }
  if (item.event_type?.startsWith("coding_agent.")) return "Coding agent"
  if (item.event_type?.startsWith("delivery.")) return "Delivery"
  if (item.event_type?.startsWith("planning.")) return "Planning"
  return "SpecGate"
}

function mapFeedbackSignal(item: GovernanceFeedbackEventDTO): GovernanceFeedbackSignal | null {
  const id = item.id?.trim()
  if (!id) return null

  return {
    id,
    eventType: item.event_type || "feedback",
    status: item.status || "received",
    reason: item.reason || "Feedback signal received.",
    changeRequestId: item.change_request_id || undefined,
    artifactId: item.artifact_id || undefined,
    featureId: item.feature_id || undefined,
    sourceLabel: feedbackSourceLabel(item),
    createdAt: item.created_at || item.updated_at,
  }
}

async function fetchArtifactProposals(base: string, signal: AbortSignal): Promise<ArtifactProposalQueue> {
  const response = await fetch(`${base}/artifact-edit/proposals`, { signal })
  if (!response.ok) {
    throw new Error(`artifact proposals request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ArtifactEditProposalListResponse
  return {
    items: (payload.items ?? []).flatMap((item) => {
      const mapped = mapProposal(item)
      return mapped ? [mapped] : []
    }),
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactProposalDiff(base: string, proposalId: string, signal: AbortSignal): Promise<ArtifactProposalDiff> {
  const response = await fetch(`${base}/artifact-edit/sessions/${encodeURIComponent(proposalId)}/diff`, { signal })
  if (!response.ok) {
    throw new Error(`artifact proposal diff request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ArtifactEditDiffResponse
  return {
    summary: payload.summary || "No diff summary returned",
    files: (payload.files ?? []).map((file, index) => ({
      key: file.key || `file-${index + 1}`,
      status: file.status || "modified",
      modified: file.modified ?? true,
    })),
    unifiedDiff: payload.unified_diff || "",
    status: "ready",
    source: "registry",
  }
}

async function fetchGovernanceFeedbackSignals(base: string, signal: AbortSignal): Promise<GovernanceFeedbackInbox> {
  const baseUrl = base.replace(/\/$/, "")
  const response = await fetch(`${baseUrl}/governance/feedback-events?status=received&limit=20`, { signal })
  if (!response.ok) {
    throw new Error(`governance feedback request failed: ${response.status}`)
  }

  const payload = (await response.json()) as GovernanceFeedbackEventListResponse
  return {
    items: (payload.items ?? []).flatMap((item) => {
      const signal = mapFeedbackSignal(item)
      return signal ? [signal] : []
    }),
    status: "ready",
    source: "registry",
  }
}

// Approve = save the sourced edit session as a draft revision on the base
// artifact; reject = discard the session. Both are durable human decisions
// backed by Doc Registry endpoints (per doc-registry spec §6).
export async function approveArtifactProposal(proposalId: string, requestedBy: string): Promise<void> {
  const base = docRegistryBase()
  if (!base) throw new Error("Doc Registry is not configured.")
  const response = await fetch(`${base}/artifact-edit/sessions/${encodeURIComponent(proposalId)}/save`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ requested_by: requestedBy }),
  })
  if (!response.ok) {
    throw new Error(`proposal approve failed: ${response.status}`)
  }
}

export async function rejectArtifactProposal(proposalId: string): Promise<void> {
  const base = docRegistryBase()
  if (!base) throw new Error("Doc Registry is not configured.")
  const response = await fetch(`${base}/artifact-edit/sessions/${encodeURIComponent(proposalId)}`, {
    method: "DELETE",
  })
  if (!response.ok) {
    throw new Error(`proposal reject failed: ${response.status}`)
  }
}

function emptyRegistryProposalQueue(status: ArtifactProposalQueue["status"]): ArtifactProposalQueue {
  return { items: [], status, source: "registry" }
}

function emptyRegistryFeedbackInbox(status: GovernanceFeedbackInbox["status"]): GovernanceFeedbackInbox {
  return { items: [], status, source: "registry" }
}

function emptyRegistryProposalDiff(status: ArtifactProposalDiff["status"]): ArtifactProposalDiff {
  return {
    summary: "No live diff loaded.",
    files: [],
    unifiedDiff: "",
    status,
    source: "registry",
  }
}

export function useArtifactProposals(): ArtifactProposalQueue & { refresh: () => void } {
  const base = docRegistryBase()
  const [queue, setQueue] = useState<ArtifactProposalQueue>(() => emptyRegistryProposalQueue(base ? "loading" : "ready"))
  const [refreshToken, setRefreshToken] = useState(0)
  const refresh = useCallback(() => setRefreshToken((token) => token + 1), [])

  useEffect(() => {
    if (!base) {
      setQueue(emptyRegistryProposalQueue("ready"))
      return
    }

    const controller = new AbortController()
    setQueue(emptyRegistryProposalQueue("loading"))

    void fetchArtifactProposals(base, controller.signal)
      .then(setQueue)
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setQueue(emptyRegistryProposalQueue("error"))
      })

    return () => controller.abort()
  }, [base, refreshToken])

  return useMemo(() => ({ ...queue, refresh }), [queue, refresh])
}

export function useGovernanceFeedbackSignals(): GovernanceFeedbackInbox {
  const base = docRegistryBase()
  const [inbox, setInbox] = useState<GovernanceFeedbackInbox>(() => emptyRegistryFeedbackInbox(base ? "loading" : "ready"))

  useEffect(() => {
    if (!base) {
      setInbox(emptyRegistryFeedbackInbox("ready"))
      return
    }

    const controller = new AbortController()
    setInbox(emptyRegistryFeedbackInbox("loading"))

    void fetchGovernanceFeedbackSignals(base, controller.signal)
      .then(setInbox)
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setInbox(emptyRegistryFeedbackInbox("error"))
      })

    return () => controller.abort()
  }, [base])

  return useMemo(() => inbox, [inbox])
}

export function useArtifactProposalDiff(
  proposal: ArtifactProposalSummary | undefined,
  enabled: boolean,
): ArtifactProposalDiff {
  const base = docRegistryBase()
  const [diff, setDiff] = useState<ArtifactProposalDiff>(() => emptyRegistryProposalDiff("idle"))

  useEffect(() => {
    if (!base) {
      setDiff(emptyRegistryProposalDiff("idle"))
      return
    }
    if (!proposal || !enabled) {
      setDiff(emptyRegistryProposalDiff("idle"))
      return
    }

    const controller = new AbortController()
    setDiff(emptyRegistryProposalDiff("loading"))

    void fetchArtifactProposalDiff(base, proposal.id, controller.signal)
      .then(setDiff)
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setDiff(emptyRegistryProposalDiff("error"))
      })

    return () => controller.abort()
  }, [base, enabled, proposal])

  return useMemo(() => diff, [diff])
}
