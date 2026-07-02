import { describe, expect, it, vi } from "vitest"

import { bootstrapIdentity, listIdentityWorkspaces } from "@/data/identity"

describe("identity adapter", () => {
  it("bootstraps a local user and workspace selection through Doc Registry", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          workspace: { id: "ws-core", slug: "core", name: "SpecGate Core" },
          user: {
            id: "user-tung",
            username: "tung",
            display_name: "Tung Local",
            email: "tung@example.com",
          },
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
    vi.stubGlobal("fetch", fetchMock)

    await expect(
      bootstrapIdentity("http://registry.test", {
        workspaceName: "SpecGate Core",
        displayName: "Tung Local",
        username: "tung",
        email: "tung@example.com",
      }),
    ).resolves.toEqual({
      workspace: { id: "ws-core", slug: "core", name: "SpecGate Core" },
      user: {
        id: "user-tung",
        username: "tung",
        name: "Tung Local",
        email: "tung@example.com",
      },
    })
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/api/v1/identity/bootstrap",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          workspace_name: "SpecGate Core",
          display_name: "Tung Local",
          username: "tung",
          email: "tung@example.com",
        }),
      }),
    )
  })

  it("lists workspaces for onboarding and workspace switching", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
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
    vi.stubGlobal("fetch", fetchMock)

    await expect(listIdentityWorkspaces("http://registry.test")).resolves.toEqual([
      { id: "ws-core", slug: "core", name: "SpecGate Core" },
      { id: "ws-docs", slug: "docs", name: "SpecGate Docs" },
    ])
  })

  it("ignores incomplete live workspace rows instead of creating local fallbacks", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          items: [
            { id: "ws-core", slug: "core", name: "SpecGate Core" },
            { slug: "missing-id", name: "Missing Id" },
            { id: "", slug: "", name: "Local workspace" },
            {},
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
    vi.stubGlobal("fetch", fetchMock)

    await expect(listIdentityWorkspaces("http://registry.test")).resolves.toEqual([
      { id: "ws-core", slug: "core", name: "SpecGate Core" },
    ])
  })
})
