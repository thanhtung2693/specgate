from __future__ import annotations

import json

import httpx
from fastapi.testclient import TestClient

from specgate_agents.governance.registry.client import DocRegistryClient
from specgate_agents.governance.webapp import app


class _AsyncNoop:
    """Awaitable no-op stand-in for an async coroutine function (e.g. settings hydration)."""

    async def __call__(self, *_args, **_kwargs) -> None:
        return None


def test_doc_registry_client_workboard_methods_unwrap_huma_body() -> None:
    seen: list[tuple[str, str, dict | None]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read()
        seen.append((request.method, request.url.path, None if not body else json.loads(body)))
        if request.url.path == "/workboard/extractions" and request.method == "POST":
            return httpx.Response(200, json={"body": {"id": "ext-1", "status": "proposed"}})
        if (
            request.url.path == "/workboard/extractions/ext-1/approve"
            and request.method == "POST"
        ):
            return httpx.Response(
                200,
                json={
                    "body": {
                        "features": [],
                        "change_requests": [],
                        "rejected_features": [],
                        "source_derived_artifact_ids": [],
                        "warnings": [],
                    }
                },
            )
        if (
            request.url.path == "/workboard/change-requests/cr-1/context-pack-artifact"
            and request.method == "POST"
        ):
            return httpx.Response(
                200,
                json={"body": {"id": "cr-1", "context_pack_artifact_id": "artifact-cp"}},
            )
        return httpx.Response(404)

    client = DocRegistryClient("http://registry.test", transport=httpx.MockTransport(handler))

    assert client.create_workboard_extraction({"source_kind": "document"})["id"] == "ext-1"
    assert client.approve_workboard_extraction("ext-1", {"approved_by": "pm"})["features"] == []
    assert (
        client.patch_change_request_context_pack_artifact("cr-1", "artifact-cp")[
            "context_pack_artifact_id"
        ]
        == "artifact-cp"
    )
    assert seen[1][1] == "/workboard/extractions/ext-1/approve"


async def test_doc_registry_client_async_workboard_methods_unwrap_huma_body() -> None:
    seen: list[tuple[str, str, dict | None]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read()
        seen.append((request.method, request.url.path, None if not body else json.loads(body)))
        if request.url.path == "/workboard/extractions" and request.method == "POST":
            return httpx.Response(200, json={"body": {"id": "ext-1", "status": "proposed"}})
        if (
            request.url.path == "/workboard/extractions/ext-1/approve"
            and request.method == "POST"
        ):
            return httpx.Response(
                200,
                json={
                    "body": {
                        "features": [],
                        "change_requests": [],
                        "rejected_features": [],
                        "source_derived_artifact_ids": [],
                        "warnings": [],
                    }
                },
            )
        return httpx.Response(404)

    client = DocRegistryClient("http://registry.test", transport=httpx.MockTransport(handler))

    assert (await client.acreate_workboard_extraction({"source_kind": "document"}))["id"] == "ext-1"
    approved = await client.aapprove_workboard_extraction("ext-1", {"approved_by": "pm"})
    assert approved["features"] == []
    assert seen[1][1] == "/workboard/extractions/ext-1/approve"


def test_format_acceptance_criteria_handles_quick_work_item_string_array() -> None:
    from specgate_agents.governance.board.context_pack import _format_acceptance_criteria

    out = _format_acceptance_criteria('["Buyer sees balance", "One-tap apply"]')
    assert out == "- Buyer sees balance\n- One-tap apply"


def test_format_acceptance_criteria_handles_rich_done_shape() -> None:
    from specgate_agents.governance.board.context_pack import _format_acceptance_criteria

    out = _format_acceptance_criteria(
        '[{"id":"a","text":"AC1","done":true},{"id":"b","text":"AC2","done":false}]'
    )
    # done flag is intentionally dropped; engineering only needs the text
    assert out == "- AC1\n- AC2"


def test_format_acceptance_criteria_handles_empty_or_invalid_payload() -> None:
    from specgate_agents.governance.board.context_pack import _format_acceptance_criteria

    assert "_No acceptance criteria captured._" in _format_acceptance_criteria(None)
    assert "_No acceptance criteria captured._" in _format_acceptance_criteria("")
    assert "_No acceptance criteria captured._" in _format_acceptance_criteria("[]")
    # Malformed JSON falls back to the raw string rather than crashing
    assert _format_acceptance_criteria("not json") == "not json"


def test_render_context_pack_includes_implementation_artifact_detail() -> None:
    from specgate_agents.governance.board.context_pack import render_context_pack

    markdown = render_context_pack(
        change_request={
            "id": "cr-1",
            "key": "CR-CHECKOUT",
            "title": "Add checkout reminders",
            "intent_md": "Remind buyers before checkout expires.",
            "work_type": "new_feature",
            "acceptance_criteria_json": json.dumps(["Buyer sees expiry warning."]),
            "lead_artifact_id": "artifact-approved",
        },
        feature={
            "id": "feature-1",
            "key": "checkout-reminders",
            "status": "candidate",
            "version": 3,
            "canonical_artifact_id": "artifact-canonical",
        },
        warnings=[{"code": "linked_knowledge_newer", "message": "Review policy docs."}],
        artifact_bundle={
            "prd": "# PRD\n\nShow a warning before checkout expires.",
            "spec": "## API\n\nExpose expiry timestamp.",
            "tasks_fe": "- Add banner component.",
            "tasks_be": "- Return expiry timestamp.",
            "tasks_qa": "- Test expired checkout.",
            "rollout": "- Ship behind checkout flag.",
            "risks": "- Timer drift.",
        },
        source_artifact_id="artifact-approved",
    )

    assert "## Execution Brief" in markdown
    assert "## Coding Agent Instructions" in markdown
    assert (
        "- If blocked by ambiguity, report it with "
        "`specgate delivery report <ref> --file blocked.json` (JSON body: "
        '`event_type: "coding_agent.blocked_ambiguity"` plus a summary '
        "naming the decision needed)."
        in markdown
    )
    assert "- Work item: CR-CHECKOUT" in markdown
    assert "- Source artifact: artifact-approved" in markdown
    # Feature line collapses key + version + status onto one row; the
    # standalone `## Linked Feature` and `## Canonical Artifact` sections
    # were removed because their content is already in Execution Brief.
    assert "- Feature: checkout-reminders (v3, candidate)" in markdown
    assert "## Linked Feature" not in markdown
    assert "## Canonical Artifact" not in markdown
    assert "## Goal" not in markdown
    assert "Implementation kind:" not in markdown
    assert "## Approved Source Documents" not in markdown
    assert "## What To Build" in markdown
    assert "## Spec" in markdown
    assert "Show a warning before checkout expires." in markdown
    assert "Expose expiry timestamp." in markdown
    assert "## Implementation Plan" in markdown
    assert "- Add banner component." in markdown
    assert "- Return expiry timestamp." in markdown
    assert "## Verification" in markdown
    assert "- Test expired checkout." in markdown
    assert "## Reference" in markdown
    assert "- Ship behind checkout flag." in markdown
    assert "## Risks And Guardrails" in markdown
    assert "- linked_knowledge_newer: Review policy docs." in markdown
    assert "## Implementation Guidance" in markdown
    assert "## Frontend Tasks" not in markdown
    assert "## Backend Tasks" not in markdown
    assert "## QA And Verification" not in markdown
    assert "## PRD" not in markdown
    # Stale GitLab reference was removed in favor of the ChangeRequest.
    assert "GitLab handoff issue" not in markdown


def test_render_context_pack_surfaces_unresolved_quality_gates() -> None:
    """Gate verdicts that did not pass ride the pack with their hints (so the
    Execute-anyway escape hatch does not silently drop gate guidance). Passing
    gates and the post-build delivery_review verdict are excluded."""
    from specgate_agents.governance.board.context_pack import render_context_pack

    markdown = render_context_pack(
        change_request={
            "id": "cr-1",
            "key": "CR-1",
            "title": "X",
            "intent_md": "do x",
            "work_type": "new_feature",
            "acceptance_criteria_json": json.dumps(["AC one"]),
        },
        feature={"id": "f1", "key": "feat", "status": "planned", "version": 1},
        warnings=[],
        gate_runs=[
            {
                "gate": "scope_clear", "state": "pass",
                "hint": "", "created_at": "2026-06-12T00:00:00Z",
            },
            {
                "gate": "acceptance_criteria_verifiable",
                "state": "warn",
                "hint": "Restate AC-1 as: p95 latency < 200ms",
                "created_at": "2026-06-12T01:00:00Z",
            },
            {
                "gate": "rollback_plan_present", "state": "fail",
                "hint": "No rollback noted", "created_at": "2026-06-12T02:00:00Z",
            },
            {
                "gate": "delivery_review", "state": "fail",
                "hint": "post-build", "created_at": "2026-06-12T03:00:00Z",
            },
        ],
    )

    assert "## Unresolved Quality Gates" in markdown
    assert "acceptance_criteria_verifiable" in markdown
    assert "Restate AC-1 as: p95 latency < 200ms" in markdown
    assert "rollback_plan_present" in markdown
    assert "No rollback noted" in markdown
    # passing gate and the post-build delivery_review are not listed here
    assert "scope_clear** (pass)" not in markdown
    gates_idx = markdown.index("## Unresolved Quality Gates")
    assert "delivery_review" not in markdown[gates_idx:]


def test_render_context_pack_includes_full_spec_without_truncation() -> None:
    from specgate_agents.governance.board.context_pack import render_context_pack

    long_spec = "## API\n\n" + ("spec body line.\n" * 400) + "\nEND_OF_SPEC_MARKER"
    long_prd = "# PRD\n\n" + ("intent line.\n" * 400) + "\nEND_OF_PRD_MARKER"

    markdown = render_context_pack(
        change_request={"id": "cr-1", "key": "CR-1", "title": "T", "intent_md": "do it"},
        feature={"id": "f-1", "key": "feat", "status": "planned", "version": 1},
        warnings=[],
        artifact_bundle={"prd": long_prd, "spec": long_spec},
        source_artifact_id="artifact-1",
    )

    # The spec is the implementation contract — it must ship in full, not a preview.
    assert "END_OF_SPEC_MARKER" in markdown
    assert "END_OF_PRD_MARKER" in markdown
    assert "Trimmed for handoff preview" not in markdown


def test_render_context_pack_surfaces_manifest_scope_and_design_refs() -> None:
    from specgate_agents.governance.board.context_pack import render_context_pack

    manifest = json.dumps(
        {
            "impacted_services": ["checkout-api", "loyalty-svc"],
            "impacted_apps": ["web-checkout"],
            "files": ["api/checkout.go", "web/checkout.tsx"],
            "design_refs": [
                {
                    "type": "figma",
                    "url": "https://figma.com/file/ABC/Checkout?node-id=1-2",
                    "file_key": "ABC",
                    "node_id": "1-2",
                }
            ],
        }
    )

    markdown = render_context_pack(
        change_request={"id": "cr-1", "key": "CR-1", "title": "T", "intent_md": "do it"},
        feature={"id": "f-1", "key": "feat", "status": "planned", "version": 1},
        warnings=[],
        artifact_bundle={"prd": "# PRD", "spec": "## API", "manifest": manifest},
        source_artifact_id="artifact-1",
    )

    assert "## Scope & Blast Radius" in markdown
    assert "checkout-api" in markdown
    assert "loyalty-svc" in markdown
    assert "web-checkout" in markdown
    assert "api/checkout.go" in markdown
    assert "web/checkout.tsx" in markdown
    assert "## Design References" in markdown
    assert "https://figma.com/file/ABC/Checkout?node-id=1-2" in markdown
    # Raw manifest JSON should not be dumped once it is parsed into sections.
    assert '"impacted_services"' not in markdown


def test_generate_quick_context_pack_publishes_approved_and_patches_cr(monkeypatch) -> None:
    from specgate_agents.governance.board import context_pack as context_pack_mod

    calls: dict[str, dict] = {}

    class FakeClient:
        def __init__(self, *_args, **_kwargs) -> None:
            pass

        def get_change_request(self, change_request_id: str) -> dict:
            assert change_request_id == "cr-quick"
            return {
                "id": "cr-quick",
                "key": "CR-IMPORT-ERRORS",
                "feature_id": "feature-uuid-1",
                "title": "Clarify import error states",
                "intent_md": "Make import failures actionable.",
                "work_type": "bug_fix",
                "acceptance_criteria_json": json.dumps(["Merchant sees the auth issue."]),
                "lead_artifact_id": "",
            }

        def get_workboard_feature(self, feature_id: str) -> dict:
            assert feature_id == "feature-uuid-1"
            return {
                "id": "feature-uuid-1",
                "key": "FEAT-PRODUCT-IMPORT",
                "status": "active",
                "version": 2,
                "canonical_artifact_id": "artifact-canonical",
            }

        def list_workboard_stale_warnings(self, **_kwargs) -> list[dict]:
            return []

        def list_feature_attachments(self, feature_id: str) -> list[dict]:
            calls["attachments_feature_id"] = feature_id
            return []

        def load_artifact_handoff_bundle(self, artifact_id: str) -> dict[str, str]:
            assert artifact_id == "artifact-canonical"
            return {
                "prd": "# Canonical PRD\n\nClarify import failures.",
                "spec": "# Canonical Spec\n\nReturn actionable import errors.",
                "tasks_fe": "- Show auth error remediation.",
                "tasks_be": "- Normalize provider error codes.",
                "tasks_qa": "- Verify failed auth import.",
                "rollout": "- Release with import support notes.",
                "risks": "- Provider error text may vary.",
            }

        def post_artifact(self, body: dict) -> dict:
            calls["artifact"] = body
            return {"id": "artifact-context-pack", **body}

        def patch_artifact_status(
            self, artifact_id: str, status: str, *, manifest_json: str | None = None
        ) -> dict:
            calls["status_patch"] = {"artifact_id": artifact_id, "status": status}
            return {"id": artifact_id, "status": status}

        def patch_change_request_context_pack_artifact(
            self, change_request_id: str, artifact_id: str
        ) -> dict:
            calls["patch_context_pack"] = {
                "change_request_id": change_request_id,
                "artifact_id": artifact_id,
            }
            return {
                "id": change_request_id,
                "context_pack_artifact_id": artifact_id,
                "lead_artifact_id": "",
            }

    monkeypatch.setattr(context_pack_mod, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(context_pack_mod, "doc_registry_base_url", lambda: "http://registry.test")

    result = context_pack_mod.generate_context_pack(
        "cr-quick",
        quick_mode=True,
        source_evidence="Merchant says import errors are confusing.",
    )

    # Doc Registry rejects "approved" on create, so the quick lane posts a draft
    # then promotes it via the status PATCH.
    assert calls["artifact"]["status"] == "draft"
    assert calls["status_patch"] == {"artifact_id": "artifact-context-pack", "status": "approved"}
    assert result["artifact"]["status"] == "approved"
    markdown = result["content_md"]
    assert "## Quick Handoff Note" in markdown
    assert "## Source Evidence" in markdown
    # Non-goals are now consolidated into Risks And Guardrails (no separate
    # `## Non-goals` heading) and `## Approved Source Documents` / `## QA
    # Checklist` were dropped because they duplicated the Execution Brief and
    # the QA section respectively.
    assert "## Non-goals" not in markdown
    assert "## QA Checklist" not in markdown
    assert "## Approved Source Documents" not in markdown
    assert "Do not expand beyond the approved ChangeRequest scope." in markdown
    assert "## Implementation Plan" in markdown
    assert "- Show auth error remediation." in markdown
    assert "- Normalize provider error codes." in markdown
    assert "## Verification" in markdown
    assert "- Verify failed auth import." in markdown
    assert "## Frontend Tasks" not in markdown
    assert "## Backend Tasks" not in markdown
    assert calls["patch_context_pack"] == {
        "change_request_id": "cr-quick",
        "artifact_id": "artifact-context-pack",
    }
    assert result["change_request"]["lead_artifact_id"] == ""
    # Attachments must be fetched by the feature KEY (matches how the UI writes +
    # how artifacts publish feature_id), never the feature UUID.
    assert calls["attachments_feature_id"] == "FEAT-PRODUCT-IMPORT"


def test_generate_quick_context_pack_without_feature_publishes_standalone_artifact(
    monkeypatch,
) -> None:
    from specgate_agents.governance.board import context_pack as context_pack_mod

    calls: dict[str, dict] = {}

    class FakeClient:
        def __init__(self, *_args, **_kwargs) -> None:
            pass

        def get_change_request(self, change_request_id: str) -> dict:
            assert change_request_id == "cr-featureless"
            return {
                "id": "cr-featureless",
                "key": "CR-FEATURELESS",
                "feature_id": "",
                "title": "Fix quick-path smoke cleanup",
                "intent_md": "Keep the quick Context Pack self-contained.",
                "work_type": "bug_fix",
                "acceptance_criteria_json": json.dumps(["Smoke artifact is created."]),
                "lead_artifact_id": "",
            }

        def get_workboard_feature(self, _feature_id: str) -> dict:
            raise AssertionError("feature lookup should not run for featureless CRs")

        def list_workboard_stale_warnings(self, **_kwargs) -> list[dict]:
            return []

        def list_feature_attachments(self, _feature_id: str) -> list[dict]:
            raise AssertionError("feature attachments should not load without a feature")

        def post_artifact(self, body: dict) -> dict:
            calls["artifact"] = body
            return {"id": "artifact-context-pack", **body}

        def patch_artifact_status(
            self, artifact_id: str, status: str, *, manifest_json: str | None = None
        ) -> dict:
            return {"id": artifact_id, "status": status}

        def patch_change_request_context_pack_artifact(
            self, change_request_id: str, artifact_id: str
        ) -> dict:
            return {"id": change_request_id, "context_pack_artifact_id": artifact_id}

    monkeypatch.setattr(context_pack_mod, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(context_pack_mod, "doc_registry_base_url", lambda: "http://registry.test")

    result = context_pack_mod.generate_context_pack("cr-featureless", quick_mode=True)

    assert calls["artifact"]["feature_id"] == ""
    assert "Feature: none" in result["content_md"]
    assert result["artifact"]["status"] == "approved"


def test_doc_registry_client_gate_runs_refresh_posts_evaluations() -> None:
    seen: list[tuple[str, str, dict | None]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read()
        seen.append((request.method, request.url.path, None if not body else json.loads(body)))
        if request.url.path == "/workboard/change-requests/cr-1/gate-runs/refresh":
            return httpx.Response(
                200, json={"body": {"items": [{"gate": "rollback_plan_present", "state": "pass"}]}}
            )
        if request.url.path == "/workboard/change-requests/cr-1/gate-runs":
            return httpx.Response(
                200, json={"body": {"items": [{"gate": "rollback_plan_present", "state": "pass"}]}}
            )
        return httpx.Response(404)

    client = DocRegistryClient("http://registry.test", transport=httpx.MockTransport(handler))
    evals = [{"gate": "rollback_plan_present", "state": "needs_human_review", "confidence": 0.5}]
    rows = client.refresh_change_request_gate_runs("cr-1", evals)
    assert rows and rows[0]["gate"] == "rollback_plan_present"

    listed = client.list_change_request_gate_runs("cr-1", limit=10)
    assert listed and listed[0]["state"] == "pass"

    # The refresh POST carries the evaluations[] the backend expects.
    post = next(s for s in seen if s[1].endswith("/gate-runs/refresh"))
    assert post[0] == "POST"
    assert post[2] == {"evaluations": evals}


def test_classify_route_endpoint_returns_suggestion_shape(monkeypatch) -> None:
    captured: dict[str, object] = {}

    class FakeStructuredModel:
        async def ainvoke(self, messages: list[object], config=None):  # noqa: ANN001, ANN202
            from specgate_agents.governance.board.route_classifier import RouteJudgment

            captured["messages"] = messages
            return RouteJudgment(
                route="quick", confidence=0.93, rationale="Small contained bug fix."
            )

    class FakeModel:
        def with_structured_output(self, schema: type[object], **_kwargs) -> FakeStructuredModel:
            captured["schema"] = schema
            return FakeStructuredModel()

    class FakeClient:
        def __init__(self, *_args, **_kwargs) -> None:
            pass

        def get_change_request(self, change_request_id: str) -> dict:
            assert change_request_id == "cr-route"
            return {
                "id": "cr-route",
                "feature_id": "feature-1",
                "title": "Fix import error copy",
                "intent_md": "Surface the auth error on a failed import.",
                "work_type": "bug_fix",
            }

        def get_workboard_feature(self, feature_id: str) -> dict:
            assert feature_id == "feature-1"
            return {"id": "feature-1", "key": "FEAT-IMPORT", "status": "active"}

    monkeypatch.setattr(
        "specgate_agents.governance.board.route_suggestion.DocRegistryClient", FakeClient
    )
    monkeypatch.setattr(
        "specgate_agents.governance.board.route_suggestion.doc_registry_base_url",
        lambda: "http://registry.test",
    )
    monkeypatch.setattr(
        "specgate_agents.governance.board.route_suggestion._hydrate_model_settings",
        _AsyncNoop(),
    )
    monkeypatch.setattr(
        "specgate_agents.governance.board.route_suggestion.ensure_llm_env", lambda: True
    )
    monkeypatch.setattr(
        "specgate_agents.governance.board.route_suggestion.build_model", lambda: FakeModel()
    )

    client = TestClient(app)
    response = client.post("/workboard/change-requests/cr-route/classify-route")
    assert response.status_code == 200
    body = response.json()
    assert body == {
        "change_request_id": "cr-route",
        "route": "quick",
        "confidence": 0.93,
        "rationale": "Small contained bug fix.",
    }
    from specgate_agents.governance.board.route_classifier import RouteJudgment

    assert captured["schema"] is RouteJudgment


def test_classify_route_endpoint_falls_back_to_full_without_llm(monkeypatch) -> None:
    class FakeClient:
        def __init__(self, *_args, **_kwargs) -> None:
            pass

        def get_change_request(self, _id: str) -> dict:
            return {"id": _id, "feature_id": "", "title": "x", "work_type": "bug_fix"}

    monkeypatch.setattr(
        "specgate_agents.governance.board.route_suggestion.DocRegistryClient", FakeClient
    )
    monkeypatch.setattr(
        "specgate_agents.governance.board.route_suggestion.doc_registry_base_url",
        lambda: "http://registry.test",
    )
    monkeypatch.setattr(
        "specgate_agents.governance.board.route_suggestion._hydrate_model_settings",
        _AsyncNoop(),
    )
    monkeypatch.setattr(
        "specgate_agents.governance.board.route_suggestion.ensure_llm_env", lambda: False
    )

    client = TestClient(app)
    response = client.post("/workboard/change-requests/cr-x/classify-route")
    assert response.status_code == 200
    assert response.json()["route"] == "full"


def test_context_pack_endpoint_passes_quick_mode_through(monkeypatch) -> None:
    captured: dict[str, object] = {}

    def fake_generate(change_request_id: str, *, quick_mode: bool, source_evidence: str) -> dict:
        captured["args"] = {
            "change_request_id": change_request_id,
            "quick_mode": quick_mode,
            "source_evidence": source_evidence,
        }
        return {"artifact": {"status": "approved"}, "content_md": "# Quick", "warnings": []}

    monkeypatch.setattr("specgate_agents.governance.webapp.generate_context_pack", fake_generate)

    client = TestClient(app)
    response = client.post(
        "/workboard/change-requests/cr-quick/context-pack",
        json={"quick_mode": True, "source_evidence": "Merchant says errors are confusing."},
    )
    assert response.status_code == 200
    assert captured["args"] == {
        "change_request_id": "cr-quick",
        "quick_mode": True,
        "source_evidence": "Merchant says errors are confusing.",
    }


def test_context_pack_endpoint_defaults_to_full_without_body(monkeypatch) -> None:
    captured: dict[str, object] = {}

    def fake_generate(change_request_id: str, *, quick_mode: bool, source_evidence: str) -> dict:
        captured["args"] = {
            "change_request_id": change_request_id,
            "quick_mode": quick_mode,
            "source_evidence": source_evidence,
        }
        return {"artifact": {"status": "draft"}, "content_md": "# Full", "warnings": []}

    monkeypatch.setattr("specgate_agents.governance.webapp.generate_context_pack", fake_generate)

    client = TestClient(app)
    response = client.post("/workboard/change-requests/cr-full/context-pack")
    assert response.status_code == 200
    assert captured["args"] == {
        "change_request_id": "cr-full",
        "quick_mode": False,
        "source_evidence": "",
    }
