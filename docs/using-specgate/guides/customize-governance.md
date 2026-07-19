# Customize governance

Use this guide to tune automatic governance through artifact metadata and team
rubrics. The resolved policy ends up frozen into each artifact version's
snapshot, so changes apply to new versions, not retroactively.

## Raise the governance level

The level (`light`, `standard`, `enhanced`) resolves from work and artifact
metadata — most directly from the declared impact. Declare impact honestly at
publish time:

```json
{
  "impact_level": "high",
  "impact_declaration": {
    "protected_domains_status": "yes",
    "data_or_schema_change": "yes"
  }
}
```

Higher impact resolves a stronger policy: more required topics, stronger
evidence expectations, human approval. The exact resolution and raise-only rules
are in the [Governance reference](../reference/governance.md#policy-resolution).
Verify what applied to a specific work item:

```bash
specgate work policy <work-ref>
```

## Apply team standards to a gate (Skill rubrics)

A Skill is a reusable team instruction. When automatic policy binds one to a
gate, the Skill's text is appended to that gate's prompt as policy, so the
checker judges by your standards.

1. Create or update the named Skill in the web UI under Settings → Governance → Skills (the
   CLI reads them: `specgate skill list`, `specgate skill show <name>`).
2. Use the fixed Skill names that automatic policy tiers already bind, such as
   `spec-review`, `prd-review`, `acceptance-criteria`, `task-breakdown`,
   `rollout-risk`, and `review-impl`.
3. Re-run readiness on a draft artifact and confirm the hint reflects your
   rubric: `specgate gates check <artifact-id>`.

Treat rubric text as policy code: a vague or contradictory rubric degrades
the gate that carries it.

Each artifact version freezes the automatically resolved policy snapshot. You
do not edit that snapshot directly. To change future behavior, update the
underlying fixed-name Skill rubric or publish a new artifact version with a
different impact declaration so automatic policy resolves again.

## Require corroborated delivery evidence

The resolved policy's `evidence_policy` controls how much an authoritative delivery
review pass may rest on the agent's own report:

- `attested_ok` (default) — a platform-model review of an evidence-backed
  report plus green checks can pass. Without a platform model, an
  agent-attested pass still needs a bound peer review or human decision.
- `corroborated_required` — a pass additionally requires independent evidence:
  either a merged PR/MR repository event whose `head_sha` matches the latest
  completion receipt's `head_revision`, or every criterion resolved through
  canonical deterministic check bindings. Without that, the verdict is clamped
  to `needs_human_review`. CI is not a first-release assurance source.

Use `corroborated_required` once a git integration or deterministic check
bindings are available and you want no delivery accepted on self-reported
evidence alone.

## Give gates more grounding

Two inputs improve gate judgment without any configuration:

- **Attachments** — pin bug reproductions, examples, or design links to the
  feature with the `gate` (or `both`) audience; the acceptance-criteria and
  scope gates receive them.
- **Verification documents** — the acceptance-criteria gates read the
  `verification` role. A test plan in the package directly improves those
  verdicts.

## Verify a customization end to end

```bash
specgate gates check <artifact-id>       # readiness with the resolved policy
specgate work policy <work-ref>          # confirm the resolved policy
specgate delivery status <work-ref> --detail
```

## Related

- [Gate catalog](../reference/gates.md)
- [Governance reference](../reference/governance.md)
- [How verification works](../concepts/verification.md)
