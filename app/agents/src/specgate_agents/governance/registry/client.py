"""HTTP client for Doc Registry (doc-registry/docs/spec.md §6).

Doc Registry is an internal service with no HTTP auth (registry spec §7); we do
not send Authorization headers. Retry policy follows the registry spec: publish
and registry write may retry up to 3 times.
"""

from __future__ import annotations

import asyncio
import time
from typing import Any, Literal

import httpx

ConflictState = Literal["no_conflict", "warning_conflict", "blocking_conflict"]


def _unwrap_huma_body(data: dict[str, Any]) -> dict[str, Any]:
    """Huma v2 wraps handlers output as ``{"body": {...}}`` (matches UI ``unwrapBody``)."""
    body = data.get("body")
    if isinstance(body, dict):
        return body
    body_alt = data.get("Body")
    if isinstance(body_alt, dict):
        return body_alt
    return data


def _parse_skills_json_payload(payload: Any) -> list[dict[str, Any]]:
    """Normalize GET /skills JSON into a list of skill dicts (sync + async paths)."""
    if isinstance(payload, dict):
        body = payload.get("body") or payload.get("Body")
        if isinstance(body, dict):
            items = body.get("items") if "items" in body else body.get("Items")
            if isinstance(items, list):
                return items
        if "items" in payload:
            items = payload.get("items")
            return items if isinstance(items, list) else []
    return payload if isinstance(payload, list) else []


def _parse_items_payload(payload: Any) -> list[dict[str, Any]]:
    """Normalize Huma/list payloads that expose an ``items`` collection."""
    if isinstance(payload, dict):
        body = payload.get("body") or payload.get("Body")
        if isinstance(body, dict):
            items = body.get("items") if "items" in body else body.get("Items")
            return items if isinstance(items, list) else []
        items = payload.get("items") if "items" in payload else payload.get("Items")
        return items if isinstance(items, list) else []
    return payload if isinstance(payload, list) else []


class DocRegistryClient:
    def __init__(
        self,
        base_url: str,
        *,
        timeout_s: float = 30.0,
        transport: httpx.BaseTransport | None = None,
        max_retries: int = 3,
        backoff_base_s: float = 0.5,
    ) -> None:
        self._base = base_url.rstrip("/")
        self._headers: dict[str, str] = {"Accept": "application/json"}
        self._timeout = timeout_s
        self._transport = transport
        self._max_retries = max(1, max_retries)
        self._backoff_base = backoff_base_s

    @property
    def base_url(self) -> str:
        """Normalized Doc Registry base URL used for cache scoping."""
        return self._base

    def _client(self) -> httpx.Client:
        return httpx.Client(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        )

    def _should_retry(self, status_code: int) -> bool:
        return status_code >= 500 or status_code == 429

    def post_artifact(self, body: dict[str, Any]) -> dict[str, Any]:
        """POST /artifacts — publish artifact bundle. Retries on 5xx/429/network errors."""
        last_exc: Exception | None = None
        for attempt in range(self._max_retries):
            try:
                with self._client() as client:
                    r = client.post("/artifacts", json=body)
                    if r.is_success:
                        raw = r.json()
                        return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw
                    if self._should_retry(r.status_code) and attempt < self._max_retries - 1:
                        last_exc = httpx.HTTPStatusError(
                            f"retryable {r.status_code}", request=r.request, response=r
                        )
                    else:
                        r.raise_for_status()
            except (httpx.TransportError, httpx.HTTPStatusError) as e:
                last_exc = e
                if attempt >= self._max_retries - 1:
                    raise
            time.sleep(self._backoff_base * (2**attempt))
        assert last_exc is not None
        raise last_exc

    async def apost_artifact(self, body: dict[str, Any]) -> dict[str, Any]:
        """Async POST /artifacts — for LangGraph async nodes (non-blocking event loop)."""
        last_exc: Exception | None = None
        for attempt in range(self._max_retries):
            try:
                async with httpx.AsyncClient(
                    base_url=self._base,
                    headers=self._headers,
                    timeout=self._timeout,
                    transport=self._transport,
                ) as client:
                    r = await client.post("/artifacts", json=body)
                    if r.is_success:
                        raw = r.json()
                        return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw
                    if self._should_retry(r.status_code) and attempt < self._max_retries - 1:
                        last_exc = httpx.HTTPStatusError(
                            f"retryable {r.status_code}", request=r.request, response=r
                        )
                    else:
                        r.raise_for_status()
            except (httpx.TransportError, httpx.HTTPStatusError) as e:
                last_exc = e
                if attempt >= self._max_retries - 1:
                    raise
            await asyncio.sleep(self._backoff_base * (2**attempt))
        assert last_exc is not None
        raise last_exc

    def get_artifact(self, artifact_id: str) -> dict[str, Any]:
        """Sync GET /artifacts/{id} — artifact metadata (incl. ``version``).

        Use in synchronous callers (e.g. ``generate_context_pack``). Prefer
        ``aget_artifact`` in async contexts.
        """
        with self._client() as client:
            r = client.get(f"/artifacts/{artifact_id}")
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def aget_artifact(self, artifact_id: str) -> dict[str, Any]:
        """Async GET /artifacts/{id} — artifact metadata (incl. ``version``).

        Returns the ``ArtifactDTO`` body. The narrative grounding markdown lives
        in the per-file endpoints (see ``aload_artifact_markdown_bundle``); this
        call is the source for the canonical artifact's ``version`` string.
        """
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get(f"/artifacts/{artifact_id}")
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    def list_feature_attachments(self, feature_id: str) -> list[dict[str, Any]]:
        """GET /features/{id}/attachments — feature-scoped reference attachments.

        Returns the raw attachment DTO list (``kind``, ``url`` /
        ``governance_file_id``, ``title``, ``note``, ``audience``). Audience filtering
        for a specific consumer (gate vs coding agent) is the caller's job.
        """
        fid = str(feature_id or "").strip()
        if not fid:
            return []
        with self._client() as client:
            r = client.get(f"/features/{fid}/attachments")
            r.raise_for_status()
            raw = r.json()
        body = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
        items = body.get("items") if isinstance(body, dict) else None
        return [i for i in items if isinstance(i, dict)] if isinstance(items, list) else []

    async def alist_feature_attachments(self, feature_id: str) -> list[dict[str, Any]]:
        """Async GET /features/{id}/attachments — feature-scoped reference attachments."""
        fid = str(feature_id or "").strip()
        if not fid:
            return []
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get(f"/features/{fid}/attachments")
            r.raise_for_status()
            raw = r.json()
        body = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
        items = body.get("items") if isinstance(body, dict) else None
        return [i for i in items if isinstance(i, dict)] if isinstance(items, list) else []

    def patch_artifact_status(
        self, artifact_id: str, status: str, *, manifest_json: str | None = None
    ) -> dict[str, Any]:
        """PATCH /artifacts/{id}/status — promote draft to approved/needs_changes/superseded.

        Retries on 5xx/429/network errors (same policy as post_artifact).
        """
        body = {"status": status}
        if manifest_json is not None:
            body["manifest"] = manifest_json
        last_exc: Exception | None = None
        for attempt in range(self._max_retries):
            try:
                with self._client() as client:
                    r = client.patch(f"/artifacts/{artifact_id}/status", json=body)
                    if r.is_success:
                        raw = r.json()
                        return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw
                    if self._should_retry(r.status_code) and attempt < self._max_retries - 1:
                        last_exc = httpx.HTTPStatusError(
                            f"retryable {r.status_code}", request=r.request, response=r
                        )
                    else:
                        r.raise_for_status()
            except (httpx.TransportError, httpx.HTTPStatusError) as e:
                last_exc = e
                if attempt >= self._max_retries - 1:
                    raise
            time.sleep(self._backoff_base * (2**attempt))
        assert last_exc is not None
        raise last_exc

    def get_conflicts(self, services: list[str]) -> dict[str, Any]:
        """GET /conflicts?services=a&services=b"""
        params = [("services", s) for s in services]
        with self._client() as client:
            r = client.get("/conflicts", params=params)
            r.raise_for_status()
            return r.json()

    async def aget_conflicts(self, services: list[str]) -> dict[str, Any]:
        """Async GET /conflicts — for LangGraph async nodes."""
        params = [("services", s) for s in services]
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get("/conflicts", params=params)
            r.raise_for_status()
            return r.json()

    def create_workboard_extraction(self, body: dict[str, Any]) -> dict[str, Any]:
        """POST /workboard/extractions."""
        with self._client() as client:
            r = client.post("/workboard/extractions", json=body)
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def acreate_workboard_extraction(self, body: dict[str, Any]) -> dict[str, Any]:
        """Async POST /workboard/extractions — for ASGI routes."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post("/workboard/extractions", json=body)
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    def approve_workboard_extraction(
        self,
        extraction_id: str,
        body: dict[str, Any],
    ) -> dict[str, Any]:
        """POST /workboard/extractions/{id}/approve."""
        with self._client() as client:
            r = client.post(f"/workboard/extractions/{extraction_id}/approve", json=body)
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def aapprove_workboard_extraction(
        self,
        extraction_id: str,
        body: dict[str, Any],
    ) -> dict[str, Any]:
        """Async POST /workboard/extractions/{id}/approve — for ASGI routes."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post(
                f"/workboard/extractions/{extraction_id}/approve", json=body
            )
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    def get_workboard_feature(self, feature_id: str) -> dict[str, Any]:
        """GET /workboard/features/{id}."""
        with self._client() as client:
            r = client.get(f"/workboard/features/{feature_id}")
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def apatch_workboard_feature(
        self,
        feature_id: str,
        body: dict[str, Any],
    ) -> dict[str, Any]:
        """Async PATCH /workboard/features/{id}."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.patch(f"/workboard/features/{feature_id}", json=body)
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    def presign_governance_file(
        self,
        *,
        name: str,
        size_bytes: int,
        content_type: str = "text/markdown",
    ) -> dict[str, Any]:
        """POST /governance/files/presign — get a presigned PUT URL for a new governance file."""
        with self._client() as client:
            r = client.post(
                "/governance/files/presign",
                json={
                    "name": name,
                    "size_bytes": size_bytes,
                    "content_type": content_type,
                },
            )
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    def upload_to_presigned_url(
        self,
        upload_url: str,
        body: bytes,
        *,
        content_type: str = "text/markdown",
    ) -> None:
        """PUT raw bytes to the S3 presigned URL returned by presign_governance_file."""
        # The presigned URL goes directly to S3/MinIO — bypass the registry httpx
        # client (different host, different headers); use a plain transport so
        # tests can stub easily.
        with httpx.Client(timeout=self._timeout, transport=self._transport) as client:
            r = client.put(upload_url, content=body, headers={"Content-Type": content_type})
            r.raise_for_status()

    def commit_governance_file(self, file_id: str) -> dict[str, Any]:
        """POST /governance/files/{id}/commit — confirm S3 upload + index the file row."""
        with self._client() as client:
            r = client.post(f"/governance/files/{file_id}/commit")
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def apresign_governance_file(
        self,
        *,
        name: str,
        size_bytes: int,
        content_type: str = "text/markdown",
    ) -> dict[str, Any]:
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post(
                "/governance/files/presign",
                json={
                    "name": name,
                    "size_bytes": size_bytes,
                    "content_type": content_type,
                },
            )
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def aupload_to_presigned_url(
        self,
        upload_url: str,
        body: bytes,
        *,
        content_type: str = "text/markdown",
    ) -> None:
        async with httpx.AsyncClient(timeout=self._timeout, transport=self._transport) as client:
            r = await client.put(upload_url, content=body, headers={"Content-Type": content_type})
            r.raise_for_status()

    async def acommit_governance_file(self, file_id: str) -> dict[str, Any]:
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post(f"/governance/files/{file_id}/commit")
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def adelete_governance_file(self, file_id: str) -> None:
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.delete(f"/governance/files/{file_id}")
            r.raise_for_status()

    async def afetch_governance_file_text(self, file_id: str) -> str:
        """Download a committed governance-chat library file as UTF-8 text.

        Reads through the Doc Registry content proxy (``/content``), not a
        presigned object-store URL — container-reachable and credential-free.
        Touch first to keep the file warm against the TTL purger.
        """
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            await client.post(f"/governance/files/{file_id}/touch")
            r = await client.get(f"/governance/files/{file_id}/content")
            r.raise_for_status()
            return r.text

    async def aread_repo_file(self, project: str, path: str, ref: str = "HEAD") -> str:
        """GET /repos/file — read one repo file through Doc Registry's integration
        credential. ``project`` is the GitLab integration resource external_key.

        Doc Registry owns the integration token; the governance-ops never holds it. Returns
        the file content, or ``""`` when the file is absent (``found:false`` / 404).
        A ``ref`` of ``"HEAD"`` (or empty) is sent as empty so Doc Registry falls
        back to the resource's configured default ref — GitLab's files API does not
        reliably resolve the literal ``HEAD`` symbolic ref.
        """
        params = {"project": project, "path": path}
        if ref and ref != "HEAD":
            params["ref"] = ref
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get("/repos/file", params=params)
            if r.status_code == 404:
                return ""
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
        if not data.get("found"):
            return ""
        return str(data.get("content") or "")

    def list_workboard_features(self) -> dict[str, Any]:
        """GET /workboard/features. Returns the raw list payload (with `items`)."""
        with self._client() as client:
            r = client.get("/workboard/features")
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def alist_workboard_features(self) -> dict[str, Any]:
        """Async GET /workboard/features for LangGraph async nodes."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get("/workboard/features")
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def aupsert_feature_by_key(self, key: str, name: str = "") -> dict[str, Any]:
        """POST /workboard/features/upsert-by-key — idempotent create-or-get."""
        body: dict[str, Any] = {"key": key}
        if name:
            body["name"] = name
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post("/workboard/features/upsert-by-key", json=body)
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def acreate_change_request(self, body: dict[str, Any]) -> dict[str, Any]:
        """POST /workboard/change-requests."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post("/workboard/change-requests", json=body)
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    def get_change_request(self, change_request_id: str) -> dict[str, Any]:
        """GET /workboard/change-requests/{id}."""
        with self._client() as client:
            r = client.get(f"/workboard/change-requests/{change_request_id}")
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    def list_change_requests(self, *, include_archived: bool = False) -> list[dict[str, Any]]:
        """GET /workboard/change-requests (archived hidden unless requested)."""
        params: dict[str, str] = {}
        if include_archived:
            params["include_archived"] = "true"
        with self._client() as client:
            r = client.get("/workboard/change-requests", params=params)
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def list_workboard_stale_warnings(
        self,
        *,
        feature_id: str | None = None,
        change_request_id: str | None = None,
    ) -> list[dict[str, Any]]:
        """GET /workboard/stale-warnings."""
        params: dict[str, str] = {}
        if feature_id:
            params["feature_id"] = feature_id
        if change_request_id:
            params["change_request_id"] = change_request_id
        with self._client() as client:
            r = client.get("/workboard/stale-warnings", params=params)
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def refresh_change_request_gate_runs(
        self,
        change_request_id: str,
        evaluations: list[dict[str, Any]] | None = None,
    ) -> list[dict[str, Any]]:
        """POST /workboard/change-requests/{id}/gate-runs/refresh.

        ``evaluations`` are agent gate verdicts (gate, state, hint, confidence,
        judge_model, eval_suite_version); omit for a deterministic-only refresh.
        """
        body: dict[str, Any] = {}
        if evaluations:
            body["evaluations"] = evaluations
        with self._client() as client:
            r = client.post(
                f"/workboard/change-requests/{change_request_id}/gate-runs/refresh",
                json=body,
            )
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def list_change_request_gate_runs(
        self,
        change_request_id: str,
        limit: int = 50,
    ) -> list[dict[str, Any]]:
        """GET /workboard/change-requests/{id}/gate-runs?limit=."""
        with self._client() as client:
            r = client.get(
                f"/workboard/change-requests/{change_request_id}/gate-runs",
                params={"limit": str(limit)},
            )
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def refresh_artifact_readiness_runs(
        self,
        artifact_id: str,
        evaluations: list[dict[str, Any]] | None = None,
    ) -> list[dict[str, Any]]:
        """POST /artifacts/{id}/readiness-runs/refresh."""
        body: dict[str, Any] = {}
        if evaluations:
            body["evaluations"] = evaluations
        with self._client() as client:
            r = client.post(f"/artifacts/{artifact_id}/readiness-runs/refresh", json=body)
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def list_artifact_readiness_runs(
        self,
        artifact_id: str,
        limit: int = 50,
    ) -> list[dict[str, Any]]:
        """GET /artifacts/{id}/readiness-runs?limit=."""
        with self._client() as client:
            r = client.get(
                f"/artifacts/{artifact_id}/readiness-runs",
                params={"limit": str(limit)},
            )
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def patch_change_request_context_pack_artifact(
        self,
        change_request_id: str,
        artifact_id: str,
    ) -> dict[str, Any]:
        """POST /workboard/change-requests/{id}/context-pack-artifact."""
        with self._client() as client:
            r = client.post(
                f"/workboard/change-requests/{change_request_id}/context-pack-artifact",
                json={"artifact_id": artifact_id},
            )
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def apatch_change_request_lead_artifact(
        self,
        change_request_id: str,
        artifact_id: str,
    ) -> dict[str, Any]:
        """POST /workboard/change-requests/{id}/lead-artifact."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post(
                f"/workboard/change-requests/{change_request_id}/lead-artifact",
                json={"artifact_id": artifact_id},
            )
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def aget_settings_unmasked_for_governance(self) -> dict[str, str]:
        """Async GET /settings with internal governance trust header for provider keys."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers={**self._headers, "X-SpecGate-Internal-Agent": "governance"},
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get("/settings")
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
            settings = data.get("settings") if isinstance(data, dict) else None
            return settings if isinstance(settings, dict) else {}

    def get_settings_unmasked_for_governance(self) -> dict[str, str]:
        """Sync GET /settings with internal governance trust header for provider keys."""
        with httpx.Client(
            base_url=self._base,
            headers={**self._headers, "X-SpecGate-Internal-Agent": "governance"},
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = client.get("/settings")
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
            settings = data.get("settings") if isinstance(data, dict) else None
            return settings if isinstance(settings, dict) else {}

    def fetch_artifact_file_text(self, artifact_id: str, key: str) -> str:
        """Resolve signed URL for file GET and download body as UTF-8 text."""
        with self._client() as client:
            r = client.get(f"/artifacts/{artifact_id}/files/{key}")
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
        inline = data.get("content")
        if isinstance(inline, str) and inline:
            return inline
        surl = data.get("signed_url")
        if not surl:
            return ""
        r2 = httpx.get(str(surl), timeout=self._timeout)
        r2.raise_for_status()
        return r2.text

    async def afetch_artifact_file_text(self, artifact_id: str, key: str) -> str:
        """Async: resolve signed URL and download UTF-8 text (non-blocking)."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get(f"/artifacts/{artifact_id}/files/{key}")
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
        surl = data.get("signed_url")
        if not surl:
            return ""
        async with httpx.AsyncClient(timeout=self._timeout) as dl:
            r2 = await dl.get(str(surl))
            r2.raise_for_status()
            return r2.text

    def load_artifact_markdown_bundle(self, artifact_id: str) -> dict[str, str]:
        """Load prd, spec, tasks_fe, tasks_be text for governance re-edit."""
        out: dict[str, str] = {}
        for key in ("prd", "spec", "tasks_fe", "tasks_be"):
            try:
                out[key] = self.fetch_artifact_file_text(artifact_id, key)
            except (httpx.HTTPError, OSError, ValueError):
                out[key] = ""
        return out

    async def aload_artifact_markdown_bundle(self, artifact_id: str) -> dict[str, str]:
        """Async load prd/spec/tasks_fe/tasks_be/tasks_qa/rollout/risks (parallel downloads).

        The quality-gate judge routes these sections per gate (impl plans for
        traceability, rollout/risks for rollback, QA for edge cases). Missing
        files resolve to an empty string.
        """

        async def one(key: str) -> tuple[str, str]:
            try:
                text = await self.afetch_artifact_file_text(artifact_id, key)
                return key, text
            except (httpx.HTTPError, OSError, ValueError):
                return key, ""

        keys = ("prd", "spec", "tasks_fe", "tasks_be", "tasks_qa", "rollout", "risks")
        pairs = await asyncio.gather(*(one(k) for k in keys))
        return dict(pairs)

    def load_artifact_handoff_bundle(self, artifact_id: str) -> dict[str, str]:
        """Load implementation handoff files from an approved artifact package."""
        keys = (
            "prd",
            "spec",
            "implementation_plan",
            "tasks_fe",
            "tasks_be",
            "tasks_qa",
            "rollout",
            "risks",
            "assumptions",
            "manifest",
        )
        out: dict[str, str] = {}
        for key in keys:
            try:
                out[key] = self.fetch_artifact_file_text(artifact_id, key)
            except (httpx.HTTPError, OSError, ValueError):
                out[key] = ""
        return out

    def get_skills(self) -> list[dict[str, Any]]:
        """GET /skills — list all skills registered in Doc Registry.

        Handles paginated ``{"items": [...]}``, Huma-style ``{"body": {"items": [...]}}``,
        and bare list responses.
        """
        with self._client() as client:
            r = client.get("/skills")
            r.raise_for_status()
            payload = r.json()
        return _parse_skills_json_payload(payload)

    async def aget_skills(self) -> list[dict[str, Any]]:
        """Async GET /skills — for LangGraph async nodes."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get("/skills")
            r.raise_for_status()
            payload = r.json()
        return _parse_skills_json_payload(payload)

    def list_events(
        self,
        *,
        after: str | None = None,
        event_type: str | None = None,
        limit: int = 100,
    ) -> list[dict[str, Any]]:
        """GET /events — poll Doc Registry artifact lifecycle events."""
        params: dict[str, Any] = {"limit": limit}
        if after:
            params["after"] = after
        if event_type:
            params["event_type"] = event_type
        with self._client() as client:
            r = client.get("/events", params=params)
            r.raise_for_status()
            payload = r.json()
        return _parse_items_payload(payload)

    async def alist_events(
        self,
        *,
        after: str | None = None,
        event_type: str | None = None,
        limit: int = 100,
    ) -> list[dict[str, Any]]:
        """Async GET /events — for orchestrator event polling."""
        params: dict[str, Any] = {"limit": limit}
        if after:
            params["after"] = after
        if event_type:
            params["event_type"] = event_type
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get("/events", params=params)
            r.raise_for_status()
            payload = r.json()
        return _parse_items_payload(payload)

    async def alist_governance_feedback_events(
        self,
        *,
        status: str | None = None,
        limit: int = 200,
    ) -> list[dict[str, Any]]:
        """Async GET /governance/feedback-events — integration-derived feedback queue.

        Doc Registry has no by-id lookup for feedback events; callers that need a
        single event fetch the queue (optionally status-scoped) and filter by id.
        """
        params: dict[str, Any] = {"limit": limit}
        if status:
            params["status"] = status
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get("/governance/feedback-events", params=params)
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def governance_context_search(self, body: dict[str, Any]) -> dict[str, Any]:
        """POST /governance/context/search — Governance Knowledge chunks (doc-registry spec §15.6).

        Body must include ``query`` (str). Optional keys: ``linked_feature_id``,
        ``linked_request_id``, ``document_types``, ``authority_levels``, ``max_chunks``,
        ``include_history``.
        """
        with self._client() as client:
            r = client.post("/governance/context/search", json=body)
            r.raise_for_status()
            return r.json()

    async def agovernance_context_search(self, body: dict[str, Any]) -> dict[str, Any]:
        """Async POST /governance/context/search."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post("/governance/context/search", json=body)
            r.raise_for_status()
            return r.json()

    async def acreate_artifact_edit_session(self, body: dict[str, Any]) -> dict[str, Any]:
        """Async POST /artifact-edit/sessions — open a server-owned editing session."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post("/artifact-edit/sessions", json=body)
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
            return data if isinstance(data, dict) else {}

    async def alist_artifact_edit_proposals(self) -> list[dict[str, Any]]:
        """Async GET /artifact-edit/proposals — active source-tagged proposals.

        The review queue: edit sessions tagged with an origin (``source_kind``)
        still awaiting a human verdict. Used to avoid opening a duplicate
        proposal for an artifact+source that already has one active.
        """
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get("/artifact-edit/proposals")
            r.raise_for_status()
            return _parse_items_payload(r.json())

    async def apatch_artifact_edit_session(
        self,
        session_id: str,
        body: dict[str, Any],
    ) -> dict[str, Any]:
        """Async POST /artifact-edit/sessions/{id}/patch."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post(f"/artifact-edit/sessions/{session_id}/patch", json=body)
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
            return data if isinstance(data, dict) else {}

    async def areplace_artifact_edit_file(
        self,
        session_id: str,
        key: str,
        body: dict[str, Any],
    ) -> dict[str, Any]:
        """Async PUT /artifact-edit/sessions/{id}/files/{key}."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.put(f"/artifact-edit/sessions/{session_id}/files/{key}", json=body)
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
            return data if isinstance(data, dict) else {}

    async def aget_artifact_edit_session_diff(self, session_id: str) -> dict[str, Any]:
        """Async GET /artifact-edit/sessions/{id}/diff."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get(f"/artifact-edit/sessions/{session_id}/diff")
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
            return data if isinstance(data, dict) else {}

    async def asave_artifact_edit_session(
        self,
        session_id: str,
        body: dict[str, Any],
    ) -> dict[str, Any]:
        """Async POST /artifact-edit/sessions/{id}/save."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post(f"/artifact-edit/sessions/{session_id}/save", json=body)
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
            return data if isinstance(data, dict) else {}

    async def alist_artifact_revisions(self, artifact_id: str) -> list[dict[str, Any]]:
        """Async GET /artifacts/{id}/revisions."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get(f"/artifacts/{artifact_id}/revisions")
            r.raise_for_status()
            return _parse_items_payload(r.json())

    async def _afetch_artifact_file_by_path(self, artifact_id: str, path: str) -> str:
        """Fetch artifact file text using the ?path= query form (handles slashed paths).

        Uses /files/_ with ?path= so the router matches the single-file GET handler
        (which returns signed_url + content). The bare /files endpoint is the list
        handler and ignores the path query param entirely.
        """
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get(
                f"/artifacts/{artifact_id}/files/_",
                params={"path": path},
            )
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
        inline = data.get("content")
        if isinstance(inline, str) and inline:
            return inline
        surl = data.get("signed_url")
        if not surl:
            return ""
        async with httpx.AsyncClient(timeout=self._timeout) as dl:
            r2 = await dl.get(str(surl))
            r2.raise_for_status()
            return r2.text

    async def aget_artifact_files(self, artifact_id: str) -> list[dict[str, Any]]:
        """List an artifact's documents [{path, role, size_bytes}] via GET /artifacts/{id}/files."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get(f"/artifacts/{artifact_id}/files")
            r.raise_for_status()
            raw = r.json()
        data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
        items = data.get("items") if isinstance(data, dict) else None
        if isinstance(items, list):
            return list(items)
        # Bare list (no body envelope)
        if isinstance(raw, dict):
            items = raw.get("items")
            if isinstance(items, list):
                return list(items)
        return []

    async def aload_artifact_files_with_content(self, artifact_id: str) -> list[dict[str, Any]]:
        """List an artifact's documents with content: [{path, role, content}].

        Keeps per-file (path) granularity — unlike ``aload_artifact_bundle_by_role``
        which collapses to {role: markdown}. The reconciliation drafter needs the
        path because the artifact-edit session is keyed by document path, so a
        proposed edit must round-trip to a specific file. Stable path order;
        missing/failed fetches yield empty content (no crash).
        """
        files = await self.aget_artifact_files(artifact_id)
        out: list[dict[str, Any]] = []
        for f in sorted(files, key=lambda x: x.get("path", "")):
            path = str(f.get("path") or "").strip()
            if not path:
                continue
            try:
                text = await self._afetch_artifact_file_by_path(artifact_id, path)
            except (httpx.HTTPError, OSError, ValueError):
                text = ""
            out.append(
                {"path": path, "role": (f.get("role") or "unspecified").strip(), "content": text}
            )
        return out

    async def alist_gate_tasks(self, artifact_id: str) -> list[dict[str, Any]]:
        """Async GET /api/v1/gate-tasks?artifact_id= — list pending gate tasks for an artifact."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get(
                "/api/v1/gate-tasks",
                params={"artifact_id": artifact_id},
            )
            r.raise_for_status()
            return r.json().get("tasks", [])

    async def aget_gate_task(self, task_id: str) -> dict[str, Any]:
        """Async GET /api/v1/gate-tasks/{task_id} — get a single gate task."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.get(f"/api/v1/gate-tasks/{task_id}")
            r.raise_for_status()
            return r.json()

    async def asubmit_gate_result(
        self,
        task_id: str,
        *,
        gate_digest: str,
        state: str,
        summary: str,
        findings: list[dict[str, Any]],
    ) -> dict[str, Any]:
        """Async POST /api/v1/gate-tasks/{task_id}/result — submit IDE-agent gate evaluation."""
        import uuid

        payload = {
            "gate_digest": gate_digest,
            "state": state,
            "summary": summary,
            "evaluator": {"executor": "ide_agent", "run_id": str(uuid.uuid4())},
            "findings": findings,
        }
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post(f"/api/v1/gate-tasks/{task_id}/result", json=payload)
            r.raise_for_status()
            return r.json()

    async def adispatch_gate_tasks(self, artifact_id: str) -> dict[str, Any]:
        """Async POST /api/v1/artifacts/{artifact_id}/gate-tasks — create ide_agent gate
        tasks for the artifact's enabled gates so a coding agent can evaluate them."""
        from urllib.parse import quote

        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as client:
            r = await client.post(f"/api/v1/artifacts/{quote(artifact_id, safe='')}/gate-tasks")
            r.raise_for_status()
            return r.json()

    async def asubmit_evidence(self, work_item_id: str, manifest: dict[str, Any]) -> dict[str, Any]:
        """Async POST /api/v1/work-items/{id}/evidence — submit a specgate.evidence/v1 manifest."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as c:
            r = await c.post(f"/api/v1/work-items/{work_item_id}/evidence", json=manifest)
            r.raise_for_status()
            return r.json()

    async def alist_evidence(self, work_item_id: str) -> list[dict[str, Any]]:
        """Async GET /api/v1/work-items/{id}/evidence — list submitted evidence manifests."""
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as c:
            r = await c.get(f"/api/v1/work-items/{work_item_id}/evidence")
            r.raise_for_status()
            return r.json().get("manifests", [])

    async def aget_evidence_gates(
        self,
        work_item_id: str,
        *,
        required_acs: str = "",
        min_test_level: str = "",
        independent_review: bool = False,
    ) -> list[dict[str, Any]]:
        """Async GET /api/v1/work-items/{id}/evidence/gates — evaluate delivery evidence gates."""
        params: dict[str, str] = {}
        if required_acs:
            params["required_acs"] = required_acs
        if min_test_level:
            params["min_test_level"] = min_test_level
        if independent_review:
            params["independent_review"] = "true"
        async with httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        ) as c:
            r = await c.get(f"/api/v1/work-items/{work_item_id}/evidence/gates", params=params)
            r.raise_for_status()
            return r.json().get("gates", [])

    async def aload_artifact_bundle_by_role(self, artifact_id: str) -> dict[str, str]:
        """Group an artifact's documents by role and fetch each by path.

        Returns {role: markdown}; multiple docs sharing a role are concatenated
        in stable path order. Missing or failed fetches are skipped (no crash).
        Uses the ?path= query form to support slashed paths (per spec §6).
        """
        files = await self.aget_artifact_files(artifact_id)
        by_role: dict[str, list[str]] = {}
        for f in sorted(files, key=lambda x: x.get("path", "")):
            path = f.get("path", "")
            role = (f.get("role") or "unspecified").strip()
            if not path:
                continue
            try:
                text = await self._afetch_artifact_file_by_path(artifact_id, path)
            except (httpx.HTTPError, OSError, ValueError):
                text = ""
            if text:
                by_role.setdefault(role, []).append(text)
        return {role: "\n\n".join(parts) for role, parts in by_role.items()}
