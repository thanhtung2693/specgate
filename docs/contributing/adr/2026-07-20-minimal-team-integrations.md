# ADR: Minimal team integration boundary

## Status

Accepted 2026-07-20.

This decision narrows the first-release provider scope described by the
[delivery-verdict trust model](2026-07-07-delivery-trust-model.md). It removes
provider CI corroboration from the first release and requires exact completion
matching before a merged PR or MR counts as repository-observed evidence. It
does not change deterministic check bindings, peer review, or human delivery
authority.

## Context

SpecGate has connection, resource-selection, webhook, tracker-adapter, and
delivery-link plumbing for GitHub, GitLab, and Linear. The pre-alpha behavior
groups those capabilities together and exposes broader claims than the normal
setup path can satisfy. A managed webhook receives only selected-resource PR/MR
or Linear events; it does not ingest provider CI state or create CI assurance.

The first team workflow should be useful after a user authorizes a provider and
selects a repository or team. It must not require CI/CD knowledge, duplicate
SpecGate authority in an external tracker, or introduce a synchronization
platform before users need one.

## Decision

SpecGate exposes two integration capabilities:

| Product capability | First-release providers | Purpose |
| --- | --- | --- |
| Repositories | GitHub, GitLab | Link provider-observed PR or MR delivery to SpecGate work |
| Work tracking | Linear | Optionally dispatch approved work to a team or cloud coding agent |

GitHub and GitLab Issues are not work-tracking destinations in the first
release. Linear does not produce repository evidence. These are product
boundaries, not a new provider-capability framework.

Integrations remain a Full-mode team feature. Local mode keeps its direct
coding-agent workflow and gains no integration UI.

## User workflow

The repository path is:

1. Authorize GitHub or GitLab.
2. Select a repository or project.
3. SpecGate provisions the managed, signed webhook.
4. A coding agent opens a PR or MR carrying the exact SpecGate work marker.
5. SpecGate links provider events to the work item and reports their current
   repository-observation state.

The optional work-tracking path is:

1. Authorize Linear and select a team.
2. From the Full work detail, explicitly choose **Hand off to Linear** for one
   approved work item.
3. SpecGate creates one Linear issue containing the work summary, acceptance
   criteria, exact work marker, authenticated Context Pack pickup command, and
   PR or MR marker instruction.
4. The coding agent authenticates to the same SpecGate workspace and reads the
   approved Context Pack.

A user may skip Linear and dispatch the same approved work directly to a coding
IDE agent. Both paths use the same Context Pack and delivery lifecycle.

## Linear authority and idempotency

A work item has at most one primary Linear handoff. Repeating or concurrently
requesting the handoff returns the existing link rather than creating another
issue. A provider failure does not change the work item's approval state and
does not persist a successful tracker link; the handoff remains retryable.

Linear workflow state is informational. Closing a Linear issue never approves
or rejects SpecGate delivery. After a human accepts the exact completion in
SpecGate, SpecGate may best-effort move the linked issue to the team's completed
state. That external transition cannot make the durable acceptance fail.

## Repository observation

A PR or MR may count as `repository_observed` only when:

1. the provider webhook signature is valid;
2. the event belongs to the selected repository or project;
3. the PR or MR contains the exact SpecGate work marker;
4. the provider reports the PR or MR as merged; and
5. its head SHA equals the latest completion's
   `git_receipt.head_revision`.

An open PR or MR is linked but not corroborating. A merged PR or MR with a
missing or different head SHA remains visible as stale and does not satisfy a
corroboration requirement. A newer completion makes an earlier match stale
until a provider event observes the new submitted head.

The provider merge commit SHA is inspection metadata. A squash or rebase merge
may produce a different merge SHA, so SpecGate says that the submitted commit
was observed on a merged PR or MR; it does not claim that the submitted commit
itself became the merge commit.

Repository observation independently confirms provider activity. It does not
claim that tests ran, that CI passed, or that the implementation is correct.
Human acceptance remains a separate decision.

## Storage and failure boundaries

The first release reuses the existing integration storage:

- `integrations` and `integration_resources` own provider connections and
  selected repositories, projects, and teams;
- `tracker_links` stores the single Linear handoff;
- `integration_delivery_links` stores the linked PR or MR, including its head
  and merge SHAs;
- webhook and governance-feedback rows retain the append-only provider trail.

No new integration table, generic synchronization engine, polling loop, or
background reconciliation process is introduced.

Invalid signatures are rejected. Events for another resource cannot mutate or
corroborate work. Events without an exact marker, and duplicate deliveries, are
recorded or ignored without creating evidence. A delivery whose prior database
transaction failed remains retryable: one redelivery atomically reclaims it,
while processed or already-running duplicates remain no-ops. A webhook-provisioning failure
leaves the repository visibly unconnected and retryable. A Full deployment's
callback URL must be reachable by the provider.

## First-release non-goals

- CI workflow or pipeline setup, ingestion, and CI-based assurance claims;
- GitHub or GitLab issue handoff;
- automatic handoff after approval;
- multiple or mirrored tracker issues;
- tracker-driven delivery acceptance;
- public or signed Context Pack URLs;
- cross-provider state synchronization;
- background retries or reconciliation.

These capabilities require observed user demand and their own trust and failure
contracts.

## Verification expectations

Provider-contract tests must cover managed hook provisioning, signatures,
workspace and resource isolation, exact-marker correlation, exact-head
matching, stale and duplicate events, Linear issue creation, handoff
idempotency, retry after provider failure, and the human-acceptance boundary.
The UI must cover both direct-agent and Linear-handoff paths and render
actionable repository states.

Changes to this capability require one real-provider dogfood pass for GitHub,
GitLab, and Linear using disposable resources. Product documentation and
provider scope text must describe the same boundary. Mock-only verification is
not enough for a release that changes the team-integration contract.

## Consequences

- A normal hosted-provider setup is authorize, select, and done.
- Repository and work-tracking responsibilities are legible in the UI and
  documentation.
- Teams can dispatch through Linear without making Linear mandatory for direct
  IDE-agent work.
- Enhanced evidence can use an exact-completion merged-PR or merged-MR
  observation without misrepresenting CI.
- Existing generic tracker and dormant CI code outside this boundary should be
  removed rather than advertised as experimental first-release behavior.
