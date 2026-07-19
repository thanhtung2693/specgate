import { afterEach, describe, expect, it, vi } from "vitest"

import { loadArtifactDecisionQueue } from "@/data/reviews"
import { isReviewItem } from "@/components/layout/reviews"
import type { WorkItem } from "@/data/workspace"

afterEach(() => vi.unstubAllGlobals())

describe("reviews data adapter", () => {
  it("keeps delivery evidence awaiting human acceptance in the review queue", () => {
    const items = [
      { key: "CR-READY", lifecycle: "Ready", delivery: "passed", deliveryVerdict: "pass", gate: "pass" },
      { key: "CR-GAPS", lifecycle: "Ready", delivery: "needs_changes", deliveryVerdict: "needs_changes", gate: "pass" },
      { key: "CR-ACCEPTED", lifecycle: "Delivered", delivery: "accepted", deliveryVerdict: "pass", gate: "pass" },
    ] as WorkItem[]

    expect(items.filter(isReviewItem).map((item) => item.key)).toEqual(["CR-READY", "CR-GAPS"])
  })

  it("merges draft and needs-changes artifacts newest first without duplicates", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const status = new URL(String(input)).searchParams.get("status")
      const items = status === "draft"
        ? [
            { id: "older", version: "v1", status: "draft", updated_at: "2026-07-01T00:00:00Z" },
            { id: "shared", version: "v2", status: "draft", updated_at: "2026-07-03T00:00:00Z" },
          ]
        : [
            { id: "newer", version: "v3", status: "needs_changes", updated_at: "2026-07-04T00:00:00Z" },
            { id: "shared", version: "v2", status: "needs_changes", updated_at: "2026-07-03T00:00:00Z" },
          ]
      return Promise.resolve(new Response(JSON.stringify({ items }), { headers: { "Content-Type": "application/json" } }))
    })
    vi.stubGlobal("fetch", fetchMock)

    const queue = await loadArtifactDecisionQueue("http://registry.test", "ws-core", new AbortController().signal)

    expect(fetchMock.mock.calls.map(([input]) => new URL(String(input)).searchParams.get("status"))).toEqual([
      "draft",
      "needs_changes",
    ])
    expect(queue.items.map((item) => item.id)).toEqual(["newer", "shared", "older"])
  })
})
