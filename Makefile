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

# Open a URL in the browser: xdg-open on Linux, open on macOS. Empty if neither is on PATH.
OPEN := $(shell command -v xdg-open 2>/dev/null || command -v open 2>/dev/null)

.PHONY: help site site-serve site-clean lighthouse

help: ## List the available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

site: web/node_modules ## Build the static help site into $(SITE_OUT)
	cd web && npx tsc -b && VITE_TRACK_STATIC=1 npx vite build --outDir dist-static
	go build -o $(TRACK_BIN) ./cmd/track
	./$(TRACK_BIN) export-site --src $(SITE_SRC) --root $(SITE_ROOT) --frontend $(WEB_DIST) --out $(SITE_OUT)
	@echo "Built $(SITE_OUT)/ — run 'make site-serve' to preview"

lighthouse: site ## Run Lighthouse on the built site and print the scores (needs Chrome)
	npx --yes @lhci/cli@0.14.x collect
	node scripts/lhci-summary.mjs
	@echo "Full report: open .lighthouseci/lhr-*.html"

site-serve: site ## Serve at http://localhost:$(SITE_PORT), open a browser, and rebuild on change
	@echo "Serving $(SITE_OUT) at http://localhost:$(SITE_PORT)/ (Ctrl-C to stop)"
	@python3 -m http.server --directory $(SITE_OUT) $(SITE_PORT) >/dev/null 2>&1 & \
	server=$$!; \
	trap 'kill $$server 2>/dev/null' EXIT; \
	trap 'kill $$server 2>/dev/null; exit 0' INT TERM; \
	sleep 1; \
	[ -n "$(OPEN)" ] && $(OPEN) "http://localhost:$(SITE_PORT)/" >/dev/null 2>&1 || true; \
	echo "Watching $(SITE_SRC), web/src, and the engine — edit and save to rebuild"; \
	while true; do \
		if [ -n "$$(find web/src cmd internal go.mod go.sum -type f -newer $(WEB_DIST)/index.html 2>/dev/null)" ]; then \
			echo "== frontend/engine changed — full rebuild =="; \
			$(MAKE) --no-print-directory site; \
		elif [ -n "$$(find $(SITE_SRC) -type f -newer $(SITE_OUT)/index.html 2>/dev/null)" ]; then \
			echo "== docs changed — rebuilding content =="; \
			./$(TRACK_BIN) export-site --src $(SITE_SRC) --root $(SITE_ROOT) --frontend $(WEB_DIST) --out $(SITE_OUT); \
		fi; \
		sleep 1; \
	done

site-clean: ## Remove the built site, static frontend, and CLI binary
	rm -rf $(SITE_OUT) $(WEB_DIST) $(TRACK_BIN)

# Install frontend dependencies once; re-run only when the lockfile changes.
web/node_modules: web/package-lock.json
	cd web && npm ci
	@touch web/node_modules
