import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanupAppTest, defaultRegistryResponse, emptyRegistryResponse, fixtureURL, renderApp, setupAppTest } from "./app-test-support"

describe("SpecGate UI shell: artifact evidence", () => {
  beforeEach(setupAppTest)
  afterEach(cleanupAppTest)

  it("does not synthesize live artifact document rows without registry paths", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-missing-path",
                  feature_id: "SG-MISSING-PATH",
                  feature_name: "Missing path artifact",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T14:45:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-missing-path/files")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ role: "spec", size_bytes: 128, updated_at: "2026-06-29T14:46:00Z" }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Missing path artifact")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Missing path artifact/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Missing path artifact" })

    expect(await within(detailDialog).findByText("No documents are available for this artifact yet.")).toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /document-1\.md/ })).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith(
      "http://registry.test/artifacts/artifact-missing-path/files/_?path=document-1.md",
      expect.any(Object),
    )

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows a live no-fallback error when artifact gate preview is unavailable", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-gate-preview-error",
                  feature_id: "SG-202",
                  feature_name: "Gate preview outage",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T11:00:00Z",
                  expected_gates: ["scope_clear"],
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/artifacts/artifact-gate-preview-error/gate-preview")) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "gate preview unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Gate preview outage")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Gate preview outage/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Gate preview outage" })

    expect(await within(detailDialog).findByText(/Gate preview unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/no fallback gate snapshot is shown/)).toBeInTheDocument()
    expect(within(detailDialog).queryByText("Scope Clear")).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/api/v1/artifacts/artifact-gate-preview-error/gate-preview?workspace_id=workspace-main",
      expect.any(Object),
    )

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows live no-fallback errors for artifact detail evidence sections", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-evidence-errors",
                  feature_id: "SG-203",
                  feature_name: "Artifact evidence outage",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T12:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-evidence-errors/files")) {
        return emptyRegistryResponse(input)
      }
      if (
        url.endsWith("/features/SG-203/attachments") ||
        url.endsWith("/governance/feedback-events?artifact_id=artifact-evidence-errors&limit=20") ||
        url.endsWith("/artifacts/artifact-evidence-errors/readiness-runs?limit=20") ||
        url.endsWith("/events?artifact_id=artifact-evidence-errors&limit=20")
      ) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "registry unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Artifact evidence outage")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Artifact evidence outage/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Artifact evidence outage" })

    expect(await within(detailDialog).findByText(/Reference attachments unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/Artifact feedback unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/Readiness history unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/Audit events unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).queryByText("No reference attachments pinned to this feature.")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("No artifact-linked feedback recorded.")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("No persisted readiness runs recorded.")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("No artifact audit events recorded.")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not synthesize live artifact feedback rows without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-feedback-missing-id",
                  feature_id: "SG-205",
                  feature_name: "Artifact feedback missing id",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T12:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/governance/feedback-events?artifact_id=artifact-feedback-missing-id&limit=20")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  event_type: "delivery.comment_scope_drift",
                  status: "received",
                  reason: "A PR comment asks for work outside the approved artifact.",
                  created_at: "2026-06-30T03:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Artifact feedback missing id")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Artifact feedback missing id/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Artifact feedback missing id" })

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/governance/feedback-events?artifact_id=artifact-feedback-missing-id&limit=20&workspace_id=workspace-main",
        expect.any(Object),
      ),
    )
    expect(within(detailDialog).getByText("No artifact-linked feedback recorded.")).toBeInTheDocument()
    expect(within(detailDialog).queryByText("delivery comment scope drift")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("A PR comment asks for work outside the approved artifact.")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not synthesize live artifact evidence rows without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-evidence-missing-ids",
                  feature_id: "SG-206",
                  feature_name: "Artifact evidence missing ids",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T12:40:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/features/SG-206/attachments")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ title: "Attachment without id", kind: "link", audience: "gate", created_at: "2026-06-30T03:00:00Z" }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-evidence-missing-ids/readiness-runs?limit=20")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ gate: "spec_completeness", state: "fail", hint: "Missing constraints", created_at: "2026-06-30T03:01:00Z" }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/events?artifact_id=artifact-evidence-missing-ids&limit=20")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ event_type: "artifact.approved", payload: { status: "approved" }, created_at: "2026-06-30T03:02:00Z" }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Artifact evidence missing ids")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Artifact evidence missing ids/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Artifact evidence missing ids" })

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/artifacts/artifact-evidence-missing-ids/readiness-runs?limit=20&workspace_id=workspace-main",
        expect.any(Object),
      ),
    )
    expect(within(detailDialog).getByText("No reference attachments pinned to this feature.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("No persisted readiness runs recorded.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("No artifact audit events recorded.")).toBeInTheDocument()
    expect(within(detailDialog).queryByText("Attachment without id")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("Missing constraints")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("Artifact Published")).not.toBeInTheDocument()
    expect(within(detailDialog).queryByText("revision-1")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows live no-fallback errors for artifact feature and policy readback", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-policy-errors",
                  feature_id: "SG-204",
                  feature_name: "Artifact policy outage",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T13:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/workboard/features") || url.endsWith("/api/v1/artifacts/artifact-policy-errors/policy")) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "registry unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Artifact policy outage")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Artifact policy outage/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Artifact policy outage" })

    expect(await within(detailDialog).findByText(/Feature context unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/Policy snapshot unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/no fallback policy explanation is shown/)).toBeInTheDocument()
    expect(within(detailDialog).queryByText("No policy explanation recorded for this artifact.")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("collapses long artifact document lists", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-many",
                  feature_id: "SG-200",
                  feature_name: "Large artifact bundle",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "now",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-many/files")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: Array.from({ length: 8 }, (_, index) => ({
                path: `doc-${index + 1}.md`,
                role: "reference",
                size_bytes: 100 + index,
              })),
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-many/readiness-runs?limit=20")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "run-live-1",
                  gate: "acceptance_criteria_verifiable",
                  state: "pass",
                  hint: "acceptance criteria are checkable",
                  evidence_json: "Each criterion names an observable outcome.",
                  created_at: "2026-06-28T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/artifacts/artifact-many/gate-preview")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              artifact_id: "artifact-many",
              preview_tasks: [
                {
                  gate_key: "scope_clear",
                  gate_version: "v1",
                  executor: "ide_agent",
                },
                {
                  gate_key: "acceptance_criteria_verifiable",
                  gate_version: "v1",
                  executor: "ide_agent",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/events?artifact_id=artifact-many&limit=20")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "event-live-1",
                  artifact_id: "artifact-many",
                  event_type: "artifact.approved",
                  payload: { version: "v0.1", status: "approved" },
                  created_at: "2026-06-28T12:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/artifacts/artifact-many/policy")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              governance_level: "standard",
              title: "Standard governance",
              summary: "Context Pack handoff, evidence, and delivery review are required.",
              reasons: ["Persisted artifact snapshot"],
              obligations: ["Keep implementation inside the approved artifact scope."],
              policy_lineage: [
                {
                  key: "builtin/standard",
                  version: "1",
                  digest: "sha256:artifactpolicyabcdef",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/workspaces")) {
        return defaultRegistryResponse(input)
      }
      return Promise.resolve(new Response(JSON.stringify({ content: "# Document" }), { headers: { "Content-Type": "application/json" } }))
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Large artifact bundle")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Large artifact bundle/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Large artifact bundle" })
    expect(await within(detailDialog).findByRole("button", { name: /doc-3\.md/ })).toBeInTheDocument()
    expect(await within(detailDialog).findByText("Readiness history")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Each run records what the checker read and why it decided.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Scope Clear")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Checks the change is bounded with explicit non-goals. Reads the spec.")).toBeInTheDocument()
    expect(within(detailDialog).queryByText(/preview\s*[-—]\s*not persisted/)).not.toBeInTheDocument()
    expect(within(detailDialog).getByText("Expected by this artifact's policy. Not run yet.")).toBeInTheDocument()
    expect(within(detailDialog).getAllByText("ide_agent").length).toBeGreaterThan(0)
    expect(within(detailDialog).getAllByText("Acceptance Criteria Verifiable").length).toBeGreaterThan(0)
    expect(within(detailDialog).getByText("Latest persisted readiness run.")).toBeInTheDocument()
    expect(within(detailDialog).getAllByText("acceptance criteria are checkable").length).toBeGreaterThan(0)
    expect(within(detailDialog).queryByText("Each criterion names an observable outcome.")).not.toBeInTheDocument()
    await user.click(within(detailDialog).getByRole("button", { name: "Why" }))
    expect(within(detailDialog).getByText("Each criterion names an observable outcome.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Audit events")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Artifact Approved")).toBeInTheDocument()
    expect(within(detailDialog).getByText("version: v0.1 / status: approved")).toBeInTheDocument()
    expect(within(detailDialog).getAllByText("Governance policy").length).toBeGreaterThan(0)
    expect(within(detailDialog).getByText("Standard governance")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Context Pack handoff, evidence, and delivery review are required.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Persisted artifact snapshot")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Keep implementation inside the approved artifact scope.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("builtin/standard")).toBeInTheDocument()
    expect(within(detailDialog).getByRole("button", { name: /doc-4\.md/ })).toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /doc-5\.md/ })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Refresh readiness/i })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Apply revision/i })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Accept exception|Resolve policy|Switch policy/i })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/artifacts/artifact-many/readiness-runs?limit=20&workspace_id=workspace-main",
      expect.any(Object),
    )
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/api/v1/artifacts/artifact-many/gate-preview?workspace_id=workspace-main",
      expect.any(Object),
    )
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/events?artifact_id=artifact-many&limit=20&workspace_id=workspace-main",
      expect.any(Object),
    )
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/api/v1/artifacts/artifact-many/policy?workspace_id=workspace-main",
      expect.any(Object),
    )

    await user.click(within(detailDialog).getByRole("button", { name: "Show 4 more documents" }))

    expect(within(detailDialog).getByRole("button", { name: /doc-8\.md/ })).toBeInTheDocument()
    expect(within(detailDialog).getByRole("button", { name: "Show fewer documents" })).toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })
})
