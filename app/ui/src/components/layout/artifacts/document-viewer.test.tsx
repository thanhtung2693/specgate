import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { DocumentDiffView } from "./document-viewer"

describe("artifact line diff budget", () => {
  it("declines a balanced comparison before allocating the quadratic matrix", () => {
    render(<DocumentDiffView before={"old\n".repeat(600)} after={"new\n".repeat(600)} />)
    expect(screen.getByRole("status")).toHaveTextContent("too large")
  })

  it("declines skewed and escaped-newline documents without rendering every line", () => {
    render(<DocumentDiffView before="old" after={"new\\n".repeat(5000)} />)
    expect(screen.getByRole("status")).toHaveTextContent("too large")
    expect(screen.queryByText("Line diff")).not.toBeInTheDocument()
  })

  it("retains the exact small-document additions and removals", () => {
    render(<DocumentDiffView before={"keep\\nold"} after={"keep\\nnew"} />)
    expect(screen.getByText("keep")).toBeInTheDocument()
    expect(screen.getByText("old").previousElementSibling).toHaveTextContent("-")
    expect(screen.getByText("new").previousElementSibling).toHaveTextContent("+")
    expect(screen.queryByRole("status")).not.toBeInTheDocument()
  })
})
