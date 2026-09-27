package webui

import (
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/ttak0422/track/internal/track/staticapp"
)

const (
	appVaultQuery   = "__track_vault"
	staticAppsRoute = "/__track/apps/"
)

// handleAppLaunch resolves a vault-local app from the workspace origin, then redirects to the
// process's separate static-only listener. The reserved __track_vault query parameter carries the
// note's vault across this boundary and is removed before app code receives its query string.
func (s *Server) handleAppLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, fmt.Errorf("method %s not allowed", r.Method), http.StatusMethodNotAllowed)
		return
	}
	name, rel, trailing, ok := parseAppPath(r.URL.Path, r.URL.EscapedPath(), "/apps/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	selectors, present := r.URL.Query()[appVaultQuery]
	if len(selectors) > 1 {
		writeError(w, fmt.Errorf("%s may appear once", appVaultQuery), http.StatusBadRequest)
		return
	}
	selector := ""
	if present {
		selector = selectors[0]
	}
	vaultKey, vaultDir, ok := s.appVault(selector)
	if !ok {
		http.NotFound(w, r)
		return
	}
	root, err := staticapp.Open(vaultDir, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	if err := staticapp.CheckIndex(root); err != nil {
		http.NotFound(w, r)
		return
	}
	if rel != "" {
		if trailing {
			http.NotFound(w, r)
			return
		}
		f, _, err := staticapp.OpenFile(root, rel)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		f.Close()
	}
	if s.appsPort == "" {
		writeError(w, fmt.Errorf("static app listener is not running"), http.StatusServiceUnavailable)
		return
	}
	if rel == "" && !trailing {
		location := r.URL.Path + "/"
		if rawQuery := r.URL.RawQuery; rawQuery != "" {
			location += "?" + rawQuery
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, location, http.StatusPermanentRedirect)
		return
	}

	targetPath := staticAppsRoute + url.PathEscape(vaultKey) + "/" + url.PathEscape(name) + "/"
	if rel != "" {
		targetPath += rel
	}
	host := s.appRedirectHost(r.Host)
	target := url.URL{
		Scheme:   "http",
		Host:     net.JoinHostPort(host, s.appsPort),
		Path:     targetPath,
		RawQuery: withoutAppVaultQuery(r.URL.RawQuery),
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// appRedirectHost pins app URLs to the listener's chosen hostname, rather than whichever loopback
// alias happened to reach the workspace. A zero value is supported by unit tests that install an
// app port without creating the paired listener: in that case use the request's host.
func (s *Server) appRedirectHost(requestHost string) string {
	if s.appsBindHost != "" {
		return s.appsBindHost
	}
	host, _, err := net.SplitHostPort(requestHost)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(requestHost, "[]")
}

// appVault resolves a launch selector without opening the vault's index. An app launch needs only
// the registered filesystem root; it must not make static file serving depend on API/store state.
func (s *Server) appVault(name string) (selector, vaultDir string, ok bool) {
	if name == "" || name == s.active.name {
		return "launch", s.active.cfg.VaultDir, true
	}
	selector = "vault-" + name
	vaultDir, ok = s.appsVaultDirs[selector]
	if !ok {
		return "", "", false
	}
	return selector, vaultDir, true
}

// staticAppsHandler serves files only from selected vault-local app roots. It has no API mux, vault
// fallback, directory index, or SPA fallback; all filesystem opens stay under apps/<name>.
func (s *Server) staticAppsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowedAppsHost(r) {
			writeError(w, fmt.Errorf("host %q not served", r.Host), http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		vaultKey, name, rel, trailing, ok := parseStaticAppPath(r.URL.Path, r.URL.EscapedPath())
		if !ok {
			http.NotFound(w, r)
			return
		}
		vaultDir, ok := s.appsVaultDirs[vaultKey]
		if !ok {
			http.NotFound(w, r)
			return
		}
		root, err := staticapp.Open(vaultDir, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer root.Close()
		if rel == "" && !trailing {
			location := r.URL.Path + "/"
			if rawQuery := r.URL.RawQuery; rawQuery != "" {
				location += "?" + rawQuery
			}
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, location, http.StatusPermanentRedirect)
			return
		}
		if rel == "" {
			rel = "index.html"
		} else if trailing {
			http.NotFound(w, r)
			return
		}
		f, info, err := staticapp.OpenFile(root, rel)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		if contentType := staticAppContentType(path.Ext(rel)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.ServeContent(w, r, path.Base(rel), info.ModTime(), f)
	})
}

func staticAppContentType(ext string) string {
	switch strings.ToLower(ext) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	default:
		return mime.TypeByExtension(ext)
	}
}

func (s *Server) allowedAppsHost(r *http.Request) bool {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || port != s.appsPort {
		return false
	}
	if loopbackHost(host) {
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		return err == nil && loopbackHost(remote)
	}
	if s.appsBindHost == "" || host != s.appsBindHost {
		return false
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && ip.IsUnspecified() {
		return false
	}
	return true
}

func parseAppPath(urlPath, escapedPath, prefix string) (name, rel string, trailing, ok bool) {
	if strings.Contains(strings.ToLower(escapedPath), "%2f") || strings.Contains(strings.ToLower(escapedPath), "%5c") || !strings.HasPrefix(urlPath, prefix) {
		return "", "", false, false
	}
	suffix := strings.TrimPrefix(urlPath, prefix)
	if suffix == "" {
		return "", "", false, false
	}
	trailing = strings.HasSuffix(suffix, "/")
	if trailing {
		suffix = strings.TrimSuffix(suffix, "/")
	}
	segments := strings.Split(suffix, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, "\\") {
			return "", "", false, false
		}
	}
	name = segments[0]
	if !staticapp.ValidName(name) {
		return "", "", false, false
	}
	if len(segments) > 1 {
		rel = strings.Join(segments[1:], "/")
		if !staticapp.ValidRelativePath(rel) {
			return "", "", false, false
		}
	}
	return name, rel, trailing, true
}

func parseStaticAppPath(urlPath, escapedPath string) (vaultKey, name, rel string, trailing, ok bool) {
	if strings.Contains(strings.ToLower(escapedPath), "%2f") || strings.Contains(strings.ToLower(escapedPath), "%5c") || !strings.HasPrefix(urlPath, staticAppsRoute) {
		return "", "", "", false, false
	}
	suffix := strings.TrimPrefix(urlPath, staticAppsRoute)
	if suffix == "" {
		return "", "", "", false, false
	}
	trailing = strings.HasSuffix(suffix, "/")
	if trailing {
		suffix = strings.TrimSuffix(suffix, "/")
	}
	segments := strings.Split(suffix, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, "\\") {
			return "", "", "", false, false
		}
	}
	if len(segments) < 2 {
		return "", "", "", false, false
	}
	vaultKey, name = segments[0], segments[1]
	if !staticapp.ValidName(name) {
		return "", "", "", false, false
	}
	if len(segments) > 2 {
		rel = strings.Join(segments[2:], "/")
		if !staticapp.ValidRelativePath(rel) {
			return "", "", "", false, false
		}
	}
	return vaultKey, name, rel, trailing, true
}

func withoutAppVaultQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	kept := parts[:0]
	for _, part := range parts {
		key, _, _ := strings.Cut(part, "=")
		decoded, err := url.QueryUnescape(key)
		if err == nil && decoded == appVaultQuery {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "&")
}

// staticAppsAddress uses the adjacent port so a fixed workspace address gives its apps a stable
// origin across restarts. A wildcard workspace bind keeps the app listener local-only; explicit
// loopback/non-loopback hostnames are preserved as the canonical redirect hostname.
func staticAppsAddress(workspaceAddr string, workspacePort int) (addr, host string, port int, err error) {
	requestedHost, _, err := net.SplitHostPort(workspaceAddr)
	if err != nil {
		return "", "", 0, err
	}
	if workspacePort < 1 || workspacePort >= 65535 {
		return "", "", 0, fmt.Errorf("workspace port %d has no available adjacent static app port", workspacePort)
	}
	host = staticAppsHost(requestedHost)
	port = workspacePort + 1
	return net.JoinHostPort(host, strconv.Itoa(port)), host, port, nil
}

func staticAppsHost(workspaceHost string) string {
	host := strings.Trim(workspaceHost, "[]")
	if host == "" {
		return "127.0.0.1"
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return "127.0.0.1"
	}
	return host
}

func listenStaticApps(workspaceAddr string, workspaceListener net.Listener) (net.Listener, string, string, error) {
	mainTCP, ok := workspaceListener.Addr().(*net.TCPAddr)
	if !ok {
		return nil, "", "", fmt.Errorf("workspace listener has non-TCP address %s", workspaceListener.Addr())
	}
	addr, host, port, err := staticAppsAddress(workspaceAddr, mainTCP.Port)
	if err != nil {
		return nil, "", "", err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", "", fmt.Errorf("listen for static apps on %s (workspace port + 1): %w", addr, err)
	}
	return listener, host, strconv.Itoa(port), nil
}
