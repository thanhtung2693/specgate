# Review Local work in your browser

Use the Local UI when you need to inspect a delivery or make a human decision.
Your coding agent continues to prepare specifications, implement and run checks
through your normal IDE/CLI workflow. Docker is not required.

## Open the UI

From the repository bound to your Local workspace:

```bash
specgate ui
```

The command starts a loopback server and opens your browser. Keep it running;
Ctrl-C stops the UI and preserves all Local records. To print a startup result
without launching a browser, run `specgate ui --no-open --json`. The command
still stays in the foreground. `--port` selects a loopback port; by default the
OS chooses an available one.

Start at a particular work item with `specgate ui <work-ref>`. While the session
is running, get its review link from another terminal:

```bash
specgate open <work-ref> --print --json
```

Use the returned URL. The link works on the machine running that session and
carries no session credentials. If it is unavailable, restart `specgate ui`
from the intended repository/workspace. With a running session, `specgate open`,
`specgate open work`, and `specgate open reviews` all open the Local work queue.

## Decide what needs you

One full-width top bar spans every screen. It shows the SpecGate logo, the
running CLI version, and the language, notification, theme and **Settings**
controls at the right edge. The logo returns to the work queue. On the queue
the bar also names the workspace and has a refresh button. In a review the same
bar holds a **Queue** back link, the work reference, **Scope**, and, when the
work is waiting on you, its position in that list with previous and next
buttons. **Waiting on you** and **Notifications**
count workspace items whose next action belongs to a human reviewer or
maintainer. Each notification names the actor and action and opens the matching
work. Use **Agent’s next step** for work the agent owns, and **All work** to
include recorded decisions. If the work list cannot be read, the UI says so.

The queue shows one line per work item: a status mark, the title, an age, and
a short note only when something needs attention (a stale review, requested
changes, or a maintainer action). The age is how long ago the work item was
created, not when it was delivered; hover it or read its accessible name for the
exact date. Search matches the title and the work reference. The reference is
shown in the review's top bar. The queue does not count evidence or gaps; open
the work to see them.

A review has two panes. The left pane lists the acceptance criteria under the
work title, with one summary line that puts problems first. The right pane shows
the evidence for the selected criterion. The decision note and both decision
buttons stay docked at the bottom, so you can decide without switching tabs. On
screens narrower than 760 px the panes stack and the decision stays at the
bottom of the screen. Select a criterion by clicking it. With a criterion
button focused, press `J` or `ArrowDown` for the next, or `K` or `ArrowUp` for
the previous. Outside the list or at either end, the keys retain their normal
behavior. No shortcut accepts or requests changes. The first criterion with a
problem is selected when the review opens.

Each criterion shows a four-step ladder: the agent’s **Claim**, the cited
**Source**, the CLI-observed **Check** and the **Test** you selected. A filled
step means something is recorded there, green means the CLI observed a pass,
red means a failure, an amber ring marks a problem, and a dotted step means
nothing is recorded. The selected criterion names the four steps under its
ladder, and a criterion with a problem always shows a text label such as
**Evidence gap**. The evidence pane spells out all four steps in words. A
problem is an evidence gap, a claim the agent reports as partial or not done,
a failed or skipped check or test, or a required test with no results. A
criterion without an optional selected test is not a problem.

Under the criteria, **Delivery details** holds **Delivery report and checks**,
**Since the last human review** and **Version details**. Version details give
the review and completion IDs, context digest, acceptance basis and CLI next
action. **Scope** in the review header opens the approved Context Pack and
documents in a full-height drawer up to 480 px wide. On narrow screens, it
leaves 34 px of the backdrop visible along the left edge. A link to
`?ref=<work-ref>&tab=context` opens the review with the drawer open, and
`&tab=decision` opens the review with the note focused.

The visible page checks for new deliveries about every 30 seconds. **New
delivery available** appears when another delivery arrives for the work you're
inspecting. Your current evidence, draft note, open source view and dialogs
stay in place. Choose **View latest** to load the new delivery and keep your
draft note. Inspect the new evidence before confirming a decision. A new
confirmation opened after the notice directs you to **View latest**. An already
open confirmation remains bound to the old review, and the CLI refuses it if the
identity changed.

The right pane keeps the agent’s report apart from what the CLI observed. The
agent’s report and an agent-reported check status appear as quoted claims.
Only a check the CLI ran, or a selected-test result it observed, is shown as
observed. The cited source view reads actual cited checkout lines and a
tracked-file diff when available; unavailable references stay explicit. **View
original data** opens the underlying criterion report in a bounded disclosure.

“Agent reports met” and a CLI-observed command remain distinct from a passing
selected test. The test step describes observations only: an observed
selected-test outcome, a missing required result, or that no test was selected.
A failed or skipped test remains a test observation rather than being labelled
absent. Source citations display a recorded line number when available;
original citation text remains in the underlying data. Passing checks do not
record human acceptance; only the final confirmation records a decision.

Expand **Delivery report and checks** to see the completion summary and every
reported check, including green checks and checks with the same name. Each
check gives the recorded command, result, provenance and any available detail.
Use the agent's summary and citations to judge which production path ran, what
was simulated, and whether an exit code reflects the behavior the criterion
requires.

**Since the last human review** compares this delivery's criteria, checks and
summary with the latest earlier human-decided review for this work. It shows
the previous decision and note above the before-and-after evidence.
**Previous evidence details** gives both review and completion IDs and the
earlier evidence data. Submissions without a decision are skipped. If there is
no earlier decision or the prior evidence cannot be read, the UI says so.
This comparison uses the saved reports. The source diff and freshness
indicators describe the current checkout.

In **Scope**, approved documents render as Markdown, including headings, lists, tables and
code. **Original source** switches the reader to the pinned document’s raw
Markdown. Choose **Formatted view** to return to the rendered document. Close
the drawer with its close button, Esc, a click outside it or the browser Back
button to return to the review. For a generated Quick Context
Pack, impact and policy metadata appear under **Technical details**, leaving
intent and acceptance criteria prominent. Authored documents keep all their
sections in the reading view. Source HTML is shown as text, images show their
alternative text, and only HTTP(S) links open in a separate tab.

Choose accept or request changes after inspecting the evidence. The note field
keeps its height while you type and move to the decision buttons. A change
request needs a note containing at least one non-whitespace character. The
confirmation applies to the review/completion and basis you inspected. If those
changed meanwhile, choose **Refresh review** in the confirmation. Your draft
stays in the note. Inspect the refreshed evidence before confirming again.
The confirmation shows **Saving decision…** while the CLI is recording it.
**Decision recorded** appears once the CLI confirms the write, and the dock then
shows the recorded decision and any saved note. For a long note, focus it with
Tab and use the arrow keys to scroll.
Historical acceptance stays recorded even if later checkout changes are detected.

EN/VI changes interface text only. Original titles, criteria, source documents,
claims and notes remain in their original language. Light/dark themes and
reduced-motion preferences affect presentation, not the underlying evidence;
with reduced motion, the ladder, pane and transition animations are off.
Use the language button in the top bar (it shows the current language and
switches to the other one) and the sun/moon button to switch theme; these
preferences are remembered in this browser. On screens up to 760 px wide, the
bar stays on one line with 12 px of space at each edge. The queue keeps the
SpecGate wordmark. A review shows the logo and hides the position and navigation
buttons. At 560 px and below, the version and workspace name are hidden in the
bar. The running CLI version is still shown in **Settings**.

## Read cited source and current changes

A `file`, `test`, or `code` citation with a local path and a numeric line can
show nearby lines from the repository running this UI. The view labels them
**Current checkout** and shows the Git HEAD when inspection resolves it. It
reads the path and line from the completion report. The browser selects the
work and criterion.
If a newer completion appears, choose **View latest** before opening its references.

For an exact tracked file, the diff compares current checkout text with the
file at that same path in captured HEAD. Deleted files and citations that moved
out of range can still have a diff. Directory and submodule citations cannot
expand into diffs. Untracked files can show current source without a diff.
Switching between **Current code** and **Diff against HEAD** keeps keyboard focus
on the selected control.
Missing citations, heading-only references, unsafe paths, binary files and
files over 1 MiB show an explicit unavailable reason. Changed inputs with more
than 32,768 lines combined can still show the current excerpt when available.
The view limits source to 120 lines and 64 KiB of text, and diff output to
64 KiB. Truncation stays visible.

Git internals, credentials, private keys and Local storage are excluded. Only
the `.specgate/work` subtree is readable within `.specgate`. Source and diff
are current checkout observations, not a frozen delivery snapshot or proof
that a selected test passed. They do not change the reported claim, evidence
provenance, review identity or human acceptance authority.

## Maintain the installation

The settings dialog keeps version and cache controls in separate rows.
The gear shows a separate indicator when a CLI update is available. **Version**
shows the available release and lets you check or review it. Installing an
update requires a confirmation that shows the target and affected scope.
The updater refreshes the CLI and its managed IDE setup. It preserves
native-manager-owned plugins. Development binaries do not offer self-update.
Restart the UI after a successful update to use the new executable.
Closing a preview or confirmation returns to settings. Closing settings returns
to the work you were inspecting without discarding its draft decision note.

Cache cleanup previews only known, regenerable SpecGate-owned cache entries
and checks that the preview still matches before removing them. It does not
clear `.specgate/work`, specifications, work history, evidence, decisions or
backups. Unknown files, symlinks and active/unverified temporary files are not
eligible. This is narrower than `cleanup --work`.
On Windows, SpecGate shows that cache cleanup is unavailable and keeps the cache.

## Boundaries

This UI reads the selected Local workspace through the CLI owner. It does not
provide specification authoring, work creation, a shell, a test runner, chat,
or agent orchestration. The loopback listener trusts every local account and
process with access to it. The browser session, Origin and CSRF checks protect
against other browser origins. Local HTTP has no OS-user authentication.

## Related

- [Try Local review notifications](local-notification-pilot.md)
