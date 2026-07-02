import {
  BoxesIcon,
  GaugeIcon,
  SquareKanbanIcon,
} from "lucide-react"
import type { ComponentType } from "react"

export type AppSection = {
  id: string
  label: string
  path: string
  description: string
  icon: ComponentType
}

export const sections: AppSection[] = [
  {
    id: "work",
    label: "Work",
    path: "/work",
    description: "Catch up on work items, attention queues, and next actions.",
    icon: SquareKanbanIcon,
  },
  {
    id: "reviews",
    label: "Reviews",
    path: "/reviews",
    description: "Review approvals, delivery verdicts, and gate failures.",
    icon: GaugeIcon,
  },
  {
    id: "artifacts",
    label: "Artifacts",
    path: "/artifacts",
    description: "Inspect versioned PRDs, specs, risks, and task bundles.",
    icon: BoxesIcon,
  },
]
