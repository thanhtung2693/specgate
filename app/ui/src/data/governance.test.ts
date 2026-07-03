import { afterEach, describe, expect, it, vi } from "vitest"

import { loadGovernanceCatalog } from "@/data/governance"

describe("governance data adapter", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("does not synthesize live profiles or policy levels without registry keys", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/governance-profiles")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { display_name: "Missing profile key", change_type: "feature" },
                { namespace: "specgate", key: "generic_change", display_name: "Generic change" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/policies/levels")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              levels: [
                { display_name: "Missing level key", approval_policy: "none" },
                { governance_level: "standard", display_name: "Standard" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return Promise.resolve(new Response("{}", { status: 404 }))
    })
    vi.stubGlobal("fetch", fetchMock)

    const catalog = await loadGovernanceCatalog("http://registry.test")

    expect(catalog.profiles).toEqual([
      expect.objectContaining({
        id: "specgate/generic_change",
        displayName: "Generic change",
      }),
    ])
    expect(catalog.policyLevels).toEqual([
      expect.objectContaining({
        level: "standard",
        displayName: "Standard",
      }),
    ])
  })
})
