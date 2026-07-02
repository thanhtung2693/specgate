"""Unit tests for quick_work_item AC drafting and key derivation."""
import pytest

from specgate_agents.governance.board.quick_work_item import _derive_feature_key


def test_derive_feature_key_from_issue_key():
    key = _derive_feature_key("Fix login", "APP-123")
    assert key == "app-123"


def test_derive_feature_key_from_title():
    key = _derive_feature_key("Fix Login Button Click", "")
    assert key == "fix-login-button-click"


def test_derive_feature_key_title_truncated():
    long_title = "A" * 100
    key = _derive_feature_key(long_title, "")
    assert len(key) <= 40


def test_derive_feature_key_fallback():
    key = _derive_feature_key("", "")
    assert key == "quick-bug"


@pytest.mark.asyncio
async def test_draft_acceptance_criteria_fallback_when_llm_disabled(monkeypatch):
    """When LLM env is missing, a single fallback AC is returned."""
    monkeypatch.setattr(
        "specgate_agents.governance.board.quick_work_item.ensure_llm_env",
        lambda: False,
    )
    from specgate_agents.governance.board.quick_work_item import _draft_acceptance_criteria

    acs = await _draft_acceptance_criteria("Fix login", "Users can't log in")
    assert len(acs) >= 1
    assert isinstance(acs[0], str)
    assert "Fix login" in acs[0]


@pytest.mark.asyncio
async def test_create_quick_work_item_uses_supplied_acceptance_criteria(monkeypatch):
    """Caller-provided ACs are preserved instead of being replaced by the fallback drafter."""
    import specgate_agents.governance.board.quick_work_item as quick

    created_cr_body = {}

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        async def aupsert_feature_by_key(self, key: str, name: str):
            assert key == "feat-login"
            assert name == "Login"
            return {"id": "feature-1"}

        async def acreate_change_request(self, body):
            created_cr_body.update(body)
            return {"id": "cr-1", "key": "CR-1"}

    async def fail_if_called(_title: str, _description: str):
        raise AssertionError("drafting should not run when ACs are supplied")

    def fake_context_pack(_change_request_id: str, **_kwargs):
        return {"artifact": {"id": "pack-1"}}

    monkeypatch.setattr(quick, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(quick, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(quick, "_draft_acceptance_criteria", fail_if_called)
    monkeypatch.setattr(
        "specgate_agents.governance.board.context_pack.generate_context_pack",
        fake_context_pack,
    )

    result = await quick.create_quick_work_item(
        title="Login",
        description="Fix login",
        feature_key="feat-login",
        acceptance_criteria=[" Users can log in. ", "", "Invalid login shows an error."],
    )

    assert result["phase"] == "handoff"
    assert result["acceptance_criteria"] == [
        "Users can log in.",
        "Invalid login shows an error.",
    ]
    assert created_cr_body["acceptance_criteria_json"] == (
        '["Users can log in.", "Invalid login shows an error."]'
    )


@pytest.mark.asyncio
async def test_create_quick_work_item_without_feature_key_stays_featureless(monkeypatch):
    """Quick-route CRs do not invent Feature rows when the caller omits feature_key."""
    import specgate_agents.governance.board.quick_work_item as quick

    created_cr_body = {}

    class FakeClient:
        def __init__(self, _base_url: str):
            pass

        async def aupsert_feature_by_key(self, _key: str, _name: str):
            raise AssertionError("feature upsert should not run without feature_key")

        async def acreate_change_request(self, body):
            created_cr_body.update(body)
            return {"id": "cr-quick", "key": "CR-QUICK"}

    def fake_context_pack(_change_request_id: str, **_kwargs):
        return {"artifact": {"id": "pack-quick"}}

    monkeypatch.setattr(quick, "DocRegistryClient", FakeClient)
    monkeypatch.setattr(quick, "doc_registry_base_url", lambda: "http://registry")
    monkeypatch.setattr(
        "specgate_agents.governance.board.context_pack.generate_context_pack",
        fake_context_pack,
    )

    result = await quick.create_quick_work_item(
        title="Fix flaky smoke",
        description="Small bugfix with local acceptance criteria.",
        acceptance_criteria=["Smoke command exits zero."],
    )

    assert "feature_id" not in created_cr_body
    assert "feature_id" not in result
    assert "feature_key" not in result
    assert result["change_request_id"] == "cr-quick"
    assert result["phase"] == "handoff"
