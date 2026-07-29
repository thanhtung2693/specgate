# Who SpecGate is for

SpecGate serves two situations that fail in different ways. A solo developer
has no second person to catch an agent's mistakes, so every check has to be a
mechanism. A team has several people and machines sharing one governance
state, so decisions need attribution and nobody may accept their own work.

Everything in the solo group runs in Local mode: one binary, SQLite, no
Docker, no server, no model API key. The team group leans on Full mode.
[Feature status](../reference/feature-status.md) records which of these
surfaces are established and which are newer.

## Working solo: the mechanism is your second person

| Pain | What SpecGate does |
| --- | --- |
| The agent reports "done" but skipped a criterion, or its evidence does not hold | Delivery review reads every acceptance criterion separately, never as one aggregate. A criterion bound with `@check:<name>` takes its verdict from the named check result, not from the agent's claim. `change submit --run-checks` re-executes the reported commands, corrects any claim that does not reproduce, and reports `corrected N agent-reported check result(s)`. Each criterion's reason names who observed the deciding check — `observed by the SpecGate CLI` or `reported by the coding agent, not re-run`. A citation whose heading is not in the cited file is recorded as `heading_not_found` instead of being presented as verified. |
| A typo quietly disables enforcement | A malformed `@check:` binding is refused when the criterion is written — at `work create-quick` and at `change approve` — rather than silently treated as unbound. Unambiguous shapes (`@CHECK:unit`, trailing punctuation, a leading binding) are normalized instead of rejected. |
| A new session, or a new agent, loses the working context | Governance state lives outside the conversation. In a bound repository the installed session hook routes the agent to a lifecycle phase, `specgate change status <ref>` returns the state, the next actor, and the exact next command, and `specgate work context <ref>` returns the approved contract with a `context_digest`. A dead chat thread costs nothing that the store does not still hold. |
| Spec files in the repository: not committed, edited repeatedly, or multiplying | Publishing snapshots the file content into SpecGate's own store; the approved version is immutable and survives any later edit, rename, or deletion of the source. Sources are read from the working tree, so commit status does not matter, and SpecGate never moves, commits, or rewrites your files. A revision is a new version published against an explicit `base_version` and compared with `--preview --compare`; `specgate coverage` classifies every canonical spec as delivered, unfinished, stale, or uncovered. |
| The spec changed while the agent was implementing | Approval pins one exact version, and "latest file on disk" stops being the authority. The Context Pack carries the approved content and its digest, and the `spec_repo_drift` gate reports repository documents that contradict the approved artifact. |
| You vibe-code and do not know the commands | The installed skills and session hook route natural-language work — "implement the tagging feature I wrote up in docs/" — to the right lifecycle phase without you naming SpecGate or any command. `specgate <command> --help` covers the rest. |
| You cannot tell whether the process pays for itself | `specgate stats` reports first-pass yield, pre- and post-build governance signals, rework, and cycle time from recorded runs. These are signals for human interpretation, not proof. |

Two limits worth knowing up front:

- **Git receipts need an `origin` remote.** Without one, delivery evidence is
  not checked against later changes to the checkout, and status says so. A
  repository that has never been pushed does not get staleness detection.
- **Local mode is one machine.** The supported migration is
  `portable export` into a Full workspace, not syncing Local stores between
  machines.

## Working as a team: shared state needs attribution

| Pain | What SpecGate does |
| --- | --- |
| Nobody can prove which version was approved, by whom | Approval records the selected username against one immutable artifact version. `specgate audit <ref>` prints the governance trail — artifact events, gate runs, reviews, decisions — and `--verify` recomputes the tamper-evidence chain. |
| The implementer accepts their own work | The completion reporter cannot approve its own delivery, and a peer review must come from a different agent than the completion. A new completion always needs its own review and human decision. |
| Handing a review to a teammate without giving them a server | `delivery handoff export` writes a committable, checksummed review request; `delivery handoff show` renders it read-only and re-derives the verdict from the evidence it carries, so a stale bundle cannot present an old pass as current. A newer surface — the bundle schema can still change. |
| Several people and machines need one governance state | Full mode keeps workspaces on the appliance; any machine pointing at it sees the same state, and `workspace members` shows who is in it. A Local workspace moves in through `portable export` / `portable import --dry-run`. |
| Evidence was recorded against a different commit than the one being reviewed | `change status` compares the stored Git receipt — repository, branch, HEAD, diff digest — with the current checkout and reports mismatches as explicit stale warnings. Team repositories have remotes, so this check is active where it matters most. |
| A merged PR is never reconciled with the work it delivered | Full-mode GitHub/GitLab Repositories observe the merged PR/MR at the exact submitted commit and record the corroboration against the work item. SpecGate observes the merge itself; it does not read CI results. |
| Approved work needs to reach the tracker | Full-mode Linear Work tracking hands approved work to one selected team; direct IDE handoff stays available. |
| Team context never reaches the agent | Full-mode Knowledge stores workspace-scoped documents with embedding-backed search and cited retrieval, and Context Packs carry Knowledge provenance. A newer surface. |

## The boundary in one sentence

Solo, SpecGate replaces the reviewer you do not have with mechanisms that make
the agent prove each criterion. On a team, it replaces the argument about what
was approved and who accepted what with a record that lives outside chat
history and outside the repository.

## Related

- [Quickstart](../quickstart.md)
- [How SpecGate works](how-specgate-works.md)
- [Feature status](../reference/feature-status.md)
- [Trust and security](trust-and-security.md)
