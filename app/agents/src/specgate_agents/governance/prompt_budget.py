"""Deterministic bounds for user-authored text sent to governance models."""

from __future__ import annotations

MAX_ARTIFACT_CHARS = 48_000
MAX_RUBRIC_CHARS = 8_000
MAX_DELIVERY_REPORT_CHARS = 48_000
MAX_DELIVERY_CRITERIA_CHARS = 16_000
MAX_CHAT_DOCUMENT_CHARS = 32_000
MAX_CHAT_DOCUMENT_PART_CHARS = 12_000
MAX_CHAT_USER_MESSAGE_CHARS = 32_000
MAX_CHAT_ARTIFACT_FIELD_CHARS = 2_000
MAX_CHAT_READINESS_RUNS = 8
MAX_CHAT_READINESS_HINT_CHARS = 2_000
MAX_CHAT_READINESS_EVIDENCE_CHARS = 4_000
TRUNCATION_MARKER = "\n\n[SpecGate truncated oversized model input]\n\n"


def cap_model_text(text: str, limit: int) -> str:
    """Bound text while preserving its beginning and conclusion."""
    if limit <= 0:
        return ""
    if len(text) <= limit:
        return text
    available = limit - len(TRUNCATION_MARKER)
    if available <= 0:
        return TRUNCATION_MARKER[:limit]
    head = (available * 3) // 4
    tail = available - head
    return text[:head] + TRUNCATION_MARKER + text[-tail:]


def cap_document_bundle(bundle: dict[str, str]) -> dict[str, str]:
    """Bound the bundle without choosing documents by filename or content."""
    if not bundle:
        return {}
    per_document = min(MAX_CHAT_DOCUMENT_PART_CHARS, MAX_CHAT_DOCUMENT_CHARS // len(bundle))
    return {key: cap_model_text(str(bundle[key] or ""), per_document) for key in sorted(bundle)}
