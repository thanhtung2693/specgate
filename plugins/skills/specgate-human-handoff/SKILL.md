---
name: specgate-human-handoff
description: Use when a human decision is needed or the user asks to inspect SpecGate work. Present the exact CLI next action and offer browser review when the user chooses it in Local mode.
---

# Present the human handoff

Apply the [SpecGate operating contract](../specgate/SKILL.md#operating-contract); complement the current phase.
Reuse the exact work reference and latest authoritative status from the current
phase. If there is no status, read `specgate change status <work-ref> --json`.
Do not select work from a branch name or guess an identity.

When the next actor is human, name the decision needed, title and reference,
and most relevant evidence or gap in the user's language. Always show `next_command` verbatim.
For delivery handoffs, follow [the receipt format](references/delivery-receipt.md).
When the next actor is an agent, continue the current phase. A requested view
can show the status without implying that human action is due.

In Local mode, look up a review link only when the user has chosen browser review
in this interaction. If they prefer the CLI or have expressed no preference,
use the CLI handoff and skip the lookup. For requested Local browser review,
run `specgate open <work-ref> --print --json`.

Use the returned `data.url` verbatim in a descriptive Markdown link. Never
invent a host, port, route or query string, and never include credentials or
session tokens. A Local URL requires a live UI session on the same machine.
If the command reports no Local session, offer `specgate ui <work-ref>` from
the bound repository. Start the UI only when the user asks to start it.

Do not translate original criteria, specification text, claims, or recorded
notes. Never auto-accept, request changes, or open a decision endpoint merely
to produce a link. Do not send the handoff to an external service unless asked.
