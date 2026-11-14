.PHONY: help build test lint generate-plugins sync-plugins check-plugins

help:
	@echo "  build            Build the Local CLI"
	@echo "  test             Run the CLI, installer, release-readiness, pilot and landing tests"
	@echo "  lint             Check the CLI with go vet"
	@echo "  sync-plugins     Regenerate and copy canonical plugins into the CLI"
	@echo "  check-plugins    Verify embedded plugins match canonical sources"

build:
	$(MAKE) -C app/cli build

test: check-plugins build
	$(MAKE) -C app/cli test
	node --test docs/release-readiness.test.mjs .github/scripts/release-notes.test.mjs scripts/install-cli.test.mjs scripts/local-review-notifications.test.mjs app/cli/test/e2e/local-delivery-proof.test.mjs app/landing/landing.test.mjs media/specgate-promo/promo.test.mjs

lint:
	$(MAKE) -C app/cli lint

generate-plugins:
	@python3 scripts/generate-plugin-metadata.py --plugin-dir plugins

sync-plugins: generate-plugins
	@sh scripts/sync-embedded-plugins.sh

check-plugins:
	@scripts/check-plugins.sh
