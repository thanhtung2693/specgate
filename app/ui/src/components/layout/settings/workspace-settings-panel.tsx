// Workspace settings panel: read-only cooperative identity visibility.

import { useEffect, useMemo, useState } from "react"
import { RotateCwIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { listWorkspaceMembers, type IdentityWorkspace, type WorkspaceMember } from "@/data/identity"
import { docRegistryBase } from "@/data/model-settings"
import { cn } from "@/lib/utils"
import { WorkspaceMemberAvatar, WorkspaceMemberHoverCard } from "../member-hover-card"
import { toneClass, type WorkspaceProfile } from "../shared"

function resolveWorkspace(profile: WorkspaceProfile, options: IdentityWorkspace[]): IdentityWorkspace | null {
  const live =
    options.find((choice) => profile.id && choice.id === profile.id) ??
    options.find((choice) => profile.slug && choice.slug === profile.slug) ??
    options.find((choice) => choice.name === profile.name)
  if (live) return live
  if (!profile.id) return null
  return {
    id: profile.id,
    slug: profile.slug ?? profile.id,
    name: profile.name,
  }
}

function MemberRow({ member }: { member: WorkspaceMember }) {
  return (
    <li>
      <WorkspaceMemberHoverCard member={member}>
        <div
          tabIndex={0}
          className="flex w-full min-w-0 items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/35 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        >
          <WorkspaceMemberAvatar member={member} />
          <span className="grid min-w-0 flex-1">
            <span className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="truncate text-sm font-medium">{member.displayName}</span>
              {member.current ? (
                <Badge variant="outline" className={cn("border text-[0.65rem]", toneClass("success"))}>
                  You
                </Badge>
              ) : null}
            </span>
            <span className="truncate text-xs text-muted-foreground">
              @{member.username}{member.email ? ` · ${member.email}` : ""}
            </span>
          </span>
          <Badge variant="secondary" className="shrink-0 text-[0.65rem]">
            {member.role}
          </Badge>
        </div>
      </WorkspaceMemberHoverCard>
    </li>
  )
}

export function WorkspaceSettingsPanel({
  profile,
  workspaceOptions,
}: {
  profile: WorkspaceProfile
  workspaceOptions: IdentityWorkspace[]
}) {
  const base = useMemo(() => docRegistryBase(), [])
  const workspace = useMemo(() => resolveWorkspace(profile, workspaceOptions), [profile, workspaceOptions])
  const [members, setMembers] = useState<WorkspaceMember[]>([])
  const [status, setStatus] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const [refreshToken, setRefreshToken] = useState(0)

  useEffect(() => {
    if (!base || !workspace) {
      setMembers([])
      setStatus("idle")
      return
    }

    const controller = new AbortController()
    setStatus("loading")
    void listWorkspaceMembers(
      base,
      workspace.id,
      { userId: profile.user.id, username: profile.user.username },
      controller.signal,
    )
      .then((items) => {
        setMembers(items)
        setStatus("ready")
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return
        setStatus("error")
      })

    return () => controller.abort()
  }, [base, profile.user.id, profile.user.username, refreshToken, workspace])

  return (
    <section className="grid gap-5">
      <div>
        <h2 className="text-sm font-semibold">Workspace</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Read-only workspace identity for audit attribution and cooperative visibility.
        </p>
      </div>
      <section className="grid gap-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold">Team members</h3>
            <p className="mt-1 text-xs text-muted-foreground">
              Members come from Doc Registry identity bootstrap records; access control remains outside this alpha UI.
            </p>
          </div>
          {workspace ? (
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="border font-mono text-[0.65rem]">{workspace.slug}</Badge>
              <Button variant="outline" size="sm" className="rounded-md" disabled={status === "loading"} onClick={() => setRefreshToken((value) => value + 1)}>
                <RotateCwIcon data-icon="inline-start" /> Refresh
              </Button>
            </div>
          ) : null}
        </div>
        {!base ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">
            Configure VITE_DOC_REGISTRY_URL to view workspace members.
          </p>
        ) : !workspace ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">
            Select a registry workspace to view members.
          </p>
        ) : status === "loading" ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">Loading workspace members...</p>
        ) : status === "error" ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">Workspace members unavailable.</p>
        ) : members.length === 0 ? (
          <p className="rounded-md border bg-background/70 p-3 text-sm text-muted-foreground">No team members found.</p>
        ) : (
          <ul className="divide-y rounded-lg border bg-background/70">
            {members.map((member) => (
              <MemberRow key={member.userId} member={member} />
            ))}
          </ul>
        )}
      </section>
    </section>
  )
}
