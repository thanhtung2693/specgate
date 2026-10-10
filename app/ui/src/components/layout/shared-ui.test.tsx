import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { MarkdownText } from "./shared-ui"

const mermaid = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn(async () => ({ svg: "<svg><text>Preserved layout</text></svg>" })),
}))
vi.mock("mermaid", () => ({ default: mermaid }))
afterEach(cleanup)

it("keeps strict rendering and classic diagram layout across Mermaid upgrades", async () => {
  render(<MarkdownText content={"```mermaid\nflowchart LR\n A --> B\n```"} />)
  await waitFor(() => expect(mermaid.initialize).toHaveBeenCalledWith({
    startOnLoad: false,
    securityLevel: "strict",
    theme: "default",
    layout: "dagre",
    look: "classic",
  }))
  expect(await screen.findByText("Preserved layout")).toBeInTheDocument()
})
