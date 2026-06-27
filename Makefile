# Local build & preview of the static help site (SSG), so you can check the published output without
# cutting a release. Run these inside `nix develop` (or with Go + Node + Python on PATH).
#
# The static frontend is built into web/dist-static (not web/dist) so it never shadows the live
# `track web` dev build, which is picked up from web/dist when running from the repo.

SITE_OUT   ?= _site
SITE_SRC   ?= docs/help
SITE_ROOT  ?= index
SITE_PORT  ?= 8000
TRACK_BIN  := bin/track
WEB_DIST   := web/dist-static

.PHONY: help site site-serve site-clean

help: ## List the available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

site: web/node_modules ## Build the static help site into $(SITE_OUT)
	cd web && npx tsc -b && VITE_TRACK_STATIC=1 npx vite build --outDir dist-static
	go build -o $(TRACK_BIN) ./cmd/track
	./$(TRACK_BIN) export-site --src $(SITE_SRC) --root $(SITE_ROOT) --frontend $(WEB_DIST) --out $(SITE_OUT)
	@echo "Built $(SITE_OUT)/ — run 'make site-serve' to preview"

site-serve: site ## Build, then serve the site at http://localhost:$(SITE_PORT)
	@echo "Serving $(SITE_OUT) at http://localhost:$(SITE_PORT)/ (Ctrl-C to stop)"
	cd $(SITE_OUT) && python3 -m http.server $(SITE_PORT)

site-clean: ## Remove the built site, static frontend, and CLI binary
	rm -rf $(SITE_OUT) $(WEB_DIST) $(TRACK_BIN)

# Install frontend dependencies once; re-run only when the lockfile changes.
web/node_modules: web/package-lock.json
	cd web && npm ci
	@touch web/node_modules
