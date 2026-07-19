import { afterEach, describe, expect, it, vi } from "vitest"

import { fetchArtifactsByStatus, mapArtifact, mapArtifactFeature, mapArtifactGatePreview } from "@/data/artifacts"

afterEach(() => vi.unstubAllGlobals())

describe("artifact data adapter", () => {
  it("fetches a complete artifact decision status slice", async () => {
    const controller = new AbortController()
    const fetchMock = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
      Promise.resolve(
        new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } }),
      ),
    )
    vi.stubGlobal("fetch", fetchMock)

    const fetchScopedArtifactsByStatus = fetchArtifactsByStatus as unknown as (
      baseUrl: string,
      status: "draft" | "needs_changes",
      signal: AbortSignal,
      workspaceId: string,
    ) => Promise<unknown>
    await fetchScopedArtifactsByStatus("http://registry.test/", "needs_changes", controller.signal, "ws-core")

    const [rawUrl, init] = fetchMock.mock.calls[0]!
    const url = new URL(String(rawUrl))
    expect(`${url.origin}${url.pathname}`).toBe("http://registry.test/artifacts")
    expect(Object.fromEntries(url.searchParams)).toEqual({
      limit: "200",
      status: "needs_changes",
      workspace_id: "ws-core",
    })
    expect(init?.signal && "aborted" in init.signal ? init.signal.aborted : undefined).toBe(false)
    controller.abort()
    expect(init?.signal && "aborted" in init.signal ? init.signal.aborted : undefined).toBe(true)
  })

  it("does not synthesize live linked features without registry ids", () => {
    expect(mapArtifactFeature({ id: "feature-1", key: "SG-1", name: "Checkout" })).toMatchObject({
      id: "feature-1",
      key: "SG-1",
      name: "Checkout",
    })
    expect(mapArtifactFeature({ name: "Missing feature reference" })).toBeNull()
  })

  it("keeps featureless artifacts standalone", () => {
    expect(
      mapArtifact({
        id: "artifact-quick",
        version: "v0.1",
        status: "approved",
        request_type: "change_request",
        impact_level: "low",
        updated_at: "2026-07-01T00:00:00Z",
      }),
    ).toMatchObject({
      id: "artifact-quick",
      featureId: undefined,
      featureName: "Standalone artifact",
    })
  })

  it("does not synthesize live gate preview rows without gate keys", () => {
    expect(mapArtifactGatePreview({ gate_key: "delivery_review", gate_version: "v1" })).toMatchObject({
      gateKey: "delivery_review",
      gateVersion: "v1",
    })
    expect(mapArtifactGatePreview({ note: "Missing gate key" })).toBeNull()
  })

  it("supplies run-state language when a gate preview has no note", () => {
    expect(mapArtifactGatePreview({ gate_key: "scope_clear" })?.note).toBe(
      "Expected by this artifact's policy. Not run yet.",
    )
    expect(mapArtifactGatePreview({ gate_key: "scope_clear", note: "Expected gate from artifact snapshot." })?.note).toBe(
      "Expected gate from artifact snapshot.",
    )
  })
})
