# Root Makefile — repository-level development uses the same single-container
# appliance topology shipped to Full-mode users.

.PHONY: help setup env build up down logs seed seed-skills generate-plugins sync-plugins check-plugins

LOCAL_DEPLOY_DIR := deploy/local
LOCAL_VERSION ?= dev
LOCAL_PROJECT ?= specgate-dev
LOCAL_IMAGE := ghcr.io/thanhtung2693/specgate:$(LOCAL_VERSION)
COMPOSE := SPECGATE_VERSION=$(LOCAL_VERSION) SPECGATE_COMPOSE_PROJECT=$(LOCAL_PROJECT) docker compose \
	--env-file $(LOCAL_DEPLOY_DIR)/.env \
	-f $(LOCAL_DEPLOY_DIR)/compose.yml

help:
	@echo "Contributor local appliance:"
	@echo "  setup         Create env, build the all-in-one image, and start it"
	@echo "  build         Build the all-in-one development image"
	@echo "  up            Start the appliance (detached, waits for health)"
	@echo "  down          Stop the appliance without deleting data"
	@echo "  logs          Follow appliance logs"
	@echo "  seed          Load demo governance data into the running stack (idempotent)"
	@echo "  seed-skills   Force-refresh LLM gate rubric skills (auto-runs on startup)"
	@echo "  env           Create private appliance env files and encryption key"
	@echo "Plugins:"
	@echo "  generate-plugins Generate native plugin manifests from plugins/package.json"
	@echo "  sync-plugins  Sync root plugin assets into agentpackages/plugins/"
	@echo "  check-plugins Verify embedded plugins match canonical sources in plugins/ (CI guard)"

# Contributor bootstrap — clone -> one all-in-one container. Docker caching
# keeps rebuilds incremental. Model keys remain optional.
setup: env build up
	@echo ""
	@echo "==> SpecGate is up (no API keys required to boot). Next steps:"
	@echo "    - UI:            http://localhost:$$(. ./$(LOCAL_DEPLOY_DIR)/.env 2>/dev/null; echo $${SPECGATE_PORT:-3000})"
	@echo "    - Doc Registry:  http://localhost:$$(. ./$(LOCAL_DEPLOY_DIR)/.env 2>/dev/null; echo $${SPECGATE_PORT:-3000})/api/doc-registry"
	@echo "    - Agents API:    http://localhost:$$(. ./$(LOCAL_DEPLOY_DIR)/.env 2>/dev/null; echo $${SPECGATE_PORT:-3000})/api/agents"
	@echo "    - Set your LLM + embedding provider + key in the app:  Settings → Models"
	@echo "    - Install CLI:       curl -fsSL https://raw.githubusercontent.com/thanhtung2693/specgate/main/scripts/install-cli.sh | sh"
	@echo "    - Point CLI:         specgate config server http://localhost:$$(. ./$(LOCAL_DEPLOY_DIR)/.env 2>/dev/null; echo $${SPECGATE_PORT:-3000})/api/doc-registry"
	@echo "    - Write IDE setup:   specgate plugins install"
	@echo "    - Load demo data (optional):  make seed"

env:
	@scripts/check-ports.sh $(if $(NON_INTERACTIVE),--non-interactive,)
	@./scripts/dev-secrets.sh

build:
	docker build \
		-f docker/Dockerfile.local \
		--build-arg VERSION=$(LOCAL_VERSION) \
		-t $(LOCAL_IMAGE) \
		.

up:
	$(COMPOSE) up -d --wait

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=200 specgate

seed:
	@test -n "$(DEMO_WORKSPACE_ID)" || (echo "Set DEMO_WORKSPACE_ID: make seed DEMO_WORKSPACE_ID=<workspace-id>"; exit 2)
	$(COMPOSE) exec -T --user specgate specgate /usr/local/bin/doc-registry --seed-demo --seed-demo-workspace-id "$(DEMO_WORKSPACE_ID)" $(if $(DEMO_CREATED_BY),--seed-demo-created-by "$(DEMO_CREATED_BY)")

# Force-refresh LLM gate rubric skills from skills_seed.json. Normally this
# runs automatically on every server startup; use this target to apply seed
# changes without restarting the container.
seed-skills:
	$(COMPOSE) exec -T --user specgate specgate /usr/local/bin/doc-registry --seed-skills

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
