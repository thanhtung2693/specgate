import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanupAppTest, defaultRegistryResponse, emptyRegistryResponse, fixtureURL, getLangGraphClientMock, renderApp, renderGovernanceAgent, sessionStorageKey, setupAppTest } from "./app-test-support"

describe("SpecGate UI shell: navigation and agent", () => {
  beforeEach(setupAppTest)
  afterEach(cleanupAppTest)
  const langGraphClientMock = getLangGraphClientMock()

  it("requires Full mode when the Doc Registry is not configured", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
    localStorage.clear()

    renderApp("/work")

    expect(await screen.findByRole("heading", { name: "Web UI requires Full mode" })).toBeInTheDocument()
    expect(screen.getByText("specgate init --mode full")).toBeInTheDocument()
    expect(screen.getByText(/Local mode stays in the CLI and IDE/)).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Set up attribution" })).not.toBeInTheDocument()
    expect(localStorage.getItem(sessionStorageKey)).toBeNull()
  })

  it("renders Work as the primary app surface", async () => {
    renderApp()

    expect(screen.getByRole("heading", { name: "Work" })).toBeInTheDocument()
    expect(screen.getByText("Governance Board")).toBeInTheDocument()
    expect(screen.getByText("Work queue")).toBeInTheDocument()
    expect(screen.getByRole("table", { name: "Work queue" })).toBeInTheDocument()
    expect(screen.getByLabelText("Search work")).toBeInTheDocument()
    expect(screen.getByText("Action queue")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Board" })).toHaveAttribute("aria-pressed", "false")
    expect(screen.getByRole("button", { name: "List" })).toHaveAttribute("aria-pressed", "true")
    expect(screen.getByText("SpecGate Core")).toBeInTheDocument()
  })

  it("requires an active workspace before opening the LangGraph governance agent", async () => {
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "http://agents.test")
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(JSON.stringify({ configured: true }), {
      headers: { "Content-Type": "application/json" },
    }))))

    renderGovernanceAgent()

    expect(await screen.findByRole("heading", { name: "Select a workspace" })).toBeInTheDocument()
    expect(screen.getByText("Choose a workspace before starting a governed conversation.")).toBeInTheDocument()
    expect(screen.queryByPlaceholderText("Ask about artifacts, readiness results, or Governance Knowledge. Type / for prompts")).not.toBeInTheDocument()
    expect(langGraphClientMock.search).not.toHaveBeenCalled()
  })

  it("switches work views and filters the list queue", async () => {
    renderApp("/work")
    const user = userEvent.setup()

    expect(screen.getByText("Work queue")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Board" }))
    expect(screen.getByText("Lifecycle lanes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "List" }))

    await user.type(screen.getByLabelText("Search work"), "verification")

    expect(screen.getAllByText("Pre-release verification sweep").length).toBeGreaterThan(0)
    expect(screen.queryByText("Doc Registry migration cleanup")).not.toBeInTheDocument()

    await user.clear(screen.getByLabelText("Search work"))
    await user.click(screen.getByRole("button", { name: /^Blocked\d+$/ }))

    expect(screen.getAllByText("Pre-release verification sweep").length).toBeGreaterThan(0)
    expect(screen.getByText("Delivery evidence trust stamp")).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: /Ready for pickup/ }))
    expect(screen.queryByRole("button", { name: /Gate failures/ })).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: /Action queue/ }))
    expect(screen.getByRole("button", { name: /Reason: All reasons/ })).toBeInTheDocument()
    await user.click(screen.getByRole("link", { name: "Agent skills setup primitives" }))

    expect(screen.getAllByRole("heading", { name: "Agent skills setup primitives" }).length).toBeGreaterThan(0)
  })

  it("fetches governance stats when the disclosure expands and links ledger rows to verification", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/api/v1/stats")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
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
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("button", { name: "Governance stats · last 30 days" }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/api/v1/stats?workspace_id=workspace-main&days=30",
        expect.any(Object),
      ),
    )
    const statsRegion = screen.getByRole("region", { name: "Governance stats" })
    expect(await within(statsRegion).findByText("80%")).toBeInTheDocument()
    expect(within(statsRegion).getByText("First-pass yield")).toBeInTheDocument()
    const ledgerLink = within(statsRegion).getByRole("link", { name: "SG-142" })
    expect(ledgerLink).toHaveAttribute("href", "/work/SG-142?tab=verification")
    expect(within(statsRegion).getByText("Gate signals (pre-build)")).toBeInTheDocument()
    expect(within(statsRegion).getByText("Review signals (post-build)")).toBeInTheDocument()
    expect(within(statsRegion).getByText("Signal ledger")).toBeInTheDocument()
    expect(within(statsRegion).getByText("Gate signal")).toBeInTheDocument()
    expect(within(statsRegion).queryByText("Gate Catch")).not.toBeInTheDocument()
    expect(within(statsRegion).getByText("Acceptance criteria are missing measurable outcomes.")).toBeInTheDocument()
  })

  it("shows the not-enough-data line instead of percentages when nothing was reviewed", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/api/v1/stats")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({ window_days: 30, reviewed_items: 0, first_pass: 0, ledger: [] }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("button", { name: "Governance stats · last 30 days" }))

    expect(await screen.findByText("Not enough data yet — run a few governed work items first.")).toBeInTheDocument()
    const statsRegion = screen.getByRole("region", { name: "Governance stats" })
    expect(within(statsRegion).queryByText(/%/)).not.toBeInTheDocument()
    expect(within(statsRegion).queryByText("First-pass yield")).not.toBeInTheDocument()
  })

  it("does not fetch governance stats before the disclosure is expanded", async () => {
    const fetchMock = vi.fn(defaultRegistryResponse)
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")

    expect(await screen.findByRole("button", { name: "Governance stats · last 30 days" })).toBeInTheDocument()
    expect(await screen.findByText("Work queue")).toBeInTheDocument()
    expect(fetchMock.mock.calls.filter(([input]) => String(input).includes("/api/v1/stats"))).toHaveLength(0)
  })

  it("keeps work creation outside the UI workspace", () => {
    renderApp("/work")

    expect(screen.queryByRole("button", { name: "Create work" })).not.toBeInTheDocument()
    expect(screen.queryByRole("dialog", { name: "Create work" })).not.toBeInTheDocument()
  })

  it("exposes the primary navigation structure", async () => {
    renderApp("/reviews")

    expect(screen.getByRole("link", { name: /^Work$/ })).toHaveAttribute("href", "/work")
    expect(screen.getByRole("link", { name: /^Reviews$/ })).toHaveAttribute("href", "/reviews")
    expect(screen.getByRole("link", { name: /^Artifacts$/ })).toHaveAttribute("href", "/artifacts")
    expect(screen.getByRole("button", { name: "Settings" })).toBeInTheDocument()
    expect(await screen.findByText(/items need review/)).toBeInTheDocument()
    expect(screen.getByText("Delivery evidence")).toBeInTheDocument()
    expect(screen.getByLabelText("Search reviews")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /Needs changes/ })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /Gate failed/ })).toBeInTheDocument()
    expect(
      screen.getAllByRole("link", { name: /Inspect review/ }).some((link) => {
        const href = link.getAttribute("href") ?? ""
        return href.startsWith("/work/") && href.includes("tab=verification")
      }),
    ).toBe(true)
    expect(screen.queryByText("Feedback signals")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Ask about review gaps|Ask review summary/ })).not.toBeInTheDocument()
    expect(screen.queryByRole("link", { name: /Skills/ })).not.toBeInTheDocument()
  })

  it("opens the work item Verification tab from the review queue", async () => {
    renderApp("/reviews")
    const user = userEvent.setup()

    const keyLink = await screen.findByRole("link", { name: "SG-155" })
    expect(keyLink).toHaveAttribute("href", "/work/SG-155?tab=verification")

    await user.click(keyLink)

    expect((await screen.findAllByRole("heading", { name: "Doc Registry migration cleanup" })).length).toBeGreaterThan(0)
    expect(screen.getByRole("tab", { name: "Verification", selected: true })).toBeInTheDocument()
  })

  it("orders Reviews as artifact decisions, then delivery evidence", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input))
      if (url.pathname === "/artifacts" && url.searchParams.get("status") === "draft") {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "artifact-draft-review", feature_name: "Draft review", version: "v1", status: "draft", updated_at: "2026-07-11T01:00:00Z" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.pathname === "/artifacts" && url.searchParams.get("status") === "needs_changes") {
        return emptyRegistryResponse(input)
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/reviews?artifact=artifact-draft-review")

    const headings = (await screen.findAllByRole("heading", { level: 2 })).map((heading) => heading.textContent)
    const ordered = ["Artifact decisions", "Delivery evidence"]
    expect(ordered.map((label) => headings.indexOf(label))).toEqual([expect.any(Number), expect.any(Number)])
    expect(headings.indexOf(ordered[0])).toBeLessThan(headings.indexOf(ordered[1]))
    expect(await screen.findByRole("dialog", { name: "Draft review" })).toBeInTheDocument()
    expect(screen.queryByText("Feedback signals")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Ask about review gaps|Ask review summary/ })).not.toBeInTheDocument()
  })

  it("hides governance chat when the chat model has no key", async () => {
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "http://agents.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith("/api/v1/workspaces")) {
        return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url === "http://agents.test/governance/chat/health") {
        return Promise.resolve(
          new Response(JSON.stringify({ configured: false, provider: "openai", model: "gpt-5.4-mini" }), {
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work")

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "http://agents.test/governance/chat/health",
      expect.any(Object),
    ))
    expect(screen.queryByRole("button", { name: "Open governance agent" })).not.toBeInTheDocument()
    expect(screen.queryByText(/GOVERNANCE_OPS_API_KEY/)).not.toBeInTheDocument()
  })

  it("hides governance chat when health is unreachable", async () => {
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "http://agents.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === "http://agents.test/governance/chat/health") {
        return Promise.reject(new TypeError("network unavailable"))
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      "http://agents.test/governance/chat/health",
      expect.any(Object),
    ))
    expect(screen.queryByRole("button", { name: "Open governance agent" })).not.toBeInTheDocument()
  })

  it("shows only honest chat prompts and no durable-history controls", async () => {
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "http://agents.test")
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === "http://agents.test/governance/chat/health") {
        return Promise.resolve(new Response(JSON.stringify({ configured: true }), {
          headers: { "Content-Type": "application/json" },
        }))
      }
      return defaultRegistryResponse(input, init)
    }))
    renderApp("/work")
    const user = userEvent.setup()

    const trigger = await screen.findByRole("button", { name: "Open governance agent" })
    await user.click(trigger)

    expect(await screen.findByText("Artifacts, stored readiness, and Governance Knowledge.")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "View agent threads" })).not.toBeInTheDocument()
    const composer = screen.getByPlaceholderText("Ask about artifacts, readiness results, or Governance Knowledge. Type / for prompts")
    await user.type(composer, "/")
    expect(await screen.findByText("Artifact summary")).toBeInTheDocument()
    expect(screen.getByText("Readiness results")).toBeInTheDocument()
    expect(screen.getByText("Knowledge search")).toBeInTheDocument()
    expect(screen.queryByText("Prepare handoff")).not.toBeInTheDocument()
    expect(screen.queryByText("Evidence summary")).not.toBeInTheDocument()
  })

  it("hides review filters when there is nothing to review", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(
          new Response(JSON.stringify({ items: [] }), {
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/reviews")

    expect(await screen.findByText("No delivery evidence needs review.")).toBeInTheDocument()
    expect(screen.queryByLabelText("Search reviews")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "All" })).not.toBeInTheDocument()
  })

  it("does not present unavailable review data as an empty queue", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(new Response("registry unavailable", { status: 503 }))
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/reviews")

    expect(await screen.findByText("Delivery evidence unavailable")).toBeInTheDocument()
    expect(screen.getByText(/Live review data is unavailable/)).toBeInTheDocument()
    expect(screen.queryByText("No delivery evidence needs review.")).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Search reviews")).not.toBeInTheDocument()
  })

  it("filters the review queue by review reason", async () => {
    renderApp("/reviews")
    const user = userEvent.setup()

    await user.click(screen.getByRole("button", { name: /Needs changes/ }))

    expect(screen.getByLabelText("2 visible items")).toBeInTheDocument()
    expect(screen.getByText("visible items")).toBeInTheDocument()
    expect(screen.getByText("Pre-release verification sweep")).toBeInTheDocument()
    expect(screen.getByText("Doc Registry migration cleanup")).toBeInTheDocument()

    await user.type(screen.getByLabelText("Search reviews"), "zzzz")

    expect(screen.getByText("No reviews in this view")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Clear filters" }))
    expect(screen.getByText("Doc Registry migration cleanup")).toBeInTheDocument()
  })

  it("opens workspace actions from the sidebar user block", async () => {
    renderApp("/work")
    const user = userEvent.setup()

    expect(screen.queryByRole("button", { name: /SpecGate Core/ })).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Open workspace menu" }))

    expect(screen.queryByRole("menuitem", { name: "Change name" })).not.toBeInTheDocument()
    // Workspace switching appears only when the appliance reports more than one
    // workspace. Logout and client-only identity editing are not UI actions.
    expect(screen.queryByRole("menuitem", { name: "Change workspace" })).not.toBeInTheDocument()
    expect(screen.queryByRole("menuitem", { name: "Logout" })).not.toBeInTheDocument()
  })

  it("does not show local workspace fallback choices when registry workspaces are unavailable", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/workspaces") {
        return Promise.resolve(new Response("registry unavailable", { status: 503 }))
      }
      if (url === "http://registry.test/workboard/change-requests") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")
    const user = userEvent.setup()

    await user.click(screen.getByRole("button", { name: "Open workspace menu" }))

    // With registry workspaces unavailable there is at most the profile's own
    // workspace, so no bundled fallback choices and no switcher entry at all.
    expect(screen.queryByRole("menuitem", { name: "Change workspace" })).not.toBeInTheDocument()
    expect(screen.queryByText("SpecGate Sandbox")).not.toBeInTheDocument()
    expect(screen.queryByText("SpecGate Docs")).not.toBeInTheDocument()
  })

  it("returns to explicit setup when the saved workspace no longer exists", async () => {
    localStorage.setItem(
      sessionStorageKey,
      JSON.stringify({
        profile: {
          id: "d747-stale-workspace",
          slug: "specgate-core",
          name: "SpecGate Core",
          user: {
            id: "stale-user",
            username: "thanhtung",
            name: "Tung Local",
            email: "tung@example.com",
          },
        },
      }),
    )
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/workspaces") {
        return Promise.resolve(
          new Response(JSON.stringify({ items: [{ id: "workspace-other", slug: "other", name: "Other" }] })),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/knowledge")

    expect(await screen.findByRole("heading", { name: "Set up attribution" })).toBeInTheDocument()
    expect(screen.getByRole("alert")).toHaveTextContent("Saved workspace is no longer available")
    expect(fetchMock).not.toHaveBeenCalledWith(
      "http://registry.test/api/v1/identity/bootstrap",
      expect.any(Object),
    )
    expect(localStorage.getItem(sessionStorageKey)).toBeNull()
  })

  it("keeps setup open when a configured Doc Registry is unavailable", async () => {
    localStorage.clear()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/workspaces" || url.endsWith("/api/v1/identity/bootstrap")) {
        return Promise.resolve(new Response("registry unavailable", { status: 503 }))
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")
    const user = userEvent.setup()

    expect(await screen.findByRole("heading", { name: "Set up attribution" })).toBeInTheDocument()
    await user.type(screen.getByLabelText("Display name"), "Offline Dev")
    await user.type(screen.getByLabelText("Username"), "offline")
    await user.click(screen.getByRole("button", { name: "Continue" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("Doc Registry is unavailable")
    expect(screen.getByRole("alert")).toHaveTextContent("specgate doctor")
    expect(screen.getByRole("heading", { name: "Set up attribution" })).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Work" })).not.toBeInTheDocument()
    expect(localStorage.getItem(sessionStorageKey)).toBeNull()
  })

  it("bootstraps submitted attribution through Doc Registry using the first existing workspace", async () => {
    localStorage.clear()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/workspaces") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "ws-core", slug: "core", name: "SpecGate Core" },
                { id: "ws-docs", slug: "docs", name: "SpecGate Docs" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/api/v1/identity/bootstrap" && init?.method === "POST") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              user: {
                id: "user-dev",
                username: "dev",
                display_name: "Dev",
              },
              workspace: { id: "ws-core", slug: "core", name: "SpecGate Core" },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/workboard/change-requests?workspace_id=ws-core") {
        return emptyRegistryResponse(input)
      }
      return Promise.resolve(new Response("{}", { status: 404 }))
    })
    vi.stubGlobal("fetch", fetchMock)

    renderApp("/work")

    const user = userEvent.setup()
    expect(await screen.findByRole("heading", { name: "Set up attribution" })).toBeInTheDocument()
    await user.type(screen.getByLabelText("Display name"), "Dev")
    await user.type(screen.getByLabelText("Username"), "dev")
    await waitFor(() => expect(screen.getByLabelText("Workspace name")).toHaveValue("SpecGate Core"))
    await user.click(screen.getByRole("button", { name: "Continue" }))
    await screen.findByRole("heading", { name: "Work" })
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/api/v1/identity/bootstrap",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            workspace_name: "SpecGate Core",
            display_name: "Dev",
            username: "dev",
          }),
        }),
      )
    })
    await waitFor(() => {
      expect(screen.getAllByText("SpecGate Core").length).toBeGreaterThan(0)
    })
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/workboard/change-requests?workspace_id=ws-core",
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      )
    })
  })
})
