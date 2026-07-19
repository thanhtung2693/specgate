"""Opt-in live smoke test for the governance LangSmith tracing boundary.

Intentionally separate from the simulated suite. It hits a real external
service and should only run when explicitly requested:

    GOVERNANCE_LIVE_SMOKE=1 uv run pytest -m live_smoke tests/test_live_smoke_governance.py -q
"""

from __future__ import annotations

import os
import pathlib
import time
import uuid
from datetime import UTC, datetime
from typing import Any

import pytest

pytestmark = pytest.mark.live_smoke


def _load_dotenv(monkeypatch: pytest.MonkeyPatch) -> None:
    env_path = pathlib.Path(__file__).resolve().parent.parent / ".env"
    if not env_path.is_file():
        return
    for line in env_path.read_text().splitlines():
        s = line.strip()
        if not s or s.startswith("#") or "=" not in s:
            continue
        key, _, val = s.partition("=")
        if key.strip() and not os.environ.get(key.strip()):
            monkeypatch.setenv(key.strip(), val.strip().strip('"').strip("'"))


@pytest.fixture
def live_smoke_env(monkeypatch: pytest.MonkeyPatch) -> None:
    _load_dotenv(monkeypatch)
    if os.environ.get("GOVERNANCE_LIVE_SMOKE", "").lower() not in {"1", "true", "yes", "on"}:
        pytest.skip("set GOVERNANCE_LIVE_SMOKE=1 to run live governance-ops smoke tests")
    monkeypatch.setenv("LANGCHAIN_TRACING_V2", "true")
    monkeypatch.setenv("LANGSMITH_TRACING_V2", "true")
    monkeypatch.setenv("LANGSMITH_TRACING", "true")


@pytest.mark.usefixtures("live_smoke_env")
def test_live_langsmith_trace_roundtrip() -> None:
    """LangSmith accepts and returns a minimal governance-ops smoke trace."""
    if not (os.environ.get("LANGCHAIN_API_KEY") or os.environ.get("LANGSMITH_API_KEY")):
        pytest.skip("missing LANGCHAIN_API_KEY or LANGSMITH_API_KEY")

    from langsmith import Client
    from langsmith.utils import LangSmithNotFoundError

    project_name = (
        os.environ.get("LANGCHAIN_PROJECT")
        or os.environ.get("LANGSMITH_PROJECT")
        or "specgate-governance-live-smoke"
    )
    run_id = uuid.uuid4()
    client = Client()
    client.create_run(
        id=run_id,
        name="governance-live-smoke-trace",
        run_type="chain",
        project_name=project_name,
        inputs={"purpose": "governance live smoke"},
        tags=["live_smoke", "governance"],
        start_time=datetime.now(UTC),
    )
    client.update_run(
        run_id,
        outputs={"ok": True},
        end_time=datetime.now(UTC),
    )

    run: Any | None = None
    for attempt in range(8):
        try:
            run = client.read_run(run_id)
            break
        except LangSmithNotFoundError:
            if attempt == 7:
                pytest.fail(
                    f"LangSmith run {run_id} was not readable after retry; "
                    "trace ingestion may be delayed."
                )
            time.sleep(0.75 * (attempt + 1))
    assert run is not None
    assert str(run.id) == str(run_id)
    assert run.name == "governance-live-smoke-trace"
