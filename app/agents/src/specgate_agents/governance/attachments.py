"""Feature reference-attachment helpers shared by the coding-agent context-pack
and the quality-gate judge.

Attachments are fetched from Doc Registry (``/features/{id}/attachments``). Each
carries an ``audience`` flag — ``gate`` (reviewer), ``coding_agent`` (handoff),
or ``both`` — that controls who sees it. The coding-agent context-pack renders
``coding_agent`` / ``both``; the gate renders ``gate`` / ``both``. The default at
write time is gate-only, so reaching the coding agent is a deliberate opt-in.
"""

from __future__ import annotations

from collections.abc import Iterable
from typing import Any

# Audience values mirror the Go artifactattachment.Audience constants.
AUDIENCE_GATE = "gate"
AUDIENCE_CODING_AGENT = "coding_agent"
AUDIENCE_BOTH = "both"


def filter_by_audience(
    attachments: Iterable[dict[str, Any]] | None,
    audiences: Iterable[str],
) -> list[dict[str, Any]]:
    """Return attachments whose ``audience`` is in ``audiences``."""
    wanted = {str(a).strip() for a in audiences}
    out: list[dict[str, Any]] = []
    for att in attachments or []:
        if isinstance(att, dict) and str(att.get("audience") or "").strip() in wanted:
            out.append(att)
    return out


def _attachment_line(att: dict[str, Any], *, base_url: str) -> str:
    """One markdown bullet for an attachment. File/image rows point at the Doc
    Registry content proxy (container-reachable), never an S3 URL."""
    title = str(att.get("title") or "").strip()
    note = str(att.get("note") or "").strip()
    kind = str(att.get("kind") or "").strip() or "file"
    label = title or kind
    url = str(att.get("url") or "").strip()
    file_id = str(att.get("governance_file_id") or "").strip()
    # File/image rows ALWAYS resolve to the Doc Registry content proxy, never an
    # S3 URL — even if a url field is somehow populated. Links use their url.
    if kind in ("file", "image") and file_id:
        base = base_url.rstrip("/")
        url = f"{base}/governance/files/{file_id}/content"
    elif not url and file_id:
        base = base_url.rstrip("/")
        url = f"{base}/governance/files/{file_id}/content"
    target = url or "(no location)"
    suffix = f" — {note}" if note else ""
    return f"- [{kind}] {label}: {target}{suffix}"


def render_attachments_section(
    attachments: Iterable[dict[str, Any]] | None,
    *,
    audiences: Iterable[str],
    base_url: str = "",
    heading: str = "## Reference Attachments",
) -> list[str]:
    """Render the audience-filtered attachments as markdown lines (heading +
    bullets), or ``[]`` when none match. Returned as a list to ``extend`` into an
    existing line buffer."""
    matched = filter_by_audience(attachments, audiences)
    if not matched:
        return []
    lines = [heading]
    lines.extend(_attachment_line(att, base_url=base_url) for att in matched)
    lines.append("")
    return lines


__all__ = [
    "AUDIENCE_GATE",
    "AUDIENCE_CODING_AGENT",
    "AUDIENCE_BOTH",
    "filter_by_audience",
    "render_attachments_section",
]
