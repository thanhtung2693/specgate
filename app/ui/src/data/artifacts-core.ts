
import { createRegistryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { mapGovernancePolicy, type GovernancePolicyDTO, type GovernancePolicySummary } from "@/data/workboard"

type ArtifactDTO = Partial<components["schemas"]["ArtifactDTO"]>

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
  executor?: string
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

type ArtifactFileContentResponse = {
  content?: string
}

type FeatureDTO = {
  id?: string
  key?: string
  name?: string
  status?: string
  version?: number
  canonical_artifact_id?: string
}

type ListFeaturesResponse = {
  items?: FeatureDTO[]
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
  executor?: string
  evidence?: string
  createdAt: string
}

export type ArtifactEventSummary = {
  id: string
  type: string
  payloadSummary: string
  createdAt: string
}

export type ArtifactFeatureSummary = {
  id: string
  key: string
  name: string
  status: string
  version?: number
  canonicalArtifactId?: string
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

export const gatePreviewNotRunNote = "Expected by this artifact's policy. Not run yet."

export function emptyRegistryItems<T>(status: "ready" | "loading" | "error") {
  return { items: [] as T[], status, source: "registry" as const }
}

export function registryItems<T>(items: T[], status: "ready" | "loading" | "error") {
  return { items, status, source: "registry" as const }
}

export function emptyRegistryStatus(status: "ready" | "loading" | "error") {
  return { status, source: "registry" as const }
}

export function emptyRegistryContent(status: ArtifactDocumentContentView["status"], unavailableReason?: string): ArtifactDocumentContentView {
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
    executor: item.executor,
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

export function mapArtifactFeature(item: FeatureDTO): ArtifactFeatureSummary | null {
  const id = item.id?.trim() || item.key?.trim()
  if (!id) return null

  return {
    id,
    key: item.key?.trim() || id,
    name: item.name?.trim() || item.key?.trim() || id,
    status: item.status || "unknown",
    version: item.version,
    canonicalArtifactId: item.canonical_artifact_id,
  }
}

export function mapArtifactGatePreview(item: ArtifactGatePreviewDTO): ArtifactGatePreviewSummary | null {
  const gateKey = item.gate_key?.trim()
  if (!gateKey) return null

  const note = item.note?.trim()
  return {
    gateKey,
    gateVersion: item.gate_version,
    executor: item.executor,
    note: note || gatePreviewNotRunNote,
  }
}

// Shared fetch → check → parse → map pipeline for the registry list endpoints
// that all return `{ items: [...] }`. Each mapper drops rows missing real
// registry ids/paths (returns null), so those guards stay per-summary. Callers
// that need extra shaping (e.g. version sorting) wrap the returned items.
async function fetchRegistryList<In, Out>(
  url: string,
  signal: AbortSignal,
  errorLabel: string,
  mapper: (item: In) => Out | null,
): Promise<{ items: Out[]; status: "ready"; source: "registry" }> {
  const response = await fetch(url, { signal })
  if (!response.ok) {
    throw new Error(`${errorLabel} request failed: ${response.status}`)
  }

  const payload = (await response.json()) as { items?: In[] }

  return {
    items: (payload.items ?? []).flatMap((item) => {
      const mapped = mapper(item)
      return mapped ? [mapped] : []
    }),
    status: "ready",
    source: "registry",
  }
}

export const trimBase = (baseUrl: string) => baseUrl.replace(/\/$/, "")

export function withWorkspace(path: string, workspaceId: string): string {
  const workspace = workspaceId.trim()
  if (!workspace) throw new Error("workspaceId is required")
  return `${path}${path.includes("?") ? "&" : "?"}workspace_id=${encodeURIComponent(workspace)}`
}

export async function fetchArtifacts(baseUrl: string, workspaceId: string, signal: AbortSignal): Promise<ArtifactListView> {
  const { data, error, response } = await createRegistryClient(baseUrl).GET("/artifacts", {
    params: { query: { workspace_id: workspaceId, limit: 50 } },
    signal,
  })
  if (error || !data) {
    throw new Error(`artifacts request failed: ${response.status}`)
  }
  return {
    items: (data.items ?? []).flatMap((item) => {
      const mapped = mapArtifact(item)
      return mapped ? [mapped] : []
    }),
    status: "ready",
    source: "registry",
  }
}

export async function fetchArtifactsByStatus(
  baseUrl: string,
  status: "draft" | "needs_changes",
  signal: AbortSignal,
  workspaceId: string,
): Promise<ArtifactListView> {
  const { data, error, response } = await createRegistryClient(baseUrl).GET("/artifacts", {
    params: { query: { workspace_id: workspaceId, status, limit: 200 } },
    signal,
  })
  if (error || !data) {
    throw new Error(`artifacts request failed: ${response.status}`)
  }
  return {
    items: (data.items ?? []).flatMap((item) => {
      const mapped = mapArtifact(item)
      return mapped ? [mapped] : []
    }),
    status: "ready",
    source: "registry",
  }
}

export async function fetchArtifactVersions(baseUrl: string, featureId: string, workspaceId: string, signal: AbortSignal): Promise<ArtifactVersionsView> {
  const url = withWorkspace(`${trimBase(baseUrl)}/artifacts?feature_id=${encodeURIComponent(featureId)}&limit=100`, workspaceId)
  const view = await fetchRegistryList(url, signal, "artifact versions", mapArtifact)

  return {
    ...view,
    items: view.items.sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true })),
  }
}

export async function fetchArtifactFiles(baseUrl: string, artifactId: string, workspaceId: string, signal: AbortSignal): Promise<ArtifactFilesView> {
  return fetchRegistryList(
    withWorkspace(`${trimBase(baseUrl)}/artifacts/${encodeURIComponent(artifactId)}/files`, workspaceId),
    signal,
    "artifact files",
    mapArtifactFile,
  )
}

export async function fetchArtifactDocumentContent(
  baseUrl: string,
  artifactId: string,
  path: string,
  workspaceId: string,
  signal: AbortSignal,
): Promise<ArtifactDocumentContentView> {
  const response = await fetch(
    withWorkspace(`${trimBase(baseUrl)}/artifacts/${encodeURIComponent(artifactId)}/files/_?path=${encodeURIComponent(path)}`, workspaceId),
    { signal },
  )
  if (!response.ok) {
    throw new Error(`artifact file content request failed: ${response.status}`)
  }

  const payload = (await response.json()) as ArtifactFileContentResponse
  const content = payload.content ?? ""

  return {
    content,
    status: "ready",
    source: "registry",
  }
}

export async function fetchArtifactAttachments(
  baseUrl: string,
  featureId: string,
  workspaceId: string,
  signal: AbortSignal,
): Promise<ArtifactAttachmentsView> {
  return fetchRegistryList(
    withWorkspace(`${trimBase(baseUrl)}/features/${encodeURIComponent(featureId)}/attachments`, workspaceId),
    signal,
    "artifact attachments",
    mapArtifactAttachment,
  )
}

export async function fetchArtifactFeedback(
  baseUrl: string,
  artifactId: string,
  workspaceId: string,
  signal: AbortSignal,
): Promise<ArtifactFeedbackView> {
  return fetchRegistryList(
    withWorkspace(`${trimBase(baseUrl)}/governance/feedback-events?artifact_id=${encodeURIComponent(artifactId)}&limit=20`, workspaceId),
    signal,
    "artifact feedback",
    mapArtifactFeedback,
  )
}

export async function fetchArtifactReadinessRuns(
  baseUrl: string,
  artifactId: string,
  workspaceId: string,
  signal: AbortSignal,
): Promise<ArtifactReadinessRunsView> {
  return fetchRegistryList(
    withWorkspace(`${trimBase(baseUrl)}/artifacts/${encodeURIComponent(artifactId)}/readiness-runs?limit=20`, workspaceId),
    signal,
    "artifact readiness runs",
    mapArtifactReadinessRun,
  )
}

export async function fetchArtifactEvents(
  baseUrl: string,
  artifactId: string,
  workspaceId: string,
  signal: AbortSignal,
): Promise<ArtifactEventsView> {
  const query = new URLSearchParams({ artifact_id: artifactId, limit: "20" })
  return fetchRegistryList(
    withWorkspace(`${trimBase(baseUrl)}/events?${query.toString()}`, workspaceId),
    signal,
    "artifact events",
    mapArtifactEvent,
  )
}

export async function fetchArtifactFeature(
  baseUrl: string,
  artifact: ArtifactSummary,
  workspaceId: string,
  signal: AbortSignal,
): Promise<ArtifactFeatureView> {
  const response = await fetch(withWorkspace(`${trimBase(baseUrl)}/workboard/features`, workspaceId), { signal })
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

export async function fetchArtifactPolicy(
  baseUrl: string,
  artifactId: string,
  workspaceId: string,
  signal: AbortSignal,
): Promise<ArtifactPolicyView> {
  const response = await fetch(withWorkspace(`${trimBase(baseUrl)}/api/v1/artifacts/${encodeURIComponent(artifactId)}/policy`, workspaceId), { signal })
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

export async function fetchArtifactGatePreview(
  baseUrl: string,
  artifactId: string,
  workspaceId: string,
  signal: AbortSignal,
): Promise<ArtifactGatePreviewView> {
  const response = await fetch(withWorkspace(`${trimBase(baseUrl)}/api/v1/artifacts/${encodeURIComponent(artifactId)}/gate-preview`, workspaceId), { signal })
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
