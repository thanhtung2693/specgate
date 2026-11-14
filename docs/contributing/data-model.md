# Data model

The Local SQLite store owns governance state. The selected store belongs to
the local user. The CLI configuration keeps the selected identity and
workspace plus per-checkout bindings separately.

| Record | What it identifies |
|---|---|
| Local user and workspace | Attribution and a boundary for governance work |
| Feature | A workspace-owned key and its canonical approved artifact |
| Artifact version | Immutable normalized documents, source provenance, policy, rubric text and digests |
| Work item | Expected outcome and explicit criteria, optionally pinned to an exact approved artifact |
| Context Pack | Immutable implementation scope and inputs for that work |
| Readiness run and task | A deterministic evaluation or agent-attested result tied to artifact and policy digests |
| Completion feedback event | Append-only criterion evidence, checks and Git receipt |
| Delivery review | Assessment of one exact completion |
| Human decision | Actor, note, reviewed completion and review, and applicable acceptance basis |
| Verification pin, input watch and checkpoint | Opted-in exact command selection and checkout observations |
| Source requirement and lineage | Reviewed source IDs, explicit work mappings, and declared relationships across artifact versions |

A quick work item has no lead artifact. An artifact-backed Context Pack retains
its approved version even when a newer version becomes canonical. A new
completion supersedes earlier evidence for the active delivery review.
Historical reviews and previously recorded human decisions remain readable.
The optional UI uses exactly these Local records through the CLI.

Follow the [contracts](contracts.md) for compatibility, evidence provenance and
decision safety. See [Local operation](../using-specgate/guides/operate-specgate.md)
for backups and legacy deployment recovery.
