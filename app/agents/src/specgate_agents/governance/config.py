"""Environment-driven configuration (no secrets in code)."""

from __future__ import annotations

import os
import socket
from urllib.parse import urlsplit, urlunsplit


def _b(name: str, default: str = "") -> str:
    return os.environ.get(name, default).strip()


def _resolve_host_runtime_url(raw_url: str) -> str:
    """Remap Docker-only hostnames to loopback when running natively.

    ``langgraph.json`` loads ``agents/.env`` for both Docker and native
    ``langgraph dev``. That file uses ``host.docker.internal`` so containers can
    reach host services, but some native shells cannot resolve that hostname.
    When resolution fails, fall back to ``127.0.0.1`` so host-mode governance runs
    still reach the local Doc Registry.
    """
    if not raw_url:
        return raw_url
    parts = urlsplit(raw_url)
    if parts.hostname != "host.docker.internal":
        return raw_url
    try:
        socket.getaddrinfo(parts.hostname, parts.port or 80)
        return raw_url
    except socket.gaierror:
        pass

    netloc = "127.0.0.1"
    if parts.port is not None:
        netloc = f"{netloc}:{parts.port}"
    if parts.username:
        auth = parts.username
        if parts.password:
            auth = f"{auth}:{parts.password}"
        netloc = f"{auth}@{netloc}"
    return urlunsplit((parts.scheme, netloc, parts.path, parts.query, parts.fragment))


def doc_registry_base_url() -> str:
    raw = _b("DOC_REGISTRY_BASE_URL", "http://localhost:8080").rstrip("/")
    return _resolve_host_runtime_url(raw)


def model() -> str:
    from specgate_agents.governance.provider_keys import governance_model

    return governance_model()


def governance_version() -> str:
    return _b("GOVERNANCE_VERSION", "governance-v0.1")
