import { afterEach, describe, expect, it, vi } from "vitest"

import { loadGovernancePolicyLevels } from "@/data/governance"

describe("governance data adapter", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("does not synthesize live policy levels without registry keys", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
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

    const policyLevels = await loadGovernancePolicyLevels("http://registry.test")

    expect(policyLevels).toEqual([
      expect.objectContaining({
        level: "standard",
        displayName: "Standard",
      }),
    ])
  })

  it("reports policy-level request failures", async () => {
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/policies/levels")) {
        return Promise.resolve(new Response("{}", { status: 503 }))
      }
      return Promise.resolve(new Response("{}"))
    }))

    await expect(loadGovernancePolicyLevels("http://registry.test/")).rejects.toThrow("policy levels request failed: 503")
  })
})
