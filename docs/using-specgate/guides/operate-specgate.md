# Operate SpecGate

This guide covers Local installation, backups, and recovery.

## Operate Local CLI

Local mode needs no background service. From the bound repository, run
`specgate doctor` to check the store, workspace binding, shell, and IDE files.
Use `specgate work resume <work-ref> --json` to pick up a specific work item.

Run `specgate ui` for the optional browser review surface. It stays in the
foreground; Ctrl-C stops its loopback listener without deleting Local data.
See [Local review](local-review.md) for links, decisions and maintenance.

To upgrade the CLI, run `specgate update` or rerun the installer. Back up the
selected Local store first and review the [changelog](../../../CHANGELOG.md)
for compatibility changes. Existing work remains `unconfigured` until a human
pins a verification contract; the first enhanced write upgrades the Local
store as described below.

Refresh CLI-managed plugins using the same agent and global/project scope used
at installation. Update native Codex or Claude Code plugins through their IDE
plugin manager, then start a new IDE session.

For a backup, stop all CLI and IDE-agent operations that write Local state,
then copy the entire selected Local store directory and CLI configuration to a
private backup location. Honor custom `--local-dir`, `SPECGATE_LOCAL_DIR`, and
`SPECGATE_CONFIG_PATH` settings; do not assume the repository's `.specgate/`
directory contains the store. Keep SQLite sidecar files with the database if
present. See [Configuration](../reference/configuration.md#cli).

The first Local checkpoint, v2 verification pin, or source-lineage publication
also upgrades the store's write compatibility. The confirmation shows the exact
`state.db.pre-enhanced.bak` path and requires `--yes` in unattended use. Keep a
supporting CLI for the upgraded live store: older CLIs are blocked from writes,
and v1 handoff export refuses pins, checkpoints, lineage, or decision
bases rather than dropping them. The backup is a pre-upgrade snapshot, not a
lossless downgrade of later work. Never overwrite the live database to return
to an old CLI; copy the backup to a separately selected recovery location.

Received stores use a new checkout generation and require a CLI that
supports it. A copy retains its original work and acceptance history. Read a
source checkpoint with `work resume <ref> --since <checkpoint-id>` to see
its note and the comparison limit. Inspect any pending review again in the
receiving CLI or browser before deciding.

## Move a Local store to another checkout

Before exporting, make sure the receiving Git checkout can be identified as
the same repo. Use the same Git origin on both machines. If the source has no
origin, make at least one commit before exporting and carry its Git history to
the receiving checkout so it contains the recorded commit and tree.

1. Stop agents, CLI jobs and browser UI sessions that write to the source
   store. From the bound source checkout, you can record a pickup checkpoint:

   ```sh
   specgate work checkpoint "$work_ref" --note "What is done and what remains"
   ```

   The first checkpoint may ask to upgrade the store even after a fresh
   install. Review the confirmation. It shows where SpecGate will save
   `state.db.pre-enhanced.bak` before upgrading. Older CLIs cannot write to
   the upgraded store. If you prefer to skip this step, prepare the snapshot
   without `--checkpoint`.

   Prepare the **entire selected Local store**. The work reference and optional
   checkpoint identify where to resume. Review the workspace and user inventory
   before confirming. If you made a checkpoint, add `--checkpoint` followed by
   the ID the CLI printed.

   ```sh
   specgate transfer prepare --work "$work_ref" --file "$archive" \
     --note "What to pick up"
   ```

2. Record the archive SHA-256 and move the private archive through a channel
   you trust. Move the Git checkout separately. Include uncommitted and untracked
   files you need, any completion draft you want to reuse, and referenced
   evidence files such as logs under `.specgate/work`. A checkpoint records
   observations and hashes. It carries no working files.

3. On the receiving machine, use a supporting CLI to inspect the archive.
   Compare its SHA-256 with the one you recorded. Receive into a new directory
   under an existing writable parent, then activate for the intended Git root.

   ```sh
   specgate transfer inspect --file "$archive"
   specgate transfer receive --file "$archive" --into "$new_dir" \
     --expect-snapshot "$snapshot_sha256"
   specgate transfer activate --from "$new_dir" --repo "$checkout"
   ```

4. Run the returned `work resume` command from the receiving checkout. Follow
   the existing next actor and next action. The source checkpoint, checks and
   decisions remain available as history. When the work needs fresh evidence,
   check local paths and tools before rerunning its checks on the receiving
   checkout. Review any pending decision there before accepting it.

Preparing and receiving preserve work and workspace IDs, approved artifacts
and policy snapshots, verification, checkpoints, evidence and decisions
from the source SQLite snapshot. The archive contains private history.
Keep it private and use only self-produced or trusted snapshots. Checksums
verify byte consistency. Git working files, external evidence bytes, CLI
configuration, credentials, `.env`, toolchains, UI sessions, caches, old
backups and physical SQLite sidecars stay on their original machine.

Receive creates an unbound store and fresh generation. Activation previews
both bindings and writes only the chosen project's binding. Use a fresh
destination again when returning to the previous machine. Earlier stores
remain available. SpecGate does not merge them or detect later offline
writes from an earlier store. See [the transfer command reference](../reference/cli.md#local-snapshot-transfer).

## Previous installations

The Full appliance and remote commands have been retired. Installation and
updates leave existing deployments, configuration, volumes, and backups in
place. Use a prior compatible CLI if you need to access that deployment.
If the current CLI stops on a legacy remote configuration, keep it in place.
For an independent Local setup:

1. Set `SPECGATE_CONFIG_PATH` to a separate file and unset `SPECGATE_SERVER`.
2. Run `specgate init` outside the legacy checkout, whose committed
   `.specgate/config` may still name the old server. Supply `--local-dir` with
   a fresh directory if you want a separate SQLite store.
3. Keep using the new configuration path. Return to the target checkout and
   run `specgate workspace bind <slug>` to attach the Local workspace.

The old deployment and configuration remain available to prior compatible
tooling.

Existing portable bundles remain files you own. Keep them with their
compatible tooling. Use a complete Local backup for Local recovery. The
[delivery handoff](../reference/cli.md#hand-a-review-to-a-teammate-through-the-repository)
is an unsigned bounded review request and carries neither a complete store
nor a shared acceptance decision.

## Continue

- [Configuration reference](../reference/configuration.md)
- [Trust and security](../concepts/trust-and-security.md)
