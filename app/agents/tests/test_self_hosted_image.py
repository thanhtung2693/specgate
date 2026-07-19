from __future__ import annotations

from pathlib import Path


def test_agents_image_reads_langgraph_json_at_startup() -> None:
    repo_root = Path(__file__).resolve().parents[3]
    dockerfile = repo_root / "docker" / "Dockerfile.agents"

    text = dockerfile.read_text(encoding="utf-8")

    # Default CMD runs `langgraph dev` (langgraph-cli[inmem]) — reads langgraph.json
    # from WORKDIR. Assert the CMD array form, not a comment substring.
    assert '"langgraph", "dev"' in text
    assert "WORKDIR /deps/agents" in text
    # langgraph.json must exist in the agents package for the CMD to work.
    assert (repo_root / "app" / "agents" / "langgraph.json").exists()


def test_local_appliance_uses_python_313_for_agents() -> None:
    repo_root = Path(__file__).resolve().parents[3]
    dockerfile = repo_root / "docker" / "Dockerfile.local"

    text = dockerfile.read_text(encoding="utf-8")

    # LangChain still imports Pydantic v1 compatibility code, which warns on
    # Python 3.14. Keep the appliance runtime on the supported 3.13 line.
    assert "FROM python:3.13-slim-trixie AS agents-build" in text
