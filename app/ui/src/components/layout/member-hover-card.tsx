import { type ReactNode } from "react"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card"
import { type WorkspaceMember } from "@/data/identity"
import { cn } from "@/lib/utils"
import { toneClass, userInitials } from "./shared"

export function WorkspaceMemberAvatar({
  member,
  size = "default",
}: {
  member: WorkspaceMember
  size?: "default" | "lg"
}) {
  return (
    <Avatar size={size} className="rounded-lg">
      <AvatarFallback className="rounded-lg bg-card font-semibold text-foreground">
        {userInitials(member.displayName || member.username)}
      </AvatarFallback>
    </Avatar>
  )
}

function WorkspaceMemberDetails({ member }: { member: WorkspaceMember }) {
  return (
    <div className="grid gap-4">
      <div className="flex items-start gap-3">
        <WorkspaceMemberAvatar member={member} size="lg" />
        <div className="grid min-w-0 gap-1">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h4 className="truncate text-sm font-semibold">{member.displayName}</h4>
            {member.current ? (
              <Badge variant="outline" className={cn("border text-[0.65rem]", toneClass("success"))}>
                You
              </Badge>
            ) : null}
          </div>
          <p className="truncate font-mono text-xs text-muted-foreground">@{member.username}</p>
          {member.email ? <p className="truncate text-xs text-muted-foreground">{member.email}</p> : null}
        </div>
      </div>
      <dl className="grid grid-cols-[88px_minmax(0,1fr)] gap-x-3 gap-y-2 text-xs">
        <dt className="text-muted-foreground">Role</dt>
        <dd className="font-medium">{member.role}</dd>
        <dt className="text-muted-foreground">Source</dt>
        <dd className="font-medium">Workspace member</dd>
      </dl>
    </div>
  )
}

export function WorkspaceMemberHoverCard({
  member,
  children,
}: {
  member: WorkspaceMember
  children: ReactNode
}) {
  return (
    <HoverCard openDelay={150} closeDelay={80}>
      <HoverCardTrigger asChild>{children}</HoverCardTrigger>
      <HoverCardContent
        align="start"
        sideOffset={8}
        role="tooltip"
        aria-label={`${member.displayName} member details`}
        className="w-[min(20rem,calc(100vw-2rem))] p-4"
      >
        <WorkspaceMemberDetails member={member} />
      </HoverCardContent>
    </HoverCard>
  )
}
