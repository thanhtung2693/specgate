# Glossary

- **Acceptance criterion (AC)** — observable delivery condition with a stable ID,
  verification method, individual claim, verdict, and evidence links.
- **Artifact** — versioned package of planning or specification documents.
- **Artifact version** — immutable publication of one artifact package.
- **Canonical artifact** — current reviewed source of truth for a Feature.
- **Change request / work item** — one governed unit of delivery linked to a
  Feature.
- **Context Pack** — approved implementation contract assembled for a coding
  agent.
- **Delivery evidence** — checks, assertions, or external observations mapped to
  implementation and acceptance criteria.
- **Delivery review** — post-build evaluation of AC claims and evidence.
- **Executor** — place or actor running a gate: deterministic, IDE agent,
  platform LLM, human, or external producer.
- **Feature** — product capability with a canonical artifact and linked work.
- **Full route** — richer planning route for work requiring a full artifact
  bundle.
- **Gate** — versioned governance check returning a common verdict and evidence.
- **Gate package** — declarative package containing gate, policy, and binding
  definitions.
- **Governance level** — resolved control strength: `light`, `standard`, or
  `enhanced`.
- **Governance policy** — reusable versioned configuration selecting levels,
  gates, evidence, approval, and executors.
- **Handoff** — point where approved context becomes available to implementation.
- **Impact declaration** — author-provided structured risk and change-impact
  signals used by policy resolution.
- **Governed knowledge** — reusable documents indexed for governance reviews
  and summaries without becoming product source of truth.
- **Quick route** — streamlined planning route for small, understood work.
- **Readiness** — pre-implementation assessment of whether governed intent is
  complete and clear enough for the next step.
- **Resolved governance profile** — immutable policy snapshot stored with an
  artifact version.
- **Skill** — reusable rubric or agent instruction consumed by a gate or
  workflow.
- **Stale handoff** — Context Pack or approval based on an older source version.
- **Trust tier** — server-stamped evidence provenance; not a guarantee of
  semantic correctness.
- **Verdict** — common gate state: `pass`, `warn`, `fail`,
  `needs_human_review`, `not_applicable`, or `not_run`.

## Related

- [Artifacts and Context Packs](../concepts/artifacts-and-context-packs.md)
- [Governance and gates](../concepts/governance-and-gates.md)
- [Evidence reference](evidence.md)
