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
