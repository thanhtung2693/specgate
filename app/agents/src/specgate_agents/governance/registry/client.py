"""HTTP client for Doc Registry (doc-registry/docs/spec.md §6).

Doc Registry is an internal service with no HTTP auth (registry spec §7); we do
not send Authorization headers.
"""

from __future__ import annotations

from typing import Any

import httpx

_MAX_ARTIFACT_FILE_BYTES = 1 << 20
_MAX_ARTIFACT_PACKAGE_BYTES = 10 << 20


def _unwrap_huma_body(data: dict[str, Any]) -> dict[str, Any]:
    """Huma v2 wraps handlers output as ``{"body": {...}}`` (matches UI ``unwrapBody``)."""
    body = data.get("body")
    if isinstance(body, dict):
        return body
    body_alt = data.get("Body")
    if isinstance(body_alt, dict):
        return body_alt
    return data


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
    ) -> None:
        self._base = base_url.rstrip("/")
        self._headers: dict[str, str] = {"Accept": "application/json"}
        self._timeout = timeout_s
        self._transport = transport

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

    def _aclient(self) -> httpx.AsyncClient:
        return httpx.AsyncClient(
            base_url=self._base,
            headers=self._headers,
            timeout=self._timeout,
            transport=self._transport,
        )

    def _workspace_params(self, workspace_id: str | None) -> dict[str, str]:
        workspace = str(workspace_id or "").strip()
        return {"workspace_id": workspace} if workspace else {}

    def get_artifact(self, artifact_id: str, *, workspace_id: str | None = None) -> dict[str, Any]:
        """Sync GET /artifacts/{id} — artifact metadata (incl. ``version``).

        Use in synchronous callers. Prefer ``aget_artifact`` in async contexts.
        """
        with self._client() as client:
            r = client.get(f"/artifacts/{artifact_id}", params=self._workspace_params(workspace_id))
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def aget_artifact(
        self, artifact_id: str, *, workspace_id: str | None = None
    ) -> dict[str, Any]:
        """Async GET /artifacts/{id} — artifact metadata (incl. ``version``).

        Returns the ``ArtifactDTO`` body. Narrative grounding lives in the
        role-tagged file endpoints (see ``aload_artifact_bundle_by_role``); this
        call is the source for the canonical artifact's ``version`` string.
        """
        async with self._aclient() as client:
            r = await client.get(
                f"/artifacts/{artifact_id}", params=self._workspace_params(workspace_id)
            )
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    def list_feature_attachments(
        self, feature_id: str, *, workspace_id: str | None = None
    ) -> list[dict[str, Any]]:
        """GET /features/{id}/attachments — feature-scoped reference attachments.

        Returns the raw attachment DTO list (``kind``, ``url`` /
        ``governance_file_id``, ``title``, ``note``, ``audience``). Audience filtering
        for a specific consumer (gate vs coding agent) is the caller's job.
        """
        fid = str(feature_id or "").strip()
        if not fid:
            return []
        with self._client() as client:
            r = client.get(
                f"/features/{fid}/attachments", params=self._workspace_params(workspace_id)
            )
            r.raise_for_status()
            raw = r.json()
        body = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
        items = body.get("items") if isinstance(body, dict) else None
        return [i for i in items if isinstance(i, dict)] if isinstance(items, list) else []

    async def alist_feature_attachments(
        self, feature_id: str, *, workspace_id: str | None = None
    ) -> list[dict[str, Any]]:
        """Async GET /features/{id}/attachments — feature-scoped reference attachments."""
        fid = str(feature_id or "").strip()
        if not fid:
            return []
        async with self._aclient() as client:
            r = await client.get(
                f"/features/{fid}/attachments", params=self._workspace_params(workspace_id)
            )
            r.raise_for_status()
            raw = r.json()
        body = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
        items = body.get("items") if isinstance(body, dict) else None
        return [i for i in items if isinstance(i, dict)] if isinstance(items, list) else []

    def get_workboard_feature(
        self, feature_id: str, *, workspace_id: str | None = None
    ) -> dict[str, Any]:
        """GET /workboard/features/{id}."""
        with self._client() as client:
            r = client.get(
                f"/workboard/features/{feature_id}",
                params=self._workspace_params(workspace_id),
            )
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def aupsert_feature_by_key(
        self, key: str, name: str = "", *, workspace_id: str | None = None
    ) -> dict[str, Any]:
        """POST /workboard/features/upsert-by-key — idempotent create-or-get."""
        body: dict[str, Any] = {"key": key, "workspace_id": str(workspace_id or "").strip()}
        if name:
            body["name"] = name
        async with self._aclient() as client:
            r = await client.post("/workboard/features/upsert-by-key", json=body)
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def acreate_change_request(
        self, body: dict[str, Any], *, workspace_id: str | None = None
    ) -> dict[str, Any]:
        """POST /workboard/change-requests."""
        body = {**body}
        if workspace_id is not None:
            body["workspace_id"] = str(workspace_id).strip()
        async with self._aclient() as client:
            r = await client.post("/workboard/change-requests", json=body)
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    def get_change_request(
        self, change_request_id: str, *, workspace_id: str | None = None
    ) -> dict[str, Any]:
        """GET /workboard/change-requests/{id}."""
        with self._client() as client:
            r = client.get(
                f"/workboard/change-requests/{change_request_id}",
                params=self._workspace_params(workspace_id),
            )
            r.raise_for_status()
            raw = r.json()
            return _unwrap_huma_body(raw) if isinstance(raw, dict) else raw

    async def alist_acceptance_criteria(
        self, change_request_id: str, *, workspace_id: str | None = None
    ) -> list[dict[str, Any]]:
        """GET canonical acceptance-criterion rows, including stable ids."""
        async with self._aclient() as client:
            r = await client.get(
                f"/workboard/change-requests/{change_request_id}/acceptance-criteria",
                params=self._workspace_params(workspace_id),
            )
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def list_workboard_stale_warnings(
        self,
        *,
        feature_id: str | None = None,
        change_request_id: str | None = None,
        workspace_id: str | None = None,
    ) -> list[dict[str, Any]]:
        """GET /workboard/stale-warnings."""
        params: dict[str, str] = {}
        if feature_id:
            params["feature_id"] = feature_id
        if change_request_id:
            params["change_request_id"] = change_request_id
        params.update(self._workspace_params(workspace_id))
        with self._client() as client:
            r = client.get("/workboard/stale-warnings", params=params)
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def refresh_change_request_gate_runs(
        self,
        change_request_id: str,
        evaluations: list[dict[str, Any]] | None = None,
        *,
        evaluations_only: bool = False,
        workspace_id: str | None = None,
    ) -> list[dict[str, Any]]:
        """POST /workboard/change-requests/{id}/gate-runs/refresh.

        ``evaluations`` are agent gate verdicts (gate, state, hint, confidence,
        judge_model, eval_suite_version); omit for a deterministic-only refresh.
        """
        body: dict[str, Any] = {}
        if evaluations:
            body["evaluations"] = evaluations
        if evaluations_only:
            body["evaluations_only"] = True
        with self._client() as client:
            r = client.post(
                f"/workboard/change-requests/{change_request_id}/gate-runs/refresh",
                json=body,
                params=self._workspace_params(workspace_id),
            )
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def list_change_request_gate_runs(
        self,
        change_request_id: str,
        limit: int = 50,
        *,
        workspace_id: str | None = None,
    ) -> list[dict[str, Any]]:
        """GET /workboard/change-requests/{id}/gate-runs?limit=."""
        with self._client() as client:
            r = client.get(
                f"/workboard/change-requests/{change_request_id}/gate-runs",
                params={"limit": str(limit), **self._workspace_params(workspace_id)},
            )
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def refresh_artifact_readiness_runs(
        self,
        artifact_id: str,
        evaluations: list[dict[str, Any]] | None = None,
        *,
        workspace_id: str | None = None,
    ) -> list[dict[str, Any]]:
        """POST /artifacts/{id}/readiness-runs/refresh."""
        body: dict[str, Any] = {}
        if evaluations:
            body["evaluations"] = evaluations
        with self._client() as client:
            r = client.post(
                f"/artifacts/{artifact_id}/readiness-runs/refresh",
                json=body,
                params=self._workspace_params(workspace_id),
            )
            r.raise_for_status()
            return _parse_items_payload(r.json())

    def list_artifact_readiness_runs(
        self,
        artifact_id: str,
        limit: int = 50,
        *,
        workspace_id: str | None = None,
    ) -> list[dict[str, Any]]:
        """GET /artifacts/{id}/readiness-runs?limit=."""
        params = {"limit": str(limit), **self._workspace_params(workspace_id)}
        with self._client() as client:
            r = client.get(
                f"/artifacts/{artifact_id}/readiness-runs",
                params=params,
            )
            r.raise_for_status()
            return _parse_items_payload(r.json())

    async def asearch_governance_knowledge(
        self,
        *,
        workspace_id: str,
        query: str,
        linked_feature_id: str = "",
        linked_request_id: str = "",
        document_types: list[str] | None = None,
        authority_levels: list[str] | None = None,
        limit: int = 5,
        context_mode: str = "section",
    ) -> dict[str, Any]:
        """POST /governance/context/search — workspace-scoped Knowledge retrieval."""
        body: dict[str, Any] = {
            "workspace_id": workspace_id,
            "query": query,
            "linked_feature_id": linked_feature_id,
            "linked_request_id": linked_request_id,
            "document_types": document_types or [],
            "authority_levels": authority_levels or [],
            "max_chunks": limit,
            "context_mode": context_mode,
        }
        async with self._aclient() as client:
            r = await client.post("/governance/context/search", json=body)
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else raw
            if (
                isinstance(data, dict)
                and "items" not in data
                and isinstance(data.get("results"), list)
            ):
                data = {**data, "items": data["results"]}
            return data

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

    async def aget_skills(self, *, workspace_id: str) -> list[dict[str, Any]]:
        """Async workspace-scoped GET /skills — for LangGraph async nodes."""
        workspace = str(workspace_id or "").strip()
        if not workspace:
            raise ValueError("workspace_id is required")
        async with self._aclient() as client:
            r = await client.get("/skills", params={"workspace_id": workspace})
            r.raise_for_status()
            payload = r.json()
        return _parse_items_payload(payload)

    async def alist_governance_feedback_events(
        self,
        *,
        workspace_id: str,
        status: str | None = None,
        change_request_id: str | None = None,
        event_type: str | None = None,
        limit: int = 200,
    ) -> list[dict[str, Any]]:
        """Async GET /governance/feedback-events — integration-derived feedback queue.

        Doc Registry has no by-id lookup for feedback events; callers that need a
        single event fetch the queue (optionally status-scoped) and filter by id.
        """
        workspace = str(workspace_id or "").strip()
        if not workspace:
            raise ValueError("workspace_id is required")
        params: dict[str, Any] = {"limit": limit, "workspace_id": workspace}
        if status:
            params["status"] = status
        if change_request_id:
            params["change_request_id"] = change_request_id
        if event_type:
            params["event_type"] = event_type
        async with self._aclient() as client:
            r = await client.get("/governance/feedback-events", params=params)
            r.raise_for_status()
            return _parse_items_payload(r.json())

    async def _afetch_artifact_file_by_path(
        self, artifact_id: str, path: str, *, workspace_id: str | None = None
    ) -> str:
        """Fetch bounded artifact text using the slash-safe ``?path=`` route."""
        async with self._aclient() as client:
            r = await client.get(
                f"/artifacts/{artifact_id}/files/_",
                params={"path": path, **self._workspace_params(workspace_id)},
            )
            r.raise_for_status()
            raw = r.json()
            data = _unwrap_huma_body(raw) if isinstance(raw, dict) else {}
        inline = data.get("content")
        if isinstance(inline, str):
            if len(inline.encode("utf-8")) > _MAX_ARTIFACT_FILE_BYTES:
                raise ValueError("artifact file exceeds the 1 MiB limit")
            return inline
        return ""

    async def aget_artifact_files(
        self, artifact_id: str, *, workspace_id: str | None = None
    ) -> list[dict[str, Any]]:
        """List an artifact's documents [{path, role, size_bytes}] via GET /artifacts/{id}/files."""
        async with self._aclient() as client:
            r = await client.get(
                f"/artifacts/{artifact_id}/files", params=self._workspace_params(workspace_id)
            )
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

    async def adispatch_gate_tasks(
        self, artifact_id: str, *, workspace_id: str | None = None
    ) -> dict[str, Any]:
        """Async POST /api/v1/artifacts/{artifact_id}/gate-tasks — create ide_agent gate
        tasks for the artifact's enabled gates so a coding agent can evaluate them."""
        from urllib.parse import quote

        async with self._aclient() as client:
            r = await client.post(
                f"/api/v1/artifacts/{quote(artifact_id, safe='')}/gate-tasks",
                params=self._workspace_params(workspace_id),
            )
            r.raise_for_status()
            return r.json()

    async def aload_artifact_bundle_by_role(
        self, artifact_id: str, *, workspace_id: str | None = None
    ) -> dict[str, str]:
        """Group an artifact's documents by role and fetch each by path.

        Returns {role: markdown}; multiple docs sharing a role are concatenated
        in stable path order. Missing or failed fetches are skipped (no crash).
        Uses the ?path= query form to support slashed paths (per spec §6).
        """
        files = await self.aget_artifact_files(artifact_id, workspace_id=workspace_id)
        by_role: dict[str, list[str]] = {}
        package_bytes = 0
        for f in sorted(files, key=lambda x: x.get("path", "")):
            path = f.get("path", "")
            role = (f.get("role") or "unspecified").strip()
            if not path:
                continue
            try:
                text = await self._afetch_artifact_file_by_path(
                    artifact_id, path, workspace_id=workspace_id
                )
            except (httpx.HTTPError, OSError, ValueError):
                text = ""
            if text:
                package_bytes += len(text.encode("utf-8"))
                if package_bytes > _MAX_ARTIFACT_PACKAGE_BYTES:
                    raise ValueError("artifact package exceeds the 10 MiB limit")
                by_role.setdefault(role, []).append(text)
        return {role: "\n\n".join(parts) for role, parts in by_role.items()}
