# Local build & preview of the static help site (SSG), so you can check the published output without
# cutting a release. Run these inside `nix develop` (or with Go + Node + Python on PATH).
#
# The static help site is built into web/dist-static, kept separate from web/dist (the Vite build of
# the live workspace) so the two never clobber each other.

SITE_OUT   ?= _site
SITE_SRC   ?= docs/help
SITE_PORT  ?= 8000
WEB_ADDR   ?= 127.0.0.1:8765
TRACK_BIN  := bin/track
WEB_DIST   := web/dist-static

# Open a URL in the browser: xdg-open on Linux, open on macOS. Empty if neither is on PATH.
OPEN := $(shell command -v xdg-open 2>/dev/null || command -v open 2>/dev/null)

.PHONY: help site site-serve site-dev site-data site-clean lighthouse web-nvim

help: ## List the available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

# SITE_BASE is the deploy base path baked into the bundle (asset/data URLs + router basepath). Default
# root; set e.g. SITE_BASE=/track/ for a GitHub Pages project subpath.
SITE_BASE  ?= /

site: web/node_modules ## Build + prerender the static help site into $(SITE_OUT)
	rm -rf $(SITE_OUT)
	cd web && npx tsc -b
	cd web && VITE_TRACK_STATIC=1 SITE_BASE=$(SITE_BASE) npx vite build --outDir dist-static
	cd web && VITE_TRACK_STATIC=1 SITE_BASE=$(SITE_BASE) npx vite build --ssr src/entry-server.tsx --outDir dist-server
	go build -o $(TRACK_BIN) ./cmd/track
	./$(TRACK_BIN) export-site --src $(SITE_SRC) --frontend $(WEB_DIST) --out $(SITE_OUT)
	node web/scripts/prerender.mjs $(SITE_OUT) web/dist-server/entry-server.js
	@echo "Built + prerendered $(SITE_OUT)/ — run 'make site-serve' to preview"

lighthouse: site ## Run Lighthouse on the built site and print the scores (needs Chrome)
	npx --yes @lhci/cli@0.14.x collect
	node scripts/lhci-summary.mjs
	@echo "Full report: open .lighthouseci/lhr-*.html"

# site-data regenerates only the exported JSON bundle ($(SITE_OUT)/data) — the part that changes when a
# note changes. It needs a frontend dir to satisfy export-site, so it hands it a throwaway stub. Fast:
# no Vite build, no prerender. Re-run it after editing docs/help while `make site-dev` is running.
site-data:
	go build -o $(TRACK_BIN) ./cmd/track
	mkdir -p .site-stub && printf '<!doctype html><div id="root"></div>' > .site-stub/index.html
	./$(TRACK_BIN) export-site --src $(SITE_SRC) --frontend .site-stub --out $(SITE_OUT)

site-dev: web/node_modules site-data ## Dev preview: Vite dev server (HMR) over the exported data — fast iteration
	@echo "Vite dev server (static mode, HMR). Edit web/src for instant reload; re-run 'make site-data' after docs/help edits."
	cd web && VITE_TRACK_STATIC=1 npx vite

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
			./$(TRACK_BIN) export-site --src $(SITE_SRC) --frontend $(WEB_DIST) --out $(SITE_OUT); \
		fi; \
		sleep 1; \
	done

site-clean: ## Remove the built site, static frontend, and CLI binary
	rm -rf $(SITE_OUT) $(WEB_DIST) $(TRACK_BIN)

# Run the live web workspace the way the Neovim plugin does: headless nvim launches `:Track web`
# (using the Nix-built track, which embeds the real frontend), then blocks so the server stays up.
# Ctrl-C interrupts the wait; nvim exits and takes the web-server job down with it. Point at a vault
# with `TRACK_VAULT=<path> make web-nvim` (defaults to the plugin's configured vault).
web-nvim: ## Launch the live web workspace via headless Neovim (Ctrl-C to stop)
	@echo "track web via headless Neovim at http://$(WEB_ADDR)/ — Ctrl-C to stop"
	nix run .#test-nvim -- --headless \
		-c 'Track web --addr $(WEB_ADDR)' \
		-c 'lua vim.wait(2147483647, function() return false end)' \
		-c 'qa!'

# Install frontend dependencies once; re-run only when the lockfile changes.
web/node_modules: web/package-lock.json
	cd web && npm ci
	@touch web/node_modules
