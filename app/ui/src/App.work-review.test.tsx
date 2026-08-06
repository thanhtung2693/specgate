import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanupAppTest, defaultRegistryResponse, deliveredRegistryResponse, emptyRegistryResponse, registryWorkItems, renderApp, setupAppTest } from "./app-test-support"

describe("SpecGate UI shell: work review", () => {
  beforeEach(setupAppTest)
  afterEach(cleanupAppTest)

  it("surfaces delivered work in a dedicated queue chip instead of the action queue", async () => {
    vi.stubGlobal("fetch", vi.fn(deliveredRegistryResponse))
    renderApp("/work")
    const user = userEvent.setup()

    const deliveredChip = await screen.findByRole("button", { name: /^Delivered1$/ })
    const allWorkChip = screen.getByRole("button", { name: /^All work/ })
    expect(deliveredChip.compareDocumentPosition(allWorkChip) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.queryByText("Delivered settings polish")).not.toBeInTheDocument()

    await user.click(deliveredChip)

    expect(screen.getAllByText("Delivered settings polish").length).toBeGreaterThan(0)
    expect(screen.getAllByText("Accepted").length).toBeGreaterThan(0)
    expect(screen.queryByText("Pre-release verification sweep")).not.toBeInTheDocument()
  })

  it("includes acceptance-ready delivery in the Work needs-review queue", async () => {
    const readyForDecision = {
      ...registryWorkItems[0],
      id: "SG-READY",
      key: "SG-READY",
      title: "Acceptance-ready delivery",
      delivery_review: {
        verdict: "pass",
        hint: "Waiting for you to accept it.",
        reviewed_at: "2026-07-19T12:00:00Z",
      },
    }
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(
          new Response(JSON.stringify({ items: [readyForDecision] }), {
            headers: { "Content-Type": "application/json" },
          }),
        )
      }
      return defaultRegistryResponse(input, init)
    }))
    renderApp("/work")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("button", { name: /^Needs review1$/ }))

    expect(screen.getAllByText("Acceptance-ready delivery").length).toBeGreaterThan(0)
  })

  it("records a workspace-scoped human delivery acceptance and refreshes the work item", async () => {
    const readyForDecision = {
      id: "cr-ready",
      key: "CR-READY",
      title: "Acceptance-ready delivery",
      phase: "Review",
      delivery_review: {
        verdict: "pass",
        hint: "Waiting for you to accept it.",
        reviewed_at: "2026-07-19T12:00:00Z",
      },
    }
    let workboardReads = 0
    let deliveryReads = 0
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input))
      if (url.pathname === "/workboard/change-requests" && init?.method !== "PATCH") {
        workboardReads += 1
        return Promise.resolve(new Response(JSON.stringify({ items: [readyForDecision] }), {
          headers: { "Content-Type": "application/json" },
        }))
      }
      if (url.pathname === "/api/v1/work-items/cr-ready/delivery-status") {
        deliveryReads += 1
        return Promise.resolve(new Response(JSON.stringify({
          change_request_id: "cr-ready",
          gate_run_id: "platform-review-1",
          completion_feedback_event_id: "completion-1",
          found: true,
          verdict: "pass",
          executor: "platform",
          reviewed_at: "2026-07-19T12:00:00Z",
          git_receipt: { head_revision: "abc123def456" },
        }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.pathname === "/api/v1/work-items/cr-ready/delivery-decision") {
        return Promise.resolve(new Response(JSON.stringify({
          change_request_id: "cr-ready",
          verdict: "pass",
          executor: "human",
          actor: "thanhtung",
        }), { headers: { "Content-Type": "application/json" } }))
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-READY")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Verification" }))
    await user.click(await screen.findByRole("button", { name: "Accept delivery" }))

    const dialog = screen.getByRole("dialog", { name: "Accept delivery" })
    expect(within(dialog).getByText(/CR-READY · Acceptance-ready delivery/)).toBeInTheDocument()
    expect(within(dialog).getByText(/abc123def456/)).toBeInTheDocument()
    expect(within(dialog).getByText(/completion-1/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole("button", { name: "Accept delivery" }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://registry.test/api/v1/work-items/cr-ready/delivery-decision?workspace_id=workspace-main",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            decision: "approve",
            actor: "thanhtung",
            reviewed_gate_run_id: "platform-review-1",
            completion_feedback_event_id: "completion-1",
          }),
        }),
      )
    })
    await waitFor(() => {
      expect(workboardReads).toBeGreaterThan(1)
      expect(deliveryReads).toBeGreaterThan(1)
    })
  })

  it("requires useful request-changes feedback and shows backend rejection", async () => {
    let decisionCalls = 0
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = new URL(String(input))
      if (url.pathname === "/workboard/change-requests") {
        return Promise.resolve(new Response(JSON.stringify({ items: [{
          id: "cr-ready",
          key: "CR-READY",
          title: "Acceptance-ready delivery",
          phase: "Review",
          delivery_review: { verdict: "pass" },
        }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.pathname === "/api/v1/work-items/cr-ready/delivery-status") {
        return Promise.resolve(new Response(JSON.stringify({
          found: true,
          gate_run_id: "platform-review-1",
          completion_feedback_event_id: "completion-1",
          verdict: "pass",
          executor: "platform",
          reviewed_at: "2026-07-19T12:00:00Z",
        }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.pathname === "/api/v1/work-items/cr-ready/delivery-decision") {
        decisionCalls += 1
        return Promise.resolve(new Response(JSON.stringify({
          detail: "the latest completion has not been reviewed; run delivery review before recording a human decision",
        }), { status: 422, headers: { "Content-Type": "application/problem+json" } }))
      }
      return emptyRegistryResponse(input)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/work/CR-READY")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Verification" }))
    await user.click(await screen.findByRole("button", { name: "Request changes" }))
    const dialog = screen.getByRole("dialog", { name: "Request changes" })
    expect(within(dialog).getByText(/Completion completion-1/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole("button", { name: "Request changes" }))

    expect(within(dialog).getByRole("alert")).toHaveTextContent("Describe what must change before resubmission.")
    expect(decisionCalls).toBe(0)

    await user.type(within(dialog).getByPlaceholderText(/Describe specific evidence/), "Add narrow viewport evidence.")
    await user.click(within(dialog).getByRole("button", { name: "Request changes" }))

    expect(await within(dialog).findByRole("alert")).toHaveTextContent("the latest completion has not been reviewed")
    expect(decisionCalls).toBe(1)
    expect(fetchMock).toHaveBeenCalledWith(
      "http://registry.test/api/v1/work-items/cr-ready/delivery-decision?workspace_id=workspace-main",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          decision: "reject",
          actor: "thanhtung",
          reviewed_gate_run_id: "platform-review-1",
          completion_feedback_event_id: "completion-1",
          note: "Add narrow viewport evidence.",
        }),
      }),
    )
  })

  it("clears delivery feedback when cancelling a decision", async () => {
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
      const url = new URL(String(input))
      if (url.pathname === "/workboard/change-requests") {
        return Promise.resolve(new Response(JSON.stringify({ items: [{
          id: "cr-ready",
          key: "CR-READY",
          title: "Acceptance-ready delivery",
          phase: "Review",
          delivery_review: { verdict: "pass" },
        }] }), { headers: { "Content-Type": "application/json" } }))
      }
      if (url.pathname === "/api/v1/work-items/cr-ready/delivery-status") {
        return Promise.resolve(new Response(JSON.stringify({
          found: true,
          gate_run_id: "platform-review-1",
          completion_feedback_event_id: "completion-1",
          verdict: "pass",
          executor: "platform",
        }), { headers: { "Content-Type": "application/json" } }))
      }
      return emptyRegistryResponse(input)
    }))
    renderApp("/work/CR-READY")
    const user = userEvent.setup()

    await user.click(await screen.findByRole("tab", { name: "Verification" }))
    await user.click(await screen.findByRole("button", { name: "Request changes" }))
    let dialog = screen.getByRole("dialog", { name: "Request changes" })
    await user.type(within(dialog).getByRole("textbox"), "Old rejection note")
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }))

    await user.click(screen.getByRole("button", { name: "Accept delivery" }))
    dialog = screen.getByRole("dialog", { name: "Accept delivery" })
    expect(within(dialog).getByRole("textbox")).toHaveValue("")
  })

  it("shows a View verdict action without an unsupported review-summary prompt for delivered work", async () => {
    vi.stubGlobal("fetch", vi.fn(deliveredRegistryResponse))
    renderApp("/work/SG-160")
    const user = userEvent.setup()

    expect((await screen.findAllByRole("heading", { name: "Delivered settings polish" })).length).toBeGreaterThan(0)
    expect(screen.queryByRole("button", { name: "View handoff" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Ask about handoff blockers" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Ask for review summary" })).not.toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "View verdict" }))

    expect(screen.getByRole("tab", { name: "Verification", selected: true })).toBeInTheDocument()
    expect(await screen.findByText("Delivery review")).toBeInTheDocument()
  })

  it("excludes delivered work from the review queue count", async () => {
    vi.stubGlobal("fetch", vi.fn(deliveredRegistryResponse))
    renderApp("/reviews")

    expect(await screen.findByText("2 items need review")).toBeInTheDocument()
    expect(screen.queryByText("Delivered settings polish")).not.toBeInTheDocument()
  })

  it("pluralizes the review count heading for a single item", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(
          new Response(
            JSON.stringify({ items: [registryWorkItems.find((item) => item.id === "SG-147")] }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/reviews")

    expect(await screen.findByText("1 item needs review")).toBeInTheDocument()
    expect(screen.queryByText(/items need review/)).not.toBeInTheDocument()
  })

  it("uses the authoritative delivery review verdict in the review queue", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [
                {
                  id: "cr-review",
                  key: "CR-REVIEW",
                  title: "Needs human review",
                  delivery_review: {
                    verdict: "needs_human_review",
                    hint: "Missing one browser check.",
                    reviewed_at: "2026-07-03T14:10:28Z",
                  },
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
    renderApp("/reviews")

    expect(await screen.findByText("1 item needs review")).toBeInTheDocument()
    expect(screen.getByText("Needs review")).toBeInTheDocument()
    expect(screen.getByText("Missing one browser check.")).toBeInTheDocument()
    expect(screen.queryByText("Ready for review")).not.toBeInTheDocument()
  })

  it("keeps acceptance-ready review copy ahead of the Review phase proxy", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes("/workboard/change-requests") && init?.method !== "PATCH") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: [{
                id: "cr-ready-review",
                key: "CR-READY-REVIEW",
                title: "Acceptance-ready review",
                phase: "Review",
                delivery_review: {
                  verdict: "pass",
                  hint: "Waiting for you to accept it.",
                  reviewed_at: "2026-07-19T12:00:00Z",
                },
              }],
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
        )
      }
      return defaultRegistryResponse(input, init)
    })
    vi.stubGlobal("fetch", fetchMock)
    renderApp("/reviews")

    const row = await screen.findByRole("row", { name: /CR-READY-REVIEW/ })
    expect(within(row).getByText("Ready for human review")).toBeInTheDocument()
    expect(within(row).getByText("Waiting for you to accept it.")).toBeInTheDocument()
    expect(within(row).getByRole("link", { name: "Inspect review outcome" })).toBeInTheDocument()
    expect(within(row).queryByText("A required gate failed.")).not.toBeInTheDocument()
  })

  it("drops the Owner column from the work queue table", async () => {
    renderApp("/work")

    expect(await screen.findByText("Work queue")).toBeInTheDocument()
    expect(await screen.findByRole("columnheader", { name: "Waiting on" })).toBeInTheDocument()
    expect(screen.queryByText("Owner")).not.toBeInTheDocument()
  })

  it("labels pickup-ready queue rows without implying delivery passed", async () => {
    renderApp("/work")

    const row = await screen.findByRole("row", { name: /SG-151/ })

    // The badge is the row's only state label now, so it has to carry pickup
    // readiness without borrowing the vocabulary of an accepted delivery.
    expect(within(row).getByText("Ready for pickup")).toBeInTheDocument()
    expect(within(row).queryByText("Passed")).not.toBeInTheDocument()
    const blockerCell = row.querySelector("[data-slot='work-blocker']") as HTMLElement
    expect(blockerCell).not.toBeNull()
    expect(blockerCell.textContent).not.toContain("none")
  })

  it("labels pickup-ready work detail without implying delivery passed", async () => {
    renderApp("/work/SG-151")

    expect(await screen.findByRole("heading", { name: "Agent skills setup primitives" })).toBeInTheDocument()
    expect(screen.getByText("Ready for pickup")).toBeInTheDocument()
    expect(screen.queryByText("Passed")).not.toBeInTheDocument()
  })

  it("drops the Owner column from the review queue table", async () => {
    renderApp("/reviews")

    expect(await screen.findByText("Finished work")).toBeInTheDocument()
    expect(await screen.findByText("Pre-release verification sweep")).toBeInTheDocument()
    expect(screen.queryByText("Owner")).not.toBeInTheDocument()
  })

  it("drops Owner and Detail source from the work context panel", async () => {
    renderApp("/work/SG-142")

    expect(await screen.findByText("Work context")).toBeInTheDocument()
    expect(screen.getByText("Waiting on")).toBeInTheDocument()
    expect(screen.queryByText("Owner")).not.toBeInTheDocument()
    expect(screen.queryByText("Detail source")).not.toBeInTheDocument()
  })

  it("keeps Work read-only and exposes a copyable CLI resume command", async () => {
    renderApp("/work/SG-155")

    expect(await screen.findByRole("button", { name: "Copy resume command" })).toBeEnabled()
    expect(screen.queryByText("Route decision")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Create quick Context Pack/ })).not.toBeInTheDocument()
  })

  it("refreshes Work explicitly and reports the last successful refresh", async () => {
    renderApp("/work")
    const user = userEvent.setup()

    const refresh = await screen.findByRole("button", { name: "Refresh work" })
    await user.click(refresh)

    expect(await screen.findByText(/Last refreshed/)).toBeInTheDocument()
  })
})
