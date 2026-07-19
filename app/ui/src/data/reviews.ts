import { useCallback, useEffect, useMemo, useState } from "react"

import { fetchArtifactsByStatus, type ArtifactSummary } from "@/data/artifacts"
import { docRegistryBase } from "@/data/model-settings"

export type ArtifactDecisionQueue = {
  items: ArtifactSummary[]
  status: "ready" | "loading" | "error"
  source: "registry"
}

export async function loadArtifactDecisionQueue(base: string, workspaceId: string, signal: AbortSignal): Promise<ArtifactDecisionQueue> {
  const [drafts, needsChanges] = await Promise.all([
    fetchArtifactsByStatus(base, "draft", signal, workspaceId),
    fetchArtifactsByStatus(base, "needs_changes", signal, workspaceId),
  ])
  const items = new Map<string, ArtifactSummary>()
  for (const artifact of [...drafts.items, ...needsChanges.items]) items.set(artifact.id, artifact)
  return {
    items: [...items.values()].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)),
    status: "ready",
    source: "registry",
  }
}

function emptyArtifactDecisionQueue(status: ArtifactDecisionQueue["status"]): ArtifactDecisionQueue {
  return { items: [], status, source: "registry" }
}

export function useArtifactDecisionQueue(workspaceId: string): ArtifactDecisionQueue & { refresh: () => void } {
  const base = docRegistryBase()
  const [queue, setQueue] = useState<ArtifactDecisionQueue>(() => emptyArtifactDecisionQueue(base ? "loading" : "ready"))
  const [refreshToken, setRefreshToken] = useState(0)
  const refresh = useCallback(() => setRefreshToken((token) => token + 1), [])

  useEffect(() => {
    if (!base) {
      setQueue(emptyArtifactDecisionQueue("ready"))
      return
    }
    const controller = new AbortController()
    setQueue(emptyArtifactDecisionQueue("loading"))
    void loadArtifactDecisionQueue(base, workspaceId, controller.signal)
      .then(setQueue)
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setQueue(emptyArtifactDecisionQueue("error"))
      })
    return () => controller.abort()
  }, [base, refreshToken, workspaceId])

  return useMemo(() => ({ ...queue, refresh }), [queue, refresh])
}
