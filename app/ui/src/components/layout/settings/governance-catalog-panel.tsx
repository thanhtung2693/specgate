// Settings panel extracted from settings.tsx. See app/ui/AGENTS.md.

import { useEffect, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  loadGovernancePolicyLevels,
  type GovernancePolicyLevelSummary,
} from "@/data/governance"
import { docRegistryBase } from "@/data/model-settings"
import { TeamSkillsPanel } from "./skills-settings-panel"

type LoadState<T> = { status: "idle" | "loading" | "ready" | "error"; items: T[] }

export function GovernanceSettingsPanel({ workspaceId }: { workspaceId?: string }) {
  return (
    <section className="grid gap-6">
      <TeamSkillsPanel workspaceId={workspaceId} />
      <GovernancePolicyReference />
    </section>
  )
}

function GovernancePolicyReference() {
  const registryBase = docRegistryBase()
  const started = useRef(false)
  const levelsController = useRef<AbortController | null>(null)
  const [levels, setLevels] = useState<LoadState<GovernancePolicyLevelSummary>>({ status: "idle", items: [] })
  const referenceOpenRef = useRef(false)
  const loadReferenceRef = useRef<() => void>(() => undefined)

  useEffect(() => () => {
    levelsController.current?.abort()
  }, [])

  useEffect(() => {
    levelsController.current?.abort()
    started.current = false
    setLevels({ status: "idle", items: [] })
    if (referenceOpenRef.current) {
      queueMicrotask(() => loadReferenceRef.current())
    }
  }, [registryBase])

  function loadLevels() {
    if (!registryBase) return
    levelsController.current?.abort()
    levelsController.current = new AbortController()
    setLevels({ status: "loading", items: [] })
    void loadGovernancePolicyLevels(registryBase, levelsController.current.signal)
      .then((items) => setLevels({ status: "ready", items }))
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setLevels({ status: "error", items: [] })
      })
  }

  function loadReference() {
    if (!registryBase || started.current) return
    started.current = true
    loadLevels()
  }

  loadReferenceRef.current = loadReference

  const settled = levels.status === "ready" || levels.status === "error"
  const summary = settled
    ? `${levels.items.length} ${levels.items.length === 1 ? "tier" : "tiers"}`
    : levels.status === "loading" ? "Loading…" : "Automatic policy tiers"

  return (
    <details className="group border-t pt-5" onToggle={(event) => { referenceOpenRef.current = event.currentTarget.open; if (event.currentTarget.open) loadReference() }}>
      <summary className="cursor-pointer list-none rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold">Policy reference</h3>
            <p className="mt-1 text-xs text-muted-foreground">{summary}</p>
          </div>
          <span aria-hidden="true" className="text-muted-foreground transition-transform group-open:rotate-90">›</span>
        </div>
      </summary>
      <div className="mt-4 grid gap-5">
        {!registryBase ? <p className="rounded-md border p-3 text-sm text-muted-foreground">Configure VITE_DOC_REGISTRY_URL to view policy reference.</p> : null}
        {levels.status === "error" ? <div className="flex items-center justify-between gap-3"><p className="text-sm text-muted-foreground">Policy tiers unavailable.</p><Button size="sm" variant="outline" onClick={loadLevels}>Retry policy tiers</Button></div> : null}
        {levels.status === "ready" ? <PolicyLevels levels={levels.items} /> : null}
      </div>
    </details>
  )
}

function PolicyLevels({ levels }: { levels: GovernancePolicyLevelSummary[] }) {
  return <CatalogGroup title="Policy tiers" empty="No policy tiers advertised yet." rows={levels.map((level) => ({
    key: level.level, title: level.displayName, id: level.level, approvalPolicy: level.approvalPolicy,
    evidencePolicy: level.evidencePolicy, gates: level.enabledGates, evidence: level.requiredEvidence,
    roles: level.requiredRoles, topics: level.requiredTopics,
  }))} />
}

type CatalogRow = { key: string; title: string; id: string; approvalPolicy: string; evidencePolicy: string; gates: string[]; evidence: string[]; roles: string[]; topics: string[] }

function CatalogGroup({ title, empty, rows }: { title: string; empty: string; rows: CatalogRow[] }) {
  return (
    <section className="grid gap-2">
      <h3 className="text-sm font-semibold">{title}</h3>
      {rows.length === 0 ? <p className="rounded-md border p-3 text-sm text-muted-foreground">{empty}</p> : rows.map((row) => <CatalogDetails key={row.key} row={row} />)}
    </section>
  )
}

function CatalogDetails({ row }: { row: CatalogRow }) {
  const counts = [countLabel(row.gates.length, "gate"), countLabel(row.evidence.length, "evidence"), countLabel(row.roles.length, "role"), countLabel(row.topics.length, "topic")].join(" · ")
  return (
    <details className="rounded-md border bg-background/70 p-3">
      <summary className="cursor-pointer list-none">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="font-medium">{row.title}</span>
          <span className="flex flex-wrap gap-2">
            <Badge variant="outline" className="rounded-md font-mono text-[0.7rem]">{row.approvalPolicy}</Badge>
            <Badge variant="outline" className="rounded-md font-mono text-[0.7rem]">{row.evidencePolicy}</Badge>
          </span>
        </div>
        <p className="mt-1 text-xs text-muted-foreground">{counts}</p>
      </summary>
      <div className="mt-3 grid gap-3 border-t pt-3 text-xs text-muted-foreground sm:grid-cols-2">
        <TokenList label="Gates" items={row.gates} />
        <TokenList label="Evidence" items={row.evidence} />
        <TokenList label="Roles" items={row.roles} />
        <TokenList label="Topics" items={row.topics} />
        <div className="sm:col-span-2"><span className="font-medium text-foreground/80">Identifier</span><p className="mt-1 font-mono">{row.id}</p></div>
      </div>
    </details>
  )
}

function countLabel(count: number, noun: string) {
  return `${count} ${noun}${count === 1 || noun === "evidence" ? "" : "s"}`
}

function TokenList({ label, items }: { label: string; items: string[] }) {
  return <div><p className="font-medium text-foreground/80">{label}</p><div className="mt-1 flex flex-wrap gap-1.5">{items.length ? items.map((item) => <span key={item} className="rounded-md border px-1.5 py-0.5 font-mono text-[11px]">{item}</span>) : <span>None recorded.</span>}</div></div>
}
