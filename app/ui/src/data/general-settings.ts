import { parseSettingsBody } from "@/data/model-settings"

export type GeneralSettingsData = {
  "governance.auto_archive_on_delivery_pass": "true" | "false"
  "retention.artifact_sweep_enabled": "true" | "false"
}

export const defaultGeneralSettings: GeneralSettingsData = {
  "governance.auto_archive_on_delivery_pass": "false",
  "retention.artifact_sweep_enabled": "false",
}

function normalizeGeneralSettings(settings?: Record<string, string>): GeneralSettingsData {
  return {
    "governance.auto_archive_on_delivery_pass": settings?.["governance.auto_archive_on_delivery_pass"] === "true" ? "true" : "false",
    "retention.artifact_sweep_enabled": settings?.["retention.artifact_sweep_enabled"] === "true" ? "true" : "false",
  }
}

export async function loadGeneralSettings(base: string): Promise<GeneralSettingsData> {
  const response = await fetch(`${base}/settings`, { method: "GET" })
  if (!response.ok) throw new Error(`GET /settings failed (${response.status})`)
  return normalizeGeneralSettings(parseSettingsBody(await response.json()).settings)
}

export type WorkspaceCleanupCounts = {
  expired_artifacts_deleted: number
  referenced_skipped: number
  demo_features_deleted: number
  demo_change_requests_deleted: number
  demo_artifacts_deleted: number
  archived_change_requests_deleted: number
}

// Housekeeping cleanup: immediate retention sweep, demo seed removal, archived
// work-item purge. Never touches approved/draft artifacts, active features, or
// non-archived work items.
export async function runWorkspaceCleanup(base: string, workspaceId: string): Promise<WorkspaceCleanupCounts> {
  const workspace = workspaceId.trim()
  if (!workspace) throw new Error("workspaceId is required")
  const response = await fetch(`${base.replace(/\/$/, "")}/maintenance/cleanup?workspace_id=${encodeURIComponent(workspace)}`, { method: "POST" })
  if (!response.ok) {
    const text = await response.text().catch(() => "")
    throw new Error(`POST /maintenance/cleanup failed (${response.status})${text ? `: ${text.slice(0, 160)}` : ""}`)
  }
  const body = (await response.json()) as Partial<WorkspaceCleanupCounts>
  return {
    expired_artifacts_deleted: body.expired_artifacts_deleted ?? 0,
    referenced_skipped: body.referenced_skipped ?? 0,
    demo_features_deleted: body.demo_features_deleted ?? 0,
    demo_change_requests_deleted: body.demo_change_requests_deleted ?? 0,
    demo_artifacts_deleted: body.demo_artifacts_deleted ?? 0,
    archived_change_requests_deleted: body.archived_change_requests_deleted ?? 0,
  }
}

export async function saveGeneralSettings(base: string, settings: GeneralSettingsData): Promise<GeneralSettingsData> {
  const response = await fetch(`${base}/settings`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ settings }),
  })
  if (!response.ok) {
    const text = await response.text().catch(() => "")
    throw new Error(`PUT /settings failed (${response.status})${text ? `: ${text.slice(0, 160)}` : ""}`)
  }
  return normalizeGeneralSettings(parseSettingsBody(await response.json()).settings)
}
