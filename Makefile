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
	@echo "Contributor source integration:"
	@echo "  setup         One command from a clean checkout: env files + secrets + full stack up (no keys)"
	@echo "  up            Start the full stack (detached, waits for health)"
	@echo "  down          Stop the stack"
	@echo "  seed          Load demo governance data into the running stack (idempotent)"
	@echo "  seed-skills   Force-refresh LLM gate rubric skills (auto-runs on startup)"
	@echo "  env           Create missing module .env files + generate the settings encryption key"
	@echo "Plugins:"
	@echo "  generate-plugins Generate native plugin manifests from plugins/package.json"
	@echo "  sync-plugins  Sync root plugin assets into agentpackages/plugins/"
	@echo "  check-plugins Verify embedded plugins match canonical sources in plugins/ (CI guard)"

# Contributor bootstrap — clone -> source stack in one command. Idempotent:
# re-running only fills in what is missing and reconciles the stack. LLM provider,
# model, and key are configured in-app afterwards (Settings → Models), not here.
setup: env up
	@echo ""
	@echo "==> SpecGate is up (no API keys required to boot). Next steps:"
	@echo "    - UI:            http://localhost:$$(. ./.env 2>/dev/null; echo $${UI_PORT:-3000})"
	@echo "    - Doc Registry:  http://localhost:$$(. ./.env 2>/dev/null; echo $${DOC_REGISTRY_PORT:-8080})"
	@echo "    - Agents API:    http://localhost:$$(. ./.env 2>/dev/null; echo $${AGENTS_PORT:-2024}) (keyless langgraph dev runtime)"
	@echo "    - Set your LLM + embedding provider + key in the app:  Settings → Models"
	@echo "    - Install CLI:       curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh"
	@echo "    - Point CLI:         specgate config server http://localhost:$$(. ./.env 2>/dev/null; echo $${UI_PORT:-3000})/api/doc-registry"
	@echo "    - Write IDE setup:   specgate plugins install"
	@echo "    - Load demo data (optional):  make seed"

# Create any missing module .env from its .env.example (never overwrites an
# existing .env), then generate SETTINGS_ENCRYPTION_KEY into app/doc-registry/.env.
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

# Optional demo governance data for evaluating the UI. Demo work and Knowledge
# are workspace-scoped, so pass the workspace ID that should own them. Runs the
# seed inside the running doc-registry container; it is idempotent by feature
# key, so re-running never duplicates.
seed:
	@test -n "$(DEMO_WORKSPACE_ID)" || (echo "Set DEMO_WORKSPACE_ID: make seed DEMO_WORKSPACE_ID=<workspace-id>"; exit 2)
	$(COMPOSE) exec -T -w /src/doc-registry doc-registry go run ./cmd/doc-registry --seed-demo --seed-demo-workspace-id "$(DEMO_WORKSPACE_ID)" $(if $(DEMO_CREATED_BY),--seed-demo-created-by "$(DEMO_CREATED_BY)")

# Force-refresh LLM gate rubric skills from skills_seed.json. Normally this
# runs automatically on every server startup; use this target to apply seed
# changes without restarting the container.
seed-skills:
	$(COMPOSE) exec -T -w /src/doc-registry doc-registry go run ./cmd/doc-registry --seed-skills

# Plugin sync — copies the canonical plugin sources into downstream destinations:
#   1. plugins/skills and plugins/hooks stay the native plugin source of truth
#   2. app/doc-registry/internal/agentpackages/plugins/  — embedded by the Go server
#   3. app/cli/internal/command/local_plugin_assets/ — embedded by Local CLI
#      (//go:embed cannot follow symlinks across the module boundary, so real copies needed)
# Run after editing plugin files. Commit the result; check-plugins (CI) guards drift.
AGENTPKG_PLUGINS := app/doc-registry/internal/agentpackages/plugins
LOCAL_PLUGIN_ASSETS := app/cli/internal/command/local_plugin_assets

generate-plugins:
	@python3 app/doc-registry/scripts/generate-plugin-metadata.py --plugin-dir plugins
	@echo "Generated plugin manifests from plugins/package.json."

sync-plugins: generate-plugins
	@SPECGATE_PLUGIN_SOURCE=plugins SPECGATE_EMBEDDED_PLUGIN_DEST=$(AGENTPKG_PLUGINS) sh app/doc-registry/scripts/sync-embedded-plugins.sh
	@SPECGATE_PLUGIN_SOURCE=plugins SPECGATE_EMBEDDED_PLUGIN_DEST=$(LOCAL_PLUGIN_ASSETS) sh app/doc-registry/scripts/sync-embedded-plugins.sh
	@echo "Synced plugin skills, hooks, manifests, and package files. Commit the result."

# Verify embedded plugin content matches canonical sources in plugins/.
# Works whether the plugin dirs use symlinks (diff follows them) or copies.
# CI guard — run in CI to catch out-of-sync embedded files.
check-plugins:
	@python3 app/doc-registry/scripts/generate-plugin-metadata.py --plugin-dir plugins --check
	@for file in \
		$(LOCAL_PLUGIN_ASSETS)/.agents/plugins/marketplace.json \
		$(LOCAL_PLUGIN_ASSETS)/.agents/plugins/personal-marketplace.json; do \
		if ! git ls-files --error-unmatch "$$file" >/dev/null 2>&1; then \
			echo "ERROR: required embedded plugin asset is not tracked: $$file" >&2; exit 1; \
		fi; \
	done
	@diff -r plugins/skills $(AGENTPKG_PLUGINS)/skills >/dev/null
	@diff -r plugins/hooks $(AGENTPKG_PLUGINS)/hooks >/dev/null
	@diff -r plugins/assets $(AGENTPKG_PLUGINS)/assets >/dev/null
	@diff -r plugins/rules $(AGENTPKG_PLUGINS)/rules >/dev/null
	@diff -r plugins/.agents $(AGENTPKG_PLUGINS)/.agents >/dev/null
	@diff -r plugins/.codex-plugin $(AGENTPKG_PLUGINS)/.codex-plugin >/dev/null
	@diff -r plugins/.claude-plugin $(AGENTPKG_PLUGINS)/.claude-plugin >/dev/null
	@diff -r plugins/.cursor-plugin $(AGENTPKG_PLUGINS)/.cursor-plugin >/dev/null
	@diff plugins/README.md $(AGENTPKG_PLUGINS)/README.md >/dev/null
	@diff plugins/README.md.tmpl $(AGENTPKG_PLUGINS)/README.md.tmpl >/dev/null
	@diff plugins/package.json $(AGENTPKG_PLUGINS)/package.json >/dev/null
	@diff -r plugins/skills $(LOCAL_PLUGIN_ASSETS)/skills >/dev/null
	@diff -r plugins/hooks $(LOCAL_PLUGIN_ASSETS)/hooks >/dev/null
	@diff -r plugins/assets $(LOCAL_PLUGIN_ASSETS)/assets >/dev/null
	@diff -r plugins/rules $(LOCAL_PLUGIN_ASSETS)/rules >/dev/null
	@diff -r plugins/.agents $(LOCAL_PLUGIN_ASSETS)/.agents >/dev/null
	@diff -r plugins/.codex-plugin $(LOCAL_PLUGIN_ASSETS)/.codex-plugin >/dev/null
	@diff -r plugins/.claude-plugin $(LOCAL_PLUGIN_ASSETS)/.claude-plugin >/dev/null
	@diff -r plugins/.cursor-plugin $(LOCAL_PLUGIN_ASSETS)/.cursor-plugin >/dev/null
	@diff plugins/README.md $(LOCAL_PLUGIN_ASSETS)/README.md >/dev/null
	@diff plugins/README.md.tmpl $(LOCAL_PLUGIN_ASSETS)/README.md.tmpl >/dev/null
	@diff plugins/package.json $(LOCAL_PLUGIN_ASSETS)/package.json >/dev/null
	@echo "plugins in sync with canonical plugin sources"
	@if grep -rn 'resolve_work_item\|list_work_items\|report_implementation_feedback\|trigger_delivery_review' \
	    plugins/skills plugins/hooks plugins/rules 2>/dev/null; then \
	  echo "ERROR: plugins still contain legacy tool call names — migrate to CLI commands" >&2; exit 1; fi
	@if grep -rn '\.claude/skills/using-specgate\|legacy global skill\|older global skill' \
	    plugins/hooks $(AGENTPKG_PLUGINS)/hooks 2>/dev/null; then \
	  echo "ERROR: plugin hooks must use bundled skills, not stale global fallbacks" >&2; exit 1; fi
	@for skill in specgate specgate-project-setup specgate-work-preparation specgate-work-delivery; do \
	  if [ ! -f "plugins/skills/$$skill/SKILL.md" ]; then \
	    echo "ERROR: missing focused skill plugins/skills/$$skill/SKILL.md" >&2; exit 1; fi; \
	  if ! grep -Fxq "name: $$skill" "plugins/skills/$$skill/SKILL.md"; then \
	    echo "ERROR: skill directory and frontmatter name disagree for $$skill" >&2; exit 1; fi; done
	@for phrase in "specgate doctor --json" "specgate work show" "specgate work context" "specgate delivery report" "specgate change submit" "specgate change status"; do \
	  if ! grep -rn "$$phrase" plugins/skills plugins/rules/ >/dev/null 2>&1; then \
	    echo "ERROR: missing required CLI command in plugins: $$phrase" >&2; exit 1; fi; done
	@tmp_root=$$(mktemp -d); cleanup() { rm -rf "$$tmp_root"; }; trap cleanup EXIT INT TERM; \
	  mkdir -p "$$tmp_root/home/.claude/skills/using-specgate" "$$tmp_root/cli-plugin"; \
	  printf '%s\n' 'OLD GLOBAL UNRELATED CONTENT' > "$$tmp_root/home/.claude/skills/using-specgate/SKILL.md"; \
	  cp -R plugins/. "$$tmp_root/cli-plugin"; \
	  printf '%s\n' 'specgate-plugin-v1' > "$$tmp_root/cli-plugin/.specgate-owned"; \
	  native_context=$$(HOME="$$tmp_root/home" plugins/hooks/session-start codex | jq -r '.additionalContext'); \
	  cli_context=$$(HOME="$$tmp_root/home" "$$tmp_root/cli-plugin/hooks/session-start" codex | jq -r '.additionalContext'); \
	  if ! printf '%s\n' "$$native_context" | grep -Fq 'load `specgate`'; then \
	    cleanup; echo "ERROR: session-start hook did not route explicit SpecGate work" >&2; exit 1; fi; \
	  if ! printf '%s\n' "$$native_context" | grep -Fq 'IDE plugin manager owns'; then \
	    cleanup; echo "ERROR: session-start hook did not identify native marketplace ownership" >&2; exit 1; fi; \
	  if ! printf '%s\n' "$$cli_context" | grep -Fq 'SpecGate CLI owns'; then \
	    cleanup; echo "ERROR: session-start hook did not identify CLI ownership" >&2; exit 1; fi; \
	  if printf '%s\n' "$$native_context" | grep -Fq '# Using SpecGate'; then \
	    cleanup; echo "ERROR: session-start hook injected the full router instead of the short bootstrap" >&2; exit 1; fi; \
	  if printf '%s\n' "$$native_context" | grep -Fq 'UNRELATED CONTENT'; then \
	    cleanup; echo "ERROR: session-start hook loaded stale global skill content" >&2; exit 1; fi; \
	  cleanup
	@echo "plugin CLI migration checks passed"
