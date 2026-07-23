import { screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanupAppTest, emptyRegistryResponse, openSettings, renderApp, setupAppTest } from "./app-test-support"

describe("SpecGate UI shell: integrations and models", () => {
  beforeEach(setupAppTest)
  afterEach(cleanupAppTest)

  it("adds an integration through Doc Registry", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let createdBody: Record<string, string> | null = null
    let tokenBody: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations" && init?.method === "POST") {
        createdBody = JSON.parse(String(init.body)) as Record<string, string>
        return Promise.resolve({
          ok: true,
          json: async () => ({
            id: "int-github",
            provider: "github",
            name: createdBody?.name,
            status: "connected",
            base_url: createdBody?.base_url,
            config_json: createdBody?.config_json,
            auth_method: "pat",
            has_api_token: false,
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-github/api-token?workspace_id=workspace-main" && init?.method === "PUT") {
        tokenBody = JSON.parse(String(init.body)) as Record<string, string>
        return Promise.resolve({ ok: true, status: 204, text: async () => "" } as Response)
      }
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    expect(await within(settingsDialog).findByText("No integrations connected yet. Add GitHub, GitLab, or Linear when you need provider signals.")).toBeInTheDocument()
    await user.click(within(settingsDialog).getByRole("button", { name: "Add integration" }))

    const addDialog = screen.getByRole("dialog", { name: "Add integration" })
    const nameInput = within(addDialog).getByPlaceholderText("GitHub")
    await user.clear(nameInput)
    await user.type(nameInput, "GitHub workspace")
    expect(within(addDialog).queryByText("Base URL")).not.toBeInTheDocument()
    await user.click(within(addDialog).getByRole("button", { name: /API token/ }))
    await user.type(within(addDialog).getByPlaceholderText("ghp_..."), "github-test-token")
    await user.click(within(addDialog).getByRole("button", { name: "Add integration" }))

    await waitFor(() => expect(createdBody).not.toBeNull())
    expect(createdBody).toMatchObject({
      provider: "github",
      name: "GitHub workspace",
      config_json: JSON.stringify({ enabled: true }),
    })
    expect(createdBody).not.toHaveProperty("base_url")
    expect(tokenBody).toEqual({ api_token: "github-test-token" })
    expect(await within(settingsDialog).findByText("GitHub workspace")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("API token stored. Link resources here; reprovisioning, disconnect, and webhook-secret management stay with backend/admin flows.")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("No linked resources yet. Use Link resource to register a repository or team; webhook management stays in backend/admin flows.")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Loading linked resources...")).not.toBeInTheDocument()
  })

  it("does not render live integrations without registry ids", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                provider: "github",
                name: "Missing id integration",
                status: "connected",
                auth_method: "pat",
                has_api_token: true,
              },
            ],
          }),
        } as Response)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))

    expect(await within(settingsDialog).findByText("No integrations connected yet. Add GitHub, GitLab, or Linear when you need provider signals.")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Missing id integration")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/integrations/undefined/resources")
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/integrations/undefined/webhook-events?limit=3")

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows linked integration resources as read-only health", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                id: "int-gitlab",
                provider: "gitlab",
                name: "GitLab delivery",
                status: "connected",
                auth_method: "pat",
                has_api_token: true,
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-gitlab/resources?workspace_id=workspace-main") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                id: "res-web",
                resource_type: "project",
                external_key: "specgate/web",
                display_name: "SpecGate Web",
                default_ref: "main",
                has_webhook_secret: true,
                config_json: JSON.stringify({
                  webhook_status: "connected",
                  provider_webhook_id: "hook-42",
                }),
              },
              {
                id: "res-secret",
                resource_type: "project",
                external_key: "specgate/unparseable",
                display_name: "Unparseable Repo",
                has_webhook_secret: true,
                config_json: "{not-json",
              },
              {
                id: "res-error",
                resource_type: "repo",
                external_key: "specgate/broken",
                display_name: "Broken Hook",
                config_json: JSON.stringify({
                  webhook_status: "error",
                  webhook_last_error: "permission denied",
                }),
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-gitlab/webhook-events?workspace_id=workspace-main&limit=3") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                id: "webhook-1",
                event_type: "tracker_issue",
                status: "processed",
                correlation_id: "CR-7D0AD9AF",
                received_at: "2026-06-30T06:01:00Z",
              },
              {
                id: "webhook-2",
                event_type: "comment",
                status: "failed",
                error: "signature mismatch",
                received_at: "2026-06-30T06:00:00Z",
              },
            ],
          }),
        } as Response)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    expect(await within(settingsDialog).findByText("GitLab delivery")).toBeInTheDocument()

    // Details are lazy: nothing is fetched per integration until it is expanded.
    expect(within(settingsDialog).queryByText("Linked resources")).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/integrations/int-gitlab/resources?workspace_id=workspace-main")
    expect(fetchMock).not.toHaveBeenCalledWith("http://registry.test/integrations/int-gitlab/webhook-events?workspace_id=workspace-main&limit=3")

    await user.click(within(settingsDialog).getByRole("button", { name: "Show details" }))
    expect(await within(settingsDialog).findByText("Linked resources")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("SpecGate Web")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Ref main")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Webhook connected")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Provider hook hook-42")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Unparseable Repo")).toBeInTheDocument()
    expect(within(settingsDialog).getAllByText("Webhook connected")).toHaveLength(1)
    expect(within(settingsDialog).getAllByText("Webhook secret stored").length).toBeGreaterThan(0)
    expect(within(settingsDialog).getByText("Broken Hook")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Webhook error")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("permission denied")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Recent webhooks")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Tracker issue")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("processed")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("CR-7D0AD9AF")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Comment")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("failed")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("signature mismatch")).toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Reprovision/ })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Delete resource/ })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Record webhook/ })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/integrations/int-gitlab/resources?workspace_id=workspace-main")
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/integrations/int-gitlab/webhook-events?workspace_id=workspace-main&limit=3")
  })

  it("links a repository resource through Doc Registry without admin controls", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let linkedBody: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                id: "int-github",
                provider: "github",
                name: "GitHub delivery",
                status: "connected",
                auth_method: "pat",
                has_api_token: true,
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-github/resources?workspace_id=workspace-main") {
        if (init?.method === "POST") {
          linkedBody = JSON.parse(String(init.body)) as Record<string, string>
          return Promise.resolve({
            ok: true,
            json: async () => ({
              id: "res-api",
              resource_type: linkedBody?.resource_type,
              external_id: linkedBody?.external_id,
              external_key: linkedBody?.external_key,
              display_name: linkedBody?.display_name,
              default_ref: linkedBody?.default_ref,
              has_webhook_secret: true,
              config_json: JSON.stringify({ webhook_status: "connected", provider_webhook_id: "hook-api" }),
            }),
          } as Response)
        }
        return emptyRegistryResponse(input)
      }
      if (url === "http://registry.test/integrations/int-github/repos?workspace_id=workspace-main&limit=50") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                external_id: "987",
                external_key: "specgate/api",
                display_name: "SpecGate API",
                default_ref: "main",
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-github/webhook-events?workspace_id=workspace-main&limit=3") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    expect(await within(settingsDialog).findByText("GitHub delivery")).toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Link resource" }))
    const linkDialog = await screen.findByRole("dialog", { name: "Link resource" })
    expect(await within(linkDialog).findByText("SpecGate API")).toBeInTheDocument()
    await user.click(within(linkDialog).getByRole("button", { name: "Link resource" }))

    await waitFor(() => expect(linkedBody).not.toBeNull())
    expect(linkedBody).toMatchObject({
      resource_type: "repo",
      external_id: "987",
      external_key: "specgate/api",
      display_name: "SpecGate API",
      default_ref: "main",
    })
    expect(await within(settingsDialog).findByText("Webhook connected")).toBeInTheDocument()
    expect(within(settingsDialog).getByText("Provider hook hook-api")).toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Reprovision/ })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Delete resource/ })).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByRole("button", { name: /Rotate secret/ })).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not link integration resource candidates without registry keys", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let linkedBody: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                id: "int-github",
                provider: "github",
                name: "GitHub delivery",
                status: "connected",
                auth_method: "pat",
                has_api_token: true,
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-github/repos?workspace_id=workspace-main&limit=50") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            items: [
              {
                external_id: "987",
                display_name: "SpecGate API",
                default_ref: "main",
              },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-github/resources?workspace_id=workspace-main") {
        if (init?.method === "POST") {
          linkedBody = JSON.parse(String(init.body)) as Record<string, string>
        }
        return emptyRegistryResponse(input)
      }
      if (url === "http://registry.test/integrations/int-github/webhook-events?workspace_id=workspace-main&limit=3") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    expect(await within(settingsDialog).findByText("GitHub delivery")).toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Link resource" }))
    const linkDialog = await screen.findByRole("dialog", { name: "Link resource" })
    expect(await within(linkDialog).findByText("No repositories returned for this integration credential.")).toBeInTheDocument()
    expect(within(linkDialog).queryByText("SpecGate API")).not.toBeInTheDocument()
    expect(within(linkDialog).getByRole("button", { name: "Link resource" })).toBeDisabled()
    expect(linkedBody).toBeNull()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("only asks for integration Base URL for self-hosted API-token setup", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let createdBody: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations" && init?.method === "POST") {
        createdBody = JSON.parse(String(init.body)) as Record<string, string>
        return Promise.resolve({
          ok: true,
          json: async () => ({
            id: "int-gitlab",
            provider: "gitlab",
            name: createdBody?.name,
            status: "connected",
            base_url: createdBody?.base_url,
            config_json: createdBody?.config_json,
            auth_method: "pat",
            has_api_token: false,
          }),
        } as Response)
      }
      if (url === "http://registry.test/integrations/int-gitlab/api-token?workspace_id=workspace-main" && init?.method === "PUT") {
        return Promise.resolve({ ok: true, status: 204, text: async () => "" } as Response)
      }
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    await user.click(await within(settingsDialog).findByRole("button", { name: "Add integration" }))

    const addDialog = screen.getByRole("dialog", { name: "Add integration" })
    await user.click(within(addDialog).getByRole("button", { name: /GitLab/ }))
    expect(within(addDialog).queryByText("Base URL")).not.toBeInTheDocument()

    await user.click(within(addDialog).getByRole("button", { name: /API token/ }))
    await user.click(within(addDialog).getByRole("checkbox", { name: /Self-hosted GitLab/ }))
    const baseUrlInput = within(addDialog).getByLabelText("Base URL")
    await user.clear(baseUrlInput)
    await user.type(baseUrlInput, "https://gitlab.example.com")
    await user.type(within(addDialog).getByPlaceholderText("glpat-..."), "gitlab-test-token")
    await user.click(within(addDialog).getByRole("button", { name: "Add integration" }))

    await waitFor(() => expect(createdBody).not.toBeNull())
    expect(createdBody).toMatchObject({
      provider: "gitlab",
      name: "GitLab",
      base_url: "https://gitlab.example.com",
    })
  })

  it("starts hosted integration OAuth without Base URL or API token", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let oauthBody: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations/oauth/begin" && init?.method === "POST") {
        oauthBody = JSON.parse(String(init.body)) as Record<string, string>
        return Promise.resolve({ ok: true, json: async () => ({ authorize_url: "#oauth-provider" }) } as Response)
      }
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    await user.click(await within(settingsDialog).findByRole("button", { name: "Add integration" }))

    const addDialog = screen.getByRole("dialog", { name: "Add integration" })
    await user.click(within(addDialog).getByRole("button", { name: /OAuth/ }))
    expect(within(addDialog).queryByText("Base URL")).not.toBeInTheDocument()
    expect(within(addDialog).queryByLabelText("API token")).not.toBeInTheDocument()
    await user.click(within(addDialog).getByRole("button", { name: "Add integration" }))

    await waitFor(() => expect(oauthBody).not.toBeNull())
    expect(oauthBody).toMatchObject({
      provider: "github",
      name: "GitHub",
      config_json: JSON.stringify({ enabled: true }),
    })
    expect(oauthBody).not.toHaveProperty("base_url")
    expect(oauthBody).toHaveProperty("redirect_target")
    expect(oauthBody!.redirect_target).toContain("settings=integrations")
    expect(window.location.hash).toBe("#oauth-provider")
    window.history.replaceState(null, "", "/")
  })

  it("shows an integration OAuth error when Doc Registry omits the authorize URL", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/integrations/oauth/begin" && init?.method === "POST") {
        return Promise.resolve({ ok: true, json: async () => ({}) } as Response)
      }
      if (url === "http://registry.test/integrations?workspace_id=workspace-main") {
        return emptyRegistryResponse(input)
      }
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({ ok: true, json: async () => ({ data: [] }) } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))
    await user.click(await within(settingsDialog).findByRole("button", { name: "Add integration" }))

    const addDialog = screen.getByRole("dialog", { name: "Add integration" })
    await user.click(within(addDialog).getByRole("button", { name: /OAuth/ }))
    await user.click(within(addDialog).getByRole("button", { name: "Add integration" }))

    expect(await within(addDialog).findByText(/provider authorization URL was not returned/i)).toBeInTheDocument()
    expect(window.location.hash).not.toBe("#oauth-provider")
  })

  it("loads and saves OpenRouter governance model settings", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let savedSettings: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("openrouter.ai")) {
        if (url.includes("output_modalities=embeddings")) {
          return Promise.resolve(
            new Response(JSON.stringify({
              data: [
                { id: "mistralai/mistral-embed-2312", name: "Mistral Embed", architecture: { output_modalities: ["embeddings"] } },
                { id: "someorg/chat-only", name: "Chat Only", architecture: { output_modalities: ["text"] } },
              ],
            })),
          )
        }
        return Promise.resolve(
          new Response(JSON.stringify({
            data: [
              { id: "deepseek/deepseek-v4-flash", name: "DeepSeek V4 Flash", architecture: { output_modalities: ["text"] } },
              { id: "anthropic/claude-sonnet-4.6", name: "Claude Sonnet 4.6", architecture: { output_modalities: ["text"] } },
              { id: "someorg/image-gen", name: "Image Gen", architecture: { output_modalities: ["image", "text"] } },
            ],
          })),
        )
      }
      if (url === "http://registry.test/settings" && init?.method === "PUT") {
        savedSettings = JSON.parse(String(init.body)).settings as Record<string, string>
        return Promise.resolve({ ok: true, json: async () => ({ settings: savedSettings }) } as Response)
      }
      if (url === "http://registry.test/settings") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            settings: {
              "governance.model_provider": "openrouter",
              "governance.model": "deepseek/deepseek-v4-flash",
              "governance.default_thinking_level": "medium",
              "embedding.model_provider": "openrouter",
              "embedding.model": "mistralai/mistral-embed-2312",
              "openrouter.api_key": "***",
            },
          }),
        } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /Models/ }))
    expect(await within(settingsDialog).findByText("DeepSeek V4 Flash")).toBeInTheDocument()
    expect(await within(settingsDialog).findByText("Claude Sonnet 4.6")).toBeInTheDocument()
    expect(await within(settingsDialog).findByText("Mistral Embed")).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Chat Only")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Image Gen")).not.toBeInTheDocument()
    expect(within(settingsDialog).getByRole("button", { name: "Save" })).toBeDisabled()

    const search = within(settingsDialog).getByPlaceholderText("Search models or type a model ID")
    await user.type(search, "z-ai/glm-5.1")
    await user.click(await within(settingsDialog).findByRole("button", { name: /Use z-ai\/glm-5\.1/ }))
    expect(within(settingsDialog).getByRole("button", { name: "Save" })).toBeEnabled()
    await user.click(within(settingsDialog).getByRole("button", { name: "Save" }))

    await waitFor(() => expect(savedSettings).not.toBeNull())
    expect(savedSettings!["governance.model_provider"]).toBe("openrouter")
    expect(savedSettings!["governance.model"]).toBe("z-ai/glm-5.1")
    expect(savedSettings!["governance.default_thinking_level"]).toBe("medium")
    expect(savedSettings!["embedding.model_provider"]).toBe("openrouter")
    expect(savedSettings!["embedding.model"]).toBe("mistralai/mistral-embed-2312")
    expect(savedSettings!["openrouter.api_key"]).toBe("***")
  })

  it("loads and saves General governance defaults, auto-archive, and the retention sweeper toggle", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let savedSettings: Record<string, string> | null = null
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/settings" && init?.method === "PUT") {
        savedSettings = JSON.parse(String(init.body)).settings as Record<string, string>
        return Promise.resolve({ ok: true, json: async () => ({ settings: savedSettings }) } as Response)
      }
      if (url === "http://registry.test/settings") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            settings: {
              "governance.auto_archive_on_delivery_pass": "false",
              "governance.feature_freshness_sla_days": "7",
              "governance.artifact_stale_days": "5",
              "governancefiles.ttl_days": "90",
              "governance.gate_confidence_threshold": "0.7",
              "retention.artifact_sweep_enabled": "false",
            },
          }),
        } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /General/ }))
    expect(await within(settingsDialog).findByRole("heading", { name: "General settings" })).toBeInTheDocument()
    expect(within(settingsDialog).queryByLabelText("Auto-refresh Feature overviews")).not.toBeInTheDocument()
    expect(within(settingsDialog).getByLabelText("Auto-archive after human delivery acceptance")).not.toBeChecked()
    expect(within(settingsDialog).getByLabelText("Artifact retention sweeper")).not.toBeChecked()
    expect(within(settingsDialog).queryByLabelText("Feature freshness SLA days")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByLabelText("Artifact stale after days")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Operational boundaries")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Governance file retention")).not.toBeInTheDocument()
    expect(within(settingsDialog).queryByLabelText(/Governance file retention days/i)).not.toBeInTheDocument()

    await user.click(within(settingsDialog).getByLabelText("Auto-archive after human delivery acceptance"))
    await user.click(within(settingsDialog).getByLabelText("Artifact retention sweeper"))
    expect(within(settingsDialog).getByRole("button", { name: "Save" })).toBeEnabled()
    await user.click(within(settingsDialog).getByRole("button", { name: "Save" }))

    await waitFor(() => expect(savedSettings).not.toBeNull())
    expect(savedSettings).toEqual({
      "governance.auto_archive_on_delivery_pass": "true",
      "retention.artifact_sweep_enabled": "true",
    })
    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("runs workspace cleanup from General settings behind an explicit confirm", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    let cleanupCalls = 0
    let cleanupURL = ""
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/api/v1/workspaces") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core" }] }),
        } as Response)
      }
      if (url.startsWith("http://registry.test/maintenance/cleanup") && init?.method === "POST") {
        cleanupCalls++
        cleanupURL = url
        return Promise.resolve({
          ok: true,
          json: async () => ({
            expired_artifacts_deleted: 2,
            referenced_skipped: 1,
            demo_features_deleted: 3,
            demo_change_requests_deleted: 5,
            demo_artifacts_deleted: 12,
            archived_change_requests_deleted: 4,
          }),
        } as Response)
      }
      if (url === "http://registry.test/settings") {
        return Promise.resolve({
          ok: true,
          json: async () => ({ settings: { "retention.artifact_sweep_enabled": "false" } }),
        } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /General/ }))
    expect(await within(settingsDialog).findByRole("heading", { name: "General settings" })).toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Clean up workspace" }))
    expect(cleanupCalls).toBe(0)
    expect(within(settingsDialog).getByText(/cannot be undone/i)).toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Cancel cleanup" }))
    expect(cleanupCalls).toBe(0)

    await user.click(within(settingsDialog).getByRole("button", { name: "Clean up workspace" }))
    await user.click(within(settingsDialog).getByRole("button", { name: "Yes, delete clutter" }))

    await waitFor(() => expect(cleanupCalls).toBe(1))
    expect(cleanupURL).toBe("http://registry.test/maintenance/cleanup?workspace_id=workspace-main")
    expect(await within(settingsDialog).findByText(/Cleanup complete/)).toBeInTheDocument()
    expect(within(settingsDialog).getByText(/2 expired artifacts/)).toBeInTheDocument()
    expect(within(settingsDialog).getByText(/4 archived work items/)).toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows saved custom model slugs even when they are missing from the catalog", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes("openrouter.ai")) {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            data: [
              { id: "deepseek/deepseek-v4-flash", name: "DeepSeek V4 Flash", architecture: { output_modalities: ["text"] } },
              { id: "mistralai/mistral-embed-2312", name: "Mistral Embed", architecture: { output_modalities: ["embeddings"] } },
            ],
          }),
        } as Response)
      }
      if (url === "http://registry.test/settings") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            settings: {
              "governance.model_provider": "openrouter",
              "governance.model": "vendor/private-governance",
              "governance.default_thinking_level": "medium",
              "embedding.model_provider": "openrouter",
              "embedding.model": "vendor/private-embedding",
              "openrouter.api_key": "***",
            },
          }),
        } as Response)
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)

    const { user, settingsDialog } = await openSettings()

    await user.click(within(settingsDialog).getByRole("button", { name: /Models/ }))

    expect(await within(settingsDialog).findByRole("button", { name: /vendor\/private-governance/ })).toHaveTextContent(
      "Selected",
    )
    expect(await within(settingsDialog).findByRole("button", { name: /vendor\/private-embedding/ })).toHaveTextContent(
      "Selected",
    )
  })

  it("accepts an explicit provider model ID outside the built-in suggestions", async () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url === "http://registry.test/settings") {
        return Promise.resolve({
          ok: true,
          json: async () => ({
            settings: {
              "governance.model_provider": "openai",
              "governance.model": "gpt-5.4-mini",
            },
          }),
        } as Response)
      }
      return emptyRegistryResponse(input)
    }))

    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Models/ }))
    const modelSearch = await within(settingsDialog).findByPlaceholderText("Search models or type a model ID")
    await user.type(modelSearch, "future-provider-model")

    expect(within(settingsDialog).getByRole("button", { name: /Use future-provider-model/ })).toBeInTheDocument()
  })

  it("groups integrations by repository and work-tracking roles without an experimental label", async () => {
    const { user, settingsDialog } = await openSettings()
    await user.click(within(settingsDialog).getByRole("button", { name: /Integrations/ }))

    expect(await within(settingsDialog).findByRole("heading", { name: "Repositories" })).toBeInTheDocument()
    expect(within(settingsDialog).getByRole("heading", { name: "Work tracking" })).toBeInTheDocument()
    expect(within(settingsDialog).queryByText("Experimental")).not.toBeInTheDocument()

    await user.click(within(settingsDialog).getByRole("button", { name: "Add integration" }))
    const addDialog = screen.getByRole("dialog", { name: "Add integration" })
    expect(within(addDialog).getByText("Marked pull requests and exact submitted commits.")).toBeInTheDocument()
    expect(within(addDialog).getByText("Marked merge requests and exact submitted commits.")).toBeInTheDocument()
    expect(within(addDialog).getByText("Optional handoff of approved work to a Linear team.")).toBeInTheDocument()
    expect(within(addDialog).getByRole("button", { name: /OAuth/ })).toHaveClass("border-primary/70")
  })

  it("redirects unknown sections back to Work", () => {
    renderApp("/not-a-section")

    expect(screen.getByRole("heading", { name: "Work" })).toBeInTheDocument()
  })
})
