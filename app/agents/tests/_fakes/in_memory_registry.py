"""In-process Doc Registry stub used by tests + LangGraph dev fallback.

Hosts scratch bodies + artifact metadata in memory so a governance graph can
build without a live Doc Registry connection.
"""

from __future__ import annotations

import uuid
from typing import Any


class InMemoryRegistry:
    """Stub registry that satisfies the ``ArtifactService`` Protocol."""

    def __init__(self) -> None:
        self._scratch: dict[str, str] = {}
        self._artifacts: dict[str, dict[str, Any]] = {}

    async def apost_artifact(self, *, kind: str, manifest: dict[str, Any], refs: list[dict]) -> str:
        artifact_id = f"artf-{uuid.uuid4().hex[:8]}"
        self._artifacts[artifact_id] = {
            "kind": kind,
            "manifest": manifest,
            "refs": refs,
            "status": "draft",
        }
        return artifact_id

    async def aput_artifact_status(
        self,
        *,
        artifact_id: str,
        status: str,
        manifest_patch: dict[str, Any] | None = None,
        if_match: str | None = None,
    ) -> None:
        _ = if_match
        if artifact_id in self._artifacts:
            if status:
                self._artifacts[artifact_id]["status"] = status
            if manifest_patch is not None:
                existing = self._artifacts[artifact_id].get("manifest")
                merged = dict(existing) if isinstance(existing, dict) else {}
                merged.update(manifest_patch)
                self._artifacts[artifact_id]["manifest"] = merged

    async def aget_scratch_body(self, *, scratch_file_id: str) -> str:
        return self._scratch.get(scratch_file_id, "")

    async def aput_scratch_body(self, *, body: str) -> str:
        file_id = f"scratch-{uuid.uuid4().hex[:8]}"
        self._scratch[file_id] = body
        return file_id
