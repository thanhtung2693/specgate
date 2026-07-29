# Who SpecGate is for

SpecGate helps in two situations, and they fail in different ways.

Working solo, nobody double-checks what the coding agent did. Working in a
team, people need to agree on what was approved and who accepted what.

Everything in the solo list works in Local mode: one binary, no Docker, no
server, no model API key. The team list mostly needs Full mode.

## Working solo

You have no reviewer, so SpecGate plays that role with checks instead of trust.

| The problem | What SpecGate does about it |
| --- | --- |
| The agent says "done" but skipped something, or its proof is wrong | SpecGate goes through your acceptance criteria one by one — never as a single "looks good". If a criterion is tied to a test, the test result decides, not the agent's word. On submit, SpecGate can rerun the checks the agent claims it ran and correct any claim that does not reproduce. It also tells you, per criterion, whether a result was actually observed or only reported by the agent. |
| A small typo turns a check off without you noticing | If SpecGate cannot tell which check you meant, it refuses right when you write the criterion, instead of quietly skipping the check later. |
| You open a new chat and the agent forgets where things stood | Progress lives in SpecGate, not in the conversation. In a set-up repository, a new session finds the open work, its state, and the exact next command by itself. Losing a chat thread loses nothing. |
| Spec files pile up in the repo and you do not want to commit them | Publishing takes a snapshot into SpecGate's own store. The approved version is frozen there, so the file in your repo can stay uncommitted, change, or be deleted — the approved copy survives. SpecGate never touches your files. |
| You changed the spec while the agent was still implementing | Approval pins one exact version. "Whatever the file says right now" stops being the authority, and SpecGate flags repo documents that contradict the approved version. |
| You vibe-code and do not know any SpecGate commands | You do not need to. Say "implement the feature I wrote up in docs/" and the installed plugin routes the agent through the right steps. |
| You wonder whether the process is worth it | `specgate stats` shows how often work passed review on the first try, what got caught, and how much rework happened — from your real history. |

Two limits, stated up front:

- **Staleness detection needs a Git remote.** In a repository that has never
  been pushed, SpecGate cannot warn you that evidence was recorded against
  older code — and it tells you so rather than pretending.
- **Local mode is one machine.** To move to a shared setup, export the
  workspace into Full mode; Local stores are not synced between machines.

## Working as a team

Several people share one governance state, so decisions need names on them.

| The problem | What SpecGate does about it |
| --- | --- |
| "Which version did we approve, and who approved it?" | Every approval is recorded with a name against one frozen version. `specgate audit` prints the whole history of a work item and can verify nothing was tampered with. |
| The person who did the work accepts their own work | Not allowed. Whoever reported the completion cannot approve it, and a second review must come from someone else. |
| Handing a review to a teammate who has no SpecGate server | Export the review as a single file they can open read-only. The file re-checks its own evidence, so an old file cannot pretend to be a fresh pass. |
| Everyone needs to see the same state, from any machine | Full mode keeps workspaces on a shared server. Any machine pointing at it sees the same work, and you can see who is in the workspace. |
| The evidence was recorded against a different commit than the one being reviewed | SpecGate compares the recorded checkout with the current one and warns loudly when they do not match — for example, when someone forgot to pull. |
| A merged PR never gets matched back to the work it delivered | GitHub and GitLab integrations confirm the merge at the exact commit the agent submitted, and record that against the work item. |
| Approved work needs to reach the tracker | The Linear integration hands approved work to your team's board; handing it straight to an IDE agent stays available. |
| Team knowledge (policies, past decisions) never reaches the agent | Knowledge stores your team's documents and feeds relevant, cited excerpts into the context the agent receives. |

## The short version

Solo, SpecGate is the reviewer you do not have: it makes the agent prove every
criterion. In a team, it ends the argument about what was approved and who
accepted what, with a record that lives outside chat history and outside the
repository.

## Related

- [Quickstart](../quickstart.md)
- [How SpecGate works](how-specgate-works.md)
- [Feature status](../reference/feature-status.md)
- [Trust and security](trust-and-security.md)
