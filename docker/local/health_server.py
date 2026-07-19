"""Loopback-only aggregate health adapter for the appliance gateway."""

from __future__ import annotations

import json
import os
import socket
import subprocess
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.request import urlopen


STATE_DIR = Path("/run/specgate/components")
RESTART_DIR = Path("/run/specgate/restarts")
FAILURE_FILE = Path("/data/diagnostics/last-failure.json")


def probe_url(url: str) -> bool:
    try:
        with urlopen(url, timeout=2) as response:  # noqa: S310 - loopback-only URLs
            return 200 <= response.status < 300
    except OSError:
        return False


def probe_port(host: str, port: int) -> bool:
    try:
        with socket.create_connection((host, port), timeout=2):
            return True
    except OSError:
        return False


def component_state(name: str, ready: bool) -> str:
    if ready:
        (RESTART_DIR / name).unlink(missing_ok=True)
        return "ready"
    try:
        return (STATE_DIR / name).read_text().strip() or "unavailable"
    except OSError:
        return "unavailable"


def last_failure() -> dict[str, object] | None:
    try:
        value = json.loads(FAILURE_FILE.read_text())
        return value if isinstance(value, dict) else None
    except (OSError, json.JSONDecodeError):
        return None


def component_payload() -> dict[str, object]:
    postgres = subprocess.run(
        [
            "pg_isready",
            "-h",
            "127.0.0.1",
            "-p",
            "5432",
            "-U",
            os.environ.get("POSTGRES_USER", "docreg"),
            "-d",
            os.environ.get("POSTGRES_DB", "docreg"),
        ],
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        timeout=2,
    ).returncode == 0
    states = {
        "postgres": postgres,
        "doc-registry": probe_url("http://127.0.0.1:8080/readyz"),
        "agents": probe_port("127.0.0.1", 2024),
        "nginx": probe_url("http://127.0.0.1:3000/"),
    }
    version = os.environ.get("SPECGATE_VERSION", "dev")
    details = {
        "postgres": {"endpoint": "127.0.0.1:5432"},
        "doc-registry": {"endpoint": "127.0.0.1:8080", "public_path": "/api/doc-registry/"},
        "agents": {"endpoint": "127.0.0.1:2024", "public_path": "/api/agents/"},
        "nginx": {"endpoint": "0.0.0.0:3000", "public_path": "/"},
    }
    components = {
        name: {
            "status": "ok" if ready else "fail",
            "state": component_state(name, ready),
            "version": version,
            **details[name],
        }
        for name, ready in states.items()
    }
    return {
        "status": "ok" if all(states.values()) else "degraded",
        "version": version,
        "components": components,
        "last_failure": last_failure(),
    }


class HealthHandler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler API
        if self.path not in {"/healthz", "/healthz/components"}:
            self.send_error(404)
            return

        try:
            payload = component_payload()
            status = 200 if payload["status"] == "ok" else 503
        except (OSError, subprocess.TimeoutExpired):
            payload = {"status": "degraded", "components": {}}
            status = 503

        if self.path == "/healthz":
            payload = {"status": payload["status"]}
        else:
            # Diagnostics remain readable while a component is unavailable.
            status = 200
        body = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format: str, *args: object) -> None:
        # Docker probes this endpoint every five seconds. Component state and
        # last-failure.json carry actionable diagnostics; access lines would
        # only grow the container log indefinitely.
        return


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", 9090), HealthHandler).serve_forever()
