# Glossary

| Term | Meaning |
|---|---|
| Acceptance criterion | Observable delivery condition with evidence and review verdict. |
| Artifact | Versioned package of planning or specification documents. |
| Artifact version | Immutable publication of one artifact package. |
| Canonical artifact | Current reviewed source of truth for a feature. |
| Change request | Governed unit of delivery. Also called a work item. |
| Context Pack | Approved implementation contract assembled for a coding agent. |
| Delivery evidence | Checks, claims, files, links, or external observations mapped to delivery. |
| Delivery review | Post-implementation review of evidence against acceptance criteria and policy. |
| Executor | Actor or system that runs a gate: deterministic code, IDE agent, platform model, human, or integration. |
| Feature | Product capability with linked work and an optional canonical artifact. |
| Full route | Planning route based on a reviewed artifact bundle. |
| Gate | Versioned governance check that returns a common verdict and evidence. |
| Governance level | Resolved control strength: `light`, `standard`, or `enhanced`. |
| Governance policy | Versioned configuration for levels, gates, evidence, approval, and executors. |
| Handoff | Point where approved context becomes available for implementation. |
| Impact declaration | Structured author signal about risk, blast radius, rollback, and protected domains. |
| Knowledge document | Reusable material indexed for governance assistance without becoming product source of truth. |
| Quick route | Lightweight route for small, understood work with direct acceptance criteria. |
| Readiness | Pre-implementation assessment of whether intent is clear enough to hand off. |
| Resolved governance profile | Immutable policy snapshot stored with an artifact version. |
| Skill | Reusable rubric or agent instruction used by gates or workflows. |
| Stale handoff | Context Pack or approval based on an older source version. |
| Trust tier | Server-stamped evidence provenance; not proof of semantic correctness. |
| Verdict | Gate state such as `pass`, `warn`, `fail`, `needs_human_review`, `not_applicable`, or `not_run`. |

## Related

- [How SpecGate works](../concepts/how-specgate-works.md)
- [Artifacts and Context Packs](../concepts/artifacts-and-context-packs.md)
- [Governance and gates](../concepts/governance-and-gates.md)
