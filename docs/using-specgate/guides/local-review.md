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

The top bar shows the running CLI version. The SpecGate logo returns to the
work queue; the gear opens **Settings**. **Waiting on you** and **Notifications**
count workspace items whose next action belongs to a human reviewer or
maintainer. Each notification names the actor and action and opens the matching
work. Use **Agent’s next step** for work the agent owns, and **All work** to
include recorded decisions. The sheet behind a row marks that it has a delivery;
it does not count submissions. If the work list cannot be read, the UI says so.

Open a work item to compare its **Evidence**, **Scope** and **Decision** tabs.
Expand **Version details** under the title for review and completion IDs,
context digest, acceptance basis and CLI next action. The visible page checks for new deliveries
about every 30 seconds. **New delivery available** appears when another
delivery arrives for the work you're inspecting. Your current evidence,
draft note, open source view and dialogs stay in place. Choose **View latest**
to load the new delivery on the same tab and keep your draft note. Inspect the
new evidence before confirming a decision. A new confirmation opened after the
notice directs you to **View latest**. An already open confirmation remains
bound to the old review, and the CLI refuses it if the identity changed.

Criterion sheets keep the complete requirement text visible, with three marks
for the agent claim, observed check and attached test, plus a result label.
Expand a row to read any gap, original report, run results and cited source
together. References show the recorded path, citation, check and selected tests.
The source view reads actual cited checkout lines and a tracked-file diff when
available; unavailable references stay explicit. **View original
data** opens the underlying criterion report in a bounded disclosure.

“Agent reports met” and a CLI-observed command remain distinct from a passing
selected test. **Run results** describes observations only: a command result,
an observed selected-test outcome, or the absence of an attached test. A failed or skipped
test remains a test observation rather than being labelled absent. Source
citations display a recorded line number when available; original citation
text remains in the underlying data. A criterion can have sufficient evidence
without optional selected JUnit cases. Passing checks do not record human
acceptance. **Review decision** opens the raised note panel beside the criterion
summary; only the final confirmation records a decision.

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
Markdown. Choose **Formatted view** to return to the rendered document. For a
generated Quick Context Pack, impact and policy metadata appear under
**Technical details**, leaving intent and acceptance criteria prominent.
Authored documents keep all their sections in the reading view. Source HTML is
shown as text, images show their alternative text, and only HTTP(S) links open
in a separate tab.

Choose accept or request changes after inspecting the evidence. A change
request needs a note containing at least one non-whitespace character. The
confirmation applies to the review/completion and basis you inspected. If those
changed meanwhile, choose **Refresh review** in the confirmation. Your draft
stays on the Decision tab. Inspect the refreshed evidence before confirming again.
The confirmation shows **Saving decision…** while the CLI is recording it.
**Decision recorded** appears once the CLI confirms the write.
Historical acceptance stays recorded even if later checkout changes are detected.

EN/VI changes interface text only. Original titles, criteria, source documents,
claims and notes remain in their original language. Light/dark themes and
reduced-motion preferences affect presentation, not the underlying evidence.
Use the EN/VI buttons in the top bar and the sun/moon button to switch theme;
these preferences are remembered in this browser. On narrow screens, the
utility controls can use a second header row so the language and notification
controls remain accessible.

## Read cited source and current changes

A file citation with a numeric line, or a line range, can show actual nearby
lines from the repository running this UI. The view labels them **Current
checkout** and identifies the captured Git HEAD. It reads the cited path from
the completion report; it does not accept a browser-supplied path or revision.
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
