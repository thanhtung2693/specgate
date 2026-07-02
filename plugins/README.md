# SpecGate Plugins

Looking to install SpecGate in your IDE? Follow
[Install SpecGate in your coding IDE](../docs/guides/install-ide-plugins.md).
This page documents plugin source layout for repository maintainers.

Single source of all IDE integration files. The plugin root is this `plugins/`
directory for repository development; the CLI installer writes Codex's personal
plugin package to `~/.codex/plugins/specgate`, matching Codex's local-plugin
layout. Project-local Codex installs write to `.codex/plugins/specgate` so the
generated plugin copy does not sit inside this canonical source directory.

## Structure

```
plugins/
├── .agents/plugins/    Codex marketplace manifest for local/repo testing
├── .codex-plugin/      Codex plugin manifest (install: codex plugin marketplace add --sparse plugins <repo>)
├── .claude-plugin/     Claude Code plugin manifest (install: /plugin marketplace add <repo>/plugins)
├── .cursor-plugin/     Cursor plugin manifest and marketplace metadata
├── assets/             Shared plugin assets, including the landing-page logo
├── skills/             Shared focused lifecycle skills — single source of truth
├── hooks/              Shared IDE hooks — single source of truth (session-start, run-hook.cmd, *.json)
├── rules/              Cursor rules
└── package.json        Served package inventory and focused skill list
```

There should not be a `plugins/specgate/` directory in this repository. If it
appears, it is a stale project-local install artifact; remove it and rerun
`specgate plugins install --project-local --agent codex` to regenerate
`.codex/plugins/specgate/` instead.

Codex keeps marketplace and plugin metadata at the plugin root:

- `.agents/plugins/marketplace.json` makes `plugins/` a Codex marketplace root.
- `.codex-plugin/` is the plugin metadata used by Codex plugin install.

Claude Code uses `.claude-plugin/` for plugin metadata. `specgate plugins
install --agent claude` writes the same native plugin package into
`~/.claude/skills/specgate`, which Claude Code loads as a skills-directory
plugin.

## Claude Code plugin

Use `install.sh --agent claude` or `specgate plugins install --agent claude` for
end-user setup. It refreshes focused Skills and hook settings; the
`.claude-plugin/` manifest stays in this directory for Claude Code plugin
compatibility.

The short `/using-specgate` router selects the setup, preparation, or delivery
skill without loading the full lifecycle into every conversation.

## Skill invocation modes

SpecGate keeps the installed skills focused, but not every skill should feel like
a top-level command:

- `using-specgate` is the router. Invoke it for normal SpecGate work so the
  agent chooses exactly one narrower phase before acting.
- `setting-up-specgate-project` is explicit setup. Invoke it when installing or
  refreshing SpecGate for a repository, or when canonical docs, tracker mirrors,
  readiness rules, verification commands, or domain vocabulary are still unknown.
- Lifecycle skills stay focused: `preparing-work` covers shaping and artifact
  readiness before approval; `delivering-work` covers pickup, scoped
  implementation, evidence, and delivery review. Prefer reaching them through
  the router unless the user names the phase directly.

The current package keeps all focused skills model-visible for IDE
compatibility. Treat the distinction above as the operating contract: keep
descriptions trigger-oriented, avoid workflow summaries in frontmatter, and put
phase-specific instructions in the phase skill rather than the router.

## Project Setup Skill

`setting-up-specgate-project` gives agents a first-run setup map before they use
the lifecycle skills in an unfamiliar repository. It discovers:

- canonical docs and module ownership boundaries;
- tracker and work mirrors;
- readiness rules and hard stops;
- verification commands;
- domain vocabulary and glossary sources;
- Context Pack inputs and gaps.

The skill produces a Markdown map for the current project. It does not write
server storage, change UI components, or alter the Context Pack schema.

## Codex plugin

```
codex plugin marketplace add --sparse plugins thanhtung2693/specgate
```

Then install SpecGate from the Codex plugin panel and restart Codex so the new
plugin skills and bundled hooks are loaded in fresh threads. Codex requires a
hook trust review before plugin-bundled hooks run.

`specgate plugins install` writes global user files by default. Use
`--project-local` only when a repository should vendor its SpecGate IDE setup.
For Codex, project-local plugin files live under `.codex/plugins/specgate/`.

## CLI-managed install

`plugins/install.sh` is a bootstrap wrapper: it installs or locates the
`specgate` CLI, then delegates IDE file writes to:

```bash
specgate --server http://<your-registry> plugins install --agent all
```

Verify local IDE files with:

```bash
specgate plugins doctor --agent all
```

This keeps JSON/TOML merging in Go instead of shell and avoids a Python
dependency on end-user machines.

## Marketplace readiness

The generated native manifests, shared logo asset, focused Skills, and package
inventory are the marketplace submission source material. Until Codex, Claude
Code, and Cursor marketplace submission requirements are finalized for this
project, keep the local installer as the development/private-distribution path
and use marketplace manifests as compatibility metadata.

## Cursor plugin

Add from the Cursor marketplace panel (project- or user-scoped).

## Skill quality checklist

Before changing a focused Skill under `plugins/skills/`, check each item:

- **Invocation:** keep the Skill model-invoked only when the agent must discover
  it without the user naming it. Put the leading word first in the description.
- **Scope:** keep `using-specgate` as the router. Phase Skills should describe
  only their lifecycle phase, not repeat the whole SpecGate workflow.
- **Steps:** every ordered step ends with a checkable completion criterion.
- **Reference:** move branch-specific or reusable reference out of `SKILL.md`
  only when a clear context pointer tells the agent when to read it.
- **Portability:** write for IDE agents in arbitrary repositories. Mark filenames,
  manifests, shell snippets, and artifact documents as examples unless they are
  required SpecGate contract names.
- **Pruning:** remove duplicated lifecycle prose, stale commands, and no-op
  instructions before adding new text.
- **Sync:** after editing Skills, run `make sync-plugins` and `make check-plugins`
  so the embedded package copy stays identical to `plugins/`.

## Dogfood observation checklist

When testing the Skills in a fresh agent thread, record findings against the
behavior the Skills are meant to make predictable:

- **Router:** the agent loads `using-specgate` and selects exactly one lifecycle
  Skill before acting, unless the request is only a SpecGate question.
- **Pickup:** the agent runs `specgate work show`, `specgate gates status`,
  `specgate work policy`, and `specgate work context` before editing.
- **Stop conditions:** the agent stops when approval, acceptable gates,
  acceptance criteria, or a fresh Context Pack are missing, and names the exact
  blocker.
- **Scope:** implementation changes map back to Context Pack acceptance criteria,
  non-goals, verification items, or required repo-doc updates.
- **Evidence:** completion reports include checks, affected files, and exactly
  one claim per acceptance criterion with concrete evidence.
- **Delivery loop:** failed delivery review verdicts drive focused rework from
  `outstanding_md`; passed verdicts are read from persisted delivery status.
- **Tone/load:** Skill text helps the agent move without excessive ceremony,
  repeated explanation, or command thrash.

Use this finding shape so follow-up edits stay surgical:

```markdown
## Finding

- Work item:
- Expected skill behavior:
- What the agent actually did:
- Transcript excerpt or command output:
- Proposed improvement:
```

## Keeping plugins in sync

`package.json` is the package inventory consumed by the Go server. It declares
shared plugin metadata, the focused skills, the installer's skill cleanup list,
and the exact files that may be served under `/plugins/*`.

`skills/`, `hooks/`, `rules/`, package metadata, and `package.json` are the
canonical sources used by native plugin manifests and shell installers. The only
copied destination is the Go server's embedded package tree:

| Destination | Purpose |
|---|---|
| `app/doc-registry/internal/agentpackages/plugins/` | Go server embeds these at compile time (`//go:embed`) to serve the install script |

After editing shared metadata in `package.json`, regenerate the native plugin
metadata:

```bash
make generate-plugins
```

After editing any file in `skills/`, `hooks/`, `rules/`, `package.json`, or
generated plugin metadata, run:

```bash
make sync-plugins   # copies root plugin assets into agentpackages/plugins/
```

Commit the resulting copies. In CI, `make check-plugins` verifies generated
metadata, diffs the embedded copy, and fails the build if either side diverges.

In Docker dev mode, doc-registry's Air watcher mounts the root `plugins/`
directory read-only, runs `app/doc-registry/scripts/sync-embedded-plugins.sh`,
regenerates embedded native metadata from the copied `package.json`, and rebuilds
automatically when plugin manifests, skills, hooks, or shell files change.
Production binaries still need a rebuild because the served plugin assets are
compiled into the binary.

`//go:embed` cannot follow symlinks across the Go module boundary (`app/doc-registry/` is the module root; `plugins/` is outside it), so `agentpackages/plugins/` must use real copies, not symlinks. Intra-directory symlinks within `agentpackages/plugins/` would work but add no value since `make sync-plugins` handles the sync.
