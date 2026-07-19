from __future__ import annotations

import json
import os
from pathlib import Path

from specgate_agents.langgraph_config import configure_langgraph_env


def test_configure_langgraph_env_reads_langgraph_json(tmp_path, monkeypatch) -> None:
    config = {
        "graphs": {"governance": "./src/specgate_agents/governance/graph.py:graph"},
        "http": {"app": "./src/specgate_agents/governance/webapp.py:app"},
    }
    config_path = tmp_path / "langgraph.json"
    config_path.write_text(json.dumps(config), encoding="utf-8")

    monkeypatch.delenv("LANGSERVE_GRAPHS", raising=False)
    monkeypatch.delenv("LANGGRAPH_HTTP", raising=False)

    configure_langgraph_env(config_path)

    graphs = json.loads(os.environ["LANGSERVE_GRAPHS"])
    http = json.loads(os.environ["LANGGRAPH_HTTP"])

    assert graphs == {
        "governance": f"{tmp_path}/src/specgate_agents/governance/graph.py:graph",
    }
    assert http == {"app": f"{tmp_path}/src/specgate_agents/governance/webapp.py:app"}


def test_configure_langgraph_env_preserves_explicit_env(tmp_path, monkeypatch) -> None:
    config_path = tmp_path / "langgraph.json"
    config_path.write_text(
        json.dumps({"graphs": {"governance": "./graph.py:graph"}}),
        encoding="utf-8",
    )

    monkeypatch.setenv("LANGSERVE_GRAPHS", '{"existing": "graph.py:graph"}')

    configure_langgraph_env(config_path)

    assert os.environ["LANGSERVE_GRAPHS"] == '{"existing": "graph.py:graph"}'


def test_repository_langgraph_config_uses_governance_entrypoints() -> None:
    config_path = Path(__file__).resolve().parents[1] / "langgraph.json"
    config = json.loads(config_path.read_text(encoding="utf-8"))

    assert config["graphs"] == {
        "governance": "./src/specgate_agents/governance/governance_chat.py:graph",
    }
    assert config["http"] == {
        "app": "./src/specgate_agents/governance/webapp.py:app",
    }

    import specgate_agents.governance.governance_chat  # noqa: F401
    import specgate_agents.governance.webapp  # noqa: F401
