import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanupAppTest, defaultRegistryResponse, emptyRegistryResponse, fixtureURL, renderApp, setupAppTest } from "./app-test-support"

describe("SpecGate UI shell: artifact library", () => {
  beforeEach(setupAppTest)
  afterEach(cleanupAppTest)

  it("renders artifact list and selected artifact detail", async () => {
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect(screen.getByRole("heading", { name: "Artifacts" })).toBeInTheDocument()
    expect(screen.getByText("Artifact library")).toBeInTheDocument()
    expect(screen.getByLabelText("Search artifacts")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Current" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "All statuses" })).toBeInTheDocument()
    expect(await screen.findByRole("button", { name: "Approved" })).toBeInTheDocument()
    expect((await screen.findAllByText("Doc Registry migration cleanup")).length).toBeGreaterThan(0)
    expect(screen.queryByText("Documents")).not.toBeInTheDocument()
    expect(screen.queryByRole("dialog", { name: "Document inspector" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Preview spec" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Download summary" })).not.toBeInTheDocument()

    await user.click(await screen.findByRole("button", { name: /Doc Registry migration cleanup/ }))

    const detailDialog = await screen.findByRole("dialog", { name: "Doc Registry migration cleanup" })
    const detailScrollArea = detailDialog.querySelector("[data-slot='scroll-area']")
    expect(detailDialog).toHaveClass("grid")
    expect(detailScrollArea).toHaveClass("min-h-0", "overflow-hidden")
    expect(within(detailDialog).getByText("Documents")).toBeInTheDocument()
    expect(within(detailDialog).getAllByText("migration/spec.md").length).toBeGreaterThan(0)
    expect(within(detailDialog).getByText("Feedback")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Attachments")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Migration rollout note")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Completion evidence is ready for delivery review.")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Expected gates")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Readiness history")).toBeInTheDocument()
    expect(within(detailDialog).getAllByText("Spec Completeness").length).toBeGreaterThan(0)
    expect(within(detailDialog).getAllByText("missing constraints").length).toBeGreaterThan(0)
    expect(within(detailDialog).getByText("Audit events")).toBeInTheDocument()
    expect(within(detailDialog).getByText("Artifact Published")).toBeInTheDocument()
    expect(within(detailDialog).queryByRole("link", { name: "Open work" })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: "Ask agent" })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Refresh readiness/i })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Rerun/i })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: /Save revision/i })).not.toBeInTheDocument()

    await user.click(within(detailDialog).getByRole("button", { name: /migration\/spec\.md/ }))

    const previewDialog = await screen.findByRole("dialog", { name: "Document inspector" })
    expect(within(previewDialog).getByRole("button", { name: "View" })).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Diff" })).toBeInTheDocument()
    expect(within(previewDialog).getByLabelText("Version")).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Markdown" })).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Code" })).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Copy" })).toBeInTheDocument()
    expect(await within(previewDialog).findByText("Mermaid diagram")).toBeInTheDocument()
    expect(await within(previewDialog).findByLabelText("Mermaid diagram viewport")).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Source" })).toBeInTheDocument()
    expect(within(previewDialog).queryByText("sample")).not.toBeInTheDocument()
    await user.click(within(previewDialog).getByRole("button", { name: "Code" }))
    expect(within(previewDialog).getByText(/```mermaid/)).toBeInTheDocument()
    await user.click(within(previewDialog).getByRole("button", { name: "Diff" }))
    expect(await within(previewDialog).findByText("Line diff")).toBeInTheDocument()
    expect(within(previewDialog).queryByLabelText("Compare")).not.toBeInTheDocument()
    expect(within(previewDialog).getByText("Latest v0.1")).toBeInTheDocument()
    expect(within(previewDialog).getByText(/noisy expected misses/)).toBeInTheDocument()
    await user.click(within(previewDialog).getByRole("button", { name: "Close" }))
    await user.click(within(detailDialog).getByRole("button", { name: "Close" }))

    await user.click(screen.getByRole("button", { name: /Agent skills setup primitives/ }))

    const skillsDialog = await screen.findByRole("dialog", { name: "Agent skills setup primitives" })
    expect(within(skillsDialog).getAllByText("setup/spec.md").length).toBeGreaterThan(0)
  })

  it("hides superseded artifacts by default and reveals them with All statuses", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-SG-155",
                  feature_id: "SG-155",
                  feature_name: "Doc Registry migration cleanup",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-27T05:20:00Z",
                },
                {
                  id: "artifact-superseded-pipeline",
                  feature_id: "SG-140",
                  feature_name: "Previous summary pipeline",
                  version: "v0.3",
                  status: "superseded",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-20T05:20:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Doc Registry migration cleanup")).length).toBeGreaterThan(0)
    expect(screen.queryByRole("button", { name: /Previous summary pipeline/ })).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "All statuses" }))

    expect(await screen.findByRole("button", { name: /Previous summary pipeline/ })).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Current" }))

    expect(screen.queryByRole("button", { name: /Previous summary pipeline/ })).not.toBeInTheDocument()
  })

  it("filters artifacts by search and status", async () => {
    renderApp("/artifacts")
    const user = userEvent.setup()

    await user.type(screen.getByLabelText("Search artifacts"), "skills")

    expect(screen.getAllByText("Agent skills setup primitives").length).toBeGreaterThan(0)
    expect(screen.queryByRole("button", { name: /Doc Registry migration cleanup/ })).not.toBeInTheDocument()

    await user.clear(screen.getByLabelText("Search artifacts"))
    await user.type(screen.getByLabelText("Search artifacts"), "missing artifact")

    expect(screen.getByText("No artifacts match")).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Clear filters" }))
    await user.click(screen.getByRole("button", { name: "Approved" }))

    expect(screen.getAllByText("Doc Registry migration cleanup").length).toBeGreaterThan(0)
  })

  it("restores the selected artifact from the URL query", async () => {
    renderApp("/artifacts?artifact=artifact-SG-151")

    const detailDialog = await screen.findByRole("dialog", { name: "Agent skills setup primitives" })
    expect(within(detailDialog).queryByRole("link", { name: "Open work" })).not.toBeInTheDocument()
    expect((await within(detailDialog).findAllByText("setup/spec.md")).length).toBeGreaterThan(0)
  })

  it("restores the selected artifact from the CLI review URL", async () => {
    renderApp("/artifacts/artifact-SG-151")

    const detailDialog = await screen.findByRole("dialog", { name: "Agent skills setup primitives" })
    expect(within(detailDialog).queryByRole("link", { name: "Open work" })).not.toBeInTheDocument()
    expect((await within(detailDialog).findAllByText("setup/spec.md")).length).toBeGreaterThan(0)
  })

  it("keeps document inspection while omitting the duplicate artifact agent action", async () => {
    renderApp("/artifacts")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("button", { name: /Doc Registry migration cleanup/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Doc Registry migration cleanup" })
    await user.click(within(detailDialog).getByRole("button", { name: /migration\/tasks\.md/ }))
    const previewDialog = await screen.findByRole("dialog", { name: "Document inspector" })
    expect(await within(previewDialog).findByText("Doc Registry migration cleanup Tasks")).toBeInTheDocument()
    expect(within(previewDialog).queryByText("No previewable Markdown content available.")).not.toBeInTheDocument()
    await user.click(within(previewDialog).getByRole("button", { name: "Close" }))

    expect(within(detailDialog).queryByRole("button", { name: "Ask agent" })).not.toBeInTheDocument()
  })

  it("shows an empty artifact document without treating storage as unavailable", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-empty-body",
                  feature_id: "SG-201",
                  feature_name: "Unavailable live document",
                  version: "v0.2",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-empty-body/files")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ path: "live/spec.md", role: "spec", size_bytes: 2048 }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts?feature_id=SG-201&limit=100")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-empty-body",
                  feature_id: "SG-201",
                  feature_name: "Unavailable live document",
                  version: "v0.2",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T10:00:00Z",
                },
                {
                  id: "artifact-empty-body-previous",
                  feature_id: "SG-201",
                  feature_name: "Unavailable live document",
                  version: "v0.1",
                  status: "superseded",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-28T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-empty-body/files/_?path=live%2Fspec.md")) {
        return Promise.resolve(
          new Response(JSON.stringify({ content: "", size_bytes: 0 }), {
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Unavailable live document")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Unavailable live document/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Unavailable live document" })
    await user.click(await within(detailDialog).findByRole("button", { name: /live\/spec\.md/ }))

    const previewDialog = await screen.findByRole("dialog", { name: "Document inspector" })
    expect(within(previewDialog).queryByText(/stored content is unavailable/)).not.toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Copy" })).toBeDisabled()
    expect(within(previewDialog).getByRole("button", { name: "Diff" })).toBeDisabled()
    expect(within(previewDialog).queryByText("sample")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows a live no-fallback error when artifact version history is unavailable", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-version-history-error",
                  feature_id: "SG-206",
                  feature_name: "Version history outage",
                  version: "v0.2",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "medium",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T15:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-version-history-error/files")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{ path: "live/spec.md", role: "spec", size_bytes: 2048 }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts?feature_id=SG-206&limit=100")) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "registry unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      if (url.endsWith("/artifacts/artifact-version-history-error/files/_?path=live%2Fspec.md")) {
        return Promise.resolve(
          new Response(JSON.stringify({ content: "# Live Spec\n\nReal registry body." }), {
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/artifacts")
    const user = userEvent.setup()

    expect((await screen.findAllByText("Version history outage")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Version history outage/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Version history outage" })
    await user.click(await within(detailDialog).findByRole("button", { name: /live\/spec\.md/ }))

    const previewDialog = await screen.findByRole("dialog", { name: "Document inspector" })
    expect(await within(previewDialog).findByText("Live Spec")).toBeInTheDocument()
    expect(await within(previewDialog).findByText(/Version history unavailable/)).toBeInTheDocument()
    expect(within(previewDialog).getByText(/no fallback version comparison is shown/)).toBeInTheDocument()
    expect(within(previewDialog).getByRole("button", { name: "Diff" })).toBeDisabled()
    expect(within(previewDialog).queryByText(/Latest v0\.2/)).not.toBeInTheDocument()
    expect(within(previewDialog).queryByText("sample")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows a live no-fallback error when artifact documents are unavailable", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-documents-error",
                  feature_id: "SG-205",
                  feature_name: "Document list outage",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T14:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/artifacts/artifact-documents-error/files")) {
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

    expect((await screen.findAllByText("Document list outage")).length).toBeGreaterThan(0)
    await user.click(screen.getByRole("button", { name: /Document list outage/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Document list outage" })

    expect(await within(detailDialog).findByText(/Artifact documents unavailable/)).toBeInTheDocument()
    expect(within(detailDialog).getByText(/no fallback document list is shown/)).toBeInTheDocument()
    expect(within(detailDialog).queryByText("No documents are available for this artifact yet.")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not synthesize live artifact rows without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  feature_id: "SG-MISSING-ID",
                  feature_name: "Missing id artifact",
                  version: "v0.1",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "low",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-29T14:30:00Z",
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

    expect(await screen.findByText("No artifacts in this workspace")).toBeInTheDocument()
    expect(screen.getByText("specgate artifact publish --help")).toBeInTheDocument()
    expect(screen.queryByLabelText("Search artifacts")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Current" })).not.toBeInTheDocument()
    expect(screen.queryByText("Missing id artifact")).not.toBeInTheDocument()
    expect(screen.queryByText("artifact-1")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/artifacts/artifact-1/files", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not present an unavailable artifact library as empty", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(new Response("registry unavailable", { status: 503 }))
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/artifacts")

    expect(await screen.findByText("Artifact library unavailable")).toBeInTheDocument()
    expect(screen.getByText(/Check Doc Registry connectivity/)).toBeInTheDocument()
    expect(screen.queryByText("No artifacts in this workspace")).not.toBeInTheDocument()
    expect(screen.queryByText("specgate artifact publish --help")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Search artifacts")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("keeps the artifact library read-only and routes decisions to Reviews", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-reviewable",
                  feature_id: "SG-REVIEW",
                  feature_name: "Reviewable artifact",
                  version: "v0.1",
                  status: "draft",
                  request_type: "change_request",
                  impact_level: "medium",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-07-11T00:00:00Z",
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

    expect(await screen.findByRole("button", { name: /Reviewable artifact/ })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Features" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Current" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "All statuses" })).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: /Reviewable artifact/ }))
    const detailDialog = await screen.findByRole("dialog", { name: "Reviewable artifact" })

    expect(within(detailDialog).queryByRole("button", { name: "Approve" })).not.toBeInTheDocument()
    expect(within(detailDialog).queryByRole("button", { name: "Request changes" })).not.toBeInTheDocument()
    expect(within(detailDialog).getByRole("link", { name: "Open in Reviews" })).toHaveAttribute(
      "href",
      "/reviews?artifact=artifact-reviewable",
    )
  })

  it("shows feature lineage inside artifact detail", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/artifacts?limit=50")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "artifact-feature-canonical",
                  feature_id: "SG-301",
                  feature_name: "Feature console",
                  version: "v0.3",
                  status: "approved",
                  request_type: "change_request",
                  impact_level: "medium",
                  artifact_completeness: "full",
                  source_kind: "context_pack",
                  updated_at: "2026-06-30T12:00:00Z",
                },
                {
                  id: "artifact-feature-previous",
                  feature_id: "SG-301",
                  feature_name: "Feature console previous",
                  version: "v0.2",
                  status: "superseded",
                  request_type: "change_request",
                  impact_level: "medium",
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
      if (url.endsWith("/artifacts?feature_id=SG-301&limit=100")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "artifact-feature-canonical", feature_id: "SG-301", feature_name: "Feature console", version: "v0.3", status: "approved", request_type: "change_request", impact_level: "medium", artifact_completeness: "full", source_kind: "context_pack", updated_at: "2026-06-30T12:00:00Z" },
                { id: "artifact-feature-previous", feature_id: "SG-301", feature_name: "Feature console previous", version: "v0.2", status: "superseded", request_type: "change_request", impact_level: "medium", artifact_completeness: "full", source_kind: "context_pack", updated_at: "2026-06-29T12:00:00Z" },
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

    await user.click(await screen.findByRole("button", { name: /Feature console/ }))
    const artifactDialog = await screen.findByRole("dialog", { name: "Feature console" })

    expect(within(artifactDialog).getByRole("heading", { name: "Feature lineage" })).toBeInTheDocument()
    expect(await within(artifactDialog).findByText("artifact-feature-previous")).toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })
})
