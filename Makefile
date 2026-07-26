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

check-plugins:
	@scripts/check-plugins.sh
