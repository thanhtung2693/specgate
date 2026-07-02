import { describe, expect, it } from "vitest"

import type { WorkItem } from "@/data/workspace"

import { reviewTone, statusTone } from "./shared"

describe("statusTone", () => {
  it("maps gate statuses", () => {
    expect(statusTone("gate", "pass")).toBe("success")
    expect(statusTone("gate", "pending")).toBe("warning")
    expect(statusTone("gate", "fail")).toBe("danger")
  })

  it("maps gate-run states", () => {
    expect(statusTone("state", "pass")).toBe("success")
    expect(statusTone("state", "not_applicable")).toBe("success")
    expect(statusTone("state", "fail")).toBe("danger")
    expect(statusTone("state", "warn")).toBe("warning")
    expect(statusTone("state", "needs_human_review")).toBe("warning")
    expect(statusTone("state", "pending")).toBe("warning")
    expect(statusTone("state", "unknown")).toBe("neutral")
  })

  it("maps severities case-insensitively", () => {
    expect(statusTone("severity", "Warning")).toBe("warning")
    expect(statusTone("severity", "warn")).toBe("warning")
    expect(statusTone("severity", "ERROR")).toBe("danger")
    expect(statusTone("severity", "danger")).toBe("danger")
    expect(statusTone("severity", "critical")).toBe("danger")
    expect(statusTone("severity", "blocking")).toBe("danger")
    expect(statusTone("severity", "info")).toBe("neutral")
  })

  it("maps artifact statuses", () => {
    expect(statusTone("artifact", "approved")).toBe("success")
    expect(statusTone("artifact", "needs_changes")).toBe("warning")
    expect(statusTone("artifact", "draft")).toBe("neutral")
  })

  it("maps plugin statuses", () => {
    expect(statusTone("plugin", "installed")).toBe("success")
    expect(statusTone("plugin", "needs refresh")).toBe("warning")
    expect(statusTone("plugin", "not installed")).toBe("neutral")
  })


  it("maps webhook event statuses", () => {
    expect(statusTone("webhookEvent", "processed")).toBe("success")
    expect(statusTone("webhookEvent", "failed")).toBe("danger")
    expect(statusTone("webhookEvent", "pending")).toBe("warning")
    expect(statusTone("webhookEvent", "unknown")).toBe("neutral")
  })

  it("maps integration statuses", () => {
    expect(statusTone("integration", "connected")).toBe("success")
    expect(statusTone("integration", "error")).toBe("danger")
    expect(statusTone("integration", "disabled")).toBe("neutral")
    expect(statusTone("integration", "not connected")).toBe("neutral")
  })

  it("maps policy levels", () => {
    expect(statusTone("policy", "enhanced")).toBe("warning")
    expect(statusTone("policy", "light")).toBe("success")
    expect(statusTone("policy", "standard")).toBe("neutral")
  })

  it("maps feedback signal statuses", () => {
    expect(statusTone("feedbackSignal", "received")).toBe("warning")
    expect(statusTone("feedbackSignal", "pending")).toBe("warning")
    expect(statusTone("feedbackSignal", "accepted")).toBe("success")
    expect(statusTone("feedbackSignal", "processed")).toBe("success")
    expect(statusTone("feedbackSignal", "rejected")).toBe("neutral")
    expect(statusTone("feedbackSignal", "ignored")).toBe("neutral")
  })

  it("keeps per-domain behavior for the same status value", () => {
    // "pending" diverges by domain: warning for gates, neutral for plugins.
    expect(statusTone("gate", "pending")).toBe("warning")
    expect(statusTone("plugin", "pending")).toBe("neutral")
    expect(statusTone("artifact", "pending")).toBe("neutral")
  })
})

describe("reviewTone", () => {
  function item(overrides: Partial<Pick<WorkItem, "delivery" | "gate">>): Pick<WorkItem, "delivery" | "gate"> {
    return { delivery: "not_started", gate: "pending", ...overrides }
  }

  it("flags needs_changes and gate failures as warning", () => {
    expect(reviewTone(item({ delivery: "needs_changes" }) as WorkItem)).toBe("warning")
    expect(reviewTone(item({ gate: "fail" }) as WorkItem)).toBe("warning")
    expect(reviewTone(item({ delivery: "ready", gate: "fail" }) as WorkItem)).toBe("warning")
  })

  it("marks ready and passed deliveries as success", () => {
    expect(reviewTone(item({ delivery: "ready" }) as WorkItem)).toBe("success")
    expect(reviewTone(item({ delivery: "passed" }) as WorkItem)).toBe("success")
  })

  it("defaults to neutral", () => {
    expect(reviewTone(item({}) as WorkItem)).toBe("neutral")
  })
})
