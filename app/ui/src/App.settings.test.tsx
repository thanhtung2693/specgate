import { act, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter, useNavigate } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanupAppTest, defaultRegistryResponse, emptyRegistryResponse, openSettings, renderApp, setupAppTest } from "./app-test-support"
import App from "./App"

describe("SpecGate UI shell: settings", () => {
  beforeEach(setupAppTest)
  afterEach(cleanupAppTest)

  it("moves keyboard users past the persistent navigation without changing routes", async () => {
    renderApp("/reviews")
    const user = userEvent.setup()

    await user.click(screen.getByRole("button", { name: "Skip to main content" }))

    expect(document.activeElement).toHaveAttribute("id", "main-content")
    expect(screen.getByRole("heading", { name: "Reviews" })).toBeInTheDocument()
  })

  it("opens a settings section page when a settings query arrives while Settings is already open", async () => {
    let navigateToGovernanceSettings: (() => void) | undefined
    function DeepLinkProbe() {
      const navigate = useNavigate()
      navigateToGovernanceSettings = () => navigate("/work?settings=governance")
      return null
    }

    render(
      <MemoryRouter initialEntries={["/work"]}>
        <App />
        <DeepLinkProbe />
      </MemoryRouter>,
    )
    const user = userEvent.setup()

    await user.click(screen.getByRole("button", { name: "Settings" }))
    const settingsDialog = await screen.findByRole("dialog", { name: "Settings" })
    expect(within(settingsDialog).getByText("General settings")).toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: "Back to settings sections" })).not.toBeInTheDocument()

    await act(async () => {
      navigateToGovernanceSettings?.()
    })

    expect(within(settingsDialog).getByRole("button", { name: "Back to settings sections" })).toBeInTheDocument()
    expect(await within(settingsDialog).findByText("Team rubric Skills")).toBeInTheDocument()
    expect(await within(settingsDialog).findByText("No team rubric Skills found.")).toBeInTheDocument()
  })

  it("keeps implementation tools and resources out of browser Settings", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/skills?workspace_id=workspace-main") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "skill-readiness",
                  name: "checking-spec-readiness",
                  description: "Review artifacts for minimum executable contract coverage.",
                  prompt: "Check goal, scope, non-goals, acceptance criteria, constraints, risks, and verification.",
                  created_at: "2026-06-20T10:00:00Z",
                  updated_at: "2026-06-27T10:00:00Z",
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

    const { settingsDialog } = await openSettings()

    expect(within(settingsDialog).queryByText("Team rubric Skills")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Agent tool catalog")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Resources")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("specgate://skills")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("search_knowledge")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Execute tool|Run tool|Rotate token/i })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText(/API key/i)).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/skills", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("manages registry team rubric Skills without plugin mutation controls", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/skills?workspace_id=workspace-main") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "skill-readiness",
                  name: "checking-spec-readiness",
                  description: "Review artifacts for minimum executable contract coverage.",
                  prompt: "Check goal, scope, non-goals, acceptance criteria, constraints, risks, and verification.",
                  created_at: "2026-06-20T10:00:00Z",
                  updated_at: "2026-06-27T10:00:00Z",
                },
                {
                  id: "skill-delivery",
                  name: "completing-delivery",
                  description: "Report implementation evidence against approved Context Pack criteria.",
                  prompt: "Collect evidence and submit delivery feedback.",
                  created_at: "2026-06-20T10:00:00Z",
                  updated_at: "2026-06-28T10:00:00Z",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/api/v1/skills/skill-readiness?workspace_id=workspace-main") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              body: {
                id: "skill-readiness",
                name: "checking-spec-readiness",
                description: "Review artifacts for minimum executable contract coverage.",
                prompt: "# Checking spec readiness\n\nCheck goal, scope, non-goals, acceptance criteria, constraints, risks, and verification.",
                created_at: "2026-06-20T10:00:00Z",
                updated_at: "2026-06-27T10:00:00Z",
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/skills" && init?.method === "POST") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              body: {
                id: "skill-created",
                name: "delivery-evidence",
                description: "Use when reviewing delivery evidence.",
                prompt: "# Delivery evidence\n\nCheck changed files, tests, and docs.",
                created_at: "2026-06-30T10:00:00Z",
                updated_at: "2026-06-30T10:00:00Z",
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/skills/skill-readiness?workspace_id=workspace-main" && init?.method === "PUT") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              body: {
                id: "skill-readiness",
                name: "checking-spec-readiness",
                description: "Use when evaluating readiness evidence.",
                prompt: "# Checking spec readiness\n\nCheck approved evidence only.",
                created_at: "2026-06-20T10:00:00Z",
                updated_at: "2026-06-30T10:05:00Z",
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/skills/skill-readiness?workspace_id=workspace-main" && init?.method === "DELETE") {
        return Promise.resolve(new Response(JSON.stringify({ body: { ok: true } }), { headers: { "Content-Type": "application/json" } }))
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    expect(within(settingsDialog).queryByText("Team rubric Skills")).not.toBeInTheDocument()
    await user.click(within(settingsDialog).getByRole("button", { name: "GovernanceSkills and policy tiers" }))

    expect(await within(settingsDialog).findByText("Team rubric Skills")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("checking-spec-readiness")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText(/^Registry$/)).not.toBeInTheDocument()
    const skillDescription = within(settingsDialog).getByText("Review artifacts for minimum executable contract coverage.")
    expect(skillDescription).toHaveClass("break-words")
    expect(within(settingsDialog).getByText("completing-delivery")).toBeInTheDocument()
    expect(within(settingsDialog).getByRole("button", { name: "Add Skill" })).toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Install skill/i })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/skills?workspace_id=workspace-main", expect.any(Object))

    await user.click(within(settingsDialog).getByRole("button", { name: "Add Skill" }))
    const addDialog = await screen.findByRole("dialog", { name: "Add Skill" })
    await user.type(within(addDialog).getByLabelText("Skill name"), "delivery-evidence")
    await user.type(within(addDialog).getByLabelText("Description"), "Use when reviewing delivery evidence.")
    await user.type(within(addDialog).getByLabelText("Prompt"), "# Delivery evidence\n\nCheck changed files, tests, and docs.")
    await user.click(within(addDialog).getByRole("button", { name: "Create Skill" }))
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add Skill" })).not.toBeInTheDocument())
    expect(within(settingsDialog).getByText("delivery-evidence")).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/skills",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          name: "delivery-evidence",
          description: "Use when reviewing delivery evidence.",
          prompt: "# Delivery evidence\n\nCheck changed files, tests, and docs.",
          workspace_id: "workspace-main",
        }),
      }),
    )

    await user.click(within(settingsDialog).getByRole("button", { name: "Manage checking-spec-readiness Skill" }))
    const detailDialog = await screen.findByRole("dialog", { name: "checking-spec-readiness" })
    expect(within(detailDialog).getByText("Check goal, scope, non-goals, acceptance criteria, constraints, risks, and verification.")).toBeInTheDocument()
    await user.click(within(detailDialog).getByRole("button", { name: "Edit Skill" }))
    expect(screen.queryByRole("dialog", { name: "Edit Skill" })).not.toBeInTheDocument()
    await user.clear(within(detailDialog).getByLabelText("Description"))
    await user.type(within(detailDialog).getByLabelText("Description"), "Use when evaluating readiness evidence.")
    await user.clear(within(detailDialog).getByLabelText("Prompt"))
    await user.type(within(detailDialog).getByLabelText("Prompt"), "# Checking spec readiness\n\nCheck approved evidence only.")
    await user.click(within(detailDialog).getByRole("button", { name: "Save Skill" }))
    await waitFor(() => expect(within(detailDialog).queryByLabelText("Prompt")).not.toBeInTheDocument())
    expect(screen.getByText("Check approved evidence only.")).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/skills/skill-readiness?workspace_id=workspace-main",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          name: "checking-spec-readiness",
          description: "Use when evaluating readiness evidence.",
          prompt: "# Checking spec readiness\n\nCheck approved evidence only.",
        }),
      }),
    )

    await user.click(within(detailDialog).getByRole("button", { name: "Delete Skill" }))
    const deleteDialog = await screen.findByRole("dialog", { name: "Delete checking-spec-readiness Skill?" })
    await user.click(within(deleteDialog).getByRole("button", { name: "Delete Skill" }))
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "checking-spec-readiness" })).not.toBeInTheDocument())
    expect(within(settingsDialog).queryByText("checking-spec-readiness")).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/skills/skill-readiness?workspace_id=workspace-main", expect.objectContaining({ method: "DELETE" }))
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/skills/skill-readiness?workspace_id=workspace-main", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not render live registry Skills without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/policies/levels") {
        return Promise.resolve(new Response(JSON.stringify({ levels: [] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url === "http://registry.test/api/v1/skills?workspace_id=workspace-main") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  name: "missing-id-skill",
                  description: "This Skill should not be editable without a registry id.",
                  prompt: "No durable id.",
                  updated_at: "2026-06-30T10:00:00Z",
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

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: "GovernanceSkills and policy tiers" }))

    expect(await within(settingsDialog).findByText("Team rubric Skills")).toBeInTheDocument()
    expect(await within(settingsDialog).findByText("No team rubric Skills found.")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("missing-id-skill")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: "Manage missing-id-skill Skill" })).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/skills/missing-id-skill?workspace_id=workspace-main", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("retries team rubric Skills without reloading policy reference", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let skillRequests = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/skills?workspace_id=workspace-main") {
        skillRequests += 1
        if (skillRequests === 1) return Promise.resolve(new Response("{}", { status: 503 }))
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "skill-readiness",
                  name: "checking-spec-readiness",
                  description: "Review artifacts for executable handoff coverage.",
                  prompt: "Check goal, scope, non-goals, acceptance criteria, constraints, risks, and verification.",
                  updated_at: "2026-06-27T10:00:00Z",
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

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: "GovernanceSkills and policy tiers" }))

    await within(settingsDialog).findByText("Team rubric Skills unavailable.")
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/governance-profiles", expect.any(Object))
    await user.click(within(settingsDialog).getByRole("button", { name: "Retry Skills" }))
    expect(await within(settingsDialog).findByText("checking-spec-readiness")).toBeInTheDocument()
    expect(skillRequests).toBe(2)

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("lazy-loads independent policy reference sections and expands compact row details", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/policies/levels") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              levels: [
                {
                  governance_level: "enhanced",
                  display_name: "Enhanced",
                  approval_policy: "human_required",
                  evidence_policy: "corroborated_required",
                  required_roles: ["PM", "QA"],
                  required_topics: ["risk"],
                  required_evidence: ["test_report"],
                  enabled_gates: ["readiness", "delivery_review"],
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

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /GovernanceSkills and policy tiers/ }))

    expect(within(settingsDialog).queryByRole("heading", { name: "Governance" })).not.toBeInTheDocument()
    expect(within(settingsDialog).getByText("Policy reference")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Live")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Governance health")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/governance-profiles", expect.any(Object))
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/policies/levels", expect.any(Object))

    await user.click(within(settingsDialog).getByText("Policy reference"))
    expect(await within(settingsDialog).findByText("1 tier")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Enhanced")).toBeInTheDocument()
    expect(within(settingsDialog).getAllByText("human_required").length).toBeGreaterThan(0)
    expect(within(settingsDialog).getAllByText("corroborated_required").length).toBeGreaterThan(0)
    const enhancedRow = within(settingsDialog).getByText("Enhanced").closest("details")
    expect(enhancedRow).not.toBeNull()
    expect(within(enhancedRow!).getByText("2 gates · 1 evidence · 2 roles · 1 topic")).toBeVisible()
    expect(within(enhancedRow!).getByText("delivery_review")).not.toBeVisible()
    await user.click(within(enhancedRow!).getByText("Enhanced"))
    expect(within(enhancedRow!).getByText("delivery_review")).toBeVisible()
    expect(within(enhancedRow!).getByText("PM")).toBeVisible()
    expect(within(enhancedRow!).getByText("risk")).toBeVisible()
    expect(within(enhancedRow!).getByText("test_report")).toBeVisible()
    expect(within(enhancedRow!).getByText("enhanced")).toBeVisible()
    expect(within(settingsDialog).queryByRole("button", { name: /Import profile|Activate policy|Accept exception|Resolve policy|Record feedback/i })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/policies/levels", expect.any(Object))
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/policy-health", expect.any(Object))
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/outcome-feedback", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("keeps registry diagnostics out of browser Settings", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { settingsDialog } = await openSettings()

    expect(within(settingsDialog).queryByRole("button", { name: /Diagnostics|Registry and CLI/ })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("heading", { name: "Diagnostics" })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Registry build")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/meta", expect.any(Object))
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/api/v1/status", expect.any(Object))

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("lets mobile Settings sections return to the section list", async () => {
    const { user, settingsDialog } = await openSettings()

    expect(within(settingsDialog).queryByRole("button", { name: "Back to settings sections" })).not.toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))

    expect(within(settingsDialog).getByRole("button", { name: "Back to settings sections" })).toBeInTheDocument()
    expect(within(settingsDialog).getByRole("heading", { name: "Integrations" })).toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Back to settings sections" }))

    expect(within(settingsDialog).queryByRole("button", { name: "Back to settings sections" })).not.toBeInTheDocument()
    expect(within(settingsDialog).getByRole("button", { name: /Models/ })).toBeInTheDocument()
    expect(within(settingsDialog).getByRole("button", { name: /General/ })).toBeInTheDocument()
  })

  it("opens Settings Integrations from the OAuth return query", async () => {
    renderApp("/work?settings=integrations")

    const settingsDialog = await screen.findByRole("dialog", { name: "Settings" })
    expect(await within(settingsDialog).findByRole("heading", { name: "Integrations" })).toBeInTheDocument()
    expect(within(settingsDialog).getByRole("button", { name: /Integrations/ })).toHaveAttribute("aria-current", "page")
  })

  it("shows current workspace members in Settings", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (
        url ===
        "http://registry.test/api/v1/workspaces/workspace-main/members?current_user_id=user-local&current_username=thanhtung"
      ) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              workspace: { id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" },
              members: [
                {
                  workspace_id: "workspace-main",
                  user_id: "user-owner",
                  username: "ada",
                  display_name: "Ada Lovelace",
                  email: "ada@example.com",
                  role: "owner",
                },
                {
                  workspace_id: "workspace-main",
                  user_id: "user-local",
                  username: "thanhtung",
                  display_name: "Tung Local",
                  email: "tung@example.com",
                  role: "member",
                  current: true,
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

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Workspace/ }))

    expect(await within(settingsDialog).findByRole("heading", { name: "Team members" })).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Ada Lovelace")).toBeInTheDocument()
    expect(within(settingsDialog).getByText(/@ada/)).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Tung Local")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("You")).toBeInTheDocument()
    await user.hover(within(settingsDialog).getByText("Ada Lovelace"))
    expect(await screen.findByRole("tooltip", { name: "Ada Lovelace member details" })).toBeInTheDocument()
    expect(screen.getByText("Workspace member")).toBeInTheDocument()
    expect(screen.getByText("ada@example.com")).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/api/v1/workspaces/workspace-main/members?current_user_id=user-local&current_username=thanhtung",
      expect.any(Object),
    )
  })

})
