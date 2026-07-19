from __future__ import annotations

from specgate_agents.governance.prompt_budget import (
    MAX_ARTIFACT_CHARS,
    MAX_CHAT_DOCUMENT_CHARS,
    TRUNCATION_MARKER,
    cap_document_bundle,
    cap_model_text,
)


def test_cap_model_text_bounds_and_marks_oversized_content() -> None:
    text = "start\n" + ("private implementation detail\n" * 10_000) + "end\n"

    capped = cap_model_text(text, MAX_ARTIFACT_CHARS)

    assert len(capped) <= MAX_ARTIFACT_CHARS
    assert capped.startswith("start\n")
    assert capped.endswith("end\n")
    assert TRUNCATION_MARKER in capped


def test_cap_model_text_leaves_small_content_unchanged() -> None:
    assert cap_model_text("small", MAX_ARTIFACT_CHARS) == "small"


def test_cap_document_bundle_enforces_aggregate_budget() -> None:
    bundle = {f"doc-{index}": "x" * 20_000 for index in range(5)}

    capped = cap_document_bundle(bundle)

    assert sum(len(value) for value in capped.values()) <= MAX_CHAT_DOCUMENT_CHARS
    assert capped.keys() == bundle.keys()
    assert TRUNCATION_MARKER in "".join(capped.values())
