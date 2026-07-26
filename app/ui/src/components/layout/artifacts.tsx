// Artifact library list and filtering.

import { useCallback, useEffect, useMemo, useState } from "react"
import { useNavigate, useSearchParams } from "react-router"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useArtifactData } from "@/data/artifacts"
import { type WorkItem } from "@/data/workspace"
import { formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { readableKey, statusTone, toneClass } from "./shared"
import { ArtifactDetailDialog, artifactStatusText } from "./artifact-detail"

export function ArtifactsPage({ reviewer, workspaceId, workItems = [], routeArtifactId }: { reviewer: string; workspaceId: string; workItems?: WorkItem[]; routeArtifactId?: string }) {
  const artifacts = useArtifactData(workspaceId)
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const artifactParam = searchParams.get("artifact") ?? undefined
  const routeSelectedArtifactId = artifactParam ?? routeArtifactId
  const [selectedId, setSelectedId] = useState<string | undefined>(() => routeSelectedArtifactId)
  const [artifactQuery, setArtifactQuery] = useState("")
  const [artifactStatusFilter, setArtifactStatusFilter] = useState("current")
  const artifactStatuses = useMemo(
    () => Array.from(new Set(artifacts.items.map((artifact) => artifact.status))).filter(Boolean),
    [artifacts.items],
  )
  const filteredArtifacts = useMemo(() => {
    const query = artifactQuery.trim().toLowerCase()
    return artifacts.items.filter((artifact) => {
      const matchesStatus =
        artifactStatusFilter === "all" ||
        (artifactStatusFilter === "current" ? artifact.status !== "superseded" : artifact.status === artifactStatusFilter)
      const searchable = [
        artifact.featureName,
        artifact.featureId,
        artifact.id,
        artifact.version,
        artifactStatusText(artifact.status),
        readableKey(artifact.requestType),
      ]
        .join(" ")
        .toLowerCase()
      return matchesStatus && (!query || searchable.includes(query))
    })
  }, [artifactQuery, artifactStatusFilter, artifacts.items])
  const selectedArtifact = artifacts.items.find((artifact) => artifact.id === selectedId)
  const filtersActive = artifactQuery.trim().length > 0 || artifactStatusFilter !== "current"
  const liveArtifactEmpty = artifacts.source === "registry" && artifacts.status === "ready" && !filtersActive
  const artifactEmptyTitle =
    artifacts.status === "loading"
      ? "Loading artifacts"
      : artifacts.status === "error"
        ? "Artifact library unavailable"
        : liveArtifactEmpty
          ? "No artifacts in this workspace"
          : "No artifacts match"
  const artifactEmptyDescription =
    artifacts.status === "loading"
      ? "Reading artifact bundles from Doc Registry."
      : artifacts.status === "error"
        ? "Check Doc Registry connectivity, then refresh this library."
      : liveArtifactEmpty
        ? "Publish a governed artifact from the CLI or IDE agent, then refresh this library."
        : "Clear search or status filters to inspect the full library."

  const updateArtifactQuery = useCallback((id: string | undefined) => {
    const next = new URLSearchParams(searchParams)
    if (id) {
      next.set("artifact", id)
    } else {
      next.delete("artifact")
    }
    next.delete("feature")
    setSearchParams(next)
  }, [searchParams, setSearchParams])

  function selectArtifact(id: string) {
    setSelectedId(id)
    updateArtifactQuery(id)
  }

  useEffect(() => {
    setSelectedId(routeSelectedArtifactId)
  }, [routeSelectedArtifactId])

  function clearArtifactFilters() {
    setArtifactQuery("")
    setArtifactStatusFilter("current")
  }

  return (
    <div className="grid gap-4">
      <section className="min-w-0 overflow-hidden rounded-lg border bg-card">
        <div className="grid gap-2 border-b px-3 py-2.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-sm font-semibold">Artifact library</h2>
              <Badge variant="outline" className="font-mono">
                {filteredArtifacts.length}
              </Badge>
              <p className="text-xs text-muted-foreground">versioned bundles</p>
            </div>
          </div>
          {artifacts.status === "loading" || artifacts.items.length > 0 ? (
            <div className="flex flex-wrap items-center gap-2">
              <Input
                className="h-8 min-w-[220px] flex-1 rounded-md text-sm"
                value={artifactQuery}
                onChange={(event) => setArtifactQuery(event.target.value)}
                placeholder="Search artifacts"
                aria-label="Search artifacts"
              />
              <div className="flex flex-wrap gap-1.5">
                <Button
                  type="button"
                  variant={artifactStatusFilter === "current" ? "secondary" : "outline"}
                  size="sm"
                  className="h-8 rounded-md text-xs"
                  onClick={() => setArtifactStatusFilter("current")}
                >
                  Current
                </Button>
                <Button
                  type="button"
                  variant={artifactStatusFilter === "all" ? "secondary" : "outline"}
                  size="sm"
                  className="h-8 rounded-md text-xs"
                  onClick={() => setArtifactStatusFilter("all")}
                >
                  All statuses
                </Button>
                {artifactStatuses.map((status) => (
                  <Button
                    type="button"
                    key={status}
                    variant={artifactStatusFilter === status ? "secondary" : "outline"}
                    size="sm"
                    className="h-8 rounded-md text-xs"
                    onClick={() => setArtifactStatusFilter(status)}
                  >
                    {artifactStatusText(status)}
                  </Button>
                ))}
              </div>
            </div>
          ) : null}
        </div>
        <div>
          <table className="w-full table-fixed text-left text-xs" aria-label="Artifact library">
            <caption className="sr-only">Versioned artifacts in the active workspace</caption>
            <colgroup>
              <col className="w-[38%]" />
              <col className="w-[12%]" />
              <col className="w-[14%]" />
              <col className="w-[18%]" />
              <col className="w-[18%]" />
            </colgroup>
            <thead className="hidden border-b bg-muted/40 text-xs text-muted-foreground sm:table-header-group">
              <tr>
                <th scope="col" className="px-3 py-2 font-medium">Artifact</th>
                <th scope="col" className="px-3 py-2 font-medium">Version</th>
                <th scope="col" className="px-3 py-2 font-medium">Impact</th>
                <th scope="col" className="px-3 py-2 font-medium">Type</th>
                <th scope="col" className="px-3 py-2 font-medium">Updated</th>
              </tr>
            </thead>
            <tbody className="grid sm:table-row-group">
              {filteredArtifacts.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center">
                    <h3 className="text-sm font-semibold">{artifactEmptyTitle}</h3>
                    <p className="mt-1 text-sm text-muted-foreground">{artifactEmptyDescription}</p>
                    {filtersActive ? (
                      <Button type="button" variant="outline" size="sm" className="mt-3 rounded-md" onClick={clearArtifactFilters}>
                        Clear filters
                      </Button>
                    ) : liveArtifactEmpty ? (
                      <div className="mt-3 inline-flex rounded-md border bg-background px-3 py-2 font-mono text-xs text-muted-foreground">
                        specgate artifact publish --help
                      </div>
                    ) : null}
                  </td>
                </tr>
              ) : (
                filteredArtifacts.map((artifact) => {
                  const linkedWorkItem = workItems.find((item) => item.leadArtifactId === artifact.id)
                  return (
                <tr
                  key={artifact.id}
                  className={cn(
                    "grid gap-2 border-b px-3 py-3 transition-colors last:border-b-0 hover:bg-muted/35 sm:table-row sm:px-0 sm:py-0",
                    artifact.id === selectedId && "bg-muted/45",
                  )}
                >
                  <td className="min-w-0 sm:px-3 sm:py-2">
                    <button type="button" className="min-w-0 text-left" onClick={() => selectArtifact(artifact.id)}>
                      <span className="flex min-w-0 flex-wrap items-center gap-2">
                        <span className="truncate font-medium">{linkedWorkItem?.title ?? artifact.featureName}</span>
                        <Badge variant="outline" className={cn("shrink-0 border text-[0.68rem]", toneClass(statusTone("artifact",artifact.status)))}>
                          {artifactStatusText(artifact.status)}
                        </Badge>
                      </span>
                      <span className="mt-1 block font-mono text-xs text-muted-foreground">{linkedWorkItem?.key ?? artifact.featureId ?? "standalone quick-path"}</span>
                    </button>
                  </td>
                  <td className="flex justify-between gap-3 truncate font-mono text-muted-foreground sm:table-cell sm:px-3 sm:py-2"><span className="font-sans font-medium text-foreground sm:hidden">Version</span>{artifact.version}</td>
                  <td className="flex justify-between gap-3 truncate sm:table-cell sm:px-3 sm:py-2"><span className="font-medium sm:hidden">Impact</span>{readableKey(artifact.impactLevel)}</td>
                  <td className="flex justify-between gap-3 truncate sm:table-cell sm:px-3 sm:py-2"><span className="font-medium sm:hidden">Type</span>{readableKey(artifact.requestType)}</td>
                  <td className="flex justify-between gap-3 truncate text-muted-foreground sm:table-cell sm:px-3 sm:py-2"><span className="font-medium text-foreground sm:hidden">Updated</span>{formatDateTime(artifact.updatedAt)}</td>
                </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </section>
      <ArtifactDetailDialog
        artifact={selectedArtifact}
        open={selectedArtifact !== undefined}
        reviewer={reviewer}
        workspaceId={workspaceId}
        mode="library"
        onOpenChange={(open) => {
          if (!open) {
            setSelectedId(undefined)
            if (routeArtifactId) {
              navigate("/artifacts", { replace: true })
            } else {
              updateArtifactQuery(undefined)
            }
          }
        }}
        onDecided={artifacts.refresh}
      />
    </div>
  )
}
