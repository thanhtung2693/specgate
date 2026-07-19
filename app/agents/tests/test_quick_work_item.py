"""Unit tests for quick_work_item AC drafting and creation."""

import pytest


@pytest.mark.asyncio
async def test_draft_acceptance_criteria_requires_llm_when_not_supplied(monkeypatch):
    """When ACs are omitted, missing model config is surfaced instead of generic ACs."""
    monkeypatch.setattr(
        "specgate_agents.governance.board.quick_work_item.ensure_llm_env",
        lambda: False,
    )
    from specgate_agents.governance.board.quick_work_item import _draft_acceptance_criteria

    with pytest.raises(ValueError, match="acceptance criteria"):
        await _draft_acceptance_criteria("Fix login", "Users can't log in")


@pytest.mark.asyncio
async def test_create_quick_work_item_requires_workspace_before_work(monkeypatch):
    import specgate_agents.governance.board.quick_work_item as quick

    monkeypatch.setattr(quick, "DocRegistryClient", lambda *_args: pytest.fail("registry called"))

    with pytest.raises(ValueError, match="workspace_id is required"):
        await quick.create_quick_work_item(
            title="Fix login",
            description="Users cannot log in.",
            acceptance_criteria=["Users can log in."],
        )


@pytest.mark.asyncio
async def test_create_quick_work_item_uses_supplied_acceptance_criteria(monkeypatch):
    """Caller-provided ACs are preserved instead of being replaced by the fallback drafter."""
    import specgate_agents.governance.board.quick_work_item as quick

    created_cr_body = {}

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        async def aupsert_feature_by_key(self, key: str, name: str, *, workspace_id: str):
            assert key == "feat-login"
            assert name == "Login"
            assert workspace_id == "ws-quick"
            return {"id": "feature-1"}

        async def acreate_change_request(self, body, *, workspace_id: str):
            assert workspace_id == "ws-quick"
            created_cr_body.update(body)
            return {"id": "cr-1", "key": "CR-1"}

    async def fail_if_called(_title: str, _description: str):
        raise AssertionError("drafting should not run when ACs are supplied")

    monkeypatch.setattr(quick, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(quick, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(quick, "_draft_acceptance_criteria", fail_if_called)

    result = await quick.create_quick_work_item(
        title="Login",
        description="Fix login",
        feature_key="feat-login",
        workspace_id="ws-quick",
        acceptance_criteria=[" Users can log in. ", "", "Invalid login shows an error."],
    )

    assert result["phase"] == "ready"
    assert result["acceptance_criteria"] == [
        "Users can log in.",
        "Invalid login shows an error.",
    ]
    assert created_cr_body["acceptance_criteria_json"] == (
        '["Users can log in.", "Invalid login shows an error."]'
    )


@pytest.mark.asyncio
async def test_create_quick_work_item_requires_acceptance_criteria_without_model(monkeypatch):
    """Quick work without ACs fails honestly instead of creating a generic criterion."""
    import specgate_agents.governance.board.quick_work_item as quick

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        async def acreate_change_request(self, _body):
            raise AssertionError("change request must not be created without acceptance criteria")

    monkeypatch.setattr(quick, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(quick, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(quick, "ensure_llm_env", lambda: False)

    with pytest.raises(ValueError, match="acceptance criteria"):
        await quick.create_quick_work_item(
            title="Fix login",
            description="Users cannot log in.",
            workspace_id="ws-quick",
        )


@pytest.mark.asyncio
async def test_create_quick_work_item_rejects_an_empty_draft(monkeypatch):
    """A model response without criteria must not create a handoff-ready work item."""
    import specgate_agents.governance.board.quick_work_item as quick

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        async def acreate_change_request(self, _body, *, workspace_id: str):
            raise AssertionError("empty drafted criteria must not create a change request")

    async def empty_draft(_title: str, _description: str):
        return []

    monkeypatch.setattr(quick, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(quick, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(quick, "_draft_acceptance_criteria", empty_draft)

    with pytest.raises(ValueError, match="acceptance criteria"):
        await quick.create_quick_work_item(
            title="Fix login",
            description="Users cannot log in.",
            workspace_id="ws-quick",
        )


@pytest.mark.asyncio
async def test_create_quick_work_item_preserves_human_authored_bindings(monkeypatch):
    """Human-authored check bindings ride quick-work ACs into canonical CR JSON."""
    import specgate_agents.governance.board.quick_work_item as quick

    created_cr_body = {}

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        async def aupsert_feature_by_key(self, _key: str, _name: str, *, workspace_id: str):
            raise AssertionError("feature upsert should not run without feature_key")

        async def acreate_change_request(self, body, *, workspace_id: str):
            assert workspace_id == "ws-quick"
            created_cr_body.update(body)
            return {"id": "cr-1", "key": "CR-1"}

    async def fail_if_called(_title: str, _description: str):
        raise AssertionError("drafting should not run when ACs are supplied")

    monkeypatch.setattr(quick, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(quick, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(quick, "_draft_acceptance_criteria", fail_if_called)

    result = await quick.create_quick_work_item(
        title="Login",
        description="Fix login",
        workspace_id="ws-quick",
        acceptance_criteria=[
            {"text": " Users can log in. ", "verification_binding": " integration "},
            {"text": "Invalid login shows an error."},
        ],
    )

    assert result["acceptance_criteria"] == [
        {"text": "Users can log in.", "verification_binding": "integration"},
        {"text": "Invalid login shows an error."},
    ]
    assert created_cr_body["acceptance_criteria_json"] == (
        '[{"text": "Users can log in.", "verification_binding": "integration"}, '
        '{"text": "Invalid login shows an error."}]'
    )


@pytest.mark.asyncio
async def test_create_quick_work_item_without_feature_key_stays_featureless(monkeypatch):
    """Quick-route CRs do not invent Feature rows when the caller omits feature_key."""
    import specgate_agents.governance.board.quick_work_item as quick

    created_cr_body = {}

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        async def aupsert_feature_by_key(self, _key: str, _name: str, *, workspace_id: str):
            raise AssertionError("feature upsert should not run without feature_key")

        async def acreate_change_request(self, body, *, workspace_id: str):
            assert workspace_id == "ws-quick"
            created_cr_body.update(body)
            return {"id": "cr-quick", "key": "CR-QUICK"}

    monkeypatch.setattr(quick, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(quick, "doc_registry_base_url", lambda: "http://registry")

    result = await quick.create_quick_work_item(
        title="Fix flaky smoke",
        description="Small bugfix with local acceptance criteria.",
        workspace_id="ws-quick",
        acceptance_criteria=["Smoke command exits zero."],
    )

    assert "feature_id" not in created_cr_body
    assert "feature_id" not in result
    assert "feature_key" not in result
    assert result["change_request_id"] == "cr-quick"
    assert result["phase"] == "ready"


@pytest.mark.asyncio
async def test_create_quick_work_item_is_ready_without_a_persisted_pack(monkeypatch):
    """Quick packs are derived by Doc Registry, not persisted as draft artifacts."""
    import specgate_agents.governance.board.quick_work_item as quick

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        async def acreate_change_request(self, _body, *, workspace_id: str):
            assert workspace_id == "ws-quick"
            return {"id": "cr-quick", "key": "CR-QUICK"}

    monkeypatch.setattr(quick, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(quick, "doc_registry_base_url", lambda: "http://registry")

    result = await quick.create_quick_work_item(
        title="Fix startup",
        description="Port collision should be clear.",
        workspace_id="ws-quick",
        acceptance_criteria=["The error names the port."],
    )

    assert result["phase"] == "ready"
