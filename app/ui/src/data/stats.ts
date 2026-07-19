import { useEffect, useState } from "react"

type GovernanceStatsLedgerEntryDTO = {
  occurred_at?: string
  change_request_key?: string
  kind?: string
  gate?: string
  detail?: string
}

type GovernanceStatsDTO = {
  window_days?: number
  reviewed_items?: number
  first_pass?: number
  gate_catches_pre_build?: number
  review_catches_post_build?: number
  review_catches_fixed?: number
  rework?: number
  items_with_rework?: number
  ambiguity_blocks?: number
  cycle_time_avg_hours?: number
  cycle_time_items?: number
  ledger?: GovernanceStatsLedgerEntryDTO[]
}

type GovernanceStatsLedgerEntry = {
  occurredAt: string
  changeRequestKey: string
  kind: string
  gate?: string
  detail?: string
}

export type GovernanceStatsSummary = {
  windowDays: number
  reviewedItems: number
  firstPass: number
  gateCatchesPreBuild: number
  reviewCatchesPostBuild: number
  reviewCatchesFixed: number
  rework: number
  itemsWithRework: number
  ambiguityBlocks: number
  cycleTimeAvgHours: number
  cycleTimeItems: number
  ledger: GovernanceStatsLedgerEntry[]
}

export type GovernanceStatsView = {
  item?: GovernanceStatsSummary
  status: "ready" | "loading" | "error" | "unconfigured" | "workspace_required"
  source: "registry"
}

// buildStatsPath targets GET /api/v1/stats (doc-registry spec §6.8) with the
// fixed 30-day board window, scoped to the workspace when one is selected.
export function buildStatsPath(workspaceId?: string): string {
  const params = new URLSearchParams()
  const trimmed = workspaceId?.trim()
  if (trimmed) params.set("workspace_id", trimmed)
  params.set("days", "30")
  return `/api/v1/stats?${params.toString()}`
}

export function mapGovernanceStats(payload: GovernanceStatsDTO): GovernanceStatsSummary {
  return {
    windowDays: payload.window_days ?? 30,
    reviewedItems: payload.reviewed_items ?? 0,
    firstPass: payload.first_pass ?? 0,
    gateCatchesPreBuild: payload.gate_catches_pre_build ?? 0,
    reviewCatchesPostBuild: payload.review_catches_post_build ?? 0,
    reviewCatchesFixed: payload.review_catches_fixed ?? 0,
    rework: payload.rework ?? 0,
    itemsWithRework: payload.items_with_rework ?? 0,
    ambiguityBlocks: payload.ambiguity_blocks ?? 0,
    cycleTimeAvgHours: payload.cycle_time_avg_hours ?? 0,
    cycleTimeItems: payload.cycle_time_items ?? 0,
    ledger: (payload.ledger ?? []).flatMap((entry) => {
      const occurredAt = entry.occurred_at?.trim()
      const changeRequestKey = entry.change_request_key?.trim()
      if (!occurredAt || !changeRequestKey) return []
      return [
        {
          occurredAt,
          changeRequestKey,
          kind: entry.kind?.trim() || "catch",
          gate: entry.gate,
          detail: entry.detail,
        },
      ]
    }),
  }
}

export async function fetchGovernanceStats(
  baseUrl: string,
  workspaceId?: string,
  signal?: AbortSignal,
): Promise<GovernanceStatsView> {
	if (!workspaceId?.trim()) throw new Error("workspaceId is required")
  const response = await fetch(`${baseUrl.replace(/\/$/, "")}${buildStatsPath(workspaceId)}`, { signal })
  if (!response.ok) {
    throw new Error(`stats request failed: ${response.status}`)
  }

  const payload = (await response.json()) as GovernanceStatsDTO
  return { item: mapGovernanceStats(payload), status: "ready", source: "registry" }
}

// useGovernanceStats reads the read-only governance-value stats projection.
// It fetches on mount, so callers must mount it lazily (e.g. behind a
// disclosure) when the surface should not pay for stats on every load.
export function useGovernanceStats(workspaceId?: string): GovernanceStatsView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<GovernanceStatsView>(() =>
    baseUrl ? (workspaceId?.trim() ? { status: "loading", source: "registry" } : { status: "workspace_required", source: "registry" }) : { status: "unconfigured", source: "registry" },
  )

  useEffect(() => {
    if (!baseUrl) {
      setView({ status: "unconfigured", source: "registry" })
      return
    }
	if (!workspaceId?.trim()) {
		setView({ status: "workspace_required", source: "registry" })
		return
	}

    const controller = new AbortController()
    setView({ status: "loading", source: "registry" })

    void fetchGovernanceStats(baseUrl, workspaceId, controller.signal)
      .then((next) => setView(next))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView({ status: "error", source: "registry" })
      })

    return () => controller.abort()
  }, [baseUrl, workspaceId])

  return view
}
