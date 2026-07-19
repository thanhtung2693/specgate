export type KnowledgeDocument = {
  documentId: string
  version: string
  workspaceId?: string
  parentVersion?: string
  isLatest: boolean
  title: string
  documentType: string
  authorityLevel: string
  sourceKind: string
  sourceUri?: string
  mimeType?: string
  originalFilename?: string
  status: string
  linkedFeatureId?: string
  linkedRequestId?: string
  uploadedBy?: string
  createdAt: string
  updatedAt: string
  summary?: string
  notes?: string
  tags: string[]
  errorMessage?: string
  chunkCount: number
}

export type KnowledgeListResult = {
  items: KnowledgeDocument[]
  total: number
  embeddingsEnabled: boolean
}

export type KnowledgeDocumentDetail = {
  document: KnowledgeDocument
  history: KnowledgeDocument[]
  extractedPreview?: string
}

export type UploadKnowledgeInput = {
  workspaceId: string
  file: File
  title: string
  documentType: string
  authorityLevel: string
  uploadedBy?: string
  actorRole?: string
  tags?: string[]
  notes?: string
  linkedFeatureId?: string
  linkedRequestId?: string
  documentId?: string
  parentVersion?: string
  newVersion?: string
}

export type CurateKnowledgeLinksInput = {
  workspaceId: string
  version?: string
  linkedFeatureId?: string
  linkedRequestId?: string
  clearFeatureLink?: boolean
  clearRequestLink?: boolean
  uploadedBy?: string
  actorRole?: string
  notes?: string
}

type KnowledgeDocumentDTO = Record<string, unknown>

function normalizedBase(baseUrl: string) {
  return baseUrl.replace(/\/$/, "")
}

function requiredWorkspaceId(workspaceId: string): string {
  const workspace = workspaceId.trim()
  if (!workspace) throw new Error("workspaceId is required")
  return workspace
}

function mapDocument(dto: KnowledgeDocumentDTO): KnowledgeDocument {
  return {
    documentId: String(dto.document_id ?? ""),
    version: String(dto.version ?? ""),
    workspaceId: dto.workspace_id as string | undefined,
    parentVersion: dto.parent_version as string | undefined,
    isLatest: Boolean(dto.is_latest),
    title: String(dto.title ?? ""),
    documentType: String(dto.document_type ?? ""),
    authorityLevel: String(dto.authority_level ?? ""),
    sourceKind: String(dto.source_kind ?? ""),
    sourceUri: dto.source_uri as string | undefined,
    mimeType: dto.mime_type as string | undefined,
    originalFilename: dto.original_filename as string | undefined,
    status: String(dto.status ?? ""),
    linkedFeatureId: dto.linked_feature_id as string | undefined,
    linkedRequestId: dto.linked_request_id as string | undefined,
    uploadedBy: dto.uploaded_by as string | undefined,
    createdAt: String(dto.created_at ?? ""),
    updatedAt: String(dto.updated_at ?? ""),
    summary: dto.summary as string | undefined,
    notes: dto.notes as string | undefined,
    tags: Array.isArray(dto.tags) ? dto.tags.map(String) : [],
    errorMessage: dto.error_message as string | undefined,
    chunkCount: Number(dto.chunk_count ?? 0),
  }
}

async function responseBody(response: Response, label: string): Promise<KnowledgeDocumentDTO> {
  if (!response.ok) throw new Error(`${label} request failed: ${response.status}`)
  return response.json() as Promise<KnowledgeDocumentDTO>
}

export async function listKnowledgeDocuments(baseUrl: string, workspaceId: string, signal?: AbortSignal): Promise<KnowledgeListResult> {
  const query = new URLSearchParams({ workspace_id: requiredWorkspaceId(workspaceId), limit: "100" })
  const response = await fetch(`${normalizedBase(baseUrl)}/documents?${query}`, { signal })
  const payload = await responseBody(response, "knowledge list")
  return {
    items: ((payload.items as KnowledgeDocumentDTO[]) ?? []).map(mapDocument),
    total: Number(payload.total ?? 0),
    embeddingsEnabled: Boolean(payload.embeddings_enabled),
  }
}

export async function getKnowledgeDocument(baseUrl: string, workspaceId: string, documentId: string, version?: string, signal?: AbortSignal): Promise<KnowledgeDocumentDetail> {
  const query = new URLSearchParams({ workspace_id: requiredWorkspaceId(workspaceId) })
  if (version) query.set("version", version)
  const response = await fetch(`${normalizedBase(baseUrl)}/documents/${encodeURIComponent(documentId)}?${query}`, { signal })
  const payload = await responseBody(response, "knowledge detail")
  return {
    document: mapDocument(payload.document as KnowledgeDocumentDTO),
    history: ((payload.history as KnowledgeDocumentDTO[]) ?? []).map(mapDocument),
    extractedPreview: payload.extracted_preview as string | undefined,
  }
}

function appendOptional(form: FormData, key: string, value?: string) {
  if (value) form.append(key, value)
}

export async function uploadKnowledgeDocument(baseUrl: string, input: UploadKnowledgeInput): Promise<KnowledgeDocument> {
  const form = new FormData()
  form.append("file", input.file)
  form.append("workspace_id", requiredWorkspaceId(input.workspaceId))
  form.append("title", input.title)
  form.append("document_type", input.documentType)
  form.append("authority_level", input.authorityLevel)
  appendOptional(form, "uploaded_by", input.uploadedBy)
  appendOptional(form, "actor_role", input.actorRole)
  appendOptional(form, "notes", input.notes)
  appendOptional(form, "linked_feature_id", input.linkedFeatureId)
  appendOptional(form, "linked_request_id", input.linkedRequestId)
  appendOptional(form, "document_id", input.documentId)
  appendOptional(form, "parent_version", input.parentVersion)
  appendOptional(form, "new_version", input.newVersion)
  input.tags?.forEach((tag) => form.append("tags", tag))
  const response = await fetch(`${normalizedBase(baseUrl)}/documents/upload`, { method: "POST", body: form })
  return mapDocument(await responseBody(response, "knowledge upload"))
}

export async function curateKnowledgeLinks(baseUrl: string, documentId: string, input: CurateKnowledgeLinksInput): Promise<KnowledgeDocument> {
  const body = {
    workspace_id: requiredWorkspaceId(input.workspaceId),
    version: input.version,
    linked_feature_id: input.linkedFeatureId,
    linked_request_id: input.linkedRequestId,
    clear_feature_link: input.clearFeatureLink,
    clear_request_link: input.clearRequestLink,
    uploaded_by: input.uploadedBy,
    actor_role: input.actorRole,
    notes: input.notes,
  }
  const cleanBody = Object.fromEntries(Object.entries(body).filter(([, value]) => value !== undefined))
  const response = await fetch(`${normalizedBase(baseUrl)}/documents/${encodeURIComponent(documentId)}/links`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(cleanBody),
  })
  return mapDocument(await responseBody(response, "knowledge link curation"))
}

export async function retryKnowledgeDocument(baseUrl: string, workspaceId: string, documentId: string, version: string): Promise<KnowledgeDocument> {
  const query = new URLSearchParams({ workspace_id: requiredWorkspaceId(workspaceId), version })
  const response = await fetch(`${normalizedBase(baseUrl)}/documents/${encodeURIComponent(documentId)}/retry?${query}`, { method: "POST" })
  return mapDocument(await responseBody(response, "knowledge retry"))
}

export async function deleteKnowledgeVersion(baseUrl: string, workspaceId: string, documentId: string, version: string): Promise<void> {
  const query = new URLSearchParams({ workspace_id: requiredWorkspaceId(workspaceId), version })
  const response = await fetch(`${normalizedBase(baseUrl)}/documents/${encodeURIComponent(documentId)}?${query}`, { method: "DELETE" })
  await responseBody(response, "knowledge delete")
}
