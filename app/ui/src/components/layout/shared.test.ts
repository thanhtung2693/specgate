import { describe, expect, it } from "vitest"

import type { WorkItem } from "@/data/workspace"

import { gateCatalog, gateChecks, parseGateEvidence, reviewTone, statusTone } from "./shared"

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

describe("gateCatalog", () => {
  it("explains what each catalog gate checks and reads in one sentence", () => {
    for (const [gate, entry] of Object.entries(gateCatalog)) {
      expect(entry.name.trim().length, gate).toBeGreaterThan(0)
      expect(entry.checks, gate).toMatch(/^(Checks|Judges) /)
    }
    expect(gateChecks("spec_completeness")).toBe(
      "Checks the package covers its required topics (outcomes, criteria, risks…). Reads every document in the package.",
    )
    expect(gateChecks("delivery_review")).toBe("Judges the delivery evidence against every acceptance criterion.")
    expect(gateChecks("unknown_gate")).toBeUndefined()
  })
})

describe("parseGateEvidence", () => {
  it("returns nothing for empty or content-free evidence", () => {
    expect(parseGateEvidence(undefined)).toBeUndefined()
    expect(parseGateEvidence("")).toBeUndefined()
    expect(parseGateEvidence("{}")).toBeUndefined()
    expect(parseGateEvidence('""')).toBeUndefined()
    expect(parseGateEvidence('{"unrelated": true}')).toBeUndefined()
  })

  it("treats non-JSON evidence as a plain supporting quote", () => {
    expect(parseGateEvidence("Section 3 names explicit non-goals.")).toEqual({
      quote: "Section 3 names explicit non-goals.",
      rows: [],
    })
  })

  it("reads the gate-run envelope: platform judge, confidence, and inner quote", () => {
    const details = parseGateEvidence(
      JSON.stringify({
        evidence_contract_version: "gate-run-v1",
        evaluator: { type: "agent_judge", judge_model: "gpt-5-mini" },
        confidence: 0.42,
        evidence: "Judge confidence below threshold",
      }),
    )
    expect(details).toMatchObject({
      evaluator: "platform_model",
      judgeModel: "gpt-5-mini",
      confidence: 0.42,
      quote: "Judge confidence below threshold",
    })
  })

  it("omits judge and confidence for deterministic runs with no evidence", () => {
    const details = parseGateEvidence(
      JSON.stringify({
        evidence_contract_version: "gate-run-v1",
        evaluator: { type: "deterministic", judge_model: "deterministic-v1" },
        confidence: 1,
        evidence: "",
      }),
    )
    expect(details).toBeUndefined()
  })

  it("labels agent-attested runs and unpacks delivery-review criteria and checks", () => {
    const details = parseGateEvidence(
      JSON.stringify({
        evaluator: { type: "agent_judge", judge_model: "agent_attested" },
        confidence: 1,
        evidence: JSON.stringify({
          criteria: [{ criterion_id: "ac-1", text: "CI passes", verdict: "met", why: "coding-agent claim: satisfied" }],
          checks: [{ name: "tests", status: "pass", detail: "146 passed" }],
        }),
      }),
    )
    expect(details?.evaluator).toBe("agent")
    expect(details?.rows).toEqual([
      { label: "CI passes", state: "met", why: "coding-agent claim: satisfied" },
      { label: "tests", state: "pass", why: "146 passed" },
    ])
  })

  it("unpacks per-topic completeness evidence with the summary as quote", () => {
    const details = parseGateEvidence(
      JSON.stringify({
        topics: [{ topic: "risks", status: "missing", why: "No risk section found." }],
        summary: "Spec is missing risk coverage.",
      }),
    )
    expect(details?.quote).toBe("Spec is missing risk coverage.")
    expect(details?.rows).toEqual([{ label: "risks", state: "missing", why: "No risk section found." }])
  })

  it("labels ide_agent executor evidence as agent-attested and keeps string findings", () => {
    const details = parseGateEvidence(
      JSON.stringify({ executor: "ide_agent", findings: ["Scope bounded in spec §2", { raw: "dropped" }] }),
    )
    expect(details?.evaluator).toBe("agent")
    expect(details?.rows).toEqual([{ label: "Scope bounded in spec §2" }])
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
