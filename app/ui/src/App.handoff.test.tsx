import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanupAppTest, emptyRegistryResponse, fixtureURL, renderApp, setupAppTest } from "./app-test-support"

describe("SpecGate UI shell: handoff", () => {
  beforeEach(setupAppTest)
  afterEach(cleanupAppTest)

  it("supports Context Pack copy and markdown download from work detail", async () => {
    let downloadedBlob: Blob | undefined
    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockImplementation((blob) => {
      if (blob instanceof Blob) downloadedBlob = blob
      return "blob:context-pack"
    })
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined)
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined)
    renderApp("/work/SG-155")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Handoff" }))
    expect(screen.getAllByRole("button", { name: "Copy resume command" })).toHaveLength(1)
    await user.click(screen.getByRole("button", { name: "Copy handoff" }))
    expect(await screen.findByRole("button", { name: "Handoff copied" })).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Download .md" }))
    expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob))
    expect(click).toHaveBeenCalled()
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:context-pack")
    await expect(downloadedBlob?.text()).resolves.toContain("specgate work context SG-155")

    createObjectURL.mockRestore()
    revokeObjectURL.mockRestore()
    click.mockRestore()
  })

  it("hands a Ready Context Pack to one selected Linear team and reads back the linked issue", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let handedOff = false
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core" }] })))
      if (url.endsWith("/workboard/change-requests")) return Promise.resolve(new Response(JSON.stringify({ items: [{
        id: "cr-linear", key: "CR-LINEAR", phase: "Ready", work_type: "cleanup", title: "Linear handoff", intent_md: "Hand off an approved Context Pack.",
        created_by: "DX", created_at: "2026-07-21T09:00:00Z", updated_at: "2026-07-21T09:00:00Z",
      }] })))
      if (url.endsWith("/api/v1/work-items/cr-linear/context-pack")) return Promise.resolve(new Response(JSON.stringify({ state: "assembled", markdown: "# Ready handoff" })))
      if (url.endsWith("/workboard/change-requests/cr-linear/tracker-links")) return Promise.resolve(new Response(JSON.stringify({ items: handedOff ? [{ identifier: "ENG-42", url: "https://linear.app/acme/issue/ENG-42", state: "opened" }] : [] })))
      if (url.endsWith("/integrations")) return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "int-linear", provider: "linear", name: "Linear", status: "connected", has_oauth_token: true }] })))
      if (url.endsWith("/integrations/int-linear/resources")) return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "team-platform", resource_type: "team", external_key: "platform", display_name: "Platform" }] })))
      if (url.endsWith("/workboard/change-requests/cr-linear/linear-handoff") && init?.method === "POST") {
        handedOff = true
        return Promise.resolve(new Response(JSON.stringify({ created: true, link: { identifier: "ENG-42", url: "https://linear.app/acme/issue/ENG-42", state: "opened" } })))
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-LINEAR")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Handoff" }))
    expect(await screen.findByRole("button", { name: "Copy handoff" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Hand off to Linear" })).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => fixtureURL(input).endsWith("/integrations"))).toBe(false)

    await user.click(screen.getByRole("button", { name: "Hand off to Linear" }))
    const dialog = await screen.findByRole("dialog", { name: "Hand off to Linear" })
    expect(await within(dialog).findByText("Platform")).toBeInTheDocument()
    await user.click(within(dialog).getByRole("button", { name: "Hand off to Linear" }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/workboard/change-requests/cr-linear/linear-handoff?workspace_id=workspace-main",
      expect.objectContaining({ method: "POST" }),
    ))
    expect(await screen.findByText("ENG-42")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Hand off to Linear" })).not.toBeInTheDocument()
  })

  it("renders actionable open, exact, and stale repository observations", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core" }] })))
      if (url.endsWith("/workboard/change-requests")) return Promise.resolve(new Response(JSON.stringify({ items: [{
        id: "cr-repository", key: "CR-REPOSITORY", phase: "Review", work_type: "cleanup", title: "Repository observations", intent_md: "Read persisted delivery links.",
        created_by: "DX", created_at: "2026-07-21T09:00:00Z", updated_at: "2026-07-21T09:00:00Z",
      }] })))
      if (url.endsWith("/api/v1/work-items/cr-repository/delivery-status?detail=true")) return Promise.resolve(new Response(JSON.stringify({ found: true, verdict: "pass", git_receipt: { head_revision: "submitted-head" } })))
      if (url.endsWith("/workboard/change-requests/cr-repository/delivery-links")) return Promise.resolve(new Response(JSON.stringify({ items: [
        { external_key: "#12", title: "Exact", url: "https://github.test/pull/12", state: "merged", head_sha: "submitted-head", merge_commit_sha: "merge-12", updated_at: "2026-07-21T09:00:00Z" },
        { external_key: "#13", title: "Stale", url: "https://github.test/pull/13", state: "merged", head_sha: "old-head", merge_commit_sha: "merge-13", updated_at: "2026-07-21T09:00:00Z" },
        { external_key: "#14", title: "Open", url: "https://github.test/pull/14", state: "opened", head_sha: "submitted-head", updated_at: "2026-07-21T09:00:00Z" },
      ] })))
      return emptyRegistryResponse(input)
    }))
    renderApp("/work/CR-REPOSITORY")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Verification" }))
    expect(await screen.findByText("Submitted commit observed on merged PR/MR.")).toBeInTheDocument()
    expect(screen.getByText("PR/MR merged, but it does not match the latest submitted commit. Update or resubmit, then merge the matching head.")).toBeInTheDocument()
    expect(screen.getByText("PR/MR linked; merge it before repository observation can corroborate delivery.")).toBeInTheDocument()
  })

  it("previews and copies the registry-derived Context Pack in work detail", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let downloadedBlob: Blob | undefined
    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockImplementation((blob) => {
      if (blob instanceof Blob) downloadedBlob = blob
      return "blob:canonical-context-pack"
    })
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined)
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined)
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/workboard/change-requests")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-live-pack",
                  key: "CR-LIVE-PACK",
                  feature_id: "feature-pack",
                  work_type: "cleanup",
                  title: "Registry-derived Context Pack preview",
                  intent_md: "Show the exact handoff material that CLI and IDE agents read.",
                  created_by: "DX",
                  created_at: "2026-06-27T05:00:00Z",
                  updated_at: "2026-06-27T05:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/work-items/cr-live-pack/context-pack")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              state: "assembled",
              markdown:
                "# Registry-derived Context Pack\n\n## Execution Brief\n\nUse the registry-derived pack, not a browser summary.\n\n## Resume\n\n```bash\nspecgate work context cr-live-pack\n```",
              change_request_id: "cr-live-pack",
              feature_id: "feature-pack",
              source_artifact_id: "artifact-pack",
              warnings: [{ code: "linked_knowledge_newer", message: "Linked knowledge changed after handoff." }],
              knowledge_provenance: [{ document_id: "knowledge-1", title: "Domain terms", version: "v1.0" }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-LIVE-PACK")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Handoff" }))

    expect(await screen.findByText("Registry-derived Context Pack")).toBeInTheDocument()
    expect(screen.getByText("Use the registry-derived pack, not a browser summary.")).toBeInTheDocument()
    expect(screen.getByText("Linked knowledge changed after handoff.")).toBeInTheDocument()
    expect(screen.getByText("Domain terms")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Copy handoff" }))
    expect(await screen.findByRole("button", { name: "Handoff copied" })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Download .md" }))
    await expect(downloadedBlob?.text()).resolves.toContain("# Registry-derived Context Pack")
    await expect(downloadedBlob?.text()).resolves.toContain("specgate work context cr-live-pack")
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/work-items/cr-live-pack/context-pack?workspace_id=workspace-main", expect.any(Object))

    createObjectURL.mockRestore()
    revokeObjectURL.mockRestore()
    click.mockRestore()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("disables live handoff markdown actions when Context Pack readback fails", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/workboard/change-requests")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-live-pack-error",
                  key: "CR-LIVE-PACK-ERROR",
                  feature_id: "feature-pack-error",
                  work_type: "cleanup",
                  title: "Context Pack outage",
                  intent_md: "Do not hand off browser fallback markdown when registry readback fails.",
                  created_by: "DX",
                  created_at: "2026-06-27T06:00:00Z",
                  updated_at: "2026-06-27T06:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/work-items/cr-live-pack-error/context-pack")) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: "context pack unavailable" }), {
            status: 500,
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-LIVE-PACK-ERROR")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Handoff" }))

    expect(await screen.findByText(/^Context Pack unavailable\./)).toBeInTheDocument()
    expect(screen.getByText(/no fallback handoff markdown is copied or downloaded/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Copy handoff" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Download .md" })).toBeDisabled()
    expect(screen.queryByText("Context Pack preview")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("disables live handoff markdown actions when Context Pack readback has no markdown", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.endsWith("/workboard/change-requests")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-live-pack-pending",
                  key: "CR-LIVE-PACK-PENDING",
                  feature_id: "feature-pack-pending",
                  work_type: "cleanup",
                  title: "Context Pack unavailable",
                  intent_md: "Do not hand off browser fallback markdown when Context Pack readback has no markdown.",
                  created_by: "DX",
                  created_at: "2026-06-27T07:00:00Z",
                  updated_at: "2026-06-27T07:30:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/work-items/cr-live-pack-pending/context-pack")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              state: "pending",
              markdown: "",
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-LIVE-PACK-PENDING")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Handoff" }))

    expect(await screen.findByText(/^Context Pack unavailable\./)).toBeInTheDocument()
    expect(screen.getByText(/no fallback handoff markdown is copied or downloaded/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Copy handoff" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Download .md" })).toBeDisabled()
    expect(screen.queryByText("Context Pack preview")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })
})
