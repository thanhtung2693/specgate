#!/usr/bin/env python3
"""Generate IDE plugin metadata from plugins/package.json."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


SCRIPT_PATH = Path(__file__).resolve()


def discover_repo_root() -> Path:
    for parent in [SCRIPT_PATH.parent, *SCRIPT_PATH.parents]:
        if (parent / "plugins/package.json").is_file():
            return parent
    return SCRIPT_PATH.parent


REPO_ROOT = discover_repo_root()
DEFAULT_PLUGIN_DIR = REPO_ROOT / "plugins"


def compact(value: object) -> str:
    return json.dumps(value, separators=(",", ":"), ensure_ascii=False)


def pretty(value: object) -> str:
    return json.dumps(value, indent=2, ensure_ascii=False) + "\n"


def load_package(plugin_dir: Path) -> dict:
    package_path = plugin_dir / "package.json"
    data = json.loads(package_path.read_text())
    required = [
        "name",
        "display_name",
        "version",
        "description",
        "short_description",
        "long_description",
        "developer_name",
        "repository",
        "license",
        "category",
        "keywords",
        "skills",
    ]
    missing = [key for key in required if key not in data]
    if missing:
        raise SystemExit(f"{package_path} missing required keys: {', '.join(missing)}")
    return data


def outputs(plugin_dir: Path, pkg: dict) -> dict[Path, str]:
    name = pkg["name"]
    display_name = pkg["display_name"]
    developer_name = pkg["developer_name"]
    description = pkg["description"]
    version = pkg["version"]
    repository = pkg["repository"]
    license_name = pkg["license"]
    keywords = pkg["keywords"]
    skills = pkg["skills"]

    author = {"name": developer_name}
    codex = {
        "name": name,
        "version": version,
        "description": description,
        "author": author,
        "repository": repository,
        "license": license_name,
        "keywords": keywords,
        "skills": "./skills/",
        "interface": {
            "displayName": display_name,
            "shortDescription": pkg["short_description"],
            "longDescription": pkg["long_description"],
            "developerName": developer_name,
            "category": pkg["category"],
            "capabilities": ["Read", "Write"],
            "composerIcon": "./assets/logo.svg",
            "logo": "./assets/logo.svg",
            "defaultPrompt": [
                "Check this artifact with SpecGate readiness.",
                "Pick up and implement this SpecGate work item.",
                "Complete delivery review for this change request.",
            ],
        },
    }

    claude = {
        "name": name,
        "description": description,
        "version": version,
        "author": author,
        "homepage": repository,
        "repository": repository,
        "license": license_name,
        "keywords": keywords,
        "skills": "./skills/",
        "hooks": "./hooks/hooks-claude.json",
    }

    claude_marketplace = {
        "name": name,
        "owner": author,
        "metadata": {"description": f"{display_name} focused lifecycle skills for Claude Code."},
        "plugins": [
            {
                "name": name,
                "source": "./",
                "description": description,
                "version": version,
                "author": author,
                "strict": True,
                "skills": [f"./skills/{skill}" for skill in skills],
                "hooks": "./hooks/hooks-claude.json",
            }
        ],
    }

    cursor = {
        "name": name,
        "displayName": display_name,
        "version": version,
        "description": description,
        "author": author,
        "homepage": repository,
        "repository": repository,
        "license": license_name,
        "keywords": keywords,
        "category": "developer-tools",
        "logo": "assets/logo.svg",
        "skills": "./skills/",
        "rules": "./rules/",
        "hooks": "./hooks/hooks-cursor.json",
    }

    cursor_marketplace = {
        "name": name,
        "owner": author,
        "metadata": {"description": f"{display_name} focused lifecycle skills for Cursor."},
        "plugins": [
            {
                "name": name,
                "source": "./",
                "description": description,
                "version": version,
            }
        ],
    }

    codex_marketplace = {
        "name": name,
        "interface": {"displayName": display_name},
        "plugins": [
            {
                "name": name,
                "source": {"source": "local", "path": "./"},
                "policy": {"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
                "category": pkg["category"],
            }
        ],
    }

    codex_personal_marketplace = {
        "name": "personal",
        "interface": {"displayName": "Personal"},
        "plugins": [
            {
                "name": name,
                "source": {"source": "local", "path": "__SPECGATE_PLUGIN_PATH__"},
                "policy": {"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
                "category": pkg["category"],
            }
        ],
    }

    return {
        plugin_dir / ".codex-plugin/plugin.json": pretty(codex),
        plugin_dir / ".claude-plugin/plugin.json": pretty(claude),
        plugin_dir / ".claude-plugin/marketplace.json": pretty(claude_marketplace),
        plugin_dir / ".cursor-plugin/plugin.json": pretty(cursor),
        plugin_dir / ".cursor-plugin/marketplace.json": pretty(cursor_marketplace),
        plugin_dir / ".agents/plugins/marketplace.json": pretty(codex_marketplace),
        plugin_dir / ".agents/plugins/personal-marketplace.json": pretty(codex_personal_marketplace),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--plugin-dir",
        type=Path,
        default=DEFAULT_PLUGIN_DIR,
        help="plugin package directory containing package.json",
    )
    parser.add_argument("--check", action="store_true", help="fail if generated files are stale")
    args = parser.parse_args()
    plugin_dir = args.plugin_dir.resolve()

    stale: list[Path] = []
    for path, content in outputs(plugin_dir, load_package(plugin_dir)).items():
        if args.check:
            if not path.exists() or path.read_text() != content:
                stale.append(path)
            continue
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    if stale:
        for path in stale:
            try:
                display_path = path.relative_to(REPO_ROOT)
            except ValueError:
                display_path = path
            print(f"stale generated plugin metadata: {display_path}", file=sys.stderr)
        print("run: make generate-plugins", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
