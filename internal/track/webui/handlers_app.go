package webui

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// handleApp serves the embedded React frontend: a request that maps to a real built file (the hashed
// JS/CSS bundles, icons, etc.) returns that file, and anything else falls back to index.html so the
// client-side router can handle deep links like /notes/123.
func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}

	upath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if upath == "" || upath == "." || upath == "index.html" {
		s.serveIndex(w, r)
		return
	}

	f, err := s.webRoot.Open(upath)
	if err != nil {
		if strings.HasPrefix(upath, "assets/") {
			writeError(w, fmt.Errorf("asset %s not found", upath), http.StatusNotFound)
			return
		}
		s.serveIndex(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		if strings.HasPrefix(upath, "assets/") {
			writeError(w, fmt.Errorf("asset %s not found", upath), http.StatusNotFound)
			return
		}
		s.serveIndex(w, r)
		return
	}

	if ct := mime.TypeByExtension(path.Ext(upath)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// Vite emits content-hashed filenames under assets/, so those are safe to cache indefinitely; any
	// other static file (icons, etc.) is left uncached so swaps are picked up immediately.
	if strings.HasPrefix(upath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, upath, stat.ModTime(), rs)
		return
	}
	_, _ = io.Copy(w, f)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	raw, err := fs.ReadFile(s.webRoot, "index.html")
	if err != nil {
		writeError(w, fmt.Errorf("read index: %w", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	// Inject the configured default theme. Config.WebTheme is normalized to system/light/dark, so this
	// is never arbitrary text from the user's config.
	theme := s.cfg.WebTheme
	if theme == "" {
		theme = "system"
	}
	html := strings.ReplaceAll(string(raw), "__TRACK_DEFAULT_THEME__", theme)
	overrides := ""
	if s.colorCSS != "" {
		overrides = "<style id=\"track-colors\">\n" + s.colorCSS + "</style>"
	}
	html = strings.ReplaceAll(html, "__TRACK_COLOR_OVERRIDES__", overrides)
	_, _ = w.Write([]byte(html))
}

// handleAsset serves a note's media/attachments from the vault's single assets directory
// (<vault>/assets). Notes reference an attachment with the relative path "assets/<file>"; the frontend
// rewrites that to /api/asset?name=<file> so the file is served from the vault instead of being
// resolved against the /notes/<id> route and swallowed by the SPA index fallback (an embedded
// image/PDF would otherwise render the app inside itself). A legacy "kind" query parameter, if present,
// is ignored. name is constrained to the assets directory so a note cannot read arbitrary files via
// "../" traversal.
func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, errors.New("name is required"), http.StatusBadRequest)
		return
	}
	dir := s.cfg.AssetsDir()
	// Clean the slash path, drop any leading separator, then confirm the result stays inside the assets
	// directory before touching the filesystem.
	clean := strings.TrimPrefix(path.Clean("/"+name), "/")
	full := filepath.Join(dir, filepath.FromSlash(clean))
	rel, err := filepath.Rel(dir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		writeError(w, errors.New("invalid asset path"), http.StatusBadRequest)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		writeError(w, errors.New("asset not found"), http.StatusNotFound)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		writeError(w, errors.New("asset not found"), http.StatusNotFound)
		return
	}
	if ct := mime.TypeByExtension(filepath.Ext(full)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// Vault assets can change underneath us (an edit or a cloud sync), so they are not cached.
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}
