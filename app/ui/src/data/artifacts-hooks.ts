import { useCallback, useEffect, useState } from "react"
import { useQuery } from "@tanstack/react-query"

import {
  emptyRegistryContent,
  emptyRegistryItems,
  emptyRegistryStatus,
  fetchArtifactAttachments,
  fetchArtifactDocumentContent,
  fetchArtifactEvents,
  fetchArtifactFeature,
  fetchArtifactFeedback,
  fetchArtifactFiles,
  fetchArtifactGatePreview,
  fetchArtifactPolicy,
  fetchArtifactReadinessRuns,
  fetchArtifacts,
  fetchArtifactVersions,
  registryItems,
  trimBase,
  withWorkspace,
  type ArtifactAttachmentSummary,
  type ArtifactAttachmentsView,
  type ArtifactDocumentContentView,
  type ArtifactDocumentSummary,
  type ArtifactEventSummary,
  type ArtifactEventsView,
  type ArtifactFeatureView,
  type ArtifactFeedbackSummary,
  type ArtifactFeedbackView,
  type ArtifactFilesView,
  type ArtifactGatePreviewSummary,
  type ArtifactGatePreviewView,
  type ArtifactListView,
  type ArtifactPolicyView,
  type ArtifactReadinessRunSummary,
  type ArtifactReadinessRunsView,
  type ArtifactSummary,
  type ArtifactVersionsView,
} from "./artifacts-core"

export function useArtifactData(workspaceId: string): ArtifactListView & { refresh: () => void } {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const enabled = Boolean(baseUrl && workspaceId.trim())
  const query = useQuery({
    queryKey: ["artifacts", baseUrl, workspaceId],
    queryFn: ({ signal }) => fetchArtifacts(baseUrl!, workspaceId, signal),
    enabled,
  })
  const refetch = query.refetch
  const refresh = useCallback(() => {
    if (enabled) void refetch()
  }, [enabled, refetch])
  const view = enabled
    ? (query.data ?? emptyRegistryItems<ArtifactSummary>(query.isError ? "error" : "loading"))
    : emptyRegistryItems<ArtifactSummary>("ready")
  return { ...view, refresh }
}

// Human approval decision on an artifact: approve or request changes. Durable
// action backed by PATCH /artifacts/{id}/status (doc-registry spec §6); the
// status transition and its artifact event are written server-side.
export async function updateArtifactStatus(
  artifactId: string,
  status: "approved" | "needs_changes",
  options: { approvedBy: string; note?: string },
  workspaceId: string,
): Promise<void> {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  if (!baseUrl) throw new Error("Doc Registry is not configured.")
  const response = await fetch(withWorkspace(`${trimBase(baseUrl)}/artifacts/${encodeURIComponent(artifactId)}/status`, workspaceId), {
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

export function useArtifactFiles(artifactId: string | undefined, workspaceId: string, enabled: boolean): ArtifactFilesView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactFilesView>(() =>
    emptyRegistryItems<ArtifactDocumentSummary>(baseUrl ? "loading" : "ready"),
  )

  useEffect(() => {
    if (!artifactId) {
      setView(emptyRegistryItems<ArtifactDocumentSummary>("ready"))
      return
    }

    if (!baseUrl || !workspaceId.trim()) {
      setView(emptyRegistryItems<ArtifactDocumentSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactDocumentSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactDocumentSummary>("loading"))

    void fetchArtifactFiles(baseUrl, artifactId, workspaceId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactDocumentSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled, workspaceId])

  return view
}

export function useArtifactVersions(
  featureId: string | undefined,
  current: ArtifactSummary | undefined,
  workspaceId: string,
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

    if (!baseUrl || !workspaceId.trim()) {
      setView(emptyRegistryItems<ArtifactSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(registryItems<ArtifactSummary>([current], "ready"))
      return
    }

    const controller = new AbortController()
    setView(registryItems<ArtifactSummary>([current], "loading"))

    void fetchArtifactVersions(baseUrl, featureId, workspaceId, controller.signal)
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
  }, [featureId, current, baseUrl, enabled, workspaceId])

  return view
}

export function useArtifactDocumentContent(
  artifactId: string | undefined,
  path: string | undefined,
  workspaceId: string,
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

    if (!baseUrl || !workspaceId.trim()) {
      setView(emptyRegistryContent("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryContent("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryContent("loading"))

    void fetchArtifactDocumentContent(baseUrl, artifactId, path, workspaceId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryContent("error", "Could not load this document body from Doc Registry."))
      })

    return () => controller.abort()
  }, [artifactId, path, baseUrl, enabled, workspaceId])

  return view
}

export function useArtifactAttachments(
  featureId: string | undefined,
  artifactId: string | undefined,
  workspaceId: string,
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

    if (!baseUrl || !workspaceId.trim()) {
      setView(emptyRegistryItems<ArtifactAttachmentSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactAttachmentSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactAttachmentSummary>("loading"))

    void fetchArtifactAttachments(baseUrl, featureId, workspaceId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactAttachmentSummary>("error"))
      })

    return () => controller.abort()
  }, [featureId, artifactId, baseUrl, enabled, workspaceId])

  return view
}

export function useArtifactFeedback(artifactId: string | undefined, workspaceId: string, enabled: boolean): ArtifactFeedbackView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactFeedbackView>(() =>
    emptyRegistryItems<ArtifactFeedbackSummary>(baseUrl ? "loading" : "ready"),
  )

  useEffect(() => {
    if (!artifactId) {
      setView(emptyRegistryItems<ArtifactFeedbackSummary>("ready"))
      return
    }

    if (!baseUrl || !workspaceId.trim()) {
      setView(emptyRegistryItems<ArtifactFeedbackSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactFeedbackSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactFeedbackSummary>("loading"))

    void fetchArtifactFeedback(baseUrl, artifactId, workspaceId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactFeedbackSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled, workspaceId])

  return view
}

export function useArtifactReadinessRuns(
  artifactId: string | undefined,
  workspaceId: string,
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

    if (!baseUrl || !workspaceId.trim()) {
      setView(emptyRegistryItems<ArtifactReadinessRunSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactReadinessRunSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactReadinessRunSummary>("loading"))

    void fetchArtifactReadinessRuns(baseUrl, artifactId, workspaceId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactReadinessRunSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled, workspaceId])

  return view
}

export function useArtifactEvents(
  artifactId: string | undefined,
  workspaceId: string,
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

    if (!baseUrl || !workspaceId.trim()) {
      setView(emptyRegistryItems<ArtifactEventSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactEventSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactEventSummary>("loading"))

    void fetchArtifactEvents(baseUrl, artifactId, workspaceId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactEventSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled, workspaceId])

  return view
}

export function useArtifactFeature(
  artifact: ArtifactSummary | undefined,
  workspaceId: string,
  enabled: boolean,
): ArtifactFeatureView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactFeatureView>(() => emptyRegistryStatus(baseUrl ? "loading" : "ready"))

  useEffect(() => {
    if (!artifact) {
      setView(emptyRegistryStatus("ready"))
      return
    }

    if (!baseUrl || !workspaceId.trim()) {
      setView(emptyRegistryStatus("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryStatus("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryStatus("loading"))

    void fetchArtifactFeature(baseUrl, artifact, workspaceId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView({ status: "error", source: "registry" })
      })

    return () => controller.abort()
  }, [artifact, baseUrl, enabled, workspaceId])

  return view
}

export function useArtifactPolicy(
  artifactId: string | undefined,
  workspaceId: string,
  enabled: boolean,
): ArtifactPolicyView {
  const baseUrl = import.meta.env.VITE_DOC_REGISTRY_URL as string | undefined
  const [view, setView] = useState<ArtifactPolicyView>(() => emptyRegistryStatus(baseUrl ? "loading" : "ready"))

  useEffect(() => {
    if (!artifactId) {
      setView(emptyRegistryStatus("ready"))
      return
    }

    if (!baseUrl || !workspaceId.trim()) {
      setView(emptyRegistryStatus("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryStatus("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryStatus("loading"))

    void fetchArtifactPolicy(baseUrl, artifactId, workspaceId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView({ status: "error", source: "registry" })
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled, workspaceId])

  return view
}

export function useArtifactGatePreview(
  artifactId: string | undefined,
  workspaceId: string,
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

    if (!baseUrl || !workspaceId.trim()) {
      setView(emptyRegistryItems<ArtifactGatePreviewSummary>("ready"))
      return
    }
    if (!enabled) {
      setView(emptyRegistryItems<ArtifactGatePreviewSummary>("ready"))
      return
    }

    const controller = new AbortController()
    setView(emptyRegistryItems<ArtifactGatePreviewSummary>("loading"))

    void fetchArtifactGatePreview(baseUrl, artifactId, workspaceId, controller.signal)
      .then((registryView) => setView(registryView))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setView(emptyRegistryItems<ArtifactGatePreviewSummary>("error"))
      })

    return () => controller.abort()
  }, [artifactId, baseUrl, enabled, workspaceId])

  return view
}
