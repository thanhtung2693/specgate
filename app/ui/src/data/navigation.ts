import {
  BoxesIcon,
  LibraryIcon,
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
    description: "What needs your decision, what is being built, and what is waiting.",
    icon: SquareKanbanIcon,
  },
  {
    id: "reviews",
    label: "Reviews",
    path: "/reviews",
    description: "Work waiting for your approval or your acceptance.",
    icon: GaugeIcon,
  },
  {
    id: "artifacts",
    label: "Artifacts",
    path: "/artifacts",
    description: "Every approved version of what you asked for.",
    icon: BoxesIcon,
  },
  {
    id: "knowledge",
    label: "Knowledge",
    path: "/knowledge",
    description: "Reference material the agent can read while it works.",
    icon: LibraryIcon,
  },
]
