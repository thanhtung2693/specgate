import { useCallback, useEffect, useMemo, useState } from "react"

import { mapGovernancePolicy, type GovernancePolicySummary } from "@/data/workboard"

type ArtifactDTO = {
  id?: string
  feature_id?: string
  feature_name?: string
  version?: string
  status?: string
  request_type?: string
  impact_level?: string
  artifact_completeness?: string
  source_kind?: string
  source_revision?: string
  created_by?: string
  updated_at?: string
  expected_gates?: string[]
}

type ArtifactGatePreviewDTO = {
  gate_key?: string
  gate_version?: string
  executor?: string
  note?: string
}

type ArtifactGatePreviewResponse = {
  artifact_id?: string
  preview_tasks?: ArtifactGatePreviewDTO[]
  body?: {
    artifact_id?: string
    preview_tasks?: ArtifactGatePreviewDTO[]
  }
}

type ArtifactFileDTO = {
  path?: string
  role?: string
  size_bytes?: number
  updated_at?: string
}

type ArtifactAttachmentDTO = {
  id?: string
  feature_id?: string
  kind?: string
  url?: string
  governance_file_id?: string
  title?: string
  note?: string
  audience?: string
  created_by?: string
  created_at?: string
}

type ArtifactFeedbackEventDTO = {
  id?: string
  artifact_id?: string
  event_type?: string
  status?: string
  reason?: string
  created_at?: string
  updated_at?: string
}

type ArtifactReadinessRunDTO = {
  id?: string
  artifact_id?: string
  gate?: string
  state?: string
  hint?: string
  evidence_json?: string
  created_at?: string
}

type ArtifactEventDTO = {
  id?: string
  artifact_id?: string
  event_type?: string
  payload?: Record<string, unknown>
  created_at?: string
}

type ArtifactRevisionDTO = {
  revision_id?: string
  base_artifact_id?: string
  artifact_id?: string
  materialized_artifact_id?: string
  state?: string
  session_id?: string
  parent_revision_id?: string
  lineage_root_artifact_id?: string
  created_at?: string
}

type ListArtifactsResponse = {
  items?: ArtifactDTO[]
}

type ListArtifactFilesResponse = {
  items?: ArtifactFileDTO[]
}

type ListArtifactAttachmentsResponse = {
  items?: ArtifactAttachmentDTO[]
}

type ListArtifactFeedbackEventsResponse = {
  items?: ArtifactFeedbackEventDTO[]
}

type ListArtifactReadinessRunsResponse = {
  items?: ArtifactReadinessRunDTO[]
}

type ListArtifactEventsResponse = {
  items?: ArtifactEventDTO[]
}

type ListArtifactRevisionsResponse = {
  items?: ArtifactRevisionDTO[]
}

type ArtifactFileContentResponse = {
  content?: string
  signed_url?: string
}

type FeatureDTO = {
  id?: string
  key?: string
  name?: string
  summary?: string
  status?: string
  version?: number
  canonical_artifact_id?: string
  summary_md?: string
  summary_source_version?: string
}

type ListFeaturesResponse = {
  items?: FeatureDTO[]
}

type GovernancePolicyDTO = {
  governance_level?: string
  title?: string
  summary?: string
  reasons?: string[]
  obligations?: string[]
  policy_lineage?: Array<{
    key?: string
    version?: string
    digest?: string
  }>
}

export type ArtifactDocumentSummary = {
  path: string
  role: string
  sizeBytes: number
  updatedAt?: string
  hasMermaid?: boolean
}

export type ArtifactSummary = {
  id: string
  featureId?: string
  featureName: string
  version: string
  status: string
  requestType: string
  impactLevel: string
  completeness: string
  sourceKind: string
  sourceRevision?: string
  createdBy: string
  updatedAt: string
  expectedGates: string[]
}

export type ArtifactListView = {
  items: ArtifactSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type ArtifactFilesView = {
  items: ArtifactDocumentSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type ArtifactDocumentContentView = {
  content: string
  status: "ready" | "loading" | "error"
  source: "registry"
  unavailableReason?: string
}

export type ArtifactVersionsView = {
  items: ArtifactSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type ArtifactAttachmentSummary = {
  id: string
  kind: string
  title: string
  note?: string
  audience: string
  url?: string
  governanceFileId?: string
  createdBy?: string
  createdAt: string
}

export type ArtifactFeedbackSummary = {
  id: string
  type: string
  status: string
  reason: string
  createdAt: string
}

export type ArtifactReadinessRunSummary = {
  id: string
  gate: string
  state: string
  hint: string
  evidence?: string
  createdAt: string
}

export type ArtifactEventSummary = {
  id: string
  type: string
  payloadSummary: string
  createdAt: string
}

export type ArtifactRevisionSummary = {
  id: string
  state: string
  baseArtifactId: string
  materializedArtifactId?: string
  parentRevisionId?: string
  lineageRootArtifactId?: string
  createdAt: string
}

export type ArtifactFeatureSummary = {
  id: string
  key: string
  name: string
  status: string
  version?: number
  summary?: string
  summaryMd?: string
  canonicalArtifactId?: string
  summarySourceVersion?: string
}

export type ArtifactAttachmentsView = {
  items: ArtifactAttachmentSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type ArtifactFeedbackView = {
  items: ArtifactFeedbackSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type ArtifactReadinessRunsView = {
  items: ArtifactReadinessRunSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type ArtifactEventsView = {
  items: ArtifactEventSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type ArtifactRevisionsView = {
  items: ArtifactRevisionSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type ArtifactFeatureView = {
  item?: ArtifactFeatureSummary
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type ArtifactPolicyView = {
  item?: GovernancePolicySummary
  status: "ready" | "loading" | "error"
  source: "registry"
}

export type ArtifactGatePreviewSummary = {
  gateKey: string
  gateVersion?: string
  executor?: string
  note: string
}

export type ArtifactGatePreviewView = {
  items: ArtifactGatePreviewSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

function emptyRegistryItems<T>(status: "ready" | "loading" | "error") {
  return { items: [] as T[], status, source: "registry" as const }
}

function registryItems<T>(items: T[], status: "ready" | "loading" | "error") {
  return { items, status, source: "registry" as const }
}

function emptyRegistryStatus(status: "ready" | "loading" | "error") {
  return { status, source: "registry" as const }
}

function emptyRegistryContent(status: ArtifactDocumentContentView["status"], unavailableReason?: string): ArtifactDocumentContentView {
  return { content: "", status, source: "registry", unavailableReason }
}

export function mapArtifact(item: ArtifactDTO): ArtifactSummary | null {
  const id = item.id?.trim()
  if (!id) return null

  return {
    id,
    featureId: item.feature_id || undefined,
    featureName: item.feature_name || item.feature_id || "Standalone artifact",
    version: item.version || "v0.0",
    status: item.status || "draft",
    requestType: item.request_type || "unknown",
    impactLevel: item.impact_level || "low",
    completeness: item.artifact_completeness || "partial",
    sourceKind: item.source_kind || "unknown",
    sourceRevision: item.source_revision,
    createdBy: item.created_by || "unknown",
    updatedAt: item.updated_at || "unknown",
    expectedGates: item.expected_gates ?? [],
  }
}

function mapArtifactFile(item: ArtifactFileDTO): ArtifactDocumentSummary | null {
  const path = item.path?.trim()
  if (!path) return null

  return {
    path,
    role: item.role || "unspecified",
    sizeBytes: item.size_bytes ?? 0,
    updatedAt: item.updated_at,
  }
}

function mapArtifactAttachment(item: ArtifactAttachmentDTO): ArtifactAttachmentSummary | null {
  const id = item.id?.trim()
  if (!id) return null

  return {
    id,
    kind: item.kind || "link",
    title: item.title || item.url || item.governance_file_id || id,
    note: item.note,
    audience: item.audience || "gate",
    url: item.url,
    governanceFileId: item.governance_file_id,
    createdBy: item.created_by,
    createdAt: item.created_at || "unknown",
  }
}

function mapArtifactFeedback(item: ArtifactFeedbackEventDTO): ArtifactFeedbackSummary | null {
  const id = item.id?.trim()
  if (!id) return null

  return {
    id,
    type: item.event_type || "feedback",
    status: item.status || "received",
    reason: item.reason || "Feedback received.",
    createdAt: item.created_at || item.updated_at || "unknown",
  }
}

function mapArtifactReadinessRun(item: ArtifactReadinessRunDTO): ArtifactReadinessRunSummary | null {
  const id = item.id?.trim()
  if (!id) return null

  return {
    id,
    gate: item.gate || "unknown",
    state: item.state || "not_run",
    hint: item.hint || "No readiness hint recorded.",
    evidence: item.evidence_json,
    createdAt: item.created_at || "unknown",
  }
}

function summarizeEventPayload(payload: Record<string, unknown> | undefined): string {
  if (!payload || Object.keys(payload).length === 0) return "No payload"

  const preferred = ["version", "status", "reason", "actor", "source_kind"]
    .map((key) => {
      const value = payload[key]
      return typeof value === "string" && value.trim() ? `${key}: ${value}` : ""
    })
    .filter(Boolean)

  if (preferred.length > 0) return preferred.join(" / ")

  try {
    return JSON.stringify(payload).slice(0, 120)
  } catch {
    return "Payload recorded"
  }
}

function mapArtifactEvent(item: ArtifactEventDTO): ArtifactEventSummary | null {
  const id = item.id?.trim()
  if (!id) return null

  return {
    id,
    type: item.event_type || "artifact.event",
    payloadSummary: summarizeEventPayload(item.payload),
    createdAt: item.created_at || "unknown",
  }
}

function mapArtifactRevision(item: ArtifactRevisionDTO): ArtifactRevisionSummary | null {
  const id = item.revision_id?.trim()
  if (!id) return null

  return {
    id,
    state: item.state || "saved",
    baseArtifactId: item.base_artifact_id || item.artifact_id || "unknown-artifact",
    materializedArtifactId: item.materialized_artifact_id || item.artifact_id || undefined,
    parentRevisionId: item.parent_revision_id || undefined,
    lineageRootArtifactId: item.lineage_root_artifact_id || undefined,
    createdAt: item.created_at || "unknown",
  }
}

export function mapArtifactFeature(item: FeatureDTO): ArtifactFeatureSummary | null {
  const id = item.id?.trim() || item.key?.trim()
  if (!id) return null

  return {
    id,
    key: item.key?.trim() || id,
    name: item.name?.trim() || item.key?.trim() || id,
    status: item.status || "unknown",
    version: item.version,
    summary: item.summary,
    summaryMd: item.summary_md,
    canonicalArtifactId: item.canonical_artifact_id,
    summarySourceVersion: item.summary_source_version,
  }
}

export function mapArtifactGatePreview(item: ArtifactGatePreviewDTO): ArtifactGatePreviewSummary | null {
  const gateKey = item.gate_key?.trim()
  if (!gateKey) return null

  return {
    gateKey,
    gateVersion: item.gate_version,
    executor: item.executor,
    note: item.note || "preview - not persisted",
  }
}

async function fetchArtifacts(baseUrl: string, signal: AbortSignal): Promise<ArtifactListView> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/artifacts?limit=50`, { signal })
  if (!response.ok) {
    throw new Error(`artifacts request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListArtifactsResponse

  return {
    items: (payload.items ?? []).flatMap((item) => {
      const mapped = mapArtifact(item)
      return mapped ? [mapped] : []
    }),
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactVersions(baseUrl: string, featureId: string, signal: AbortSignal): Promise<ArtifactVersionsView> {
  const url = `${baseUrl.replace(/\/$/, "")}/artifacts?feature_id=${encodeURIComponent(featureId)}&limit=100`
  const response = await fetch(url, { signal })
  if (!response.ok) {
    throw new Error(`artifact versions request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListArtifactsResponse
  const items = (payload.items ?? []).flatMap((item) => {
    const mapped = mapArtifact(item)
    return mapped ? [mapped] : []
  })

  return {
    items: items.sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true })),
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactFiles(baseUrl: string, artifactId: string, signal: AbortSignal): Promise<ArtifactFilesView> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/artifacts/${encodeURIComponent(artifactId)}/files`, { signal })
  if (!response.ok) {
    throw new Error(`artifact files request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListArtifactFilesResponse

  return {
    items: (payload.items ?? []).flatMap((item) => {
      const mapped = mapArtifactFile(item)
      return mapped ? [mapped] : []
    }),
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactDocumentContent(
  baseUrl: string,
  artifactId: string,
  path: string,
  signal: AbortSignal,
): Promise<ArtifactDocumentContentView> {
  const response = await fetch(
    `${baseUrl.replace(/\/$/, "")}/artifacts/${encodeURIComponent(artifactId)}/files/_?path=${encodeURIComponent(path)}`,
    { signal },
  )
  if (!response.ok) {
    throw new Error(`artifact file content request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ArtifactFileContentResponse
  const content = payload.content ?? ""

  if (content.trim().length === 0 && payload.signed_url) {
    return {
      content: "",
      status: "error",
      source: "registry",
      unavailableReason: "The document exists, but its stored content is unavailable from this local registry volume.",
    }
  }

  return {
    content,
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactAttachments(
  baseUrl: string,
  featureId: string,
  signal: AbortSignal,
): Promise<ArtifactAttachmentsView> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/features/${encodeURIComponent(featureId)}/attachments`, { signal })
  if (!response.ok) {
    throw new Error(`artifact attachments request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListArtifactAttachmentsResponse

  return {
    items: (payload.items ?? []).flatMap((item) => {
      const attachment = mapArtifactAttachment(item)
      return attachment ? [attachment] : []
    }),
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactFeedback(
  baseUrl: string,
  artifactId: string,
  signal: AbortSignal,
): Promise<ArtifactFeedbackView> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/governance/feedback-events?artifact_id=${encodeURIComponent(artifactId)}&limit=20`, {
    signal,
  })
  if (!response.ok) {
    throw new Error(`artifact feedback request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListArtifactFeedbackEventsResponse

  return {
    items: (payload.items ?? []).flatMap((item) => {
      const feedback = mapArtifactFeedback(item)
      return feedback ? [feedback] : []
    }),
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactReadinessRuns(
  baseUrl: string,
  artifactId: string,
  signal: AbortSignal,
): Promise<ArtifactReadinessRunsView> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/artifacts/${encodeURIComponent(artifactId)}/readiness-runs?limit=20`, {
    signal,
  })
  if (!response.ok) {
    throw new Error(`artifact readiness runs request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListArtifactReadinessRunsResponse

  return {
    items: (payload.items ?? []).flatMap((item) => {
      const run = mapArtifactReadinessRun(item)
      return run ? [run] : []
    }),
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactEvents(
  baseUrl: string,
  artifactId: string,
  signal: AbortSignal,
): Promise<ArtifactEventsView> {
  const query = new URLSearchParams({ artifact_id: artifactId, limit: "20" })
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/events?${query.toString()}`, { signal })
  if (!response.ok) {
    throw new Error(`artifact events request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListArtifactEventsResponse

  return {
    items: (payload.items ?? []).flatMap((item) => {
      const event = mapArtifactEvent(item)
      return event ? [event] : []
    }),
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactRevisions(
  baseUrl: string,
  artifactId: string,
  signal: AbortSignal,
): Promise<ArtifactRevisionsView> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/artifacts/${encodeURIComponent(artifactId)}/revisions`, { signal })
  if (!response.ok) {
    throw new Error(`artifact revisions request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListArtifactRevisionsResponse

  return {
    items: (payload.items ?? []).flatMap((item) => {
      const revision = mapArtifactRevision(item)
      return revision ? [revision] : []
    }),
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactFeature(
  baseUrl: string,
  artifact: ArtifactSummary,
  signal: AbortSignal,
): Promise<ArtifactFeatureView> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/workboard/features`, { signal })
  if (!response.ok) {
    throw new Error(`features request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ListFeaturesResponse
  const match = (payload.items ?? []).find((feature) => {
    if (!artifact.featureId) return feature.canonical_artifact_id === artifact.id
    const featureRefs = [feature.id, feature.key].filter(Boolean)
    return (
      feature.canonical_artifact_id === artifact.id ||
      featureRefs.includes(artifact.featureId)
    )
  })

  return {
    item: match ? mapArtifactFeature(match) ?? undefined : undefined,
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactPolicy(
  baseUrl: string,
  artifactId: string,
  signal: AbortSignal,
): Promise<ArtifactPolicyView> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/api/v1/artifacts/${encodeURIComponent(artifactId)}/policy`, { signal })
  if (!response.ok) {
    throw new Error(`artifact policy request failed: ${response.status}`)
  }

  const payload = (await response.json()) as GovernancePolicyDTO

  return {
    item: mapGovernancePolicy(payload),
    status: "ready",
    source: "registry",
  }
}

async function fetchArtifactGatePreview(
  baseUrl: string,
  artifactId: string,
  signal: AbortSignal,
): Promise<ArtifactGatePreviewView> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/api/v1/artifacts/${encodeURIComponent(artifactId)}/gate-preview`, { signal })
  if (!response.ok) {
    throw new Error(`artifact gate preview request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ArtifactGatePreviewResponse
  const previewTasks = payload.preview_tasks ?? payload.body?.preview_tasks ?? []

  return {
    items: previewTasks.flatMap((item) => {
      const preview = mapArtifactGatePreview(item)
      return preview ? [preview] : []
    }),
    status: "ready",
    source: "registry",
  }
}

export function useArtifactData(): ArtifactListView & { refresh: () => void } {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactListView>(() => emptyRegistryItems<ArtifactSummary>(baseUrl ? "loading" : "ready"))
  const [refreshToken, setRefreshToken] = useState(0)
  const refresh = useCallback(() => setRefreshToken((token) => token + 1), [])

  useEffect(() => {
    if (!baseUrl) {
      setView(emptyRegistryItems<ArtifactSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactSummary>("loading"))

    void fetchArtifacts(baseUrl, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactSummary>("error"))
      })

    return () => controller.abort()
  }, [baseUrl, refreshToken])

  return useMemo(() => ({ ...view, refresh }), [view, refresh])
}

// Human approval decision on an artifact: approve or request changes. Durable
// action backed by PATCH /artifacts/{id}/status (doc-registry spec §6); the
// status transition and its artifact event are written server-side.
export async function updateArtifactStatus(
  artifactId: string,
  status: "approved" | "needs_changes",
  options: { approvedBy: string; note?: string },
): Promise<void> {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  if (!baseUrl) throw new Error("Doc Registry is not configured.")
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/artifacts/${encodeURIComponent(artifactId)}/status`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      status,
      approved_by: options.approvedBy,
      note: options.note || undefined,
      actor_kind: "human",
    }),
  })
  if (!response.ok) {
    throw new Error(`artifact status update failed: ${response.status}`)
  }
}

export type FeatureListView = {
  items: ArtifactFeatureSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

async function fetchFeatureList(baseUrl: string, signal: AbortSignal): Promise<FeatureListView> {
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}/workboard/features`, { signal })
  if (!response.ok) {
    throw new Error(`features request failed: ${response.status}`)
  }
  const payload = (await response.json()) as ListFeaturesResponse
  const items = (payload.items ?? [])
    .map(mapArtifactFeature)
    .filter((feature): feature is ArtifactFeatureSummary => feature !== null)
  return { items, status: "ready", source: "registry" }
}

// useFeatureList lists the governed features (distinct, not per-version) so a
// user can look up an existing feature's key before publishing a new artifact.
export function useFeatureList(): FeatureListView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<FeatureListView>(() =>
    baseUrl ? { items: [], status: "loading", source: "registry" } : { items: [], status: "ready", source: "registry" },
  )

  useEffect(() => {
    if (!baseUrl) {
      setView({ items: [], status: "ready", source: "registry" })
      return
    }
    const controller = new AbortController()
    setView({ items: [], status: "loading", source: "registry" })
    void fetchFeatureList(baseUrl, controller.signal)
      .then((next) => setView(next))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView({ items: [], status: "error", source: "registry" })
      })
    return () => controller.abort()
  }, [baseUrl])

  return useMemo(() => view, [view])
}

export function useArtifactFiles(artifactId: string | undefined, enabled: boolean): ArtifactFilesView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactFilesView>(() =>
    emptyRegistryItems<ArtifactDocumentSummary>(baseUrl ? "loading" : "ready"),
  )

  useEffect(() => {
    if (!artifactId) {
      setView(emptyRegistryItems<ArtifactDocumentSummary>("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryItems<ArtifactDocumentSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactDocumentSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactDocumentSummary>("loading"))

    void fetchArtifactFiles(baseUrl, artifactId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactDocumentSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled])

  return useMemo(() => view, [view])
}

export function useArtifactVersions(
  featureId: string | undefined,
  current: ArtifactSummary | undefined,
  enabled: boolean,
): ArtifactVersionsView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactVersionsView>(() =>
    baseUrl ? registryItems<ArtifactSummary>(current ? [current] : [], "loading") : emptyRegistryItems<ArtifactSummary>("ready"),
  )

  useEffect(() => {
    if (!featureId || !current) {
      setView(emptyRegistryItems<ArtifactSummary>("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryItems<ArtifactSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(registryItems<ArtifactSummary>([current], "ready"))
      return
    }

    const controller = new AbortController()
    setView(registryItems<ArtifactSummary>([current], "loading"))

    void fetchArtifactVersions(baseUrl, featureId, controller.signal)
      .then((registryView) => {
        const hasCurrent = registryView.items.some((item) => item.id === current.id)
        setView({
          ...registryView,
          items: hasCurrent ? registryView.items : [current, ...registryView.items],
        })
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(registryItems<ArtifactSummary>([current], "error"))
      })

    return () => controller.abort()
  }, [featureId, current, baseUrl, enabled])

  return useMemo(() => view, [view])
}

export function useArtifactDocumentContent(
  artifactId: string | undefined,
  path: string | undefined,
  enabled: boolean,
): ArtifactDocumentContentView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactDocumentContentView>(() =>
    emptyRegistryContent(baseUrl ? "loading" : "ready"),
  )

  useEffect(() => {
    if (!artifactId || !path) {
      setView(emptyRegistryContent("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryContent("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryContent("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryContent("loading"))

    void fetchArtifactDocumentContent(baseUrl, artifactId, path, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryContent("error", "Could not load this document body from Doc Registry."))
      })

    return () => controller.abort()
  }, [artifactId, path, baseUrl, enabled])

  return useMemo(() => view, [view])
}

export function useArtifactAttachments(
  featureId: string | undefined,
  artifactId: string | undefined,
  enabled: boolean,
): ArtifactAttachmentsView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactAttachmentsView>(() =>
    emptyRegistryItems<ArtifactAttachmentSummary>(baseUrl ? "loading" : "ready"),
  )

  useEffect(() => {
    if (!featureId || !artifactId) {
      setView(emptyRegistryItems<ArtifactAttachmentSummary>("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryItems<ArtifactAttachmentSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactAttachmentSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactAttachmentSummary>("loading"))

    void fetchArtifactAttachments(baseUrl, featureId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactAttachmentSummary>("error"))
      })

    return () => controller.abort()
  }, [featureId, artifactId, baseUrl, enabled])

  return useMemo(() => view, [view])
}

export function useArtifactFeedback(artifactId: string | undefined, enabled: boolean): ArtifactFeedbackView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactFeedbackView>(() =>
    emptyRegistryItems<ArtifactFeedbackSummary>(baseUrl ? "loading" : "ready"),
  )

  useEffect(() => {
    if (!artifactId) {
      setView(emptyRegistryItems<ArtifactFeedbackSummary>("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryItems<ArtifactFeedbackSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactFeedbackSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactFeedbackSummary>("loading"))

    void fetchArtifactFeedback(baseUrl, artifactId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactFeedbackSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled])

  return useMemo(() => view, [view])
}

export function useArtifactReadinessRuns(
  artifactId: string | undefined,
  enabled: boolean,
): ArtifactReadinessRunsView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactReadinessRunsView>(() =>
    emptyRegistryItems<ArtifactReadinessRunSummary>(baseUrl ? "loading" : "ready"),
  )

  useEffect(() => {
    if (!artifactId) {
      setView(emptyRegistryItems<ArtifactReadinessRunSummary>("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryItems<ArtifactReadinessRunSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactReadinessRunSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactReadinessRunSummary>("loading"))

    void fetchArtifactReadinessRuns(baseUrl, artifactId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactReadinessRunSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled])

  return useMemo(() => view, [view])
}

export function useArtifactEvents(
  artifactId: string | undefined,
  enabled: boolean,
): ArtifactEventsView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactEventsView>(() =>
    emptyRegistryItems<ArtifactEventSummary>(baseUrl ? "loading" : "ready"),
  )

  useEffect(() => {
    if (!artifactId) {
      setView(emptyRegistryItems<ArtifactEventSummary>("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryItems<ArtifactEventSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactEventSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactEventSummary>("loading"))

    void fetchArtifactEvents(baseUrl, artifactId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactEventSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled])

  return useMemo(() => view, [view])
}

export function useArtifactRevisions(
  artifactId: string | undefined,
  enabled: boolean,
): ArtifactRevisionsView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactRevisionsView>(() =>
    emptyRegistryItems<ArtifactRevisionSummary>(baseUrl ? "loading" : "ready"),
  )

  useEffect(() => {
    if (!artifactId) {
      setView(emptyRegistryItems<ArtifactRevisionSummary>("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryItems<ArtifactRevisionSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactRevisionSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactRevisionSummary>("loading"))

    void fetchArtifactRevisions(baseUrl, artifactId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactRevisionSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled])

  return useMemo(() => view, [view])
}

export function useArtifactFeature(
  artifact: ArtifactSummary | undefined,
  enabled: boolean,
): ArtifactFeatureView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactFeatureView>(() => emptyRegistryStatus(baseUrl ? "loading" : "ready"))

  useEffect(() => {
    if (!artifact) {
      setView(emptyRegistryStatus("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryStatus("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryStatus("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryStatus("loading"))

    void fetchArtifactFeature(baseUrl, artifact, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView({ status: "error", source: "registry" })
      })

    return () => controller.abort()
  }, [artifact, baseUrl, enabled])

  return useMemo(() => view, [view])
}

export function useArtifactPolicy(
  artifactId: string | undefined,
  enabled: boolean,
): ArtifactPolicyView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactPolicyView>(() => emptyRegistryStatus(baseUrl ? "loading" : "ready"))

  useEffect(() => {
    if (!artifactId) {
      setView(emptyRegistryStatus("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryStatus("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryStatus("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryStatus("loading"))

    void fetchArtifactPolicy(baseUrl, artifactId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView({ status: "error", source: "registry" })
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled])

  return useMemo(() => view, [view])
}

export function useArtifactGatePreview(
  artifactId: string | undefined,
  fallbackGates: string[] | undefined,
  enabled: boolean,
): ArtifactGatePreviewView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactGatePreviewView>(() =>
    emptyRegistryItems<ArtifactGatePreviewSummary>(baseUrl ? "loading" : "ready"),
  )

  useEffect(() => {
    if (!artifactId) {
      setView(emptyRegistryItems<ArtifactGatePreviewSummary>("ready"))
      return
    }

    if (!baseUrl) {
      setView(emptyRegistryItems<ArtifactGatePreviewSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactGatePreviewSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactGatePreviewSummary>("loading"))

    void fetchArtifactGatePreview(baseUrl, artifactId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactGatePreviewSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, fallbackGates, baseUrl, enabled])

  return useMemo(() => view, [view])
}
