import { ChevronRightIcon, CodeIcon } from "lucide-react"
import { useEffect, useId, useMemo, useState, type ReactNode } from "react"
import ReactMarkdown, { type Components } from "react-markdown"
import remarkGfm from "remark-gfm"

import { requestGovernanceAgentPrompt } from "@/components/agent/governance-agent-events"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { GovernancePolicySummary } from "@/data/workboard"
import { cn } from "@/lib/utils"
import { parseGateEvidence, readableKey, stateText, statusTone, toneClass, type GateEvidenceDetails } from "./shared"

export function openGovernanceAgentModal() {
  document.querySelector<HTMLButtonElement>('[aria-label="Open governance agent"]:not([disabled])')?.click()
}

export function runGovernanceAgentPrompt(prompt: string) {
  openGovernanceAgentModal()
  requestGovernanceAgentPrompt(prompt)
}

export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // Fall back below for browser surfaces that deny Clipboard API writes.
  }

  if (typeof document.execCommand !== "function") return false

  const textarea = document.createElement("textarea")
  textarea.value = text
  textarea.setAttribute("readonly", "")
  textarea.style.position = "fixed"
  textarea.style.left = "-9999px"
  document.body.append(textarea)
  textarea.select()
  const copied = document.execCommand("copy")
  textarea.remove()
  return copied
}

export function ActionTooltip({ content, children }: { content: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  return (
    <Tooltip open={open}>
      <TooltipTrigger
        asChild
        onPointerEnter={() => setOpen(true)}
        onPointerLeave={() => setOpen(false)}
        onPointerDown={() => setOpen(false)}
      >
        {children}
      </TooltipTrigger>
      <TooltipContent>{content}</TooltipContent>
    </Tooltip>
  )
}

export function MermaidDiagram({ code }: { code: string }) {
  const id = useId().replaceAll(":", "")
  const [mode, setMode] = useState<"rendered" | "source">("rendered")
  const [svg, setSvg] = useState("")
  const [error, setError] = useState("")

  useEffect(() => {
    let cancelled = false

    if (mode !== "rendered") return

    void import("mermaid")
      .then(async ({ default: mermaid }) => {
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          theme: document.documentElement.classList.contains("dark") ? "dark" : "default",
        })
        const result = await mermaid.render(`specgate-${id}`, code)
        if (!cancelled) {
          setSvg(result.svg)
          setError("")
        }
      })
      .catch((renderError: unknown) => {
        if (!cancelled) {
          setSvg("")
          setError(renderError instanceof Error ? renderError.message : "Unable to render Mermaid diagram.")
        }
      })

    return () => {
      cancelled = true
    }
  }, [code, id, mode])

  return (
    <section className="rounded-lg border bg-background/70">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b p-3">
        <div>
          <h4 className="text-sm font-semibold">Mermaid diagram</h4>
        </div>
        <div className="flex flex-wrap items-center gap-1">
          <Button
            variant={mode === "rendered" ? "secondary" : "ghost"}
            size="sm"
            className="rounded-md"
            onClick={() => setMode("rendered")}
          >
            Render
          </Button>
          <Button variant={mode === "source" ? "secondary" : "ghost"} size="sm" className="rounded-md" onClick={() => setMode("source")}>
            <CodeIcon data-icon="inline-start" />
            Source
          </Button>
        </div>
      </div>
      {mode === "source" ? (
        <pre className="max-h-72 overflow-auto p-4 font-mono text-xs leading-5 text-muted-foreground">{code}</pre>
      ) : (
        <div className="max-h-[min(52vh,520px)] min-h-72 overflow-auto p-4" aria-label="Mermaid diagram viewport">
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
          {!error && svg ? (
            <div
              className="[&_svg]:h-auto [&_svg]:w-full [&_svg]:max-w-none"
              dangerouslySetInnerHTML={{ __html: svg }}
            />
          ) : null}
        </div>
      )}
    </section>
  )
}

export function MarkdownText({ content, compact = false }: { content: string; compact?: boolean }) {
  const components: Components = {
    h1: ({ children }) => (
      <h1 className={compact ? "text-sm font-semibold leading-6 text-foreground" : "text-xl font-semibold leading-8 text-foreground"}>
        {children}
      </h1>
    ),
    h2: ({ children }) => (
      <h2 className={compact ? "text-sm font-semibold leading-6 text-foreground" : "pt-1 text-base font-semibold leading-6 text-foreground"}>
        {children}
      </h2>
    ),
    h3: ({ children }) => (
      <h3 className={compact ? "text-sm font-semibold leading-6 text-foreground" : "pt-1 text-sm font-semibold leading-6 text-foreground"}>
        {children}
      </h3>
    ),
    p: ({ children }) => <p className="font-normal">{children}</p>,
    a: ({ children, href }) => (
      <a href={href} className="text-foreground underline underline-offset-4" target="_blank" rel="noreferrer">
        {children}
      </a>
    ),
    ul: ({ children }) => <ul className="ml-5 list-disc space-y-1">{children}</ul>,
    ol: ({ children }) => <ol className="ml-5 list-decimal space-y-1">{children}</ol>,
    li: ({ children }) => <li className="pl-1">{children}</li>,
    blockquote: ({ children }) => <blockquote className="border-l-2 pl-4 text-muted-foreground">{children}</blockquote>,
    table: ({ children }) => (
      <div className="overflow-x-auto rounded-md border">
        <table className="w-full min-w-[520px] border-collapse text-left text-sm">{children}</table>
      </div>
    ),
    th: ({ children }) => <th className="border-b bg-muted/40 px-3 py-2 font-medium text-foreground">{children}</th>,
    td: ({ children }) => <td className="border-b px-3 py-2 align-top last:border-b">{children}</td>,
    code: ({ children, className }) => {
      const code = String(children).replace(/\n$/, "")
      const language = /language-(\w+)/.exec(className ?? "")?.[1]
      if (language === "mermaid") {
        return <MermaidDiagram code={code} />
      }

      if (className) {
        return (
          <code className={cn("block whitespace-pre-wrap break-words rounded-md bg-muted/55 p-3 font-mono text-xs leading-5 text-foreground", className)}>
            {children}
          </code>
        )
      }

      return <code className="rounded bg-muted/70 px-1.5 py-0.5 font-mono text-[0.86em] text-foreground">{children}</code>
    },
    pre: ({ children }) => <>{children}</>,
  }

  return (
    <div className={cn(
      "grid font-normal text-muted-foreground",
      compact ? "max-w-none gap-1 text-sm leading-6" : "max-w-4xl gap-3 text-[0.93rem] leading-7",
    )}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  )
}

function gateEvidenceOriginLabel(details: GateEvidenceDetails): string | undefined {
  if (details.evaluator === "agent") return "Agent-attested"
  if (details.evaluator === "platform_model") {
    return details.judgeModel ? `Evaluated by platform model (${details.judgeModel})` : "Evaluated by platform model"
  }
  return undefined
}

// "Why" disclosure for a persisted gate/readiness run: who evaluated it, how
// confident, and the evidence the checker recorded. Renders nothing when the
// run carries no parseable evidence — never a raw JSON dump.
export function GateEvidenceWhy({ evidence }: { evidence?: string }) {
  const details = useMemo(() => parseGateEvidence(evidence), [evidence])
  const [open, setOpen] = useState(false)
  if (!details) return null

  const origin = gateEvidenceOriginLabel(details)
  const confidence = details.confidence !== undefined ? `confidence ${details.confidence}` : undefined
  return (
    <div className="mt-2">
      <button
        type="button"
        aria-expanded={open}
        className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        onClick={() => setOpen((current) => !current)}
      >
        <ChevronRightIcon className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        Why
      </button>
      {open ? (
        <div className="mt-2 grid gap-1.5 rounded-md border bg-card/50 p-2.5 text-xs">
          {origin || confidence ? (
            <p className="text-muted-foreground">{[origin, confidence].filter(Boolean).join(" · ")}</p>
          ) : null}
          {details.quote ? (
            <blockquote className="border-l-2 pl-2 leading-5 text-muted-foreground">{details.quote}</blockquote>
          ) : null}
          {details.rows.map((row, index) => (
            <div key={`${row.label}-${index}`} className="flex flex-wrap items-baseline gap-x-1.5 gap-y-0.5">
              <span className="min-w-0 font-medium text-foreground">{row.label}</span>
              {row.state ? (
                <Badge variant="outline" className={cn("h-4 border px-1 text-[10px]", toneClass(statusTone("state", row.state)))}>
                  {stateText(row.state)}
                </Badge>
              ) : null}
              {row.why ? <span className="text-muted-foreground">{row.why}</span> : null}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function shortDigest(value?: string): string {
  if (!value) return ""
  return value.length > 16 ? `${value.slice(0, 12)}...` : value
}

export function PolicyExplanationSection({
  policy,
  status,
  context,
}: {
  policy?: GovernancePolicySummary
  status: "ready" | "loading" | "error"
  context: "work" | "artifact"
}) {
  const title = context === "artifact" ? "Governance policy" : "Policy explanation"

  if (status === "loading") {
    return (
      <section className="rounded-lg border bg-background/70 p-4">
        <h3 className="text-sm font-semibold">{title}</h3>
        <p className="mt-2 text-sm text-muted-foreground">Loading policy explanation...</p>
      </section>
    )
  }

  if (status === "error") {
    return (
      <section className="rounded-lg border bg-background/70 p-4">
        <h3 className="text-sm font-semibold">{title}</h3>
        <p className="mt-2 text-sm text-muted-foreground">
          {context === "artifact"
            ? "Policy snapshot unavailable. Check Doc Registry connectivity; no fallback policy explanation is shown in live mode."
            : "Policy explanation unavailable. Check Doc Registry connectivity; no fallback policy guidance is shown in live mode."}
        </p>
      </section>
    )
  }

  if (!policy) return null

  return (
    <section className="rounded-lg border bg-background/70 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-sm font-semibold">{title}</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {context === "artifact"
              ? "Persisted policy snapshot for this artifact version."
              : "Object-scoped policy guidance for the CLI and IDE-agent delivery loop."}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge variant="outline" className={cn("border", toneClass(statusTone("policy", policy.level)))}>
            {readableKey(policy.level)}
          </Badge>
          <Badge variant="outline" className="rounded-full">
            read-only
          </Badge>
        </div>
      </div>
      <div className="mt-3 rounded-md border bg-card/70 p-3">
        <p className="text-sm font-medium">{policy.title}</p>
        <p className="mt-1 text-sm leading-6 text-muted-foreground">{policy.summary}</p>
      </div>
      {policy.reasons.length > 0 ? (
        <div className="mt-3">
          <h4 className="text-xs font-semibold text-muted-foreground">Reasons</h4>
          <div className="mt-2 flex flex-wrap gap-2">
            {policy.reasons.map((reason) => (
              <Badge key={reason} variant="secondary" className="max-w-full whitespace-normal rounded-md text-left">
                {reason}
              </Badge>
            ))}
          </div>
        </div>
      ) : null}
      {policy.obligations.length > 0 ? (
        <div className="mt-3">
          <h4 className="text-xs font-semibold text-muted-foreground">Obligations</h4>
          <div className="mt-2 grid gap-2">
            {policy.obligations.map((obligation) => (
              <p key={obligation} className="rounded-md border bg-background/70 px-2 py-1.5 text-xs leading-5 text-muted-foreground">
                {obligation}
              </p>
            ))}
          </div>
        </div>
      ) : null}
      {policy.lineage.length > 0 ? (
        <div className="mt-3 border-t pt-3">
          <h4 className="text-xs font-semibold text-muted-foreground">Policy lineage</h4>
          <div className="mt-2 grid gap-1">
            {policy.lineage.slice(0, 4).map((entry) => (
              <div key={`${entry.key}-${entry.version ?? ""}-${entry.digest ?? ""}`} className="grid gap-2 rounded-md px-2 py-1.5 text-xs text-muted-foreground sm:grid-cols-[minmax(0,1fr)_auto_auto]">
                <span className="min-w-0 truncate font-mono text-foreground">{entry.key}</span>
                {entry.version ? <span className="font-mono">{entry.version}</span> : null}
                {entry.digest ? <span className="font-mono">{shortDigest(entry.digest)}</span> : null}
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </section>
  )
}
