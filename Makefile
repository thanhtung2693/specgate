# Root Makefile — repo-wide helper targets.
# Per-module builds/tests live in app/doc-registry/Makefile, app/agents/ (uv), app/ui/ (npm).

.PHONY: help setup env up down seed seed-skills generate-plugins sync-plugins check-plugins

ENV_MODULES := app/doc-registry app/agents app/ui

# Onboarding uses the dev override so the agents service runs the keyless
# in-memory LangGraph runtime (no LANGSMITH_API_KEY). Production uses
# `docker compose` against docker-compose.yml alone (Self-Hosted Lite image).
COMPOSE := docker compose -f docker-compose.yml -f docker-compose.dev.yml

ROOT_ENV := .env

help:
	@echo "Onboarding (self-host):"
	@echo "  setup         One command from a clean checkout: env files + secrets + full stack up (no keys)"
	@echo "  up            Start the full stack (detached, waits for health)"
	@echo "  down          Stop the stack"
	@echo "  seed          Load the demo planning dataset into the running stack (idempotent)"
	@echo "  seed-skills   Force-refresh LLM gate rubric skills (auto-runs on startup)"
	@echo "  env           Create missing module .env files + generate the settings encryption key"
	@echo "Plugins:"
	@echo "  generate-plugins Generate native plugin manifests from plugins/package.json"
	@echo "  sync-plugins  Sync root plugin assets into agentpackages/plugins/"
	@echo "  check-plugins Verify embedded plugins match canonical sources in plugins/ (CI guard)"

# Operator bootstrap — clone -> running SpecGate in one command. Idempotent:
# re-running only fills in what is missing and reconciles the stack. LLM provider,
# model, and key are configured in-app afterwards (Settings -> Model), not here.
setup: env up
	@echo ""
	@echo "==> SpecGate is up (no API keys required to boot). Next steps:"
	@echo "    - UI:            http://localhost:$$(. ./.env 2>/dev/null; echo $${UI_PORT:-3000})"
	@echo "    - Doc Registry:  http://localhost:$$(. ./.env 2>/dev/null; echo $${DOC_REGISTRY_PORT:-8080})"
	@echo "    - Agents API:    http://localhost:$$(. ./.env 2>/dev/null; echo $${AGENTS_PORT:-2024}) (keyless langgraph dev runtime)"
	@echo "    - Set your LLM + embedding provider + key in the app:  Settings -> Model"
	@echo "    - Install CLI:       curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh"
	@echo "    - Point CLI:         specgate config set server http://localhost:$$(. ./.env 2>/dev/null; echo $${DOC_REGISTRY_PORT:-8080})"
	@echo "    - Write IDE setup:   curl -fsSL http://localhost:$$(. ./.env 2>/dev/null; echo $${DOC_REGISTRY_PORT:-8080})/plugins/install.sh | sh"
	@echo "    - Load demo data (optional):  make seed"

# Create any missing module .env from its .env.example (never overwrites an
# existing .env), then generate SETTINGS_ENCRYPTION_KEY into app/doc-registry/.env.
# mcp.api_key auto-generates into the settings DB on first server start.
env:
	@scripts/check-ports.sh $(if $(NON_INTERACTIVE),--non-interactive,)
	@for m in $(ENV_MODULES); do \
	  if [ ! -f $$m/.env ] && [ -f $$m/.env.example ]; then \
	    cp $$m/.env.example $$m/.env; echo "created $$m/.env"; \
	  fi; \
	done
	@./scripts/dev-secrets.sh

up:
	$(COMPOSE) up -d --wait

down:
	$(COMPOSE) down

# Optional demo dataset for evaluating the UI. Runs the seed inside the running
# doc-registry container so it targets the compose Postgres; --seed-demo applies
# migrations, seeds, and exits without binding the server port. Idempotent by
# feature key, so re-running never duplicates.
seed:
	$(COMPOSE) exec -T -w /src/doc-registry doc-registry go run ./cmd/doc-registry --seed-demo

# Force-refresh LLM gate rubric skills from skills_seed.json. Normally this
# runs automatically on every server startup; use this target to apply seed
# changes without restarting the container.
seed-skills:
	$(COMPOSE) exec -T -w /src/doc-registry doc-registry go run ./cmd/doc-registry --seed-skills

# Plugin sync — copies the canonical plugin sources into downstream destinations:
#   1. plugins/skills and plugins/hooks stay the native plugin source of truth
#   2. app/doc-registry/internal/agentpackages/plugins/  — embedded by the Go server
#      (//go:embed cannot follow symlinks across the module boundary, so real copies needed)
# Run after editing plugin files. Commit the result; check-plugins (CI) guards drift.
AGENTPKG_PLUGINS := app/doc-registry/internal/agentpackages/plugins

generate-plugins:
	@python3 app/doc-registry/scripts/generate-plugin-metadata.py --plugin-dir plugins
	@echo "Generated plugin manifests from plugins/package.json."

sync-plugins: generate-plugins
	@SPECGATE_PLUGIN_SOURCE=plugins SPECGATE_EMBEDDED_PLUGIN_DEST=$(AGENTPKG_PLUGINS) sh app/doc-registry/scripts/sync-embedded-plugins.sh
	@echo "Synced plugin skills, hooks, manifests, and installer files. Commit the result."

# Verify embedded plugin content matches canonical sources in plugins/.
# Works whether the plugin dirs use symlinks (diff follows them) or copies.
# CI guard — run in CI to catch out-of-sync embedded files.
check-plugins:
	@python3 app/doc-registry/scripts/generate-plugin-metadata.py --plugin-dir plugins --check
	@diff -r plugins/skills $(AGENTPKG_PLUGINS)/skills >/dev/null
	@diff -r plugins/hooks $(AGENTPKG_PLUGINS)/hooks >/dev/null
	@diff -r plugins/assets $(AGENTPKG_PLUGINS)/assets >/dev/null
	@diff -r plugins/rules $(AGENTPKG_PLUGINS)/rules >/dev/null
	@diff -r plugins/.agents $(AGENTPKG_PLUGINS)/.agents >/dev/null
	@diff -r plugins/.codex-plugin $(AGENTPKG_PLUGINS)/.codex-plugin >/dev/null
	@diff -r plugins/.claude-plugin $(AGENTPKG_PLUGINS)/.claude-plugin >/dev/null
	@diff -r plugins/.cursor-plugin $(AGENTPKG_PLUGINS)/.cursor-plugin >/dev/null
	@diff plugins/package.json $(AGENTPKG_PLUGINS)/package.json >/dev/null
	@echo "plugins in sync with canonical plugin sources"
	@for snippet in \
	  'normalize_agents()' \
	  'tmpfile=$$(mktemp' \
	  'find_specgate_bin()' \
	  'plugins install --agent "$$AGENT"' \
	  '"$$specgate_bin" "$$@"'; do \
	  if ! grep -Fq "$$snippet" plugins/install.sh; then \
	    echo "ERROR: root plugin installer missing hardening snippet: $$snippet" >&2; exit 1; fi; \
	  if ! grep -Fq "$$snippet" $(AGENTPKG_PLUGINS)/install.sh.tmpl; then \
	    echo "ERROR: embedded plugin installer missing hardening snippet: $$snippet" >&2; exit 1; fi; \
	done
	@if grep -rn 'ReadMcpResourceTool\|resolve_work_item\|list_work_items\|report_implementation_feedback\|trigger_delivery_review' \
	    plugins/skills plugins/hooks plugins/rules 2>/dev/null; then \
	  echo "ERROR: plugins still contain MCP tool call names — migrate to CLI commands" >&2; exit 1; fi
	@for skill in using-specgate setting-up-specgate-project preparing-work delivering-work; do \
	  if [ ! -f "plugins/skills/$$skill/SKILL.md" ]; then \
	    echo "ERROR: missing focused skill plugins/skills/$$skill/SKILL.md" >&2; exit 1; fi; done
	@if find plugins/skills -maxdepth 1 -type d -name 'specgate-handoff' | grep -q .; then \
	  echo "ERROR: removed specgate-handoff skill still exists" >&2; exit 1; fi
	@for phrase in "specgate status --json" "specgate work context" "specgate delivery report" "specgate gates run" "specgate delivery review"; do \
	  if ! grep -rn "$$phrase" plugins/skills plugins/rules/ >/dev/null 2>&1; then \
	    echo "ERROR: missing required CLI command in plugins: $$phrase" >&2; exit 1; fi; done
	@echo "plugin CLI migration checks passed"
