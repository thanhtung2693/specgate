# SpecGate Plugins

Canonical source for SpecGate IDE skills, hooks, rules, assets, and generated
manifests. End-user installation belongs in
`docs/using-specgate/guides/install-ide-plugins.md`; this page is for repository maintainers.

## Layout

```text
plugins/
├── .agents/plugins/    Codex marketplace metadata
├── .codex-plugin/      Codex manifest
├── .claude-plugin/     Claude Code manifest and marketplace metadata
├── .cursor-plugin/     Cursor manifest and marketplace metadata
├── assets/             Shared plugin assets
├── hooks/              Session hooks and platform adapters
├── rules/              Cursor rules
├── skills/             Focused lifecycle skills
└── package.json        Canonical metadata and served-file inventory
```

Do not create `plugins/specgate/`. The CLI writes the project-local root skill
to `.agents/skills/specgate` or `.claude/skills/specgate`, with phase skills in
the matching `specgate-*` directories.

## Source and generated files

Edit metadata in `package.json`; `make generate-plugins` produces native
manifests in this directory and repository-root marketplace pointers. Each root
pointer resolves the plugin source as `./plugins`:

- `.agents/plugins/marketplace.json`
- `.claude-plugin/marketplace.json`
- `.cursor-plugin/marketplace.json`

The Go server cannot embed files outside its module, so `make sync-plugins`
copies canonical assets into
`app/doc-registry/internal/agentpackages/plugins/`. Do not edit that copy.

## Skill invocation modes

`specgate` selects one narrow phase for normal SpecGate
work.

`specgate-project-setup` is explicit setup. It maps canonical docs,
ownership, tracker mirrors, readiness rules, verification commands, and domain
vocabulary for an unfamiliar repository.

Lifecycle skills stay focused:

- `specgate-work-preparation` shapes slices, publishes artifacts, and checks readiness. It
  stops before implementation and human approval.
- `specgate-work-delivery` picks up approved work, implements within the Context Pack,
  reports evidence, and completes delivery review.

Keep frontmatter descriptions trigger-oriented. Put phase instructions only in
the owning phase skill; do not expand the router into a second workflow.

## Install behavior

Codex and Claude Code can install this package through their plugin managers.
The SpecGate CLI detects that ownership and avoids duplicate files.

The CLI owns JSON/TOML merging and marked user destinations when it installs:

```bash
specgate plugins install --agent all
specgate plugins doctor --agent all
```

Global Codex and Claude Code installs receive native plugin packages with the
focused skills and hooks. Project-local installs use each IDE's repository skill
directory; Cursor also receives its rule. Use `--project-local` only when a
repository should vendor those files. Restart the selected IDE after refresh.

The checked-in marketplace files support repository development and private
distribution. They are not an official-directory availability claim.

## Editing checklist

- Keep the router short and each lifecycle skill within one phase.
- Preserve human approval and Context Pack authority as hard stops.
- Prefer observable completion criteria and exact CLI commands.
- Remove duplicated explanation before adding guidance.
- Keep examples portable across repositories and build systems.
- Add metadata once in `package.json`, then regenerate.

After changing metadata, skills, hooks, rules, or assets:

```bash
make sync-plugins
make check-plugins
```

`make check-plugins` verifies generated metadata, embedded copies, required
skills, and CLI commands. Production binaries need a rebuild after embedded
assets change.

## Manual observation

In a fresh IDE thread, check that the agent:

- routes to exactly one phase;
- stops on missing approval, criteria, acceptable gates, or a fresh pack;
- maps edits to scope and non-goals;
- reports observed checks and criterion-specific evidence;
- uses failed verdict findings for focused rework.

Record a problem with the work reference, expected behavior, observed behavior,
relevant transcript or output, and the smallest proposed skill change.
