import { describe, expect, it } from "vitest"

import { summarizeDeliveryTrust, trustTierLabel } from "@/data/delivery-trust"

describe("delivery trust summary", () => {
  it("separates evidence, assurance, human decision, reviewer, and receipt", () => {
    expect(
      summarizeDeliveryTrust({
        found: true,
        verdict: "pass",
        judgeModel: "agent_attested",
        executor: "platform",
        gitReceipt: {
          headRevision: "abc123def4567890",
          changedFiles: [],
          warnings: [],
        },
        peerReview: { state: "stale" },
        criteria: [
          {
            id: "ac-1",
            text: "Run the test suite",
            verdict: "met",
            trustTier: "grounded",
          },
          {
            id: "ac-2",
            text: "Observe the merged pull request",
            verdict: "met",
            trustTier: "repository_observed",
          },
        ],
        checks: [],
      }),
    ).toEqual({
      evidence: "Ready for human review",
      assurance: "Agent-reported; local citation captured; Submitted commit observed on merged PR/MR",
      decision: "Awaiting human acceptance",
      reviewer: "Implementing agent",
      receipt: "Receipt recorded at commit abc123def456",
      peerReview: "Stale",
      modelReviewed: false,
    })
  })

  it("names an unavailable policy without treating it as an evidence failure", () => {
    expect(
      summarizeDeliveryTrust({
        found: true,
        verdict: "needs_human_review",
        reasonCode: "policy_unavailable",
        judgeModel: "deterministic_policy_guard",
        executor: "platform",
        criteria: [],
        checks: [],
      }),
    ).toMatchObject({
      evidence: "Policy unavailable",
      decision: "Awaiting human acceptance",
      reviewer: "Deterministic policy guard",
      receipt: "No Git receipt recorded",
      modelReviewed: false,
    })
  })

  it("includes repository corroboration in assurance", () => {
    const status = Object.assign(
      {
        found: true,
        verdict: "pass",
        criteria: [],
        checks: [],
      },
      { assuranceSources: ["repository_observed"] },
    )

    expect(summarizeDeliveryTrust(status).assurance).toBe(
      "Agent-reported; Submitted commit observed on merged PR/MR",
    )
  })

  it("shows that a newer completion is awaiting its own review", () => {
    expect(
      summarizeDeliveryTrust({
        found: true,
        verdict: "needs_human_review",
        reasonCode: "delivery_review_outdated",
        criteria: [],
        checks: [],
      }),
    ).toMatchObject({
      evidence: "Review pending for latest completion",
      decision: "Awaiting human acceptance",
      reviewer: "Reviewer not recorded",
    })
  })

  it("shows receipt availability and warnings instead of hiding them behind a commit", () => {
    expect(
      summarizeDeliveryTrust({
        found: true,
        verdict: "pass",
        gitReceipt: {
          availability: "available",
          headRevision: "abc123def4567890",
          changedFiles: [],
          warnings: ["Unrelated dirty checkout files were not included"],
        },
        criteria: [],
        checks: [],
      }).receipt,
    ).toBe("Receipt recorded at commit abc123def456; warning: Unrelated dirty checkout files were not included")

    expect(
      summarizeDeliveryTrust({
        found: true,
        verdict: "pass",
        gitReceipt: {
          availability: "unavailable",
          changedFiles: [],
          warnings: ["Git metadata could not be read"],
        },
        criteria: [],
        checks: [],
      }).receipt,
    ).toBe("Git receipt unavailable; warning: Git metadata could not be read")
  })

  it("identifies model review narrowly and does not bless unknown trust tiers", () => {
    expect(
      summarizeDeliveryTrust({
        found: true,
        verdict: "pass",
        judgeModel: "gpt-5.2",
        executor: "platform",
        criteria: [],
        checks: [],
      }).modelReviewed,
    ).toBe(true)
    expect(trustTierLabel("verified")).toBe("Unrecognized trust tier")
  })

  it("does not rewrite a human rejection as failed evidence", () => {
    expect(
      summarizeDeliveryTrust({
        found: true,
        verdict: "fail",
        evidenceVerdict: "pass",
        executor: "human",
        criteria: [],
        checks: [],
      }),
    ).toMatchObject({
      evidence: "Ready for human review",
      decision: "Rejected",
      reviewer: "Evidence reviewer not recorded",
      decisionActor: "Decision by human reviewer",
      modelReviewed: false,
    })
  })

  it("keeps model evidence review visible after a human decision", () => {
    expect(
      summarizeDeliveryTrust({
        found: true,
        verdict: "pass",
        evidenceVerdict: "needs_changes",
        executor: "human",
        actor: "lead@example.com",
        judgeModel: "gpt-5.2",
        criteria: [],
        checks: [],
      }),
    ).toMatchObject({
      evidence: "Evidence gaps found",
      decision: "Accepted",
      reviewer: "Platform model (gpt-5.2)",
      decisionActor: "Decision by lead@example.com",
      modelReviewed: true,
    })
  })
})
