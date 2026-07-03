import { parseSettingsBody } from "@/data/model-settings"

export type GeneralSettingsData = {
  "governance.auto_archive_on_delivery_pass": "true" | "false"
  "governance.feature_freshness_sla_days": string
  "governance.artifact_stale_days": string
  "governancefiles.ttl_days": string
  "governance.gate_confidence_threshold": string
}

export type EditableGeneralSettingsData = Pick<
  GeneralSettingsData,
  | "governance.auto_archive_on_delivery_pass"
  | "governance.feature_freshness_sla_days"
  | "governance.artifact_stale_days"
>

export const defaultGeneralSettings: GeneralSettingsData = {
  "governance.auto_archive_on_delivery_pass": "false",
  "governance.feature_freshness_sla_days": "7",
  "governance.artifact_stale_days": "5",
  "governancefiles.ttl_days": "90",
  "governance.gate_confidence_threshold": "0.7",
}

export function normalizeGeneralSettings(settings?: Record<string, string>): GeneralSettingsData {
  return {
    ...defaultGeneralSettings,
    "governance.auto_archive_on_delivery_pass": settings?.["governance.auto_archive_on_delivery_pass"] === "true" ? "true" : "false",
    "governance.feature_freshness_sla_days":
      settings?.["governance.feature_freshness_sla_days"] || defaultGeneralSettings["governance.feature_freshness_sla_days"],
    "governance.artifact_stale_days":
      settings?.["governance.artifact_stale_days"] || defaultGeneralSettings["governance.artifact_stale_days"],
    "governancefiles.ttl_days": settings?.["governancefiles.ttl_days"] || defaultGeneralSettings["governancefiles.ttl_days"],
    "governance.gate_confidence_threshold":
      settings?.["governance.gate_confidence_threshold"] || defaultGeneralSettings["governance.gate_confidence_threshold"],
  }
}

export function editableGeneralSettings(settings: GeneralSettingsData): EditableGeneralSettingsData {
  return {
    "governance.auto_archive_on_delivery_pass": settings["governance.auto_archive_on_delivery_pass"],
    "governance.feature_freshness_sla_days": settings["governance.feature_freshness_sla_days"],
    "governance.artifact_stale_days": settings["governance.artifact_stale_days"],
  }
}

export async function loadGeneralSettings(base: string): Promise<GeneralSettingsData> {
  const response = await fetch(`${base}/settings`, { method: "GET" })
  if (!response.ok) throw new Error(`GET /settings failed (${response.status})`)
  return normalizeGeneralSettings(parseSettingsBody(await response.json()).settings)
}

export async function saveGeneralSettings(base: string, settings: EditableGeneralSettingsData): Promise<GeneralSettingsData> {
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
