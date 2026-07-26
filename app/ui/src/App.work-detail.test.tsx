import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanupAppTest, defaultRegistryResponse, emptyRegistryResponse, fixtureURL, renderApp, setupAppTest } from "./app-test-support"

describe("SpecGate UI shell: work detail", () => {
  beforeEach(setupAppTest)
  afterEach(cleanupAppTest)

  it("renders route-backed work item detail", async () => {
    renderApp("/work/SG-155")

    expect(screen.getByRole("heading", { name: "Work" })).toBeInTheDocument()
    expect((await screen.findAllByRole("heading", { name: "Doc Registry migration cleanup" })).length).toBeGreaterThan(0)
    expect(await screen.findByRole("tab", { name: "Handoff" })).toBeInTheDocument()
    expect(screen.queryByText("Governance agent context")).not.toBeInTheDocument()
    expect(screen.getByText("Resume in CLI")).toBeInTheDocument()
  })

  it("shows an explicit state for unknown work item routes", async () => {
    renderApp("/work/SG-404")

    expect(await screen.findByRole("heading", { name: "Work item not found" })).toBeInTheDocument()
    expect(screen.getByText("SG-404")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Back to work" })).toHaveAttribute("href", "/work")
    expect(screen.queryByText("Work queue")).not.toBeInTheDocument()
  })

  it("does not show not-found while a registry work item route is loading", () => {
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "http://registry.test")
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>(() => {})))

    renderApp("/work/CR-LIVE")

    expect(screen.getByRole("heading", { name: "Loading work item" })).toBeInTheDocument()
    expect(screen.getByText("CR-LIVE")).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Work item not found" })).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not synthesize live gate runs without registry ids", async () => {
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
                  id: "cr-gate-run-missing",
                  key: "CR-GATE-RUN-MISSING",
                  work_type: "cleanup",
                  title: "Gate run missing id",
                  intent_md: "Do not render fake gate-run rows.",
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
      if (url.endsWith("/workboard/change-requests/cr-gate-run-missing/gate-runs?limit=10")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  gate: "delivery_review",
                  state: "fail",
                  hint: "Missing delivery evidence",
                  created_at: "2026-06-27T06:00:00Z",
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
    renderApp("/work/CR-GATE-RUN-MISSING")
    const user = userEvent.setup()

    expect(await screen.findByText("Gate run missing id")).toBeInTheDocument()
    await user.click(screen.getByRole("tab", { name: "Verification" }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/workboard/change-requests/cr-gate-run-missing/gate-runs?limit=10&workspace_id=workspace-main",
        expect.any(Object),
      ),
    )
    expect(screen.getByText("No persisted gate runs yet.")).toBeInTheDocument()
    expect(screen.queryByText("Missing delivery evidence")).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows live no-fallback errors for work item readback sections", async () => {
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
                  id: "cr-readback",
                  key: "CR-READBACK",
                  feature_id: "feature-readback",
                  work_type: "cleanup",
                  title: "Work readback outage",
                  intent_md: "Show registry readback failures without sample fallbacks.",
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
      if (
        url.endsWith("/workboard/change-requests/cr-readback/acceptance-criteria") ||
        url.endsWith("/workboard/change-requests/cr-readback/next-actions") ||
        url.endsWith("/workboard/change-requests/cr-readback/gate-runs?limit=10") ||
        url.endsWith("/workboard/change-requests/cr-readback/tracker-links") ||
        url.endsWith("/workboard/features/feature-readback") ||
        url.endsWith("/api/v1/work-items/cr-readback/policy") ||
        url.endsWith("/api/v1/work-items/cr-readback/delivery-status?detail=true")
      ) {
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
    renderApp("/work/CR-READBACK")
    const user = userEvent.setup()

    expect(await screen.findByText("Work readback outage")).toBeInTheDocument()
    expect(await screen.findByText(/Feature context unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/Acceptance criteria unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/Linked issues unavailable/)).toBeInTheDocument()
    expect(screen.queryByText(/No acceptance criteria are recorded for this work item yet/)).not.toBeInTheDocument()
    expect(screen.queryByText("No tracker links recorded.")).not.toBeInTheDocument()
    await user.click(screen.getByRole("tab", { name: "Verification" }))

    expect(await screen.findByText(/Gate next actions unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/Gate run history unavailable/)).toBeInTheDocument()
    expect(await screen.findByText(/Policy explanation unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/no fallback policy guidance is shown/)).toBeInTheDocument()
    expect(screen.getByText(/Delivery review readback unavailable/)).toBeInTheDocument()
    expect(screen.getByText(/no fallback delivery review detail is shown/)).toBeInTheDocument()
    expect(screen.queryByText("No next actions recorded.")).not.toBeInTheDocument()
    expect(screen.queryByText("No persisted gate runs yet.")).not.toBeInTheDocument()
    expect(screen.queryByText(/Current verdict is/)).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("does not synthesize live tracker links without registry identifiers and URLs", async () => {
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
                  id: "cr-tracker-missing",
                  key: "CR-TRACKER-MISSING",
                  feature_id: "feature-tracker-missing",
                  work_type: "cleanup",
                  title: "Tracker link missing id",
                  intent_md: "Do not render fake issue links.",
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
      if (url.endsWith("/workboard/change-requests/cr-tracker-missing/tracker-links")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { state: "opened", tracker_state: "Open" },
                { identifier: "ENG-123", state: "opened", tracker_state: "Open" },
                { url: "https://tracker.test/ENG-124", state: "opened", tracker_state: "Open" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-TRACKER-MISSING")

    expect(await screen.findByText("Tracker link missing id")).toBeInTheDocument()
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/workboard/change-requests/cr-tracker-missing/tracker-links?workspace_id=workspace-main",
        expect.any(Object),
      ),
    )
    expect(screen.getByText("No tracker links recorded.")).toBeInTheDocument()
    expect(screen.queryByText("tracker-1")).not.toBeInTheDocument()
    expect(screen.queryByText("ENG-123")).not.toBeInTheDocument()
    expect(screen.queryByRole("link", { name: /tracker/i })).not.toBeInTheDocument()

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("shows work-item freshness signals as read-only registry context", async () => {
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
                  id: "cr-live",
                  key: "CR-LIVE",
                  feature_id: "feature-live",
                  work_type: "cleanup",
                  title: "Freshness signal check",
                  intent_md: "Show stale Context Pack and tracker contradiction without fixing it from the browser.",
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
      if (url.endsWith("/workboard/stale-warnings?change_request_id=cr-live")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  code: "linked_knowledge_newer",
                  severity: "warning",
                  message: "Linked Knowledge changed after the approved source artifact.",
                  feature_id: "feature-live",
                  change_request_id: "cr-live",
                  artifact_id: "artifact-context-pack",
                },
                {
                  code: "tracker_status_conflict",
                  severity: "warning",
                  message: "Tracker says complete but merge evidence is missing.",
                  feature_id: "feature-live",
                  change_request_id: "cr-live",
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
    renderApp("/work/CR-LIVE")

    expect((await screen.findAllByText("Freshness signal check")).length).toBeGreaterThan(0)
    expect(await screen.findByText("Freshness signals")).toBeInTheDocument()
    expect(screen.getByText("Linked Knowledge Newer")).toBeInTheDocument()
    expect(screen.getByText("Linked Knowledge changed after the approved source artifact.")).toBeInTheDocument()
    expect(screen.getByText("Tracker Status Conflict")).toBeInTheDocument()
    expect(screen.getByText("Tracker says complete but merge evidence is missing.")).toBeInTheDocument()
    expect(screen.getByText("artifact-context-pack")).toBeInTheDocument()
    expect(screen.getByText("read-only")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Regenerate/i })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Rerun/i })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Resolve/i })).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/workboard/stale-warnings?change_request_id=cr-live&workspace_id=workspace-main",
      expect.any(Object),
    )

    vi.unstubAllGlobals()
    vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
  })

  it("renders the delivery verdict before gate summary and collapses gate detail in the verification tab", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/work-items/SG-155/delivery-status?detail=true")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              change_request_id: "SG-155",
              gate_run_id: "platform-review-sg-155",
              completion_feedback_event_id: "completion-sg-155",
              found: true,
              verdict: "pass",
              hint: "All acceptance criteria are satisfied.",
              reviewed_at: "2026-06-27T05:30:00Z",
              judge_model: "gpt-5.2",
              executor: "platform",
              git_receipt: {
                availability: "available",
                base_revision: "base-1",
                branch: "codex/trust-display",
                changed_files: ["app/ui/src/components/layout/work/item-detail.tsx"],
                diff_digest: "sha256:digest",
                head_revision: "abc123def4567890",
                repository: "specgate",
                warnings: [],
              },
              peer_review: {
                agent_name: "review-agent",
                reviewed_at: "2026-06-27T05:20:00Z",
                state: "stale",
              },
              per_criterion: [
                {
                  criterion_id: "ac-1",
                  text: "Trust provenance stays visible",
                  verdict: "met",
                  trust_tier: "grounded",
                  verification_binding: "app/ui/src/components/layout/work/item-detail.tsx:723",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/workboard/change-requests/SG-155/next-actions")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { gate: "delivery_review", state: "pass", hint: "Delivery review passed." },
                { gate: "spec_completeness", state: "not_applicable", hint: "Not required for quick work." },
                { gate: "design_readiness", state: "not_applicable", hint: "Not required for quick work." },
                { gate: "risk_review", state: "not_applicable", hint: "Not required for quick work." },
                { gate: "release_notes", state: "not_applicable", hint: "Not required for quick work." },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/workboard/change-requests/SG-155/gate-runs?limit=10")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "run-2", gate: "delivery_review", state: "pass", hint: "Latest delivery verdict.", created_at: "2026-06-27T05:30:00Z" },
                { id: "run-1", gate: "delivery_review", state: "pending", hint: "Waiting on delivery evidence.", created_at: "2026-06-27T05:00:00Z" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/SG-155?tab=verification")
    const user = userEvent.setup()

    const deliveryHeading = await screen.findByText("Delivery review")
    expect(await screen.findByLabelText("Delivery evidence available for review")).toBeInTheDocument()
    expect(screen.queryByLabelText("Delivery evidence has gaps")).not.toBeInTheDocument()
    expect(await screen.findAllByText("Ready for human review")).toHaveLength(2)
    expect(screen.getByText("Agent-reported; local citation captured")).toBeInTheDocument()
    expect(screen.getByText("Awaiting human acceptance")).toBeInTheDocument()
    expect(screen.getByText("Receipt recorded at commit abc123def456")).toBeInTheDocument()
    expect(screen.getByText("Platform model (gpt-5.2)")).toBeInTheDocument()
    expect(screen.getByText("Stale")).toBeInTheDocument()
    expect(screen.getByText("Grounded")).toBeInTheDocument()
    expect(screen.getByText("app/ui/src/components/layout/work/item-detail.tsx:723")).toBeInTheDocument()
    expect(screen.getByText(/A model review evaluates submitted evidence/)).toBeInTheDocument()
    expect(screen.queryByText(/Current verdict is/)).not.toBeInTheDocument()
    const gateHeading = screen.getByText("Gate state")
    const gateStateCard = gateHeading.closest(".rounded-lg")
    expect(gateStateCard).not.toBeNull()
    expect(within(gateStateCard as HTMLElement).getByText("Passed")).toBeInTheDocument()
    expect(within(gateStateCard as HTMLElement).queryByText("Failed")).not.toBeInTheDocument()
    expect(deliveryHeading.compareDocumentPosition(gateHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByText("What each gate checked and why, per run.")).toBeInTheDocument()
    expect(screen.queryByText("Readiness and quality outcomes stay in item context.")).not.toBeInTheDocument()

    const gateSummaryToggle = screen.getByRole("button", { name: "5 gates · 1 passed · 4 not applicable" })
    expect(gateSummaryToggle).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByText("Not required for quick work.")).not.toBeInTheDocument()
    await user.click(gateSummaryToggle)
    expect(screen.getAllByText("Not required for quick work.")).toHaveLength(4)
    expect(
      screen.getByText("Checks the package covers its required topics (outcomes, criteria, risks…). Reads every document in the package."),
    ).toBeInTheDocument()
    expect(screen.getAllByText("Judges the delivery evidence against every acceptance criterion.").length).toBeGreaterThan(1)

    expect(screen.getByText("Latest delivery verdict.")).toBeInTheDocument()
    expect(screen.queryByText("Waiting on delivery evidence.")).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Show all runs" }))
    expect(screen.getByText("Waiting on delivery evidence.")).toBeInTheDocument()
    expect(screen.getByText("Latest delivery verdict.")).toBeInTheDocument()
  })

  it("renders a human rejection as authority without rewriting passing evidence", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/api/v1/work-items/SG-155/delivery-status?detail=true")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              change_request_id: "SG-155",
              found: true,
              verdict: "fail",
              evidence_verdict: "pass",
              hint: "Human reviewer requested rework.",
              reviewed_at: "2026-06-27T05:30:00Z",
              executor: "human",
              actor: "lead",
              note: "Restore the rollback test.",
              confidence: 0,
              per_criterion: [],
              checks: [],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/SG-155?tab=verification")

    expect(await screen.findByLabelText("Delivery rejected")).toBeInTheDocument()
    expect(screen.queryByLabelText("Delivery evidence has gaps")).not.toBeInTheDocument()
    expect(screen.getByText("Ready for human review")).toBeInTheDocument()
    expect(screen.getAllByText("Rejected")).toHaveLength(2)
    expect(screen.getByText(/Restore the rollback test/)).toBeInTheDocument()
    expect(screen.getByText("0% reviewer confidence")).toBeInTheDocument()
  })

  it("discloses gate-run evidence with judge origin, confidence, and content", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/workboard/change-requests/SG-155/gate-runs?limit=10")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "run-judged",
                  gate: "canonical_spec",
                  state: "warn",
                  hint: "Judge confidence below threshold",
                  evidence_json: JSON.stringify({
                    evidence_contract_version: "gate-run-v1",
                    gate: "canonical_spec",
                    evaluator: { type: "agent_judge", judge_model: "gpt-5-mini" },
                    verdict: "warn",
                    confidence: 0.85,
                    evidence: "The working spec has not been promoted to canonical.",
                  }),
                  created_at: "2026-06-27T05:30:00Z",
                },
                {
                  id: "run-attested",
                  gate: "delivery_review",
                  state: "pass",
                  hint: "Verdict derived from the coding agent's acceptance-criteria claims.",
                  evidence_json: JSON.stringify({
                    evidence_contract_version: "gate-run-v1",
                    gate: "delivery_review",
                    evaluator: { type: "agent_judge", judge_model: "agent_attested" },
                    verdict: "pass",
                    confidence: 1,
                    evidence: JSON.stringify({
                      criteria: [{ criterion_id: "ac-1", text: "Doc registry CI passes", verdict: "met", why: "coding-agent claim: satisfied" }],
                      checks: [],
                    }),
                  }),
                  created_at: "2026-06-27T05:20:00Z",
                },
                {
                  id: "run-deterministic",
                  gate: "spec_drafted",
                  state: "pass",
                  hint: "artifact-SG-155",
                  evidence_json: JSON.stringify({
                    evidence_contract_version: "gate-run-v1",
                    gate: "spec_drafted",
                    evaluator: { type: "deterministic", judge_model: "deterministic-v1" },
                    verdict: "pass",
                    confidence: 1,
                    evidence: "",
                  }),
                  created_at: "2026-06-27T05:10:00Z",
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
    renderApp("/work/SG-155?tab=verification")
    const user = userEvent.setup()

    expect(await screen.findByText("Gate run history")).toBeInTheDocument()
    // The deterministic run carries no judgment or evidence content, so it gets no disclosure.
    const whyButtons = await screen.findAllByRole("button", { name: "Why" })
    expect(whyButtons).toHaveLength(2)

    await user.click(whyButtons[0])
    expect(screen.getByText("Evaluated by platform model (gpt-5-mini) · confidence 0.85")).toBeInTheDocument()
    expect(screen.getByText("The working spec has not been promoted to canonical.")).toBeInTheDocument()

    await user.click(whyButtons[1])
    expect(screen.getByText("Agent-attested · confidence 1")).toBeInTheDocument()
    expect(screen.getByText("Doc registry CI passes")).toBeInTheDocument()
    expect(screen.getByText("coding-agent claim: satisfied")).toBeInTheDocument()
  })

  it("derives acceptance criteria state from the delivery verdict when present", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/workboard/change-requests/SG-155/acceptance-criteria")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "ac-1", text: "Doc registry CI passes", done: false, source: "spec" },
                { id: "ac-2", text: "Expected misses are quiet", done: false, source: "spec" },
                { id: "ac-3", text: "Text alone must not establish identity", done: false, source: "spec" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/work-items/SG-155/delivery-status?detail=true")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              change_request_id: "SG-155",
              found: true,
              verdict: "needs_changes",
              per_criterion: [
                { criterion_id: "ac-1", text: "Doc registry CI passes", verdict: "met", why: "CI evidence is green." },
                { criterion_id: "ac-2", text: "Expected misses are quiet", verdict: "unmet", why: "Noise remains in logs." },
                { criterion_id: "different-id", text: "Text alone must not establish identity", verdict: "met", why: "Wrong identity." },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/SG-155")

    expect(await screen.findByText("Doc registry CI passes")).toBeInTheDocument()
    expect(await screen.findByText("1/3")).toBeInTheDocument()
    expect(screen.getByText("Met")).toBeInTheDocument()
    expect(screen.getByText("Unmet")).toBeInTheDocument()
    expect(screen.queryByText("2/3")).not.toBeInTheDocument()
  })

  it("prioritizes a human-review delivery verdict in the work detail header", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = fixtureURL(input)
      if (url.endsWith("/workboard/change-requests/SG-155/acceptance-criteria")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                { id: "ac-1", text: "Delivered work leaves the attention list", done: false, source: "spec" },
                { id: "ac-2", text: "Status badge refreshes", done: false, source: "spec" },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      if (url.endsWith("/api/v1/work-items/SG-155/delivery-status?detail=true")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              change_request_id: "SG-155",
              gate_run_id: "platform-review-sg-155",
              completion_feedback_event_id: "completion-sg-155",
              found: true,
              verdict: "needs_human_review",
              executor: "platform",
              reviewed_at: "2026-07-03T14:10:28Z",
              hint: "One criterion is still unclear.",
              per_criterion: [
                { criterion_id: "ac-1", text: "Delivered work leaves the attention list", verdict: "unclear", why: "Needs proof." },
                { criterion_id: "ac-2", text: "Status badge refreshes", verdict: "met", why: "Covered." },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/SG-155")
    const user = userEvent.setup()

    expect(await screen.findByText("Delivered work leaves the attention list")).toBeInTheDocument()
    expect(screen.getByText("Needs review")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: /Inspect gaps/ })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Ask about review gaps/ })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /View handoff/ })).not.toBeInTheDocument()

    await user.click(screen.getByRole("tab", { name: "Verification" }))
    expect(screen.getByRole("button", { name: "Accept delivery" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Request changes" })).toBeInTheDocument()
  })
})
