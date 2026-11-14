# Customize governance

Use this guide to add project-specific instructions to Local readiness gates.

## Add Local project instructions

Ask your coding agent to edit ordinary Markdown files in your repository's
`.specgate/rubrics/` directory. The four supported filenames extend existing
Local semantic gates:

| Filename | Additional instructions for |
| --- | --- |
| `scope_clear.md` | Product intent, scope and boundaries |
| `spec_completeness.md` | Required readiness topics |
| `acceptance_criteria_verifiable.md` | Observable outcomes and verification |
| `spec_repo_drift.md` | Approved artifact versus governed repository documentation |

For example, put this text in `.specgate/rubrics/scope_clear.md`:

```markdown
Require an explicit rollback boundary for changes affecting persisted data.
Cite the exact approved source passage supporting that boundary.
```

The CLI appends this text to the embedded gate rubric; it does not replace the
base instructions. Missing files retain their exact defaults. Each active file
must contain nonempty UTF-8 text, at most 64 KiB, without binary control
characters. Files and their `.specgate/rubrics/` directories must be regular
non-symlink paths inside the selected Git project. Use the exact lowercase
filenames above. Unknown Markdown names and case variants such as
`scope_clear.MD` are errors on all filesystems. Unrelated non-Markdown files
are ignored and preserved. Invalid active files fail both preview and
publication with the affected path.

Preview through your normal artifact manifest:

```bash
specgate artifact publish --file .specgate/work/artifact.json --preview --json
```

Inspect `policy.rubrics`: `source` identifies the project-relative file or
`embedded_default`, and `digest` hashes the effective base-plus-project text.
`policy_digest` identifies the whole resolved policy. Preview writes no state.
After human confirmation, publish the same manifest. Publication reads the
current files again, so preview again if you edit them before publishing.

Publication freezes the effective text and source in the immutable artifact
policy. Your coding agent reads tasks from
`specgate gates tasks list <artifact-id> --json` and judges only their frozen
`skill_content`, copying the returned digests exactly. Edits or removal affect
future previews and new artifact versions; existing artifacts, approved work,
tasks, evidence and acceptance history keep their original policy.

Rubric text adds judgment instructions only. It cannot add gates, change
deterministic checks, reduce approval or evidence requirements, or grant human
acceptance. Contradictory instructions remain reviewable policy text. No scripts,
hooks, providers, model runner or UI editor are introduced. Your existing
authoring framework and coding agent keep ownership of specs and implementation.

Rubrics are user-owned sources: SpecGate never creates, rewrites or deletes
them, and `cleanup --work` preserves them. Choose their Git policy yourself;
if you want them versioned, explicitly include them according to your project's
ignore rules. SpecGate preserves them.

A supporting CLI is required to read frozen project rubrics.

Make a [complete Local backup](operate-specgate.md#operate-local-cli) before
switching to an earlier CLI.

## Related

- [Gate catalog](../reference/gates.md)
- [Governance reference](../reference/governance.md)
- [How verification works](../concepts/verification.md)
