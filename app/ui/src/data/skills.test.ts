import { describe, expect, it, vi } from "vitest"

import { createRegistrySkill, deleteRegistrySkill, getRegistrySkill, listRegistrySkills, updateRegistrySkill } from "@/data/skills"

describe("registry skills adapter", () => {
  it("lists registry Skills as summaries", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          items: [
            {
              id: "skill-1",
              name: "checking-spec-readiness",
              description: "Review spec artifacts.",
              prompt: "Full rubric body stays out of the list row.",
              updated_at: "2026-06-27T10:00:00Z",
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
    vi.stubGlobal("fetch", fetchMock)

    await expect(listRegistrySkills("http://registry.test")).resolves.toEqual([
      {
        id: "skill-1",
        name: "checking-spec-readiness",
        description: "Review spec artifacts.",
        updatedAt: "2026-06-27T10:00:00Z",
      },
    ])
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/skills", { signal: undefined })
  })

  it("loads a single registry Skill prompt as read-only detail", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          body: {
            id: "skill-1",
            name: "checking-spec-readiness",
            description: "Review spec artifacts.",
            prompt: "# Rubric\n\nCheck goals, scope, acceptance criteria, and verification.",
            created_at: "2026-06-20T10:00:00Z",
            updated_at: "2026-06-27T10:00:00Z",
          },
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
    vi.stubGlobal("fetch", fetchMock)

    await expect(getRegistrySkill("http://registry.test", "skill-1")).resolves.toEqual({
      id: "skill-1",
      name: "checking-spec-readiness",
      description: "Review spec artifacts.",
      prompt: "# Rubric\n\nCheck goals, scope, acceptance criteria, and verification.",
      createdAt: "2026-06-20T10:00:00Z",
      updatedAt: "2026-06-27T10:00:00Z",
    })
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/api/v1/skills/skill-1", { signal: undefined })
  })

  it("accepts the live top-level SkillDTO detail response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          id: "skill-2",
          name: "review-impl",
          description: "Review implementation bundles.",
          prompt: "# Review\n\nCheck plans and verification.",
          updated_at: "2026-06-28T10:00:00Z",
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
    vi.stubGlobal("fetch", fetchMock)

    await expect(getRegistrySkill("http://registry.test", "skill-2")).resolves.toMatchObject({
      id: "skill-2",
      name: "review-impl",
      prompt: "# Review\n\nCheck plans and verification.",
    })
  })

  it("creates, updates, and deletes registry Skills through the mutable system routes", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url === "http://registry.test/skills" && init?.method === "POST") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              body: {
                id: "skill-new",
                name: "delivery-evidence",
                description: "Use when reviewing delivery evidence.",
                prompt: "# Delivery evidence\n\nCheck tests and docs.",
                updated_at: "2026-06-30T10:00:00Z",
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/skills/skill-new" && init?.method === "PUT") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              body: {
                id: "skill-new",
                name: "delivery-evidence",
                description: "Use when reviewing completed delivery evidence.",
                prompt: "# Delivery evidence\n\nCheck changed files, tests, and docs.",
                updated_at: "2026-06-30T10:05:00Z",
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url === "http://registry.test/skills/skill-new" && init?.method === "DELETE") {
        return Promise.resolve(new Response(JSON.stringify({ body: { ok: true } }), { headers: { "Content-Type": "application/json" } }))
      }
      return Promise.resolve(new Response("{}", { status: 404 }))
    })
    vi.stubGlobal("fetch", fetchMock)

    await expect(
      createRegistrySkill("http://registry.test", {
        name: "delivery-evidence",
        description: "Use when reviewing delivery evidence.",
        prompt: "# Delivery evidence\n\nCheck tests and docs.",
      }),
    ).resolves.toMatchObject({ id: "skill-new", name: "delivery-evidence" })
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/skills",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          name: "delivery-evidence",
          description: "Use when reviewing delivery evidence.",
          prompt: "# Delivery evidence\n\nCheck tests and docs.",
        }),
      }),
    )

    await expect(
      updateRegistrySkill("http://registry.test", "skill-new", {
        name: "delivery-evidence",
        description: "Use when reviewing completed delivery evidence.",
        prompt: "# Delivery evidence\n\nCheck changed files, tests, and docs.",
      }),
    ).resolves.toMatchObject({
      id: "skill-new",
      description: "Use when reviewing completed delivery evidence.",
    })
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/skills/skill-new",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({
          name: "delivery-evidence",
          description: "Use when reviewing completed delivery evidence.",
          prompt: "# Delivery evidence\n\nCheck changed files, tests, and docs.",
        }),
      }),
    )

    await expect(deleteRegistrySkill("http://registry.test", "skill-new")).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith("http://registry.test/skills/skill-new", expect.objectContaining({ method: "DELETE" }))
  })
})
