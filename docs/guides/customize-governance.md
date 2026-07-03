# Customize governance

Use this guide to tune which gates run, how strictly evidence is judged, and
which team standards the checkers apply. All of these levers end up frozen
into each artifact version's profile snapshot, so changes apply to new
versions, not retroactively.

## Raise or lower the governance level

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
evidence expectations, human approval. The exact resolution and merge rules
are in the [Governance reference](../reference/governance.md#policy-resolution).
Verify what applied to a specific work item:

```bash
specgate work policy <work-ref>
```

## Apply team standards to a gate (Skill rubrics)

A Skill is a reusable team instruction. When a profile binds one to a gate,
the Skill's text is appended to that gate's prompt as policy, so the checker
judges by your standards.

1. Create the Skill in the web UI under Settings → Governance → Skills (the
   CLI reads them: `specgate skill list`, `specgate skill show <name>`).
2. Bind it in the profile's `gate_skills` map, for example
   `{"acceptance_criteria_verifiable": "spec-verifier"}`.
3. Re-run readiness on a draft artifact and confirm the hint reflects your
   rubric: `specgate gates check <artifact-id>`.

Treat rubric text as policy code: a vague or contradictory rubric degrades
the gate that carries it.

## Require corroborated delivery evidence

The profile's `evidence_policy` controls how much a passing delivery review
may rest on the agent's own report:

- `attested_ok` (default) — an evidence-backed agent report plus green
  checks can pass.
- `corroborated_required` — a pass additionally requires a matched merged-PR
  webhook event from a git integration; without one the verdict is clamped
  to `needs_human_review`.

Use `corroborated_required` once a git integration is connected and you want
no delivery accepted on self-reported evidence alone.

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
specgate gates check <artifact-id>       # readiness with the new profile
specgate work policy <work-ref>          # confirm the resolved policy
specgate delivery status <work-ref> --detail
```

## Related

- [Gate catalog](../reference/gates.md)
- [Governance reference](../reference/governance.md)
- [How verification works](../concepts/verification.md)
