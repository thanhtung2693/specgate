# Try Local review notifications

The optional pilot script watches one bound SpecGate Local repository and workspace while its terminal is open. Use the normal [CLI and coding agent workflow](coding-agent-workflow.md) to prepare work, report evidence, and make decisions.

## Start and stop

You need Node.js 26 or later, the SpecGate source checkout, and a configured Local CLI. From your project repository, run `specgate doctor --json` to check its mode and `specgate workspace current --json` to find its bound workspace slug or ID.

From the SpecGate source checkout, run the script with the path to your project repository root and that workspace. Use the exact slug, including any Unicode letters or digits, or the immutable workspace ID from the CLI output.

```sh
node scripts/local-review-notifications.mjs --repo '/path/to/your/project' --workspace 'your-workspace'
```

The script uses `specgate` on `PATH`. To try a specific build, append `--specgate '/absolute/path/to/specgate'`. For multiple repositories, run one instance per repository and workspace in separate terminals.

The first successful poll records a baseline silently, including work already waiting. The script then waits 30 seconds after each poll completes before reading again. It prints a notice when it observes a new action for a reviewer or maintainer, including a replacement review, and shows each matching review and decision state once per run. Failed reads appear in the terminal and preserve the last observations. To stop, press Ctrl-C. Restarting captures a new baseline. A transition that starts and ends between polls can be missed.

Terminal notices are the default. On macOS, append `--macos-notifications` if you want a system notice as well. Delivery depends on your macOS notification settings. The system notice includes the repository name, work reference, and actor. The terminal shows the review command. Stop the script and restart without the flag to turn off system notices.

## Review a notice

Run the read-only command printed in the terminal to open the exact work in its bound workspace and read current evidence. The command as printed uses POSIX shell syntax. From another shell, move to the displayed repository and run `specgate --workspace <workspace-id> change status <work-ref> --detail` with the displayed IDs and the same CLI build. Base any decision on that fresh status. The [Local browser review](local-review.md) is available when you choose to open it.

The script reads the repository's Local CLI without recording decisions or saving its own history. Stopping it leaves the normal CLI and agent workflow available.
