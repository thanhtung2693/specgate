import { afterEach, describe, expect, it, vi } from "vitest"

import { loadGovernanceCatalog } from "@/data/governance"

describe("governance data adapter", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("does not synthesize live outcome feedback without registry ids", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/governance-profiles")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/api/v1/policies/levels")) {
        return Promise.resolve(new Response(JSON.stringify({ levels: [] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/api/v1/policy-health")) {
        return Promise.resolve(new Response(JSON.stringify({ policies: [] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/api/v1/outcome-feedback")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  work_item_id: "CR-MISSING-ID",
                  type: "override",
                  reason: "No durable signal id",
                },
                {
                  id: "outcome-missing-work",
                  type: "escaped_defect",
                  reason: "No work item reference",
                },
                {
                  id: "outcome-1",
                  work_item_id: "CR-123",
                  type: "rejected_evidence",
                  reason: "Evidence did not match the criterion.",
                },
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

    expect(catalog.outcomeFeedback).toEqual([
      expect.objectContaining({
        id: "outcome-1",
        workItemId: "CR-123",
        type: "rejected_evidence",
      }),
    ])
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
      if (url.endsWith("/api/v1/policy-health")) {
        return Promise.resolve(new Response(JSON.stringify({ policies: [] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/api/v1/outcome-feedback")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } }))
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
