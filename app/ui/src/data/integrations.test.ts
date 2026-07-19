import { describe, expect, it, vi } from "vitest"

import {
  beginPendingIntegrationOAuth,
  createIntegration,
  createIntegrationResource,
  handoffToLinear,
  integrationProviders,
  listIntegrationRepos,
  listIntegrationResources,
  listIntegrationWebhookEvents,
  listIntegrations,
  listLinearTeams,
  setIntegrationApiToken,
} from "@/data/integrations"

describe("integrations adapter workspace boundary", () => {
  it("scopes reads and writes to the selected workspace", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (init?.method === "PUT") return Promise.resolve(new Response(null, { status: 204 }))
      if (url.endsWith("/oauth/begin")) return Promise.resolve(new Response(JSON.stringify({ authorize_url: "https://provider.test/oauth" })))
      if (init?.method === "POST") return Promise.resolve(new Response(JSON.stringify({ id: "int-1", provider: "github", name: "GitHub", status: "connected" })))
      return Promise.resolve(new Response(JSON.stringify({ items: [] })))
    })
    vi.stubGlobal("fetch", fetchMock)

    await listIntegrations("http://registry.test", "ws-a")
    await listIntegrationResources("http://registry.test", "ws-a", "int-1")
    await listIntegrationWebhookEvents("http://registry.test", "ws-a", "int-1")
    await listIntegrationRepos("http://registry.test", "ws-a", "int-1", "specgate/api")
    await listLinearTeams("http://registry.test", "ws-a", "int-1")
    await createIntegration("http://registry.test", "ws-a", { provider: "github", name: "GitHub" })
    await createIntegrationResource("http://registry.test", "ws-a", "int-1", { resource_type: "project", external_key: "specgate/api" })
    await setIntegrationApiToken("http://registry.test", "ws-a", "int-1", "secret")
    await beginPendingIntegrationOAuth("http://registry.test", "ws-a", { provider: "github", name: "GitHub" })

    expect(fetchMock.mock.calls.map(([input]) => String(input))).toEqual([
      "http://registry.test/integrations?workspace_id=ws-a",
      "http://registry.test/integrations/int-1/resources?workspace_id=ws-a",
      "http://registry.test/integrations/int-1/webhook-events?workspace_id=ws-a&limit=3",
      "http://registry.test/integrations/int-1/repos?workspace_id=ws-a&limit=50&search=specgate%2Fapi",
      "http://registry.test/integrations/int-1/linear/teams?workspace_id=ws-a",
      "http://registry.test/integrations",
      "http://registry.test/integrations/int-1/resources?workspace_id=ws-a",
      "http://registry.test/integrations/int-1/api-token?workspace_id=ws-a",
      "http://registry.test/integrations/oauth/begin",
    ])
    expect(JSON.parse(String(fetchMock.mock.calls[5][1]?.body))).toMatchObject({ workspace_id: "ws-a" })
    expect(JSON.parse(String(fetchMock.mock.calls[6][1]?.body))).toMatchObject({ workspace_id: "ws-a" })
    expect(JSON.parse(String(fetchMock.mock.calls[8][1]?.body))).toMatchObject({ workspace_id: "ws-a" })
  })

  it("rejects empty workspace IDs before making a request", async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)

    await expect(listIntegrations("http://registry.test", " ")).rejects.toThrow("workspaceId is required")
    await expect(createIntegration("http://registry.test", "", { provider: "github", name: "GitHub" })).rejects.toThrow("workspaceId is required")
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("classifies repository and work-tracking providers", () => {
    expect(integrationProviders.map((provider) => [provider.id, provider.category, provider.scope])).toEqual([
      ["github", "repositories", "Marked pull requests and exact submitted commits."],
      ["gitlab", "repositories", "Marked merge requests and exact submitted commits."],
      ["linear", "work_tracking", "Optional handoff of approved work to a Linear team."],
    ])
  })

  it("posts a Linear handoff for the exact workspace, integration, and team resource", async () => {
    const fetchMock = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) => Promise.resolve(new Response(JSON.stringify({
      created: true,
      link: { identifier: "ENG-123", url: "https://linear.app/acme/issue/ENG-123", state: "opened", tracker_state: "Todo" },
    }))))
    vi.stubGlobal("fetch", fetchMock)

    await handoffToLinear("http://registry.test", "ws-a", "cr-1", "int-linear", "team-platform")

    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/workboard/change-requests/cr-1/linear-handoff?workspace_id=ws-a",
      expect.objectContaining({ method: "POST" }),
    )
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      integration_id: "int-linear",
      resource_id: "team-platform",
    })
  })
})
