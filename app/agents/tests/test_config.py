"""Governance env config helpers."""

import pytest

from specgate_agents.governance import config


def test_doc_registry_base_url_default(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("DOC_REGISTRY_BASE_URL", raising=False)
    assert config.doc_registry_base_url() == "http://localhost:8080"


def test_doc_registry_base_url_strips_slash(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("DOC_REGISTRY_BASE_URL", "http://registry:8080/")
    assert config.doc_registry_base_url() == "http://registry:8080"


def test_doc_registry_base_url_falls_back_from_host_docker_internal(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("DOC_REGISTRY_BASE_URL", "http://host.docker.internal:8080/")

    def _raise(*_args: object, **_kwargs: object) -> object:
        raise socket.gaierror("not resolvable")

    import socket

    monkeypatch.setattr(socket, "getaddrinfo", _raise)

    assert config.doc_registry_base_url() == "http://127.0.0.1:8080"


def test_doc_registry_base_url_keeps_resolvable_host_docker_internal(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("DOC_REGISTRY_BASE_URL", "http://host.docker.internal:8080/")

    import socket

    monkeypatch.setattr(
        socket,
        "getaddrinfo",
        lambda *_args, **_kwargs: [
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("192.168.65.2", 8080))
        ],
    )

    assert config.doc_registry_base_url() == "http://host.docker.internal:8080"


def test_model_resolves_from_doc_registry_settings(monkeypatch: pytest.MonkeyPatch) -> None:
    from specgate_agents.governance.provider_keys import (
        clear_provider_api_keys,
        set_provider_api_keys_from_settings,
    )

    clear_provider_api_keys()
    set_provider_api_keys_from_settings(
        {
            "governance.model_provider": "anthropic",
            "governance.model": "claude-sonnet-4-6",
            "anthropic.api_key": "anthropic-test",
        }
    )

    assert config.model() == "claude-sonnet-4-6"

    clear_provider_api_keys()


