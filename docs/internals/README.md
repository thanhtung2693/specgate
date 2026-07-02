# Maintainer internals

These documents support SpecGate contributors and maintainers. Product users
normally do not need them.

## Cross-module contracts

- [Contracts](../contracts.md) — shared statuses, payloads, governance, evidence, and integration contracts
- [Data model](../data-model.md) — logical entities and relationships
- [Testing strategy](../testing.md) — repository-wide test and debugging guidance
- [Machine-readable contracts](../contracts/) — evidence schema and fixtures
- [Governance conformance fixtures](../conformance/) — policy and gate compatibility cases

## Module documentation

- [Doc Registry](../../app/doc-registry/docs/) — Go service PRD, technical specification, and operations
- [Governance-ops](../../app/agents/docs/) — LangGraph governance-ops intent and contracts
- [Web UI](../../app/ui/docs/) — routes, UI behavior, onboarding, settings, and artifact/workflow surfaces
- [CLI agent rules](../../app/cli/AGENTS.md) — CLI architecture and output contracts

## Design history

Historical design documents are not currently checked into this docs tree.
Shipped behavior remains canonical in current contracts, module specs, code,
tests, and user documentation.

## Document layers

- PRD: product intent, goals, and non-goals
- Spec: behavior and implementation contract
- README: development flow and navigation
- User guide: supported workflow and successful product use

Changes to product behavior should update the narrowest canonical contract and
the affected user guide in the same change.
