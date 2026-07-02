"""Tests for DocRegistryClient.aget_artifact_files and aload_artifact_bundle_by_role.

These methods read a published artifact's documents by role via GET /artifacts/{id}/files.
"""

from __future__ import annotations

import httpx
import pytest

from specgate_agents.governance.registry.client import DocRegistryClient

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_client(handler) -> DocRegistryClient:
    """Build a DocRegistryClient backed by an httpx.MockTransport."""
    transport = httpx.MockTransport(handler)
    return DocRegistryClient("http://registry.test", transport=transport)


# ---------------------------------------------------------------------------
# aget_artifact_files
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_aget_artifact_files_returns_items() -> None:
    """GET /artifacts/{id}/files returns parsed [{path, role, size_bytes}] list."""
    items = [
        {"path": "spec.md", "role": "spec", "size_bytes": 1024},
        {"path": "plan.md", "role": "plan", "size_bytes": 512},
    ]

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/artifacts/art-1/files"
        assert request.method == "GET"
        return httpx.Response(200, json={"items": items})

    client = _make_client(handler)
    result = await client.aget_artifact_files("art-1")
    assert result == items


@pytest.mark.asyncio
async def test_aget_artifact_files_empty_when_no_items() -> None:
    """Missing items key returns an empty list gracefully."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={})

    client = _make_client(handler)
    result = await client.aget_artifact_files("art-1")
    assert result == []


@pytest.mark.asyncio
async def test_aget_artifact_files_huma_body_envelope() -> None:
    """Items wrapped under Huma body envelope are unwrapped correctly."""
    items = [{"path": "spec.md", "role": "spec", "size_bytes": 100}]

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"body": {"items": items}})

    client = _make_client(handler)
    result = await client.aget_artifact_files("art-1")
    assert result == items


# ---------------------------------------------------------------------------
# aload_artifact_bundle_by_role
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_aload_artifact_bundle_by_role_groups_and_concatenates() -> None:
    """Two spec docs + one plan doc: spec role concatenated in path order (a then b)."""
    files_items = [
        {"path": "a-spec.md", "role": "spec", "size_bytes": 100},
        {"path": "b-spec.md", "role": "spec", "size_bytes": 200},
        {"path": "plan.md", "role": "plan", "size_bytes": 50},
    ]
    # Map path → text content for faking file fetches
    file_texts = {
        "a-spec.md": "# Spec A",
        "b-spec.md": "# Spec B",
        "plan.md": "# Plan",
    }

    def handler(request: httpx.Request) -> httpx.Response:
        path = request.url.path
        params = dict(request.url.params)

        # List files endpoint
        if path == "/artifacts/art-1/files" and "path" not in params:
            return httpx.Response(200, json={"items": files_items})

        # Per-file fetch via ?path= query
        if "path" in params:
            file_path = params["path"]
            text = file_texts.get(file_path, "")
            # Return inline content (no signed URL needed for unit test)
            return httpx.Response(
                200,
                json={"body": {"content": text, "signed_url": ""}},
            )

        return httpx.Response(404, json={"error": "not found"})

    client = _make_client(handler)
    bundle = await client.aload_artifact_bundle_by_role("art-1")

    assert set(bundle.keys()) == {"spec", "plan"}
    # spec: a-spec.md then b-spec.md (stable path-sorted order)
    assert bundle["spec"] == "# Spec A\n\n# Spec B"
    assert bundle["plan"] == "# Plan"


@pytest.mark.asyncio
async def test_aload_artifact_bundle_by_role_skips_failed_fetch() -> None:
    """A file fetch that raises HTTPError is skipped; other roles still returned."""
    files_items = [
        {"path": "spec.md", "role": "spec", "size_bytes": 100},
        {"path": "bad.md", "role": "plan", "size_bytes": 0},
    ]
    def handler(request: httpx.Request) -> httpx.Response:
        path = request.url.path
        params = dict(request.url.params)

        if path == "/artifacts/art-1/files" and "path" not in params:
            return httpx.Response(200, json={"items": files_items})

        if "path" in params:
            file_path = params["path"]
            if file_path == "spec.md":
                return httpx.Response(
                    200,
                    json={"body": {"content": "# Spec", "signed_url": ""}},
                )
            # Simulate server error for bad.md
            return httpx.Response(500, json={"error": "internal error"})

        return httpx.Response(404, json={"error": "not found"})

    client = _make_client(handler)
    bundle = await client.aload_artifact_bundle_by_role("art-1")

    # Only spec should be in the bundle; plan fetch failed (500) → skipped
    assert "spec" in bundle
    assert bundle["spec"] == "# Spec"
    assert "plan" not in bundle


@pytest.mark.asyncio
async def test_aload_artifact_bundle_by_role_stable_sort_order() -> None:
    """Docs for the same role are concatenated in ascending path order."""
    # Provide items in reverse alpha to ensure sort is applied
    files_items = [
        {"path": "z-last.md", "role": "spec", "size_bytes": 10},
        {"path": "a-first.md", "role": "spec", "size_bytes": 10},
    ]
    file_texts = {
        "z-last.md": "LAST",
        "a-first.md": "FIRST",
    }

    def handler(request: httpx.Request) -> httpx.Response:
        path = request.url.path
        params = dict(request.url.params)

        if path == "/artifacts/art-1/files" and "path" not in params:
            return httpx.Response(200, json={"items": files_items})

        if "path" in params:
            file_path = params["path"]
            text = file_texts.get(file_path, "")
            return httpx.Response(200, json={"body": {"content": text, "signed_url": ""}})

        return httpx.Response(404)

    client = _make_client(handler)
    bundle = await client.aload_artifact_bundle_by_role("art-1")

    assert bundle["spec"] == "FIRST\n\nLAST"


@pytest.mark.asyncio
async def test_aload_artifact_bundle_by_role_missing_role_becomes_unspecified() -> None:
    """A file with no role falls back to 'unspecified' key."""
    files_items = [
        {"path": "readme.md", "role": "", "size_bytes": 10},
    ]

    def handler(request: httpx.Request) -> httpx.Response:
        path = request.url.path
        params = dict(request.url.params)

        if path == "/artifacts/art-1/files" and "path" not in params:
            return httpx.Response(200, json={"items": files_items})

        if "path" in params:
            return httpx.Response(200, json={"body": {"content": "# Readme", "signed_url": ""}})

        return httpx.Response(404)

    client = _make_client(handler)
    bundle = await client.aload_artifact_bundle_by_role("art-1")

    assert "unspecified" in bundle
    assert bundle["unspecified"] == "# Readme"
