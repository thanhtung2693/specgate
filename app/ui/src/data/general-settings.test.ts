import { afterEach, describe, expect, it, vi } from "vitest"

import { runWorkspaceCleanup } from "@/data/general-settings"

describe("general-settings data adapter", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("scopes destructive cleanup to the selected workspace", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({})))
    vi.stubGlobal("fetch", fetchMock)

    await runWorkspaceCleanup("http://registry.test/", "ws/a")

    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/maintenance/cleanup?workspace_id=ws%2Fa", { method: "POST" })
  })

  it("rejects destructive cleanup without a workspace before fetching", async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal("fetch", fetchMock)

    await expect(runWorkspaceCleanup("http://registry.test", " ")).rejects.toThrow("workspaceId is required")
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
