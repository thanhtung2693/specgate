import { describe, expect, it } from "vitest"

import { mapArtifact, mapArtifactFeature, mapArtifactGatePreview } from "@/data/artifacts"

describe("artifact data adapter", () => {
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

  it("replaces the registry gate-preview placeholder note with run-state language", () => {
    expect(mapArtifactGatePreview({ gate_key: "scope_clear", note: "preview — not persisted" })?.note).toBe(
      "Expected for this artifact's profile. Not run yet.",
    )
    expect(mapArtifactGatePreview({ gate_key: "scope_clear", note: "preview - not persisted" })?.note).toBe(
      "Expected for this artifact's profile. Not run yet.",
    )
    expect(mapArtifactGatePreview({ gate_key: "scope_clear" })?.note).toBe(
      "Expected for this artifact's profile. Not run yet.",
    )
    expect(mapArtifactGatePreview({ gate_key: "scope_clear", note: "Expected gate from artifact snapshot." })?.note).toBe(
      "Expected gate from artifact snapshot.",
    )
  })
})
