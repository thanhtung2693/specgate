import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { vi } from "vitest"

import { GovernanceAgent } from "@/components/agent/governance-agent"
import { TooltipProvider } from "@/components/ui/tooltip"

import App from "./App"

const langGraphClientMock = vi.hoisted(() => ({
  create: vi.fn(),
  delete: vi.fn(),
  get: vi.fn(),
  getState: vi.fn(),
  search: vi.fn(),
  update: vi.fn(),
}))

export function getLangGraphClientMock() {
  return langGraphClientMock
}

export const sessionStorageKey = "specgate.ui.session.v2"

export function seedLocalSession() {
  localStorage.setItem(
    sessionStorageKey,
    JSON.stringify({
      profile: {
        id: "workspace-main",
        name: "SpecGate Core",
        user: {
          id: "user-local",
          username: "thanhtung",
          name: "Tung Local",
          email: "tung@example.com",
        },
      },
    }),
  )
}

vi.mock("@langchain/langgraph-sdk", () => ({
  Client: vi.fn(function Client() {
    return {
      threads: langGraphClientMock,
    }
  }),
}))

export function renderApp(path = "/work") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  )
}

export function renderGovernanceAgent(workspaceId?: string) {
  return render(
    <TooltipProvider>
      <GovernanceAgent workspaceId={workspaceId} />
    </TooltipProvider>,
  )
}

export const registryWorkItems = [
  {
    id: "SG-142",
    key: "SG-142",
    work_type: "cleanup",
    title: "Simplify CLI command surface",
    intent_md: "Reduce command sprawl while keeping governed handoff behavior explicit.",
    created_by: "Product",
    created_at: "2026-06-27T05:00:00Z",
    updated_at: "2026-06-27T05:12:00Z",
  },
  {
    id: "SG-136",
    key: "SG-136",
    work_type: "new_feature",
    title: "Workspace and user onboarding review",
    intent_md: "Clarify workspace/user setup, docs, and username-dependent surfaces.",
    created_by: "DX",
    created_at: "2026-06-27T04:00:00Z",
    updated_at: "2026-06-27T05:00:00Z",
  },
  {
    id: "SG-151",
    key: "SG-151",
    work_type: "new_feature",
    title: "Agent skills setup primitives",
    intent_md: "Define small setup skills for project onboarding and plugin refresh.",
    phase: "Ready",
    created_by: "Governance",
    created_at: "2026-06-27T04:20:00Z",
    updated_at: "2026-06-27T05:10:00Z",
  },
  {
    id: "SG-155",
    key: "SG-155",
    work_type: "cleanup",
    title: "Doc Registry migration cleanup",
    intent_md: "Ready for delivery review with Context Pack.",
    phase: "Review",
    created_by: "Backend",
    created_at: "2026-06-27T04:30:00Z",
    updated_at: "2026-06-27T05:20:00Z",
  },
  {
    id: "SG-153",
    key: "SG-153",
    work_type: "new_feature",
    title: "Delivery evidence trust stamp",
    intent_md: "Expose trustworthy evidence state for delivery review and audits.",
    created_by: "Platform",
    created_at: "2026-06-27T03:30:00Z",
    updated_at: "2026-06-27T04:55:00Z",
  },
  {
    id: "SG-147",
    key: "SG-147",
    work_type: "cleanup",
    title: "Pre-release verification sweep",
    intent_md: "Complete pre-release verification evidence.",
    phase: "Review",
    created_by: "QA",
    created_at: "2026-06-27T03:00:00Z",
    updated_at: "2026-06-27T04:45:00Z",
  },
]

const deliveredChangeRequest = {
  id: "SG-160",
  key: "SG-160",
  work_type: "cleanup",
  title: "Delivered settings polish",
  intent_md: "Shipped through delivery review.",
  phase: "delivered",
  created_by: "Platform",
  created_at: "2026-06-27T02:00:00Z",
  updated_at: "2026-06-27T05:40:00Z",
}

export function deliveredRegistryResponse(input: RequestInfo | URL, init?: RequestInit) {
  const url = String(input)
  if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
    return Promise.resolve(
      new Response(JSON.stringify({ items: [...registryWorkItems, deliveredChangeRequest] }), {
        headers: { "Content-Type": "application/json" },
      }),
    )
  }
  return defaultRegistryResponse(input, init)
}

export function defaultRegistryResponse(input: RequestInfo | URL, init?: RequestInit) {
  const url = fixtureURL(input)
  if (url.endsWith("/api/v1/workspaces")) {
    return Promise.resolve(
      new Response(JSON.stringify({ items: [{ id: "workspace-main", name: "SpecGate Core", slug: "specgate-core", is_default: true }] }), {
        headers: { "Content-Type": "application/json" },
      }),
    )
  }
  if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
    return Promise.resolve(new Response(JSON.stringify({ items: registryWorkItems }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/governance/feedback-events?status=received&limit=20")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            {
              id: "feedback-SG-147-browser-smoke",
              event_type: "coding_agent.blocked_ambiguity",
              status: "received",
              reason: "Browser smoke evidence is required before delivery review can pass.",
              change_request_id: "SG-147",
              artifact_id: "artifact-SG-147",
              created_at: "2026-06-27T04:51:00Z",
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/workboard/features")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            {
              id: "SG-155",
              key: "SG-155",
              name: "Doc Registry migration cleanup",
              status: "approved",
              version: 1,
              canonical_artifact_id: "artifact-SG-155",
            },
            {
              id: "SG-151",
              key: "SG-151",
              name: "Agent skills setup primitives",
              status: "approved",
              version: 1,
              canonical_artifact_id: "artifact-SG-151",
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/workboard/features/SG-155")) {
    return Promise.resolve(new Response(JSON.stringify({ id: "SG-155", key: "SG-155", name: "Doc Registry migration cleanup", canonical_artifact_id: "artifact-SG-155" }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/workboard/features/SG-151")) {
    return Promise.resolve(new Response(JSON.stringify({ id: "SG-151", key: "SG-151", name: "Agent skills setup primitives", canonical_artifact_id: "artifact-SG-151" }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/api/v1/work-items/SG-155/context-pack")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          state: "assembled",
          markdown: "# Context Pack\n\nRun `specgate work context SG-155` before implementation.",
          change_request_id: "SG-155",
          feature_id: "SG-155",
          governance_level: "standard",
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
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
              created_by: "governance",
              updated_at: "2026-06-27T05:20:00Z",
              expected_gates: ["spec_completeness"],
            },
            {
              id: "artifact-SG-151",
              feature_id: "SG-151",
              feature_name: "Agent skills setup primitives",
              version: "v0.1",
              status: "approved",
              request_type: "change_request",
              impact_level: "low",
              artifact_completeness: "full",
              source_kind: "context_pack",
              created_by: "governance",
              updated_at: "2026-06-27T05:10:00Z",
              expected_gates: ["spec_completeness"],
            },
            {
              id: "artifact-quick-standalone",
              feature_id: "",
              version: "v0.20260703140824",
              status: "approved",
              request_type: "change_request",
              impact_level: "medium",
              artifact_completeness: "full",
              source_kind: "context_pack",
              created_by: "governance",
              updated_at: "2026-06-27T05:22:00Z",
              expected_gates: ["delivery_review"],
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/artifacts?feature_id=SG-155&limit=100")) {
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
              id: "artifact-SG-155-previous",
              feature_id: "SG-155",
              feature_name: "Doc Registry migration cleanup",
              version: "v0.0",
              status: "superseded",
              request_type: "change_request",
              impact_level: "low",
              artifact_completeness: "full",
              source_kind: "context_pack",
              updated_at: "2026-06-27T04:20:00Z",
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/artifacts?feature_id=SG-151&limit=100")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            {
              id: "artifact-SG-151",
              feature_id: "SG-151",
              feature_name: "Agent skills setup primitives",
              version: "v0.1",
              status: "approved",
              request_type: "change_request",
              impact_level: "low",
              artifact_completeness: "full",
              source_kind: "context_pack",
              updated_at: "2026-06-27T05:10:00Z",
            },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/artifacts/artifact-SG-155/files")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            { path: "migration/spec.md", role: "spec", size_bytes: 3900, updated_at: "2026-06-27T05:20:00Z" },
            { path: "migration/tasks.md", role: "tasks", size_bytes: 1600, updated_at: "2026-06-27T05:20:00Z" },
            { path: "migration/verification.md", role: "verification", size_bytes: 1200, updated_at: "2026-06-27T05:20:00Z" },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/artifacts/artifact-SG-151/files")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [
            { path: "setup/spec.md", role: "spec", size_bytes: 4200, updated_at: "2026-06-27T05:10:00Z" },
            { path: "setup/tasks.md", role: "tasks", size_bytes: 1800, updated_at: "2026-06-27T05:10:00Z" },
          ],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.includes("/artifacts/artifact-SG-155/files/_?path=migration%2Fspec.md")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          content:
            "# Doc Registry migration cleanup\n\nThis spec keeps migration behavior deterministic while reducing noisy expected misses.\n\n```mermaid\nsequenceDiagram\n  participant CI\n  participant DB\n  CI->>DB: Apply migrations\n  DB-->>CI: Schema ready\n```\n",
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.includes("/artifacts/artifact-SG-155-previous/files/_?path=migration%2Fspec.md")) {
    return Promise.resolve(new Response(JSON.stringify({ content: "# Doc Registry migration cleanup\n\nPrevious version reduced expected migration misses.\n" }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.includes("/artifacts/artifact-SG-155/files/_?path=migration%2Ftasks.md")) {
    return Promise.resolve(new Response(JSON.stringify({ content: "# Doc Registry migration cleanup Tasks\n\n- Report completion through the governed delivery loop.\n" }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/features/SG-155/attachments")) {
    return Promise.resolve(
      new Response(
        JSON.stringify({
          items: [{ id: "attachment-SG-155", kind: "link", title: "Migration rollout note", note: "Reference used by quality-gate review.", audience: "gate", created_by: "governance", created_at: "2026-06-27T05:00:00Z" }],
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    )
  }
  if (url.endsWith("/governance/feedback-events?artifact_id=artifact-SG-155&limit=20")) {
    return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "feedback-SG-155", artifact_id: "artifact-SG-155", event_type: "coding_agent.completed", status: "received", reason: "Completion evidence is ready for delivery review.", created_at: "2026-06-27T05:00:00Z" }] }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/artifacts/artifact-SG-155/readiness-runs?limit=20")) {
    return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "readiness-SG-155", gate: "spec_completeness", state: "warn", hint: "missing constraints", created_at: "2026-06-27T05:00:00Z" }] }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/events?artifact_id=artifact-SG-155&limit=20")) {
    return Promise.resolve(new Response(JSON.stringify({ items: [{ id: "event-SG-155", artifact_id: "artifact-SG-155", event_type: "artifact.published", payload: { version: "v0.1" }, created_at: "2026-06-27T05:00:00Z" }] }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/api/v1/artifacts/artifact-SG-155/gate-preview")) {
    return Promise.resolve(new Response(JSON.stringify({ preview_tasks: [{ gate_key: "spec_completeness", note: "Expected gate from artifact snapshot." }] }), { headers: { "Content-Type": "application/json" } }))
  }
  if (url.endsWith("/api/v1/artifacts/artifact-SG-155/policy")) {
    return Promise.resolve(new Response(JSON.stringify({ governance_level: "standard", title: "Standard governance", summary: "Policy snapshot.", reasons: [], obligations: [] }), { headers: { "Content-Type": "application/json" } }))
  }
  return Promise.resolve(new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } }))
}

export function fixtureURL(input: RequestInfo | URL) {
  const request = new URL(String(input), "http://registry.test")
  request.searchParams.delete("workspace_id")
  return request.toString()
}

export function emptyRegistryResponse(input: RequestInfo | URL) {
  if (fixtureURL(input).endsWith("/api/v1/workspaces")) {
    return defaultRegistryResponse(input)
  }
  return Promise.resolve(new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } }))
}

export async function openSettings(path = "/work") {
  renderApp(path)
  const user = userEvent.setup()
  await user.click(screen.getByRole("button", { name: "Settings" }))
  return {
    user,
    settingsDialog: await screen.findByRole("dialog", { name: "Settings" }),
  }
}

export function setupAppTest() {
    localStorage.clear()
    seedLocalSession()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    vi.stubEnv("VITE_LANGGRAPH_API_URL", "")
    vi.stubGlobal("fetch", vi.fn(defaultRegistryResponse))
    langGraphClientMock.create.mockResolvedValue({
      thread_id: "thread-created",
      metadata: { source: "specgate-ui", surface: "governance-agent", title: "Governance thread" },
      updated_at: "2026-06-30T00:00:00Z",
    })
    langGraphClientMock.delete.mockResolvedValue(undefined)
    langGraphClientMock.get.mockResolvedValue({
      thread_id: "thread-delivery",
      metadata: { source: "specgate-ui", surface: "governance-agent", title: "Delivery review" },
      updated_at: "2026-06-30T00:00:00Z",
    })
    langGraphClientMock.getState.mockResolvedValue({ values: { messages: [] } })
    langGraphClientMock.search.mockResolvedValue([])
    langGraphClientMock.update.mockResolvedValue(undefined)
  }

export function cleanupAppTest() {
    localStorage.clear()
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
    vi.clearAllMocks()
  }
