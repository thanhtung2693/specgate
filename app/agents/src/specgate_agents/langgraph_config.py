from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any


def _default_config_path() -> Path:
    configured = os.environ.get("LANGGRAPH_CONFIG_FILE")
    return Path(configured) if configured else Path(__file__).parents[2] / "langgraph.json"


def _resolve_import_path(value: str, base_dir: Path) -> str:
    path, separator, target = value.partition(":")
    if not separator:
        return value
    if path.startswith("."):
        return f"{(base_dir / path).resolve()}:{target}"
    return value


def _resolve_graphs(raw_graphs: Any, base_dir: Path) -> Any:
    if not isinstance(raw_graphs, dict):
        return raw_graphs
    resolved: dict[str, Any] = {}
    for graph_id, graph_config in raw_graphs.items():
        if isinstance(graph_config, str):
            resolved[graph_id] = _resolve_import_path(graph_config, base_dir)
        elif isinstance(graph_config, dict) and isinstance(graph_config.get("path"), str):
            resolved[graph_id] = {
                **graph_config,
                "path": _resolve_import_path(graph_config["path"], base_dir),
            }
        else:
            resolved[graph_id] = graph_config
    return resolved


def _resolve_http(raw_http: Any, base_dir: Path) -> Any:
    if not isinstance(raw_http, dict):
        return raw_http
    resolved = dict(raw_http)
    if isinstance(resolved.get("app"), str):
        resolved["app"] = _resolve_import_path(resolved["app"], base_dir)
    return resolved


def configure_langgraph_env(config_path: str | Path | None = None) -> None:
    path = Path(config_path) if config_path is not None else _default_config_path()
    if not path.exists():
        return

    config = json.loads(path.read_text(encoding="utf-8"))
    base_dir = path.parent

    if "graphs" in config and "LANGSERVE_GRAPHS" not in os.environ:
        os.environ["LANGSERVE_GRAPHS"] = json.dumps(
            _resolve_graphs(config["graphs"], base_dir),
            separators=(",", ":"),
        )

    if "http" in config and "LANGGRAPH_HTTP" not in os.environ:
        os.environ["LANGGRAPH_HTTP"] = json.dumps(
            _resolve_http(config["http"], base_dir),
            separators=(",", ":"),
        )
