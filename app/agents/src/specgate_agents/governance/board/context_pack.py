from __future__ import annotations

import base64
import json
import logging
from datetime import UTC, datetime
from typing import Any

from specgate_agents.governance.attachments import (
    AUDIENCE_BOTH,
    AUDIENCE_CODING_AGENT,
    render_attachments_section,
)
from specgate_agents.governance.config import doc_registry_base_url, governance_version
from specgate_agents.governance.quality_gates.profile_snapshot import (
    UnsupportedSnapshotVersion,
    parse_profile_snapshot,
)
from specgate_agents.governance.registry.client import DocRegistryClient

logger = logging.getLogger(__name__)


def _b64(text: str) -> str:
    return base64.b64encode(text.encode("utf-8")).decode("ascii")


def _snapshot_governance_level(raw: str | None) -> str:
    """Return the governance_level from a gates_profile_snapshot_json string.

    Returns "" on empty, legacy, or corrupt input.
    Returns "" (with a warning) on unsupported snapshot versions rather than
    raising — the context pack surface is informational; a snapshot version
    incompatibility should be visible but must not block the pack render.
    """
    if not raw or not raw.strip():
        return ""
    try:
        snap = parse_profile_snapshot(raw)
        return snap.governance_level or ""
    except UnsupportedSnapshotVersion:
        logger.warning(
            "context pack: unsupported snapshot schema version in source artifact snapshot; "
            "governance_level will be omitted from context pack result"
        )
        return ""
    except Exception:
        logger.warning("context pack: failed to parse snapshot for governance_level", exc_info=True)
        return ""


def _format_acceptance_criteria(raw: str | None) -> str:
    """Render acceptance_criteria_json as plain bullets.

    Accepts the UI-managed shape (``[{"id": ..., "text": ..., "done": bool}]``).
    Engineering handoff only needs the requirement text, so ``done`` is dropped.
    """
    if not raw or not raw.strip():
        return "- _No acceptance criteria captured._"
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError:
        return raw
    if not isinstance(parsed, list) or not parsed:
        return "- _No acceptance criteria captured._"
    bullets: list[str] = []
    for row in parsed:
        if isinstance(row, dict):
            text = str(row.get("text") or "").strip()
        elif isinstance(row, str):
            text = row.strip()
        else:
            text = ""
        if text:
            bullets.append(f"- {text}")
    return "\n".join(bullets) if bullets else "- _No acceptance criteria captured._"


def _section_or_empty(title: str, body: str | None) -> list[str]:
    text = (body or "").strip()
    if not text:
        return []
    return [f"## {title}", text, ""]


def _section_or_placeholder(title: str, body: str | None, placeholder: str) -> list[str]:
    return [f"## {title}", (body or "").strip() or placeholder, ""]


def _unresolved_quality_gates(gate_runs: list[dict[str, Any]] | None) -> str:
    """Latest-per-gate non-pass verdicts (warn/fail/needs_human_review) as markdown
    bullets carrying each gate's hint, excluding the post-build ``delivery_review``
    (its own re-handoff section). Returns "" when every gate passed / not-applicable."""
    if not gate_runs:
        return ""
    latest: dict[str, dict[str, Any]] = {}
    for run in gate_runs:
        gate = str(run.get("gate") or "")
        if not gate or gate == "delivery_review":
            continue
        created = str(run.get("created_at") or "")
        current = latest.get(gate)
        if current is None or created >= str(current.get("created_at") or ""):
            latest[gate] = run
    lines: list[str] = []
    for gate in sorted(latest):
        run = latest[gate]
        state = str(run.get("state") or "")
        if state not in ("warn", "fail", "needs_human_review"):
            continue
        hint = str(run.get("hint") or "").strip()
        lines.append(f"- **{gate}** ({state}){': ' + hint if hint else ''}")
    return "\n".join(lines)


def _artifact_bundle_value(bundle: dict[str, str] | None, key: str) -> str:
    if not bundle:
        return ""
    return str(bundle.get(key) or "").strip()


def _joined_sections(*parts: str) -> str:
    items = [part.strip() for part in parts if part and part.strip()]
    return "\n\n".join(items)


def _role_sections(bundle: dict[str, str] | None, quick_mode: bool) -> list[str]:
    spec = _joined_sections(
        _artifact_bundle_value(bundle, "prd"),
        _artifact_bundle_value(bundle, "spec"),
    )
    design = _artifact_bundle_value(bundle, "design")
    implementation = _joined_sections(
        _artifact_bundle_value(bundle, "implementation_plan"),
        _artifact_bundle_value(bundle, "tasks_fe"),
        _artifact_bundle_value(bundle, "tasks_be"),
    )
    verification = _artifact_bundle_value(bundle, "tasks_qa")
    reference = _joined_sections(
        _artifact_bundle_value(bundle, "rollout"),
        _artifact_bundle_value(bundle, "risks"),
        _artifact_bundle_value(bundle, "assumptions"),
    )

    if quick_mode:
        spec_placeholder = "_No canonical spec content found in the source artifact._"
    else:
        spec_placeholder = "_No spec content found in the source artifact._"

    qa_placeholder_lines = [
        "- Verify every acceptance criterion manually or with automated coverage.",
        "- Confirm no adjacent feature behavior regressed.",
    ]

    out: list[str] = []
    out.extend(_section_or_placeholder("Spec", spec, spec_placeholder))
    out.extend(_section_or_empty("Design", design))
    out.extend(
        _section_or_placeholder(
            "Implementation Plan",
            implementation,
            "_No implementation plan content found._",
        )
    )
    out.extend(
        _section_or_placeholder("Verification", verification, "\n".join(qa_placeholder_lines))
    )
    out.extend(_section_or_empty("Reference", reference))
    return out


def _manifest_sections(manifest_raw: str | None) -> list[str]:
    """Parse the artifact manifest into dev-facing scope + design-ref sections.

    The manifest is raw JSON in the handoff bundle. A coding agent needs the
    blast radius (impacted services/apps, files likely touched) and design
    references (Figma) as explicit, actionable sections, not a raw JSON dump.
    """
    text = (manifest_raw or "").strip()
    if not text:
        return []
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return []
    if not isinstance(data, dict):
        return []

    def _str_list(value: Any) -> list[str]:
        if not isinstance(value, list):
            return []
        return [str(v).strip() for v in value if str(v).strip()]

    out: list[str] = []
    services = _str_list(data.get("impacted_services"))
    apps = _str_list(data.get("impacted_apps"))
    files = _str_list(data.get("files"))
    if services or apps or files:
        scope = ["## Scope & Blast Radius"]
        if services:
            scope.append("**Impacted services:** " + ", ".join(services))
        if apps:
            scope.append("**Impacted apps:** " + ", ".join(apps))
        if files:
            scope.append("**Files likely touched:**")
            scope.extend(f"- {f}" for f in files)
        scope.append("")
        out.extend(scope)

    ref_lines: list[str] = []
    refs = data.get("design_refs")
    if isinstance(refs, list):
        for ref in refs:
            if isinstance(ref, dict):
                url = str(ref.get("url") or "").strip()
                if not url:
                    continue
                label = str(ref.get("type") or "design").strip()
                node = str(ref.get("node_id") or "").strip()
                suffix = f" (node {node})" if node else ""
                ref_lines.append(f"- [{label}] {url}{suffix}")
            elif isinstance(ref, str) and ref.strip():
                ref_lines.append(f"- {ref.strip()}")
    if ref_lines:
        out.extend(["## Design References", *ref_lines, ""])

    return out


def render_context_pack(
    *,
    change_request: dict[str, Any],
    feature: dict[str, Any],
    warnings: list[dict[str, Any]],
    artifact_bundle: dict[str, str] | None = None,
    source_artifact_id: str = "",
    quick_mode: bool = False,
    source_evidence: str = "",
    attachments: list[dict[str, Any]] | None = None,
    gate_runs: list[dict[str, Any]] | None = None,
) -> str:
    warning_lines = (
        "\n".join(f"- {w.get('code')}: {w.get('message')}" for w in warnings) or "- None"
    )
    unresolved_gates = _unresolved_quality_gates(gate_runs)
    ac = _format_acceptance_criteria(change_request.get("acceptance_criteria_json"))
    work_type = str(change_request.get("work_type") or "new_feature")
    cr_key = str(change_request.get("key") or change_request.get("id") or "")
    feature_key = str(feature.get("key") or feature.get("id") or "")
    canonical_artifact_id = str(feature.get("canonical_artifact_id") or "")
    handoff_source_artifact_id = (
        source_artifact_id
        or str(change_request.get("lead_artifact_id") or "")
        or canonical_artifact_id
    )
    manifest = _artifact_bundle_value(artifact_bundle, "manifest")
    reference = _joined_sections(
        _artifact_bundle_value(artifact_bundle, "rollout"),
        _artifact_bundle_value(artifact_bundle, "risks"),
        _artifact_bundle_value(artifact_bundle, "assumptions"),
    )

    lines = [
        "# Implementation Context Pack",
        "",
        "## Coding Agent Instructions",
        "- Read this Context Pack before editing.",
        (
            "- Treat the approved spec as the implementation contract, stronger than chat or "
            "stale repo docs."
        ),
        "- Stay inside approved scope and acceptance criteria.",
        "- Update repo-owned docs when shipped behavior changes.",
        (
            "- Use the SpecGate CLI for the handoff loop (`specgate work ...`, "
            "`specgate delivery report ...`, `specgate delivery review ...`)."
        ),
        (
            "- If blocked by ambiguity, report it with "
            "`specgate delivery report <ref> --file blocked.json` (JSON body: "
            '`event_type: "coding_agent.blocked_ambiguity"` plus a summary '
            "naming the decision needed)."
        ),
        "",
        "## Execution Brief",
        f"- Work item: {cr_key or 'unknown'}",
        f"- Title: {change_request.get('title') or ''}",
        (
            f"- Feature: {feature_key} (v{feature.get('version')}, {feature.get('status') or ''})"
            if feature_key
            else "- Feature: none"
        ),
        f"- Work type: {work_type}",
        f"- Source artifact: {handoff_source_artifact_id or 'missing'}",
        f"- Canonical artifact: {canonical_artifact_id or 'missing'}",
        "",
    ]
    if quick_mode:
        lines.extend(
            [
                "## Quick Handoff Note",
                (
                    "This Context Pack was approved from the quick ChangeRequest review gate "
                    "without generating a full role-tagged spec artifact. Treat the "
                    "ChangeRequest and its acceptance criteria as the source of truth."
                ),
                "",
                "## Source Evidence",
                source_evidence or "_No source evidence captured._",
                "",
            ]
        )
    lines.extend(
        [
            "## What To Build",
            str(change_request.get("intent_md") or ""),
            "",
            "## Acceptance Criteria",
            str(ac),
            "",
        ]
    )
    # Quality gates that did not pass at handoff (incl. when the human used the
    # Execute-anyway escape hatch) — the agent gets the gate hints rather than
    # silently losing them. The delivery_review verdict is excluded (post-build).
    if unresolved_gates:
        lines.extend(
            [
                "## Unresolved Quality Gates",
                "_These quality gates did not pass at handoff. Account for them as you implement._",
                unresolved_gates,
                "",
            ]
        )
    lines.extend(_manifest_sections(manifest))
    lines.extend(_role_sections(artifact_bundle, quick_mode))
    # Feature reference attachments the product team explicitly opted into the
    # coding-agent handoff (audience coding_agent/both) — links, files, bug
    # screenshots. gate-only attachments never reach here.
    lines.extend(
        render_attachments_section(
            attachments,
            audiences=(AUDIENCE_CODING_AGENT, AUDIENCE_BOTH),
            base_url=doc_registry_base_url(),
        )
    )
    lines.extend(
        [
            "## Risks And Guardrails",
            "\n".join(
                part
                for part in [
                    reference.strip(),
                    "### Stale Knowledge Warnings\n" + warning_lines,
                    "- Do not expand beyond the approved ChangeRequest scope.",
                    (
                        "- Do not update the Feature canonical artifact until "
                        "implementation is validated."
                    ),
                ]
                if part
            ),
            "",
        ]
    )
    lines.extend(
        [
            "## Implementation Guidance",
            (
                "- Treat the approved role-based spec documents and implementation plan "
                "sections as the implementation contract."
            ),
            "- Keep changes scoped to the acceptance criteria and guardrails above.",
            (
                "- Report changed files, test results, unresolved blockers, and any "
                "spec mismatch back on the ChangeRequest."
            ),
        ]
    )
    return "\n".join(lines)


def generate_context_pack(
    change_request_id: str,
    *,
    quick_mode: bool = False,
    source_evidence: str = "",
) -> dict[str, Any]:
    client = DocRegistryClient(doc_registry_base_url())
    change_request = client.get_change_request(change_request_id)
    feature_id = str(change_request.get("feature_id") or "").strip()
    feature = client.get_workboard_feature(feature_id) if feature_id else {}
    warnings = client.list_workboard_stale_warnings(change_request_id=change_request_id)
    # Unresolved gate verdicts ride the pack so the Execute-anyway escape hatch
    # does not silently drop gate guidance (e.g. the AC-verifiability restatement).
    try:
        gate_runs = client.list_change_request_gate_runs(change_request_id)
    except Exception:
        gate_runs = []
    source_artifact_id = str(
        change_request.get("lead_artifact_id") or feature.get("canonical_artifact_id") or ""
    )
    artifact_bundle = (
        client.load_artifact_handoff_bundle(source_artifact_id) if source_artifact_id else {}
    )
    # Read the source artifact to extract governance_level from its snapshot.
    # Best-effort: a missing/failing artifact read does not block the pack render.
    governance_level = ""
    if source_artifact_id:
        try:
            source_artifact = client.get_artifact(source_artifact_id)
            governance_level = _snapshot_governance_level(
                str(source_artifact.get("gates_profile_snapshot_json") or "")
            )
        except Exception:
            logger.warning(
                "context pack: failed to read source artifact %s for governance_level",
                source_artifact_id,
                exc_info=True,
            )
    attachments = []
    if feature:
        # Attachments are keyed by the feature KEY (feature-backed artifacts
        # publish feature_id=feature.key, and the UI writes attachments under
        # that), not the feature UUID — fetch by key so the handoff actually
        # sees opted-in references.
        try:
            attachments = client.list_feature_attachments(
                str(feature.get("key") or feature.get("id") or "")
            )
        except Exception:
            attachments = []
    markdown = render_context_pack(
        change_request=change_request,
        feature=feature,
        warnings=warnings,
        artifact_bundle=artifact_bundle,
        source_artifact_id=source_artifact_id,
        quick_mode=quick_mode,
        source_evidence=source_evidence,
        attachments=attachments,
        gate_runs=gate_runs,
    )
    # The normal handoff no longer persists a derived Context Pack artifact —
    # the pack is assembled on-read by the MCP resource (specgate://context-pack/
    # {cr}) from the approved spec artifact. Only the QUICK lane (no full PRD/spec
    # artifact) still publishes a source-of-truth artifact, storing the rendered
    # handoff as implementation_plan (the resource falls back to it when there is
    # no prd/spec to assemble).
    artifact: dict[str, Any] = {}
    change_request_out: dict[str, Any] = change_request
    if quick_mode:
        stamp = datetime.now(UTC).strftime("%Y%m%d%H%M%S")
        artifact = client.post_artifact(
            {
                "feature_id": "",
                "request_type": "change_request",
                "impact_level": "medium",
                "artifact_phase": "phase2",
                "artifact_completeness": "full",
                "version": f"v0.{stamp}",
                # POST /artifacts only accepts create statuses; promote to approved
                # via the status PATCH below.
                "status": "draft",
                "governance_version": governance_version(),
                "impacted_services": [],
                "files": {
                    "implementation_plan": _b64(markdown),
                },
            }
        )
        artifact_id = str(artifact.get("id") or artifact.get("artifact_id") or "")
        if artifact_id:
            artifact = client.patch_artifact_status(artifact_id, "approved")
            change_request_out = client.patch_change_request_context_pack_artifact(
                change_request_id,
                artifact_id,
            )
    result: dict[str, Any] = {
        "artifact": artifact,
        "change_request": change_request_out,
        "content_md": markdown,
        "warnings": warnings,
    }
    if governance_level:
        result["governance_level"] = governance_level
    return result
