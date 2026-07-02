import { afterEach, describe, expect, it, vi } from "vitest"

import { buildStatsPath, fetchGovernanceStats, mapGovernanceStats } from "@/data/stats"

describe("governance stats data adapter", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("builds a fixed 30-day stats path scoped to the selected workspace", () => {
    expect(buildStatsPath("workspace-main")).toBe("/api/v1/stats?workspace_id=workspace-main&days=30")
    expect(buildStatsPath("  ")).toBe("/api/v1/stats?days=30")
    expect(buildStatsPath()).toBe("/api/v1/stats?days=30")
  })

  it("maps the registry stats payload and drops incomplete ledger rows", () => {
    const summary = mapGovernanceStats({
      window_days: 30,
      reviewed_items: 5,
      first_pass: 4,
      gate_catches_pre_build: 3,
      review_catches_post_build: 2,
      review_catches_fixed: 1,
      rework: 2,
      items_with_rework: 1,
      ambiguity_blocks: 1,
      cycle_time_avg_hours: 6.4,
      cycle_time_items: 4,
      ledger: [
        {
          occurred_at: "2026-06-27T05:00:00Z",
          change_request_key: "SG-142",
          kind: "gate_catch",
          gate: "spec_completeness",
          detail: "Acceptance criteria are missing measurable outcomes.",
        },
        { occurred_at: "2026-06-27T04:00:00Z", kind: "review_catch" },
        { change_request_key: "SG-136", kind: "ambiguity_block" },
      ],
    })

    expect(summary).toMatchObject({
      windowDays: 30,
      reviewedItems: 5,
      firstPass: 4,
      gateCatchesPreBuild: 3,
      reviewCatchesPostBuild: 2,
      reviewCatchesFixed: 1,
      rework: 2,
      itemsWithRework: 1,
      ambiguityBlocks: 1,
      cycleTimeAvgHours: 6.4,
      cycleTimeItems: 4,
    })
    expect(summary.ledger).toEqual([
      {
        occurredAt: "2026-06-27T05:00:00Z",
        changeRequestKey: "SG-142",
        kind: "gate_catch",
        gate: "spec_completeness",
        detail: "Acceptance criteria are missing measurable outcomes.",
      },
    ])
  })

  it("fetches governance stats from Doc Registry", async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ window_days: 30, reviewed_items: 2, first_pass: 2, ledger: [] }), {
          headers: { "Content-Type": "application/json" },
        }),
      ),
    )
    vi.stubGlobal("fetch", fetchMock)

    const view = await fetchGovernanceStats("http://registry.test/", "ws-1")

    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/stats?workspace_id=ws-1&days=30", expect.any(Object))
    expect(view.status).toBe("ready")
    expect(view.item).toMatchObject({ reviewedItems: 2, firstPass: 2, ledger: [] })
  })

  it("throws on non-ok stats responses", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("unavailable", { status: 503 }))))

    await expect(fetchGovernanceStats("http://registry.test")).rejects.toThrow("stats request failed: 503")
  })
})
