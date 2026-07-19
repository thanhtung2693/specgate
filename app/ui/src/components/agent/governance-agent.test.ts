import { describe, expect, it } from "vitest"
import type { MessageState } from "@assistant-ui/react"

import { hasVisibleMessageContent } from "@/components/agent/governance-agent"

function message(partial: Partial<MessageState>) {
  return partial as MessageState
}

describe("governance agent transcript", () => {
  it("does not render assistant reasoning-only chunks as blank transcript rows", () => {
    expect(
      hasVisibleMessageContent(
        message({
          role: "assistant",
          content: [{ type: "reasoning", text: "thinking" }],
        }),
      ),
    ).toBe(false)
  })

  it("renders assistant text once content arrives", () => {
    expect(
      hasVisibleMessageContent(
        message({
          role: "assistant",
          content: [{ type: "text", text: "Hello!" }],
        }),
      ),
    ).toBe(true)
  })
})
