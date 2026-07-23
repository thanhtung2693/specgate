import { useCallback, useEffect, useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"

import {
  emptyRegistryDetail,
  emptyRegistryView,
  fetchWorkboard,
  fetchWorkItemDetail,
  type WorkboardData,
  type WorkItemDetailData,
} from "./workboard-core"
import type { WorkItem } from "./workspace"

export function useWorkboardData(selectedWorkspaceId?: string): WorkboardData {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [refreshGeneration, setRefreshGeneration] = useState(0)
  const workspaceId = selectedWorkspaceId?.trim() ?? ""
  const enabled = Boolean(baseUrl && workspaceId)
  const query = useQuery({
    queryKey: ["workboard", baseUrl, workspaceId],
    queryFn: ({ signal }) => fetchWorkboard(baseUrl!, signal, workspaceId),
    enabled,
  })
  const refetch = query.refetch
  const refresh = useCallback(() => {
    if (!enabled) return
    setRefreshGeneration((generation) => generation + 1)
    void refetch()
  }, [enabled, refetch])
  const view = enabled
    ? (query.data ?? { ...emptyRegistryView, status: query.isError ? "error" : "loading" })
    : emptyRegistryView
  const refreshing = refreshGeneration > 0 && query.isFetching
  const lastRefreshedAt = refreshGeneration > 0 && query.dataUpdatedAt > 0
    ? new Date(query.dataUpdatedAt).toISOString()
    : undefined
  const refreshError = refreshGeneration > 0 && query.isError
    ? "Refresh failed. Showing the last successful data."
    : undefined

  return { ...view, refresh, refreshing, lastRefreshedAt, refreshError, refreshGeneration }
}

export function useWorkItemDetail(item: WorkItem, workspaceId: string, enabled: boolean, refreshGeneration = 0): WorkItemDetailData {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [detail, setDetail] = useState<WorkItemDetailData>(() => (baseUrl ? emptyRegistryDetail("loading") : emptyRegistryDetail("ready")))

  useEffect(() => {
    if (!baseUrl) {
      setDetail(emptyRegistryDetail("ready"))
      return
    }
    if (!enabled || !workspaceId.trim()) {
      setDetail(emptyRegistryDetail("ready"))
      return
    }

    const controller = new AbortController()
    setDetail(emptyRegistryDetail("loading"))

    void fetchWorkItemDetail(baseUrl, item, workspaceId, controller.signal)
      .then((registryDetail) => setDetail(registryDetail))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setDetail(emptyRegistryDetail("error"))
      })

    return () => controller.abort()
  }, [baseUrl, enabled, item, refreshGeneration, workspaceId])

  return useMemo(() => detail, [detail])
}
