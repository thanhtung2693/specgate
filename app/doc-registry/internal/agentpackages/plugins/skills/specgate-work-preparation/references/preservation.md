# Preservation criteria when behavior changes

Ask the human which existing behaviors must remain observable. Inspect relevant
tests and source specs; propose only a small, reviewed set. Record each approved
invariant as an ordinary work acceptance criterion. Use an existing `@source`
identity only when the approved inventory contains it. Bind `@check` only if
the inspected test actually exercises that criterion.

Example: a workspace can change its display name. Preserve stored data after
reopening, keep the old name when the edit is cancelled, and leave another
workspace unchanged. These are separate criteria, not a blanket regression
claim.

A passing pre-change test is useful baseline evidence but does not prove the
post-change result. Rerun selected tests after implementation. Report baseline
failures; do not silently repair or ignore them. Manual and unbound criteria
remain reviewed claims, not automated guarantees.
